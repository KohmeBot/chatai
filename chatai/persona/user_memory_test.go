package persona

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/favor"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func memoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "memory.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&UserMemory{}, &MemoryCursor{}, &MemoryAudit{}, &PendingMemoryMessage{}, &ChatMessageRecord{}, &UserImpression{}, &favor.FavorRecord{}))
	return db
}

func TestMemoryMigrationMergesLegacyWithoutOverwritingNewData(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.Create(&UserImpression{UserID: 1, Content: "喜欢Go"}).Error)
	require.NoError(t, db.Create(&favor.FavorRecord{UserId: 1, Favor: 512}).Error)
	require.NoError(t, db.Create(&favor.FavorRecord{UserId: 2, Favor: -10}).Error)
	require.NoError(t, db.Create(&UserImpression{UserID: 3, Content: "喜欢猫"}).Error)
	require.NoError(t, MigrateUserMemories(db))
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Equal(t, "喜欢Go", profile.Summary)
	require.Equal(t, int64(512), profile.Relationship.Score)
	profile.Summary = "现在喜欢Rust"
	require.NoError(t, writeUserMemory(db, profile))
	require.NoError(t, MigrateUserMemories(db))
	profile, err = readUserMemory(db, 1)
	require.NoError(t, err)
	require.Equal(t, "现在喜欢Rust", profile.Summary)
	other, err := readUserMemory(db, 2)
	require.NoError(t, err)
	require.Equal(t, int64(-10), other.Relationship.Score)
	third, err := readUserMemory(db, 3)
	require.NoError(t, err)
	require.Equal(t, "喜欢猫", third.Summary)
	newUser, err := readUserMemory(db, 4)
	require.NoError(t, err)
	require.Equal(t, int64(0), newUser.Relationship.Score)
	require.Equal(t, "陌生", newUser.Relationship.Level)
}

func TestMemoryCASRejectsStaleAndConcurrentFirstWrites(t *testing.T) {
	db := memoryTestDB(t)
	first, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.NoError(t, writeUserMemory(db, first))
	require.ErrorContains(t, writeUserMemory(db, first), "changed")
	first, err = readUserMemory(db, 1)
	require.NoError(t, err)
	second := first
	second.Summary = "新记忆"
	require.NoError(t, writeUserMemory(db, second))
	require.ErrorContains(t, writeUserMemory(db, first), "changed")
	got, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Equal(t, "新记忆", got.Summary)
}

func TestMemoryChangeValidatesOwnershipExpiryAndMerges(t *testing.T) {
	now := time.Now()
	profile := UserProfile{UserID: 1, Items: []MemoryItem{{Key: "style", Kind: "preference", Text: "长回答"}, {Key: "old", Text: "旧偏好"}}}
	rows := []ChatMessageRecord{{ID: 10, GroupID: 2, UserID: 1, CreatedAt: now}, {ID: 11, GroupID: 2, UserID: 3, CreatedAt: now}}
	change := MemoryChange{UserID: 1, EvidenceIDs: []uint{10}, Reason: "用户明确说明",
		Upsert: []MemoryItem{{Key: "style", Kind: "preference", Text: "简短回答", SourceID: 10, Confidence: "explicit"},
			{Key: "exam", Kind: "state", Text: "准备考试", SourceID: 10, Confidence: "explicit"}}, Forget: []string{"old"}}
	next, err := applyMemoryChange(profile, change, rows, 2, 99, now)
	require.NoError(t, err)
	require.Len(t, next.Items, 2)
	require.Equal(t, "简短回答", next.Items[0].Text)
	require.Equal(t, int64(2), next.Items[0].GroupID)
	require.WithinDuration(t, now.Add(14*24*time.Hour), *next.Items[1].ExpiresAt, time.Second)
	require.Equal(t, "长回答", profile.Items[0].Text, "must not mutate the input profile")
	change.EvidenceIDs = []uint{11}
	_, err = applyMemoryChange(profile, change, rows, 2, 99, now)
	require.ErrorContains(t, err, "this user's")
	change.EvidenceIDs = []uint{10}
	change.Upsert[0].SourceID = 11
	_, err = applyMemoryChange(profile, change, rows, 2, 99, now)
	require.ErrorContains(t, err, "source_id")
	change.Upsert = nil
	change.PreviousVersion = 1
	_, err = applyMemoryChange(profile, change, rows, 2, 99, now)
	require.ErrorContains(t, err, "stale")
}

