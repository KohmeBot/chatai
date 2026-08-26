package chataisdk

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/agent"
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

// NewVisionModel creates a model backed by the chatai plugin's routes.vision
// configuration. It returns an error when the plugin is too old to expose the
// method or when no vision route is configured.
func (c *ChatAIInvoker) NewVisionModel(system string, online bool, thinking bool, responseJson bool) (model.LargeModel, error) {
	method := c.v.MethodByName("NewVisionModel")
	if !method.IsValid() {
		return nil, fmt.Errorf("NewVisionModel method not found")
	}
	res := method.Call([]reflect.Value{reflect.ValueOf(system), reflect.ValueOf(online), reflect.ValueOf(thinking), reflect.ValueOf(responseJson)})
	if len(res) != 1 {
		return nil, fmt.Errorf("NewVisionModel returned an invalid result")
	}
	if (res[0].Kind() == reflect.Interface || res[0].Kind() == reflect.Ptr) && res[0].IsNil() {
		return nil, fmt.Errorf("vision model is not configured")
	}
	visionModel, ok := res[0].Interface().(model.LargeModel)
	if !ok {
		return nil, fmt.Errorf("NewVisionModel returned an invalid model")
	}
	return visionModel, nil
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

func (c *ChatAIInvoker) RegisterAgentTool(tool agent.Tool) error {
	res := c.v.MethodByName("RegisterAgentTool").Call([]reflect.Value{reflect.ValueOf(tool)})
	if len(res) != 1 {
		return fmt.Errorf("RegisterAgentTool method not found")
	}
	return res[0].Interface().(error)
}
