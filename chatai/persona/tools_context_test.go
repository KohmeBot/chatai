package persona

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
)

func TestTimeMessageResultsMarksAndNormalizesSelfIdentity(t *testing.T) {
	const selfUserID int64 = 10001
	messages := []GroupMessage{
		{
			User:          User{UserId: selfUserID, Nickname: "你"}, // 兼容改造前已入库的数据。
			TargetUser:    User{UserId: 20002, Nickname: "群友"},
			QuotedUser:    User{UserId: selfUserID, Nickname: "你"},
			Content:       "这是我此前发出的消息",
			QuotedContent: "这是我此前被引用的消息",
			MsgType:       MsgTypeReply,
			CreatedAt:     time.Now(),
		},
		{
			User:       User{UserId: 20002, Nickname: "群友"},
			TargetUser: User{UserId: selfUserID, Nickname: "你"},
			Content:    "这条消息发给 Agent",
			MsgType:    MsgTypeAt,
			CreatedAt:  time.Now(),
		},
	}

	results := timeMessageResults(messages, selfUserID)
	require.Len(t, results, 2)
	require.True(t, results[0].IsSelf)
	require.Equal(t, selfNickname, results[0].Nickname)
	require.True(t, results[0].QuotedIsSelf)
	require.Equal(t, selfNickname, results[0].QuotedNickname)
	require.False(t, results[0].TargetIsSelf)
	require.False(t, results[1].IsSelf)
	require.Equal(t, "群友", results[1].Nickname)
	require.True(t, results[1].TargetIsSelf)
	require.Equal(t, selfNickname, results[1].TargetNickname)
}

func TestContextResultJSONExposesSelfUserIDAndIsSelf(t *testing.T) {
	const selfUserID int64 = 10001
	result := contextQueryResult{
		CurrentTime: "2026-08-12T17:12:49+08:00",
		SelfUserID:  selfUserID,
		Count:       1,
		Messages: timeMessageResults([]GroupMessage{{
			User:      User{UserId: selfUserID, Nickname: "你"},
			Content:   "旧消息",
			MsgType:   MsgTypeText,
			CreatedAt: time.Now(),
		}}, selfUserID),
	}

	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"current_time":"2026-08-12T17:12:49+08:00",
		"self_user_id":10001,
		"count":1,
		"has_more":false,
		"messages":[{
			"message_id":0,
			"created_at":"`+result.Messages[0].CreatedAt+`",
			"user_id":10001,
			"nickname":"Agent自己",
			"is_self":true,
			"type":"text",
			"content":"旧消息"
		}]
	}`, string(raw))
}

func TestReadPrivateContextFiltersAndPagesCurrentSession(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&PrivateChatMessageRecord{}))
	session := PrivateSession{BotID: 10001, UserID: 20002}
	p := NewPrivatePersona(session, nil, db, Options{ContextLimit: 3})
	start := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	save := func(persona *Persona, timestamp time.Time, sender int64, text string, messageID int64) uint {
		t.Helper()
		id, err := persona.savePrivateMessage(GroupMessage{
			User: User{UserId: sender}, CreatedAt: timestamp, Content: text, MsgType: MsgTypeText, MsgID: messageID,
		})
		require.NoError(t, err)
		return id
	}
	save(p, start.Add(-time.Minute), session.UserID, "发布 API 过早", 1)
	first := save(p, start, session.UserID, "发布 API 第一步", 2)
	save(p, start.Add(time.Minute), session.UserID, "其他话题", 3)
	second := save(p, start.Add(90*time.Second), session.BotID, "发布 API 第二步", 4)
	third := save(p, start.Add(3*time.Minute), session.UserID, "发布 api 第三步", 5)
	end := start.Add(4 * time.Minute)
	save(p, end, session.UserID, "发布 API 过晚", 6)
	otherPeer := NewPrivatePersona(PrivateSession{BotID: session.BotID, UserID: 20003}, nil, db, Options{})
	otherBot := NewPrivatePersona(PrivateSession{BotID: 10002, UserID: session.UserID}, nil, db, Options{})
	save(otherPeer, start, otherPeer.private.UserID, "发布 API 其他用户", 5)
	save(otherBot, start, otherBot.private.UserID, "发布 API 其他机器人", 5)

	tool := p.privateContextTools()[0]
	require.Equal(t, agent.ToolScopePrivate, tool.Scope)
	query := map[string]any{"since": start, "until": end, "keyword": " API ", "limit": 2}
	read := func(query map[string]any) contextQueryResult {
		t.Helper()
		raw, err := json.Marshal(query)
		require.NoError(t, err)
		result, err := tool.Handler(nil, raw)
		require.NoError(t, err)
		return result.(contextQueryResult)
	}
	page := read(query)
	require.Equal(t, session.BotID, page.SelfUserID)
	require.Equal(t, 2, page.Count)
	require.True(t, page.HasMore)
	require.Equal(t, 2, page.NextOffset)
	require.Len(t, page.Messages, 2)
	require.Equal(t, second, page.Messages[0].SourceID)
	require.True(t, page.Messages[0].IsSelf)
	require.Equal(t, third, page.Messages[1].SourceID)
	require.EqualValues(t, 5, page.Messages[1].MessageID)
	query["offset"] = page.NextOffset
	page = read(query)
	require.False(t, page.HasMore)
	require.Zero(t, page.NextOffset)
	require.Len(t, page.Messages, 1)
	require.Equal(t, first, page.Messages[0].SourceID)

	page = read(map[string]any{"last_minutes": 8, "keyword": "api"})
	require.Len(t, page.Messages, 2)
	require.Equal(t, third, page.Messages[0].SourceID)
	_, err := tool.Handler(nil, json.RawMessage(`{"last_minutes":30,"since":"2026-08-07 10:00"}`))
	require.ErrorContains(t, err, "last_minutes and since")
}
