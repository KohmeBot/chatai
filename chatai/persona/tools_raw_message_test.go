package persona

import (
	"encoding/json"
	"testing"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type deliveryTestCaller struct {
	requests []zero.APIRequest
	response *zero.APIResponse
}

func (c *deliveryTestCaller) CallAPI(req zero.APIRequest) (zero.APIResponse, error) {
	c.requests = append(c.requests, req)
	if c.response != nil {
		return *c.response, nil
	}
	return zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":0}`)}, nil
}

func TestReadForwardAndQuotedMessagePreserveNestedArrays(t *testing.T) {
	caller := &deliveryTestCaller{}
	zero.APICallers.Store(987654322, caller)
	t.Cleanup(func() { zero.APICallers.Delete(987654322) })
	ctx := zero.GetBot(987654322)
	rc := &agent.RunContext{Values: map[string]any{"zero_ctx": ctx}}
	p := &Persona{groupID: 123}
	const nodes = `{"messages":[{"sender":{"user_id":1,"nickname":"原作者"},"content":[{"type":"face","data":{"id":"1"}}]}]}`
	caller.response = &zero.APIResponse{Status: "ok", Data: gjson.Parse(nodes)}
	result, err := p.handleReadForwardMessage(rc, json.RawMessage(`{"id":"forward-id"}`))
	require.NoError(t, err)
	require.JSONEq(t, nodes, string(result.(json.RawMessage)))
	require.Equal(t, "forward-id", caller.requests[0].Params["id"])
	caller.response = &zero.APIResponse{Status: "failed", RetCode: 100}
	_, err = p.handleReadForwardMessage(rc, json.RawMessage(`{"id":"forward-id"}`))
	require.Error(t, err)
	const raw = `[{"type":"custom","data":{"number":123,"nested":[{"value":"original"}]}}]`
	caller.response = &zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":7,"sender":{"user_id":1},"message":` + raw + `}`)}
	quoted, preserved := getQuotedMessage(ctx, message.Message{message.Reply(7)})
	require.JSONEq(t, raw, preserved)
	require.Equal(t, int64(1), quoted.Sender.ID)
}

func TestEverySendToolRejectsZeroIDBeforeRecordingOrMarkingDelivery(t *testing.T) {
	p := &Persona{groupID: 123}
	for name, tc := range map[string]struct {
		handler agent.Handler
		args    string
	}{
		"text":     {p.handleSendMessage, `{"text":"hello"}`},
		"image":    {p.handleSendImage, `{"url":"https://example.com/a.png"}`},
		"at":       {p.handleAtUser, `{"user_id":1,"text":"hello"}`},
		"batch":    {p.handleSendMessages, `{"messages":["first","second"]}`},
		"raw":      {p.handleSendRawMessage, `{"message":[{"type":"face","data":{"id":"1"}}]}`},
		"forward":  {p.handleSendRawMessage, `{"message":[{"type":"node","data":{"id":"1"}}]}`},
		"followup": {p.handleAskUserAndWait, `{"user_id":1,"question":"which?"}`},
	} {
		t.Run(name, func(t *testing.T) {
			caller := &deliveryTestCaller{}
			zero.APICallers.Store(987654321, caller)
			t.Cleanup(func() { zero.APICallers.Delete(987654321) })
			ctx := zero.GetBot(987654321)
			ctx.Event = &zero.Event{SelfID: 987654321, GroupID: 123}
			rc := &agent.RunContext{Values: map[string]any{"zero_ctx": ctx}}
			_, err := tc.handler(rc, json.RawMessage(tc.args))
			require.ErrorIs(t, err, agent.ErrMessageDeliveryFailed)
			require.False(t, rc.ActionPerformed())
			require.False(t, rc.ResponseDelivered())
			require.False(t, rc.WaitingForUser())
			require.Empty(t, p.followUps)
			require.Len(t, caller.requests, 1)
			if name == "forward" {
				require.Equal(t, "send_group_forward_msg", caller.requests[0].Action)
			}
		})
	}
}

func TestRawMessagePayloadPreservesStringsAndExpandsForwardContent(t *testing.T) {
	segments := message.Message{message.Text("[CQ:face,id=1] & <hello>"), {Type: "custom", Data: map[string]string{"value": "a,b&c"}}}
	payload, forward, err := rawMessagePayload(segments)
	require.NoError(t, err)
	require.False(t, forward)
	require.Equal(t, segments, payload)
	nodes := message.Message{{Type: "node", Data: map[string]string{"name": "匿名", "uin": "10000", "content": `[{"type":"text","data":{"text":"原文"}}]`}}}
	payload, forward, err = rawMessagePayload(nodes)
	require.NoError(t, err)
	require.True(t, forward)
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	require.JSONEq(t, `[{"type":"node","data":{"name":"匿名","uin":"10000","content":[{"type":"text","data":{"text":"原文"}}]}}]`, string(encoded))
	_, _, err = rawMessagePayload(append(nodes, message.Text("bad mix")))
	require.Error(t, err)
	_, _, err = rawMessagePayload(nil)
	require.Error(t, err)
	require.NoError(t, checkMessageID(-1), "negative nonzero IDs are valid")
}

func TestRawMessageSurvivesPersistenceAndModelEnvelope(t *testing.T) {
	raw := `[{"type":"node","data":{"content":[{"type":"text","data":{"text":"<&>"}}]}}]`
	msg := GroupMessage{RawMessage: "[CQ:forward,id=abc]", RawSegments: originalSegments(json.RawMessage(raw), nil), QuotedRawSegments: raw, MsgType: "custom"}
	stored := messageRecord(123, msg).message()
	require.Equal(t, msg, stored)
	p := &Persona{groupID: 123}
	var envelope agentEventEnvelope
	require.NoError(t, json.Unmarshal([]byte(p.eventPrompt(stored, "", 1)), &envelope))
	require.Equal(t, msg.RawMessage, envelope.Message.RawMessage)
	require.JSONEq(t, raw, string(envelope.Message.RawSegments))
	require.JSONEq(t, raw, string(envelope.Message.QuotedRawSegments))
	require.NotEmpty(t, formatMessage(msg))
}
