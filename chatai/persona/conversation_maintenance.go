package persona

import (
	"context"
	"errors"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PendingMemoryMessage only references actual visible Agent dialogue, never a
// range of group history. Keeping references durable also preserves retries.
type PendingMemoryMessage struct {
	GroupID   int64 `gorm:"primaryKey"`
	OwnerID   int64 `gorm:"primaryKey"`
	SourceID  uint  `gorm:"primaryKey"`
	SelfID    int64
	ExpiresAt time.Time `gorm:"index"`
}

type conversationKey struct {
	GroupID int64
	OwnerID int64
}

func (m *MemoryMaintainer) conversationMode() bool {
	return m != nil && !m.ObserveGroupChat
}

func (m *MemoryMaintainer) beginConversation(groupID, userID int64) func() {
	if !m.conversationMode() {
		return func() {}
	}
	key := conversationKey{groupID, userID}
	m.mu.Lock()
	m.active[key]++
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.active[key]--
		if m.active[key] == 0 {
			delete(m.active, key)
		}
	}
}

func (m *MemoryMaintainer) conversationActive(key conversationKey) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[key] > 0
}

func (m *MemoryMaintainer) stageConversation(groupID int64, rc *agent.RunContext, sources []dialogueSource, expires time.Time) error {
	if !m.conversationMode() {
		return nil
	}
	selfID, _ := rc.Values["self_user_id"].(int64)
	if groupID <= 0 || rc.UserID <= 0 || selfID <= 0 {
		return nil
	}
	return m.db.Transaction(func(tx *gorm.DB) error {
		// A continuation extends all unprocessed messages for this conversation.
		if err := tx.Model(&PendingMemoryMessage{}).
			Where("group_id = ? AND owner_id = ? AND expires_at < ?", groupID, rc.UserID, expires).
			Update("expires_at", expires).Error; err != nil {
			return err
		}
		for _, source := range sources {
			var row ChatMessageRecord
			err := tx.Where("group_id = ? AND user_id = ? AND message_id = ?", groupID, source.UserID, source.MessageID).
				Order("id DESC").First(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// A failed persistence must never acquire evidence from unrelated chat.
				continue
			}
			if err != nil {
				return err
			}
			pending := PendingMemoryMessage{GroupID: groupID, OwnerID: rc.UserID, SourceID: row.ID, SelfID: selfID, ExpiresAt: expires}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&pending).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Called by the worker even if no further group messages ever arrive.
func (m *MemoryMaintainer) processConversations(ctx context.Context, now time.Time) error {
	db := m.db.WithContext(ctx)
	var keys []conversationKey
	if err := db.Model(&PendingMemoryMessage{}).Select("group_id, owner_id").
		Group("group_id, owner_id").Having("MAX(expires_at) <= ?", now).
		Order("group_id, owner_id").Find(&keys).Error; err != nil {
		return err
	}
	for _, key := range keys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.conversationActive(key) {
			continue
		}
		if err := m.processConversation(ctx, key, now); err != nil {
			logrus.Warnf("[Memory][group=%d user=%d] 对话记忆维护失败，保留待处理消息: %v", key.GroupID, key.OwnerID, err)
		}
	}
	return nil
}

func (m *MemoryMaintainer) processConversation(ctx context.Context, key conversationKey, now time.Time) error {
	db := m.db.WithContext(ctx)
	var pending []PendingMemoryMessage
	if err := db.Where("group_id = ? AND owner_id = ?", key.GroupID, key.OwnerID).
		Order("source_id ASC").Limit(m.batch).Find(&pending).Error; err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(pending))
	for _, item := range pending {
		ids = append(ids, item.SourceID)
	}
	var rows []ChatMessageRecord
	if err := db.Where("group_id = ? AND id IN ?", key.GroupID, ids).Order("id ASC").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return m.processRows(ctx, key.GroupID, pending[0].SelfID, rows, func(tx *gorm.DB, consumed []ChatMessageRecord) error {
		if m.conversationActive(key) {
			return errors.New("conversation resumed; defer memory update")
		}
		var extended int64
		if err := tx.Model(&PendingMemoryMessage{}).
			Where("group_id = ? AND owner_id = ? AND expires_at > ?", key.GroupID, key.OwnerID, now).
			Count(&extended).Error; err != nil {
			return err
		}
		if extended > 0 {
			return errors.New("conversation expiry changed")
		}
		consumedIDs := make([]uint, 0, len(consumed))
		for _, row := range consumed {
			consumedIDs = append(consumedIDs, row.ID)
		}
		result := tx.Where("group_id = ? AND owner_id = ? AND source_id IN ?", key.GroupID, key.OwnerID, consumedIDs).
			Delete(&PendingMemoryMessage{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(consumedIDs)) {
			return errors.New("pending conversation changed")
		}
		return nil
	})
}
