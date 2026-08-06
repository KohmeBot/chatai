package persona

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChatMessageRecordRoundTripPreservesContextFields(t *testing.T) {
	createdAt := time.Date(2026, 8, 6, 12, 30, 0, 0, time.Local)
	want := GroupMessage{
		User:       User{UserId: 10, Nickname: "sender"},
		TargetUser: User{UserId: 20, Nickname: "target"},
		Content:    "reply text",
		MsgType:    MsgTypeReply,
		MsgID:      30,
		CreatedAt:  createdAt,
		Url:        "https://example.com/image.png",
		FileName:   "image.png",
		Refer:      true,
	}
	record := messageRecord(40, want)
	require.Equal(t, int64(40), record.GroupID)
	require.Equal(t, want, record.message())
}

func TestRepeatSignatureIncludesImageAndTarget(t *testing.T) {
	base := GroupMessage{MsgType: MsgTypeReply, Content: "same", TargetUser: User{UserId: 1}, Url: "https://example.com/a.png"}
	differentImage := base
	differentImage.Url = "https://example.com/b.png"
	differentTarget := base
	differentTarget.TargetUser.UserId = 2

	require.NotEqual(t, repeatSignature(base), repeatSignature(differentImage))
	require.NotEqual(t, repeatSignature(base), repeatSignature(differentTarget))
	require.True(t, repeatable(base))
	require.False(t, repeatable(GroupMessage{MsgType: MsgTypePoke}))
}
