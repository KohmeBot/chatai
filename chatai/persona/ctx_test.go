package persona

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGroupContextShouldRepeatAtConfiguredThresholdOnce(t *testing.T) {
	var ctx groupContext
	msg := GroupMessage{User: User{UserId: 1}, Content: "复读", MsgType: MsgTypeText, CreatedAt: time.Now()}
	for i := 0; i < 2; i++ {
		ctx.AppendMsg(msg, 0)
		require.False(t, ctx.ShouldRepeat(msg, 3))
	}
	ctx.AppendMsg(msg, 0)
	require.True(t, ctx.ShouldRepeat(msg, 3))
	ctx.AppendMsg(msg, 0)
	require.False(t, ctx.ShouldRepeat(msg, 3))
}

func TestGroupContextDoesNotRepeatUnsupportedMessage(t *testing.T) {
	var ctx groupContext
	msg := GroupMessage{User: User{UserId: 1}, MsgType: MsgTypePoke, CreatedAt: time.Now()}
	ctx.AppendMsg(msg, 0)
	ctx.AppendMsg(msg, 0)
	require.False(t, ctx.ShouldRepeat(msg, 2))
}

func TestGroupContextCanRepeatSameContentInANewSequence(t *testing.T) {
	var ctx groupContext
	repeated := GroupMessage{Content: "again", MsgType: MsgTypeText, CreatedAt: time.Now()}
	other := GroupMessage{Content: "break", MsgType: MsgTypeText, CreatedAt: time.Now()}
	for i := 0; i < 2; i++ {
		ctx.AppendMsg(repeated, 0)
	}
	require.True(t, ctx.ShouldRepeat(repeated, 2))
	ctx.AppendMsg(other, 0)
	require.False(t, ctx.ShouldRepeat(other, 2))
	ctx.AppendMsg(repeated, 0)
	require.False(t, ctx.ShouldRepeat(repeated, 2))
	ctx.AppendMsg(repeated, 0)
	require.True(t, ctx.ShouldRepeat(repeated, 2))
}
