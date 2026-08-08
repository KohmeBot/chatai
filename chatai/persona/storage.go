package persona

import (
	"sort"
	"time"
)

// ChatMessageRecord 持久化群聊上下文；复读窗口不依赖此表。
type ChatMessageRecord struct {
	ID              uint  `gorm:"primaryKey"`
	GroupID         int64 `gorm:"index:idx_chat_group_created"`
	UserID          int64 `gorm:"index:idx_chat_group_user_created"`
	UserNickname    string
	TargetUserID    int64
	TargetNickname  string
	QuotedUserID    int64
	QuotedNickname  string
	Content         string
	QuotedContent   string
	MsgType         string
	MessageID       int64 `gorm:"index"`
	URL             string
	FileName        string
	QuotedMessageID int64
	QuotedURL       string
	Referred        bool
	CreatedAt       time.Time `gorm:"index:idx_chat_group_created;index:idx_chat_group_user_created"`
}

// MessageTimeRange 使用左闭右开区间 [Start, End)，便于无重叠地组合相邻时间段。
type MessageTimeRange struct {
	Start time.Time
	End   time.Time
}

// ContextMessageQuery 描述最近上下文的可选筛选与分页条件。
// 查询始终从最新消息向前取一页，返回值再恢复为时间正序，便于模型阅读。
type ContextMessageQuery struct {
	UserID  int64
	Start   *time.Time
	End     *time.Time
	Keyword string
	Offset  int
	Limit   int
}

func messageRecord(groupID int64, msg GroupMessage) ChatMessageRecord {
	return ChatMessageRecord{GroupID: groupID, UserID: msg.User.UserId, UserNickname: msg.User.Nickname,
		TargetUserID: msg.TargetUser.UserId, TargetNickname: msg.TargetUser.Nickname, Content: msg.Content,
		QuotedUserID: msg.QuotedUser.UserId, QuotedNickname: msg.QuotedUser.Nickname, QuotedContent: msg.QuotedContent,
		MsgType: msg.MsgType, MessageID: msg.MsgID, QuotedMessageID: msg.QuotedMsgID, URL: msg.Url, FileName: msg.FileName,
		QuotedURL: msg.QuotedURL, Referred: msg.Refer, CreatedAt: msg.CreatedAt}
}

func (r ChatMessageRecord) message() GroupMessage {
	return GroupMessage{User: User{UserId: r.UserID, Nickname: r.UserNickname}, TargetUser: User{UserId: r.TargetUserID, Nickname: r.TargetNickname},
		QuotedUser: User{UserId: r.QuotedUserID, Nickname: r.QuotedNickname}, Content: r.Content, QuotedContent: r.QuotedContent,
		MsgType: r.MsgType, MsgID: r.MessageID, QuotedMsgID: r.QuotedMessageID, CreatedAt: r.CreatedAt, Url: r.URL,
		FileName: r.FileName, QuotedURL: r.QuotedURL, Refer: r.Referred}
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

func (p *Persona) queryContextMessages(input ContextMessageQuery) ([]GroupMessage, bool, error) {
	if input.Limit <= 0 {
		return nil, false, nil
	}
	query := p.db.Where("group_id = ?", p.groupID)
	if input.UserID > 0 {
		query = query.Where("user_id = ?", input.UserID)
	}
	if input.Start != nil {
		query = query.Where("created_at >= ?", *input.Start)
	}
	if input.End != nil {
		query = query.Where("created_at < ?", *input.End)
	}
	if input.Keyword != "" {
		query = query.Where("content LIKE ?", "%"+input.Keyword+"%")
	}
	var rows []ChatMessageRecord
	if err := query.Order("id DESC").Offset(input.Offset).Limit(input.Limit + 1).Find(&rows).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > input.Limit
	if hasMore {
		rows = rows[:input.Limit]
	}
	result := make([]GroupMessage, len(rows))
	for i := range rows {
		result[len(rows)-1-i] = rows[i].message()
	}
	return result, hasMore, nil
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

func repeatable(msg GroupMessage) bool {
	switch msg.MsgType {
	case MsgTypeText, MsgTypeImg, MsgTypeAt, MsgTypeReply:
		return true
	default:
		return false
	}
}

// shouldRepeat 只使用当前 Persona 的内存滑动窗口判断连续相同消息。
func (p *Persona) shouldRepeat(msg GroupMessage, threshold int) bool {
	if threshold < 2 {
		threshold = 3
	}
	p.repeatMu.Lock()
	defer p.repeatMu.Unlock()

	if len(p.repeatWindow) > 0 && !p.repeatWindow[len(p.repeatWindow)-1].ContentEqual(msg) {
		p.repeatTriggered = false
	}
	p.repeatWindow = append(p.repeatWindow, msg)
	if len(p.repeatWindow) > threshold {
		p.repeatWindow = p.repeatWindow[len(p.repeatWindow)-threshold:]
	}
	if !repeatable(msg) || p.repeatTriggered || len(p.repeatWindow) < threshold {
		return false
	}
	first := p.repeatWindow[0]
	for _, item := range p.repeatWindow[1:] {
		if !first.ContentEqual(item) {
			return false
		}
	}
	p.repeatTriggered = true
	return true
}
