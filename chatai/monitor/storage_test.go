package monitor

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func storageDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	sql, err := db.DB()
	require.NoError(t, err)
	sql.SetMaxOpenConns(1)
	t.Cleanup(func() { sql.Close() })
	return db
}

func TestPersistentRestartEvictionAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	db := storageDB(t, path)
	s, err := NewPersistent(db, 1, 0)
	require.NoError(t, err)
	require.Equal(t, 7*24*time.Hour, s.retention)
	now := time.Now()
	for id := uint64(1); id <= 3; id++ {
		s.Observe(agent.Event{Kind: "run_started", RunID: id, Time: now, Text: "hello", UserID: 42})
		s.Observe(agent.Event{Kind: "model_finished", RunID: id, Time: now.Add(time.Second), InputTokens: 7, OutputTokens: 3, Reasoning: "provider"})
		if id != 3 {
			s.Observe(agent.Event{Kind: "run_finished", RunID: id, Time: now.Add(2 * time.Second), DurationMS: 2000})
		}
	}
	require.Len(t, s.runs, 1)
	rows, err := s.readSnapshot("")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	for _, r := range rows {
		require.Empty(t, r.Events)
	}
	firstID := s.session + "-1"
	require.NoError(t, s.Close())
	sql, _ := db.DB()
	require.NoError(t, sql.Close())
	restarted, err := NewPersistent(storageDB(t, path), 1, 7)
	require.NoError(t, err)
	rows, err = restarted.readSnapshot(firstID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 7, rows[0].InputTokens)
	require.Equal(t, "provider", rows[0].Events[1].Reasoning)
	require.Len(t, rows[0].Events, 3)
	interrupted, err := restarted.readSnapshot(s.session + "-3")
	require.NoError(t, err)
	require.Equal(t, "error", interrupted[0].Status)
	require.Contains(t, interrupted[0].Error, "进程中断")
	require.EqualValues(t, 1000, interrupted[0].DurationMS)
	restarted.Observe(agent.Event{Kind: "run_started", RunID: 1, Time: now})
	require.NotEqual(t, firstID, restarted.runs[1].ID)
	rows, err = restarted.readSnapshot("")
	require.NoError(t, err)
	require.Len(t, rows, 4)
}

