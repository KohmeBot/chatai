package skill

import (
	"fmt"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm/clause"
)

// Residency is an administrator preference, separate from skill content.
// Directory skills use their name; database skills use their stable record ID.
type Residency struct {
	Key string `gorm:"primaryKey;size:128"`
}

func (Residency) TableName() string { return "chatai_skill_residencies" }

func ResidentKey(name string, id uint) string {
	if id != 0 {
		return fmt.Sprintf("record:%d", id)
	}
	return "directory:" + name
}

func (s *Service) ResidentKeys() (map[string]bool, error) {
	keys := make(map[string]bool)
	if s == nil || s.db == nil {
		return keys, nil
	}
	var rows []Residency
	if err := s.db.Order("key").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		keys[row.Key] = true
	}
	return keys, nil
}

func (s *Service) SetResident(groupID int64, name string, id uint, enabled bool) error {
	if s == nil || !s.opts.Enabled || s.db == nil {
		return fmt.Errorf("Skills 未启用")
	}
	if (id == 0) == (name == "") {
		return fmt.Errorf("必须指定技能名称或记录 ID，不能同时指定")
	}
	if enabled {
		if id != 0 {
			if _, err := s.residentRecord(groupID, id); err != nil {
				return err
			}
		} else {
			if s.Directory == nil {
				return fmt.Errorf("Skills 目录未启用")
			}
			items, err := s.Directory.List()
			if err != nil {
				return err
			}
			found := false
			for _, item := range items {
				if item.Name == name {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("未找到可用 Skill")
			}
			if _, err := s.Directory.Read(name, "SKILL.md"); err != nil {
				return err
			}
		}
	}
	row := Residency{Key: ResidentKey(name, id)}
	if !enabled {
		return s.db.Delete(&row).Error
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func (s *Service) residentRecord(groupID int64, id uint) (Record, error) {
	var row Record
	err := s.db.Where("id = ? AND status = ? AND (scope = ? OR (scope = ? AND group_id = ?))", id, StatusActive, ScopeGlobal, ScopeGroup, groupID).First(&row).Error
	if err == nil && row.Source == SourceGenerated && !row.ExpiresAt.After(time.Now()) {
		err = fmt.Errorf("Skill 已过期")
	}
	return row, err
}

// ResidentSkills reloads current content for every run; stale selections cannot
// revive disabled, expired, deleted, or out-of-scope skills.
func (s *Service) ResidentSkills(groupID int64) ([]agent.SkillMatch, error) {
	if s == nil || !s.opts.Enabled || s.db == nil {
		return nil, nil
	}
	keys, err := s.ResidentKeys()
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}
	var result []agent.SkillMatch
	if s.Directory != nil {
		items, err := s.Directory.List()
		if err != nil {
			logrus.Warnf("[Skill][常驻目录加载失败] %v", err)
		}
		for _, item := range items {
			if !keys[ResidentKey(item.Name, 0)] {
				continue
			}
			body, err := s.Directory.Read(item.Name, "SKILL.md")
			if err != nil {
				logrus.Warnf("[Skill][常驻读取失败] %s: %v", item.Name, err)
				continue
			}
			result = append(result, agent.SkillMatch{Name: item.Name, Path: item.Path, Markdown: body})
		}
	}
	var rows []Record
	if err := s.db.Where("status = ? AND (scope = ? OR (scope = ? AND group_id = ?))", StatusActive, ScopeGlobal, ScopeGroup, groupID).Order("id").Find(&rows).Error; err != nil {
		return result, err
	}
	for _, row := range rows {
		if !keys[ResidentKey("", row.ID)] || (row.Source == SourceGenerated && !row.ExpiresAt.After(time.Now())) {
			continue
		}
		result = append(result, agent.SkillMatch{ID: row.ID, Name: row.Name, Markdown: row.MarkdownText(), RequiredTools: row.RequiredTools()})
	}
	return result, nil
}
