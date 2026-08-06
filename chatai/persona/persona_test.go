package persona

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEventPromptUsesFormattedMessageAndIncludesQuote(t *testing.T) {
	p := &Persona{groupID: 123}
	msg := GroupMessage{
		User: User{UserId: 1, Nickname: "提问者"}, TargetUser: User{UserId: 2, Nickname: "机器人"},
		QuotedUser: User{UserId: 3, Nickname: "原作者"}, Content: "这是真的吗？", QuotedContent: "原消息内容",
		MsgType: MsgTypeReply, MsgID: 11, QuotedMsgID: 10, CreatedAt: time.Now(),
	}
	prompt := p.eventPrompt(msg, "")
	require.Contains(t, prompt, formatMessage(msg))
	require.Contains(t, prompt, "原消息内容")
	require.Contains(t, prompt, "原作者(3)")
}

func TestAgentRulesPrioritizeContextBeforeAskingUser(t *testing.T) {
	require.True(t, strings.Index(agentRules, "第一步是判断是否需要群聊上下文") < strings.Index(agentRules, "不要反问用户"))
}
