package chataisdk

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin/v2"
	"reflect"
)

type ChatAIInvoker struct {
	v reflect.Value
}

func NewChatAIInvoker(env plugin.Env) (*ChatAIInvoker, error) {
	p, ok := env.GetPlugin("chatai")
	if !ok {
		return nil, fmt.Errorf("chatai plugin not found")
	}
	return &ChatAIInvoker{
		v: reflect.ValueOf(p),
	}, nil
}

func (c *ChatAIInvoker) DoRequest(req string) (string, error) {
	res := c.v.MethodByName("DoRequest").Call([]reflect.Value{reflect.ValueOf(req)})
	if len(res) != 2 {
		return "", fmt.Errorf("DoRequest method not found")
	}
	resp := res[0].Interface().(string)
	if res[1].IsNil() {
		return resp, nil
	}
	return resp, res[1].Interface().(error)
}

func (c *ChatAIInvoker) NewModel(system string, online bool, thinking bool, responseJson bool) (model.LargeModel, error) {
	res := c.v.MethodByName("NewModel").Call([]reflect.Value{reflect.ValueOf(system), reflect.ValueOf(online), reflect.ValueOf(thinking), reflect.ValueOf(responseJson)})
	if len(res) != 1 {
		return nil, fmt.Errorf("NewModel method not found")
	}
	return res[0].Interface().(model.LargeModel), nil
}

func (c *ChatAIInvoker) NewDefaultModel(online bool, thinking bool, responseJson bool) (model.LargeModel, error) {
	res := c.v.MethodByName("NewDefaultModel").Call([]reflect.Value{reflect.ValueOf(online), reflect.ValueOf(thinking), reflect.ValueOf(responseJson)})
	if len(res) != 1 {
		return nil, fmt.Errorf("NewDefaultModel method not found")
	}
	return res[0].Interface().(model.LargeModel), nil
}

func (c *ChatAIInvoker) DoRequestWithModel(req string, m model.LargeModel) (string, error) {
	res := c.v.MethodByName("DoRequestWithModel").Call([]reflect.Value{reflect.ValueOf(req), reflect.ValueOf(m)})
	if len(res) != 2 {
		return "", fmt.Errorf("DoRequestWithModel method not found")
	}
	resp := res[0].Interface().(string)
	if res[1].IsNil() {
		return resp, nil
	}
	return resp, res[1].Interface().(error)
}
