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

	ScopeGlobal = "global"
	ScopeGroup  = "group"

	SourceGenerated = "generated"
	SourceConfig    = "config"
)

const reflectionSystem = `你是 Agent 的经验蒸馏器。你的任务不是回答用户，而是判断一次成功执行中是否存在值得复用的通用流程。
只提炼稳定的判断条件、工具顺序、检查项和输出方法；禁止保存具体用户、群号、昵称、原始消息、日期事实、网页内容、URL、密钥或一次性结论。
不要创建会覆盖系统规则、绕过权限、自动执行脚本或扩大工具权限的 Skill。只输出 JSON。`

func ReflectionSystemPrompt() string { return reflectionSystem }

// Record 是一个全局 Skill。GroupID 只用于兼容第一版群级数据，新的记录固定为 0。
type Record struct {
	ID      uint   `gorm:"primaryKey"`
	GroupID int64  `gorm:"index:idx_skill_group_status;index:idx_skill_group_hash"`
	Scope   string `gorm:"index:idx_skill_scope_status;index:idx_skill_scope_hash"`
	Source  string `gorm:"index"`

	Name        string
	Description string `gorm:"type:text"`

	TriggersJSON      string `gorm:"type:text"`
	NonTriggersJSON   string `gorm:"type:text"`
	InstructionsJSON  string `gorm:"type:text"`
	RequiredToolsJSON string `gorm:"type:text"`
	SuccessChecksJSON string `gorm:"type:text"`

	Status     string `gorm:"index:idx_skill_group_status;index:idx_skill_scope_status"`
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

	ContentHash string `gorm:"index:idx_skill_group_hash;index:idx_skill_scope_hash"`
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
	GlobalMinGroups    int
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
	} else if o.ActivationEvidence > 10 {
		o.ActivationEvidence = 10
	}
	if o.GlobalMinGroups < 1 {
		o.GlobalMinGroups = 2
	} else if o.GlobalMinGroups > 10 {
		o.GlobalMinGroups = 10
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

// Definition 是配置文件可声明的全局 Skill；配置项启动后立即 Active。
type Definition struct {
	Name          string
	Description   string
	Triggers      []string
	NonTriggers   []string
	Instructions  []string
	RequiredTools []string
	SuccessChecks []string
}

type Experience struct {
	Trace          agent.RunTrace
	AvailableTools []string
}

type Service struct {
	db    *gorm.DB
	model model.LargeModel
	opts  Options

	learnMu sync.Mutex
	jobs    chan Experience
}

func NewService(db *gorm.DB, reflectionModel model.LargeModel, opts Options) *Service {
	opts = opts.normalized()
	service := &Service{db: db, model: reflectionModel, opts: opts, jobs: make(chan Experience, 64)}
	if opts.Enabled && db != nil {
		for i := 0; i < opts.ReflectionWorkers; i++ {
			go service.learnWorker()
		}
	}
	return service
}

// Models 返回 Skill 系统需要迁移的数据表。
func Models() []any { return []any{&Record{}, &Evidence{}} }

// Initialize 把第一版群级记录迁移并合并为全局 Skill，然后同步配置声明。
func (s *Service) Initialize(definitions []Definition) error {
	if s == nil || !s.opts.Enabled || s.db == nil {
		return nil
	}
	s.learnMu.Lock()
	defer s.learnMu.Unlock()
	if err := s.migrateLegacyRecords(); err != nil {
		return err
	}
	if err := s.syncConfiguredSkills(definitions); err != nil {
		return err
	}
	if err := s.mergeGlobalDuplicates(); err != nil {
		return err
	}
	return s.enforceGlobalEvidence()
}

func (s *Service) migrateLegacyRecords() error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var rows []Record
		if err := tx.Where("scope = '' OR scope IS NULL OR source = '' OR source IS NULL OR scope = ?", ScopeGroup).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			source := row.Source
			if source == "" {
				source = SourceGenerated
			}
			if source != SourceGenerated {
				continue
			}
			if row.GroupID != 0 {
				if err := tx.Model(&Evidence{}).Where("skill_id = ? AND group_id = 0", row.ID).Update("group_id", row.GroupID).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&Record{}).Where("id = ?", row.ID).Updates(map[string]any{
				"scope": ScopeGlobal, "source": SourceGenerated, "group_id": 0,
			}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&Record{}).Where("scope = ? AND (source = '' OR source IS NULL)", ScopeGlobal).Update("source", SourceGenerated).Error
	})
}

