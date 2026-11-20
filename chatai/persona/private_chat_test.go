package persona

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/skill"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type privateReplyTestModel struct {
	events   []agentEventEnvelope
	requests []model.Request
}

func (m *privateReplyTestModel) Request(req *model.Request, res *model.Response) error {
	var event agentEventEnvelope
	if err := json.NewDecoder(strings.NewReader(req.Question)).Decode(&event); err != nil {
		return err
	}
	m.events = append(m.events, event)
	m.requests = append(m.requests, *req)
	res.ToolCalls = []model.ToolCall{{
		ID: "reply", Type: "function",
		Function: model.ToolCallFunction{Name: "send_message", Arguments: `{"text":"收到","_finish_after_success":true}`},
	}}
	return nil
}

func TestPrivateAgentPreloadsSharedResidentSkill(t *testing.T) {
	const botID, userID = 987654338, 123
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&PrivateChatMessageRecord{}, &PrivateUserMemory{}, &agent.ToolPreference{}))
	require.NoError(t, db.AutoMigrate(skill.Models()...))
	prefs, err := agent.LoadToolPreferences(db)
	require.NoError(t, err)
	s := skill.NewService(db, nil, skill.Options{Enabled: true})
	s.Directory, err = skill.NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.Directory.Save("demo", "SKILL.md", "---\nname: demo\ndescription: Shared directory skill\ntools: [send_message, shared_tool, group_only_tool]\n---\nRESIDENT_PRIVATE_BODY", false))
	require.NoError(t, s.SetResident(12345, "demo", 0, true))
	llm := new(privateReplyTestModel)
	p := NewPrivatePersona(PrivateSession{BotID: botID, UserID: userID}, nil, db, Options{
		Skills: s, AgentModel: llm, ToolPreferences: prefs, ProgressAfter: time.Hour,
		ExtraTools: []agent.Tool{
			{Definition: agent.Function("shared_tool", "Shared tool", nil),
				Handler: func(*agent.RunContext, json.RawMessage) (any, error) { return true, nil }},
			{Definition: agent.Function("group_only_tool", "Group-only tool", nil), Scope: agent.ToolScopeGroup,
				Handler: func(*agent.RunContext, json.RawMessage) (any, error) { return true, nil }},
		},
	})
	caller := &deliveryTestCaller{response: &zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":42}`)}}
	zero.APICallers.Store(botID, caller)
	t.Cleanup(func() { zero.APICallers.Delete(botID) })
	ctx := zero.GetBot(botID)
	ctx.Event = &zero.Event{PostType: "message", DetailType: "private", SelfID: botID, UserID: userID,
		MessageID: int64(11), Message: message.Message{message.Text("demo")}}
	require.NoError(t, p.UpdateContext(ctx))
	require.Len(t, llm.requests, 1)
	require.Contains(t, llm.requests[0].Question, "RESIDENT_PRIVATE_BODY")
	var exposed []string
	for _, tool := range llm.requests[0].Tools {
		exposed = append(exposed, tool.Function.Name)
	}
	require.Subset(t, exposed, []string{"search_tools", "read_skill", "list_skills", "search_skills", "shared_tool"})
	require.NotContains(t, exposed, "group_only_tool")
	require.Equal(t, "send_private_msg", caller.requests[0].Action)
}

