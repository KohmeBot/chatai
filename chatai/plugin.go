package chatai

import (
	"errors"
	"github.com/kohmebot/chatai/chatai/favor"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/model/tongyi"
	"github.com/kohmebot/pkg/gopool"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
	"github.com/wdvxdr1123/ZeroBot"
)

type ChatPlugin struct {
	conf           Config
	env            plugin.Env
	batch          model.Batch
	batchMp        model.BatchMap
	gTicker        *GroupTicker
	warmUpModel    model.LargeModel
	joinGroupModel model.LargeModel
	pokeModel      model.LargeModel
	onBootModel    model.LargeModel

	otherModel model.LargeModel
}

func NewPlugin() plugin.Plugin {
	return &ChatPlugin{
		batchMp: model.NewBatchMap(),
	}
}

func (c *ChatPlugin) DoRequest(req string) (string, error) {
	request := &model.Request{
		Question: req,
	}
	resp := &model.Response{}
	err := c.otherModel.Request(request, resp)
	if err != nil {
		return "", err
	}
	if len(resp.ErrorMsg) > 0 {
		return "", errors.New(resp.ErrorMsg)
	}
	return resp.Answer, nil

}

func (c *ChatPlugin) OnInit(engine plugin.Engine, env plugin.Env) error {
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
	err = db.AutoMigrate(&favor.FavorRecord{})
	if err != nil {
		return err
	}
	m := tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, favor.Favor{Enable: c.conf.Favor}.WithSystem(c.conf.Prompt), c.conf.Online, c.conf.MaxTokens, c.conf.Thinking, c.conf.Favor)
	c.batch = model.NewBatch(m, c.onResponse)
	for user, prompt := range c.conf.PromptTarget {
		tm := tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, prompt, c.conf.Online, c.conf.MaxTokens, c.conf.Thinking, c.conf.Favor)
		b := model.NewBatch(tm, c.onResponse)
		c.batchMp.SetBatch(user, b)
		logrus.Infof("init prompt %s for %d", prompt, user)
	}
	c.warmUpModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.WarmGroupConfig.Prompt, false, c.conf.MaxTokens, c.conf.Thinking, c.conf.Favor)
	c.joinGroupModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.JoinGroupConfig.Prompt, false, c.conf.MaxTokens, c.conf.Thinking, c.conf.Favor)
	c.pokeModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, favor.Favor{Enable: c.conf.Favor}.WithSystem(c.conf.PokeGroupConfig.Prompt), false, c.conf.MaxTokens, c.conf.Thinking, c.conf.Favor)
	c.onBootModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.OnBootConfig.Prompt, false, c.conf.MaxTokens, c.conf.Thinking, c.conf.Favor)

	c.otherModel = tongyi.NewTongYiModel(c.conf.ModelName, c.conf.ApiKey, c.conf.Prompt, false, c.conf.MaxTokens, c.conf.Thinking, c.conf.Favor)

	c.SetOnAt(engine)
	c.SetOnJoinGroup(engine)
	c.SetOnPoke(engine)
	c.SetOnWarmup(engine)

	return nil

}

func (c *ChatPlugin) OnBoot() {
	gopool.Go(func() {
		c.onBoot()
	})
}

func (c *ChatPlugin) OnHelp(ctx *zero.Ctx) {

}

func (c *ChatPlugin) Name() string {
	return "chatai"
}

func (c *ChatPlugin) Version() string {
	return "v0.2.0-alpha.5"
}
