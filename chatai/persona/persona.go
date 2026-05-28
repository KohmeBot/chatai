package persona

import (
	"encoding/json"
	"github.com/kohmebot/chatai/chatai/favor"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
	"strings"
	"time"
)

type Persona struct {
	// zero.Ctx 用于交互
	groupId   int64
	threshold float64

	gc   groupContext
	bot  *zero.Ctx
	db   *gorm.DB
	urge *SpeechUrge

	llm model.LargeModel

	env plugin.Env

	autoSpeak bool
}

func NewPersona(groupId int64, env plugin.Env, db *gorm.DB, threshold float64, llm model.LargeModel) *Persona {
	return &Persona{
		db:      db,
		llm:     llm,
		groupId: groupId,
		env:     env,
		urge:    NewSpeechUrge(threshold),
	}
}

func (p *Persona) SetAutoSpeak() {
	p.autoSpeak = true
}

func (p *Persona) UpdateContext(ctx *zero.Ctx) error {

	p.gc.Flush()

	segments := ctx.Event.Message
	var msgType string
	for _, segment := range segments {
		msgType = segment.Type
		if msgType != MsgTypeText {
			// 找到第一个非text的消息
			break
		}
	}

	if ctx.Event.SubType == MsgTypePoke {
		msgType = MsgTypePoke
	}

	if !HasMsgType(msgType) {
		return nil
	}

	msgId, _ := ctx.Event.MessageID.(int64)

	msg := GroupMessage{
		User: User{
			UserId:   ctx.Event.UserID,
			Nickname: ctx.CardOrNickName(ctx.Event.UserID),
		},
		TargetUser: User{},
		Url:        getUrl(segments),
		Content:    getText(segments),
		MsgType:    msgType,
		MsgID:      msgId,
		CreatedAt:  time.Now(),
	}

	target := getTargetID(ctx)
	if target > 0 {
		msg.TargetUser = User{
			UserId:   target,
			Nickname: ctx.CardOrNickName(target),
		}
	}

	if ctx.Event.IsToMe {
		msg.TargetUser = User{
			Nickname: "你",
			UserId:   ctx.Event.SelfID,
		}
	}

	// 统计过去 120 秒的消息数，用于活跃度计算
	recentCount := p.gc.AppendMsg(msg, 120*time.Second)

	if msg.MsgType == MsgTypeText {
		// 如果群内在复读，直接参与复读就好了
		repeat, repeated := p.gc.RepeatThis(msg.Content)
		if repeated {
			return nil
		}
		if repeat {
			ctx.Send(ctx.Event.Message)
			return nil
		}
	}

	before := p.urge.Value()
	speak := p.urge.Update(msg, recentCount, ctx.Event.IsToMe)
	after := p.urge.Value()

	logrus.Infof("[%d] 更新发言欲 %.2f -> %.2f", p.groupId, before, after)

	if speak {
		logrus.Infof("trigger speak, to me %v", ctx.Event.IsToMe)
		return p.speak(ctx, msg)
	}

	return nil

}

func (p *Persona) speak(ctx *zero.Ctx, msg GroupMessage) error {
	isAtMe := ctx.Event.IsToMe

	if !isAtMe && !p.autoSpeak {
		return nil
	}

	if msg.MsgType == MsgTypePoke && ctx.Event.IsToMe {
		if !p.gc.CanPoke(msg.User.UserId) {
			return nil
		}
	}

	msgCtx := p.gc.Context()

	builder := newPromptBuilder(msgCtx)
	if isAtMe {
		val, err := favor.GetFavor(p.db, msg.User.UserId)
		if err != nil {
			return err
		}
		builder.WithAtMe(msg, val, msg.MsgType == MsgTypePoke)
	}

	go p.thinking(ctx, msg, builder)

	return nil
}

func (p *Persona) thinking(ctx *zero.Ctx, msg GroupMessage, builder *promptBuilder) {
	msgCtx := builder.msgCtx
	isAtMe := builder.isAtMe

	rsp, err := p.sendRequest(builder)
	if err != nil {
		p.env.Error(ctx, err)
		return
	}

	if rsp.NewAbstract != "" {
		// 更新摘要
		p.gc.UpdateAbstract(abstract{
			content:   rsp.NewAbstract,
			startTime: msgCtx.startTime,
			endTime:   msgCtx.endTime,
		})
	}

	msgs := make([]message.Segment, 0)
	if isAtMe {
		_ = favor.UpdateFavor(p.db, msg.User.UserId, rsp.Favor)
		if msg.MsgID > 0 {
			msgs = append(msgs, message.Reply(msg.MsgID))
		}
		msgs = append(msgs, message.At(msg.User.UserId))

	} else {
		// 非At消息
		switch {

		case rsp.ReplayMsg > 0:
			// 有引用回复
			// 获取到对应消息的发送者，需要at
			rMsg := ctx.GetMessage(rsp.ReplayMsg)
			if len(rMsg.Elements) > 0 {
				msgs = append(msgs, message.Reply(rsp.ReplayMsg), message.At(rMsg.Sender.ID))
			}
		case rsp.AtTarget > 0:
			// 有At对象
			msgs = append(msgs, message.At(rsp.AtTarget))
		}

	}

	if rsp.PokeTarget > 0 {
		// send_poke为napcat的私有接口，并不遵循onebot11标准，在非napcat上可能会报错
		ctx.CallAction("send_poke", zero.Params{"group_id": p.groupId, "user_id": rsp.PokeTarget})
	}

	if rsp.Text != "" {
		if len(msgs) > 0 {
			msgs = append(msgs, message.Text(" "))
		}
		msgs = append(msgs, message.Text(rsp.Text))
	}

	ctx.Send(msgs)
}

func (p *Persona) sendRequest(builder *promptBuilder) (*ChatJson, error) {
	req := &model.Request{
		Question: builder.Build(),
	}
	rsp := &model.Response{}
	err := p.llm.Request(req, rsp)
	if err != nil {
		return nil, err
	}

	res := rsp.Answer

	// 容错：AI可能在JSON外面加```json```
	res = strings.TrimSpace(res)
	res = strings.TrimPrefix(res, "```json")
	res = strings.TrimPrefix(res, "```")
	res = strings.TrimSuffix(res, "```")

	var cj ChatJson
	err = json.Unmarshal([]byte(res), &cj)
	return &cj, err
}
