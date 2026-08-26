package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
)

// RunContext 是一次 Agent 执行的运行时环境。Values 可供插件扩展工具传递自定义依赖。
type RunContext struct {
	context.Context
	GroupID int64
	UserID  int64
	Values  map[string]any

	actionMu          sync.RWMutex
	actionPerformed   bool
	deliveryMu        sync.RWMutex
	responseDelivered bool
	deliveryKind      string
	waitingMu         sync.RWMutex
	waitingForUser    bool
	decisionMu        sync.RWMutex
	latestDecision    string
	traceMu           sync.RWMutex
	trace             RunTrace
}

// ToolTrace 是供 Skill 学习使用的脱敏工具执行记录。它不保存原始参数和结果。
type ToolTrace struct {
	Name        string
	OK          bool
	Duration    time.Duration
	GroupAction bool
}

// RunTrace 汇总一次 Agent 运行。Prompt 只在内存中交给反思模型，不直接持久化。
type RunTrace struct {
	RunID             uint64
	GroupID           int64
	UserID            int64
	Prompt            string
	StartedAt         time.Time
	Duration          time.Duration
	DecisionSteps     int
	InputTokens       int64
	OutputTokens      int64
	ToolCalls         []ToolTrace
	UsedSkillIDs      []uint
	ActionPerformed   bool
	ResponseDelivered bool
	DeliveryKind      string
	FinalAnswer       string
	Error             string
}

// MarkActionPerformed records that this run has completed a visible group action.
func (r *RunContext) MarkActionPerformed() {
	r.actionMu.Lock()
	r.actionPerformed = true
	r.actionMu.Unlock()
}

// ActionPerformed reports whether a visible group action has completed.
func (r *RunContext) ActionPerformed() bool {
	r.actionMu.RLock()
	defer r.actionMu.RUnlock()
	return r.actionPerformed
}

// MarkResponseDelivered records that the user-visible response was successfully
// delivered either by the host or by a group-action tool.
func (r *RunContext) MarkResponseDelivered(kind string) {
	r.deliveryMu.Lock()
	r.responseDelivered = true
	r.deliveryKind = strings.TrimSpace(kind)
	r.deliveryMu.Unlock()
}

// ResponseDelivered reports whether this run produced a visible response.
func (r *RunContext) ResponseDelivered() bool {
	r.deliveryMu.RLock()
	defer r.deliveryMu.RUnlock()
	return r.responseDelivered
}

func (r *RunContext) delivery() (bool, string) {
	r.deliveryMu.RLock()
	defer r.deliveryMu.RUnlock()
	return r.responseDelivered, r.deliveryKind
}

// SetWaitingForUser records whether a tool is currently waiting for a group
// member's reply. Callers can use this to avoid sending misleading progress
// messages while the next step depends on user input.
func (r *RunContext) SetWaitingForUser(waiting bool) {
	r.waitingMu.Lock()
	r.waitingForUser = waiting
	r.waitingMu.Unlock()
}

// WaitingForUser reports whether the current run is paused for user input.
func (r *RunContext) WaitingForUser() bool {
	r.waitingMu.RLock()
	defer r.waitingMu.RUnlock()
	return r.waitingForUser
}

// SetLatestDecision publishes the newest model decision for slow-run progress reporting.
func (r *RunContext) SetLatestDecision(decision string) {
	decision = strings.TrimSpace(decision)
	if decision == "" {
		return
	}
	r.decisionMu.Lock()
	r.latestDecision = decision
	r.decisionMu.Unlock()
}

// LatestDecision returns the newest decision published by the runner.
func (r *RunContext) LatestDecision() string {
	r.decisionMu.RLock()
	defer r.decisionMu.RUnlock()
	return r.latestDecision
}

func (r *RunContext) startTrace(runID uint64, prompt string) {
	r.traceMu.Lock()
	r.trace = RunTrace{RunID: runID, GroupID: r.GroupID, UserID: r.UserID, Prompt: prompt, StartedAt: time.Now()}
	r.traceMu.Unlock()
}

func (r *RunContext) recordModelStep(response *model.Response) {
	r.traceMu.Lock()
	r.trace.DecisionSteps++
	r.trace.InputTokens += response.InputToken
	r.trace.OutputTokens += response.OutToken
	r.traceMu.Unlock()
}

