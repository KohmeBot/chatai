package persona

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/favor"
	"strings"
)

const stylePrompt = `
你说话的风格要像真实的群友，不要用书面标点。具体来说：
- 不要在句尾加句号，口语里没人这么说话
- 语气词、省略、不完整的句子都是正常的

`

const speakPrompt = `
你想参与这个群聊，可以选择以下任意一种方式：
- 对某条消息发表看法或接话
- 主动@某个人问他问题或调侃他
- 抛出一个和当前话题相关的新问题
你对这个群聊的印象是,这些印象会影响你在这个群内的理解,说话方式和话题偏好:
%s
请记住:
- 不要强行评论所有内容，挑你最感兴趣的一点说，如果你要接话或引用某条消息，只考虑最近5条消息，忽略更早的内容
- 标注了[已回复]的消息不要再引用或回应，避免重复
- 如果你在最近的消息记录中已经对某个话题发表过看法，不要重复接同一个话题，换一个角度或保持沉默
以下是较早之前的聊天摘要，仅供参考背景，不代表当前话题：
%s

以下是最新的群聊内容，优先关注这部分，格式为：
<消息ID> [时间] 昵称(用户ID) [行为]: 内容
其中"行为"说明这条消息的类型，例如：
- [说]: 普通文字消息
- [@某人 说]: @某人并说了什么
- [回复某人]: 引用回复某人的消息

%s
`

const speakPromptWithAtMe = `
你对这个群聊的印象是,这些印象会影响你在这个群内的理解,说话方式和话题偏好:
%s
%s @了你，对你说了「%s」，你对他的好感度是%d(%s:%s)，好感度的范围是(%d~%d)，你需要根据他说的话做出反应
你对他的印象是,这会影响你对他的理解、判断和交流方式:
%s
你需要认真回应他说的话。除非他明显是在开玩笑或随便聊，否则都应该给出实质性的回答，不要一笔带过
以下是较早之前的聊天摘要，仅供参考背景，不代表当前话题：
%s

以下是最新的群聊内容，优先关注这部分，格式为：
<消息ID> [时间] 昵称(用户ID) [行为]: 内容
其中"行为"说明这条消息的类型，例如：
- [说]: 普通文字消息
- [@某人 说]: @某人并说了什么
- [回复某人]: 引用回复某人的消息

%s
`

