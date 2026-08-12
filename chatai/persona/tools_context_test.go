package persona

import (
	"encoding/json"
	"testing"
	"time"

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
