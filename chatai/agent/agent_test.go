package agent

import (
	"context"
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
		ID: 9, Name: "summarize_chat", Description: "总结群聊",
		Markdown:      "# 群聊总结\n\n读取相关上下文，按主题总结，并确认覆盖用户指定的时间范围。",
		RequiredTools: []string{"read_context"},
	}}}}
	answer, err := runner.Run(runCtx, "总结一下", "")
	require.NoError(t, err)
	require.Equal(t, "done", answer)
	require.True(t, called)
	require.Contains(t, toolNames(llm.requests[1].Tools), "read_context")
	require.Contains(t, llm.requests[1].History[2].Content, `"kind":"skill"`)
	require.Contains(t, llm.requests[1].History[2].Content, `"markdown":"# 群聊总结`)
	require.NotContains(t, llm.requests[1].History[2].Content, "required_tools")
	trace := runCtx.Trace()
	require.Equal(t, uint64(1), uint64(len(trace.UsedSkillIDs)))
	require.Equal(t, uint(9), trace.UsedSkillIDs[0])
	require.Equal(t, 3, trace.DecisionSteps)
	require.Equal(t, int64(12), trace.InputTokens)
	require.Equal(t, int64(3), trace.OutputTokens)
}

func TestRunnerLoadsInstructionOnlyMarkdownSkill(t *testing.T) {
	registry := NewRegistry()
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"组织讨论结论"}`)}},
		{Answer: "done"},
	}}
	runCtx := &RunContext{GroupID: 7}
	_, err := (&Runner{Model: llm, Tools: registry, Skills: scriptedSkillSearcher{matches: []SkillMatch{{
		ID: 11, Name: "organize_discussion", Description: "组织群聊讨论结论",
		Markdown: "先区分已达成共识、仍有分歧和待确认事项，再形成简洁结论。",
	}}}}).Run(runCtx, "整理讨论结论", "")
	require.NoError(t, err)
	require.Equal(t, []uint{11}, runCtx.Trace().UsedSkillIDs)
	require.Contains(t, llm.requests[1].History[2].Content, "待确认事项")
}

func TestRunnerSkipsSkillWhenAnyRequiredToolIsMissing(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Tool{
		Definition: Function("read_context", "read context", map[string]any{}),
		Handler:    func(_ *RunContext, _ json.RawMessage) (any, error) { return "history", nil },
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"总结群聊"}`)}},
		{Answer: "done"},
	}}
	runCtx := &RunContext{GroupID: 7}
	_, err := (&Runner{Model: llm, Tools: registry, Skills: scriptedSkillSearcher{matches: []SkillMatch{{
		ID: 10, Name: "broken_skill", Description: "missing tool", RequiredTools: []string{"read_context", "missing_tool"},
	}}}}).Run(runCtx, "总结群聊", "")
	require.NoError(t, err)
	require.Empty(t, runCtx.Trace().UsedSkillIDs)
	require.NotContains(t, toolNames(llm.requests[1].Tools), "read_context")
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

func TestRegistryValidatesAndNormalizesToolContract(t *testing.T) {
	registry := NewRegistry()
	require.Error(t, registry.Register(Tool{
		Definition: Function("invalid_risk", "invalid", map[string]any{}), Risk: ToolRisk("critical"),
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) { return nil, nil },
	}))
	require.Error(t, registry.Register(Tool{
		Definition: Function("contradictory", "invalid", map[string]any{}), ReadOnly: true, GroupAction: true,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) { return nil, nil },
	}))
	require.NoError(t, registry.Register(Tool{
		Definition: Function("legacy_side_effect", "legacy", map[string]any{}), SearchTerms: []string{"legacy"},
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) { return nil, nil },
	}))
	result := registry.Search("legacy", 1)
	require.Len(t, result, 1)
	require.Equal(t, "uncategorized", result[0].Namespace)
	require.Equal(t, "medium", result[0].Risk)
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

func TestRunnerStopsAfterDeliveredGroupActionWhenConfigured(t *testing.T) {
	registry := NewRegistry()
	sendCount := 0
	require.NoError(t, registry.Register(Tool{
		Definition: Function("send_message", "send", map[string]any{}), SearchTerms: []string{"send"}, GroupAction: true,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
			sendCount++
			return "sent", nil
		},
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"send"}`)}},
		{ToolCalls: []model.ToolCall{call("send", "send_message", `{}`)}},
		{Answer: "this must not become a duplicate reply"},
	}}
	runCtx := new(RunContext)
	_, err := (&Runner{Model: llm, Tools: registry, StopAfterGroupAction: true}).Run(runCtx, "reply", "")
	require.NoError(t, err)
	require.Equal(t, 1, sendCount)
	require.Len(t, llm.requests, 2)
	require.True(t, runCtx.ResponseDelivered())
	require.Equal(t, "tool:send_message", runCtx.Trace().DeliveryKind)
}

func TestRunnerRejectsDuplicateGroupActionsInOneDecision(t *testing.T) {
	registry := NewRegistry()
	sendCount := 0
	require.NoError(t, registry.Register(Tool{
		Definition: Function("send_message", "send", map[string]any{}), SearchTerms: []string{"send"}, GroupAction: true,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
			sendCount++
			return "sent", nil
		},
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"send"}`)}},
		{ToolCalls: []model.ToolCall{call("first", "send_message", `{}`), call("duplicate", "send_message", `{}`)}},
	}}
	runCtx := new(RunContext)
	_, err := (&Runner{Model: llm, Tools: registry, StopAfterGroupAction: true}).Run(runCtx, "reply", "")
	require.NoError(t, err)
	require.Equal(t, 1, sendCount)
	trace := runCtx.Trace()
	require.Len(t, trace.ToolCalls, 3)
	require.True(t, trace.ToolCalls[1].OK)
	require.False(t, trace.ToolCalls[2].OK)
}

