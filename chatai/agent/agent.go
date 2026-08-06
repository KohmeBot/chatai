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

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
)

// RunContext 是一次 Agent 执行的运行时环境。Values 可供插件扩展工具传递自定义依赖。
type RunContext struct {
	context.Context
	GroupID int64
	UserID  int64
	Values  map[string]any

	actionMu        sync.RWMutex
	actionPerformed bool
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

type Handler func(*RunContext, json.RawMessage) (any, error)

type Tool struct {
	Definition model.Tool
	Handler    Handler
	// SearchTerms 用于分层工具搜索，模型初始不会直接看到该工具。
	SearchTerms []string
	// GroupAction 表示工具成功后已经完成发送消息、@ 或戳一戳等群聊动作。
	GroupAction bool
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

type SearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
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
		if score > 0 {
			matches = append(matches, scored{SearchResult{Name: name, Description: description}, score})
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

func searchToolDefinition() model.Tool {
	return Function("search_tools", "按当前任务搜索并加载少量相关工具；需要执行能力或遇到不懂、不确定的信息时先调用", map[string]any{
		"query": map[string]any{"type": "string", "description": "需要完成的能力，例如：读取群聊上下文、联网搜索不确定的信息、发送回复"},
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
	MaxSteps      int
	RequireAction bool
}

var ErrGroupActionRequired = errors.New("agent finished without a group action")

var runSequence atomic.Uint64

// Run 驱动标准 function-calling 循环，直到模型不再请求工具。
func (r *Runner) Run(ctx *RunContext, prompt, imageURL string) (string, error) {
	if r.Model == nil || r.Tools == nil {
		return "", errors.New("agent runner is not initialized")
	}
	steps := r.MaxSteps
	if steps <= 0 {
		steps = 8
	}
	runID := runSequence.Add(1)
	history := make([]model.Message, 0, steps*2)
	activeTools := make(map[string]bool)
	question := prompt
	logrus.Infof("[Agent][run=%d][开始] group=%d user=%d max_steps=%d require_action=%t image=%t prompt=%s", runID, ctx.GroupID, ctx.UserID, steps, r.RequireAction, imageURL != "", logValue(prompt))
	for i := 0; i < steps; i++ {
		definitions := r.Tools.Definitions(activeTools)
		logrus.Infof("[Agent][run=%d][请求模型] step=%d/%d history=%d action_done=%t active_tools=%v exposed_tools=%v question=%s", runID, i+1, steps, len(history), ctx.ActionPerformed(), sortedActiveToolNames(activeTools), definitionNames(definitions), logValue(question))
		response := new(model.Response)
		err := r.Model.Request(&model.Request{
			Question: question,
			History:  history,
			Tools:    definitions,
			ImageURL: imageURL,
		}, response)
		if err != nil {
			logrus.Errorf("[Agent][run=%d][模型请求失败] step=%d/%d error=%v", runID, i+1, steps, err)
			return "", err
		}
		if response.ErrorMsg != "" {
			logrus.Errorf("[Agent][run=%d][模型返回错误] step=%d/%d error=%s", runID, i+1, steps, response.ErrorMsg)
			return "", errors.New(response.ErrorMsg)
		}
		logrus.Infof("[Agent][run=%d][模型决策] step=%d/%d tool_calls=%d answer=%s", runID, i+1, steps, len(response.ToolCalls), logValue(response.Answer))
		if response.Reasoning != "" {
			logrus.Infof("[Agent][run=%d][模型推理] step=%d/%d reasoning=%s", runID, i+1, steps, logValue(response.Reasoning))
		} else {
			logrus.Infof("[Agent][run=%d][模型推理] step=%d/%d reasoning=<模型未返回 reasoning_content>", runID, i+1, steps)
		}
		question, imageURL = "", ""
		if len(response.ToolCalls) == 0 {
			if r.RequireAction && !ctx.ActionPerformed() {
				if i+1 == steps {
					logrus.Warnf("[Agent][run=%d][未完成] 已用完 %d 轮，但模型没有完成群聊动作", runID, steps)
					return response.Answer, ErrGroupActionRequired
				}
				history = append(history, model.Message{Role: "assistant", Content: response.Answer})
				question = "你还没有完成群聊动作。必须先调用 search_tools 加载并成功调用 send_message、send_messages、at_user 或 poke_user 中至少一个；不能静默结束。"
				logrus.Warnf("[Agent][run=%d][继续决策] step=%d/%d 原因=模型未调用群聊动作 corrective_prompt=%s", runID, i+1, steps, question)
				continue
			}
			logrus.Infof("[Agent][run=%d][完成] step=%d/%d action_done=%t final_answer=%s", runID, i+1, steps, ctx.ActionPerformed(), logValue(response.Answer))
			return response.Answer, nil
		}
		history = append(history, model.Message{Role: "assistant", Content: response.Answer, ToolCalls: response.ToolCalls, ReasoningContent: response.Reasoning})
		for _, call := range response.ToolCalls {
			logrus.Infof("[Agent][run=%d][调用工具] step=%d/%d call_id=%s tool=%s args=%s", runID, i+1, steps, call.ID, call.Function.Name, logValue(call.Function.Arguments))
			var result any
			var callErr error
			if call.Function.Name == "search_tools" {
				var input struct {
					Query string `json:"query"`
					Limit int    `json:"limit"`
				}
				if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
					callErr = err
				} else {
					found := r.Tools.Search(input.Query, input.Limit)
					result = found
					for _, item := range found {
						activeTools[item.Name] = true
					}
					logrus.Infof("[Agent][run=%d][工具搜索] step=%d/%d query=%q hits=%v active_tools=%v", runID, i+1, steps, input.Query, searchResultNames(found), sortedActiveToolNames(activeTools))
				}
			} else if !activeTools[call.Function.Name] {
				callErr = fmt.Errorf("tool %q is not active; call search_tools first", call.Function.Name)
			} else {
				result, callErr = r.Tools.execute(ctx, call)
				if callErr == nil && r.Tools.isGroupAction(call.Function.Name) {
					ctx.MarkActionPerformed()
				}
			}
			payload := map[string]any{"ok": callErr == nil, "result": result}
			if callErr != nil {
				payload["error"] = callErr.Error()
			}
			encoded, _ := json.Marshal(payload)
			logrus.Infof("[Agent][run=%d][工具结果] step=%d/%d call_id=%s tool=%s ok=%t action_done=%t payload=%s", runID, i+1, steps, call.ID, call.Function.Name, callErr == nil, ctx.ActionPerformed(), logValue(string(encoded)))
			history = append(history, model.Message{Role: "tool", ToolCallID: call.ID, Content: string(encoded)})
		}
	}
	logrus.Warnf("[Agent][run=%d][未完成] exceeded maximum of %d steps action_done=%t", runID, steps, ctx.ActionPerformed())
	return "", fmt.Errorf("agent exceeded maximum of %d steps", steps)
}

func (r *Registry) isGroupAction(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name].GroupAction
}

func definitionNames(definitions []model.Tool) []string {
	names := make([]string, len(definitions))
	for i := range definitions {
		names[i] = definitions[i].Function.Name
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

func logValue(value string) string {
	const maxRunes = 6000
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…[日志已截断]"
}

func Function(name, description string, properties map[string]any, required ...string) model.Tool {
	params := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		params["required"] = required
	}
	return model.Tool{Type: "function", Function: model.ToolFunction{Name: name, Description: description, Parameters: params}}
}