func (r *RunContext) recordToolCall(name string, ok, groupAction bool, duration time.Duration) {
	r.traceMu.Lock()
	r.trace.ToolCalls = append(r.trace.ToolCalls, ToolTrace{Name: name, OK: ok, GroupAction: groupAction, Duration: duration})
	r.traceMu.Unlock()
}

func (r *RunContext) markSkillUsed(id uint) {
	if id == 0 {
		return
	}
	r.traceMu.Lock()
	for _, existing := range r.trace.UsedSkillIDs {
		if existing == id {
			r.traceMu.Unlock()
			return
		}
	}
	r.trace.UsedSkillIDs = append(r.trace.UsedSkillIDs, id)
	r.traceMu.Unlock()
}

func (r *RunContext) finishTrace(answer string, err error) {
	r.traceMu.Lock()
	r.trace.Duration = time.Since(r.trace.StartedAt)
	r.trace.ActionPerformed = r.ActionPerformed()
	r.trace.ResponseDelivered, r.trace.DeliveryKind = r.delivery()
	r.trace.FinalAnswer = answer
	if err != nil {
		r.trace.Error = err.Error()
	}
	r.traceMu.Unlock()
}

// RefreshTraceOutcome synchronizes delivery state that may be completed by the
// host after the model loop has already returned.
func (r *RunContext) RefreshTraceOutcome() {
	r.traceMu.Lock()
	r.trace.ActionPerformed = r.ActionPerformed()
	r.trace.ResponseDelivered, r.trace.DeliveryKind = r.delivery()
	r.traceMu.Unlock()
}

// Trace 返回当前运行轨迹的副本。
func (r *RunContext) Trace() RunTrace {
	r.traceMu.RLock()
	defer r.traceMu.RUnlock()
	result := r.trace
	result.ToolCalls = append([]ToolTrace(nil), r.trace.ToolCalls...)
	result.UsedSkillIDs = append([]uint(nil), r.trace.UsedSkillIDs...)
	return result
}

type Handler func(*RunContext, json.RawMessage) (any, error)

type ToolRisk string

const (
	ToolRiskLow    ToolRisk = "low"
	ToolRiskMedium ToolRisk = "medium"
	ToolRiskHigh   ToolRisk = "high"
)

type Tool struct {
	Definition model.Tool
	Handler    Handler
	// Namespace groups tools by capability so search results expose a legible
	// high-level surface without renaming the stable function identifiers.
	Namespace string
	// ReadOnly and Idempotent document the execution contract used by the
	// harness, logs, tests and future policy gates.
	ReadOnly   bool
	Idempotent bool
	Risk       ToolRisk
	// SearchTerms 用于分层工具搜索，模型初始不会直接看到该工具。
	SearchTerms []string
	// GroupAction 表示工具成功后已经完成发送消息、@ 或戳一戳等群聊动作。
	GroupAction bool
	// DecisionBoundary 表示该工具的结果必须先交回模型再执行其他工具。
	// 适用于等待外部输入的工具，避免模型在拿到新信息前执行同轮的预生成动作。
	DecisionBoundary bool
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: make(map[string]Tool)} }

func (r *Registry) Register(tool Tool) error {
	name := tool.Definition.Function.Name
	if name == "" || tool.Handler == nil {
		return errors.New("agent tool must have a name and handler")
	}
	if name == "search_tools" {
		return errors.New("agent tool name search_tools is reserved")
	}
	if tool.Namespace == "" {
		tool.Namespace = "uncategorized"
	}
	if tool.Risk == "" {
		if tool.ReadOnly {
			tool.Risk = ToolRiskLow
		} else {
			tool.Risk = ToolRiskMedium
		}
	}
	switch tool.Risk {
	case ToolRiskLow, ToolRiskMedium, ToolRiskHigh:
	default:
		return fmt.Errorf("agent tool %q has invalid risk %q", name, tool.Risk)
	}
	if tool.GroupAction && tool.ReadOnly {
		return fmt.Errorf("agent tool %q cannot be both read-only and a group action", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("agent tool %q already registered", name)
	}
	r.tools[name] = tool
	return nil
}

func (r *Registry) Definitions(names map[string]bool) []model.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	selected := make([]string, 0, len(names))
	for name := range r.tools {
		if names[name] {
			selected = append(selected, name)
		}
	}
	sort.Strings(selected)
	result := make([]model.Tool, 0, len(selected)+1)
	result = append(result, searchToolDefinition())
	for _, name := range selected {
		result = append(result, r.tools[name].Definition)
	}
	return result
}

