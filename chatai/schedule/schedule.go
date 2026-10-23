// Package schedule provides durable, single-process task scheduling.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type Task struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	GroupID     int64          `gorm:"index" json:"group_id,string"`
	UserID      int64          `json:"user_id,string"`
	BotID       int64          `json:"bot_id,string"`
	Name        string         `json:"name"`
	Instruction string         `json:"instruction"`
	Cron        string         `json:"cron"`
	Timezone    string         `json:"timezone"`
	NextRunAt   *time.Time     `gorm:"index" json:"next_run_at"`
	Status      string         `gorm:"index" json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Runs        []Run          `gorm:"-" json:"runs"`
}

type Run struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TaskID      uint       `gorm:"index" json:"task_id"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Status      string     `gorm:"index" json:"status"`
	Error       string     `json:"error,omitempty"`
}

func (Task) TableName() string { return "scheduled_tasks" }
func (Run) TableName() string  { return "scheduled_task_runs" }

type Input struct {
	Name         string     `json:"name"`
	Instruction  string     `json:"instruction"`
	Cron         string     `json:"cron"`
	Timezone     string     `json:"timezone"`
	RunAt        *time.Time `json:"run_at"`
	DelaySeconds int        `json:"delay_seconds"`
}

type Service struct {
	db *gorm.DB
	// Ready resolves live bot availability before claiming a task.
	Ready   func(Task) bool
	Execute func(Task) error
	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

func Models() []any { return []any{&Task{}, &Run{}} }

func parse(spec, timezone string) (cron.Schedule, error) {
	if len(strings.Fields(spec)) != 5 {
		return nil, errors.New("cron 必须为五段：分 时 日 月 星期")
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("无效时区: %w", err)
	}
	s, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("无效 cron: %w", err)
	}
	s.(*cron.SpecSchedule).Location = loc
	return s, nil
}

func (s *Service) Create(group, user, bot int64, in Input) (*Task, error) {
	if group <= 0 || user <= 0 || bot <= 0 {
		return nil, errors.New("群、用户和机器人 ID 必须为正数")
	}
	in.Name, in.Instruction, in.Cron = strings.TrimSpace(in.Name), strings.TrimSpace(in.Instruction), strings.TrimSpace(in.Cron)
	if len(in.Cron) > 100 || len(in.Timezone) > 100 {
		return nil, errors.New("cron 和时区最多 100 字节")
	}
	if in.Instruction == "" || len(in.Instruction) > 16000 || len(in.Name) > 200 {
		return nil, errors.New("任务说明不能为空且最多 16000 字节，名称最多 200 字节")
	}
	if in.Name == "" {
		r := []rune(in.Instruction)
		in.Name = string(r[:min(len(r), 40)])
	}
	if in.Timezone == "" {
		in.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return nil, fmt.Errorf("无效时区: %w", err)
	}
	modes := 0
	if in.Cron != "" {
		modes++
	}
	if in.RunAt != nil {
		modes++
	}
	if in.DelaySeconds != 0 {
		modes++
	}
	if modes != 1 {
		return nil, errors.New("run_at、delay_seconds、cron 必须且只能填写一个")
	}
	now := time.Now().UTC()
	var next time.Time
	if in.Cron != "" {
		c, err := parse(in.Cron, in.Timezone)
		if err != nil {
			return nil, err
		}
		next = c.Next(now)
	} else if in.RunAt != nil {
		next = in.RunAt.UTC()
	} else {
		if in.DelaySeconds < 1 || int64(in.DelaySeconds) > 315360000 {
			return nil, errors.New("delay_seconds 必须在 1 秒至 10 年之间")
		}
		next = now.Add(time.Duration(in.DelaySeconds) * time.Second)
	}
	if next.IsZero() || !next.After(now) {
		return nil, errors.New("任务必须有有效的未来执行时间")
	}
	next = next.UTC()
	t := &Task{GroupID: group, UserID: user, BotID: bot, Name: in.Name, Instruction: in.Instruction, Cron: in.Cron, Timezone: in.Timezone, NextRunAt: &next, Status: "active"}
	return t, s.db.Create(t).Error
}

func (s *Service) List(group, user int64, offset int) ([]Task, error) {
	items := []Task{}
	q := s.db.Where("group_id = ?", group)
	if user > 0 {
		q = q.Where("user_id = ?", user)
	}
	if err := q.Order("id DESC").Offset(offset).Limit(50).Find(&items).Error; err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Runs = []Run{}
		if err := s.db.Where("task_id = ?", items[i].ID).Order("id DESC").Limit(10).Find(&items[i].Runs).Error; err != nil {
			return nil, err
		}
	}
	return items, nil
}

// Delete prevents subsequent claims. An already running action may finish.
func (s *Service) Delete(group, user int64, id uint) error {
	q := s.db.Where("group_id = ?", group)
	if user > 0 {
		q = q.Where("user_id = ?", user)
	}
	r := q.Delete(&Task{}, id)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected == 0 {
		return errors.New("任务不存在或无权删除")
	}
	return nil
}

