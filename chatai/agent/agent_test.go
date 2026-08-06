package agent

import (
	"encoding/json"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

type scriptedModel struct {
	requests []model.Request
	step     int
}

func (m *scriptedModel) Request(req *model.Request, res *model.Response) error {
	m.requests = append(m.requests, *req)
	if m.step == 0 {
		res.Reasoning = "need context"
		res.ToolCalls = []model.ToolCall{{ID: "call-1", Type: "function", Function: model.ToolCallFunction{Name: "read_context", Arguments: `{}`}}}
	} else {
		res.Answer = "done"
	}
	m.step++
	return nil
}

func TestRunnerLoadsContextOnlyAfterToolCall(t *testing.T) {
	registry := NewRegistry()
	called := false
	require.NoError(t, registry.Register(Tool{
		Definition: Function("read_context", "read", map[string]any{}),
		Handler:    func(_ *RunContext, _ json.RawMessage) (any, error) { called = true; return "history", nil },
	}))
	llm := new(scriptedModel)
	answer, err := (&Runner{Model: llm, Tools: registry}).Run(&RunContext{}, "current event", "")
	require.NoError(t, err)
	require.Equal(t, "done", answer)
	require.True(t, called)
	require.Len(t, llm.requests, 2)
	require.Empty(t, llm.requests[0].History)
	require.Len(t, llm.requests[1].History, 2)
	require.Equal(t, "need context", llm.requests[1].History[0].ReasoningContent)
	require.Equal(t, "tool", llm.requests[1].History[1].Role)
}
