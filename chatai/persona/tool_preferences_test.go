package persona

import (
	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

func TestDisabledToolPreferencesPersistPerGroup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tools.db")), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	require.NoError(t, db.AutoMigrate(&agent.ToolPreference{}))
	p := NewPersona(10, nil, db, Options{})
	require.True(t, p.tools.Has("send_message"))
	require.NoError(t, p.SetToolDisabled("send_message", true))
	reloaded := NewPersona(10, nil, db, Options{})
	require.False(t, reloaded.tools.Has("send_message"))
	other := NewPersona(20, nil, db, Options{})
	require.True(t, other.tools.Has("send_message"))
	require.NoError(t, reloaded.SetToolDisabled("send_message", false))
	require.True(t, NewPersona(10, nil, db, Options{}).tools.Has("send_message"))
	require.Error(t, p.SetToolDisabled("missing", true))
	require.Error(t, p.SetToolDisabled("search_tools", true))
}
