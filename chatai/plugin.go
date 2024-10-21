package chatai

import (
	"chatai/chatai/model"
	"github.com/kohmebot/plugin"
	"github.com/kohmebot/plugin/pkg/command"
	"github.com/kohmebot/plugin/pkg/version"
	"github.com/wdvxdr1123/ZeroBot"
)

type chatPlugin struct {
	conf  Config
	env   plugin.Env
	batch *model.Batch
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

	c.SetOnAt(engine)

	return nil

}

func (c *chatPlugin) Name() string {
	return "chatai"
}

func (c *chatPlugin) Description() string {
	//TODO implement me
	panic("implement me")
}

func (c *chatPlugin) Commands() command.Commands {
	//TODO implement me
	panic("implement me")
}

func (c *chatPlugin) Version() version.Version {
	//TODO implement me
	panic("implement me")
}
