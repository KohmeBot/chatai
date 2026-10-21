package persona

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestInspectShortMemoryExpiryAndScope(t *testing.T) {
	p := memoryPersona()
	p.conversations = map[int64]conversationState{1: {Note: conversationNote{Summary: "current"}, Updated: time.Now()}, 2: {Note: conversationNote{Summary: "expired"}, Updated: time.Now().Add(-time.Hour)}}
	rows := p.ShortMemorySnapshot()
	require.Len(t, rows, 1)
	require.Equal(t, "1", rows[0]["user_id"])
	p.opts.ConversationMemory = false
	require.Empty(t, p.ShortMemorySnapshot())
}

func TestInspectMemoryUsesCanonicalStoreAndVisibility(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&GroupImpression{}))
	expired := time.Now().Add(-time.Hour)
	require.NoError(t, writeUserMemory(db, UserProfile{UserID: 1, Summary: "canonical", Items: []MemoryItem{{Key: "a", Kind: "preference", Text: "shared", GroupID: 20}, {Key: "b", Kind: "state", Text: "other group", GroupID: 20}, {Key: "c", Kind: "state", Text: "expired", GroupID: 10, ExpiresAt: &expired}}}))
	require.NoError(t, db.Create(&GroupImpression{GroupID: 10, Content: "group ten"}).Error)
	p := memoryPersona()
	p.db = db
	value, err := p.MemorySnapshot(0)
	require.NoError(t, err)
	snapshot := value.(map[string]any)
	require.Equal(t, "group ten", snapshot["group_impression"])
	users := snapshot["users"].([]map[string]any)
	require.Len(t, users, 1)
	require.Equal(t, "canonical", users[0]["summary"])
	items := users[0]["items"].([]MemoryItem)
	require.Len(t, items, 1)
	require.Equal(t, "shared", items[0].Text)
}
