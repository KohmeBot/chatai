package chatai

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/pkg/gopool"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/extension"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func (c *ChatPlugin) SetOnMessage(engine plugin.Engine) {
	engine.OnMessage(c.env.Groups().Rule(), func(ctx *zero.Ctx) bool {
		return !zero.OnlyPrivate(ctx)
	}).Handle(func(ctx *zero.Ctx) {
		group := ctx.Event.GroupID
		p := c.personaMap[group]
		if p == nil {
			return
		}
		err := p.UpdateContext(ctx)
		if err != nil {
			c.env.Error(ctx, err)
			return
		}
	})

	engine.OnNotice(c.env.Groups().Rule()).Handle(func(ctx *zero.Ctx) {
		if ctx.Event.SubType != persona.MsgTypePoke {
			return
		}
		if ctx.Event.UserID == ctx.Event.SelfID {
			// 发送者是自己就不用记录了
			return
		}
		group := ctx.Event.GroupID
		p := c.personaMap[group]
		if p == nil {
			return
		}
		err := p.UpdateContext(ctx)
		if err != nil {
			c.env.Error(ctx, err)
			return
		}
	})
}

func (c *ChatPlugin) setOnPrivateMessage(engine plugin.Engine, opts persona.Options) func(persona.PrivateSession) *persona.Persona {
	var mu sync.Mutex
	sessions := make(map[persona.PrivateSession]*persona.Persona)
	lookup := func(session persona.PrivateSession) *persona.Persona {
		mu.Lock()
		defer mu.Unlock()
		p := sessions[session]
		if p == nil {
			p = persona.NewPrivatePersona(session, c.env, c.db, opts)
			sessions[session] = p
		}
		return p
	}
	allowed := func(ctx *zero.Ctx) bool {
		return c.conf.PrivateChat.allows(ctx.Event.SelfID, ctx.Event.UserID)
	}
	// 默认优先级的命令先处理，拦截的命令不会再触发私聊 Agent。
	engine.OnMessage(zero.OnlyPrivate, allowed).SecondPriority().Handle(func(ctx *zero.Ctx) {
		session := persona.PrivateSession{BotID: ctx.Event.SelfID, UserID: ctx.Event.UserID}
		p := lookup(session)
		if err := p.UpdateContext(ctx); err != nil {
			logrus.Errorf("[ChatAI][私聊][bot=%d user=%d] %v", session.BotID, session.UserID, err)
			ctx.SendPrivateMessage(session.UserID, message.Message{message.Text("这次处理失败了，请稍后再试。")})
		}
	})
	return lookup
}

func (c *ChatPlugin) SetOnJoinGroup(engine plugin.Engine) {
	if !c.conf.JoinGroupConfig.Enable {
		return
	}
	engine.OnNotice(c.env.Groups().Rule()).Handle(func(ctx *zero.Ctx) {
		if ctx.Event.NoticeType != "group_increase" {
			return
		}
		gopool.Go(func() {
			var err error
			defer func() {
				if err != nil {
					c.env.Error(ctx, err)
				}
			}()

			nickName := ctx.CardOrNickName(ctx.Event.UserID)

			req := &model.Request{
				Question: fmt.Sprintf(string(c.conf.JoinGroupConfig.Trigger), nickName),
			}
			res := &model.Response{}
			err = c.joinGroupModel.Request(req, res)
			if err != nil {
				return
			}
			if len(res.ErrorMsg) > 0 {
				logrus.Warn(res.ErrorMsg)
				return
			}

			err = c.sendHostMessage(ctx, message.Message{message.At(ctx.Event.UserID), message.Text(" " + res.Answer)})
		})

	})
}

func (c *ChatPlugin) SetOnUsage(engine plugin.Engine) {
	engine.OnCommand("usage", c.env.SuperUser().Rule()).Handle(func(ctx *zero.Ctx) {
		var cmdArgs extension.CommandModel
		var err error

		defer func() {
			if err != nil {
				c.env.Error(ctx, err)
			}
		}()

		err = ctx.Parse(&cmdArgs)
		if err != nil {

			return
		}

		day := 1
		if cmdArgs.Args != "" {
			day, err = strconv.Atoi(cmdArgs.Args)
			if err != nil {
				return
			}
		}

		now := time.Now()
		start := time.Date(
			now.Year(),
			now.Month(),
			now.Day()-(day-1),
			0, 0, 0, 0,
			time.Local,
		)
		end := time.Date(
			now.Year(),
			now.Month(),
			now.Day()+1,
			0, 0, 0, 0,
			time.Local,
		)

		var usages []model.TokenUsage
		err = c.db.Where("created_at >= ? AND created_at < ?", start.UTC(), end.UTC()).Find(&usages).Error
		if err != nil {
			return
		}

		modelMap := map[string][]model.TokenUsage{}
		for _, usage := range usages {
			modelMap[usage.ModelName] = append(modelMap[usage.ModelName], usage)
		}

		if len(modelMap) == 0 {
			err = c.sendHostMessage(ctx, message.Message{message.Text("没有找到任何记录")})
			return
		}
		for name, usages := range modelMap {
			var (
				totalInput  int64
				totalOutput int64
				totalTTL    time.Duration
			)

			for _, u := range usages {
				totalInput += u.InputToken
				totalOutput += u.OutputToken
				totalTTL += u.TTL
			}
			avgTTL := totalTTL / time.Duration(len(usages))

			var builder strings.Builder
			s, e := start.Format("01月02日"), end.Add(-time.Second).Format("01月02日")
			if s == e {
				builder.WriteString(fmt.Sprintf("%s\n", s))
			} else {
				builder.WriteString(fmt.Sprintf("%s - %s\n", s, e))
			}

			builder.WriteString(fmt.Sprintf("模型名称: %s\n", name))
			builder.WriteString(fmt.Sprintf("输入Token(K): %.4f\n", float64(totalInput)/1000))
			builder.WriteString(fmt.Sprintf("输出Token(K): %.4f\n", float64(totalOutput)/1000))
			builder.WriteString(fmt.Sprintf("平均调用时长: %s\n", avgTTL.String()))
			err = c.sendHostMessage(ctx, message.Message{message.Text(builder.String())})
			if err != nil {
				return
			}
			time.Sleep(time.Second)
		}

	}).SetBlock(true)
}

func (c *ChatPlugin) sendHostMessage(ctx *zero.Ctx, segments message.Message) error {
	isPrivate := zero.OnlyPrivate(ctx)
	if p := c.personaMap[ctx.Event.GroupID]; !isPrivate && p != nil {
		return p.SendHostMessage(ctx, segments)
	}
	// Without a group Agent, retry the same administrative reply directly.
	for attempt := 1; attempt <= agent.MaxDeliveryAttempts; attempt++ {
		var messageID int64
		if isPrivate {
			messageID = ctx.SendPrivateMessage(ctx.Event.UserID, segments)
		} else {
			messageID = ctx.Send(segments).ID()
		}
		if messageID != 0 {
			return nil
		}
		logrus.Warnf("[发送失败] message_id=0 attempt=%d/%d", attempt, agent.MaxDeliveryAttempts)
	}
	logrus.Error("[发送失败] 三次发送均返回 message_id=0")
	return agent.ErrMessageDeliveryFailed
}