func (s *Service) syncConfiguredSkills(definitions []Definition) error {
	cleaned := make([]Proposal, 0, len(definitions))
	seenNames := make(map[string]bool)
	for _, definition := range definitions {
		proposal, err := validateConfiguredDefinition(definition)
		if err != nil {
			return fmt.Errorf("configured skill %q: %w", definition.Name, err)
		}
		if seenNames[proposal.Name] {
			return fmt.Errorf("configured skill %q is duplicated", proposal.Name)
		}
		seenNames[proposal.Name] = true
		cleaned = append(cleaned, proposal)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Record{}).Where("scope = ? AND source = ?", ScopeGlobal, SourceConfig).Update("status", StatusRetired).Error; err != nil {
			return err
		}
		for _, proposal := range cleaned {
			hash := proposalHash(proposal)
			var row Record
			err := tx.Where("scope = ? AND source = ? AND name = ?", ScopeGlobal, SourceConfig, proposal.Name).First(&row).Error
			updates := map[string]any{
				"group_id": 0, "scope": ScopeGlobal, "source": SourceConfig, "name": proposal.Name,
				"description": proposal.Description, "triggers_json": encodeStrings(proposal.Triggers),
				"non_triggers_json": encodeStrings(proposal.NonTriggers), "instructions_json": encodeStrings(proposal.Instructions),
				"required_tools_json": encodeStrings(proposal.RequiredTools), "success_checks_json": encodeStrings(proposal.SuccessChecks),
				"status": StatusActive, "confidence": 1.0, "base_ttl_days": 0, "expires_at": time.Time{}, "content_hash": hash,
			}
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				row = recordFromConfiguredProposal(proposal, hash)
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			case err != nil:
				return err
			default:
				if row.ContentHash != hash {
					updates["version"] = row.Version + 1
				}
				if err := tx.Model(&Record{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func recordFromConfiguredProposal(proposal Proposal, hash string) Record {
	return Record{
		GroupID: 0, Scope: ScopeGlobal, Source: SourceConfig, Name: proposal.Name, Description: proposal.Description,
		TriggersJSON: encodeStrings(proposal.Triggers), NonTriggersJSON: encodeStrings(proposal.NonTriggers),
		InstructionsJSON: encodeStrings(proposal.Instructions), RequiredToolsJSON: encodeStrings(proposal.RequiredTools),
		SuccessChecksJSON: encodeStrings(proposal.SuccessChecks), Status: StatusActive, Version: 1,
		Confidence: 1, ContentHash: hash,
	}
}

func (s *Service) mergeGlobalDuplicates() error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var rows []Record
		if err := tx.Where("scope = ? AND status <> ?", ScopeGlobal, StatusRetired).
			Order("CASE WHEN source = 'config' THEN 0 ELSE 1 END, evidence_count DESC, id ASC").Find(&rows).Error; err != nil {
			return err
		}
		canonical := make([]Record, 0, len(rows))
		for _, row := range rows {
			merged := false
			for i := range canonical {
				if canonical[i].Source == SourceConfig && row.Source == SourceConfig && canonical[i].Name != row.Name {
					continue
				}
				if !sameWorkflow(canonical[i], row) {
					continue
				}
				updated, err := mergeRecord(tx, canonical[i], row)
				if err != nil {
					return err
				}
				canonical[i] = updated
				merged = true
				break
			}
			if !merged {
				canonical = append(canonical, row)
			}
		}
		return nil
	})
}

func sameWorkflow(a, b Record) bool {
	if a.Name == b.Name && (a.Source == SourceConfig || b.Source == SourceConfig) {
		return true
	}
	if !sameStringSet(a.RequiredTools(), b.RequiredTools()) {
		return false
	}
	query := b.Description + " " + strings.Join(b.Triggers(), " ")
	return matchScore(query, a) >= 0.42
}

func (s *Service) enforceGlobalEvidence() error {
	var rows []Record
	if err := s.db.Where("scope = ? AND source = ? AND status = ?", ScopeGlobal, SourceGenerated, StatusActive).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		groups, err := s.evidenceGroupCount(row.ID, 0)
		if err != nil {
			return err
		}
		if groups < s.opts.GlobalMinGroups {
			if err := s.db.Model(&Record{}).Where("id = ?", row.ID).Update("status", StatusShadow).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func mergeRecord(tx *gorm.DB, canonical, duplicate Record) (Record, error) {
	status := strongerStatus(canonical.Status, duplicate.Status)
	if canonical.Source == SourceConfig {
		status = StatusActive
	}
	updates := map[string]any{
		"evidence_count":       canonical.EvidenceCount + duplicate.EvidenceCount,
		"success_count":        canonical.SuccessCount + duplicate.SuccessCount,
		"failure_count":        canonical.FailureCount + duplicate.FailureCount,
		"consecutive_failures": minInt(canonical.ConsecutiveFailures, duplicate.ConsecutiveFailures),
		"confidence":           maxFloat(canonical.Confidence, duplicate.Confidence),
		"status":               status,
	}
	if canonical.Source != SourceConfig && duplicate.ExpiresAt.After(canonical.ExpiresAt) {
		updates["expires_at"] = duplicate.ExpiresAt
		canonical.ExpiresAt = duplicate.ExpiresAt
	}
	if duplicate.LastUsedAt != nil && (canonical.LastUsedAt == nil || duplicate.LastUsedAt.After(*canonical.LastUsedAt)) {
		updates["last_used_at"] = duplicate.LastUsedAt
		canonical.LastUsedAt = duplicate.LastUsedAt
	}
	if duplicate.LastSuccessAt != nil && (canonical.LastSuccessAt == nil || duplicate.LastSuccessAt.After(*canonical.LastSuccessAt)) {
		updates["last_success_at"] = duplicate.LastSuccessAt
		canonical.LastSuccessAt = duplicate.LastSuccessAt
	}
	if err := tx.Model(&Evidence{}).Where("skill_id = ?", duplicate.ID).Update("skill_id", canonical.ID).Error; err != nil {
		return Record{}, err
	}
	if err := tx.Model(&Record{}).Where("id = ?", canonical.ID).Updates(updates).Error; err != nil {
		return Record{}, err
	}
	if err := tx.Delete(&Record{}, duplicate.ID).Error; err != nil {
		return Record{}, err
	}
	canonical.EvidenceCount += duplicate.EvidenceCount
	canonical.SuccessCount += duplicate.SuccessCount
	canonical.FailureCount += duplicate.FailureCount
	canonical.ConsecutiveFailures = minInt(canonical.ConsecutiveFailures, duplicate.ConsecutiveFailures)
	canonical.Confidence = maxFloat(canonical.Confidence, duplicate.Confidence)
	canonical.Status = status
	return canonical, nil
}

func strongerStatus(a, b string) string {
	rank := map[string]int{StatusDisabled: 0, StatusRetired: 1, StatusStale: 2, StatusCandidate: 3, StatusShadow: 4, StatusActive: 5}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, item := range a {
		set[item] = true
	}
	for _, item := range b {
		if !set[item] {
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
	s.learnMu.Lock()
	defer s.learnMu.Unlock()
	if err := s.retireExpiredCandidates(); err != nil {
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

// SearchActiveSkills 实现 agent.SkillSearcher。
func (s *Service) SearchActiveSkills(groupID int64, query string, limit int) ([]agent.SkillMatch, error) {
	if s == nil || !s.opts.Enabled || s.db == nil || groupID == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 2 {
		limit = 2
	}
	now := time.Now()
	if err := s.db.Model(&Record{}).Where("scope = ? AND source = ? AND status = ? AND expires_at <= ?", ScopeGlobal, SourceGenerated, StatusActive, now).
		Updates(map[string]any{"status": StatusStale}).Error; err != nil {
		return nil, err
	}
	rows, err := s.searchRecords(query, []string{StatusActive}, limit, 0.22, groupID)
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
		if err := s.db.Where("id = ? AND (scope = ? OR (scope = ? AND group_id = ?))", id, ScopeGlobal, ScopeGroup, exp.Trace.GroupID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		success := exp.Trace.ActionPerformed && exp.Trace.Error == "" && toolsCovered(row.RequiredTools(), actualTools)
		updates := map[string]any{"last_used_at": now, "evidence_count": row.EvidenceCount + 1}
		if success {
			updates["success_count"] = row.SuccessCount + 1
			updates["consecutive_failures"] = 0
			updates["last_success_at"] = now
			if row.Source == SourceGenerated {
				ttl := clampTTL(row.BaseTTLDays, s.opts.DefaultTTLDays)
				updates["expires_at"] = now.AddDate(0, 0, ttl)
				updates["confidence"] = clampConfidence(row.Confidence + 0.03)
			}
		} else {
			failures := row.ConsecutiveFailures + 1
			updates["failure_count"] = row.FailureCount + 1
			updates["consecutive_failures"] = failures
			if row.Source == SourceGenerated {
				updates["confidence"] = clampConfidence(row.Confidence - 0.15)
				if failures >= 3 {
					updates["status"] = StatusDisabled
				} else if failures >= 2 {
					updates["status"] = StatusStale
				}
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
	rows, err := s.searchRecords(exp.Trace.Prompt, []string{StatusCandidate, StatusShadow, StatusStale}, 3, 0.28, 0)
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
		if err := s.applyShadowEvidence(row, exp, covered); err != nil {
			return true, err
		}
	}
	return processed, nil
}

func (s *Service) applyShadowEvidence(row Record, exp Experience, covered bool) error {
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
	groupCount, err := s.evidenceGroupCount(row.ID, exp.Trace.GroupID)
	if err != nil {
		return err
	}
	if covered && newEvidence >= s.opts.ActivationEvidence && groupCount >= s.opts.GlobalMinGroups && float64(newSuccess)/float64(newEvidence) >= 0.8 {
		var activeCount int64
		if err := s.db.Model(&Record{}).Where("scope = ? AND source = ? AND status = ?", ScopeGlobal, SourceGenerated, StatusActive).Count(&activeCount).Error; err != nil {
			return err
		}
		if int(activeCount) < s.opts.MaxActive {
			updates["status"] = StatusActive
			updates["expires_at"] = time.Now().AddDate(0, 0, clampTTL(row.BaseTTLDays, s.opts.DefaultTTLDays))
			logrus.Infof("[Skill][global skill=%d][晋升] name=%s evidence=%d groups=%d success=%d", row.ID, row.Name, newEvidence, groupCount, newSuccess)
		}
	}
	if row.ConsecutiveFailures+1 >= 3 && !covered {
		updates["status"] = StatusRetired
	}
	if err := s.db.Model(&Record{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
		return err
	}
	return s.insertEvidence(row.ID, exp, covered, false)
}

func (s *Service) evidenceGroupCount(skillID uint, currentGroupID int64) (int, error) {
	var groups []int64
	if err := s.db.Model(&Evidence{}).Distinct("group_id").Where("skill_id = ? AND technical_success = ?", skillID, true).Pluck("group_id", &groups).Error; err != nil {
		return 0, err
	}
	seen := make(map[int64]bool, len(groups)+1)
	for _, groupID := range groups {
		if groupID != 0 {
			seen[groupID] = true
		}
	}
	if currentGroupID != 0 {
		seen[currentGroupID] = true
	}
	return len(seen), nil
}

func (s *Service) retireExpiredCandidates() error {
	return s.db.Model(&Record{}).
		Where("scope = ? AND source = ? AND status IN ? AND expires_at <= ?", ScopeGlobal, SourceGenerated, []string{StatusCandidate, StatusShadow}, time.Now()).
		Update("status", StatusRetired).Error
}

func (s *Service) createCandidate(exp Experience) error {
	var count int64
	if err := s.db.Model(&Record{}).Where("scope = ? AND source = ? AND status IN ?", ScopeGlobal, SourceGenerated, []string{StatusCandidate, StatusShadow}).Count(&count).Error; err != nil {
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
	existing, err := s.searchRecords(clean.Description+" "+strings.Join(clean.Triggers, " "),
		[]string{StatusCandidate, StatusShadow, StatusActive}, 5, 0.42, 0)
	if err != nil {
		return err
	}
	proposalRecord := Record{Name: clean.Name, Description: clean.Description,
		TriggersJSON: encodeStrings(clean.Triggers), RequiredToolsJSON: encodeStrings(clean.RequiredTools)}
	for _, row := range existing {
		if !sameWorkflow(row, proposalRecord) {
			continue
		}
		covered := toolsCovered(row.RequiredTools(), successfulToolNames(exp.Trace))
		if row.Status == StatusActive {
			return s.observeActiveMatch(row, exp, covered)
		}
		return s.applyShadowEvidence(row, exp, covered)
	}

	now := time.Now()
	record := Record{
		GroupID: 0, Scope: ScopeGlobal, Source: SourceGenerated, Name: clean.Name, Description: clean.Description,
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
	logrus.Infof("[Skill][global skill=%d][生成候选] source_group=%d name=%s ttl_days=%d confidence=%.2f", record.ID, exp.Trace.GroupID, record.Name, record.BaseTTLDays, record.Confidence)
	return nil
}

func (s *Service) observeActiveMatch(row Record, exp Experience, covered bool) error {
	if !covered {
		return nil
	}
	updates := map[string]any{
		"evidence_count": row.EvidenceCount + 1,
		"success_count":  row.SuccessCount + 1,
	}
	if row.Source == SourceGenerated {
		updates["confidence"] = clampConfidence(row.Confidence + 0.02)
		updates["expires_at"] = time.Now().AddDate(0, 0, clampTTL(row.BaseTTLDays, s.opts.DefaultTTLDays))
	}
	if err := s.db.Model(&Record{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
		return err
	}
	return s.insertEvidence(row.ID, exp, true, false)
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

func (s *Service) searchRecords(query string, statuses []string, limit int, threshold float64, legacyGroupID int64) ([]Record, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	var rows []Record
	queryDB := s.db.Where("status IN ?", statuses)
	if legacyGroupID > 0 {
		queryDB = queryDB.Where("scope = ? OR (scope = ? AND group_id = ?)", ScopeGlobal, ScopeGroup, legacyGroupID)
	} else {
		queryDB = queryDB.Where("scope = ?", ScopeGlobal)
	}
	if err := queryDB.Find(&rows).Error; err != nil {
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
var toolNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)
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

func validateConfiguredDefinition(definition Definition) (Proposal, error) {
	p := Proposal{
		ShouldCreate: true, Name: definition.Name, Description: definition.Description,
		Triggers: definition.Triggers, NonTriggers: definition.NonTriggers, Instructions: definition.Instructions,
		RequiredTools: definition.RequiredTools, SuccessChecks: definition.SuccessChecks, Confidence: 1,
	}
	p.Name = strings.TrimSpace(strings.ToLower(p.Name))
	p.Description = compactText(p.Description, 300)
	if !skillNamePattern.MatchString(p.Name) {
		return Proposal{}, errors.New("invalid skill name")
	}
	if utf8.RuneCountInString(p.Description) < 10 || containsForbiddenInstruction(p.Description) {
		return Proposal{}, errors.New("invalid skill description")
	}
	p.Triggers = cleanList(p.Triggers, 1, 12, 80)
	p.NonTriggers = cleanList(p.NonTriggers, 0, 12, 100)
	p.Instructions = cleanList(p.Instructions, 1, 16, 300)
	p.SuccessChecks = cleanList(p.SuccessChecks, 1, 8, 200)
	if len(p.Triggers) == 0 || len(p.Instructions) == 0 || len(p.SuccessChecks) == 0 {
		return Proposal{}, errors.New("skill content is incomplete")
	}
	for _, item := range append(append([]string{}, p.Instructions...), p.SuccessChecks...) {
		if containsForbiddenInstruction(item) {
			return Proposal{}, errors.New("skill contains forbidden persistent instruction")
		}
	}
	tools := make([]string, 0, len(p.RequiredTools))
	seen := make(map[string]bool)
	for _, name := range p.RequiredTools {
		name = strings.TrimSpace(name)
		if name == "search_tools" || !toolNamePattern.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		tools = append(tools, name)
	}
	if len(tools) == 0 {
		return Proposal{}, errors.New("skill has no valid required tools")
	}
	sort.Strings(tools)
	p.RequiredTools = tools
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
