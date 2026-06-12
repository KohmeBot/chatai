package factory

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/model/deepseek"
	"github.com/kohmebot/chatai/chatai/model/tongyi"
	"strings"
	"time"
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

	return &modelWithUsage{
		m:    modelFn[prefix](conf),
		conf: conf,
	}

}

type modelWithUsage struct {
	m    model.LargeModel
	conf model.Config
}

func (m *modelWithUsage) Request(request *model.Request, response *model.Response) error {

	start := time.Now()
	err := m.m.Request(request, response)
	if err != nil {
		return err
	}

	if m.conf.DB == nil {
		return nil
	}

	if response != nil {
		u := &model.TokenUsage{
			InputToken:  response.InputToken,
			OutputToken: response.OutToken,
			ModelName:   m.conf.Name,
			TTL:         time.Since(start),
			CreateAt:    start.UTC(),
		}
		_ = u.Insert(m.conf.DB)
	}

	return nil

}
