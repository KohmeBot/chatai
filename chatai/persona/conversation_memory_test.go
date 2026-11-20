package persona

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type conversationTestModel struct {
	request func(*model.Request, *model.Response) error
}

func (m conversationTestModel) Request(req *model.Request, res *model.Response) error {
	return m.request(req, res)
}

func memoryPersona() *Persona {
	return &Persona{groupID: 10, opts: Options{ConversationMemory: true, ConversationWindow: 10 * time.Minute, ConversationMaxChars: 200}}
}

func memoryRun(userID int64) *agent.RunContext {
	return &agent.RunContext{UserID: userID, Values: map[string]any{conversationTurnKey: &conversationTurn{}, "self_user_id": int64(99)}}
}

func TestConversationStoresOnlyVisibleDialogueAndInjectsNextEvent(t *testing.T) {
	p := memoryPersona()
	rc := memoryRun(1)
	p.rememberDialogue(rc, "user", 1, 10, "想去杭州", "text")
	p.rememberBotDialogue(rc, message.Message{message.Text("想去几天？")}, 11)
	p.rememberBotDialogue(rc, message.Message{message.Text("发送失败")}, 0)
	p.finishConversation(rc)
	var event agentEventEnvelope
	require.NoError(t, json.Unmarshal([]byte(p.eventPrompt(GroupMessage{User: User{UserId: 1}, Content: "继续"}, "", 99)), &event))
	require.NotNil(t, event.Conversation)
	require.Contains(t, event.Conversation.Dialogue, "想去杭州")
	require.Contains(t, event.Conversation.Dialogue, "想去几天？")
	require.NotContains(t, event.Conversation.Dialogue, "发送失败")
	require.Nil(t, p.conversationNote(2, time.Now()))
	require.Nil(t, memoryPersona().conversationNote(1, time.Now()))
	var scheduled agentEventEnvelope
	require.NoError(t, json.Unmarshal([]byte(p.eventPrompt(GroupMessage{User: User{UserId: 1}}, "定时任务", 99)), &scheduled))
	require.Nil(t, scheduled.Conversation)
	require.Nil(t, p.conversationNote(1, time.Now().Add(11*time.Minute)))
	require.Empty(t, p.conversations)
}

func TestConversationCompressionHasNoHistoryOrTools(t *testing.T) {
	p := memoryPersona()
	calls := 0
	p.opts.MemoryModel = conversationTestModel{request: func(req *model.Request, res *model.Response) error {
		calls++
		require.Empty(t, req.History)
		require.Empty(t, req.Tools)
		require.Contains(t, req.Question, "杭州")
		_, hasDeadline := req.Context.Deadline()
		require.True(t, hasDeadline)
		res.Answer = `{"summary":"用户计划去杭州两天。","first_action":"先读取旅游 Skill。"}`
		return nil
	}}
	rc := memoryRun(1)
	p.rememberDialogue(rc, "user", 1, 1, strings.Repeat("杭州", 120), "text")
	p.finishConversation(rc)
	require.Equal(t, 1, calls)
	note := p.conversationNote(1, time.Now())
	require.Equal(t, "用户计划去杭州两天。", note.Summary)
	require.Empty(t, note.Dialogue)
	require.LessOrEqual(t, conversationSize(*note), 200)
}

