package chataisdk

import (
	"github.com/kohmebot/chatai/chatai"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
)

type mockEnv struct {
	plugin.Env
}

func (m *mockEnv) GetPlugin(name string) (p plugin.Plugin, ok bool) {
	return &chatai.ChatPlugin{}, true
}
func TestInvoker_NewBatch(t *testing.T) {
	_, err := NewChatAIInvoker(&mockEnv{})
	assert.NoError(t, err)

}

type stubModel struct{}

func (stubModel) Request(*model.Request, *model.Response) error { return nil }

type visionPluginStub struct {
	model        model.LargeModel
	system       string
	online       bool
	thinking     bool
	responseJSON bool
}

func (p *visionPluginStub) NewVisionModel(system string, online, thinking, responseJSON bool) model.LargeModel {
	p.system = system
	p.online = online
	p.thinking = thinking
	p.responseJSON = responseJSON
	return p.model
}

func TestInvokerNewVisionModel(t *testing.T) {
	want := stubModel{}
	pluginStub := &visionPluginStub{model: want}
	invoker := &ChatAIInvoker{v: reflect.ValueOf(pluginStub)}

	got, err := invoker.NewVisionModel("vision system", true, true, true)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, "vision system", pluginStub.system)
	assert.True(t, pluginStub.online)
	assert.True(t, pluginStub.thinking)
	assert.True(t, pluginStub.responseJSON)
}

func TestInvokerNewVisionModelNotConfigured(t *testing.T) {
	invoker := &ChatAIInvoker{v: reflect.ValueOf(&visionPluginStub{})}

	got, err := invoker.NewVisionModel("", false, false, false)

	assert.Nil(t, got)
	assert.EqualError(t, err, "vision model is not configured")
}

func TestInvokerNewVisionModelMethodNotFound(t *testing.T) {
	invoker := &ChatAIInvoker{v: reflect.ValueOf(struct{}{})}

	got, err := invoker.NewVisionModel("", false, false, false)

	assert.Nil(t, got)
	assert.EqualError(t, err, "NewVisionModel method not found")
}