func (r *Registry) GroupActionDefinitions() []model.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0)
	for name, tool := range r.tools {
		if tool.GroupAction {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := make([]model.Tool, 0, len(names))
	for _, name := range names {
		result = append(result, r.tools[name].Definition)
	}
	return result
}

type SearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Namespace   string `json:"namespace,omitempty"`
	ReadOnly    bool   `json:"read_only"`
	Idempotent  bool   `json:"idempotent"`
	SideEffect  bool   `json:"side_effect"`
	GroupAction bool   `json:"group_action"`
	Risk        string `json:"risk"`
	Kind        string `json:"kind,omitempty"`
	SkillID     uint   `json:"skill_id,omitempty"`
	Markdown    string `json:"markdown,omitempty"`
}

// SkillMatch 是 Skill 系统向 Runner 暴露的只读、已验证能力说明。
type SkillMatch struct {
	ID            uint
	Name          string
	Description   string
	Markdown      string
	RequiredTools []string
}

// SkillSearcher 由 Skill 服务实现。Runner 只检索 Active 且未过期的 Skill。
type SkillSearcher interface {
	SearchActiveSkills(groupID int64, query string, limit int) ([]SkillMatch, error)
}

func (r *Registry) Search(query string, limit int) []SearchResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > 8 {
		limit = 5
	}
	query = strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		result SearchResult
		score  int
	}
	matches := make([]scored, 0)
	for name, tool := range r.tools {
		description := tool.Definition.Function.Description
		haystack := strings.ToLower(name + " " + description + " " + strings.Join(tool.SearchTerms, " "))
		score := 0
		if query != "" && strings.Contains(haystack, query) {
			score += 20
		}
		for _, term := range tool.SearchTerms {
			term = strings.ToLower(strings.TrimSpace(term))
			if term == "" {
				continue
			}
			if strings.Contains(query, term) {
				score += 6
			}
		}
		for _, term := range strings.Fields(strings.NewReplacer("_", " ", ",", " ", "，", " ").Replace(query)) {
			term = strings.ToLower(strings.TrimSpace(term))
			if term == "" {
				continue
			}
			if strings.Contains(haystack, term) {
				score += 3
			}
		}
		if semantic := toolSemanticScore(query, haystack); semantic > score {
			score = semantic
		}
		if score > 0 {
			risk := tool.Risk
			if risk == "" {
				risk = ToolRiskLow
			}
			matches = append(matches, scored{SearchResult{
				Name: name, Description: description, Namespace: tool.Namespace,
				ReadOnly: tool.ReadOnly, Idempotent: tool.Idempotent,
				SideEffect: !tool.ReadOnly, GroupAction: tool.GroupAction, Risk: string(risk),
			}, score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].result.Name < matches[j].result.Name
		}
		return matches[i].score > matches[j].score
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	result := make([]SearchResult, len(matches))
	for i := range matches {
		result[i] = matches[i].result
	}
	return result
}

// toolSemanticScore supplements exact substring matching for Chinese and other
// languages where strings.Fields does not provide useful word segmentation.
// It deliberately remains local and deterministic so tool discovery is cheap.
func toolSemanticScore(query, haystack string) int {
	queryRunes := normalizedRunes(query)
	haystackRunes := normalizedRunes(haystack)
	if len(queryRunes) < 2 || len(haystackRunes) < 2 {
		return 0
	}
	queryGrams := runeBigrams(queryRunes)
	haystackGrams := runeBigrams(haystackRunes)
	intersection := 0
	for gram := range queryGrams {
		if haystackGrams[gram] {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	coverage := float64(intersection) / float64(len(queryGrams))
	dice := 2 * float64(intersection) / float64(len(queryGrams)+len(haystackGrams))
	score := int(12 * maxFloat(coverage, dice))
	if score == 0 && intersection >= 2 {
		return 1
	}
	return score
}

func normalizedRunes(value string) []rune {
	result := make([]rune, 0, len(value))
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			result = append(result, r)
		}
	}
	return result
}

func runeBigrams(runes []rune) map[string]bool {
	result := make(map[string]bool)
	for i := 0; i+1 < len(runes); i++ {
		result[string(runes[i:i+2])] = true
	}
	return result
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func searchToolDefinition() model.Tool {
	return Function("search_tools", "按当前任务搜索并加载少量相关工具或已验证 Skill；需要执行能力或遇到不懂、不确定的信息时先调用", map[string]any{
		"query": map[string]any{"type": "string", "description": "完整任务目标或所需能力，例如：总结最近群聊、联网搜索不确定的信息、发送回复"},
		"limit": map[string]any{"type": "integer", "description": "最多加载几个工具，默认5，最大8"},
	}, "query")
}

func (r *Registry) execute(ctx *RunContext, call model.ToolCall) (any, error) {
	r.mu.RLock()
	tool, ok := r.tools[call.Function.Name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", call.Function.Name)
	}
	return tool.Handler(ctx, json.RawMessage(call.Function.Arguments))
}

type Runner struct {
	Model         model.LargeModel
	Tools         *Registry
	Skills        SkillSearcher
	MaxSteps      int
	MaxToolCalls  int
	RequireAction bool
	// StopAfterGroupAction prevents duplicate visible replies. A group-action
	// tool already delivered the response, so another model loop is unnecessary.
	StopAfterGroupAction bool
}

var ErrGroupActionRequired = errors.New("agent finished without a group action")
var ErrToolBudgetExceeded = errors.New("agent exceeded the tool-call budget")

var runSequence atomic.Uint64

// Run 驱动标准 function-calling 循环，直到模型不再请求工具。
func (r *Runner) Run(ctx *RunContext, prompt, imageURL string, tools ...Tool) (string, error) {
	if r.Model == nil || r.Tools == nil {
		return "", errors.New("agent runner is not initialized")
	}
	steps := r.MaxSteps
	if steps <= 0 {
		steps = 8
	}
	maxToolCalls := r.MaxToolCalls
	if maxToolCalls <= 0 {
		maxToolCalls = steps * 3
	}
	toolCallCount := 0
	toolsDefine := make([]model.Tool, 0, len(tools))
	for _, tool := range tools {
		toolsDefine = append(toolsDefine, tool.Definition)
	}
	runID := runSequence.Add(1)
	ctx.startTrace(runID, prompt)
	history := make([]model.Message, 0, steps*2)
	activeTools := make(map[string]bool)
	question := prompt
	var postDeliveryErr error
	logrus.Infof("[Agent][run=%d][开始] group=%d user=%d max_steps=%d max_tool_calls=%d require_action=%t image=%t prompt=%s", runID, ctx.GroupID, ctx.UserID, steps, maxToolCalls, r.RequireAction, imageURL != "", logValue(prompt))
	for i := 0; i < steps; i++ {
		if err := runContextError(ctx); err != nil {
			ctx.finishTrace("", err)
			logrus.Warnf("[Agent][run=%d][取消] step=%d/%d error=%v", runID, i+1, steps, err)
			return "", err
		}
		finalActionStep := r.RequireAction && !ctx.ActionPerformed() && i == steps-1
		if finalActionStep {
			question = finalActionPrompt(question)
		}
		requestQuestion, requestImageURL := question, imageURL
		definitions := r.Tools.Definitions(activeTools)
		definitions = append(definitions, toolsDefine...)
		if finalActionStep {
			definitions = r.Tools.GroupActionDefinitions()
			for _, definition := range definitions {
				activeTools[definition.Function.Name] = true
			}
		}
		logrus.Infof("[Agent][run=%d][请求模型] step=%d/%d history=%d action_done=%t active_tools=%v exposed_tools=%v question=%s", runID, i+1, steps, len(history), ctx.ActionPerformed(), sortedActiveToolNames(activeTools), definitionNames(definitions), logValue(question))
		response := new(model.Response)
		err := r.Model.Request(&model.Request{
			Context:  ctx.Context,
			Question: question,
			History:  history,
			Tools:    definitions,
			ImageURL: imageURL,
		}, response)
		if err != nil {
			ctx.finishTrace("", err)
			logrus.Errorf("[Agent][run=%d][模型请求失败] step=%d/%d error=%v", runID, i+1, steps, err)
			return "", err
		}
		if response.ErrorMsg != "" {
			responseErr := errors.New(response.ErrorMsg)
			ctx.finishTrace("", responseErr)
			logrus.Errorf("[Agent][run=%d][模型返回错误] step=%d/%d error=%s", runID, i+1, steps, response.ErrorMsg)
			return "", responseErr
		}
		ctx.recordModelStep(response)
		logrus.Infof("[Agent][run=%d][模型决策] step=%d/%d tool_calls=%v answer=%q", runID, i+1, steps, toolCallNames(response.ToolCalls), response.Answer)
		if response.Reasoning != "" {
			logrus.Infof("[Agent][run=%d][模型推理] step=%d/%d reasoning=%q", runID, i+1, steps, response.Reasoning)
		} else {
			logrus.Infof("[Agent][run=%d][模型推理] step=%d/%d reasoning=<模型未返回 reasoning_content>", runID, i+1, steps)
		}
		ctx.SetLatestDecision(decisionProgress(response))
		// Question 只会由模型适配器临时附加到当前请求；必须同步写入历史，
		// 否则下一轮工具调用会从 assistant 消息开始并丢失原始用户问题。
		if userMessage, ok := requestUserMessage(requestQuestion, requestImageURL); ok {
			history = append(history, userMessage)
			logrus.Infof("[Agent][run=%d][写入历史] step=%d/%d role=user image=%t content=%s", runID, i+1, steps, requestImageURL != "", logValue(requestQuestion))
		}
		question, imageURL = "", ""
		if len(response.ToolCalls) == 0 {
			if r.RequireAction && !ctx.ActionPerformed() {
				if i+1 == steps {
					logrus.Warnf("[Agent][run=%d][未完成] 已用完 %d 轮，但模型没有完成群聊动作", runID, steps)
					ctx.finishTrace(response.Answer, ErrGroupActionRequired)
					return response.Answer, ErrGroupActionRequired
				}
				history = append(history, model.Message{Role: "assistant", Content: response.Answer})
				question = "你还没有完成群聊动作。不得把普通 assistant 文本作为最终回复。若尚未加载发送能力，先调用 search_tools 搜索“发送最终回复”；随后必须成功调用一个 group_action=true 的工具，把只面向群友的最终回复放进工具参数，不能静默结束。"
				logrus.Warnf("[Agent][run=%d][继续决策] step=%d/%d 原因=模型未调用群聊动作 corrective_prompt=%s", runID, i+1, steps, question)
				continue
			}
			logrus.Infof("[Agent][run=%d][完成] step=%d/%d action_done=%t final_answer=%s", runID, i+1, steps, ctx.ActionPerformed(), logValue(response.Answer))
			ctx.finishTrace(response.Answer, nil)
			return response.Answer, nil
		}
		history = append(history, model.Message{Role: "assistant", Content: response.Answer, ToolCalls: response.ToolCalls, ReasoningContent: response.Reasoning})
		boundaryCallIndex, boundaryToolName := -1, ""
		for callIndex, call := range response.ToolCalls {
			if r.Tools.isDecisionBoundary(call.Function.Name) {
				boundaryCallIndex, boundaryToolName = callIndex, call.Function.Name
				break
			}
		}
		for callIndex, call := range response.ToolCalls {
			logrus.Infof("[Agent][run=%d][调用工具] step=%d/%d call_id=%s tool=%s args=%s", runID, i+1, steps, call.ID, call.Function.Name, logValue(call.Function.Arguments))
			var result any
			var callErr error
			callStarted := time.Now()
			groupAction := r.Tools.isGroupAction(call.Function.Name)
			toolCallCount++
			if err := runContextError(ctx); err != nil {
				callErr = err
			} else if r.StopAfterGroupAction && ctx.ActionPerformed() {
				callErr = errors.New("tool call skipped because a visible group action already completed this run")
			} else if toolCallCount > maxToolCalls {
				callErr = ErrToolBudgetExceeded
			} else if boundaryCallIndex >= 0 && callIndex != boundaryCallIndex {
				callErr = fmt.Errorf("tool call skipped because %q requires a new model decision before other tools run", boundaryToolName)
			} else if finalActionStep && call.Function.Name == "search_tools" {
				callErr = errors.New("the final step only allows a group action")
			} else if call.Function.Name == "search_tools" {
				var input struct {
					Query string `json:"query"`
					Limit int    `json:"limit"`
				}
				if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
					callErr = err
				} else {
					found := r.Tools.Search(input.Query, input.Limit)
					for i := range found {
						found[i].Kind = "tool"
					}
					if r.Skills != nil {
						const skillLimit = 1
						skills, skillErr := r.Skills.SearchActiveSkills(ctx.GroupID, input.Query+"\n"+prompt, skillLimit)
						if skillErr != nil {
							logrus.Warnf("[Agent][run=%d][Skill 搜索失败] query=%s error=%v", runID, logValue(input.Query), skillErr)
						} else {
							for _, item := range skills {
								validTools := make([]string, 0, len(item.RequiredTools))
								allToolsAvailable := true
								for _, toolName := range item.RequiredTools {
									if r.Tools.Has(toolName) {
										validTools = append(validTools, toolName)
									} else {
										allToolsAvailable = false
									}
								}
								if !allToolsAvailable {
									logrus.Warnf("[Agent][run=%d][Skill 跳过] skill=%s 原因=存在未注册的所需工具", runID, item.Name)
									continue
								}
								found = append(found, SearchResult{Name: item.Name, Description: item.Description, Kind: "skill", SkillID: item.ID,
									Namespace: "skill", ReadOnly: true, Idempotent: true, Risk: string(ToolRiskLow), Markdown: item.Markdown})
								ctx.markSkillUsed(item.ID)
								for _, toolName := range validTools {
									activeTools[toolName] = true
								}
							}
						}
					}
					result = found
					for _, item := range found {
						if item.Kind == "tool" {
							activeTools[item.Name] = true
						}
					}
					logrus.Infof("[Agent][run=%d][工具搜索] step=%d/%d query=%s hits=%v active_tools=%v", runID, i+1, steps, logValue(input.Query), searchResultNames(found), sortedActiveToolNames(activeTools))
				}
			} else if !activeTools[call.Function.Name] {
				callErr = fmt.Errorf("tool %q is not active; call search_tools first", call.Function.Name)
			} else {
				if groupAction && ctx.ActionPerformed() {
					callErr = errors.New("a visible group action was already completed; do not send a duplicate response")
				} else {
					result, callErr = r.Tools.execute(ctx, call)
				}
				if callErr == nil && groupAction {
					ctx.MarkActionPerformed()
					ctx.MarkResponseDelivered("tool:" + call.Function.Name)
				}
			}
			ctx.recordToolCall(call.Function.Name, callErr == nil, groupAction, time.Since(callStarted))
			payload := map[string]any{"ok": callErr == nil, "status": "ok", "tool": call.Function.Name, "result": result}
			if callErr != nil {
				payload["status"] = "error"
				payload["error"] = map[string]any{"message": callErr.Error(), "retryable": false}
				if ctx.ActionPerformed() {
					postDeliveryErr = callErr
				}
			}
			encoded, _ := json.Marshal(payload)
			if callErr != nil {
				logrus.Infof("[Agent][run=%d][工具结果] step=%d/%d call_id=%s tool=%s ok=false action_done=%t error=%q", runID, i+1, steps, call.ID, call.Function.Name, ctx.ActionPerformed(), callErr.Error())
			} else {
				logrus.Infof("[Agent][run=%d][工具结果] step=%d/%d call_id=%s tool=%s ok=true action_done=%t", runID, i+1, steps, call.ID, call.Function.Name, ctx.ActionPerformed())
			}
			history = append(history, model.Message{Role: "tool", ToolCallID: call.ID, Content: string(encoded)})
		}
		if r.StopAfterGroupAction && ctx.ActionPerformed() {
			logrus.Infof("[Agent][run=%d][完成] step=%d/%d reason=group_action_delivered", runID, i+1, steps)
			ctx.finishTrace(response.Answer, postDeliveryErr)
			return response.Answer, nil
		}
		if finalActionStep {
			if !ctx.ActionPerformed() {
				logrus.Warnf("[Agent][run=%d][未完成] 最后一步未执行群聊动作 answer=%s", runID, logValue(response.Answer))
				ctx.finishTrace(response.Answer, ErrGroupActionRequired)
				return response.Answer, ErrGroupActionRequired
			}
			logrus.Warnf("[Agent][run=%d][未完成] 最后一步已执行群聊动作，但模型尚未主动完成决策", runID)
		}
	}
	logrus.Warnf("[Agent][run=%d][未完成] exceeded maximum of %d steps action_done=%t", runID, steps, ctx.ActionPerformed())
	err := fmt.Errorf("agent exceeded maximum of %d steps", steps)
	ctx.finishTrace("", err)
	return "", err
}

func runContextError(ctx *RunContext) error {
	if ctx == nil || ctx.Context == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func finalActionPrompt(question string) string {
	const instruction = "决策步数只剩最后一步。禁止继续搜索、浏览、读取上下文或调用其他非发送工具；必须基于已经获得的信息立即形成最终判断，并成功调用当前提供的一个群聊动作工具完成回复。把只面向群友的最终回复放进工具参数，不要在其中夹带分析过程、决策说明或角色扮演提示。信息不足时应明确说明不确定性，但仍然必须回复。"
	if strings.TrimSpace(question) == "" {
		return instruction
	}
	return question + "\n\n" + instruction
}

func decisionProgress(response *model.Response) string {
	if answer := strings.TrimSpace(response.Answer); answer != "" {
		return answer
	}
	if reasoning := strings.TrimSpace(response.Reasoning); reasoning != "" {
		return reasoning
	}
	if len(response.ToolCalls) > 0 {
		names := make([]string, 0, len(response.ToolCalls))
		for _, call := range response.ToolCalls {
			names = append(names, call.Function.Name)
		}
		return "准备执行：" + strings.Join(names, "、")
	}
	return ""
}

func (r *Registry) isGroupAction(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name].GroupAction
}

func (r *Registry) isDecisionBoundary(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name].DecisionBoundary
}

