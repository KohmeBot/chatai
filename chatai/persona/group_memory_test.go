package persona

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestGroupMigrationAndVersionedClear(t *testing.T) {
	db := memoryTestDB(t)
	legacy := strings.Repeat("群", 1000)
	require.NoError(t, db.Create(&GroupImpression{GroupID: 10, Content: legacy}).Error)
	require.NoError(t, MigrateGroupMemories(db))
	p := &Persona{db: db, groupID: 10}
	original, err := readGroupMemory(db, 10)
	require.NoError(t, err)
	require.Equal(t, legacy, original.Summary)
	require.Len(t, []rune(visibleGroupProfile(original, time.Now(), 8).Summary), 600)
	require.Equal(t, legacy, visibleGroupProfile(original, time.Now(), 30).Summary)
	row := ChatMessageRecord{GroupID: 10, UserID: 1, Content: "old", CreatedAt: time.Now()}
	require.NoError(t, db.Create(&row).Error)
	require.NoError(t, p.EditMemory(0, MemoryEdit{Target: "group_profile", Version: 1}, true))
	require.Error(t, writeGroupMemory(db, original))
	require.NoError(t, MigrateGroupMemories(db))
	current, err := readGroupMemory(db, 10)
	require.NoError(t, err)
	require.Empty(t, current.Summary)
	require.Equal(t, uint64(2), current.Version)
	change := GroupMemoryChange{PreviousVersion: 2, EvidenceIDs: []uint{row.ID}, Reason: "old backlog"}
	_, err = applyGroupMemoryChange(current, change, []ChatMessageRecord{row}, 99, time.Now())
	require.Error(t, err)
	row.ID++
	change.EvidenceIDs = []uint{row.ID}
	_, err = applyGroupMemoryChange(current, change, []ChatMessageRecord{row}, 99, time.Now())
	require.NoError(t, err)
}

func TestGroupEvidenceMergingAndExpiry(t *testing.T) {
	now := time.Now()
	rows := []ChatMessageRecord{{ID: 1, GroupID: 10, UserID: 1, CreatedAt: now}, {ID: 2, GroupID: 20, UserID: 2, CreatedAt: now}, {ID: 3, GroupID: 10, UserID: 99, CreatedAt: now}, {ID: 4, GroupID: 10, UserID: 2, CreatedAt: now}}
	profile := GroupProfile{GroupID: 10}
	change := GroupMemoryChange{EvidenceIDs: []uint{1, 4}, Reason: "两位群成员的消息", Upsert: []MemoryItem{{Key: "event", Kind: "state", Text: "本周活动", SourceID: 4, Confidence: "explicit"}}}
	next, err := applyGroupMemoryChange(profile, change, rows, 99, now)
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.Equal(t, int64(10), next.Items[0].GroupID)
	require.WithinDuration(t, now.Add(14*24*time.Hour), *next.Items[0].ExpiresAt, time.Second)
	require.Empty(t, visibleGroupProfile(next, now.Add(15*24*time.Hour), 30).Items)
	for _, id := range []uint{2, 3, 5} {
		bad := change
		bad.EvidenceIDs = []uint{id}
		_, err = applyGroupMemoryChange(profile, bad, rows, 99, now)
		require.Error(t, err)
	}
	change.Upsert[0].Text = "活动已调整"
	next, err = applyGroupMemoryChange(next, change, rows, 99, now)
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.Equal(t, "活动已调整", next.Items[0].Text)
	change.Upsert = nil
	change.Forget = []string{"event"}
	next, err = applyGroupMemoryChange(next, change, rows, 99, now)
	require.NoError(t, err)
	require.Empty(t, next.Items)
	change.PreviousVersion = 1
	_, err = applyGroupMemoryChange(next, change, rows, 99, now)
	require.Error(t, err)
}

