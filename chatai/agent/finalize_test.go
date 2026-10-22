package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

func TestRunDeadlineFinalizesWithFreshBoundedContext(t *testing.T) {
	for _, duringModel := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_request", true: "during_request"}[duringModel], func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			if !duringModel {
				<-parent.Done()
			}
			requests := 0
			llm := deliveryFeedbackModel(func(req *model.Request, res *model.Response) error {
				requests++
				if duringModel && requests == 1 {
					<-req.Context.Done()
					return req.Context.Err()
				}
				require.NoError(t, req.Context.Err())
				deadline, ok := req.Context.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 30*time.Second)
				require.Contains(t, req.Question, "本次执行超时了")
				require.Contains(t, req.History[0].Content, "original task")
				require.Empty(t, req.Tools)
				res.Answer = "partial answer"
				res.ToolCalls = []model.ToolCall{call("ignored", "search_tools", `{}`)}
				return nil
			})
			rc := &RunContext{Context: parent}
			answer, err := (&Runner{Model: llm, Tools: NewRegistry()}).Run(rc, "original task", "", "")
			require.NoError(t, err)
			require.Equal(t, "partial answer", answer)
			require.True(t, rc.Trace().Finalized)
			require.Empty(t, rc.Trace().ToolCalls)
		})
	}
}

func TestToolErrorsReachFinalDecision(t *testing.T) {
	for _, kind := range []string{"error", "panic", "encoding", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			registry := NewRegistry()
			require.NoError(t, registry.Register(Tool{
				Definition: Function("broken", "broken", map[string]any{}),
				Handler: func(*RunContext, json.RawMessage) (any, error) {
					switch kind {
					case "panic":
						panic("failure detail")
					case "encoding":
						return make(chan int), nil
					case "timeout":
						return nil, context.DeadlineExceeded
					default:
						return nil, errors.New("failure detail")
					}
				},
			}))
			llm := &scriptedModel{steps: []model.Response{
				{ToolCalls: []model.ToolCall{call("load", "search_tools", `{"query":"broken"}`)}},
				{ToolCalls: []model.ToolCall{call("fail", "broken", `{}`)}},
				{Answer: "result based on known facts"},
			}}
			answer, err := (&Runner{Model: llm, Tools: registry, MaxSteps: 2}).Run(&RunContext{Context: parent}, "task", "", "")
			require.NoError(t, err)
			require.Equal(t, "result based on known facts", answer)
			req := llm.requests[2]
			require.Empty(t, req.Tools)
			require.Contains(t, req.History[len(req.History)-1].Content, `"status":"error"`)
			require.NotContains(t, req.Question, "本次执行超时了", "tool timeout must not be confused with run timeout")
		})
	}
}

func TestModelFailuresDoNotRestartLoop(t *testing.T) {
	for _, providerMessage := range []bool{false, true} {
		calls := 0
		llm := deliveryFeedbackModel(func(req *model.Request, res *model.Response) error {
			calls++
			if providerMessage {
				res.ErrorMsg = "provider failed"
				return nil
			}
			return context.DeadlineExceeded // Model timeout, with a live run context.
		})
		_, err := (&Runner{Model: llm, Tools: NewRegistry()}).Run(&RunContext{Context: context.Background()}, "task", "", "")
		require.Error(t, err)
		require.Equal(t, 1, calls)
	}
}

func TestDeadlineDuringToolKeepsResultsAndSkipsRemainingCalls(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	registry := NewRegistry()
	calls := 0
	require.NoError(t, registry.Register(Tool{
		Definition: Function("read", "read", map[string]any{}),
		Handler: func(*RunContext, json.RawMessage) (any, error) {
			calls++
			<-parent.Done()
			return "partial evidence", parent.Err()
		},
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("load", "search_tools", `{"query":"read"}`)}},
		{ToolCalls: []model.ToolCall{call("first", "read", `{}`), call("second", "read", `{}`)}},
		{Answer: "partial conclusion"},
	}}
	answer, err := (&Runner{Model: llm, Tools: registry}).Run(&RunContext{Context: parent}, "task", "", "")
	require.NoError(t, err)
	require.Equal(t, "partial conclusion", answer)
	require.Equal(t, 1, calls)
	require.Len(t, llm.requests, 3)
	require.Contains(t, llm.requests[2].Question, "本次执行超时了")
	require.Contains(t, llm.requests[2].History[4].Content, "partial evidence")
	require.Equal(t, "second", llm.requests[2].History[5].ToolCallID)
}

func TestFinalizationModelErrorStopsWithoutRetry(t *testing.T) {
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	calls := 0
	llm := deliveryFeedbackModel(func(req *model.Request, res *model.Response) error {
		calls++
		return errors.New("closing model unavailable")
	})
	_, err := (&Runner{Model: llm, Tools: NewRegistry()}).Run(&RunContext{Context: parent}, "task", "", "")
	require.EqualError(t, err, "closing model unavailable")
	require.Equal(t, 1, calls)
}
