package chatai

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/pkg/chain"
	"github.com/kohmebot/pkg/gopool"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/extension"
	"github.com/wdvxdr1123/ZeroBot/message"
	"strconv"
	"strings"
	"time"
)

func (c *ChatPlugin) SetOnMessage(engine plugin.Engine) {
	engine.OnMessage(c.env.Groups().Rule()).Handle(func(ctx *zero.Ctx) {
		group := ctx.Event.GroupID
		p := c.personaMap[group]
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
		err := p.UpdateContext(ctx)
		if err != nil {
			c.env.Error(ctx, err)
			return
		}
	})
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
				Question: fmt.Sprintf(c.conf.JoinGroupConfig.Trigger, nickName),
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

			var msgChain chain.MessageChain
			msgChain.Join(message.At(ctx.Event.UserID))
			msgChain.Join(message.Text(" " + res.Answer))

			ctx.Send(msgChain)
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
			ctx.Send("没有找到任何记录")
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
			ctx.Send(builder.String())
			time.Sleep(time.Second)
		}

	}).SetBlock(true)
}
