package agent

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

func TestDeliveryFailureReturnsToModelEvenOnFinalStep(t *testing.T) {
	for _, failures := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(failures), func(t *testing.T) {
			registry := NewRegistry()
			attempts := 0
			require.NoError(t, registry.Register(Tool{
				Definition: Function("send_raw_message", "send", map[string]any{}), GroupAction: true,
				Handler: func(rc *RunContext, raw json.RawMessage) (any, error) {
					attempts++
					require.JSONEq(t, fmt.Sprintf(`{"revision":%d}`, attempts), string(raw))
					if attempts <= failures {
						return nil, ErrMessageDeliveryFailed
					}
					return map[string]any{"message_id": -42}, nil
				},
			}))
			llm := &scriptedModel{}
			for i := 1; i <= 3; i++ {
				llm.steps = append(llm.steps, model.Response{ToolCalls: []model.ToolCall{call(fmt.Sprint(i), "send_raw_message", fmt.Sprintf(`{"revision":%d}`, i))}})
			}
			rc := &RunContext{}
			_, err := (&Runner{Model: llm, Tools: registry, RequireAction: true, MaxSteps: 1, MaxToolCalls: 1}).Run(rc, "send", "", "")
			if failures == 3 {
				require.ErrorIs(t, err, ErrMessageDeliveryFailed)
				require.False(t, rc.ActionPerformed())
				require.False(t, rc.ResponseDelivered())
				require.Equal(t, 3, attempts)
			} else {
				require.NoError(t, err)
				require.True(t, rc.ResponseDelivered())
				require.Equal(t, failures+1, attempts)
			}
			feedback := llm.requests[1].History[2].Content.(string)
			require.Contains(t, feedback, `"retryable":true`)
			require.Contains(t, feedback, "message_id=0")
		})
	}
}

func TestPartialDeliveryFailureSkipsStaleCallsAndAllowsOnlyNewDecision(t *testing.T) {
	registry := NewRegistry()
	var sent []string
	require.NoError(t, registry.Register(Tool{
		Definition: Function("send_messages", "send", map[string]any{}), GroupAction: true,
		Handler: func(rc *RunContext, raw json.RawMessage) (any, error) {
			sent = append(sent, string(raw))
			if len(sent) == 1 {
				rc.MarkActionPerformed()
				rc.MarkResponseDelivered("tool:send_messages")
				return map[string]any{"message_ids": []int64{7}, "unsent_messages": []string{"second"}}, ErrMessageDeliveryFailed
			}
			return "sent remaining", nil
		},
	}))
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("a", "send_messages", `{"messages":["first","second"]}`), call("stale", "send_messages", `{}`)}},
		{ToolCalls: []model.ToolCall{call("b", "send_messages", `{"messages":["second"]}`)}},
	}}
	rc := &RunContext{}
	_, err := (&Runner{Model: llm, Tools: registry, RequireAction: true, StopAfterGroupAction: true, MaxSteps: 1, MaxToolCalls: 1}).Run(rc, "send", "", "")
	require.NoError(t, err)
	require.Len(t, sent, 2)
	require.Contains(t, llm.requests[1].History[2].Content, "unsent_messages")
	require.Contains(t, llm.requests[1].History[3].Content, "skipped")
	require.True(t, rc.ActionPerformed())
}

func TestHostFailureCountsTowardDeliveryLimit(t *testing.T) {
	registry := NewRegistry()
	attempts := 0
	require.NoError(t, registry.Register(Tool{
		Definition: Function("send_message", "send", map[string]any{}), GroupAction: true,
		Handler: func(*RunContext, json.RawMessage) (any, error) { attempts++; return nil, ErrMessageDeliveryFailed },
	}))
	rc := &RunContext{}
	rc.ReportDeliveryFailure(ErrMessageDeliveryFailed)
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("load", "search_tools", `{"query":"send"}`)}},
		{ToolCalls: []model.ToolCall{call("a", "send_message", `{}`)}},
		{ToolCalls: []model.ToolCall{call("b", "send_message", `{}`)}},
	}}
	_, err := (&Runner{Model: llm, Tools: registry, RequireAction: true}).Run(rc, "original event", "", "")
	require.ErrorIs(t, err, ErrMessageDeliveryFailed)
	require.Equal(t, 2, attempts)
	require.Contains(t, llm.requests[0].Question, "original event")
	require.Contains(t, llm.requests[0].Question, "message_id=0")
}

type deliveryFeedbackModel func(*model.Request, *model.Response) error

func (f deliveryFeedbackModel) Request(req *model.Request, res *model.Response) error {
	return f(req, res)
}

func TestProgressFailureDuringModelRequestForcesNewDecision(t *testing.T) {
	rc := &RunContext{}
	registry := NewRegistry()
	sends, decisions := 0, 0
	require.NoError(t, registry.Register(Tool{
		Definition: Function("send_message", "send", map[string]any{}), GroupAction: true,
		Handler: func(*RunContext, json.RawMessage) (any, error) { sends++; return "sent", nil },
	}))
	llm := deliveryFeedbackModel(func(req *model.Request, res *model.Response) error {
		decisions++
		if decisions == 1 {
			rc.ReportDeliveryFailure(ErrMessageDeliveryFailed)
		} else {
			require.Contains(t, req.Question, "message_id=0")
			require.Len(t, req.History, 3)
			require.Contains(t, req.History[2].Content, "skipped")
		}
		res.ToolCalls = []model.ToolCall{call(fmt.Sprint(decisions), "send_message", `{}`)}
		return nil
	})
	_, err := (&Runner{Model: llm, Tools: registry, RequireAction: true, MaxSteps: 1}).Run(rc, "send", "", "")
	require.NoError(t, err)
	require.Equal(t, 2, decisions)
	require.Equal(t, 1, sends)
}
