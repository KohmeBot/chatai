package persona

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/favor"
	"strings"
)

const speakPrompt = `
你现在需要根据你的思考，根据群聊的内容在群聊中发一句话，下面我会将摘要时间段和最近的群聊内容给你
摘要部分:
%s

以下是QQ群聊记录，格式为：
<消息ID> [时间] 昵称(用户ID) [行为]: 内容
其中"行为"说明这条消息的类型，例如：
- [说]: 普通文字消息
- [@某人 说]: @某人并说了什么
- [回复某人]: 引用回复某人的消息

%s
`

const speakPromptWithAtMe = `
%s @了你，对你说了「%s」，你对他的好感度是%d(%s:%s)，好感度的范围是(%d~%d)，下面我会将摘要时间段和最近的群聊内容给你，你需要根据最近内容回答他对你说的话
摘要部分:
%s

以下是QQ群聊记录，格式为：
<消息ID> [时间] 昵称(用户ID) [行为]: 内容
其中"行为"说明这条消息的类型，例如：
- [说]: 普通文字消息
- [@某人 说]: @某人并说了什么
- [回复某人]: 引用回复某人的消息

%s
`

const speakPromptWithPokeMe = `
%s 戳了一下你，你对他的好感度是%d(%s:%s)，好感度的范围是(%d~%d)，下面我会将摘要时间段和最近的群聊内容给你，你需要根据最近内容作出反应
摘要部分:
%s

以下是QQ群聊记录，格式为：
<消息ID> [时间] 昵称(用户ID) [行为]: 内容
其中"行为"说明这条消息的类型，例如：
- [说]: 普通文字消息
- [@某人 说]: @某人并说了什么
- [回复某人]: 引用回复某人的消息

%s
`

const speakJson = `
以下面的Json的形式回答我:
{
	"text": "你要说的话。以下情况可以留空保持沉默：群内刚有人说过类似的话、你没有有价值的补充",
	"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
	"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),非必要不回复
}
`
const speakAndUpdateAbstractJson = `
以下面的Json的形式回答我:
{
	"text": "你要说的话。以下情况可以留空保持沉默：群内刚有人说过类似的话、你没有有价值的补充",
	"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
	"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
	"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),非必要不回复,
	"newAbstract": "为你自己下次参与群聊准备的上下文备忘，用第一人称记录关键事件、你说了什么、群友的态度变化，1000字以内"
}
`
const atMeJson = `
以下面的Json的形式回答我:
{
	"text": "你要说的话。以下情况可以留空保持沉默：群内刚有人说过类似的话、你没有有价值的补充",
	"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
	"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
	"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),非必要不回复,
	"favor": 这是根据群友对你说的话增加或者减少的好感度(int格式),每次最多增加%d点,减少%d点
}
`
const atMeAndUpdateAbstractJson = `
以下面的Json的形式回答我:
{
	"text": "你要说的话。以下情况可以留空保持沉默：群内刚有人说过类似的话、你没有有价值的补充",
	"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
	"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
	"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),非必要不回复,
	"favor": 这是根据群友对你说的话增加或者减少的好感度(int格式),每次最多增加%d点,减少%d点,
	"newAbstract": "为你自己下次参与群聊准备的上下文备忘，用第一人称记录关键事件、你说了什么、群友的态度变化，1000字以内"
}
`
const pokeMeJson = `
以下面的Json的形式回答我:
{
	"text": "你要说的话。以下情况可以留空保持沉默：群内刚有人说过类似的话、你没有有价值的补充",
	"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
	"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
	"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),非必要不回复,
	"favor": 这是根据群友对你说的话增加或者减少的好感度(int格式),每次最多增加%d点,减少%d点
}
`
const pokeMeAndUpdateAbstractJson = `
以下面的Json的形式回答我:
{
	"text": "你要说的话。以下情况可以留空保持沉默：群内刚有人说过类似的话、你没有有价值的补充",
	"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
	"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
	"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),非必要不回复,
	"favor": 这是根据群友对你说的话增加或者减少的好感度(int格式),每次最多增加%d点,减少%d点,
	"newAbstract": "为你自己下次参与群聊准备的上下文备忘，用第一人称记录关键事件、你说了什么、群友的态度变化，1000字以内"
}
`

type promptBuilder struct {
	isAtMe      bool
	isPokeMe    bool
	toMeMsg     GroupMessage
	targetFavor int64

	msgCtx MsgContext
}

func newPromptBuilder(msgCtx MsgContext) *promptBuilder {
	return &promptBuilder{
		msgCtx: msgCtx,
	}
}

func (p *promptBuilder) WithAtMe(msg GroupMessage, targetFavor int64, isPoke bool) {
	p.isAtMe = true
	p.toMeMsg = msg
	p.targetFavor = targetFavor
	p.isPokeMe = isPoke
}

func (p *promptBuilder) Build() string {

	var prompt strings.Builder

	switch {
	case p.isPokeMe:
		desc := favor.GetFavorLevelInfo(p.targetFavor)
		prompt.WriteString(fmt.Sprintf(
			speakPromptWithPokeMe,
			p.toMeMsg.User.String(),
			p.targetFavor, desc.Name, desc.Desc, favor.FavorMin, favor.FavorMax,
			p.msgCtx.abstract.String(),
			p.msgCtx.groupMsgContent,
		))
	case p.isAtMe:
		desc := favor.GetFavorLevelInfo(p.targetFavor)
		prompt.WriteString(fmt.Sprintf(
			speakPromptWithAtMe,
			p.toMeMsg.User.String(), p.toMeMsg.Content,
			p.targetFavor, desc.Name, desc.Desc, favor.FavorMin, favor.FavorMax,
			p.msgCtx.abstract.String(),
			p.msgCtx.groupMsgContent,
		))
	default:
		prompt.WriteString(fmt.Sprintf(
			speakPrompt,
			p.msgCtx.abstract.String(),
			p.msgCtx.groupMsgContent,
		))
	}

	prompt.WriteByte('\n')

	switch {
	case p.isPokeMe && p.msgCtx.needUpdateAbstract:
		prompt.WriteString(pokeMeAndUpdateAbstractJson)
	case p.isPokeMe:
		prompt.WriteString(pokeMeJson)
	case p.isAtMe && p.msgCtx.needUpdateAbstract:
		prompt.WriteString(atMeAndUpdateAbstractJson)
	case p.isAtMe:
		prompt.WriteString(atMeJson)
	case p.msgCtx.needUpdateAbstract:
		prompt.WriteString(speakAndUpdateAbstractJson)
	default:
		prompt.WriteString(speakJson)
	}

	return prompt.String()

}