func TestPrivateConversationMemoryCompressionAndSnapshotIsolation(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&PrivateUserMemory{}))
	const userID = int64(42)
	p := NewPrivatePersona(PrivateSession{BotID: 101, UserID: userID}, nil, db, Options{
		ConversationMemory: true,
		MemoryModel: conversationTestModel{request: func(req *model.Request, res *model.Response) error {
			require.Contains(t, req.Question, `"context_tool":"read_private_context"`)
			res.Answer = `{"summary":"当前私聊摘要"}`
			return nil
		}},
	})
	other := NewPrivatePersona(PrivateSession{BotID: 102, UserID: userID}, nil, db, Options{ConversationMemory: true})
	group := memoryPersona()
	for _, scope := range []*Persona{p, other, group} {
		rc := memoryRun(userID)
		scope.rememberDialogue(rc, "user", userID, 1, strings.Repeat("前文", 120), "text")
		scope.finishConversation(rc)
	}
	now := time.Now()
	note := p.conversationNote(userID, now)
	require.Equal(t, "当前私聊摘要", note.Summary)
	require.Contains(t, note.FirstAction, "read_private_context")
	otherNote := other.conversationNote(userID, now)
	require.Empty(t, otherNote.Summary)
	require.Contains(t, otherNote.FirstAction, "read_private_context")
	require.LessOrEqual(t, conversationSize(*otherNote), other.opts.ConversationMaxChars)
	require.Contains(t, group.conversationNote(userID, now).FirstAction, "read_group_context")

	// A live private session without a persisted profile still appears in the monitor.
	expired := other.conversations[userID]
	expired.Updated = now.Add(-other.opts.ConversationWindow)
	other.conversations[userID] = expired
	snapshot, err := PrivateMemorySnapshot(db, 0, true, []*Persona{group, other, p})
	require.NoError(t, err)
	data := snapshot.(map[string]any)
	require.Equal(t, true, data["short_enabled"])
	require.Empty(t, data["users"])
	rows := data["short"].([]map[string]any)
	require.Len(t, rows, 1)
	require.Equal(t, "101", rows[0]["bot_id"])
	require.Equal(t, "42", rows[0]["user_id"])
	require.Equal(t, *note, rows[0]["note"])
}

func TestConversationCompressionFailureAndOversizeStayBounded(t *testing.T) {
	for _, answer := range []string{"error", "invalid json", `{"summary":""}`, `{"summary":"` + strings.Repeat("中", 500) + `"}`} {
		t.Run(answer[:min(len(answer), 20)], func(t *testing.T) {
			p := memoryPersona()
			p.opts.MemoryModel = conversationTestModel{request: func(req *model.Request, res *model.Response) error {
				if answer == "error" {
					return errors.New("offline")
				}
				res.Answer = answer
				return nil
			}}
			rc := memoryRun(1)
			p.rememberDialogue(rc, "user", 1, 1, strings.Repeat("中", 500), "text")
			p.finishConversation(rc)
			note := p.conversationNote(1, time.Now())
			require.LessOrEqual(t, conversationSize(*note), 200)
			require.Contains(t, note.FirstAction, "read_group_context")
		})
	}
}

func TestConversationSlowCompressionCannotOverwriteNewerTurn(t *testing.T) {
	p := memoryPersona()
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p.opts.MemoryModel = conversationTestModel{request: func(req *model.Request, res *model.Response) error {
		if strings.Contains(req.Question, "第二轮") {
			res.Answer = `{"summary":"第二轮最新内容"}`
			return nil
		}
		close(started)
		<-release
		res.Answer = `{"summary":"过时内容"}`
		return nil
	}}
	first := memoryRun(1)
	p.rememberDialogue(first, "user", 1, 1, strings.Repeat("第一轮", 100), "text")
	go func() { defer close(done); p.finishConversation(first) }()
	<-started
	second := memoryRun(1)
	p.rememberDialogue(second, "user", 1, 2, strings.Repeat("第二轮", 100), "text")
	p.finishConversation(second)
	close(release)
	<-done
	require.Equal(t, "第二轮最新内容", p.conversationNote(1, time.Now()).Summary)
}

func TestConversationDisabledDoesNotInjectMemory(t *testing.T) {
	p := memoryPersona()
	p.opts.ConversationMemory = false
	require.Nil(t, p.conversationNote(1, time.Now()))
	rc := &agent.RunContext{}
	p.rememberDialogue(rc, "user", 1, 1, "忽略", "text")
	p.finishConversation(rc)
	require.Empty(t, p.conversations)
}
