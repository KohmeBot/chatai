package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	StatusCandidate = "candidate"
	StatusShadow    = "shadow"
	StatusActive    = "active"
	StatusStale     = "stale"
	StatusDisabled  = "disabled"
	StatusRetired   = "retired"
)

const reflectionSystem = `你是 Agent 的经验蒸馏器。你的任务不是回答用户，而是判断一次成功执行中是否存在值得复用的通用流程。
只提炼稳定的判断条件、工具顺序、检查项和输出方法；禁止保存具体用户、群号、昵称、原始消息、日期事实、网页内容、URL、密钥或一次性结论。
不要创建会覆盖系统规则、绕过权限、自动执行脚本或扩大工具权限的 Skill。只输出 JSON。`

func ReflectionSystemPrompt() string { return reflectionSystem }

// Record 是一个完全由运行轨迹生成的群级 Skill。
type Record struct {
	ID      uint  `gorm:"primaryKey"`
	GroupID int64 `gorm:"index:idx_skill_group_status;index:idx_skill_group_hash"`

	Name        string
	Description string `gorm:"type:text"`

	TriggersJSON      string `gorm:"type:text"`
	NonTriggersJSON   string `gorm:"type:text"`
	InstructionsJSON  string `gorm:"type:text"`
	RequiredToolsJSON string `gorm:"type:text"`
	SuccessChecksJSON string `gorm:"type:text"`

	Status     string `gorm:"index:idx_skill_group_status"`
	Version    int
	Confidence float64

	EvidenceCount       int
	SuccessCount        int
	FailureCount        int
	ConsecutiveFailures int

	BaseTTLDays   int
	ExpiresAt     time.Time `gorm:"index"`
	LastUsedAt    *time.Time
	LastSuccessAt *time.Time

	ContentHash string `gorm:"index:idx_skill_group_hash"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (Record) TableName() string { return "chatai_generated_skills" }

func (r Record) Triggers() []string      { return decodeStrings(r.TriggersJSON) }
func (r Record) NonTriggers() []string   { return decodeStrings(r.NonTriggersJSON) }
func (r Record) Instructions() []string  { return decodeStrings(r.InstructionsJSON) }
func (r Record) RequiredTools() []string { return decodeStrings(r.RequiredToolsJSON) }
func (r Record) SuccessChecks() []string { return decodeStrings(r.SuccessChecksJSON) }

// Evidence 只保存不可逆任务指纹和工具名，不保存原始聊天或工具结果。
type Evidence struct {
	ID               uint  `gorm:"primaryKey"`
	SkillID          uint  `gorm:"index"`
	GroupID          int64 `gorm:"index"`
	RunID            uint64
	TaskFingerprint  string `gorm:"index"`
	TechnicalSuccess bool
	UsedSkill        bool
	DecisionSteps    int
	ToolNamesJSON    string `gorm:"type:text"`
	CreatedAt        time.Time
}

func (Evidence) TableName() string { return "chatai_generated_skill_evidence" }

type Options struct {
	Enabled            bool
	CandidateMinSteps  int
	ActivationEvidence int
	DefaultTTLDays     int
	MaxActive          int
	MaxCandidates      int
	ReflectionWorkers  int
}

func (o Options) normalized() Options {
	if o.CandidateMinSteps <= 0 {
		o.CandidateMinSteps = 4
	}
	if o.ActivationEvidence < 2 {
		o.ActivationEvidence = 3
	}
	if o.DefaultTTLDays <= 0 {
		o.DefaultTTLDays = 30
	}
	if o.DefaultTTLDays > 90 {
		o.DefaultTTLDays = 90
	}
	if o.MaxActive <= 0 {
		o.MaxActive = 20
	}
	if o.MaxCandidates <= 0 {
		o.MaxCandidates = 50
	}
	if o.ReflectionWorkers <= 0 {
		o.ReflectionWorkers = 1
	} else if o.ReflectionWorkers > 4 {
		o.ReflectionWorkers = 4
	}
	return o
}

type Experience struct {
	Trace          agent.RunTrace
	AvailableTools []string
}

type Service struct {
	db    *gorm.DB
	model model.LargeModel
	opts  Options

	groupMu    sync.Mutex
	groupLocks map[int64]*sync.Mutex
	jobs       chan Experience
}

func NewService(db *gorm.DB, reflectionModel model.LargeModel, opts Options) *Service {
	opts = opts.normalized()
	service := &Service{db: db, model: reflectionModel, opts: opts, groupLocks: make(map[int64]*sync.Mutex), jobs: make(chan Experience, 64)}
	if opts.Enabled && db != nil {
		for i := 0; i < opts.ReflectionWorkers; i++ {
			go service.learnWorker()
		}
	}
	return service
}

// Models 返回 Skill 系统需要迁移的数据表。
func Models() []any { return []any{&Record{}, &Evidence{}} }

// ObserveAsync 在用户回复完成后学习；队列满时丢弃反思，避免拖慢主流程。
func (s *Service) ObserveAsync(exp Experience) {
	if s == nil || !s.opts.Enabled || s.db == nil {
		return
	}
	select {
	case s.jobs <- exp:
	default:
		logrus.Infof("[Skill][run=%d][跳过反思] 后台学习队列繁忙", exp.Trace.RunID)
	}
}

func (s *Service) learnWorker() {
	for exp := range s.jobs {
		if err := s.Observe(exp); err != nil {
			logrus.Warnf("[Skill][run=%d][学习失败] %v", exp.Trace.RunID, err)
		}
	}
}

// Observe 同步处理一次轨迹，主要供后台 Worker 和测试调用。
func (s *Service) Observe(exp Experience) error {
	if s == nil || !s.opts.Enabled || s.db == nil {
		return nil
	}
	groupLock := s.lockForGroup(exp.Trace.GroupID)
	groupLock.Lock()
	defer groupLock.Unlock()
	if err := s.retireExpiredCandidates(exp.Trace.GroupID); err != nil {
		return err
	}

	success := technicalSuccess(exp.Trace)
	if err := s.updateUsedSkills(exp); err != nil {
		return err
	}
	matched, err := s.validateShadowSkills(exp, success)
	if err != nil {
		return err
	}
	if !success || !s.worthReflecting(exp.Trace) || matched || s.model == nil {
		return nil
	}
	return s.createCandidate(exp)
}

func (s *Service) lockForGroup(groupID int64) *sync.Mutex {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()
	lock := s.groupLocks[groupID]
	if lock == nil {
		lock = new(sync.Mutex)
		s.groupLocks[groupID] = lock
	}
	return lock
}

// SearchActiveSkills 实现 agent.SkillSearcher。
func (s *Service) SearchActiveSkills(groupID int64, query string, limit int) ([]agent.SkillMatch, error) {
	if s == nil || !s.opts.Enabled || s.db == nil || groupID == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 2 {
		limit = 2
	}
	now := time.Now()
	if err := s.db.Model(&Record{}).Where("group_id = ? AND status = ? AND expires_at <= ?", groupID, StatusActive, now).
		Updates(map[string]any{"status": StatusStale}).Error; err != nil {
		return nil, err
	}
	rows, err := s.searchRecords(groupID, query, []string{StatusActive}, limit, 0.22)
	if err != nil {
		return nil, err
	}
	result := make([]agent.SkillMatch, 0, len(rows))
	for _, row := range rows {
		result = append(result, agent.SkillMatch{ID: row.ID, Name: row.Name, Description: row.Description,
			Instructions: row.Instructions(), SuccessChecks: row.SuccessChecks(), RequiredTools: row.RequiredTools()})
	}
	return result, nil
}

func (s *Service) worthReflecting(trace agent.RunTrace) bool {
	if trace.DecisionSteps >= s.opts.CandidateMinSteps {
		return true
	}
	nonAction := 0
	seen := make(map[string]bool)
	for _, call := range trace.ToolCalls {
		if call.OK && !call.GroupAction && call.Name != "search_tools" && !seen[call.Name] {
			seen[call.Name] = true
			nonAction++
		}
	}
	return nonAction >= 2
}

func technicalSuccess(trace agent.RunTrace) bool {
	if !trace.ActionPerformed || trace.Error != "" {
		return false
	}
	for _, call := range trace.ToolCalls {
		if !call.OK {
			return false
		}
	}
	return true
}

func (s *Service) updateUsedSkills(exp Experience) error {
	if len(exp.Trace.UsedSkillIDs) == 0 {
		return nil
	}
	now := time.Now()
	actualTools := successfulToolNames(exp.Trace)
	for _, id := range exp.Trace.UsedSkillIDs {
		var row Record
		if err := s.db.Where("id = ? AND group_id = ?", id, exp.Trace.GroupID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		success := exp.Trace.ActionPerformed && exp.Trace.Error == "" && toolsCovered(row.RequiredTools(), actualTools)
		updates := map[string]any{"last_used_at": now, "evidence_count": row.EvidenceCount + 1}
		if success {
			ttl := clampTTL(row.BaseTTLDays, s.opts.DefaultTTLDays)
			updates["success_count"] = row.SuccessCount + 1
			updates["consecutive_failures"] = 0
			updates["last_success_at"] = now
			updates["expires_at"] = now.AddDate(0, 0, ttl)
			updates["confidence"] = clampConfidence(row.Confidence + 0.03)
		} else {
			failures := row.ConsecutiveFailures + 1
			updates["failure_count"] = row.FailureCount + 1
			updates["consecutive_failures"] = failures
			updates["confidence"] = clampConfidence(row.Confidence - 0.15)
			if failures >= 3 {
				updates["status"] = StatusDisabled
			} else if failures >= 2 {
				updates["status"] = StatusStale
			}
		}
		if err := s.db.Model(&Record{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := s.insertEvidence(row.ID, exp, success, true); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validateShadowSkills(exp Experience, success bool) (bool, error) {
	rows, err := s.searchRecords(exp.Trace.GroupID, exp.Trace.Prompt, []string{StatusCandidate, StatusShadow, StatusStale}, 3, 0.28)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	usedSkills := make(map[uint]bool, len(exp.Trace.UsedSkillIDs))
	for _, id := range exp.Trace.UsedSkillIDs {
		usedSkills[id] = true
	}
	processed := false
	actual := successfulToolNames(exp.Trace)
	for _, row := range rows {
		if usedSkills[row.ID] {
			continue
		}
		processed = true
		covered := success && toolsCovered(row.RequiredTools(), actual)
		updates := map[string]any{"evidence_count": row.EvidenceCount + 1, "status": StatusShadow}
		if covered {
			updates["success_count"] = row.SuccessCount + 1
			updates["consecutive_failures"] = 0
			updates["confidence"] = clampConfidence(row.Confidence + 0.06)
		} else {
			updates["failure_count"] = row.FailureCount + 1
			updates["consecutive_failures"] = row.ConsecutiveFailures + 1
			updates["confidence"] = clampConfidence(row.Confidence - 0.08)
		}
		newEvidence := row.EvidenceCount + 1
		newSuccess := row.SuccessCount
		if covered {
			newSuccess++
		}
		if covered && newEvidence >= s.opts.ActivationEvidence && float64(newSuccess)/float64(newEvidence) >= 0.8 {
			var activeCount int64
			if err := s.db.Model(&Record{}).Where("group_id = ? AND status = ?", row.GroupID, StatusActive).Count(&activeCount).Error; err != nil {
				return true, err
			}
			if int(activeCount) < s.opts.MaxActive {
				updates["status"] = StatusActive
				updates["expires_at"] = time.Now().AddDate(0, 0, clampTTL(row.BaseTTLDays, s.opts.DefaultTTLDays))
				logrus.Infof("[Skill][group=%d skill=%d][晋升] name=%s evidence=%d success=%d", row.GroupID, row.ID, row.Name, newEvidence, newSuccess)
			}
		}
		if row.ConsecutiveFailures+1 >= 3 && !covered {
			updates["status"] = StatusRetired
		}
		if err := s.db.Model(&Record{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return true, err
		}
		if err := s.insertEvidence(row.ID, exp, covered, false); err != nil {
			return true, err
		}
	}
	return processed, nil
}

func (s *Service) retireExpiredCandidates(groupID int64) error {
	if groupID == 0 {
		return nil
	}
	return s.db.Model(&Record{}).
		Where("group_id = ? AND status IN ? AND expires_at <= ?", groupID, []string{StatusCandidate, StatusShadow}, time.Now()).
		Update("status", StatusRetired).Error
}

func (s *Service) createCandidate(exp Experience) error {
	var count int64
	if err := s.db.Model(&Record{}).Where("group_id = ? AND status IN ?", exp.Trace.GroupID, []string{StatusCandidate, StatusShadow}).Count(&count).Error; err != nil {
		return err
	}
	if int(count) >= s.opts.MaxCandidates {
		return nil
	}

	proposal, err := s.reflect(exp)
	if err != nil {
		return err
	}
	if !proposal.ShouldCreate {
		return nil
	}
	clean, err := validateProposal(proposal, exp.AvailableTools, successfulToolNames(exp.Trace), s.opts.DefaultTTLDays)
	if err != nil {
		logrus.Infof("[Skill][run=%d][候选被拒绝] %v", exp.Trace.RunID, err)
		return nil
	}
	if existing, err := s.searchRecords(exp.Trace.GroupID, clean.Description+" "+strings.Join(clean.Triggers, " "),
		[]string{StatusCandidate, StatusShadow, StatusActive}, 1, 0.42); err != nil {
		return err
	} else if len(existing) > 0 {
		return nil
	}

	now := time.Now()
	record := Record{
		GroupID: exp.Trace.GroupID, Name: clean.Name, Description: clean.Description,
		TriggersJSON: encodeStrings(clean.Triggers), NonTriggersJSON: encodeStrings(clean.NonTriggers),
		InstructionsJSON: encodeStrings(clean.Instructions), RequiredToolsJSON: encodeStrings(clean.RequiredTools),
		SuccessChecksJSON: encodeStrings(clean.SuccessChecks), Status: StatusCandidate, Version: 1,
		Confidence: clean.Confidence, EvidenceCount: 1, SuccessCount: 1, BaseTTLDays: clean.TTLDays,
		ExpiresAt: now.AddDate(0, 0, clean.TTLDays), ContentHash: proposalHash(clean),
	}
	if err := s.db.Create(&record).Error; err != nil {
		return err
	}
	if err := s.insertEvidence(record.ID, exp, true, false); err != nil {
		return err
	}
	logrus.Infof("[Skill][group=%d skill=%d][生成候选] name=%s ttl_days=%d confidence=%.2f", record.GroupID, record.ID, record.Name, record.BaseTTLDays, record.Confidence)
	return nil
}

func (s *Service) reflect(exp Experience) (Proposal, error) {
	task := sanitizeTask(exp.Trace.Prompt)
	toolSummary := make([]string, 0, len(exp.Trace.ToolCalls))
	for _, call := range exp.Trace.ToolCalls {
		toolSummary = append(toolSummary, fmt.Sprintf("%s(ok=%t, group_action=%t)", call.Name, call.OK, call.GroupAction))
	}
	question := fmt.Sprintf(`%s

请分析下面一次已经技术成功的 Agent 运行。只有在流程具有跨任务复用价值、能减少未来工具发现或决策步骤时才创建 Skill。

任务：%s
决策轮数：%d
工具轨迹：%s
当前允许引用的工具：%s

输出 JSON：
{
  "should_create": true或false,
  "name": "英文 snake_case，3-64字符",
  "description": "何时使用以及何时不应使用",
  "triggers": ["2-8个简短触发表达"],
  "non_triggers": ["容易误匹配但不应使用的场景"],
  "instructions": ["2-10条通用步骤，不复制本次任务内容"],
  "required_tools": ["只能来自允许工具列表"],
  "success_checks": ["1-6条可验证检查"],
  "ttl_days": 1到90,
  "confidence": 0到1
}`, reflectionSystem, task, exp.Trace.DecisionSteps, strings.Join(toolSummary, " -> "), strings.Join(exp.AvailableTools, ", "))
	response := new(model.Response)
	if err := s.model.Request(&model.Request{Question: question}, response); err != nil {
		return Proposal{}, err
	}
	if response.ErrorMsg != "" {
		return Proposal{}, errors.New(response.ErrorMsg)
	}
	return parseProposal(response.Answer)
}

func (s *Service) insertEvidence(skillID uint, exp Experience, success, used bool) error {
	evidence := Evidence{SkillID: skillID, GroupID: exp.Trace.GroupID, RunID: exp.Trace.RunID,
		TaskFingerprint: taskFingerprint(exp.Trace.Prompt), TechnicalSuccess: success, UsedSkill: used,
		DecisionSteps: exp.Trace.DecisionSteps, ToolNamesJSON: encodeStrings(successfulToolNames(exp.Trace)), CreatedAt: time.Now()}
	return s.db.Create(&evidence).Error
}

type scoredRecord struct {
	record Record
	score  float64
}

func (s *Service) searchRecords(groupID int64, query string, statuses []string, limit int, threshold float64) ([]Record, error) {
	if groupID == 0 || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	var rows []Record
	if err := s.db.Where("group_id = ? AND status IN ?", groupID, statuses).Find(&rows).Error; err != nil {
		return nil, err
	}
	scored := make([]scoredRecord, 0, len(rows))
	for _, row := range rows {
		score := matchScore(query, row)
		if score >= threshold && !matchesNegativeTrigger(query, row.NonTriggers()) {
			scored = append(scored, scoredRecord{record: row, score: score})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].record.Confidence > scored[j].record.Confidence
		}
		return scored[i].score > scored[j].score
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	result := make([]Record, len(scored))
	for i := range scored {
		result[i] = scored[i].record
	}
	return result, nil
}

type Proposal struct {
	ShouldCreate  bool     `json:"should_create"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Triggers      []string `json:"triggers"`
	NonTriggers   []string `json:"non_triggers"`
	Instructions  []string `json:"instructions"`
	RequiredTools []string `json:"required_tools"`
	SuccessChecks []string `json:"success_checks"`
	TTLDays       int      `json:"ttl_days"`
	Confidence    float64  `json:"confidence"`
}

var skillNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
var urlPattern = regexp.MustCompile(`(?i)https?://\S+`)
var longNumberPattern = regexp.MustCompile(`\d{5,}`)

func parseProposal(raw string) (Proposal, error) {
	raw = strings.TrimSpace(raw)
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return Proposal{}, errors.New("reflection did not return a JSON object")
	}
	var result Proposal
	if err := json.Unmarshal([]byte(raw[start:end+1]), &result); err != nil {
		return Proposal{}, err
	}
	return result, nil
}

func validateProposal(p Proposal, available, actuallyUsed []string, defaultTTL int) (Proposal, error) {
	p.Name = strings.TrimSpace(strings.ToLower(p.Name))
	p.Description = compactText(p.Description, 300)
	if !skillNamePattern.MatchString(p.Name) {
		return Proposal{}, errors.New("invalid skill name")
	}
	if utf8.RuneCountInString(p.Description) < 10 || containsForbiddenInstruction(p.Description) {
		return Proposal{}, errors.New("invalid skill description")
	}
	p.Triggers = cleanList(p.Triggers, 2, 8, 80)
	p.NonTriggers = cleanList(p.NonTriggers, 0, 8, 100)
	p.Instructions = cleanList(p.Instructions, 2, 10, 300)
	p.SuccessChecks = cleanList(p.SuccessChecks, 1, 6, 200)
	if len(p.Triggers) < 2 || len(p.Instructions) < 2 || len(p.SuccessChecks) < 1 {
		return Proposal{}, errors.New("skill content is incomplete")
	}
	for _, item := range append(append([]string{}, p.Instructions...), p.SuccessChecks...) {
		if containsForbiddenInstruction(item) {
			return Proposal{}, errors.New("skill contains forbidden persistent instruction")
		}
	}
	allowed := make(map[string]bool, len(available))
	for _, name := range available {
		allowed[name] = true
	}
	used := make(map[string]bool, len(actuallyUsed))
	for _, name := range actuallyUsed {
		used[name] = true
	}
	tools := make([]string, 0, len(p.RequiredTools))
	seen := make(map[string]bool)
	for _, name := range p.RequiredTools {
		name = strings.TrimSpace(name)
		if allowed[name] && used[name] && !seen[name] && name != "search_tools" {
			seen[name] = true
			tools = append(tools, name)
		}
	}
	if len(tools) == 0 {
		return Proposal{}, errors.New("skill has no validated tools")
	}
	sort.Strings(tools)
	p.RequiredTools = tools
	p.TTLDays = clampTTL(p.TTLDays, defaultTTL)
	p.Confidence = clampConfidence(p.Confidence)
	if p.Confidence < 0.55 {
		return Proposal{}, errors.New("skill confidence is too low")
	}
	return p, nil
}