const speakPromptWithPokeMe = `
你对这个群聊的印象是,这些印象会影响你在这个群内的理解,说话方式和话题偏好:
%s
%s 戳了一下你，你对他的好感度是%d(%s:%s)，好感度的范围是(%d~%d)，你需要对他作出反应，如果你想回戳他，在 pokeTarget 里填上他的ID
你对他的印象是,这会影响你对他的理解、判断和交流方式:
%s
以下是较早之前的聊天摘要，仅供参考背景，不代表当前话题：
%s

以下是最新的群聊内容，优先关注这部分，格式为：
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
	"messages":[{
		"text": "你要说的话,默认只发送 1 条消息,但你可以一次说多句话，但每句话要有独立的意义，不要把一句话拆成多条，最多3条,如果你在最近的消息记录中已经对某个话题发表过看法，不要重复接同一个话题，换一个角度或保持沉默",
		"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
		"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),标注了[已回复]的消息不要再引用或回应，避免重复,非必要不回复
	}],
	"groupImpression": "用第一人称更新你对这个群聊的印象，融合已有认知与本次群聊上下文，仅当发现新的长期有效信息或原有认知需要修正时才输出更新后的内容，否则返回空字符串，记录长期有效的群体特征、交流氛围、话题偏好和你应如何自然参与其中，忽略具体事件和短期信息，控制在400字以内",
	"updateImpressions": [
			{
				"userId": 你需要更新印象的这个人的ID(int格式),无视水群,只更新在群聊中有实质性内容并且你对他的印象有所更新的人
				"content": "用第一人称更新你对这个人的印象，融合之前的印象和本次互动内容，300字以内。记录他的性格、说话习惯、对你的态度、特征细节。写得像备忘录而不是日记，信息密度高一点，不要有废话"
			}
		]
}
`
const speakAndUpdateAbstractJson = `
以下面的Json的形式回答我:
{
	"messages":[{
		"text": "你要说的话,默认只发送 1 条消息,但你可以一次说多句话，但每句话要有独立的意义，不要把一句话拆成多条，最多3条,如果你在最近的消息记录中已经对某个话题发表过看法，不要重复接同一个话题，换一个角度或保持沉默",
		"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
		"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
		"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),标注了[已回复]的消息不要再引用或回应，避免重复,非必要不回复
	}],
	"newAbstract": "为你自己下次参与群聊准备的上下文备忘，用第一人称记录关键事件、你说了什么、群友的态度变化，1000字以内",
	"groupImpression": "用第一人称更新你对这个群聊的印象，融合已有认知与本次群聊上下文，仅当发现新的长期有效信息或原有认知需要修正时才输出更新后的内容，否则返回空字符串，记录长期有效的群体特征、交流氛围、话题偏好和你应如何自然参与其中，忽略具体事件和短期信息，控制在400字以内",
	"updateImpressions": [
			{
				"userId": 你需要更新印象的这个人的ID(int格式),无视水群,只更新在群聊中有实质性内容并且你对他的印象有所更新的人
				"content": "用第一人称更新你对这个人的印象，融合之前的印象和本次互动内容，300字以内。记录他的性格、说话习惯、对你的态度、特征细节。写得像备忘录而不是日记，信息密度高一点，不要有废话"
			}
		]
}
`
const atMeJson = `
以下面的Json的形式回答我:
{
	"messages":[{
		"text": "你要说的话,默认只发送 1 条消息,但你可以一次说多句话，但每句话要有独立的意义，不要把一句话拆成多条，最多3条,如果你在最近的消息记录中已经对某个话题发表过看法，不要重复接同一个话题，换一个角度或保持沉默",
		"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
		"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
		"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),标注了[已回复]的消息不要再引用或回应，避免重复,非必要不回复
	}],
	"favor": 这是根据群友对你说的话增加或者减少的好感度(int格式),每次最多增加%d点,减少%d点,
	"groupImpression": "用第一人称更新你对这个群聊的印象，融合已有认知与本次群聊上下文，仅当发现新的长期有效信息或原有认知需要修正时才输出更新后的内容，否则返回空字符串，记录长期有效的群体特征、交流氛围、话题偏好和你应如何自然参与其中，忽略具体事件和短期信息，控制在400字以内",
	"updateImpressions": [
			{
				"userId": 你需要更新印象的这个人的ID(int格式),无视水群,只更新在群聊中有实质性内容并且你对他的印象有所更新的人
				"content": "用第一人称更新你对这个人的印象，融合之前的印象和本次互动内容，300字以内。记录他的性格、说话习惯、对你的态度、特征细节。写得像备忘录而不是日记，信息密度高一点，不要有废话"
			}
		]
}
`
const atMeAndUpdateAbstractJson = `
以下面的Json的形式回答我:
{
	"messages":[{
		"text": "你要说的话,默认只发送 1 条消息,但你可以一次说多句话，但每句话要有独立的意义，不要把一句话拆成多条，最多3条,如果你在最近的消息记录中已经对某个话题发表过看法，不要重复接同一个话题，换一个角度或保持沉默",
		"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
		"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
		"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),标注了[已回复]的消息不要再引用或回应，避免重复,非必要不回复
	}],
	"favor": 这是根据群友对你说的话增加或者减少的好感度(int格式),每次最多增加%d点,减少%d点,
	"newAbstract": "为你自己下次参与群聊准备的上下文备忘，用第一人称记录关键事件、你说了什么、群友的态度变化，1000字以内",
	"groupImpression": "用第一人称更新你对这个群聊的印象，融合已有认知与本次群聊上下文，仅当发现新的长期有效信息或原有认知需要修正时才输出更新后的内容，否则返回空字符串，记录长期有效的群体特征、交流氛围、话题偏好和你应如何自然参与其中，忽略具体事件和短期信息，控制在400字以内",
	"updateImpressions": [
			{
				"userId": 你需要更新印象的这个人的ID(int格式),无视水群,只更新在群聊中有实质性内容并且你对他的印象有所更新的人
				"content": "用第一人称更新你对这个人的印象，融合之前的印象和本次互动内容，300字以内。记录他的性格、说话习惯、对你的态度、特征细节。写得像备忘录而不是日记，信息密度高一点，不要有废话"
			}
		]
}
`
const pokeMeJson = `
以下面的Json的形式回答我:
{
	"messages":[{
		"text": "你要说的话,默认只发送 1 条消息,但你可以一次说多句话，但每句话要有独立的意义，不要把一句话拆成多条，最多3条,如果你在最近的消息记录中已经对某个话题发表过看法，不要重复接同一个话题，换一个角度或保持沉默",
		"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
		"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
		"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),标注了[已回复]的消息不要再引用或回应，避免重复,非必要不回复
	}],
	"favor": 这是根据群友对你说的话增加或者减少的好感度(int格式),每次最多增加%d点,减少%d点,
	"groupImpression": "用第一人称更新你对这个群聊的印象，融合已有认知与本次群聊上下文，仅当发现新的长期有效信息或原有认知需要修正时才输出更新后的内容，否则返回空字符串，记录长期有效的群体特征、交流氛围、话题偏好和你应如何自然参与其中，忽略具体事件和短期信息，控制在400字以内",
	"updateImpressions": [
			{
				"userId": 你需要更新印象的这个人的ID(int格式),无视水群,只更新在群聊中有实质性内容并且你对他的印象有所更新的人
				"content": "用第一人称更新你对这个人的印象，融合之前的印象和本次互动内容，300字以内。记录他的性格、说话习惯、对你的态度、特征细节。写得像备忘录而不是日记，信息密度高一点，不要有废话"
			}
		]
}
`
const pokeMeAndUpdateAbstractJson = `
以下面的Json的形式回答我:
{
	"messages":[{
		"text": "你要说的话,默认只发送 1 条消息,但你可以一次说多句话，但每句话要有独立的意义，不要把一句话拆成多条，最多3条,如果你在最近的消息记录中已经对某个话题发表过看法，不要重复接同一个话题，换一个角度或保持沉默",
		"atTarget": 如果你判断说这句话需要@一个人,则把它的ID填到这里(int格式),非必要不@,
		"pokeTarget": 如果你判断需要戳一个人，则把它的ID填到这里(int格式),非必要不戳他,
		"replayMsg": 如果你判断说这句话需要引用某条消息,则把它的消息id填到这里(int格式),标注了[已回复]的消息不要再引用或回应，避免重复,非必要不回复
	}],
	"favor": 这是根据群友对你说的话增加或者减少的好感度(int格式),每次最多增加%d点,减少%d点,
	"newAbstract": "为你自己下次参与群聊准备的上下文备忘，用第一人称记录关键事件、你说了什么、群友的态度变化，1000字以内",
	"groupImpression": "用第一人称更新你对这个群聊的印象，融合已有认知与本次群聊上下文，仅当发现新的长期有效信息或原有认知需要修正时才输出更新后的内容，否则返回空字符串，记录长期有效的群体特征、交流氛围、话题偏好和你应如何自然参与其中，忽略具体事件和短期信息，控制在400字以内",
	"updateImpressions": [
			{
				"userId": 你需要更新印象的这个人的ID(int格式),无视水群,只更新在群聊中有实质性内容并且你对他的印象有所更新的人
				"content": "用第一人称更新你对这个人的印象，融合之前的印象和本次互动内容，300字以内。记录他的性格、说话习惯、对你的态度、特征细节。写得像备忘录而不是日记，信息密度高一点，不要有废话"
			}
		]
}
`

