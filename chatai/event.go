package chatai

import (
	"chatai/chatai/model"
	"github.com/kohmebot/plugin/pkg/chain"
	"github.com/kohmebot/plugin/pkg/gopool"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/gorm"
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
			GroupId: ctx.Event.GroupID,
			UserId:  ctx.Event.Sender.ID,
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
			texts = append(texts, segment.Data["text"])
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

func (c *chatPlugin) onResponse(ctx *zero.Ctx, request *model.Request, response *model.Response, err error) {
	defer func() {
		if err != nil {
			c.env.Error(ctx, err)
		}
	}()
	if err != nil {
		return
	}
	db, err := c.env.GetDB()
	if err != nil {
		return
	}

	// 更新使用量
	usage := &UsageRecord{
		GroupId: ctx.Event.GroupID,
		UserId:  ctx.Event.Sender.ID,
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