// Has 判断工具是否已经注册，用于 Skill 白名单激活。
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// Names 返回所有已注册工具名的稳定排序副本。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func definitionNames(definitions []model.Tool) []string {
	names := make([]string, len(definitions))
	for i := range definitions {
		names[i] = definitions[i].Function.Name
	}
	return names
}

func toolCallNames(calls []model.ToolCall) []string {
	names := make([]string, len(calls))
	for i := range calls {
		names[i] = calls[i].Function.Name
	}
	return names
}

func sortedActiveToolNames(active map[string]bool) []string {
	names := make([]string, 0, len(active))
	for name, enabled := range active {
		if enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func searchResultNames(results []SearchResult) []string {
	names := make([]string, len(results))
	for i := range results {
		names[i] = results[i].Name
	}
	return names
}

func requestUserMessage(question, imageURL string) (model.Message, bool) {
	if question == "" && imageURL == "" {
		return model.Message{}, false
	}
	content := any(question)
	if imageURL != "" {
		content = []model.ContentPart{
			{Type: "text", Text: question},
			{Type: "image_url", ImageURL: &model.ImageURL{URL: imageURL}},
		}
	}
	return model.Message{Role: "user", Content: content}, true
}

func logValue(value string) string {
	runes := []rune(value)
	if len(runes) <= 200 {
		return value
	}

	return fmt.Sprintf(
		"%s<redacted chars=%d>%s",
		string(runes[:100]),
		len(runes)-200,
		string(runes[len(runes)-100:]),
	)
}

func Function(name, description string, properties map[string]any, required ...string) model.Tool {
	params := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		params["required"] = required
	}
	return model.Tool{Type: "function", Function: model.ToolFunction{Name: name, Description: description, Parameters: params}}
}
