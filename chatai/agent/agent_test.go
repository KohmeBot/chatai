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

type scriptedSkillSearcher struct {
	matches []SkillMatch
}

func (s scriptedSkillSearcher) SearchActiveSkills(_ int64, _ string, _ int) ([]SkillMatch, error) {
	return s.matches, nil
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

func TestRunnerSearchLoadsSkillAndActivatesRequiredTools(t *testing.T) {
	registry := NewRegistry()
	called := false
	require.NoError(t, registry.Register(Tool{
		Definition: Function("read_context", "read context", map[string]any{}),
		Handler:    func(_ *RunContext, _ json.RawMessage) (any, error) { called = true; return "history", nil },
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"总结最近群聊"}`)}},
		{ToolCalls: []model.ToolCall{call("read", "read_context", `{}`)}},
		{Answer: "done", InputToken: 12, OutToken: 3},
	}}
	runCtx := &RunContext{GroupID: 7, UserID: 8}
	runner := &Runner{Model: llm, Tools: registry, Skills: scriptedSkillSearcher{matches: []SkillMatch{{
		ID: 9, Name: "summarize_chat", Description: "总结群聊", Instructions: []string{"读取上下文", "按主题总结"},
		RequiredTools: []string{"read_context"}, SuccessChecks: []string{"覆盖时间范围"},
	}}}}
	answer, err := runner.Run(runCtx, "总结一下", "")
	require.NoError(t, err)
	require.Equal(t, "done", answer)
	require.True(t, called)
	require.Contains(t, toolNames(llm.requests[1].Tools), "read_context")
	require.Contains(t, llm.requests[1].History[2].Content, `"kind":"skill"`)
	trace := runCtx.Trace()
	require.Equal(t, uint64(1), uint64(len(trace.UsedSkillIDs)))
	require.Equal(t, uint(9), trace.UsedSkillIDs[0])
	require.Equal(t, 3, trace.DecisionSteps)
	require.Equal(t, int64(12), trace.InputTokens)
	require.Equal(t, int64(3), trace.OutputTokens)
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
		{Answer: "decision complete"},
	}}
	runCtx := &RunContext{}
	_, err := (&Runner{Model: llm, Tools: registry, RequireAction: true}).Run(runCtx, "reply", "")
	require.NoError(t, err)
	require.True(t, runCtx.ActionPerformed())
	require.Len(t, llm.requests, 3, "runner should let the model decide whether to finish after the visible action")
	require.Contains(t, llm.requests[2].History[len(llm.requests[2].History)-1].Content, "sent")
}

func TestRunnerDoesNotFinishWhileModelKeepsCallingToolsAfterGroupAction(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Tool{
		Definition: Function("send_message", "send", map[string]any{}), GroupAction: true,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) { return "sent", nil },
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"send"}`)}},
		{ToolCalls: []model.ToolCall{call("send", "send_message", `{}`)}},
		{ToolCalls: []model.ToolCall{call("send-again", "send_message", `{}`)}},
	}}
	runCtx := &RunContext{}
	_, err := (&Runner{Model: llm, Tools: registry, MaxSteps: 3, RequireAction: true}).Run(runCtx, "reply", "")
	require.EqualError(t, err, "agent exceeded maximum of 3 steps")
	require.True(t, runCtx.ActionPerformed())
	require.Len(t, llm.requests, 3)
}

func TestRunnerRepromptsThenErrorsWhenModelStaysSilent(t *testing.T) {
	registry := NewRegistry()
	llm := &scriptedModel{steps: []model.Response{{}, {}}}
	_, err := (&Runner{Model: llm, Tools: registry, MaxSteps: 2, RequireAction: true}).Run(&RunContext{}, "reply", "")
	require.ErrorIs(t, err, ErrGroupActionRequired)
	require.Contains(t, llm.requests[1].Question, "必须")
}

func TestRunnerFinalStepOnlyOffersAndExecutesGroupActions(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Tool{
		Definition:  Function("search_web", "search", map[string]any{}),
		SearchTerms: []string{"web"},
		Handler:     func(_ *RunContext, _ json.RawMessage) (any, error) { return "result", nil },
	}))
	sent := false
	require.NoError(t, registry.Register(Tool{
		Definition:  Function("send_message", "send", map[string]any{}),
		SearchTerms: []string{"send"},
		GroupAction: true,
		Handler:     func(_ *RunContext, _ json.RawMessage) (any, error) { sent = true; return "sent", nil },
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("find-web", "search_tools", `{"query":"web"}`)}},
		{ToolCalls: []model.ToolCall{call("web", "search_web", `{}`)}},
		{ToolCalls: []model.ToolCall{call("send", "send_message", `{}`)}},
	}}

	_, err := (&Runner{Model: llm, Tools: registry, MaxSteps: 3, RequireAction: true}).Run(&RunContext{}, "understand this", "")
	require.EqualError(t, err, "agent exceeded maximum of 3 steps")
	require.True(t, sent)
	require.Equal(t, []string{"send_message"}, toolNames(llm.requests[2].Tools))
	require.Contains(t, llm.requests[2].Question, "最后一步")
	require.Contains(t, llm.requests[2].Question, "禁止继续搜索")
}

func TestRunnerReturnsFinalTextWhenLastStepDoesNotCallAction(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Tool{
		Definition: Function("send_message", "send", map[string]any{}), GroupAction: true,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) { return "sent", nil },
	}))
	llm := &scriptedModel{steps: []model.Response{{Answer: "基于现有资料，这是最终判断。"}}}

	answer, err := (&Runner{Model: llm, Tools: registry, MaxSteps: 1, RequireAction: true}).Run(&RunContext{}, "reply", "")
	require.ErrorIs(t, err, ErrGroupActionRequired)
	require.Equal(t, "基于现有资料，这是最终判断。", answer)
	require.Equal(t, []string{"send_message"}, toolNames(llm.requests[0].Tools))
}

func TestRunContextPublishesLatestDecision(t *testing.T) {
	runCtx := new(RunContext)
	runCtx.SetLatestDecision("  正在整理搜索结果  ")
	require.Equal(t, "正在整理搜索结果", runCtx.LatestDecision())
}

func toolNames(tools []model.Tool) []string {
	names := make([]string, len(tools))
	for i := range tools {
		names[i] = tools[i].Function.Name
	}
	return names
}
