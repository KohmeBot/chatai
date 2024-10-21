package chatai

import (
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/model/tongyi"
	"github.com/kohmebot/plugin"
	"github.com/kohmebot/plugin/pkg/command"
	"github.com/kohmebot/plugin/pkg/version"
	"github.com/wdvxdr1123/ZeroBot"
	"time"
)

type chatPlugin struct {
	conf           Config
	env            plugin.Env
	batch          *model.Batch
	gTicker        *GroupTicker
	warmUpModel    model.LargeModel
	joinGroupModel model.LargeModel
}

func NewPlugin() plugin.Plugin {
	return &chatPlugin{}
}

func (c *chatPlugin) Init(engine *zero.Engine, env plugin.Env) error {
	c.env = env
	err := env.GetConf(&c.conf)
	if err != nil {
		return err
	}

	db, err := env.GetDB()
	if err != nil {
		return err
	}
	err = db.AutoMigrate(&UsageRecord{})
	if err != nil {
		return err
	}
	m := tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.Prompt, c.conf.Online)
	c.batch = model.NewBatch(m, c.onResponse)
	c.warmUpModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.WarmGroupConfig.Prompt, false)
	c.joinGroupModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.JoinGroupConfig.Prompt, false)
	c.SetOnAt(engine)
	c.SetOnJoinGroup(engine)
	c.SetOnWarmup(engine)

	groups := c.conf.WarmGroupConfig.Groups
	if len(groups) <= 0 {
		c.env.Groups().RangeGroup(func(group int64) bool {
			groups = append(groups, group)
			return true
		})
	}
	c.gTicker = NewGroupTicker(groups, time.Duration(c.conf.WarmGroupConfig.Duration)*time.Minute, c.onWarmup)
	return nil

}

func (c *chatPlugin) Name() string {
	return "chatai"
}

func (c *chatPlugin) Description() string {
	return "@我和我聊天吧!"
}

func (c *chatPlugin) Commands() command.Commands {
	return command.NewCommands()
}

func (c *chatPlugin) Version() version.Version {
	return version.NewVersion(0, 0, 10)
}
