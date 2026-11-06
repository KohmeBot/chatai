package skill

import (
	"context"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

func TestDisabledSkillsPersistAndKeepResidency(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	require.NoError(t, db.AutoMigrate(Models()...))
	d, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("demo", "SKILL.md", "---\nname: demo\ndescription: Demo\n---\nBody", false))
	s := &Service{db: db, Directory: d, opts: Options{Enabled: true}}
	row := Record{Name: "demo", Description: "Demo", Scope: ScopeGlobal, Source: SourceConfig, Status: StatusActive, Markdown: "Body"}
	require.NoError(t, db.Create(&row).Error)
	require.NoError(t, s.SetResident(10, "demo", 0, true))
	require.NoError(t, s.SetResident(10, "", row.ID, true))
	require.NoError(t, s.SetDisabled(10, "demo", 0, true))
	require.NoError(t, s.SetDisabled(10, "", row.ID, true))
	s = &Service{db: db, Directory: d, opts: Options{Enabled: true}}
	require.Error(t, s.CheckEnabled("demo"))
	summaries, err := s.ListSkillSummaries()
	require.NoError(t, err)
	require.Empty(t, summaries)
	matches, err := s.SearchActiveSkills(10, "demo", 5)
	require.NoError(t, err)
	require.Empty(t, matches)
	residents, err := s.ResidentSkills(10)
	require.NoError(t, err)
	require.Empty(t, residents)
	files, err := s.SearchFiles(context.Background(), "", "", "", 10)
	require.NoError(t, err)
	require.Empty(t, files.Items)
	keys, err := s.ResidentKeys()
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.NoError(t, s.SetDisabled(10, "demo", 0, false))
	require.NoError(t, s.SetDisabled(10, "", row.ID, false))
	residents, err = s.ResidentSkills(10)
	require.NoError(t, err)
	require.Len(t, residents, 2)
	require.Error(t, s.SetDisabled(10, "missing", 0, true))
	require.Error(t, s.SetDisabled(10, "demo", row.ID, true))
}
