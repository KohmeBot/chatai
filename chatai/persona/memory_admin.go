package persona

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kohmebot/chatai/chatai/favor"
	"gorm.io/gorm"
)

// MemoryEdit supports manual additions to existing profiles. Identity and
// provenance of existing items remain immutable.
type MemoryEdit struct {
	Target      string     `json:"target"`
	UserID      string     `json:"user_id"`
	Version     uint64     `json:"version"`
	Key         string     `json:"key"`
	Text        string     `json:"text"`
	Kind        string     `json:"kind"`
	Confidence  string     `json:"confidence"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Summary     string     `json:"summary"`
	Score       int64      `json:"score"`
	Description string     `json:"description"`
	Reason      string     `json:"reason"`
	Previous    string     `json:"previous"`
}

func (p *Persona) EditMemory(userID int64, edit MemoryEdit, remove bool) error {
	return p.db.Transaction(func(tx *gorm.DB) error {
		if edit.Target == "group" {
			if strings.TrimSpace(edit.Previous) == "" {
				return errors.New("群印象不存在，不能添加")
			}
			if !remove && (strings.TrimSpace(edit.Text) == "" || utf8.RuneCountInString(edit.Text) > 4000) {
				return errors.New("群印象须为 1–4000 字")
			}
			query := tx.Model(&GroupImpression{}).Where("group_id = ? AND content = ?", p.groupID, edit.Previous)
			var result *gorm.DB
			if remove {
				result = query.Delete(&GroupImpression{})
			} else {
				result = query.Update("content", edit.Text)
			}
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("群印象已变化，请刷新后重试")
			}
			return nil
		}
		profile, err := readUserMemory(tx, userID)
		if err != nil {
			return err
		}
		if profile.Version == 0 || profile.Version != edit.Version {
			return errors.New("记忆不存在或版本已变化，请刷新后重试")
		}
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
				// Keep a versioned tombstone so migration/backlogged maintenance cannot
				// recreate the old profile, or accept an already in-flight write.
				profile.Summary, profile.Items, profile.Relationship = "", []MemoryItem{}, RelationshipMemory{}
			} else {
				if utf8.RuneCountInString(edit.Summary) > 600 || utf8.RuneCountInString(edit.Description) > 200 || utf8.RuneCountInString(edit.Reason) > 200 || edit.Score < favor.FavorMin || edit.Score > favor.FavorMax {
					return errors.New("简介最多600字，关系描述和原因最多200字，好感度超出范围")
				}
				profile.Summary = edit.Summary
				profile.Relationship = RelationshipMemory{Score: edit.Score, Description: edit.Description, Reason: edit.Reason}
			}
		case "item":
			visible := visibleUserProfile(profile, p.groupID, time.Now(), int(^uint(0)>>1))
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
		if err := tx.Where("user_id = ?", userID).Order("id DESC").Limit(1).Find(&latest).Error; err != nil {
			return err
		}
		profile.ManualThrough = max(profile.ManualThrough, latest.ID)
		return writeUserMemory(tx, profile)
	})
}
