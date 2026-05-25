package factory

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/model/deepseek"
	"github.com/kohmebot/chatai/chatai/model/tongyi"
	"strings"
)

type NewModelFunc = func(model.Config) model.LargeModel

var modelFn = map[string]NewModelFunc{
	"deepseek": deepseek.NewDeepSeekModel,
	"tongyi":   tongyi.NewTongYiModel,
}

func NewLargeModel(conf model.Config) model.LargeModel {
	prefix := strings.Split(conf.Name, ":")[0]
	if _, ok := modelFn[prefix]; !ok {
		panic(fmt.Sprintf("model %s not exists", prefix))
	}
	conf.Name = strings.TrimPrefix(conf.Name, prefix+":")
	return modelFn[prefix](conf)
}
