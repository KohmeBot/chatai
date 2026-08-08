package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
)

func (p *Persona) contextTools() []agent.Tool {
	properties := contextToolProperties()
	userProperties := cloneProperties(properties)
	userProperties["user_id"] = integerProperty("用户 QQ 号")

	return []agent.Tool{
		{
			Definition:  agent.Function("read_group_context", "按最近时段、绝对时间、关键词和分页读取当前群聊天上下文；总结‘半小时前到现在’时使用 last_minutes=30", properties),
			SearchTerms: []string{"群聊上下文", "聊天记录", "历史消息", "最近消息", "上下文", "总结聊天", "半小时前", "过去几分钟", "时间范围"},
			Handler:     p.handleReadGroupContext,
		},
		{
			Definition:  agent.Function("read_user_context", "按最近时段、绝对时间、关键词和分页读取某个用户在当前群的发言", userProperties, "user_id"),
			SearchTerms: []string{"用户上下文", "某人发言", "用户记录", "历史消息", "总结发言", "半小时前", "过去几分钟", "时间范围"},
			Handler:     p.handleReadUserContext,
		},
		{
			Definition: agent.Function("read_messages_by_time", "从数据库读取当前群一个或多个时间区间内的消息，可选只看指定用户", map[string]any{
				"ranges": map[string]any{
					"type": "array", "minItems": 1, "maxItems": 10,
					"description": "一个或多个时间区间，开始时间包含、结束时间不包含",
					"items": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{
							"start": stringProperty("开始时间，如 2026-08-06 09:00 或 RFC3339"),
							"end":   stringProperty("结束时间，如 2026-08-06 12:00 或 RFC3339"),
						},
						"required": []string{"start", "end"},
					},
				},
				"user_id": integerProperty("可选，只查询该用户的 QQ 号"),
				"limit":   integerProperty("全部区间合计最多返回条数"),
			}, "ranges"),
			SearchTerms: []string{"时间段消息", "按时间查询", "某段时间", "多个时间段", "历史记录", "日期消息"},
			Handler:     p.handleReadMessagesByTime,
		},
	}
}

func contextToolProperties() map[string]any {
	return map[string]any{
		"limit":        integerProperty("本页最多返回条数，默认使用 context_limit 配置"),
		"offset":       integerProperty("从最新一条匹配消息向前跳过多少条，用于继续翻页，默认0"),
		"last_minutes": integerProperty("只看最近多少分钟；例如半小时前到现在填30"),
		"since":        stringProperty("可选，开始时间（包含），支持 RFC3339 或 YYYY-MM-DD HH:MM[:SS]"),
		"until":        stringProperty("可选，结束时间（不包含），支持 RFC3339 或 YYYY-MM-DD HH:MM[:SS]；默认当前时间"),
		"keyword":      stringProperty("可选，只返回正文包含该关键词的消息"),
	}
}

func cloneProperties(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source)+1)
	for name, property := range source {
		clone[name] = property
	}
	return clone
}

func (p *Persona) handleReadGroupContext(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	input, err := p.decodeContextQuery(raw, 0)
	if err != nil {
		return nil, err
	}
	return p.readContext(input)
}

