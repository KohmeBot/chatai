package persona

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAdminMemoryEditsPreserveHiddenItemsAndRejectCreation(t *testing.T) {
	db := memoryTestDB(t)
	p := memoryPersona()
	p.db = db
	require.NoError(t, writeUserMemory(db, UserProfile{UserID: 1, Summary: "summary", Items: []MemoryItem{{Key: "shared", Kind: "fact", Text: "old", GroupID: 20}, {Key: "hidden", Kind: "experience", Text: "private", GroupID: 20}}}))
	edit := MemoryEdit{Target: "item", Version: 1, Key: "shared", Kind: "fact", Text: "corrected", Confidence: "explicit"}
	require.NoError(t, p.EditMemory(1, edit, false))
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Equal(t, "corrected", profile.Items[0].Text)
	require.Equal(t, int64(20), profile.Items[0].GroupID)
	require.Equal(t, "private", profile.Items[1].Text)
	require.Error(t, p.EditMemory(1, edit, false)) // stale
	edit.Version = 2
	edit.Key = "new"
	require.Error(t, p.EditMemory(1, edit, false))
	edit.Key = "hidden"
	require.Error(t, p.EditMemory(1, edit, true))
	edit.Key = "shared"
	require.NoError(t, p.EditMemory(1, edit, true))
	profile, err = readUserMemory(db, 1)
	require.NoError(t, err)
	require.Len(t, profile.Items, 1)
	require.Equal(t, "hidden", profile.Items[0].Key)
	require.Error(t, p.EditMemory(2, MemoryEdit{Target: "profile", Version: 0}, false))
}

func TestAdminClearKeepsVersionedTombstone(t *testing.T) {
	db := memoryTestDB(t)
	p := memoryPersona()
	p.db = db
	require.NoError(t, writeUserMemory(db, UserProfile{UserID: 1, Summary: "old"}))
	require.NoError(t, db.Create(&ChatMessageRecord{UserID: 1, GroupID: 10}).Error)
	require.NoError(t, p.EditMemory(1, MemoryEdit{Target: "profile", Version: 1}, true))
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Empty(t, profile.Summary)
	require.Empty(t, profile.Items)
	require.Equal(t, uint64(2), profile.Version)
	require.Greater(t, profile.ManualThrough, uint(0))
	require.NoError(t, MigrateUserMemories(db))
	profile, err = readUserMemory(db, 1)
	require.NoError(t, err)
	require.Empty(t, profile.Summary)
}

func TestAdminGroupEditDeleteAndConflict(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&GroupImpression{}))
	p := memoryPersona()
	p.db = db
	require.Error(t, p.EditMemory(0, MemoryEdit{Target: "group", Text: "new"}, false))
	require.NoError(t, db.Create(&GroupImpression{GroupID: 10, Content: "before"}).Error)
	require.NoError(t, db.Create(&GroupImpression{GroupID: 20, Content: "other"}).Error)
	require.NoError(t, p.EditMemory(0, MemoryEdit{Target: "group", Previous: "before", Text: "after"}, false))
	require.Error(t, p.EditMemory(0, MemoryEdit{Target: "group", Previous: "before"}, true))
	require.NoError(t, p.EditMemory(0, MemoryEdit{Target: "group", Previous: "after"}, true))
	g, err := new(GroupImpression).Get(db, 10)
	require.NoError(t, err)
	require.Empty(t, g.Content)
	g, err = new(GroupImpression).Get(db, 20)
	require.NoError(t, err)
	require.Equal(t, "other", g.Content)
}
