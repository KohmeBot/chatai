package persona

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

func TestConversationMemoryOnlyExtractsDialogueAfterExpiry(t *testing.T) {
	for _, shortTerm := range []bool{false, true} {
		t.Run(fmt.Sprint(shortTerm), func(t *testing.T) {
			db := memoryTestDB(t)
			rows := []ChatMessageRecord{
				{GroupID: 10, UserID: 1, MessageID: 10, Content: "我喜欢简短回答"},
				{GroupID: 10, UserID: 2, MessageID: 11, Content: "普通群聊不能进入记忆"},
				{GroupID: 10, UserID: 99, MessageID: 12, Content: "好的"},
				{GroupID: 10, UserID: 1, MessageID: 13, Content: "同一个人的普通群聊也不能进入"},
				{GroupID: 20, UserID: 1, MessageID: 10, Content: "另一个群"},
			}
			require.NoError(t, db.Create(&rows).Error)
			calls := 0
			m := conversationTestModel{request: func(req *model.Request, res *model.Response) error {
				calls++
				var input struct {
					Messages []struct {
						SourceID uint `json:"source_id"`
					} `json:"messages"`
				}
				require.NoError(t, json.Unmarshal([]byte(req.Question), &input))
				require.Len(t, input.Messages, 2)
				require.Equal(t, rows[0].ID, input.Messages[0].SourceID)
				require.Equal(t, rows[2].ID, input.Messages[1].SourceID)
				res.Answer = fmt.Sprintf(`{"updates":[{"user_id":1,"previous_version":0,"evidence_ids":[%d],"reason":"对话中的明确偏好","summary":"喜欢简短回答"}]}`, rows[0].ID)
				return nil
			}}
			worker := NewMemoryMaintainer(db, m, time.Minute, 40)
			worker.Observe(10, 99)
			require.Empty(t, worker.groups)
			p := memoryPersona()
			p.db, p.opts.MemoryMaintainer, p.opts.ConversationMemory = db, worker, shortTerm
			rc := memoryRun(1)
			p.rememberDialogue(rc, "user", 1, 10, rows[0].Content, "text")
			p.rememberDialogue(rc, "agent", 99, 12, rows[2].Content, "text")
			p.finishConversation(rc)
			require.NoError(t, worker.processConversations(context.Background(), time.Now()))
			require.Zero(t, calls)
			// Durable evidence survives a restart; no incoming group event required.
			worker = NewMemoryMaintainer(db, m, time.Minute, 40)
			require.NoError(t, worker.processConversations(context.Background(), time.Now().Add(11*time.Minute)))
			require.Equal(t, 1, calls)
			require.NoError(t, worker.processConversations(context.Background(), time.Now().Add(12*time.Minute)))
			require.Equal(t, 1, calls)
			profile, err := readUserMemory(db, 1)
			require.NoError(t, err)
			require.Equal(t, "喜欢简短回答", profile.Summary)
		})
	}
}

