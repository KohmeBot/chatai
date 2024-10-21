package chatai

import (
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/model/tongyi"
	"github.com/kohmebot/plugin"
	"github.com/kohmebot/plugin/pkg/command"
	"github.com/kohmebot/plugin/pkg/gopool"
	"github.com/kohmebot/plugin/pkg/version"
	"github.com/wdvxdr1123/ZeroBot"
)

type chatPlugin struct {
	conf           Config
	env            plugin.Env
	batch          *model.Batch
	gTicker        *GroupTicker
	warmUpModel    model.LargeModel
	joinGroupModel model.LargeModel
	onBootModel    model.LargeModel
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
	m := tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.Prompt, c.conf.Online, c.conf.MaxTokens)
	c.batch = model.NewBatch(m, c.onResponse)
	c.warmUpModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.WarmGroupConfig.Prompt, false, c.conf.MaxTokens)
	c.joinGroupModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.JoinGroupConfig.Prompt, false, c.conf.MaxTokens)
	c.onBootModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.OnBootConfig.Prompt, false, c.conf.MaxTokens)
	c.SetOnAt(engine)
	c.SetOnJoinGroup(engine)
	c.SetOnWarmup(engine)

	return nil

}

func (c *chatPlugin) OnBoot() {
	gopool.Go(func() {
		c.onBoot()
	})
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
	return version.NewVersion(0, 0, 15)
}
