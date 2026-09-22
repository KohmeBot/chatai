package agent

import (
	"encoding/json"
	"github.com/kohmebot/chatai/chatai/model"
	"time"
)

// Event is an ordered observation of the loop, separate from the learning trace.
// Reasoning contains only content explicitly returned by the model provider.
type Event struct {
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
	if r.observer == nil {
		return
	}
	event.RunID, event.GroupID, event.UserID = r.Trace().RunID, r.GroupID, r.UserID
	event.Time = time.Now()
	r.observer(event)
}

// Avoid serializing tool results when the optional monitor is disabled.
func (r *RunContext) observeToolResult(step int, call model.ToolCall, result any, err error, duration time.Duration) {
	if r.observer == nil {
		return
	}
	encoded, marshalErr := json.Marshal(result)
	event := Event{Kind: "tool_finished", Step: step, CallID: call.ID, Name: call.Function.Name, Result: string(encoded), DurationMS: duration.Milliseconds()}
	if marshalErr != nil {
		event.Result = "[result could not be serialized]"
	}
	if err != nil {
		event.Error = err.Error()
	}
	r.emit(event)
	if err == nil && call.Function.Name == "read_skill" {
		r.emit(Event{Kind: "skill_loaded", Step: step, CallID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
}
