package chataisdk

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin"
	"reflect"
)

func NewBatch(env plugin.Env, on model.OnResponse) (model.Batch, error) {
	p, ok := env.GetPlugin("chatai")
	if !ok {
		return model.Batch{}, fmt.Errorf("chatai plugin not found")
	}
	val := reflect.ValueOf(p)
	b := val.MethodByName("NewBatch").Call([]reflect.Value{reflect.ValueOf(on)})[0].Interface().(model.Batch)
	return b, nil
}
