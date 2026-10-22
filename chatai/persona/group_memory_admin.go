package persona

import (
	"errors"
	"gorm.io/gorm"
	"strings"
	"time"
	"unicode/utf8"
)

func (p *Persona) editGroupMemory(tx *gorm.DB, edit MemoryEdit, remove bool) error {
	profile, err := readGroupMemory(tx, p.groupID)
	if err != nil {
		return err
	}
	if edit.Target == "group" {
		return errors.New("旧群印象接口已停用，请刷新后使用群记忆")
	}
	if profile.Version != edit.Version {
		return errors.New("群记忆已变化，请刷新后重试")
	}
	edit.Target = strings.TrimPrefix(edit.Target, "group_")
	switch edit.Target {
	case "item_create":
		if remove {
			return errors.New("新增条目不支持删除操作")
		}
		switch edit.Kind {
		case "fact", "preference", "boundary", "state", "experience":
		default:
			return errors.New("无效记忆类型")
		}
		if strings.TrimSpace(edit.Key) == "" || utf8.RuneCountInString(edit.Key) > 64 || strings.TrimSpace(edit.Text) == "" || utf8.RuneCountInString(edit.Text) > 200 {
			return errors.New("条目键须为 1–64 字，内容须为 1–200 字")
		}
		if edit.Confidence != "explicit" && edit.Confidence != "inferred" {
			return errors.New("无效可信度")
		}
		now := time.Now()
		if edit.ExpiresAt != nil && !edit.ExpiresAt.After(now) {
			return errors.New("到期时间须晚于当前时间")
		}
		if edit.Kind == "state" && edit.ExpiresAt == nil {
			return errors.New("近期状态必须有到期时间")
		}
		for _, item := range profile.Items {
			if item.Key == edit.Key {
				return errors.New("条目键已存在，请使用其他键")
			}
		}
		if len(profile.Items) >= 30 {
			return errors.New("最多保存30条结构化记忆，请先删除过时条目")
		}
		// SourceID zero denotes a manual addition, without invented chat evidence.
		profile.Items = append(profile.Items, MemoryItem{Key: edit.Key, Kind: edit.Kind, Text: edit.Text, Confidence: edit.Confidence, ExpiresAt: edit.ExpiresAt, GroupID: p.groupID, ObservedAt: now})
	case "profile":
		if remove {
			profile.Summary = ""
			profile.Items = []MemoryItem{}
		} else {
			if utf8.RuneCountInString(edit.Summary) > 600 {
				return errors.New("群简介最多600字")
			}
			profile.Summary = edit.Summary
		}
	case "item":
		visible := visibleGroupProfile(profile, time.Now(), 30)
		found := false
		for _, item := range visible.Items {
			if item.Key == edit.Key {
				found = true
			}
		}
		if !found {
			return errors.New("记忆条目不存在、已过期或不属于当前群")
		}
		for i := range profile.Items {
			item := &profile.Items[i]
			if item.Key != edit.Key {
				continue
			}
			if remove {
				profile.Items = append(profile.Items[:i], profile.Items[i+1:]...)
				break
			}
			// Changing kind could move a private group state into shared memory.
			if edit.Kind != item.Kind {
				return errors.New("不能修改条目类型及共享范围")
			}
			if strings.TrimSpace(edit.Text) == "" || utf8.RuneCountInString(edit.Text) > 200 {
				return errors.New("条目内容须为 1–200 字")
			}
			if edit.Confidence != "explicit" && edit.Confidence != "inferred" {
				return errors.New("无效可信度")
			}
			if edit.ExpiresAt != nil && !edit.ExpiresAt.After(time.Now()) {
				return errors.New("到期时间须晚于当前时间")
			}
			if item.Kind == "state" && edit.ExpiresAt == nil {
				return errors.New("近期状态必须有到期时间")
			}
			item.Text, item.Confidence, item.ExpiresAt = edit.Text, edit.Confidence, edit.ExpiresAt
			break
		}
	default:
		return errors.New("不支持的记忆操作")
	}
	var latest ChatMessageRecord
	if err := tx.Where("group_id = ?", p.groupID).Order("id DESC").Limit(1).Find(&latest).Error; err != nil {
		return err
	}
	profile.ManualThrough = max(profile.ManualThrough, latest.ID)
	return writeGroupMemory(tx, profile)
}
