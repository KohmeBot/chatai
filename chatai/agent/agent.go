package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
)

// RunContext 是一次 Agent 执行的运行时环境。Values 可供插件扩展工具传递自定义依赖。
type RunContext struct {
	context.Context
	GroupID int64
	UserID  int64
	Values  map[string]any
}

type Handler func(*RunContext, json.RawMessage) (any, error)

type Tool struct {
	Definition model.Tool
	Handler    Handler
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("agent tool %q already registered", name)
	}
	r.tools[name] = tool
	return nil
}

func (r *Registry) Definitions() []model.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]model.Tool, 0, len(names))
	for _, name := range names {
		result = append(result, r.tools[name].Definition)
	}
	return result
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
	Model    model.LargeModel
	Tools    *Registry
	MaxSteps int
}

// Run 驱动标准 function-calling 循环，直到模型不再请求工具。
func (r *Runner) Run(ctx *RunContext, prompt, imageURL string) (string, error) {
	if r.Model == nil || r.Tools == nil {
		return "", errors.New("agent runner is not initialized")
	}
	steps := r.MaxSteps
	if steps <= 0 {
		steps = 8
	}
	history := make([]model.Message, 0, steps*2)
	question := prompt
	for i := 0; i < steps; i++ {
		logrus.Infof("[Agent][group=%d user=%d step=%d/%d] 开始模型决策", ctx.GroupID, ctx.UserID, i+1, steps)
		response := new(model.Response)
		err := r.Model.Request(&model.Request{
			Question: question,
			History:  history,
			Tools:    r.Tools.Definitions(),
			ImageURL: imageURL,
		}, response)
		if err != nil {
			return "", err
		}
		if response.ErrorMsg != "" {
			logrus.Errorf("[Agent][group=%d step=%d] 模型错误: %s", ctx.GroupID, i+1, response.ErrorMsg)
			return "", errors.New(response.ErrorMsg)
		}
		if response.Reasoning != "" {
			logrus.Infof("[Agent][group=%d step=%d][思维链] %s", ctx.GroupID, i+1, logValue(response.Reasoning))
		} else {
			logrus.Infof("[Agent][group=%d step=%d][思维链] 模型未返回 reasoning_content", ctx.GroupID, i+1)
		}
		if response.Answer != "" && len(response.ToolCalls) > 0 {
			logrus.Infof("[Agent][group=%d step=%d][模型输出] %s", ctx.GroupID, i+1, logValue(response.Answer))
		}
		question, imageURL = "", ""
		if len(response.ToolCalls) == 0 {
			logrus.Infof("[Agent][group=%d step=%d][最终输出] %s", ctx.GroupID, i+1, logValue(response.Answer))
			return response.Answer, nil
		}
		history = append(history, model.Message{Role: "assistant", Content: response.Answer, ToolCalls: response.ToolCalls, ReasoningContent: response.Reasoning})
		for _, call := range response.ToolCalls {
			logrus.Infof("[Agent][group=%d step=%d][工具调用] %s args=%s", ctx.GroupID, i+1, call.Function.Name, logValue(call.Function.Arguments))
			result, callErr := r.Tools.execute(ctx, call)
			payload := map[string]any{"ok": callErr == nil, "result": result}
			if callErr != nil {
				payload["error"] = callErr.Error()
			}
			encoded, _ := json.Marshal(payload)
			logrus.Infof("[Agent][group=%d step=%d][工具结果] %s result=%s", ctx.GroupID, i+1, call.Function.Name, logValue(string(encoded)))
			history = append(history, model.Message{Role: "tool", ToolCallID: call.ID, Content: string(encoded)})
		}
	}
	return "", fmt.Errorf("agent exceeded maximum of %d steps", steps)
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
