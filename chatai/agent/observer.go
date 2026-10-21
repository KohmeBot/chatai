package agent

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
)

// Event is an ordered observation of the loop, separate from the learning trace.
// Reasoning contains only content explicitly returned by the model provider.
type Event struct {
	Summary      string    `json:"summary,omitempty"`
	UserName     string    `json:"user_name,omitempty"`
	RunID        uint64    `json:"run_id,string"`
	GroupID      int64     `json:"group_id,string"`
	UserID       int64     `json:"user_id,string"`
	Time         time.Time `json:"time"`
	Kind         string    `json:"kind"`
	Step         int       `json:"step,omitempty"`
	Model        string    `json:"model,omitempty"`
	CallID       string    `json:"call_id,omitempty"`
	Name         string    `json:"name,omitempty"`
	Text         string    `json:"text,omitempty"`
	Arguments    string    `json:"arguments,omitempty"`
	Result       string    `json:"result,omitempty"`
	Reasoning    string    `json:"reasoning,omitempty"`
	Error        string    `json:"error,omitempty"`
	DurationMS   int64     `json:"duration_ms,omitempty"`
	InputTokens  int64     `json:"input_tokens,omitempty"`
	OutputTokens int64     `json:"output_tokens,omitempty"`
}

type Observer func(Event)

func (r *RunContext) emit(event Event) {
	event.Summary = conciseSummary(event.Summary)
	if event.Summary == "" {
		switch event.Kind {
		case "run_started":
			event.Summary = "已收到任务，准备开始处理。"
		case "model_started":
			if event.Step <= 1 {
				event.Summary = "正在理解任务并确定接下来的操作。"
			} else {
				event.Summary = "正在检查上一步结果，确定下一步操作。"
			}
		case "tool_started":
			event.Summary = "正在调用：" + event.Name
		case "tool_finished":
			event.Summary = "已完成工具调用，准备检查结果。"
		case "skill_loaded":
			event.Summary = "已加载技能说明，供后续操作参考。"
		case "run_finished":
			event.Summary = "本轮处理已结束。"
		}
	}
	if event.Error != "" {
		event.Summary = "当前阶段失败，请查看错误详情。"
	}
	if event.Summary != "" {
		logrus.Infof("[Agent][run=%d][进展] step=%d %s", r.Trace().RunID, event.Step, event.Summary)
	}
	if r.observer == nil {
		return
	}
	event.RunID, event.GroupID, event.UserID = r.Trace().RunID, r.GroupID, r.UserID
	event.Time = time.Now()
	r.observer(event)
}

// Progress is a short public description, never a slice of provider reasoning.
func conciseSummary(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return text
}

// Avoid serializing tool results when the optional monitor is disabled.
func (r *RunContext) observeToolResult(step int, call model.ToolCall, result any, err error, duration time.Duration) {
	event := Event{Kind: "tool_finished", Step: step, CallID: call.ID, Name: call.Function.Name, DurationMS: duration.Milliseconds()}
	if r.observer != nil {
		encoded, marshalErr := json.Marshal(result)
		event.Result = string(encoded)
		if marshalErr != nil {
			event.Result = "[result could not be serialized]"
		}
	}
	if err != nil {
		event.Error = err.Error()
	}
	r.emit(event)
	if err == nil && call.Function.Name == "read_skill" {
		r.emit(Event{Kind: "skill_loaded", Step: step, CallID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
}
