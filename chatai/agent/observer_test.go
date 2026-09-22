package agent

import (
	"encoding/json"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

func TestObserverPreservesLoopOrderAndPayloads(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Tool{Definition: Function("read_skill", "read skill", map[string]any{}), Handler: func(*RunContext, json.RawMessage) (any, error) { return "skill contents", nil }}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("s", "search_tools", `{"query":"skill"}`)}},
		{Reasoning: "provider reasoning", InputToken: 10, OutToken: 3, ToolCalls: []model.ToolCall{call("r", "read_skill", `{"name":"example"}`)}},
		{Answer: "done"},
	}}
	var events []Event
	_, err := (&Runner{Model: llm, Tools: registry, Observer: func(e Event) { events = append(events, e) }}).Run(&RunContext{GroupID: 123, UserID: 456}, "prompt", "user input", "")
	require.NoError(t, err)
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
		require.EqualValues(t, 123, e.GroupID)
		require.EqualValues(t, 456, e.UserID)
		require.Equal(t, events[0].RunID, e.RunID)
		require.False(t, e.Time.IsZero())
	}
	require.Equal(t, []string{"run_started", "model_started", "model_finished", "tool_started", "tool_finished", "model_started", "model_finished", "tool_started", "tool_finished", "skill_loaded", "model_started", "model_finished", "run_finished"}, kinds)
	require.Equal(t, "user input", events[0].Text)
	require.Equal(t, "provider reasoning", events[6].Reasoning)
	require.EqualValues(t, 10, events[6].InputTokens)
	require.JSONEq(t, `{"name":"example"}`, events[7].Arguments)
	require.Equal(t, `"skill contents"`, events[8].Result)
}

func TestObserverModelErrorFinishesRun(t *testing.T) {
	var events []Event
	llm := &scriptedModel{steps: []model.Response{{ErrorMsg: "rate limited", InputToken: 9}}}
	_, err := (&Runner{Model: llm, Tools: NewRegistry(), Observer: func(e Event) { events = append(events, e) }}).Run(&RunContext{}, "prompt", "user", "")
	require.Error(t, err)
	require.Len(t, events, 4)
	require.Equal(t, "rate limited", events[2].Error)
	require.EqualValues(t, 9, events[2].InputTokens)
	require.Equal(t, "run_finished", events[3].Kind)
	require.Equal(t, "rate limited", events[3].Error)
}
