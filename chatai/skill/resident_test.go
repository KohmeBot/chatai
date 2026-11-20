package skill

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResidentPersistenceAndEligibility(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "skills.db")), &gorm.Config{})
	require.NoError(t, err)
	sql, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sql.Close() })
	require.NoError(t, db.AutoMigrate(Models()...))
	directory, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	body := "---\nname: demo\ndescription: Demo\ntools: [search_web]\n---\nResident body"
	require.NoError(t, directory.Save("demo", "SKILL.md", body, false))
	s := &Service{db: db, opts: Options{Enabled: true}, Directory: directory}
	row := Record{Name: "demo", Scope: ScopeGlobal, Source: SourceGenerated, Status: StatusActive, ExpiresAt: time.Now().Add(time.Hour), Markdown: "database body"}
	require.NoError(t, db.Create(&row).Error)
	require.Error(t, s.SetResident(10, "missing", 0, true))
	require.Error(t, s.SetResident(10, "demo", row.ID, true))
	require.NoError(t, s.SetResident(10, "demo", 0, true))
	require.NoError(t, s.SetResident(10, "demo", 0, true))
	require.NoError(t, s.SetResident(10, "", row.ID, true))
	// A new service instance sees saved choices and keeps same-name sources distinct.
	s = &Service{db: db, opts: Options{Enabled: true, GlobalMinGroups: 1}, Directory: directory}
	items, err := s.ResidentSkills(20)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, body, items[0].Markdown)
	require.Equal(t, "database body", items[1].Markdown)
	privateItems, err := s.ResidentSkills(0)
	require.NoError(t, err)
	require.Equal(t, items, privateItems)
	require.NoError(t, db.Model(&row).Update("expires_at", time.Now().Add(-time.Hour)).Error)
	items, err = s.ResidentSkills(20)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Error(t, s.SetResident(10, "", row.ID, true))
	require.NoError(t, db.Model(&row).Updates(map[string]any{"expires_at": time.Now().Add(time.Hour), "scope": ScopeGroup, "group_id": 10}).Error)
	evidence := Evidence{SkillID: row.ID, TechnicalSuccess: true}
	require.NoError(t, db.Create(&evidence).Error)
	require.NoError(t, s.Initialize(nil))
	require.NoError(t, db.First(&row, row.ID).Error)
	require.Equal(t, ScopeGlobal, row.Scope)
	require.Zero(t, row.GroupID)
	require.NoError(t, db.First(&evidence, evidence.ID).Error)
	require.EqualValues(t, 10, evidence.GroupID)
	items, err = s.ResidentSkills(0)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.NoError(t, db.Model(&row).Update("status", StatusDisabled).Error)
	require.NoError(t, s.SetResident(10, "demo", 0, false))
	items, err = s.ResidentSkills(10)
	require.NoError(t, err)
	require.Empty(t, items)
}
