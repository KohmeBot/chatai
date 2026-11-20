package agent

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
	require.Empty(t, r.skillToolNames("demo", ToolScopeGroup))
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

func TestToolPreferencesMigrateAndShareAcrossRegistries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tools.db")), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	require.NoError(t, db.AutoMigrate(&ToolPreference{}))
	require.NoError(t, db.Create(&[]ToolPreference{
		{GroupID: 10, Name: "demo"},
		{GroupID: 20, Name: "demo"},
		{Name: "other"},
	}).Error)
	preferences, err := LoadToolPreferences(db)
	require.NoError(t, err)
	var history []ToolPreference
	require.NoError(t, db.Order("name").Find(&history).Error)
	require.Equal(t, []ToolPreference{{Name: "demo"}, {Name: "other"}}, history)

	tool := Tool{
		Definition: Function("demo", "demo", map[string]any{}),
		Handler:    func(*RunContext, json.RawMessage) (any, error) { return nil, nil },
	}
	catalog := NewRegistryWithToolPreferences(preferences)
	require.NoError(t, catalog.Register(tool))
	group := NewRegistryWithToolPreferences(preferences)
	require.NoError(t, group.Register(tool))
	require.False(t, group.Has("demo"))
	require.Empty(t, group.Search("demo", 5))
	require.Equal(t, true, catalog.Catalog()[1]["disabled"])

	require.NoError(t, catalog.SetDisabled("demo", false, nil))
	require.True(t, group.Has("demo"))
	private := NewRegistryWithToolPreferences(preferences)
	require.NoError(t, private.Register(tool))
	require.Len(t, private.Search("demo", 5), 1)
	require.NoError(t, private.SetDisabled("demo", true, nil))
	require.False(t, group.Has("demo"))
	require.Empty(t, group.skillToolNames("demo", ToolScopeGroup))
	require.Equal(t, true, catalog.Catalog()[1]["disabled"])

	require.NoError(t, catalog.SetDisabled("demo", false, nil))
	reloaded, err := LoadToolPreferences(db)
	require.NoError(t, err)
	restarted := NewRegistryWithToolPreferences(reloaded)
	require.NoError(t, restarted.Register(tool))
	require.True(t, restarted.Has("demo"))
	require.NoError(t, db.Order("name").Find(&history).Error)
	require.Equal(t, []ToolPreference{{Name: "other"}}, history)
}