func cleanList(items []string, minItems, maxItems, maxRunes int) []string {
	result := make([]string, 0, len(items))
	seen := make(map[string]bool)
	for _, item := range items {
		item = compactText(item, maxRunes)
		key := strings.ToLower(item)
		if item == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
		if len(result) == maxItems {
			break
		}
	}
	if len(result) < minItems {
		return nil
	}
	return result
}

func containsForbiddenInstruction(value string) bool {
	lower := strings.ToLower(value)
	for _, forbidden := range []string{
		"ignore previous", "ignore all", "system prompt", "developer message", "api key", "cookie", "authorization",
		"忽略之前", "忽略以上", "系统提示", "开发者消息", "密钥", "绕过权限", "自动执行脚本",
	} {
		if strings.Contains(lower, forbidden) {
			return true
		}
	}
	return urlPattern.MatchString(value) || longNumberPattern.MatchString(value)
}

func successfulToolNames(trace agent.RunTrace) []string {
	result := make([]string, 0, len(trace.ToolCalls))
	seen := make(map[string]bool)
	for _, call := range trace.ToolCalls {
		if call.OK && call.Name != "search_tools" && !seen[call.Name] {
			seen[call.Name] = true
			result = append(result, call.Name)
		}
	}
	return result
}

func toolsCovered(required, actual []string) bool {
	if len(required) == 0 {
		return false
	}
	available := make(map[string]bool, len(actual))
	for _, name := range actual {
		available[name] = true
	}
	for _, name := range required {
		if !available[name] {
			return false
		}
	}
	return true
}

