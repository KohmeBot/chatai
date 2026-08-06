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

func TestShouldRepeatUsesInMemorySlidingWindow(t *testing.T) {
	p := new(Persona)
	first := GroupMessage{MsgType: MsgTypeText, Content: "same"}
	different := GroupMessage{MsgType: MsgTypeText, Content: "different"}

	require.False(t, p.shouldRepeat(first, 3))
	require.False(t, p.shouldRepeat(first, 3))
	require.True(t, p.shouldRepeat(first, 3))
	require.False(t, p.shouldRepeat(first, 3), "only repeat once in one uninterrupted run")
	require.False(t, p.shouldRepeat(different, 3))
	require.False(t, p.shouldRepeat(different, 3))
	require.True(t, p.shouldRepeat(different, 3), "a different message starts a new run")
}

func TestShouldRepeatUsesContentEqualForImages(t *testing.T) {
	p := new(Persona)
	first := GroupMessage{MsgType: MsgTypeImg, Url: "https://example.com/first", FileName: "same.image"}
	second := GroupMessage{MsgType: MsgTypeImg, Url: "https://example.com/second", FileName: "same.image"}

	require.False(t, p.shouldRepeat(first, 2))
	require.True(t, p.shouldRepeat(second, 2), "matching image file names are the same content even if URLs change")
}
