package chatai

import (
	"github.com/kohmebot/plugin"
	"github.com/kohmebot/plugin/pkg/command"
	"github.com/kohmebot/plugin/pkg/version"
	"github.com/wdvxdr1123/ZeroBot"
)

type chatPlugin struct {
}

func NewPlugin() plugin.Plugin {
	return &chatPlugin{}
}

func (c *chatPlugin) Init(engine *zero.Engine, env plugin.Env) error {
	//TODO implement me
	panic("implement me")
}

func (c *chatPlugin) Name() string {
	//TODO implement me
	panic("implement me")
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
