package chataisdk

import (
	"fmt"
	"github.com/kohmebot/plugin"
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
