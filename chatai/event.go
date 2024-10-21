package chatai

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin/pkg/chain"
	"github.com/kohmebot/plugin/pkg/gopool"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
	"strings"
)

func (c *chatPlugin) SetOnAt(engine *zero.Engine) {
	engine.OnMessage(c.env.Groups().Rule()).Handle(func(ctx *zero.Ctx) {
		// 只处理at消息
		if !ctx.Event.IsToMe {
			return
		}
		var err error
		defer func() {
			if err != nil {
				c.env.Error(ctx, err)
			}
		}()
		db, err := c.env.GetDB()
		if err != nil {
			return
		}
		record := UsageRecord{
			UserId: ctx.Event.Sender.ID,
		}
		allow, err := record.Allow(db, c.conf.InputToken, c.conf.OutputToken)
		if err != nil {
			return
		}
		if !allow {
			gopool.Go(func() {
				var msgChain chain.MessageChain
				msgChain.Join(message.Reply(ctx.Event.MessageID))
				msgChain.Join(message.At(ctx.Event.Sender.ID))
				msgChain.Join(message.Text(c.conf.LimitTips))
				ctx.Send(msgChain)
			})
		}

		var texts []string
		for _, segment := range ctx.Event.Message {
			if segment.Type != "text" {
				continue
			}
			val := segment.Data["text"]
			val = strings.TrimSpace(val)
			if len(val) <= 0 {
				continue
			}
			texts = append(texts, val)
		}
		if len(texts) <= 0 {
			return
		}
		c.batch.Submit(ctx, model.Key{
			GroupId: ctx.Event.GroupID,
			UserId:  ctx.Event.Sender.ID,
		}, texts)
	})
}

func (c *chatPlugin) SetOnWarmup(engine *zero.Engine) {
	if !c.conf.WarmGroupConfig.Enable {
		return
	}
	engine.OnMessage().Handle(func(ctx *zero.Ctx) {
		group := ctx.Event.GroupID
		if group >= 0 {
			c.gTicker.Update(group)
		}
	})
}

func (c *chatPlugin) SetOnJoinGroup(engine *zero.Engine) {
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
			info := ctx.GetThisGroupMemberInfo(ctx.Event.UserID, false)

			nickName, ok := info.Map()["nickname"]
			if !ok {
				err = fmt.Errorf("error fetch member info")
				return
			}
			req := &model.Request{
				Question: fmt.Sprintf(c.conf.JoinGroupConfig.Trigger, nickName),
			}
			res := &model.Response{}
			err = c.warmUpModel.Request(req, res)
			if err != nil {
				return
			}

			var msgChain chain.MessageChain
			msgChain.Line(message.At(ctx.Event.UserID))
			msgChain.Join(message.Text(res.Answer))

			ctx.Send(msgChain)
		})

	})
}

func (c *chatPlugin) onResponse(ctx *zero.Ctx, request *model.Request, response *model.Response, err error) {
	defer func() {
		if err != nil {
			c.env.Error(ctx, err)
		}
	}()
	if err != nil {
		return
	}

	if len(response.ErrorMsg) > 0 {
		var msgChain chain.MessageChain
		msgChain.Join(message.Reply(ctx.Event.MessageID))
		msgChain.Split(message.At(ctx.Event.Sender.ID), message.Text(c.conf.ErrorTips))
		ctx.Send(msgChain)
		return
	}

	db, err := c.env.GetDB()
	if err != nil {
		return
	}

	// 更新使用量
	usage := &UsageRecord{
		UserId: ctx.Event.Sender.ID,
	}
	// 批量更新不触发钩子
	err = db.Model(&usage).UpdateColumns(map[string]interface{}{
		"use_input_token":  gorm.Expr("use_input_token + ?", response.InputToken),
		"use_output_token": gorm.Expr("use_output_token + ?", response.OutToken),
	}).Error
	if err != nil {
		return
	}

	var msgChain chain.MessageChain
	msgChain.Join(message.Reply(ctx.Event.MessageID))
	msgChain.Split(message.At(ctx.Event.Sender.ID), message.Text(response.Answer))
	ctx.Send(msgChain)
}

func (c *chatPlugin) onWarmup(groupId int64) {
	req := &model.Request{
		Question: fmt.Sprintf(c.conf.WarmGroupConfig.Trigger, c.conf.WarmGroupConfig.Duration),
	}
	res := &model.Response{}
	err := c.warmUpModel.Request(req, res)
	if err != nil {
		c.env.RangeBot(func(ctx *zero.Ctx) bool {
			c.env.Error(ctx, err)
			return true
		})
		return
	}
	c.env.RangeBot(func(ctx *zero.Ctx) bool {
		ctx.SendGroupMessage(groupId, message.Text(res.Answer))
		return true
	})

}
