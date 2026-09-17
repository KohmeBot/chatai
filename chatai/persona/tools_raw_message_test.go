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
	requests  []zero.APIRequest
	response  *zero.APIResponse
	responses map[string]zero.APIResponse
}

func (c *deliveryTestCaller) CallAPI(req zero.APIRequest) (zero.APIResponse, error) {
	c.requests = append(c.requests, req)
	if response, ok := c.responses[req.Action]; ok {
		return response, nil
	}
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
	caller.responses = map[string]zero.APIResponse{
		"get_msg":         {Status: "ok", Data: gjson.Parse(`{"message_id":7,"group_id":123,"message":[{"type":"forward","data":{"id":"forward-id"}}]}`)},
		"get_forward_msg": {Status: "ok", Data: gjson.Parse(nodes)},
	}
	result, err := p.handleReadRawMessage(rc, json.RawMessage(`{"message_id":7}`))
	require.NoError(t, err)
	require.JSONEq(t, nodes, string(result.(map[string]any)["forwards"].(map[string]json.RawMessage)["forward-id"]))
	require.Equal(t, int64(7), caller.requests[0].Params["message_id"])
	require.Equal(t, "forward-id", caller.requests[1].Params["id"])
	caller.responses["get_forward_msg"] = zero.APIResponse{Status: "failed", RetCode: 100}
	result, err = p.handleReadRawMessage(rc, json.RawMessage(`{"message_id":7}`))
	require.NoError(t, err)
	require.NotEmpty(t, result.(map[string]any)["forward_errors"])
	caller.responses["get_msg"] = zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"group_id":999,"message":[]}`)}
	_, err = p.handleReadRawMessage(rc, json.RawMessage(`{"message_id":7}`))
	require.Error(t, err)
	_, err = p.handleReadRawMessage(rc, json.RawMessage(`{"message_id":0}`))
	require.Error(t, err)
	caller.responses = nil
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
	prompt := p.eventPrompt(stored, "", 1)
	require.NotContains(t, prompt, "raw_segments")
	require.NotContains(t, prompt, "raw_message")
	require.NotContains(t, prompt, "quoted_raw_segments")
	require.NotEmpty(t, formatMessage(msg))
}

func TestTextToolsRejectEscapedNewlinesBeforeDelivery(t *testing.T) {
	p := &Persona{groupID: 123}
	for name, tc := range map[string]struct {
		handler agent.Handler
		args    string
	}{
		"text":     {p.handleSendMessage, `{"text":"first\\nsecond"}`},
		"image":    {p.handleSendImage, `{"url":"https://example.com/a.png","text":"first\\r\\nsecond"}`},
		"at":       {p.handleAtUser, `{"user_id":1,"text":"first\\nsecond"}`},
		"batch":    {p.handleSendMessages, `{"messages":["valid","first\\nsecond"]}`},
		"followup": {p.handleAskUserAndWait, `{"user_id":1,"question":"first\\nsecond"}`},
	} {
		t.Run(name, func(t *testing.T) {
			caller := &deliveryTestCaller{}
			zero.APICallers.Store(987654323, caller)
			t.Cleanup(func() { zero.APICallers.Delete(987654323) })
			rc := &agent.RunContext{Values: map[string]any{"zero_ctx": zero.GetBot(987654323)}}
			_, err := tc.handler(rc, json.RawMessage(tc.args))
			require.ErrorContains(t, err, "encode only once")
			require.Empty(t, caller.requests)
			require.False(t, rc.ActionPerformed())
		})
	}
	var input struct {
		Text string `json:"text"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"text":"first\nsecond\r\nthird"}`), &input))
	require.NoError(t, validateMessageText(input.Text))
	require.Equal(t, "first\nsecond\r\nthird", input.Text)
	literal := message.Message{message.Text(`C:\new\report.txt and \n`)}
	payload, _, err := rawMessagePayload(literal)
	require.NoError(t, err)
	require.Equal(t, literal, payload)
}

func TestReadRawMessagePreservesMetadataAndDeduplicatesNestedForwards(t *testing.T) {
	const segments = `[{"type":"custom","data":{"number":9007199254740993,"nested":[{"value":"original"}]}},{"type":"forward","data":{"id":"nested"}},{"type":"forward","data":{"id":"nested"}}]`
	const nodes = `{"messages":[{"sender":{"user_id":42,"nickname":"sender"},"time":123,"content":[{"type":"forward","data":{"id":"nested"}}]}],"vendor":{"x":[1,2]}}`
	caller := &deliveryTestCaller{responses: map[string]zero.APIResponse{
		"get_msg":         {Status: "ok", Data: gjson.Parse(`{"group_id":123,"message_id":-7,"message":` + segments + `}`)},
		"get_forward_msg": {Status: "ok", Data: gjson.Parse(nodes)},
	}}
	zero.APICallers.Store(987654324, caller)
	t.Cleanup(func() { zero.APICallers.Delete(987654324) })
	p := &Persona{groupID: 123}
	result, err := p.handleReadRawMessage(&agent.RunContext{Values: map[string]any{"zero_ctx": zero.GetBot(987654324)}}, json.RawMessage(`{"message_id":-7}`))
	require.NoError(t, err)
	data := result.(map[string]any)
	require.Equal(t, segments, string(data["raw_segments"].(json.RawMessage)))
	require.Equal(t, nodes, string(data["forwards"].(map[string]json.RawMessage)["nested"]))
	require.Empty(t, data["forward_errors"])
	require.Len(t, caller.requests, 2, "cycles and duplicate IDs must not trigger more API calls")
	reply := followUpReplyResult(1, GroupMessage{MsgID: 2, QuotedMsgID: 3, RawSegments: segments})["reply"].(map[string]any)
	require.NotContains(t, reply, "raw_segments")
	require.Equal(t, int64(3), reply["quoted_message_id"])
}
