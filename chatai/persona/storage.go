package persona

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChatMessageRecord 持久化群聊上下文，不再在 Persona 内保存消息切片。
type ChatMessageRecord struct {
	ID             uint  `gorm:"primaryKey"`
	GroupID        int64 `gorm:"index:idx_chat_group_created"`
	UserID         int64 `gorm:"index:idx_chat_group_user_created"`
	UserNickname   string
	TargetUserID   int64
	TargetNickname string
	Content        string
	MsgType        string
	MessageID      int64 `gorm:"index"`
	URL            string
	FileName       string
	Referred       bool
	CreatedAt      time.Time `gorm:"index:idx_chat_group_created;index:idx_chat_group_user_created"`
}

type RepeatState struct {
	GroupID   int64 `gorm:"primaryKey"`
	Signature string
	Triggered bool
	UpdatedAt time.Time
}

type ImpressionCursor struct {
	GroupID       int64 `gorm:"primaryKey"`
	LastMessageID uint
	UpdatedAt     time.Time
}

// MessageTimeRange 使用左闭右开区间 [Start, End)，便于无重叠地组合相邻时间段。
type MessageTimeRange struct {
	Start time.Time
	End   time.Time
}

func messageRecord(groupID int64, msg GroupMessage) ChatMessageRecord {
	return ChatMessageRecord{GroupID: groupID, UserID: msg.User.UserId, UserNickname: msg.User.Nickname,
		TargetUserID: msg.TargetUser.UserId, TargetNickname: msg.TargetUser.Nickname, Content: msg.Content,
		MsgType: msg.MsgType, MessageID: msg.MsgID, URL: msg.Url, FileName: msg.FileName, Referred: msg.Refer, CreatedAt: msg.CreatedAt}
}

func (r ChatMessageRecord) message() GroupMessage {
	return GroupMessage{User: User{UserId: r.UserID, Nickname: r.UserNickname}, TargetUser: User{UserId: r.TargetUserID, Nickname: r.TargetNickname},
		Content: r.Content, MsgType: r.MsgType, MsgID: r.MessageID, CreatedAt: r.CreatedAt, Url: r.URL, FileName: r.FileName, Refer: r.Referred}
}

func (p *Persona) saveMessage(msg GroupMessage) error {
	record := messageRecord(p.groupID, msg)
	return p.db.Create(&record).Error
}

func (p *Persona) recentMessages(limit int) ([]GroupMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	var rows []ChatMessageRecord
	err := p.db.Where("group_id = ?", p.groupID).Order("id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]GroupMessage, len(rows))
	for i := range rows {
		result[len(rows)-1-i] = rows[i].message()
	}
	return result, nil
}

func (p *Persona) recentUserMessages(userID int64, limit int) ([]GroupMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	var rows []ChatMessageRecord
	err := p.db.Where("group_id = ? AND user_id = ?", p.groupID, userID).Order("id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]GroupMessage, len(rows))
	for i := range rows {
		result[len(rows)-1-i] = rows[i].message()
	}
	return result, nil
}

// messagesInTimeRanges 从持久化消息中查询一个或多个时间段，去重后按时间正序返回。
// userID 为 0 时查询全群，否则只返回指定用户的消息。
func (p *Persona) messagesInTimeRanges(ranges []MessageTimeRange, userID int64, limit int) ([]GroupMessage, error) {
	if len(ranges) == 0 || limit <= 0 {
		return nil, nil
	}
	unique := make(map[uint]ChatMessageRecord)
	for _, item := range ranges {
		query := p.db.Where("group_id = ? AND created_at >= ? AND created_at < ?", p.groupID, item.Start, item.End)
		if userID > 0 {
			query = query.Where("user_id = ?", userID)
		}
		var rows []ChatMessageRecord
		if err := query.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			unique[row.ID] = row
		}
	}
	rows := make([]ChatMessageRecord, 0, len(unique))
	for _, row := range unique {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	messages := make([]GroupMessage, len(rows))
	for i := range rows {
		messages[i] = rows[i].message()
	}
	return messages, nil
}

func (p *Persona) impressionMessages(limit int) ([]ChatMessageRecord, ImpressionCursor, error) {
	var cursor ImpressionCursor
	err := p.db.Where(ImpressionCursor{GroupID: p.groupID}).First(&cursor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cursor = ImpressionCursor{GroupID: p.groupID}
		err = nil
	}
	if err != nil {
		return nil, cursor, err
	}
	var rows []ChatMessageRecord
	err = p.db.Where("group_id = ? AND id > ?", p.groupID, cursor.LastMessageID).Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, cursor, err
}

func (p *Persona) advanceImpressionCursor(cursor ImpressionCursor, lastID uint) error {
	cursor.LastMessageID = lastID
	return p.db.Where(ImpressionCursor{GroupID: p.groupID}).Assign(cursor).FirstOrCreate(&cursor).Error
}

func repeatSignature(msg GroupMessage) string {
	payload, _ := json.Marshal([]any{msg.MsgType, msg.Content, msg.TargetUser.UserId, msg.Url, msg.FileName})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func repeatable(msg GroupMessage) bool {
	switch msg.MsgType {
	case MsgTypeText, MsgTypeImg, MsgTypeAt, MsgTypeReply:
		return true
	default:
		return false
	}
}

// shouldRepeat 使用数据库中的最近消息和状态判断，重启后也不会丢失复读轮次。
func (p *Persona) shouldRepeat(msg GroupMessage, threshold int) (bool, error) {
	if threshold < 2 {
		threshold = 3
	}
	returnValue := false
	err := p.db.Transaction(func(tx *gorm.DB) error {
		var state RepeatState
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(RepeatState{GroupID: p.groupID}).First(&state).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			state = RepeatState{GroupID: p.groupID}
			err = nil
		}
		if err != nil {
			return err
		}
		signature := repeatSignature(msg)
		if state.Signature != signature {
			state.Signature, state.Triggered = signature, false
		}
		if repeatable(msg) && !state.Triggered {
			var rows []ChatMessageRecord
			if err := tx.Where("group_id = ?", p.groupID).Order("id DESC").Limit(threshold).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == threshold {
				matched := true
				for _, row := range rows {
					if repeatSignature(row.message()) != signature {
						matched = false
						break
					}
				}
				if matched {
					state.Triggered, returnValue = true, true
				}
			}
		}
		return tx.Where(RepeatState{GroupID: p.groupID}).Assign(state).FirstOrCreate(&state).Error
	})
	return returnValue, err
}