func TestGroupMaintenanceAndAutomaticContext(t *testing.T) {
	db := memoryTestDB(t)
	row := ChatMessageRecord{GroupID: 10, UserID: 1, Content: "群用途是讨论Go", CreatedAt: time.Now()}
	require.NoError(t, db.Create(&row).Error)
	calls := 0
	m := conversationTestModel{request: func(req *model.Request, res *model.Response) error {
		calls++
		var payload map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(req.Question), &payload))
		require.Contains(t, payload, "group_profile")
		require.Contains(t, payload, "group_messages")
		res.Answer = fmt.Sprintf(`{"updates":[],"group_update":{"previous_version":0,"evidence_ids":[%d],"reason":"群用途说明","summary":"Go讨论群","upsert":[{"key":"topic","kind":"fact","text":"讨论Go","source_id":%d,"confidence":"explicit"}]}}`, row.ID, row.ID)
		return nil
	}}
	worker := NewMemoryMaintainer(db, m, time.Minute, 40)
	require.NoError(t, worker.process(context.Background(), 10, 99))
	require.NoError(t, worker.process(context.Background(), 10, 99))
	require.Equal(t, 1, calls)
	p := &Persona{db: db, groupID: 10}
	var event agentEventEnvelope
	require.NoError(t, json.Unmarshal([]byte(p.eventPrompt(GroupMessage{User: User{UserId: 1}}, "", 99)), &event))
	require.Equal(t, "Go讨论群", event.GroupMemory.Summary)
	require.Len(t, event.GroupMemory.Items, 1)
	other, err := readGroupMemory(db, 20)
	require.NoError(t, err)
	require.Empty(t, other.Items)
	require.Empty(t, other.Summary)
	var audit MemoryAudit
	require.NoError(t, db.Where("group_id = ? AND user_id = 0", 10).First(&audit).Error)
	require.Equal(t, uint64(1), audit.Version)
}

func TestGroupConflictRollsBackUserAndCursor(t *testing.T) {
	db := memoryTestDB(t)
	row := ChatMessageRecord{GroupID: 10, UserID: 1, Content: "hello", CreatedAt: time.Now()}
	require.NoError(t, db.Create(&row).Error)
	require.NoError(t, writeGroupMemory(db, GroupProfile{GroupID: 10}))
	m := conversationTestModel{request: func(_ *model.Request, res *model.Response) error {
		require.NoError(t, writeGroupMemory(db, GroupProfile{GroupID: 10, Version: 1, Summary: "人工更新"}))
		res.Answer = fmt.Sprintf(`{"updates":[{"user_id":1,"previous_version":0,"evidence_ids":[%d],"reason":"old","summary":"old"}],"group_update":{"previous_version":1,"evidence_ids":[%d],"reason":"old","summary":"old"}}`, row.ID, row.ID)
		return nil
	}}
	require.Error(t, NewMemoryMaintainer(db, m, time.Minute, 40).process(context.Background(), 10, 99))
	var cursor MemoryCursor
	require.NoError(t, db.First(&cursor, "group_id = ?", 10).Error)
	require.Zero(t, cursor.LastID)
	user, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Zero(t, user.Version)
	group, err := readGroupMemory(db, 10)
	require.NoError(t, err)
	require.Equal(t, "人工更新", group.Summary)
}

func TestGroupAdminItemsAndToolSwitch(t *testing.T) {
	db := memoryTestDB(t)
	p := &Persona{db: db, groupID: 10}
	edit := MemoryEdit{Target: "group_item_create", Key: "topic", Kind: "fact", Text: "群话题", Confidence: "explicit"}
	require.NoError(t, p.EditMemory(0, edit, false))
	edit.Version = 1
	require.Error(t, p.EditMemory(0, edit, false))
	edit.Target = "group_item"
	edit.Text = "已修改"
	require.NoError(t, p.EditMemory(0, edit, false))
	require.Error(t, p.EditMemory(0, edit, true))
	edit.Version = 2
	require.NoError(t, p.EditMemory(0, edit, true))
	group, err := readGroupMemory(db, 10)
	require.NoError(t, err)
	require.Empty(t, group.Items)
	require.Len(t, p.groupMemoryTools(), 1)
	p.opts.ImpressionUpdateEnable = true
	require.Len(t, p.groupMemoryTools(), 2)
	_, err = p.handleUpdateImpression(nil, json.RawMessage(`{"scope":"group","content":"bypass","reason":"x"}`))
	require.ErrorContains(t, err, "update_group_memory")
	// Tool writes must validate evidence and increment the same version as admin writes.
	row := ChatMessageRecord{GroupID: 10, UserID: 1, CreatedAt: time.Now()}
	require.NoError(t, db.Create(&row).Error)
	rc := &agent.RunContext{Values: map[string]any{"self_user_id": int64(99)}}
	raw := json.RawMessage(fmt.Sprintf(`{"previous_version":3,"evidence_ids":[%d],"reason":"消息说明","summary":"新简介"}`, row.ID))
	_, err = p.handleUpdateGroupMemory(rc, raw)
	require.NoError(t, err)
	_, err = p.handleUpdateGroupMemory(rc, raw)
	require.Error(t, err)
}
