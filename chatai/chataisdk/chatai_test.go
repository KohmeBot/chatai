package chataisdk

import (
	"github.com/kohmebot/chatai/chatai"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin"
	"github.com/stretchr/testify/assert"
	zero "github.com/wdvxdr1123/ZeroBot"
	"testing"
)

type mockEnv struct {
	plugin.Env
}

func (m *mockEnv) GetPlugin(name string) (p plugin.Plugin, ok bool) {
	return &chatai.ChatPlugin{}, true
}
func TestInvoker_NewBatch(t *testing.T) {
	b, err := NewBatch(&mockEnv{}, func(ctx *zero.Ctx, request *model.Request, response *model.Response, err error) {

	})
	assert.NoError(t, err)
	b.GetModel()
}
