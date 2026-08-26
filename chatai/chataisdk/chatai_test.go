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

type providerPluginStub struct {
	model        model.LargeModel
	providerName string
	modelName    string
	system       string
	online       bool
	thinking     bool
	responseJSON bool
}

func (p *providerPluginStub) NewProviderModel(providerName, modelName, system string, online, thinking, responseJSON bool) model.LargeModel {
	p.providerName = providerName
	p.modelName = modelName
	p.system = system
	p.online = online
	p.thinking = thinking
	p.responseJSON = responseJSON
	return p.model
}

func TestInvokerNewProviderModel(t *testing.T) {
	want := stubModel{}
	pluginStub := &providerPluginStub{model: want}
	invoker := &ChatAIInvoker{v: reflect.ValueOf(pluginStub)}

	got, err := invoker.NewProviderModel("tongyi", "qwen-vl-max", "provider system", true, true, true)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, "tongyi", pluginStub.providerName)
	assert.Equal(t, "qwen-vl-max", pluginStub.modelName)
	assert.Equal(t, "provider system", pluginStub.system)
	assert.True(t, pluginStub.online)
	assert.True(t, pluginStub.thinking)
	assert.True(t, pluginStub.responseJSON)
}

func TestInvokerNewProviderModelRequiresNames(t *testing.T) {
	invoker := &ChatAIInvoker{v: reflect.ValueOf(&providerPluginStub{})}

	got, err := invoker.NewProviderModel("", "", "", false, false, false)

	assert.Nil(t, got)
	assert.EqualError(t, err, "provider name and model name are required")
}

func TestInvokerNewProviderModelMethodNotFound(t *testing.T) {
	invoker := &ChatAIInvoker{v: reflect.ValueOf(struct{}{})}

	got, err := invoker.NewProviderModel("tongyi", "qwen-vl-max", "", false, false, false)

	assert.Nil(t, got)
	assert.EqualError(t, err, "NewProviderModel method not found")
}