// Recover records ambiguous interrupted executions without retrying their side effects.
// Pending overdue tasks are coalesced into one execution by the normal poller.
func (s *Service) Recover(now time.Time) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Run{}).Where("status = ?", "running").Updates(map[string]any{"status": "interrupted", "finished_at": now, "error": "进程退出导致执行中断；为避免重复副作用，此次不自动重试"}).Error; err != nil {
			return err
		}
		var tasks []Task
		if err := tx.Where("status = ?", "running").Find(&tasks).Error; err != nil {
			return err
		}
		for _, t := range tasks {
			if err := finishTask(tx, t, "interrupted", now); err != nil {
				return err
			}
		}
		return nil
	})
}

func finishTask(tx *gorm.DB, t Task, status string, now time.Time) error {
	var next *time.Time
	if t.Cron != "" {
		c, err := parse(t.Cron, t.Timezone)
		if err != nil {
			return err
		}
		n := c.Next(now).UTC()
		if !n.IsZero() {
			next = &n
			status = "active"
		}
	}
	return tx.Model(&Task{}).Where("id = ? AND status = ?", t.ID, "running").Updates(map[string]any{"status": status, "next_run_at": next}).Error
}

func (s *Service) Start(parent context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil
	}
	if s.Execute == nil {
		return errors.New("scheduler executor is missing")
	}
	if err := s.Recover(time.Now().UTC()); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		slots := make(chan struct{}, 4)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			s.poll(ctx, slots)
		}
	}()
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
		s.cancel = nil
	}
}

func (s *Service) poll(ctx context.Context, slots chan struct{}) {
	// Keyset pagination avoids an offline bot's backlog starving other bots.
	var after uint
	for ctx.Err() == nil {
		var tasks []Task
		if err := s.db.Where("status = ? AND next_run_at <= ? AND id > ?", "active", time.Now().UTC(), after).Order("id").Limit(100).Find(&tasks).Error; err != nil {
			logrus.Errorf("[Schedule] poll: %v", err)
			return
		}
		for _, task := range tasks {
			after = task.ID
			if ctx.Err() != nil {
				return
			}
			if s.Ready != nil && !s.Ready(task) {
				continue
			}
			select {
			case slots <- struct{}{}:
			default:
				return
			}
			run, err := s.claim(task, time.Now().UTC())
			if err != nil || run == nil {
				<-slots
				if err != nil {
					logrus.Errorf("[Schedule] claim: %v", err)
				}
				continue
			}
			s.wg.Add(1)
			go func(t Task, r Run) { defer s.wg.Done(); defer func() { <-slots }(); s.execute(t, r) }(task, *run)
		}
		if len(tasks) < 100 {
			return
		}
	}
}

func (s *Service) claim(t Task, now time.Time) (*Run, error) {
	var run *Run
	err := s.db.Transaction(func(tx *gorm.DB) error {
		r := tx.Model(&Task{}).Where("id = ? AND status = ? AND next_run_at <= ?", t.ID, "active", now).Update("status", "running")
		if r.Error != nil || r.RowsAffected == 0 {
			return r.Error
		}
		run = &Run{TaskID: t.ID, ScheduledAt: *t.NextRunAt, StartedAt: now, Status: "running"}
		return tx.Create(run).Error
	})
	return run, err
}

func (s *Service) execute(t Task, r Run) {
	var runErr error
	func() {
		defer func() {
			if v := recover(); v != nil {
				runErr = fmt.Errorf("执行异常: %v", v)
			}
		}()
		runErr = s.Execute(t)
	}()
	status, detail := "success", ""
	if runErr != nil {
		status, detail = "failed", runErr.Error()
		if len(detail) > 8000 {
			detail = strings.ToValidUTF8(detail[:8000], "")
		}
	}
	now := time.Now().UTC()
	persist := func() error {
		return s.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&Run{}).Where("id = ?", r.ID).Updates(map[string]any{"status": status, "error": detail, "finished_at": now}).Error; err != nil {
				return err
			}
			taskStatus := "completed"
			if runErr != nil {
				taskStatus = "failed"
			}
			if err := finishTask(tx, t, taskStatus, now); err != nil {
				return err
			}
			// Keep the most recent 100 executions per task on disk.
			return tx.Where("task_id = ? AND id NOT IN (?)", t.ID, tx.Model(&Run{}).Select("id").Where("task_id = ?", t.ID).Order("id DESC").Limit(100)).Delete(&Run{}).Error
		})
	}
	// Retry result writes, never the Agent action, after transient database contention.
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
		err = persist()
		if err == nil {
			return
		}
	}
	if err != nil {
		logrus.Errorf("[Schedule] persist result task=%d: %v", t.ID, err)
	}
}
