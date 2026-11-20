package agent

import (
	"encoding/json"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

func TestRegistryScopeNormalizesAndFiltersToolMetadata(t *testing.T) {
	registry := NewRegistry()
	for name, scope := range map[string]ToolScope{"group_history": ToolScopeGroup, "private_history": ToolScopePrivate, "shared_history": ""} {
		require.NoError(t, registry.Register(Tool{Definition: Function(name, "history", nil), Scope: scope,
			Handler: func(*RunContext, json.RawMessage) (any, error) { return nil, nil }}))
	}
	require.ErrorContains(t, registry.Register(Tool{Definition: Function("unknown_scope", "history", nil), Scope: "somewhere",
		Handler: func(*RunContext, json.RawMessage) (any, error) { return nil, nil }}), "invalid scope")
	require.Equal(t, []string{"group_history", "private_history", "shared_history"}, registry.Names())
	require.Equal(t, []string{"group_history", "shared_history"}, registry.NamesInScope(ToolScopeGroup))
	require.Equal(t, []string{"private_history", "shared_history"}, registry.NamesInScope(ToolScopePrivate))
	require.Equal(t, []string{"shared_history"}, registry.NamesInScope(ToolScopeBoth))
	require.Empty(t, registry.SearchInScope("history", 5, "invalid"))
	require.False(t, registry.HasInScope("private_history", ToolScopeGroup))
	require.True(t, registry.HasInScope("private_history", ToolScopePrivate))
	catalog := registry.Catalog()
	require.Equal(t, ToolScopeBoth, catalog[0]["scope"])
	require.Equal(t, ToolScopeGroup, catalog[1]["scope"])
	require.Equal(t, ToolScopePrivate, catalog[2]["scope"])
	require.Equal(t, ToolScopeBoth, catalog[3]["scope"])
	require.NoError(t, registry.SetDisabled("private_history", true, nil))
	require.Equal(t, []string{"shared_history"}, registry.NamesInScope(ToolScopePrivate))
}

func TestRunnerScopeFiltersSearchAndFinalDelivery(t *testing.T) {
	for _, scope := range []ToolScope{ToolScopeGroup, ToolScopePrivate} {
		t.Run(string(scope), func(t *testing.T) {
			other := ToolScopePrivate
			if scope == ToolScopePrivate {
				other = ToolScopeGroup
			}
			history := string(scope) + "_history"
			otherHistory := string(other) + "_history"
			delivery := string(scope) + "_delivery"
			otherDelivery := string(other) + "_delivery"
			registry := NewRegistry()
			var calls []string
			for name, scope := range map[string]ToolScope{history: scope, otherHistory: other, "shared_history": ToolScopeBoth, delivery: scope, otherDelivery: other} {
				require.NoError(t, registry.Register(Tool{Definition: Function(name, name, nil), Scope: scope,
					GroupAction: name == delivery || name == otherDelivery, DecisionBoundary: name == otherHistory,
					Handler: func(*RunContext, json.RawMessage) (any, error) {
						calls = append(calls, name)
						return true, nil
					}}))
			}
			llm := &scriptedModel{steps: []model.Response{
				{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"history","limit":2}`)}},
				{ToolCalls: []model.ToolCall{call("guess", otherHistory, `{}`), call("read", history, `{}`)}},
				{ToolCalls: []model.ToolCall{call("send", delivery, `{"_finish_after_success":true}`)}},
			}}
			ctx := &RunContext{Private: scope == ToolScopePrivate, GroupID: 123}
			_, err := (&Runner{Model: llm, Tools: registry, RequireAction: true, MaxSteps: 3}).Run(ctx, "current", "", "",
				Tool{Definition: Function(otherHistory, "overlapping declaration", nil), Scope: ToolScopeBoth},
				Tool{Definition: Function("other_default", "incompatible default", nil), Scope: other})
			require.NoError(t, err)
			require.Equal(t, []string{"search_tools"}, toolNames(llm.requests[0].Tools))
			require.ElementsMatch(t, []string{"search_tools", history, "shared_history"}, toolNames(llm.requests[1].Tools))
			require.Equal(t, []string{delivery}, toolNames(llm.requests[2].Tools))
			require.Equal(t, []string{history, delivery}, calls)
			var payload struct{ Result []SearchResult }
			require.NoError(t, json.Unmarshal([]byte(llm.requests[1].History[2].Content.(string)), &payload))
			require.Len(t, payload.Result, 2)
			require.Equal(t, scope, payload.Result[0].Scope)
			require.Equal(t, ToolScopeBoth, payload.Result[1].Scope)
			_, err = registry.execute(ctx, call("direct", otherDelivery, `{}`))
			require.ErrorContains(t, err, "unavailable in "+string(scope)+" conversations")
			require.Equal(t, []string{history, delivery}, calls)
		})
	}
}

type scopedSkillSearcher struct{ residentSearcher }

func (s scopedSkillSearcher) ListSkillSummaries() ([]SkillSummary, error) { return nil, nil }

func TestPrivateSkillCannotExposeGroupToolBypass(t *testing.T) {
	registry := NewRegistry()
	for name, scope := range map[string]ToolScope{"group_tool": ToolScopeGroup, "shared_tool": ToolScopeBoth, "private_tool": ToolScopePrivate} {
		require.NoError(t, registry.Register(Tool{Definition: Function(name, name, nil), Scope: scope,
			Handler: func(*RunContext, json.RawMessage) (any, error) {
				t.Fatal("loading may not execute tools")
				return nil, nil
			}}))
	}
	require.NoError(t, registry.Register(Tool{Definition: Function("read_skill", "read_skill", nil),
		Handler: func(*RunContext, json.RawMessage) (any, error) {
			return map[string]any{"content": "---\ntools: [group_tool, private_tool]\n---"}, nil
		}}))
	searcher := &scopedSkillSearcher{residentSearcher{
		resident: []SkillMatch{
			{Name: "resident", Markdown: "---\ntools: [group_tool, shared_tool]\n---\nRESIDENT_MARKER"},
			{ID: 1, Name: "group_resident", Markdown: "UNAVAILABLE_RESIDENT_MARKER", RequiredTools: []string{"group_tool"}},
		},
		scriptedSkillSearcher: scriptedSkillSearcher{matches: []SkillMatch{
			{ID: 2, Name: "group_request", Markdown: "GROUP_REQUEST_MARKER", RequiredTools: []string{"group_tool"}},
			{ID: 3, Name: "private_request", Markdown: "PRIVATE_REQUEST_MARKER", RequiredTools: []string{"private_tool"}},
		}},
	}}
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("read", "read_skill", `{}`)}},
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"request"}`)}},
		{Answer: "done"},
	}}
	ctx := &RunContext{Private: true}
	_, err := (&Runner{Model: llm, Tools: registry, Skills: searcher}).Run(ctx, "task", "", "")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"search_tools", "shared_tool", "read_skill"}, toolNames(llm.requests[0].Tools))
	require.Contains(t, llm.requests[0].Question, "RESIDENT_MARKER")
	require.NotContains(t, llm.requests[0].Question, "UNAVAILABLE_RESIDENT_MARKER")
	for _, req := range llm.requests[1:] {
		require.Contains(t, toolNames(req.Tools), "private_tool")
		require.NotContains(t, toolNames(req.Tools), "group_tool")
	}
	readHistory, searchHistory := llm.requests[1].History, llm.requests[2].History
	require.Contains(t, readHistory[len(readHistory)-1].Content, `"loaded_tools":["private_tool"]`)
	require.Contains(t, searchHistory[len(searchHistory)-1].Content, "PRIVATE_REQUEST_MARKER")
	require.NotContains(t, searchHistory[len(searchHistory)-1].Content, "GROUP_REQUEST_MARKER")
	require.Equal(t, []uint{3}, ctx.Trace().UsedSkillIDs)
}