func TestRunnerSkipsRemainingSideEffectsAfterDeliveredAction(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Tool{
		Definition: Function("send_message", "send", map[string]any{}), SearchTerms: []string{"send update"}, GroupAction: true,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) { return "sent", nil },
	}))
	updated := false
	require.NoError(t, registry.Register(Tool{
		Definition: Function("update_memory", "update memory", map[string]any{}), SearchTerms: []string{"send update"}, Risk: ToolRiskHigh,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
			updated = true
			return "updated", nil
		},
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"send update"}`)}},
		{ToolCalls: []model.ToolCall{call("send", "send_message", `{}`), call("update", "update_memory", `{}`)}},
	}}
	_, err := (&Runner{Model: llm, Tools: registry, StopAfterGroupAction: true}).Run(new(RunContext), "reply", "")
	require.NoError(t, err)
	require.False(t, updated)
}

func TestRunnerEnforcesToolCallBudget(t *testing.T) {
	registry := NewRegistry()
	called := false
	require.NoError(t, registry.Register(Tool{
		Definition: Function("read_context", "read context", map[string]any{}), SearchTerms: []string{"context"},
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
			called = true
			return "history", nil
		},
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"context"}`)}},
		{ToolCalls: []model.ToolCall{call("read", "read_context", `{}`)}},
		{Answer: "budget exhausted, answer with what is known"},
	}}
	answer, err := (&Runner{Model: llm, Tools: registry, MaxToolCalls: 1}).Run(new(RunContext), "reply", "")
	require.NoError(t, err)
	require.False(t, called)
	require.Contains(t, answer, "budget exhausted")
	require.Contains(t, llm.requests[2].History[len(llm.requests[2].History)-1].Content, ErrToolBudgetExceeded.Error())
}

func TestRunnerHonorsCancelledContextBeforeModelRequest(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	llm := &scriptedModel{steps: []model.Response{{Answer: "must not run"}}}
	_, err := (&Runner{Model: llm, Tools: NewRegistry()}).Run(&RunContext{Context: requestContext}, "reply", "")
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, llm.requests)
}

func TestRegistrySearchUsesChineseSemanticMatchingAndReturnsContracts(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Tool{
		Definition: Function("get_member_profile", "查询成员资料和群名片", map[string]any{}),
		Namespace:  "members", ReadOnly: true, Idempotent: true, Risk: ToolRiskLow,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) { return nil, nil },
	}))
	results := registry.Search("想看看这个成员的群名片信息", 5)
	require.NotEmpty(t, results)
	require.Equal(t, "get_member_profile", results[0].Name)
	require.Equal(t, "members", results[0].Namespace)
	require.True(t, results[0].ReadOnly)
	require.True(t, results[0].Idempotent)
	require.False(t, results[0].SideEffect)
	require.Equal(t, "low", results[0].Risk)
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

func TestRunnerDecisionBoundaryForcesNewDecisionBeforeOtherTools(t *testing.T) {
	registry := NewRegistry()
	asked := false
	require.NoError(t, registry.Register(Tool{
		Definition: Function("ask_user", "ask", map[string]any{}), SearchTerms: []string{"ask"}, DecisionBoundary: true,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
			asked = true
			return "user reply", nil
		},
	}))
	sendCount := 0
	require.NoError(t, registry.Register(Tool{
		Definition: Function("send_message", "send", map[string]any{}), SearchTerms: []string{"send"}, GroupAction: true,
		Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
			sendCount++
			return "sent", nil
		},
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"ask send"}`)}},
		// Even when send_message appears first, ask_user must be the only call
		// executed from this pre-reply model decision.
		{ToolCalls: []model.ToolCall{call("premature-send", "send_message", `{}`), call("ask", "ask_user", `{}`)}},
		{ToolCalls: []model.ToolCall{call("final-send", "send_message", `{}`)}},
		{Answer: "done"},
	}}

	answer, err := (&Runner{Model: llm, Tools: registry, MaxSteps: 4, RequireAction: true}).Run(&RunContext{}, "reply", "")
	require.NoError(t, err)
	require.Equal(t, "done", answer)
	require.True(t, asked)
	require.Equal(t, 1, sendCount)
	require.Contains(t, llm.requests[2].History[4].Content, "requires a new model decision")
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
