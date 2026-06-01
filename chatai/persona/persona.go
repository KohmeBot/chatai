package persona

import (
	"encoding/json"
	"github.com/kohmebot/chatai/chatai/favor"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
	"math/rand/v2"
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

	msg := newMessage(ctx)
	if msg.IsEmpty() {
		return nil
	}

	// 统计过去 120 秒的消息数，用于活跃度计算
	recentCount := p.gc.AppendMsg(msg, 120*time.Second)

	// 如果群内在复读，直接参与复读就好了
	repeat, repeated := p.gc.RepeatThis(msg)
	if repeated {
		return nil
	}
	if repeat {
		p.aiSend(ctx, ctx.Event.Message)
		return nil
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

func (p *Persona) canSendMsg(ctx *zero.Ctx, msg GroupMessage) bool {
	// get_group_shut_list为napcat的私有接口，并不遵循onebot11标准，在非napcat上可能会报错
	data := ctx.CallAction("get_group_shut_list", zero.Params{"group_id": p.groupId}).Data

	// 查看是否被禁言
	var isBan bool
	data.ForEach(func(_, value gjson.Result) bool {
		if value.Get("user_id").Int() == ctx.Event.SelfID {
			isBan = true
			return false
		}
		return true
	})
	if isBan {
		return false
	}

	// 查看是否启动了auto speak
	if !ctx.Event.IsToMe && !p.autoSpeak {
		return false
	}

	// 查看是否poke限流
	if msg.MsgType == MsgTypePoke && ctx.Event.IsToMe {
		if !p.gc.CanPoke(msg.User.UserId) {
			return false
		}
	}

	return true
}

func (p *Persona) speak(ctx *zero.Ctx, msg GroupMessage) error {

	if !p.canSendMsg(ctx, msg) {
		return nil
	}

	msgCtx := p.gc.Context()

	groupImper, err := new(GroupImpression).Get(p.db, p.groupId)
	if err != nil {
		return err
	}

	builder := newPromptBuilder(msgCtx, groupImper)
	if ctx.Event.IsToMe {
		val, err := favor.GetFavor(p.db, msg.User.UserId)
		if err != nil {
			return err
		}
		imper, err := new(UserImpression).Get(p.db, msg.User.UserId)
		if err != nil {
			return err
		}

		builder.WithAtMe(msg, val, msg.MsgType == MsgTypePoke, imper)
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

	if isAtMe {
		_ = favor.UpdateFavor(p.db, msg.User.UserId, rsp.Favor)
	}

	for _, m := range rsp.Messages {
		msgs := make([]message.Segment, 0)
		// 非At消息
		switch {
		case m.ReplayMsg > 0:
			// 有引用回复
			// 获取到对应消息的发送者，需要at
			rMsg := ctx.GetMessage(m.ReplayMsg)
			if len(rMsg.Elements) > 0 {
				msgs = append(msgs, message.Reply(m.ReplayMsg), message.At(rMsg.Sender.ID))
				if p.gc.Refer(m.ReplayMsg) {
					// 如果已引用过，则忽略就好了，容错
					continue
				}
			}
		case m.AtTarget > 0:
			// 有At对象
			msgs = append(msgs, message.At(m.AtTarget))
		}

		if m.PokeTarget > 0 {
			p.aiPoke(ctx, m.PokeTarget)
			p.randSleep()
		}

		if m.Text != "" {
			if len(msgs) > 0 {
				msgs = append(msgs, message.Text(" "))
			}
			msgs = append(msgs, message.Text(m.Text))
		}

		p.aiSend(ctx, msgs)
		p.randSleep()
	}

	// 更新印象
	for _, impression := range rsp.UpdateImpressions {
		_ = new(UserImpression).Update(p.db, impression)
	}
	_ = new(GroupImpression).Update(p.db, GroupImpression{
		GroupID: p.groupId,
		Content: rsp.GroupImpression,
	})

}

func (p *Persona) aiSend(ctx *zero.Ctx, segments message.Message) {
	msgId := ctx.Send(segments).ID()
	targetId := getTargetIDFromMsgs(ctx, segments)
	msg := GroupMessage{
		User:      User{UserId: ctx.Event.SelfID, Nickname: "你"},
		Content:   segments.ExtractPlainText(),
		MsgType:   getMsgType(segments),
		MsgID:     msgId,
		CreatedAt: time.Now(),
		Url:       getUrl(segments),
		FileName:  getFileName(segments),
	}

	if targetId > 0 {
		msg.TargetUser = User{
			UserId:   targetId,
			Nickname: ctx.CardOrNickName(targetId),
		}
	}
	p.gc.AppendMsg(msg, 0)
}

func (p *Persona) aiPoke(ctx *zero.Ctx, targetId int64) {
	// send_poke为napcat的私有接口，并不遵循onebot11标准，在非napcat上可能会报错
	ctx.CallAction("send_poke", zero.Params{"group_id": p.groupId, "user_id": targetId})
	msg := GroupMessage{
		User: User{UserId: ctx.Event.SelfID, Nickname: "你"},
		TargetUser: User{
			UserId:   targetId,
			Nickname: ctx.CardOrNickName(targetId),
		},
		MsgType:   MsgTypePoke,
		CreatedAt: time.Now(),
	}
	p.gc.AppendMsg(msg, 0)
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

func (p *Persona) randSleep() {
	d := time.Duration(rand.IntN(1000)) * time.Millisecond
	d += time.Second
	time.Sleep(d)
}
