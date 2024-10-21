package main

import (
	"github.com/kohmebot/chatai/chatai"
	"github.com/kohmebot/plugin"
)

func NewPlugin() plugin.Plugin {
	return chatai.NewPlugin()
}
