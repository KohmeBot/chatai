package persona

import (
	"strings"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

type routeTestModel struct{ name string }

func (*routeTestModel) Request(*model.Request, *model.Response) error { return nil }

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

func TestAgentRulesDefineStructuredSelfIdentity(t *testing.T) {
	require.Contains(t, agentRules, "“Agent自己”始终指你本人")
	require.Contains(t, agentRules, "is_self=true")
	require.Contains(t, agentRules, "self_user_id")
}

func TestModelForMessageRoutesWholeImageRequestToVisionModel(t *testing.T) {
	textModel := &routeTestModel{name: "text"}
	visionModel := &routeTestModel{name: "vision"}
	p := &Persona{opts: Options{AgentModel: textModel, VisionModel: visionModel}}

	selected, imageURL := p.modelForMessage(GroupMessage{Content: "这张图是什么意思？", Url: "https://example.com/image.png"})
	require.Same(t, visionModel, selected)
	require.Equal(t, "https://example.com/image.png", imageURL)

	selected, imageURL = p.modelForMessage(GroupMessage{Content: "纯文本问题"})
	require.Same(t, textModel, selected)
	require.Empty(t, imageURL)
}
