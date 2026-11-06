package skill

import (
	"fmt"
	"gorm.io/gorm/clause"
)

type Disabled struct {
	Key string `gorm:"primaryKey;size:128"`
}

func (Disabled) TableName() string { return "chatai_disabled_skills" }
func (s *Service) DisabledKeys() (map[string]bool, error) {
	keys := make(map[string]bool)
	if s == nil || s.db == nil {
		return keys, nil
	}
	var rows []Disabled
	if err := s.db.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		keys[row.Key] = true
	}
	return keys, nil
}
func (s *Service) CheckEnabled(name string) error {
	keys, err := s.DisabledKeys()
	if err != nil {
		return err
	}
	if keys[ResidentKey(name, 0)] {
		return fmt.Errorf("Skill 已禁用")
	}
	return nil
}
func (s *Service) SetDisabled(groupID int64, name string, id uint, disabled bool) error {
	if s == nil || !s.opts.Enabled || s.db == nil {
		return fmt.Errorf("Skills 未启用")
	}
	if (id == 0) == (name == "") {
		return fmt.Errorf("必须指定技能名称或记录 ID，不能同时指定")
	}
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
			}
		}
		if !found {
			return fmt.Errorf("未找到 Skill")
		}
	}
	row := Disabled{Key: ResidentKey(name, id)}
	if disabled {
		return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	}
	return s.db.Delete(&row).Error
}

func (s *Service) ListMetadata() ([]Metadata, error) {
	keys, err := s.DisabledKeys()
	if err != nil {
		return nil, err
	}
	items, err := s.Directory.List()
	if err != nil {
		return nil, err
	}
	filtered := items[:0]
	for _, item := range items {
		if !keys[ResidentKey(item.Name, 0)] {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}
