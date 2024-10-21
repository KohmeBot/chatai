package main

import (
	"chatai/chatai"
	"github.com/kohmebot/plugin"
)

func NewPlugin() plugin.Plugin {
	return chatai.NewPlugin()
}
