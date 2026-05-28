package chatai

import (
	"errors"
	"github.com/kohmebot/chatai/chatai/favor"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/model/factory"
	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/plugin/v2"
	"github.com/wdvxdr1123/ZeroBot"
	"slices"
)

type ChatPlugin struct {
	conf Config
	env  plugin.Env

	joinGroupModel model.LargeModel
	otherModel     model.LargeModel

	personaMap map[int64]*persona.Persona
}

func NewPlugin() plugin.Plugin {
	return &ChatPlugin{
		personaMap: make(map[int64]*persona.Persona),
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

	if c.conf.Threshold == 0 {
		//默认为50
		c.conf.Threshold = 50
	}

	c.personaMap = make(map[int64]*persona.Persona)
	for group := range env.Groups().RangeGroup() {
		p := persona.NewPersona(group, c.env, db, c.conf.Threshold, factory.NewLargeModel(model.Config{
			Name:         c.conf.ModelName,
			ApiKey:       c.conf.ApiKey,
			System:       c.conf.System,
			Online:       c.conf.Online,
			MaxTokens:    c.conf.MaxTokens,
			Thinking:     c.conf.Thinking,
			ResponseJson: true,
		}))
		if slices.Contains(c.conf.SpeakGroups, group) {
			p.SetAutoSpeak()
		}

		c.personaMap[group] = p
	}

	c.joinGroupModel = factory.NewLargeModel(model.Config{
		Name:         c.conf.ModelName,
		ApiKey:       c.conf.ApiKey,
		System:       c.conf.System,
		Online:       false,
		MaxTokens:    c.conf.MaxTokens,
		Thinking:     c.conf.Thinking,
		ResponseJson: false,
	})

	c.otherModel = factory.NewLargeModel(model.Config{
		Name:         c.conf.ModelName,
		ApiKey:       c.conf.ApiKey,
		System:       c.conf.System,
		Online:       false,
		MaxTokens:    c.conf.MaxTokens,
		Thinking:     c.conf.Thinking,
		ResponseJson: false,
	})

	c.SetOnMessage(engine)
	c.SetOnJoinGroup(engine)

	return nil

}

func (c *ChatPlugin) OnBoot() {

}

func (c *ChatPlugin) OnHelp(ctx *zero.Ctx) {

}

func (c *ChatPlugin) Name() string {
	return "chatai"
}

func (c *ChatPlugin) Version() string {
	return "v0.4.4"
}
