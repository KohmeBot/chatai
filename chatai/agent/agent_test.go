package agent

import (
	"encoding/json"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

type scriptedModel struct {
	requests []model.Request
	steps    []model.Response
}

func (m *scriptedModel) Request(req *model.Request, res *model.Response) error {
	m.requests = append(m.requests, *req)
	*res = m.steps[len(m.requests)-1]
	return nil
}

func call(id, name, arguments string) model.ToolCall {
	return model.ToolCall{ID: id, Type: "function", Function: model.ToolCallFunction{Name: name, Arguments: arguments}}
}

func TestRunnerSearchesBeforeLoadingAndCallingTool(t *testing.T) {
	registry := NewRegistry()
	called := false
	require.NoError(t, registry.Register(Tool{
		Definition:  Function("read_context", "read context", map[string]any{}),
		SearchTerms: []string{"context"},
		Handler:     func(_ *RunContext, _ json.RawMessage) (any, error) { called = true; return "history", nil },
	}))
	llm := &scriptedModel{steps: []model.Response{
		{Reasoning: "find a context tool", ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"context"}`)}},
		{Reasoning: "read it", ToolCalls: []model.ToolCall{call("read", "read_context", `{}`)}},
		{Answer: "done"},
	}}
	answer, err := (&Runner{Model: llm, Tools: registry}).Run(&RunContext{}, "current event", "")
	require.NoError(t, err)
	require.Equal(t, "done", answer)
	require.True(t, called)
	require.Len(t, llm.requests, 3)
	require.Equal(t, []string{"search_tools"}, toolNames(llm.requests[0].Tools))
	require.Equal(t, []string{"search_tools", "read_context"}, toolNames(llm.requests[1].Tools))
	require.Equal(t, "user", llm.requests[1].History[0].Role)
	require.Equal(t, "current event", llm.requests[1].History[0].Content)
	require.Len(t, llm.requests[2].History, 5)
	require.Equal(t, "tool", llm.requests[2].History[4].Role)
}

func TestRunnerRejectsUnloadedTool(t *testing.T) {
	registry := NewRegistry()
	called := false
	require.NoError(t, registry.Register(Tool{
		Definition: Function("hidden", "hidden", map[string]any{}),
		Handler:    func(_ *RunContext, _ json.RawMessage) (any, error) { called = true; return nil, nil },
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("hidden", "hidden", `{}`)}},
		{Answer: "done"},
	}}
	_, err := (&Runner{Model: llm, Tools: registry}).Run(&RunContext{}, "current event", "")
	require.NoError(t, err)
	require.False(t, called)
	require.Contains(t, llm.requests[1].History[2].Content, "not active")
}

func TestRunnerPreservesImageUserMessageAcrossToolRounds(t *testing.T) {
	registry := NewRegistry()
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"missing"}`)}},
		{Answer: "done"},
	}}
	_, err := (&Runner{Model: llm, Tools: registry}).Run(&RunContext{}, "describe this", "https://example.com/image.png")
	require.NoError(t, err)
	require.Len(t, llm.requests[1].History, 3)
	require.Equal(t, "user", llm.requests[1].History[0].Role)
	parts, ok := llm.requests[1].History[0].Content.([]model.ContentPart)
	require.True(t, ok)
	require.Equal(t, "describe this", parts[0].Text)
	require.Equal(t, "https://example.com/image.png", parts[1].ImageURL.URL)
}

func TestRegistryRejectsReservedSearchToolName(t *testing.T) {
	err := NewRegistry().Register(Tool{
		Definition: Function("search_tools", "override", map[string]any{}),
		Handler:    func(_ *RunContext, _ json.RawMessage) (any, error) { return nil, nil },
	})
	require.Error(t, err)
}

func TestRunnerRequiresSuccessfulGroupAction(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Tool{
		Definition:  Function("send_message", "send", map[string]any{}),
		SearchTerms: []string{"send"},
		GroupAction: true,
		Handler:     func(_ *RunContext, _ json.RawMessage) (any, error) { return "sent", nil },
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"send"}`)}},
		{ToolCalls: []model.ToolCall{call("send", "send_message", `{}`)}},
		{Answer: "done"},
	}}
	runCtx := &RunContext{}
	_, err := (&Runner{Model: llm, Tools: registry, RequireAction: true}).Run(runCtx, "reply", "")
	require.NoError(t, err)
	require.True(t, runCtx.ActionPerformed())
}

func TestRunnerRepromptsThenErrorsWhenModelStaysSilent(t *testing.T) {
	registry := NewRegistry()
	llm := &scriptedModel{steps: []model.Response{{}, {}}}
	_, err := (&Runner{Model: llm, Tools: registry, MaxSteps: 2, RequireAction: true}).Run(&RunContext{}, "reply", "")
	require.ErrorIs(t, err, ErrGroupActionRequired)
	require.Contains(t, llm.requests[1].Question, "必须")
}

func toolNames(tools []model.Tool) []string {
	names := make([]string, len(tools))
	for i := range tools {
		names[i] = tools[i].Function.Name
	}
	return names
}
