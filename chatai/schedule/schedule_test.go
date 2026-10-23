package schedule

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "schedule.db")), &gorm.Config{})
	require.NoError(t, err)
	sql, err := db.DB()
	require.NoError(t, err)
	sql.SetMaxOpenConns(1)
	t.Cleanup(func() { sql.Close() })
	require.NoError(t, db.AutoMigrate(Models()...))
	return New(db)
}

func create(t *testing.T, s *Service, spec string) Task {
	t.Helper()
	in := Input{Instruction: "发送今日摘要", Cron: spec}
	if spec == "" {
		in.DelaySeconds = 60
	}
	task, err := s.Create(1, 2, 3, in)
	require.NoError(t, err)
	return *task
}

func due(t *testing.T, s *Service, task Task) Task {
	n := time.Now().UTC().Add(-24 * time.Hour)
	task.NextRunAt = &n
	require.NoError(t, s.db.Model(&Task{}).Where("id = ?", task.ID).Update("next_run_at", n).Error)
	return task
}

func TestValidationAndTimezone(t *testing.T) {
	s := testService(t)
	past := time.Now().Add(-time.Hour)
	for _, in := range []Input{
		{}, {Instruction: "x"}, {Instruction: "x", DelaySeconds: -1}, {Instruction: "x", RunAt: &past},
		{Instruction: "x", Cron: "bad"}, {Instruction: "x", Cron: "0 0 30 2 *"},
		{Instruction: "x", Cron: "* * * * * *"}, {Instruction: "x", Cron: "* * * * *", Timezone: "bad"},
		{Instruction: "x", Cron: "* * * * *", DelaySeconds: 1},
	} {
		_, err := s.Create(1, 2, 3, in)
		require.Error(t, err)
	}
	c, err := parse("0 9 * * 1-5", "Asia/Shanghai")
	require.NoError(t, err)
	start := time.Date(2026, 10, 23, 2, 0, 0, 0, time.UTC) // Friday after 09:00 Shanghai
	require.Equal(t, time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC), c.Next(start).UTC())
}

func TestClaimCompletionAndIsolation(t *testing.T) {
	s := testService(t)
	task := due(t, s, create(t, s, ""))
	r, err := s.claim(task, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, r)
	duplicate, err := s.claim(task, time.Now().UTC())
	require.NoError(t, err)
	require.Nil(t, duplicate)
	s.Execute = func(Task) error { return nil }
	s.execute(task, *r)
	rows, err := s.List(1, 2, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "completed", rows[0].Status)
	require.Nil(t, rows[0].NextRunAt)
	require.Equal(t, "success", rows[0].Runs[0].Status)
	require.Error(t, s.Delete(9, 0, task.ID))
	require.Error(t, s.Delete(1, 9, task.ID))
	require.NoError(t, s.Delete(1, 2, task.ID))
	rows, err = s.List(1, 0, 0)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestRestartRecoveryAndMissedRuns(t *testing.T) {
	s := testService(t)
	one := due(t, s, create(t, s, ""))
	recurring := due(t, s, create(t, s, "* * * * *"))
	pending := due(t, s, create(t, s, ""))
	_, err := s.claim(one, time.Now().UTC())
	require.NoError(t, err)
	_, err = s.claim(recurring, time.Now().UTC())
	require.NoError(t, err)
	// A new service reads the same persisted database after a process restart.
	restarted := New(s.db)
	now := time.Now().UTC()
	require.NoError(t, restarted.Recover(now))
	rows, err := restarted.List(1, 0, 0)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, pending.ID, rows[0].ID)
	require.Equal(t, "active", rows[0].Status)
	require.True(t, rows[0].NextRunAt.Before(now))
	require.Equal(t, "active", rows[1].Status)
	require.True(t, rows[1].NextRunAt.After(now))
	require.Equal(t, "interrupted", rows[1].Runs[0].Status)
	require.Equal(t, "interrupted", rows[2].Status)
	require.Nil(t, rows[2].NextRunAt)
	// One overdue periodic execution skips the backlog and schedules after completion.
	periodic := due(t, s, create(t, s, "* * * * *"))
	r, err := restarted.claim(periodic, now)
	require.NoError(t, err)
	restarted.Execute = func(Task) error { return errors.New("delivery failed") }
	restarted.execute(periodic, *r)
	var saved Task
	require.NoError(t, s.db.First(&saved, periodic.ID).Error)
	require.Equal(t, "active", saved.Status)
	require.True(t, saved.NextRunAt.After(now))
	var execution Run
	require.NoError(t, s.db.First(&execution, r.ID).Error)
	require.Equal(t, "failed", execution.Status)
	require.Contains(t, execution.Error, "delivery failed")
}