func TestConversationContinuationAndFailureRetainPendingEvidence(t *testing.T) {
	db := memoryTestDB(t)
	row := ChatMessageRecord{GroupID: 10, UserID: 1, MessageID: 10, Content: "偏好"}
	require.NoError(t, db.Create(&row).Error)
	calls := 0
	answer := `invalid`
	m := conversationTestModel{request: func(_ *model.Request, res *model.Response) error {
		calls++
		res.Answer = answer
		return nil
	}}
	worker := NewMemoryMaintainer(db, m, time.Minute, 40)
	now := time.Now()
	rc := memoryRun(1)
	require.NoError(t, worker.stageConversation(10, rc, []dialogueSource{{1, 10}}, now.Add(-time.Minute)))
	done := worker.beginConversation(10, 1)
	require.NoError(t, worker.processConversations(context.Background(), now))
	require.Zero(t, calls)
	require.NoError(t, worker.stageConversation(10, rc, []dialogueSource{{1, 10}}, now.Add(time.Minute)))
	done()
	require.NoError(t, worker.processConversations(context.Background(), now))
	require.Zero(t, calls)
	require.Error(t, worker.processConversation(context.Background(), conversationKey{10, 1}, now.Add(2*time.Minute)))
	var count int64
	require.NoError(t, db.Model(&PendingMemoryMessage{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
	answer = `{"updates":[]}`
	require.NoError(t, worker.processConversation(context.Background(), conversationKey{10, 1}, now.Add(2*time.Minute)))
	require.NoError(t, db.Model(&PendingMemoryMessage{}).Count(&count).Error)
	require.Zero(t, count)
	require.Equal(t, 2, calls)
}

func TestGroupChatModeDoesNotStageConversation(t *testing.T) {
	worker := NewMemoryMaintainer(memoryTestDB(t), nil, time.Minute, 40)
	worker.ObserveGroupChat = true
	worker.Observe(10, 99)
	require.Len(t, worker.groups, 1)
	require.NoError(t, worker.stageConversation(10, memoryRun(1), []dialogueSource{{1, 10}}, time.Now()))
	var count int64
	require.NoError(t, worker.db.Model(&PendingMemoryMessage{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestExpiredConversationWorkerRunsWithoutObserve(t *testing.T) {
	db := memoryTestDB(t)
	require.NoError(t, db.Create(&ChatMessageRecord{GroupID: 10, UserID: 1, MessageID: 10, Content: "hello"}).Error)
	called := make(chan struct{}, 1)
	m := conversationTestModel{request: func(_ *model.Request, res *model.Response) error {
		res.Answer = `{"updates":[]}`
		select {
		case called <- struct{}{}:
		default:
		}
		return nil
	}}
	worker := NewMemoryMaintainer(db, m, 10*time.Millisecond, 40)
	require.NoError(t, worker.stageConversation(10, memoryRun(1), []dialogueSource{{1, 10}}, time.Now().Add(-time.Minute)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("expired conversation was not processed without a new message")
	}
	// Wait for the worker's transaction before the test database is closed.
	require.Eventually(t, func() bool {
		var count int64
		return db.Model(&PendingMemoryMessage{}).Count(&count).Error == nil && count == 0
	}, time.Second, 10*time.Millisecond)
}

func TestConversationResumedDuringExtractionDoesNotCommit(t *testing.T) {
	db := memoryTestDB(t)
	row := ChatMessageRecord{GroupID: 10, UserID: 1, MessageID: 10, Content: "hello"}
	require.NoError(t, db.Create(&row).Error)
	var worker *MemoryMaintainer
	now := time.Now()
	m := conversationTestModel{request: func(_ *model.Request, res *model.Response) error {
		require.NoError(t, worker.stageConversation(10, memoryRun(1), []dialogueSource{{1, 10}}, now.Add(time.Minute)))
		res.Answer = fmt.Sprintf(`{"updates":[{"user_id":1,"previous_version":0,"evidence_ids":[%d],"reason":"过期结果","summary":"不能提交"}]}`, row.ID)
		return nil
	}}
	worker = NewMemoryMaintainer(db, m, time.Minute, 40)
	require.NoError(t, worker.stageConversation(10, memoryRun(1), []dialogueSource{{1, 10}}, now.Add(-time.Minute)))
	require.ErrorContains(t, worker.processConversation(context.Background(), conversationKey{10, 1}, now), "expiry changed")
	var count int64
	require.NoError(t, db.Model(&PendingMemoryMessage{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
	profile, err := readUserMemory(db, 1)
	require.NoError(t, err)
	require.Empty(t, profile.Summary)
}

func TestConversationBatchingRetainsRemainingDialogue(t *testing.T) {
	db := memoryTestDB(t)
	rows := []ChatMessageRecord{
		{GroupID: 10, UserID: 1, MessageID: 10, Content: "first"},
		{GroupID: 10, UserID: 1, MessageID: 11, Content: "second"},
	}
	require.NoError(t, db.Create(&rows).Error)
	calls := 0
	m := conversationTestModel{request: func(req *model.Request, res *model.Response) error {
		var input struct {
			Messages []struct {
				SourceID uint `json:"source_id"`
			} `json:"messages"`
		}
		require.NoError(t, json.Unmarshal([]byte(req.Question), &input))
		require.Len(t, input.Messages, 1)
		require.Equal(t, rows[calls].ID, input.Messages[0].SourceID)
		calls++
		res.Answer = `{"updates":[]}`
		return nil
	}}
	worker := NewMemoryMaintainer(db, m, time.Minute, 1)
	require.NoError(t, worker.stageConversation(10, memoryRun(1), []dialogueSource{{1, 10}, {1, 11}}, time.Now().Add(-time.Minute)))
	for range 3 {
		require.NoError(t, worker.processConversations(context.Background(), time.Now()))
	}
	require.Equal(t, 2, calls)
}
