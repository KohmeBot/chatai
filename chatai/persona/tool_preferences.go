package persona

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/agent"
	"gorm.io/gorm/clause"
)

func (p *Persona) SetToolDisabled(name string, disabled bool) error {
	if p.db == nil {
		return fmt.Errorf("数据库不可用")
	}
	return p.tools.SetDisabled(name, disabled, func() error {
		row := agent.ToolPreference{GroupID: p.groupID, Name: name}
		if disabled {
			return p.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
		}
		return p.db.Delete(&row).Error
	})
}