func (p *Persona) handleReadUserContext(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		UserID int64 `json:"user_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if err := validateUserID(input.UserID); err != nil {
		return nil, err
	}
	query, err := p.decodeContextQuery(raw, input.UserID)
	if err != nil {
		return nil, err
	}
	return p.readContext(query)
}

type contextQueryInput struct {
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
	LastMinutes int    `json:"last_minutes"`
	Since       string `json:"since"`
	Until       string `json:"until"`
	Keyword     string `json:"keyword"`
}

type contextQueryResult struct {
	CurrentTime string              `json:"current_time"`
	Count       int                 `json:"count"`
	HasMore     bool                `json:"has_more"`
	NextOffset  int                 `json:"next_offset,omitempty"`
	Messages    []timeMessageResult `json:"messages"`
}

func (p *Persona) decodeContextQuery(raw json.RawMessage, userID int64) (ContextMessageQuery, error) {
	var input contextQueryInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return ContextMessageQuery{}, err
	}
	if input.Offset < 0 {
		return ContextMessageQuery{}, errors.New("offset must not be negative")
	}
	if input.LastMinutes < 0 {
		return ContextMessageQuery{}, errors.New("last_minutes must not be negative")
	}
	if input.LastMinutes > 0 && strings.TrimSpace(input.Since) != "" {
		return ContextMessageQuery{}, errors.New("last_minutes and since cannot be used together")
	}
	now := time.Now()
	query := ContextMessageQuery{UserID: userID, Keyword: strings.TrimSpace(input.Keyword), Offset: input.Offset, Limit: p.contextLimit(input.Limit)}
	if input.LastMinutes > 0 {
		start := now.Add(-time.Duration(input.LastMinutes) * time.Minute)
		query.Start = &start
	} else if strings.TrimSpace(input.Since) != "" {
		start, err := parseToolTime(input.Since, false)
		if err != nil {
			return ContextMessageQuery{}, fmt.Errorf("since: %w", err)
		}
		query.Start = &start
	}
	if strings.TrimSpace(input.Until) != "" {
		end, err := parseToolTime(input.Until, true)
		if err != nil {
			return ContextMessageQuery{}, fmt.Errorf("until: %w", err)
		}
		query.End = &end
	} else if input.LastMinutes > 0 {
		query.End = &now
	}
	if query.Start != nil && query.End != nil && !query.Start.Before(*query.End) {
		return ContextMessageQuery{}, errors.New("since must be before until")
	}
	return query, nil
}

func (p *Persona) readContext(query ContextMessageQuery) (contextQueryResult, error) {
	messages, hasMore, err := p.queryContextMessages(query)
	if err != nil {
		return contextQueryResult{}, err
	}
	result := contextQueryResult{CurrentTime: time.Now().Format(time.RFC3339), Count: len(messages), HasMore: hasMore, Messages: timeMessageResults(messages)}
	if hasMore {
		result.NextOffset = query.Offset + len(messages)
	}
	return result, nil
}

type messageTimeRangeInput struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func (p *Persona) handleReadMessagesByTime(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		Ranges []messageTimeRangeInput `json:"ranges"`
		UserID int64                   `json:"user_id"`
		Limit  int                     `json:"limit"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if len(input.Ranges) == 0 || len(input.Ranges) > 10 {
		return nil, errors.New("ranges must contain between 1 and 10 items")
	}
	if input.UserID < 0 {
		return nil, errors.New("user_id must not be negative")
	}
	ranges := make([]MessageTimeRange, len(input.Ranges))
	for i, item := range input.Ranges {
		start, err := parseToolTime(item.Start, false)
		if err != nil {
			return nil, fmt.Errorf("ranges[%d].start: %w", i, err)
		}
		end, err := parseToolTime(item.End, true)
		if err != nil {
			return nil, fmt.Errorf("ranges[%d].end: %w", i, err)
		}
		if !start.Before(end) {
			return nil, fmt.Errorf("ranges[%d]: start must be before end", i)
		}
		ranges[i] = MessageTimeRange{Start: start, End: end}
	}
	messages, err := p.messagesInTimeRanges(ranges, input.UserID, p.contextLimit(input.Limit))
	if err != nil {
		return nil, err
	}
	return timeMessageResults(messages), nil
}

func (p *Persona) contextLimit(limit int) int {
	if limit <= 0 || limit > p.opts.ContextLimit {
		return p.opts.ContextLimit
	}
	return limit
}

func parseToolTime(raw string, dateEnd bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("time cannot be empty")
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed, nil
		}
	}
	if parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		if dateEnd {
			parsed = parsed.AddDate(0, 0, 1)
		}
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("unsupported time %q; use RFC3339 or YYYY-MM-DD[ HH:MM[:SS]]", raw)
}

type timeMessageResult struct {
	MessageID       int64  `json:"message_id"`
	CreatedAt       string `json:"created_at"`
	UserID          int64  `json:"user_id"`
	Nickname        string `json:"nickname"`
	TargetUserID    int64  `json:"target_user_id,omitempty"`
	TargetNickname  string `json:"target_nickname,omitempty"`
	Type            string `json:"type"`
	Content         string `json:"content,omitempty"`
	ImageURL        string `json:"image_url,omitempty"`
	FileName        string `json:"file_name,omitempty"`
	QuotedMessageID int64  `json:"quoted_message_id,omitempty"`
	QuotedUserID    int64  `json:"quoted_user_id,omitempty"`
	QuotedNickname  string `json:"quoted_nickname,omitempty"`
	QuotedContent   string `json:"quoted_content,omitempty"`
	QuotedImageURL  string `json:"quoted_image_url,omitempty"`
	Referred        bool   `json:"referred,omitempty"`
}

func timeMessageResults(messages []GroupMessage) []timeMessageResult {
	results := make([]timeMessageResult, len(messages))
	for i, item := range messages {
		results[i] = timeMessageResult{
			MessageID: item.MsgID, CreatedAt: item.CreatedAt.Format(time.RFC3339), UserID: item.User.UserId, Nickname: item.User.Nickname,
			TargetUserID: item.TargetUser.UserId, TargetNickname: item.TargetUser.Nickname, Type: item.MsgType, Content: item.Content,
			ImageURL: item.Url, FileName: item.FileName, QuotedMessageID: item.QuotedMsgID, QuotedUserID: item.QuotedUser.UserId,
			QuotedNickname: item.QuotedUser.Nickname, QuotedContent: item.QuotedContent, QuotedImageURL: item.QuotedURL, Referred: item.Refer,
		}
	}
	return results
}
