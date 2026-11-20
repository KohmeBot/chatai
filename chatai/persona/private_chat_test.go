package persona

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type privateReplyTestModel struct {
	events []agentEventEnvelope
}

func (m *privateReplyTestModel) Request(req *model.Request, res *model.Response) error {
	var event agentEventEnvelope
	prompt, _, _ := strings.Cut(req.Question, "\n\n")
	if err := json.Unmarshal([]byte(prompt), &event); err != nil {
		return err
	}
	m.events = append(m.events, event)
	res.ToolCalls = []model.ToolCall{{
		ID: "reply", Type: "function",
		Function: model.ToolCallFunction{Name: "send_message", Arguments: `{"text":"收到","_finish_after_success":true}`},
	}}
	return nil
}

func TestPrivateConversationReadsProfileWithoutUpdatingIt(t *testing.T) {
	const botID, userID = 987654335, 123
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&PrivateChatMessageRecord{}))
	require.NoError(t, writeUserMemory(db, UserProfile{
		UserID: userID, Summary: "使用简短回答",
		Items: []MemoryItem{
			{Key: "style", Kind: "preference", Text: "喜欢清单", GroupID: 777},
			{Key: "event", Kind: "experience", Text: "来源群事件", GroupID: 777},
		},
	}))
	before, err := readUserMemory(db, userID)
	require.NoError(t, err)

	caller := &deliveryTestCaller{response: &zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":42}`)}}
	zero.APICallers.Store(botID, caller)
	t.Cleanup(func() { zero.APICallers.Delete(botID) })
	ctx := zero.GetBot(botID)
	llm := &privateReplyTestModel{}
	p := NewPrivatePersona(PrivateSession{BotID: botID, UserID: userID}, nil, db, Options{
		AgentModel: llm, MaxSteps: 1, ProgressAfter: time.Hour,
		ImpressionUpdateEnable: true, ConversationMemory: true,
		MemoryMaintainer: NewMemoryMaintainer(db, llm, time.Minute, 10),
	})
	tools := p.ToolNames()
	require.Contains(t, tools, "read_user_memory")
	for _, name := range []string{"update_user_memory", "update_impression", "update_user_favor", "read_group_context"} {
		require.NotContains(t, tools, name)
	}
	profileTool := p.privateProfileTools()[0]
	require.True(t, profileTool.ReadOnly)
	lookup, err := profileTool.Handler(nil, json.RawMessage(`{"user_id":999}`))
	require.NoError(t, err)
	require.EqualValues(t, userID, lookup.(UserProfile).UserID)

	// Simulate a temporary private event whose payload includes a group ID.
	ctx.Event = &zero.Event{PostType: "message", DetailType: "private", SelfID: botID, UserID: userID, GroupID: 777, MessageID: int64(11),
		Message: message.Message{message.Text("请把我的画像改成偏好长文")}}
	require.NoError(t, p.UpdateContext(ctx))

	// The same numeric ID in a different private session may not be quoted or
	// appear in this user's context.
	for _, session := range []PrivateSession{{BotID: botID, UserID: userID + 1}, {BotID: botID + 1, UserID: userID}} {
		other := NewPrivatePersona(session, nil, db, Options{})
		require.NoError(t, other.saveMessage(GroupMessage{MsgID: 999, Content: "其他会话的内容"}))
	}
	ctx.Event.MessageID = int64(12)
	ctx.Event.Message = message.Message{message.Reply(999), message.Text("接着上一条")}
	require.NoError(t, p.UpdateContext(ctx))

	require.Len(t, caller.requests, 2)
	for _, req := range caller.requests {
		require.Equal(t, "send_private_msg", req.Action)
		require.EqualValues(t, userID, req.Params["user_id"])
	}
	require.Len(t, llm.events, 2)
	first, second := llm.events[0], llm.events[1]
	require.Equal(t, "private_message", first.Kind)
	require.Nil(t, first.GroupMemory)
	require.Equal(t, before.Summary, first.UserMemory.Summary)
	require.Len(t, first.UserMemory.Items, 1)
	require.Equal(t, "style", first.UserMemory.Items[0].Key)
	require.Equal(t, first.UserMemory, second.UserMemory)
	require.Len(t, second.RecentMessages, 2)
	require.Equal(t, first.Message.Content, second.RecentMessages[0].Content)
	require.Equal(t, "收到", second.RecentMessages[1].Content)
	require.Empty(t, second.Message.QuotedContent)

	result, err := p.handleReadPrivateContext(nil, json.RawMessage(`{"limit":2,"offset":2}`))
	require.NoError(t, err)
	history := result.(contextQueryResult)
	require.Len(t, history.Messages, 2)
	require.Equal(t, first.Message.Content, history.Messages[0].Content)
	require.Equal(t, "收到", history.Messages[1].Content)

	after, err := readUserMemory(db, userID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	for _, table := range []any{&ChatMessageRecord{}, &PendingMemoryMessage{}, &MemoryAudit{}} {
		var count int64
		require.NoError(t, db.Model(table).Count(&count).Error)
		require.Zero(t, count)
	}
}
