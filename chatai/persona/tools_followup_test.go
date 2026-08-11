package persona

import (
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
)

func TestBuiltinToolSearchFindsAskUserAndWait(t *testing.T) {
	p := &Persona{tools: agent.NewRegistry()}
	p.registerBuiltinTools()

	results := p.tools.Search("信息不足，需要追问用户并等待回复", 5)
	require.Contains(t, searchNames(results), "ask_user_and_wait")
	for _, definition := range p.tools.GroupActionDefinitions() {
		require.NotEqual(t, "ask_user_and_wait", definition.Function.Name, "the Agent must still send a final response after the follow-up")
	}
}

func TestFollowUpCapturesOnlyTargetUsersFirstMessage(t *testing.T) {
	p := &Persona{}
	waiter, err := p.registerFollowUp(10001)
	require.NoError(t, err)

	require.False(t, p.deliverFollowUp(GroupMessage{User: User{UserId: 20002}, Content: "路过"}))
	want := GroupMessage{
		User: User{UserId: 10001, Nickname: "目标用户"}, Content: "我想选第二个",
		MsgType: MsgTypeText, MsgID: 88, CreatedAt: time.Now(),
	}
	require.True(t, p.deliverFollowUp(want))
	require.False(t, p.deliverFollowUp(GroupMessage{User: User{UserId: 10001}, Content: "第二条"}))
	require.Equal(t, want, <-waiter.reply)
}

func TestFollowUpRejectsConcurrentWaitForSameUser(t *testing.T) {
	p := &Persona{}
	first, err := p.registerFollowUp(10001)
	require.NoError(t, err)

	_, err = p.registerFollowUp(10001)
	require.ErrorContains(t, err, "already waiting")
	require.True(t, p.cancelFollowUp(first))

	_, err = p.registerFollowUp(10001)
	require.NoError(t, err)
}

func TestCancelFollowUpDoesNotRemoveNewWaiter(t *testing.T) {
	p := &Persona{}
	old, err := p.registerFollowUp(10001)
	require.NoError(t, err)
	require.True(t, p.cancelFollowUp(old))

	current, err := p.registerFollowUp(10001)
	require.NoError(t, err)
	require.False(t, p.cancelFollowUp(old))
	require.True(t, p.deliverFollowUp(GroupMessage{User: User{UserId: 10001}, Content: "答复"}))
	require.Equal(t, "答复", (<-current.reply).Content)
}

func TestFollowUpReplyResultPreservesMessageDetails(t *testing.T) {
	result := followUpReplyResult(77, GroupMessage{
		User: User{UserId: 10001, Nickname: "小明"}, MsgID: 88,
		MsgType: MsgTypeImg, Content: "看这张", Url: "https://example.com/image.png", CreatedAt: time.Now(),
	})

	require.Equal(t, false, result["timed_out"])
	require.Equal(t, int64(77), result["question_message_id"])
	reply := result["reply"].(map[string]any)
	require.Equal(t, int64(10001), reply["user_id"])
	require.Equal(t, int64(88), reply["message_id"])
	require.Equal(t, "看这张", reply["content"])
	require.Equal(t, "https://example.com/image.png", reply["image_url"])
}
