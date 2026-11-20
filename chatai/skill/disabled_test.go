package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
	groupMatches, err := s.SearchActiveSkills(10, "demo", 5)
	require.NoError(t, err)
	require.Len(t, groupMatches, 2)
	privateMatches, err := s.SearchActiveSkills(0, "demo", 5)
	require.NoError(t, err)
	require.Equal(t, groupMatches, privateMatches)
	require.NoError(t, s.SetResident(0, "demo", 0, true))
	require.NoError(t, s.SetResident(0, "", row.ID, true))
	require.NoError(t, s.SetDisabled(0, "demo", 0, true))
	require.NoError(t, s.SetDisabled(0, "", row.ID, true))
	s = &Service{db: db, Directory: d, opts: Options{Enabled: true}}
	require.Error(t, s.CheckEnabled("demo"))
	summaries, err := s.ListSkillSummaries()
	require.NoError(t, err)
	require.Empty(t, summaries)
	for _, groupID := range []int64{0, 10, 20} {
		matches, err := s.SearchActiveSkills(groupID, "demo", 5)
		require.NoError(t, err)
		require.Empty(t, matches)
		residents, err := s.ResidentSkills(groupID)
		require.NoError(t, err)
		require.Empty(t, residents)
	}
	files, err := s.SearchFiles(context.Background(), "", "", "", 10)
	require.NoError(t, err)
	require.Empty(t, files.Items)
	keys, err := s.ResidentKeys()
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.NoError(t, s.SetDisabled(10, "demo", 0, false))
	require.NoError(t, s.SetDisabled(10, "", row.ID, false))
	residents, err := s.ResidentSkills(0)
	require.NoError(t, err)
	require.Len(t, residents, 2)
	require.Error(t, s.SetDisabled(10, "missing", 0, true))
	require.Error(t, s.SetDisabled(10, "demo", row.ID, true))
}

func TestReadAssetSharesEnabledSkillsAndConfinesFileAccess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "assets.db")), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	require.NoError(t, db.AutoMigrate(Models()...))
	d, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("demo", "SKILL.md", "---\nname: demo\ndescription: Demo\n---\nBody", false))
	contents := []byte{0x00, 0x90, 0xff}
	assetPath := filepath.Join(d.path, "demo", "image.png")
	require.NoError(t, os.WriteFile(assetPath, contents, 0600))
	s := &Service{db: db, Directory: d, opts: Options{Enabled: true}}
	matches, err := s.SearchFiles(context.Background(), "image.png", "", "file", 10)
	require.NoError(t, err)
	require.Len(t, matches.Items, 1)
	for _, file := range []string{matches.Items[0].Path, matches.Items[0].AbsolutePath} {
		data, err := s.ReadAsset(file, int64(len(contents)))
		require.NoError(t, err)
		require.Equal(t, contents, data)
		_, err = s.ReadAsset(file, int64(len(contents)-1))
		require.ErrorContains(t, err, "2-byte limit")
	}
	outsidePath := filepath.Join(t.TempDir(), "image.png")
	require.NoError(t, os.WriteFile(outsidePath, contents, 0600))
	for _, file := range []string{"demo", "demo/../demo/image.png", "../image.png", outsidePath} {
		_, err := s.ReadAsset(file, 32)
		require.Error(t, err, file)
	}
	require.NoError(t, s.SetDisabled(0, "demo", 0, true))
	for _, file := range []string{matches.Items[0].Path, matches.Items[0].AbsolutePath} {
		_, err := s.ReadAsset(file, 32)
		require.ErrorContains(t, err, "已禁用")
	}
}

func TestReadAssetRejectsSiblingSkillSymlinks(t *testing.T) {
	d, err := NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, d.Save("demo", "SKILL.md", "---\nname: demo\ndescription: Demo\n---\nBody", false))
	require.NoError(t, d.Save("sibling", "SKILL.md", "---\nname: sibling\ndescription: Sibling\n---\nBody", false))
	if err := os.Symlink(filepath.Join(d.path, "sibling"), filepath.Join(d.path, "demo", "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := &Service{Directory: d, opts: Options{Enabled: true}}
	_, err = s.ReadAsset("demo/linked/SKILL.md", 1024)
	require.Error(t, err)
}