func TestMemoryVisibilityFiltersExpiredAndOtherGroupEvents(t *testing.T) {
	now := time.Now()
	expired := now.Add(-time.Hour)
	profile := UserProfile{UserID: 1, Items: []MemoryItem{
		{Key: "expired", Kind: "state", ExpiresAt: &expired, GroupID: 2},
		{Key: "other", Kind: "experience", GroupID: 3},
		{Key: "shared", Kind: "preference", GroupID: 3},
		{Key: "boundary", Kind: "boundary", GroupID: 2},
	}}
	visible := visibleUserProfile(profile, 2, now, 8)
	require.Len(t, visible.Items, 2)
	require.Equal(t, "boundary", visible.Items[0].Key)
	require.Equal(t, "shared", visible.Items[1].Key)
	require.Len(t, profile.Items, 4)
}

func TestMaintainerPersistsCursorAndDoesNotRepeatUpdates(t *testing.T) {
	db := memoryTestDB(t)
	row := ChatMessageRecord{GroupID: 2, UserID: 1, Content: "我喜欢简短回答", CreatedAt: time.Now()}
	require.NoError(t, db.Create(&row).Error)
	calls := 0
	m := conversationTestModel{request: func(req *model.Request, res *model.Response) error {
		calls++
		require.Empty(t, req.History)
		require.Empty(t, req.Tools)
		_, ok := req.Context.Deadline()
		require.True(t, ok)
		res.Answer = fmt.Sprintf(`{"updates":[{"user_id":1,"previous_version":0,"evidence_ids":[%d],"reason":"用户表达偏好","upsert":[{"key":"reply_style","kind":"preference","text":"简短回答","source_id":%d,"confidence":"explicit"}],"relationship":{"delta":1,"description":"初次交流"}}]}`, row.ID, row.ID)
		return nil
	}}
	worker := NewMemoryMaintainer(db, m, time.Minute, 40)
	require.NoError(t, worker.process(context.Background(), 2, 99))
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), profile.Relationship.Score)
	require.Equal(t, "简短回答", profile.Items[0].Text)
	worker = NewMemoryMaintainer(db, m, time.Minute, 40) // Simulate restart.
	require.NoError(t, worker.process(context.Background(), 2, 99))
	require.Equal(t, 1, calls)
	var audits int64
	require.NoError(t, db.Model(&MemoryAudit{}).Count(&audits).Error)
	require.Equal(t, int64(1), audits)
	p := &Persona{db: db, groupID: 2}
	var event agentEventEnvelope
	require.NoError(t, json.Unmarshal([]byte(p.eventPrompt(GroupMessage{User: User{UserId: 1}}, "", 99)), &event))
	require.NotNil(t, event.UserMemory)
	require.Equal(t, "简短回答", event.UserMemory.Items[0].Text)
}

