package persona

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAdminAddMemoryItem(t *testing.T) {
	db := memoryTestDB(t)
	p := memoryPersona()
	p.db = db
	require.NoError(t, writeUserMemory(db, UserProfile{UserID: 1, Summary: "unchanged", Items: []MemoryItem{{Key: "hidden", Kind: "experience", Text: "private", GroupID: 20}}}))
	edit := MemoryEdit{Target: "item_create", Version: 1, Key: "new", Kind: "state", Text: "busy", Confidence: "explicit"}
	require.Error(t, p.EditMemory(1, edit, false)) // State needs expiry.
	expiry := time.Now().Add(time.Hour)
	edit.ExpiresAt = &expiry
	require.Error(t, p.EditMemory(1, edit, true))
	edit.Key = "hidden"
	require.Error(t, p.EditMemory(1, edit, false)) // Hidden keys cannot be overwritten.
	edit.Key = "new"
	require.NoError(t, p.EditMemory(1, edit, false))
	require.Error(t, p.EditMemory(1, edit, false)) // Stale version.
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Equal(t, "unchanged", profile.Summary)
	require.Len(t, profile.Items, 2)
	require.Equal(t, "private", profile.Items[0].Text)
	require.Equal(t, p.groupID, profile.Items[1].GroupID)
	require.Zero(t, profile.Items[1].SourceID)
	require.False(t, profile.Items[1].ObservedAt.IsZero())
	require.Len(t, visibleUserProfile(profile, 20, time.Now(), 30).Items, 1)
	edit.Version = profile.Version
	require.Error(t, p.EditMemory(1, edit, false)) // Duplicate key.
	edit.Key = "other"
	edit.Kind = "invalid"
	require.Error(t, p.EditMemory(1, edit, false))
	edit.Kind = "fact"
	edit.Confidence = "invalid"
	require.Error(t, p.EditMemory(1, edit, false))
	edit.Confidence = "explicit"
	edit.Text = " "
	require.Error(t, p.EditMemory(1, edit, false))
	edit.Text = "valid"
	expired := time.Now().Add(-time.Hour)
	edit.ExpiresAt = &expired
	require.Error(t, p.EditMemory(1, edit, false))
}

func TestMemorySnapshotOrdersBeforePagination(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&GroupImpression{}))
	p := memoryPersona()
	p.db = db
	base := time.Now().Add(-time.Hour)
	for id := int64(1); id <= 52; id++ {
		require.NoError(t, writeUserMemory(db, UserProfile{UserID: id}))
		require.NoError(t, db.Model(&UserMemory{}).Where("user_id = ?", id).Update("updated_at", base.Add(time.Duration(id/2)*time.Second)).Error)
	}
	result, err := p.MemorySnapshot(0)
	require.NoError(t, err)
	page := result.(map[string]any)
	users := page["users"].([]map[string]any)
	require.Len(t, users, 50)
	require.Equal(t, true, page["has_more"])
	require.Equal(t, "52", users[0]["user_id"])
	require.Equal(t, "50", users[1]["user_id"])
	require.Equal(t, "51", users[2]["user_id"])
	result, err = p.MemorySnapshot(50)
	require.NoError(t, err)
	page = result.(map[string]any)
	users = page["users"].([]map[string]any)
	require.Equal(t, false, page["has_more"])
	require.Len(t, users, 2)
	for i, id := range []int{3, 1} {
		require.Equal(t, strconv.Itoa(id), users[i]["user_id"])
	}
}

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
	require.NoError(t, MigrateGroupMemories(db))
	require.NoError(t, p.EditMemory(0, MemoryEdit{Target: "group_profile", Version: 1, Summary: "after"}, false))
	require.Error(t, p.EditMemory(0, MemoryEdit{Target: "group_profile", Version: 1}, true))
	require.NoError(t, p.EditMemory(0, MemoryEdit{Target: "group_profile", Version: 2}, true))
	g, err := readGroupMemory(db, 10)
	require.NoError(t, err)
	require.Empty(t, g.Summary)
	g, err = readGroupMemory(db, 20)
	require.NoError(t, err)
	require.Equal(t, "other", g.Summary)
}
