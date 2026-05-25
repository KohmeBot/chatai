package chatai

import (
	"errors"
	"github.com/kohmebot/chatai/chatai/favor"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/model/factory"
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

func (c *ChatPlugin) DoRequestWithModel(req string, m model.LargeModel) (string, error) {
	request := &model.Request{
		Question: req,
	}
	resp := &model.Response{}
	err := m.Request(request, resp)
	if err != nil {
		return "", err
	}
	if len(resp.ErrorMsg) > 0 {
		return "", errors.New(resp.ErrorMsg)
	}
	return resp.Answer, nil
}

func (c *ChatPlugin) NewModel(system string, online bool, thinking bool, responseJson bool) model.LargeModel {
	return factory.NewLargeModel(model.Config{
		Name:         c.conf.ModelName,
		ApiKey:       c.conf.ApiKey,
		System:       system,
		Online:       online,
		MaxTokens:    c.conf.MaxTokens,
		Thinking:     thinking,
		ResponseJson: responseJson,
	})
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

	m := factory.NewLargeModel(model.Config{
		Name:         c.conf.ModelName,
		ApiKey:       c.conf.ApiKey,
		System:       favor.Favor{Enable: c.conf.Favor}.WithSystem(c.conf.Prompt),
		Online:       c.conf.Online,
		MaxTokens:    c.conf.MaxTokens,
		Thinking:     c.conf.Thinking,
		ResponseJson: c.conf.Favor,
	})

	c.batch = model.NewBatch(m, c.onResponse)
	for user, prompt := range c.conf.PromptTarget {
		tm := factory.NewLargeModel(model.Config{
			Name:         c.conf.ModelName,
			ApiKey:       c.conf.ApiKey,
			System:       prompt,
			Online:       c.conf.Online,
			MaxTokens:    c.conf.MaxTokens,
			Thinking:     c.conf.Thinking,
			ResponseJson: c.conf.Favor,
		})

		b := model.NewBatch(tm, c.onResponse)
		c.batchMp.SetBatch(user, b)
		logrus.Infof("init prompt %s for %d", prompt, user)
	}

	c.warmUpModel = factory.NewLargeModel(model.Config{
		Name:         c.conf.ModelName,
		ApiKey:       c.conf.ApiKey,
		System:       c.conf.WarmGroupConfig.Prompt,
		Online:       false,
		MaxTokens:    c.conf.MaxTokens,
		Thinking:     c.conf.Thinking,
		ResponseJson: c.conf.Favor,
	})

	c.joinGroupModel = factory.NewLargeModel(model.Config{
		Name:         c.conf.ModelName,
		ApiKey:       c.conf.ApiKey,
		System:       c.conf.JoinGroupConfig.Prompt,
		Online:       false,
		MaxTokens:    c.conf.MaxTokens,
		Thinking:     c.conf.Thinking,
		ResponseJson: c.conf.Favor,
	})

	c.pokeModel = factory.NewLargeModel(model.Config{
		Name:         c.conf.ModelName,
		ApiKey:       c.conf.ApiKey,
		System:       favor.Favor{Enable: c.conf.Favor}.WithSystem(c.conf.PokeGroupConfig.Prompt),
		Online:       false,
		MaxTokens:    c.conf.MaxTokens,
		Thinking:     c.conf.Thinking,
		ResponseJson: c.conf.Favor,
	})

	c.onBootModel = factory.NewLargeModel(model.Config{
		Name:         c.conf.ModelName,
		ApiKey:       c.conf.ApiKey,
		System:       c.conf.OnBootConfig.Prompt,
		Online:       false,
		MaxTokens:    c.conf.MaxTokens,
		Thinking:     c.conf.Thinking,
		ResponseJson: c.conf.Favor,
	})

	c.otherModel = factory.NewLargeModel(model.Config{
		Name:         c.conf.ModelName,
		ApiKey:       c.conf.ApiKey,
		System:       c.conf.Prompt,
		Online:       false,
		MaxTokens:    c.conf.MaxTokens,
		Thinking:     c.conf.Thinking,
		ResponseJson: c.conf.Favor,
	})

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
	return "v0.2.8"
}