func TestPrivateConversationInjectsBothProfiles(t *testing.T) {
	const botID, userID = 987654335, 123
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&PrivateChatMessageRecord{}, &PrivateUserMemory{}))
	session := PrivateSession{BotID: botID, UserID: userID}
	require.NoError(t, writeUserMemory(db, UserProfile{
		UserID: userID, Summary: "使用简短回答",
		Items: []MemoryItem{
			{Key: "style", Kind: "preference", Text: "喜欢清单", GroupID: 777},
			{Key: "event", Kind: "experience", Text: "来源群事件", GroupID: 777},
		},
	}))
	before, err := readUserMemory(db, userID)
	require.NoError(t, err)
	require.NoError(t, saveUserMemory(db, UserProfile{
		UserID: userID, Summary: "私聊画像", Items: []MemoryItem{{Key: "event", Kind: "experience", Text: "私聊共同经历"}},
	}, &session))

	caller := &deliveryTestCaller{response: &zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":42}`)}}
	zero.APICallers.Store(botID, caller)
	t.Cleanup(func() { zero.APICallers.Delete(botID) })
	ctx := zero.GetBot(botID)
	llm := &privateReplyTestModel{}
	var events []agent.Event
	p := NewPrivatePersona(session, nil, db, Options{
		AgentModel: llm, MaxSteps: 1, ProgressAfter: time.Hour,
		ImpressionUpdateEnable: true, ConversationMemory: true,
		MemoryMaintainer: NewMemoryMaintainer(db, llm, time.Minute, 10),
		Observer:         func(event agent.Event) { events = append(events, event) },
	})
	tools := p.ToolNames()
	require.Contains(t, tools, "read_user_memory")
	require.Contains(t, tools, "update_user_memory")
	for _, name := range []string{"update_impression", "update_user_favor", "read_group_context"} {
		require.NotContains(t, tools, name)
	}
	profileTool := p.privateProfileTools()[0]
	require.True(t, profileTool.ReadOnly)
	lookup, err := profileTool.Handler(nil, json.RawMessage(`{"user_id":999}`))
	require.NoError(t, err)
	profiles := lookup.(privateUserMemoryResult)
	require.EqualValues(t, userID, profiles.UserMemory.UserID)
	require.EqualValues(t, userID, profiles.PrivateUserMemory.UserID)

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
	require.Equal(t, "私聊画像", first.PrivateUserMemory.Summary)
	require.Len(t, first.PrivateUserMemory.Items, 1)
	require.Equal(t, "私聊共同经历", first.PrivateUserMemory.Items[0].Text)
	require.Equal(t, first.PrivateUserMemory, second.PrivateUserMemory)
	var inputs []string
	for _, event := range events {
		require.True(t, event.Private)
		require.Zero(t, event.GroupID)
		require.EqualValues(t, botID, event.BotID)
		require.EqualValues(t, userID, event.UserID)
		if event.Kind == "run_started" {
			inputs = append(inputs, event.Text)
		}
	}
	require.Equal(t, []string{first.Message.Content, second.Message.Content}, inputs)
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

func TestPrivateUserMemoryWritesAndConsoleEditsStayInSession(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&PrivateChatMessageRecord{}, &PrivateUserMemory{}))
	session := PrivateSession{BotID: 101, UserID: 303}
	otherBot := PrivateSession{BotID: 102, UserID: session.UserID}
	peer := User{UserId: session.UserID}
	bot := User{UserId: session.BotID}
	p := NewPrivatePersona(session, nil, db, Options{ImpressionUpdateEnable: true})
	require.NotContains(t, NewPrivatePersona(session, nil, db, Options{}).ToolNames(), "update_user_memory")
	require.NoError(t, writeUserMemory(db, UserProfile{UserID: session.UserID, Summary: "群聊画像"}))
	require.NoError(t, saveUserMemory(db, UserProfile{UserID: session.UserID, Summary: "其他机器人的私聊画像"}, &otherBot))

	now := time.Now()
	evidence, err := p.savePrivateMessage(GroupMessage{User: peer, Content: "这周休假，记住一下", CreatedAt: now})
	require.NoError(t, err)
	botMessage, err := p.savePrivateMessage(GroupMessage{User: bot, Content: "知道了", CreatedAt: now})
	require.NoError(t, err)
	other := NewPrivatePersona(otherBot, nil, db, Options{})
	otherMessage, err := other.savePrivateMessage(GroupMessage{User: peer, Content: "其他会话", CreatedAt: now})
	require.NoError(t, err)
	groupMessage := ChatMessageRecord{ID: otherMessage + 100, GroupID: 777, UserID: session.UserID, Content: "群聊", CreatedAt: now}
	require.NoError(t, db.Create(&groupMessage).Error)

	run := &agent.RunContext{Values: map[string]any{"self_user_id": session.BotID}}
	update := func(user int64, version uint64, source uint, summary string) (any, error) {
		return p.handleUpdateUserMemory(run, json.RawMessage(fmt.Sprintf(`{"user_id":%d,"previous_version":%d,"evidence_ids":[%d],"reason":"本人明确告知","summary":%q,"upsert":[{"key":"vacation","kind":"state","text":"休假","source_id":%d,"confidence":"explicit"}]}`, user, version, source, summary, source)))
	}
	for _, source := range []uint{botMessage, otherMessage, groupMessage.ID} {
		_, err := update(0, 0, source, "不应保存")
		require.ErrorContains(t, err, "current chat")
	}
	_, err = update(session.UserID+1, 0, evidence, "不应保存")
	require.ErrorContains(t, err, "must match")
	_, err = update(0, 1, evidence, "不能用群聊版本")
	require.ErrorContains(t, err, "stale memory version")
	result, err := update(0, 0, evidence, "当前私聊画像")
	require.NoError(t, err)
	updated := result.(UserProfile)
	require.Equal(t, uint64(1), updated.Version)
	require.Equal(t, "当前私聊画像", updated.Summary)
	require.Len(t, updated.Items, 1)
	require.Equal(t, evidence, updated.Items[0].SourceID)
	_, err = update(0, 0, evidence, "不能覆盖新版本")
	require.ErrorContains(t, err, "stale memory version")

	group := NewPersona(777, nil, db, Options{})
	var groupEnvelope agentEventEnvelope
	require.NoError(t, json.Unmarshal([]byte(group.eventPrompt(GroupMessage{User: peer}, "", session.BotID)), &groupEnvelope))
	require.Equal(t, "群聊画像", groupEnvelope.UserMemory.Summary)
	require.Nil(t, groupEnvelope.PrivateUserMemory)
	groupRead, err := group.handleReadUserMemory(nil, json.RawMessage(`{"user_id":303}`))
	require.NoError(t, err)
	require.Equal(t, "群聊画像", groupRead.(UserProfile).Summary)
	require.Empty(t, groupRead.(UserProfile).Items)

	snapshot, err := PrivateMemorySnapshot(db, 0)
	require.NoError(t, err)
	entries := snapshot.(map[string]any)["users"].([]map[string]any)
	require.Len(t, entries, 2)
	require.Equal(t, "101", entries[0]["bot_id"])
	require.Equal(t, "303", entries[0]["user_id"])
	edit := MemoryEdit{Target: "item", Version: updated.Version, Key: "vacation", Kind: "state", Text: "休假到下周", Confidence: "explicit", ExpiresAt: updated.Items[0].ExpiresAt}
	require.NoError(t, EditPrivateMemory(db, session, edit, false))
	require.Error(t, EditPrivateMemory(db, session, edit, false))
	require.Error(t, EditPrivateMemory(db, session, MemoryEdit{Target: "group_profile", Version: 2}, false))
	profile, err := readPrivateUserMemory(db, session)
	require.NoError(t, err)
	require.Equal(t, "休假到下周", profile.Items[0].Text)
	require.NoError(t, EditPrivateMemory(db, session, MemoryEdit{Target: "profile", Version: profile.Version}, true))
	profile, err = readPrivateUserMemory(db, session)
	require.NoError(t, err)
	require.Empty(t, profile.Items)
	require.Empty(t, profile.Summary)
	profile, err = readPrivateUserMemory(db, otherBot)
	require.NoError(t, err)
	require.Equal(t, "其他机器人的私聊画像", profile.Summary)
	profile, err = readUserMemory(db, session.UserID)
	require.NoError(t, err)
	require.Equal(t, "群聊画像", profile.Summary)
	require.Equal(t, uint64(1), profile.Version)
}
