package agent

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

func TestRunnerFinishAfterSuccess(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		args                                            []string
		failFirst, boundary, groupAction, requireAction bool
		requests                                        int
	}{
		{name: "success", args: []string{`{"_finish_after_success":true}`}, requests: 2},
		{name: "group delivery", args: []string{`{"_finish_after_success":true}`}, groupAction: true, requireAction: true, requests: 2},
		{name: "multiple tools", args: []string{`{}`, `{"_finish_after_success":true}`}, requests: 2},
		{name: "failure", args: []string{`{"_finish_after_success":true}`}, failFirst: true, requests: 3},
		{name: "earlier failure", args: []string{`{}`, `{"_finish_after_success":true}`}, failFirst: true, requests: 3},
		{name: "not last", args: []string{`{"_finish_after_success":true}`, `{}`}, requests: 3},
		{name: "boundary", args: []string{`{"_finish_after_success":true}`}, boundary: true, requests: 3},
		{name: "no flag", args: []string{`{}`}, requests: 3},
		{name: "false flag", args: []string{`{"_finish_after_success":false}`}, requests: 3},
		{name: "invalid flag", args: []string{`{"_finish_after_success":"true"}`}, requests: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewRegistry()
			invoked := 0
			require.NoError(t, registry.Register(Tool{Definition: Function("task", "task", nil), GroupAction: tc.groupAction, DecisionBoundary: tc.boundary,
				Handler: func(_ *RunContext, args json.RawMessage) (any, error) {
					invoked++
					require.NotContains(t, string(args), finishParameter)
					if tc.failFirst && invoked == 1 {
						return nil, errors.New("failed")
					}
					return "ok", nil
				}}))
			calls := []model.ToolCall{}
			for i, args := range tc.args {
				calls = append(calls, call(string(rune('a'+i)), "task", args))
			}
			llm := &scriptedModel{steps: []model.Response{
				{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"task"}`)}},
				{Answer: "执行任务", ToolCalls: calls}, {Answer: "done"},
			}}
			ctx := &RunContext{}
			_, err := (&Runner{Model: llm, Tools: registry, RequireAction: tc.requireAction}).Run(ctx, "task", "", "")
			require.NoError(t, err)
			require.Len(t, llm.requests, tc.requests)
			require.Equal(t, tc.requests, ctx.Trace().DecisionSteps)
			require.False(t, ctx.Trace().Finalized)
			if tc.requests == 2 {
				require.Empty(t, ctx.Trace().FinalAnswer)
			}
			if tc.failFirst {
				require.Contains(t, llm.requests[2].History[4].Content, "failed")
			}
		})
	}
}

func TestFinishCannotBypassRequiredAction(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"task", "send"} {
		require.NoError(t, registry.Register(Tool{Definition: Function(name, "task", nil), GroupAction: name == "send", Handler: func(*RunContext, json.RawMessage) (any, error) { return "ok", nil }}))
	}
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("s", "search_tools", `{"query":"task"}`)}},
		{ToolCalls: []model.ToolCall{call("t", "task", `{"_finish_after_success":true}`)}},
		{ToolCalls: []model.ToolCall{call("d", "send", `{"_finish_after_success":true}`)}},
	}}
	ctx := &RunContext{}
	_, err := (&Runner{Model: llm, Tools: registry, RequireAction: true, MaxSteps: 3}).Run(ctx, "task", "", "")
	require.NoError(t, err)
	require.Len(t, llm.requests, 3)
	require.True(t, ctx.ActionPerformed())
	require.False(t, ctx.Trace().Finalized)
}

func TestFinishSchemaDoesNotMutateRegistry(t *testing.T) {
	definition := Function("task", "task", nil)
	updated := withFinishParameter([]model.Tool{definition})
	require.Contains(t, updated[0].Function.Parameters["properties"], finishParameter)
	require.NotContains(t, definition.Function.Parameters["properties"], finishParameter)
}