func TestDeleteDuringExecutionDoesNotResurrect(t *testing.T) {
	s := testService(t)
	task := due(t, s, create(t, s, "* * * * *"))
	r, err := s.claim(task, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, s.Delete(1, 0, task.ID))
	s.Execute = func(Task) error { panic("broken executor") }
	s.execute(task, *r)
	rows, err := s.List(1, 0, 0)
	require.NoError(t, err)
	require.Empty(t, rows)
	var run Run
	require.NoError(t, s.db.First(&run, r.ID).Error)
	require.Equal(t, "failed", run.Status)
	require.Contains(t, run.Error, "broken executor")
}

func TestWorkerWaitsForBotAndDoesNotOverlap(t *testing.T) {
	s := testService(t)
	task := due(t, s, create(t, s, ""))
	var ready atomic.Bool
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	s.Ready = func(Task) bool { return ready.Load() }
	s.Execute = func(Task) error { calls.Add(1); close(started); <-release; return nil }
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { close(release); s.Close() })
	time.Sleep(1100 * time.Millisecond)
	require.Zero(t, calls.Load())
	ready.Store(true)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not execute restored overdue task")
	}
	duplicate, err := s.claim(task, time.Now().UTC())
	require.NoError(t, err)
	require.Nil(t, duplicate)
	require.Equal(t, int32(1), calls.Load())
}

func TestOfflineBacklogDoesNotStarveReadyTasks(t *testing.T) {
	s := testService(t)
	for i := 0; i < 101; i++ {
		due(t, s, create(t, s, ""))
	}
	ready := due(t, s, create(t, s, ""))
	s.Ready = func(task Task) bool { return task.ID == ready.ID }
	var calls atomic.Int32
	s.Execute = func(Task) error { calls.Add(1); return nil }
	s.poll(context.Background(), make(chan struct{}, 4))
	s.wg.Wait()
	require.Equal(t, int32(1), calls.Load())
}

func TestResultWriteRetryDoesNotRepeatActionAndBoundsHistory(t *testing.T) {
	s := testService(t)
	task := due(t, s, create(t, s, ""))
	for i := 0; i < 105; i++ {
		require.NoError(t, s.db.Create(&Run{TaskID: task.ID, Status: "success"}).Error)
	}
	r, err := s.claim(task, time.Now().UTC())
	require.NoError(t, err)
	var failed atomic.Bool
	require.NoError(t, s.db.Callback().Update().Before("gorm:update").Register("test:temporary_busy", func(tx *gorm.DB) {
		if tx.Statement.Table == "scheduled_task_runs" && !failed.Swap(true) {
			tx.AddError(errors.New("temporary database contention"))
		}
	}))
	defer s.db.Callback().Update().Remove("test:temporary_busy")
	calls := 0
	s.Execute = func(Task) error { calls++; return nil }
	s.execute(task, *r)
	require.Equal(t, 1, calls)
	var count int64
	require.NoError(t, s.db.Model(&Run{}).Where("task_id = ?", task.ID).Count(&count).Error)
	require.Equal(t, int64(100), count)
	var saved Run
	require.NoError(t, s.db.First(&saved, r.ID).Error)
	require.Equal(t, "success", saved.Status)
}