func matchScore(query string, row Record) float64 {
	queryNormalized := normalizeMatchText(query)
	if queryNormalized == "" {
		return 0
	}
	metadata := normalizeMatchText(row.Name + " " + row.Description)
	best := maxFloat(diceCoefficient(queryNormalized, metadata), ngramCoverage(queryNormalized, metadata))
	for _, trigger := range row.Triggers() {
		normalized := normalizeMatchText(trigger)
		if normalized == "" {
			continue
		}
		if strings.Contains(queryNormalized, normalized) || strings.Contains(normalized, queryNormalized) {
			return 1
		}
		if score := maxFloat(diceCoefficient(queryNormalized, normalized), ngramCoverage(queryNormalized, normalized)); score > best {
			best = score
		}
	}
	return best
}

// ngramCoverage 以候选触发词为分母，避免完整事件 Prompt 很长时稀释短触发词。
func ngramCoverage(query, candidate string) float64 {
	querySet, candidateSet := runeBigrams(query), runeBigrams(candidate)
	if len(querySet) == 0 || len(candidateSet) == 0 {
		return 0
	}
	intersection := 0
	for gram := range candidateSet {
		if querySet[gram] {
			intersection++
		}
	}
	return float64(intersection) / float64(len(candidateSet))
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func matchesNegativeTrigger(query string, triggers []string) bool {
	normalizedQuery := normalizeMatchText(query)
	for _, trigger := range triggers {
		normalized := normalizeMatchText(trigger)
		if len([]rune(normalized)) >= 4 && strings.Contains(normalizedQuery, normalized) {
			return true
		}
	}
	return false
}

func normalizeMatchText(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func diceCoefficient(a, b string) float64 {
	aSet, bSet := runeBigrams(a), runeBigrams(b)
	if len(aSet) == 0 || len(bSet) == 0 {
		return 0
	}
	intersection := 0
	for gram := range aSet {
		if bSet[gram] {
			intersection++
		}
	}
	return 2 * float64(intersection) / float64(len(aSet)+len(bSet))
}

func runeBigrams(value string) map[string]bool {
	runes := []rune(value)
	result := make(map[string]bool)
	if len(runes) == 1 {
		result[string(runes)] = true
		return result
	}
	for i := 0; i+1 < len(runes); i++ {
		result[string(runes[i:i+2])] = true
	}
	return result
}

func sanitizeTask(value string) string {
	value = urlPattern.ReplaceAllString(value, "<url>")
	value = longNumberPattern.ReplaceAllString(value, "<id>")
	return compactText(value, 2000)
}

func compactText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes])
	}
	return value
}

func clampTTL(value, fallback int) int {
	if value <= 0 {
		value = fallback
	}
	if value < 1 {
		return 1
	}
	if value > 90 {
		return 90
	}
	return value
}

func clampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func taskFingerprint(prompt string) string {
	normalized := normalizeMatchText(sanitizeTask(prompt))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:16])
}

func proposalHash(p Proposal) string {
	encoded, _ := json.Marshal(p)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:16])
}

func encodeStrings(items []string) string {
	encoded, _ := json.Marshal(items)
	return string(encoded)
}

func decodeStrings(raw string) []string {
	if raw == "" {
		return nil
	}
	var result []string
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}
