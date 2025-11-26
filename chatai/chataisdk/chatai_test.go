package chataisdk

import (
	"github.com/kohmebot/chatai/chatai"
	"github.com/kohmebot/plugin/v2"
	"github.com/stretchr/testify/assert"
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