func TestMaintainerInvalidOutputRetainsCursor(t *testing.T) {
	for _, answer := range []string{`{}`, `{"updates":null}`, `not json`,
		`{"updates":[{"user_id":1,"evidence_ids":[999],"reason":"伪造来源"}]}`,
		`{"updates":[{"user_id":2,"evidence_ids":[1],"reason":"其他用户"}]}`} {
		t.Run(answer, func(t *testing.T) {
			db := memoryTestDB(t)
			require.NoError(t, db.Create(&ChatMessageRecord{GroupID: 2, UserID: 1, Content: "hello", CreatedAt: time.Now()}).Error)
			m := conversationTestModel{request: func(_ *model.Request, res *model.Response) error { res.Answer = answer; return nil }}
			worker := NewMemoryMaintainer(db, m, time.Minute, 40)
			require.Error(t, worker.process(context.Background(), 2, 99))
			var cursor MemoryCursor
			require.NoError(t, db.First(&cursor, "group_id = ?", 2).Error)
			require.Zero(t, cursor.LastID)
			var count int64
			require.NoError(t, db.Model(&UserMemory{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestMaintainerConflictRollsBackWholeBatch(t *testing.T) {
	db := memoryTestDB(t)
	row := ChatMessageRecord{GroupID: 2, UserID: 1, Content: "hello", CreatedAt: time.Now()}
	require.NoError(t, db.Create(&row).Error)
	m := conversationTestModel{request: func(_ *model.Request, res *model.Response) error {
		require.NoError(t, writeUserMemory(db, UserProfile{UserID: 1, Summary: "较新的修改"}))
		res.Answer = fmt.Sprintf(`{"updates":[{"user_id":1,"previous_version":0,"evidence_ids":[%d],"reason":"旧结果","summary":"旧内容"}]}`, row.ID)
		return nil
	}}
	require.ErrorContains(t, NewMemoryMaintainer(db, m, time.Minute, 40).process(context.Background(), 2, 99), "changed")
	var cursor MemoryCursor
	require.NoError(t, db.First(&cursor, "group_id = ?", 2).Error)
	require.Zero(t, cursor.LastID)
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Equal(t, "较新的修改", profile.Summary)
}

func TestExplicitForgetPreventsBacklogResurrection(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, writeUserMemory(db, UserProfile{UserID: 1, Summary: "旧信息", Items: []MemoryItem{{Key: "old", Text: "旧信息"}}}))
	rows := []ChatMessageRecord{
		{GroupID: 2, UserID: 1, Content: "旧信息", CreatedAt: time.Now()},
		{GroupID: 2, UserID: 1, Content: "忘掉旧信息", CreatedAt: time.Now()},
	}
	require.NoError(t, db.Create(&rows).Error)
	p := &Persona{db: db, groupID: 2}
	rc := &agent.RunContext{Values: map[string]any{"self_user_id": int64(99)}}
	_, err := p.handleUpdateUserMemory(rc, []byte(fmt.Sprintf(`{"user_id":1,"previous_version":1,"evidence_ids":[%d],"reason":"明确要求遗忘","summary":"","forget":["old"]}`, rows[1].ID)))
	require.NoError(t, err)
	m := conversationTestModel{request: func(_ *model.Request, _ *model.Response) error {
		t.Fatal("old backlog must not be sent back to the model")
		return nil
	}}
	require.NoError(t, NewMemoryMaintainer(db, m, time.Minute, 40).process(context.Background(), 2, 99))
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Empty(t, profile.Summary)
	require.Empty(t, profile.Items)
}

func TestMaintainerManyUsersDoesNotSkipEleventhUser(t *testing.T) {
	db := memoryTestDB(t)
	for id := int64(1); id <= 11; id++ {
		require.NoError(t, db.Create(&ChatMessageRecord{GroupID: 20, UserID: id, Content: "hello", CreatedAt: time.Now()}).Error)
	}
	calls := 0
	m := conversationTestModel{request: func(req *model.Request, res *model.Response) error {
		calls++
		var input struct {
			Eligible []int64 `json:"eligible_user_ids"`
		}
		require.NoError(t, json.Unmarshal([]byte(req.Question), &input))
		if calls == 1 {
			require.Len(t, input.Eligible, 10)
		} else {
			require.Equal(t, []int64{11}, input.Eligible)
		}
		res.Answer = `{"updates":[]}`
		return nil
	}}
	worker := NewMemoryMaintainer(db, m, time.Minute, 40)
	require.NoError(t, worker.process(context.Background(), 20, 99))
	require.NoError(t, worker.process(context.Background(), 20, 99))
	require.Equal(t, 2, calls)
}

func TestMaintainerBootstrapBoundsHistoryAndKeepsNewestMessages(t *testing.T) {
	db := memoryTestDB(t)
	rows := make([]ChatMessageRecord, 205)
	for i := range rows {
		rows[i] = ChatMessageRecord{GroupID: 2, UserID: 1, Content: "hello", CreatedAt: time.Now()}
	}
	require.NoError(t, db.Create(&rows).Error)
	m := conversationTestModel{request: func(req *model.Request, res *model.Response) error {
		var input struct {
			Messages []struct {
				SourceID uint `json:"source_id"`
			} `json:"messages"`
		}
		require.NoError(t, json.Unmarshal([]byte(req.Question), &input))
		require.Len(t, input.Messages, 40)
		require.Equal(t, rows[5].ID, input.Messages[0].SourceID)
		res.Answer = `{"updates":[]}`
		return nil
	}}
	require.NoError(t, NewMemoryMaintainer(db, m, time.Minute, 40).process(context.Background(), 2, 99))
	var cursor MemoryCursor
	require.NoError(t, db.First(&cursor, "group_id = ?", 2).Error)
	require.Equal(t, rows[44].ID, cursor.LastID)
}

func TestStructuredMemoryToolsRespectWriteSwitchAndLegacySharesProfile(t *testing.T) {
	db := memoryTestDB(t)
	p := &Persona{db: db, tools: agent.NewRegistry()}
	p.registerBuiltinTools()
	require.Contains(t, p.ToolNames(), "read_user_memory")
	require.NotContains(t, p.ToolNames(), "update_user_memory")
	require.NotContains(t, p.ToolNames(), "update_user_favor")
	require.NoError(t, writeUserMemory(db, UserProfile{UserID: 1, Summary: "原简介",
		Items: []MemoryItem{{Key: "style", Kind: "preference", Text: "简短回答"}}}))
	_, err := p.handleUpdateImpression(nil, []byte(`{"scope":"user","user_id":1,"previous_content":"原简介","content":"新简介","reason":"明确纠正"}`))
	require.NoError(t, err)
	_, err = p.handleUpdateUserFavor(nil, []byte(`{"user_id":1,"delta":60,"reason":"实际互动"}`))
	require.NoError(t, err)
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Equal(t, "新简介", profile.Summary)
	require.Len(t, profile.Items, 1)
	require.Equal(t, int64(5), profile.Relationship.Score)
}

func TestMemoryRelationshipDeltaIsBoundedAndHasEvidence(t *testing.T) {
	profile := UserProfile{UserID: 1}
	rows := []ChatMessageRecord{{ID: 1, GroupID: 2, UserID: 1, CreatedAt: time.Now()}}
	var change MemoryChange
	require.NoError(t, json.Unmarshal([]byte(`{"user_id":1,"evidence_ids":[1],"reason":"互动产生关系变化","relationship":{"delta":100,"description":"熟悉"}}`), &change))
	_, err := applyMemoryChange(profile, change, rows, 2, 99, time.Now())
	require.ErrorContains(t, err, "-5..5")
	change.Relationship.Delta = 5
	next, err := applyMemoryChange(profile, change, rows, 2, 99, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(5), next.Relationship.Score)
}

func TestMaintainerConcurrentBatchesCommitOnlyOnce(t *testing.T) {
	db := memoryTestDB(t)
	row := ChatMessageRecord{GroupID: 2, UserID: 1, Content: "hello", CreatedAt: time.Now()}
	require.NoError(t, db.Create(&row).Error)
	require.NoError(t, db.Create(&MemoryCursor{GroupID: 2}).Error)
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	m := conversationTestModel{request: func(_ *model.Request, res *model.Response) error {
		ready <- struct{}{}
		<-release
		res.Answer = fmt.Sprintf(`{"updates":[{"user_id":1,"previous_version":0,"evidence_ids":[%d],"reason":"实际互动","relationship":{"delta":1,"description":"初次交流"}}]}`, row.ID)
		return nil
	}}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- NewMemoryMaintainer(db, m, time.Minute, 40).process(context.Background(), 2, 99)
		}()
	}
	<-ready
	<-ready
	close(release)
	err1, err2 := <-results, <-results
	require.True(t, (err1 == nil) != (err2 == nil), "exactly one batch must commit: %v, %v", err1, err2)
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), profile.Relationship.Score)
	var audits int64
	require.NoError(t, db.Model(&MemoryAudit{}).Count(&audits).Error)
	require.Equal(t, int64(1), audits)
}

func TestMaintainerWorkerCoalescesObserveAndHonorsCancellation(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.Create(&ChatMessageRecord{GroupID: 2, UserID: 1, Content: "hello", CreatedAt: time.Now()}).Error)
	started, finished := make(chan struct{}, 1), make(chan struct{}, 1)
	m := conversationTestModel{request: func(req *model.Request, _ *model.Response) error {
		started <- struct{}{}
		<-req.Context.Done()
		finished <- struct{}{}
		return req.Context.Err()
	}}
	worker := NewMemoryMaintainer(db, m, 10*time.Millisecond, 40)
	worker.ObserveGroupChat = true
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker.Observe(2, 99)
		}()
	}
	wg.Wait()
	require.Len(t, worker.groups, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("worker ignored cancellation")
	}
}