type promptBuilder struct {
	isAtMe         bool
	isPokeMe       bool
	toMeMsg        GroupMessage
	targetFavor    int64
	userImpression UserImpression

	groupImpression GroupImpression
	msgCtx          MsgContext
}

func newPromptBuilder(msgCtx MsgContext, impression GroupImpression) *promptBuilder {
	return &promptBuilder{
		msgCtx:          msgCtx,
		groupImpression: impression,
	}
}

func (p *promptBuilder) WithAtMe(msg GroupMessage, targetFavor int64, isPoke bool, impression UserImpression) {
	p.isAtMe = true
	p.toMeMsg = msg
	p.targetFavor = targetFavor
	p.isPokeMe = isPoke
	p.userImpression = impression
}

func (p *promptBuilder) Build() string {

	var prompt strings.Builder

	prompt.WriteString(stylePrompt)
	prompt.WriteByte('\n')
	switch {
	case p.isPokeMe:
		desc := favor.GetFavorLevelInfo(p.targetFavor)
		prompt.WriteString(fmt.Sprintf(
			speakPromptWithPokeMe,
			p.groupImpression.String(),
			p.toMeMsg.User.String(),
			p.targetFavor, desc.Name, desc.Desc, favor.FavorMin, favor.FavorMax,
			p.userImpression.String(),
			p.msgCtx.abstract.String(),
			p.msgCtx.groupMsgContent,
		))
	case p.isAtMe:
		desc := favor.GetFavorLevelInfo(p.targetFavor)
		prompt.WriteString(fmt.Sprintf(
			speakPromptWithAtMe,
			p.groupImpression.String(),
			p.toMeMsg.User.String(), p.toMeMsg.Content,
			p.targetFavor, desc.Name, desc.Desc, favor.FavorMin, favor.FavorMax,
			p.userImpression.String(),
			p.msgCtx.abstract.String(),
			p.msgCtx.groupMsgContent,
		))
	default:
		prompt.WriteString(fmt.Sprintf(
			speakPrompt,
			p.groupImpression.String(),
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
