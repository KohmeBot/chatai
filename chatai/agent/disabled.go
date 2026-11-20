package agent

import (
	"fmt"
	"maps"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ToolPreference struct {
	GroupID int64  `gorm:"primaryKey"`
	Name    string `gorm:"primaryKey;size:256"`
}

func (ToolPreference) TableName() string { return "chatai_disabled_tools" }

// ToolPreferences holds the enabled state shared by all of a plugin's registries.
// Readers see an immutable snapshot while a preference is being persisted.
type ToolPreferences struct {
	db       *gorm.DB
	mu       sync.Mutex
	disabled atomic.Pointer[map[string]bool]
}

// LoadToolPreferences consolidates legacy group preferences into the global
// scope. A tool disabled in any group stays disabled after the migration.
func LoadToolPreferences(db *gorm.DB) (*ToolPreferences, error) {
	preferences := &ToolPreferences{db: db}
	if db == nil {
		return preferences, nil
	}
	disabled := make(map[string]bool)
	err := db.Transaction(func(tx *gorm.DB) error {
		var rows []ToolPreference
		if err := tx.Find(&rows).Error; err != nil {
			return err
		}
		legacy := make(map[string]bool)
		for _, row := range rows {
			disabled[row.Name] = true
			if row.GroupID != 0 {
				legacy[row.Name] = true
			}
		}
		if len(legacy) == 0 {
			return nil
		}
		globalRows := make([]ToolPreference, 0, len(legacy))
		for name := range legacy {
			globalRows = append(globalRows, ToolPreference{Name: name})
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(globalRows, 100).Error; err != nil {
			return err
		}
		return tx.Where("group_id <> 0").Delete(&ToolPreference{}).Error
	})
	if err != nil {
		return nil, err
	}
	preferences.disabled.Store(&disabled)
	return preferences, nil
}

func (p *ToolPreferences) snapshot() map[string]bool {
	if p != nil {
		if names := p.disabled.Load(); names != nil {
			return *names
		}
	}
	return nil
}

func (p *ToolPreferences) save(name string, disabled bool) error {
	if disabled {
		row := ToolPreference{Name: name}
		return p.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	}
	return p.db.Where("group_id = 0 AND name = ?", name).Delete(&ToolPreference{}).Error
}

func (p *ToolPreferences) update(name string, disabled bool, save func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if save != nil {
		if err := save(); err != nil {
			return err
		}
	}
	current := p.snapshot()
	next := make(map[string]bool, len(current)+1)
	maps.Copy(next, current)
	if disabled {
		next[name] = true
	} else {
		delete(next, name)
	}
	p.disabled.Store(&next)
	return nil
}

// SetDisabled persists shared preferences before publishing a runtime change.
func (r *Registry) SetDisabled(name string, disabled bool, save func() error) error {
	r.mu.RLock()
	_, registered := r.tools[name]
	r.mu.RUnlock()
	if !registered {
		return fmt.Errorf("未找到工具，或该工具不支持禁用")
	}
	if r.preferences.db != nil {
		save = func() error { return r.preferences.save(name, disabled) }
	}
	return r.preferences.update(name, disabled, save)
}

// RestoreDisabled also retains choices for plugins registered later during startup.
func (r *Registry) RestoreDisabled(name string) {
	_ = r.preferences.update(name, true, nil)
}
