package monitor

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store summaries separately: listing a week of runs never loads event bodies.
type monitorRun struct {
	ID        string    `gorm:"primaryKey;size:80"`
	StartedAt time.Time `gorm:"index"`
	UpdatedAt time.Time `gorm:"autoUpdateTime:false;autoCreateTime:false"`
	Status    string    `gorm:"index;size:20"`
	Payload   []byte
}

type monitorEvent struct {
	RunID    string `gorm:"primaryKey;size:80"`
	Sequence int    `gorm:"primaryKey;autoIncrement:false"`
	Payload  []byte
}

// NewPersistent reuses the host database; IDs remain unique across restarts.
// Retention is measured from the start of each run, in 24-hour days.
func NewPersistent(db *gorm.DB, limit, days int) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("monitor database is required")
	}
	if days < 0 || days > 36500 {
		return nil, fmt.Errorf("monitor.retention_days must be 0..36500")
	}
	if days == 0 {
		days = 7
	}
	if err := db.AutoMigrate(&monitorRun{}, &monitorEvent{}); err != nil {
		return nil, err
	}
	s := New(limit)
	s.db, s.retention = db, time.Duration(days)*24*time.Hour
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	s.session = hex.EncodeToString(nonce[:])
	if err := s.cleanup(time.Now()); err != nil {
		return nil, err
	}
	if err := s.backfillMetrics(); err != nil {
		return nil, err
	}
	// A previous process cannot still be executing these runs. Preserve its last
	// durable observation instead of displaying an endlessly running operation.
	var unfinished []monitorRun
	if err := db.Where("status = ?", "running").Find(&unfinished).Error; err != nil {
		return nil, err
	}
	for _, row := range unfinished {
		var r Run
		if err := json.Unmarshal(row.Payload, &r); err != nil {
			return nil, err
		}
		r.Status, r.Error = "error", "进程中断：运行未完成，已恢复最后保存的记录"
		r.Summary = r.Error
		r.DurationMS = max(0, row.UpdatedAt.Sub(r.StartedAt).Milliseconds())
		payload, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		if err := db.Model(&monitorRun{}).Where("id = ?", row.ID).Updates(map[string]any{"status": r.Status, "payload": payload}).Error; err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Upgrade old summaries once, without loading event bodies on every list request.
func (s *Store) backfillMetrics() error {
	var rows []monitorRun
	if err := s.db.Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var r Run
		if err := json.Unmarshal(row.Payload, &r); err != nil {
			return err
		}
		if r.MetricsVersion >= 1 {
			continue
		}
		var events []monitorEvent
		if err := s.db.Where("run_id = ?", row.ID).Order("sequence ASC").Find(&events).Error; err != nil {
			return err
		}
		r.ModelCalls, r.ToolSkillCount, r.MetricsVersion = 0, 0, 1
		for _, item := range events {
			var e agent.Event
			if err := json.Unmarshal(item.Payload, &e); err != nil {
				return err
			}
			r.countEvent(e)
		}
		payload, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if err := s.db.Model(&monitorRun{}).Where("id = ?", row.ID).Update("payload", payload).Error; err != nil {
			return err
		}
	}
	return nil
}

// Called under mu. Append new events and update summary atomically; failed
// writes keep their pending events in memory and retry on the next observation.
func (s *Store) persist(r *Run) error {
	copy := *r
	copy.Events = nil
	payload, err := json.Marshal(copy)
	if err != nil {
		return err
	}
	updated := r.StartedAt
	if len(r.Events) > 0 {
		updated = r.Events[len(r.Events)-1].Time
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		row := monitorRun{ID: r.ID, StartedAt: r.StartedAt, UpdatedAt: updated, Status: r.Status, Payload: payload}
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			return err
		}
		for i := r.persisted; i < len(r.Events); i++ {
			data, err := json.Marshal(r.Events[i])
			if err != nil {
				return err
			}
			if err := tx.Create(&monitorEvent{RunID: r.ID, Sequence: i, Payload: data}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		r.persisted = len(r.Events)
	}
	return err
}

func (s *Store) readSnapshot(id string) ([]Run, error) {
	if s.db == nil {
		return s.snapshot(id), nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	cutoff := time.Now().Add(-s.retention)
	query := s.db.Where("started_at > ?", cutoff)
	if id != "" {
		query = query.Where("id = ?", id)
	}
	var rows []monitorRun
	if err := query.Order("started_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]Run, len(rows))
	for _, row := range rows {
		var r Run
		if err := json.Unmarshal(row.Payload, &r); err != nil {
			return nil, err
		}
		if id != "" {
			var events []monitorEvent
			if err := s.db.Where("run_id = ?", id).Order("sequence ASC").Find(&events).Error; err != nil {
				return nil, err
			}
			for _, item := range events {
				var e agent.Event
				if err := json.Unmarshal(item.Payload, &e); err != nil {
					return nil, err
				}
				r.Events = append(r.Events, e)
			}
		}
		byID[r.ID] = r
	}
	for _, r := range s.runs {
		if !r.StartedAt.After(cutoff) || (id != "" && id != r.ID) {
			continue
		}
		copy := *r
		copy.Events = nil
		if id != "" {
			copy.Events = append([]agent.Event(nil), r.Events...)
		}
		if copy.Status == "running" {
			copy.DurationMS = time.Since(copy.StartedAt).Milliseconds()
		}
		byID[r.ID] = copy
	}
	out := make([]Run, 0, len(byID))
	for _, r := range byID {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

func (s *Store) cleanup(now time.Time) error {
	cutoff := now.Add(-s.retention)
	return s.db.Transaction(func(tx *gorm.DB) error {
		ids := tx.Model(&monitorRun{}).Select("id").Where("started_at <= ?", cutoff)
		if err := tx.Where("run_id IN (?)", ids).Delete(&monitorEvent{}).Error; err != nil {
			return err
		}
		return tx.Where("started_at <= ?", cutoff).Delete(&monitorRun{}).Error
	})
}

func (s *Store) maintain(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.runs {
		if !r.StartedAt.After(now.Add(-s.retention)) {
			delete(s.runs, id)
			continue
		}
		if r.persisted < len(r.Events) {
			if err := s.persist(r); err != nil {
				return err
			}
		}
	}
	s.trimCache()
	return s.cleanup(now)
}

// Active runs and writes awaiting retry may temporarily exceed the cache limit.
func (s *Store) trimCache() {
	for len(s.runs) > s.limit {
		var oldest uint64
		var found bool
		for id, r := range s.runs {
			if r.Status != "running" && r.persisted == len(r.Events) && (!found || r.StartedAt.Before(s.runs[oldest].StartedAt)) {
				oldest, found = id, true
			}
		}
		if !found {
			return
		}
		delete(s.runs, oldest)
	}
}

// StartMaintenance is called only after the HTTP listener starts successfully.
func (s *Store) StartMaintenance() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || s.stop != nil {
		return
	}
	s.stop, s.done = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case now := <-ticker.C:
				if err := s.maintain(now); err != nil {
					logrus.Errorf("[ChatAI][Monitor] maintenance: %v", err)
				}
			}
		}
	}()
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.stop != nil {
			close(s.stop)
			<-s.done
		}
	})
	return s.maintain(time.Now())
}
