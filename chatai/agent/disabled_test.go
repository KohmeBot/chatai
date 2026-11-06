package agent

import (
	"encoding/json"
	"errors"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDisabledToolBlocksDiscoveryAndExecution(t *testing.T) {
	r := NewRegistry()
	called := false
	require.NoError(t, r.Register(Tool{Definition: Function("demo", "demo", map[string]any{}), GroupAction: true, Handler: func(*RunContext, json.RawMessage) (any, error) { called = true; return nil, nil }}))
	require.Error(t, r.SetDisabled("demo", true, func() error { return errors.New("save failed") }))
	require.True(t, r.Has("demo"))
	require.NoError(t, r.SetDisabled("demo", true, nil))
	require.False(t, r.Has("demo"))
	require.Empty(t, r.Names())
	require.Empty(t, r.Search("demo", 5))
	require.Empty(t, r.GroupActionDefinitions())
	require.Empty(t, r.skillToolNames("demo"))
	require.Len(t, r.Definitions(map[string]bool{"demo": true}), 1)
	var call model.ToolCall
	call.Function.Name = "demo"
	call.Function.Arguments = "{}"
	_, err := r.execute(nil, call)
	require.Error(t, err)
	require.False(t, called)
	require.Equal(t, true, r.Catalog()[1]["disabled"])
	require.NoError(t, r.SetDisabled("demo", false, nil))
	_, err = r.execute(nil, call)
	require.NoError(t, err)
	require.True(t, called)
	require.Error(t, r.SetDisabled("search_tools", true, nil))
}
