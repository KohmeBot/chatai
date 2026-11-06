package agent

import "fmt"

type ToolPreference struct {
	GroupID int64  `gorm:"primaryKey"`
	Name    string `gorm:"primaryKey;size:256"`
}

func (ToolPreference) TableName() string { return "chatai_disabled_tools" }

// Serialize persistence with runtime state changes.
func (r *Registry) SetDisabled(name string, disabled bool, save func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return fmt.Errorf("未找到工具，或该工具不支持禁用")
	}
	if save != nil {
		if err := save(); err != nil {
			return err
		}
	}
	r.disabled[name] = disabled
	return nil
}

// RestoreDisabled also retains choices for plugins registered later during startup.
func (r *Registry) RestoreDisabled(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disabled[name] = true
}
