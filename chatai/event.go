package chatai

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/pkg/chain"
	"github.com/kohmebot/pkg/gopool"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
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