func TestPersistentRetentionRemovesDetailsAndSummaries(t *testing.T) {
	db := storageDB(t, filepath.Join(t.TempDir(), "retention.db"))
	s, err := NewPersistent(db, 2, 2)
	require.NoError(t, err)
	now := time.Now()
	for id, age := range []time.Duration{3 * 24 * time.Hour, 24 * time.Hour} {
		s.Observe(agent.Event{RunID: uint64(id + 1), Kind: "run_started", Time: now.Add(-age), Text: "sensitive"})
		s.Observe(agent.Event{RunID: uint64(id + 1), Kind: "run_finished", Time: now.Add(-age)})
	}
	rows, err := s.readSnapshot("")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, s.maintain(now))
	var count int64
	require.NoError(t, db.Model(&monitorRun{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&monitorEvent{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.Len(t, s.runs, 1)
	// Startup also prunes records when the configured window is shortened.
	_, err = NewPersistent(db, 2, 1)
	require.NoError(t, err)
	require.NoError(t, db.Model(&monitorEvent{}).Count(&count).Error)
	require.Zero(t, count)
	_, err = NewPersistent(db, 2, -1)
	require.Error(t, err)
	_, err = NewPersistent(db, 2, 36501)
	require.Error(t, err)
}

func TestPersistentConcurrentActiveRunsAndRetry(t *testing.T) {
	db := storageDB(t, filepath.Join(t.TempDir(), "concurrent.db"))
	s, err := NewPersistent(db, 1, 7)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for id := uint64(1); id <= 8; id++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Observe(agent.Event{RunID: id, Kind: "run_started", Time: time.Now()})
			s.Observe(agent.Event{RunID: id, Kind: "model_finished", Time: time.Now(), InputTokens: 2, Text: strings.Repeat("x", 10000)})
		}()
	}
	wg.Wait()
	rows, err := s.readSnapshot("")
	require.NoError(t, err)
	require.Len(t, rows, 8)
	// Force an event insert failure. Summary and events must roll back together.
	require.NoError(t, db.Exec("CREATE TRIGGER fail_monitor_event BEFORE INSERT ON monitor_events BEGIN SELECT RAISE(FAIL, 'test failure'); END").Error)
	s.Observe(agent.Event{RunID: 1, Kind: "model_finished", Time: time.Now(), InputTokens: 3})
	var stored monitorRun
	require.NoError(t, db.First(&stored, "id = ?", s.runs[1].ID).Error)
	require.Contains(t, string(stored.Payload), `"input_tokens":2`)
	require.Equal(t, 2, s.runs[1].persisted)
	require.NoError(t, db.Exec("DROP TRIGGER fail_monitor_event").Error)
	require.NoError(t, s.maintain(time.Now()))
	for id := uint64(1); id <= 8; id++ {
		s.Observe(agent.Event{RunID: id, Kind: "run_finished", Time: time.Now()})
	}
	require.NoError(t, s.Close())
	restarted, err := NewPersistent(db, 1, 7)
	require.NoError(t, err)
	for _, r := range rows {
		detail, err := restarted.readSnapshot(r.ID)
		require.NoError(t, err)
		require.True(t, detail[0].Truncated)
		require.Equal(t, "success", detail[0].Status)
		expected := int64(2)
		if r.ID == fmt.Sprintf("%s-1", s.session) {
			expected = 5
		}
		require.Equal(t, expected, detail[0].InputTokens)
	}
}

func TestRunMetricsPersistAndBackfill(t *testing.T) {
	db := storageDB(t, filepath.Join(t.TempDir(), "metrics.db"))
	s, err := NewPersistent(db, 1, 7)
	require.NoError(t, err)
	kinds := []agent.Event{
		{Kind: "run_started"},
		{Kind: "model_started"}, {Kind: "model_finished", InputTokens: 3, OutputTokens: 4},
		{Kind: "model_started"}, {Kind: "model_finished", Error: "failed"},
		{Kind: "tool_started", Name: "read_skill"}, {Kind: "tool_finished", Name: "read_skill"},
		{Kind: "skill_loaded", Name: "read_skill"},
		{Kind: "tool_started", Name: "search_tools"},
		{Kind: "skill_loaded", Name: "report"},
		{Kind: "tool_started", Name: "pending"},
		{Kind: "run_finished"},
	}
	for _, e := range kinds {
		e.RunID, e.Time = 1, time.Now()
		s.Observe(e)
	}
	id := s.runs[1].ID
	check := func(store *Store) {
		rows, err := store.readSnapshot("")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, 2, rows[0].ModelCalls)
		require.Equal(t, 4, rows[0].ToolSkillCount)
		require.EqualValues(t, 7, rows[0].InputTokens+rows[0].OutputTokens)
		require.Empty(t, rows[0].Events)
	}
	check(s)
	restarted, err := NewPersistent(db, 1, 7)
	require.NoError(t, err)
	check(restarted)
	// Simulate a summary saved before metrics existed.
	require.NoError(t, db.Model(&monitorRun{}).Where("id = ?", id).Update("payload",
		[]byte(fmt.Sprintf("{\"id\":%q,\"started_at\":%q,\"status\":\"success\",\"input_tokens\":3,\"output_tokens\":4}", id, time.Now().Format(time.RFC3339Nano)))).Error)
	upgraded, err := NewPersistent(db, 1, 7)
	require.NoError(t, err)
	check(upgraded)
	require.NoError(t, upgraded.backfillMetrics())
	check(upgraded)
}
