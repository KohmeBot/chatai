package persona

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/favor"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

// registerBuiltinTools 只维护工具元数据与处理器映射；每个工具的逻辑在独立具名函数中。
func (p *Persona) registerBuiltinTools() {
	contextProperties := map[string]any{
		"limit":        integerProperty("本页最多返回条数，默认使用 context_limit 配置"),
		"offset":       integerProperty("从最新一条匹配消息向前跳过多少条，用于继续翻页，默认0"),
		"last_minutes": integerProperty("只看最近多少分钟；例如半小时前到现在填30"),
		"since":        stringProperty("可选，开始时间（包含），支持 RFC3339 或 YYYY-MM-DD HH:MM[:SS]"),
		"until":        stringProperty("可选，结束时间（不包含），支持 RFC3339 或 YYYY-MM-DD HH:MM[:SS]；默认当前时间"),
		"keyword":      stringProperty("可选，只返回正文包含该关键词的消息"),
	}
	userContextProperties := make(map[string]any, len(contextProperties)+1)
	for name, property := range contextProperties {
		userContextProperties[name] = property
	}
	userContextProperties["user_id"] = integerProperty("用户 QQ 号")
	tools := []agent.Tool{
		{Definition: agent.Function("get_group_member_info", "获取当前群指定成员的详细信息；缓存响应更快，no_cache=true 可获取较新的信息", map[string]any{

			"user_id":  integerProperty("成员 QQ 号"),
			"no_cache": map[string]any{"type": "boolean", "default": false, "description": "是否不使用缓存，默认 false"},
		}, "user_id"), SearchTerms: []string{"群成员信息", "成员资料", "群名片", "成员角色", "群主", "管理员", "入群时间", "最后发言", "成员等级", "专属头衔"}, Handler: p.handleGetGroupMemberInfo},
		{Definition: agent.Function("get_group_member_list", "获取当前群的成员列表；部分字段可能不如单独查询成员信息完整", map[string]any{}), SearchTerms: []string{"群成员列表", "群成员", "成员名单", "群里有谁", "群友列表", "所有成员"}, Handler: p.handleGetGroupMemberList},
		{Definition: agent.Function("read_group_context", "按最近时段、绝对时间、关键词和分页读取当前群聊天上下文；总结‘半小时前到现在’时使用 last_minutes=30", contextProperties), SearchTerms: []string{"群聊上下文", "聊天记录", "历史消息", "最近消息", "上下文", "总结聊天", "半小时前", "过去几分钟", "时间范围"}, Handler: p.handleReadGroupContext},
		{Definition: agent.Function("read_user_context", "按最近时段、绝对时间、关键词和分页读取某个用户在当前群的发言", userContextProperties, "user_id"), SearchTerms: []string{"用户上下文", "某人发言", "用户记录", "历史消息", "总结发言", "半小时前", "过去几分钟", "时间范围"}, Handler: p.handleReadUserContext},
		{Definition: agent.Function("read_messages_by_time", "从数据库读取当前群一个或多个时间区间内的消息，可选只看指定用户", map[string]any{
			"ranges":  map[string]any{"type": "array", "minItems": 1, "maxItems": 10, "description": "一个或多个时间区间，开始时间包含、结束时间不包含", "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"start": stringProperty("开始时间，如 2026-08-06 09:00 或 RFC3339"), "end": stringProperty("结束时间，如 2026-08-06 12:00 或 RFC3339")}, "required": []string{"start", "end"}}},
			"user_id": integerProperty("可选，只查询该用户的 QQ 号"), "limit": integerProperty("全部区间合计最多返回条数"),
		}, "ranges"), SearchTerms: []string{"时间段消息", "按时间查询", "某段时间", "多个时间段", "历史记录", "日期消息"}, Handler: p.handleReadMessagesByTime},
		{Definition: agent.Function("read_user_impression", "读取对某个用户的长期印象", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"), SearchTerms: []string{"用户印象", "对某人的印象", "长期记忆"}, Handler: p.handleReadUserImpression},
		{Definition: agent.Function("read_group_impression", "读取对当前群的长期印象", map[string]any{}), SearchTerms: []string{"群聊印象", "群印象", "长期记忆"}, Handler: p.handleReadGroupImpression},
		{Definition: agent.Function("read_user_favor", "读取指定用户当前的好感度、等级和说明", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"), SearchTerms: []string{"读取好感度", "查询好感度", "关系等级", "亲密度"}, Handler: p.handleReadUserFavor},
		{Definition: agent.Function("update_user_favor", "按增量修改指定用户的好感度；单次增加最多60，减少最多30", map[string]any{"user_id": integerProperty("用户 QQ 号"), "delta": integerProperty("增加或减少的数值，负数表示减少"), "reason": stringProperty("修改原因")}, "user_id", "delta", "reason"), SearchTerms: []string{"修改好感度", "增加好感度", "减少好感度", "关系变化", "亲密度"}, Handler: p.handleUpdateUserFavor},
		{Definition: agent.Function("send_message", "向当前群发送一句话，可选择引用消息", map[string]any{"text": stringProperty("要发送的话"), "reply_message_id": integerProperty("可选，引用的消息 ID")}, "text"), SearchTerms: []string{"发送消息", "回复", "说话", "引用回复"}, GroupAction: true, Handler: p.handleSendMessage},
		{Definition: agent.Function("send_messages", "把回复拆成多条独立消息依次发送，最多5条", map[string]any{"messages": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 2, "maxItems": 5, "description": "按发送顺序排列的多句话"}, "interval_ms": integerProperty("消息间隔毫秒，默认500，范围0到3000")}, "messages"), SearchTerms: []string{"多句话", "分多条回复", "拆分回复", "连续发送", "多段消息"}, GroupAction: true, Handler: p.handleSendMessages},
		{Definition: agent.Function("at_user", "在当前群 @ 某人并发送文字", map[string]any{"user_id": integerProperty("用户 QQ 号"), "text": stringProperty("要说的话")}, "user_id", "text"), SearchTerms: []string{"At某人", "@某人", "提醒某人", "指定用户回复"}, GroupAction: true, Handler: p.handleAtUser},
		{Definition: agent.Function("poke_user", "在当前群戳一戳某人", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"), SearchTerms: []string{"戳一戳", "戳某人", "poke"}, GroupAction: true, Handler: p.handlePokeUser},
		{Definition: agent.Function("schedule_task", "创建一次性定时任务，到期后由 Agent 再次决定如何执行", map[string]any{"delay_seconds": integerProperty("延迟秒数"), "instruction": stringProperty("到期时交给 Agent 的任务说明")}, "delay_seconds", "instruction"), SearchTerms: []string{"定时任务", "提醒", "稍后执行", "延迟"}, Handler: p.handleScheduleTask},
		{Definition: agent.Function("search_web", "联网搜索公开网页，返回标题、链接和摘要；遇到不懂或不确定的信息时使用", map[string]any{"query": stringProperty("搜索关键词"), "limit": integerProperty("结果数量，默认5，最大8")}, "query"), SearchTerms: []string{"联网搜索", "搜索", "搜索网页", "查资料", "最新信息", "互联网", "不懂", "不知道", "陌生概念", "事实核实"}, Handler: p.handleSearchWeb},
		{Definition: agent.Function("browse_web", "读取公开网页正文；想进一步浏览结果或搜索结果摘要不足时使用", map[string]any{"url": stringProperty("http 或 https 网页地址")}, "url"), SearchTerms: []string{"联网搜索", "搜索", "搜索网页", "查资料", "最新信息", "互联网", "浏览网页", "读取网页", "打开链接", "网页正文", "原文", "URL"}, Handler: p.handleBrowseWeb},
	}
	for _, tool := range tools {
		_ = p.tools.Register(tool)
	}
}

func integerProperty(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

type groupMemberInfo struct {
	GroupID         int64  `json:"group_id"`
	UserID          int64  `json:"user_id"`
	Nickname        string `json:"nickname"`
	Card            string `json:"card"`
	Sex             string `json:"sex"`
	Age             int32  `json:"age"`
	Area            string `json:"area"`
	JoinTime        int32  `json:"join_time"`
	LastSentTime    int32  `json:"last_sent_time"`
	Level           string `json:"level"`
	Role            string `json:"role"`
	Unfriendly      bool   `json:"unfriendly"`
	Title           string `json:"title"`
	TitleExpireTime int32  `json:"title_expire_time"`
	CardChangeable  bool   `json:"card_changeable"`
}

func (p *Persona) handleGetGroupMemberInfo(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		UserID  int64 `json:"user_id"`
		NoCache bool  `json:"no_cache"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}

	if err := validateUserID(input.UserID); err != nil {
		return nil, err
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	var member groupMemberInfo
	if err := json.Unmarshal([]byte(ctx.GetThisGroupMemberInfo(input.UserID, input.NoCache).Raw), &member); err != nil {
		return nil, fmt.Errorf("decode group member info: %w", err)
	}
	return member, nil
}

func (p *Persona) handleGetGroupMemberList(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	members := make([]groupMemberInfo, 0)
	if err := json.Unmarshal([]byte(ctx.GetThisGroupMemberList().Raw), &members); err != nil {
		return nil, fmt.Errorf("decode group member list: %w", err)
	}
	return members, nil
}

func (p *Persona) validateCurrentGroupID(groupID int64) error {
	if groupID <= 0 {
		return errors.New("group_id must be positive")
	}
	if groupID != p.groupID {
		return fmt.Errorf("group_id must be the current group (%d)", p.groupID)
	}
	return nil
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

func (p *Persona) handleReadUserImpression(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	userID, err := decodeUserID(raw)
	if err != nil {
		return nil, err
	}
	return new(UserImpression).Get(p.db, userID)
}

func (p *Persona) handleReadGroupImpression(_ *agent.RunContext, _ json.RawMessage) (any, error) {
	return new(GroupImpression).Get(p.db, p.groupID)
}

func (p *Persona) handleReadUserFavor(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	userID, err := decodeUserID(raw)
	if err != nil {
		return nil, err
	}
	value, err := favor.GetFavor(p.db, userID)
	if err != nil {
		return nil, err
	}
	level := favor.GetFavorLevelInfo(value)
	return map[string]any{"user_id": userID, "favor": value, "level": level.Name, "description": level.Desc, "range": []int64{favor.FavorMin, favor.FavorMax}}, nil
}

func (p *Persona) handleUpdateUserFavor(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		UserID int64  `json:"user_id"`
		Delta  int64  `json:"delta"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if err := validateUserID(input.UserID); err != nil {
		return nil, err
	}
	if input.Delta == 0 {
		return nil, errors.New("delta must not be zero")
	}
	before, err := favor.GetFavor(p.db, input.UserID)
	if err != nil {
		return nil, err
	}
	if err := favor.UpdateFavor(p.db, input.UserID, input.Delta); err != nil {
		return nil, err
	}
	after, err := favor.GetFavor(p.db, input.UserID)
	if err != nil {
		return nil, err
	}
	level := favor.GetFavorLevelInfo(after)
	return map[string]any{"user_id": input.UserID, "before": before, "after": after, "actual_delta": after - before, "level": level.Name, "reason": input.Reason}, nil
}

func (p *Persona) handleSendMessage(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		Text    string `json:"text"`
		ReplyID int64  `json:"reply_message_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Text) == "" {
		return nil, errors.New("message text cannot be empty")
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	segments := message.Message{}
	if input.ReplyID > 0 {
		segments = append(segments, message.Reply(input.ReplyID))
		if u := ctx.GetMessage(input.ReplyID).Sender; u != nil {
			segments = append(segments, message.At(u.ID))
			segments = append(segments, message.Text(" "))
		}

	}
	segments = append(segments, message.Text(input.Text))
	id := ctx.SendGroupMessage(p.groupID, segments)
	rc.MarkActionPerformed()
	if err := p.recordBotMessage(ctx, segments, id); err != nil {
		return nil, err
	}
	return map[string]any{"message_id": id}, nil
}

func (p *Persona) handleSendMessages(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		Messages   []string `json:"messages"`
		IntervalMS int      `json:"interval_ms"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if len(input.Messages) < 2 || len(input.Messages) > 5 {
		return nil, errors.New("messages must contain between 2 and 5 items")
	}
	if input.IntervalMS == 0 {
		input.IntervalMS = 500
	}
	if input.IntervalMS < 0 || input.IntervalMS > 3000 {
		return nil, errors.New("interval_ms must be between 0 and 3000")
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(input.Messages))
	for i, text := range input.Messages {
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("message text cannot be empty")
		}
		segments := message.Message{message.Text(text)}
		id := ctx.SendGroupMessage(p.groupID, segments)
		rc.MarkActionPerformed()
		if err := p.recordBotMessage(ctx, segments, id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		if i < len(input.Messages)-1 && input.IntervalMS > 0 {
			time.Sleep(time.Duration(input.IntervalMS) * time.Millisecond)
		}
	}
	return map[string]any{"message_ids": ids}, nil
}

func (p *Persona) handleAtUser(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		UserID int64  `json:"user_id"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if err := validateUserID(input.UserID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Text) == "" {
		return nil, errors.New("message text cannot be empty")
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	segments := message.Message{message.At(input.UserID), message.Text(" " + input.Text)}
	id := ctx.SendGroupMessage(p.groupID, segments)
	rc.MarkActionPerformed()
	if err := p.recordBotMessage(ctx, segments, id); err != nil {
		return nil, err
	}
	return map[string]any{"message_id": id}, nil
}

func (p *Persona) handlePokeUser(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	userID, err := decodeUserID(raw)
	if err != nil {
		return nil, err
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	ctx.CallAction("send_poke", zero.Params{"group_id": p.groupID, "user_id": userID})
	rc.MarkActionPerformed()
	return "ok", nil
}

func (p *Persona) handleScheduleTask(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		Delay       int    `json:"delay_seconds"`
		Instruction string `json:"instruction"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if input.Delay <= 0 || input.Delay > p.opts.ScheduleMaxSec {
		return nil, fmt.Errorf("delay_seconds must be between 1 and %d", p.opts.ScheduleMaxSec)
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	taskID := strconv.FormatInt(time.Now().UnixNano(), 36)
	time.AfterFunc(time.Duration(input.Delay)*time.Second, func() {
		_ = p.run(ctx, GroupMessage{User: User{UserId: rc.UserID, Nickname: "定时任务"}, CreatedAt: time.Now(), MsgType: MsgTypeText}, input.Instruction)
	})
	return map[string]any{"task_id": taskID, "run_at": time.Now().Add(time.Duration(input.Delay) * time.Second)}, nil
}

func (p *Persona) handleSearchWeb(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	return p.searchWeb(rc, input.Query, input.Limit)
}

func (p *Persona) handleBrowseWeb(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	return p.readWeb(rc, input.URL)
}

func (p *Persona) contextLimit(limit int) int {
	if limit <= 0 || limit > p.opts.ContextLimit {
		return p.opts.ContextLimit
	}
	return limit
}

func decodeUserID(raw json.RawMessage) (int64, error) {
	var input struct {
		UserID int64 `json:"user_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return 0, err
	}
	if err := validateUserID(input.UserID); err != nil {
		return 0, err
	}
	return input.UserID, nil
}

func validateUserID(userID int64) error {
	if userID <= 0 {
		return errors.New("user_id must be positive")
	}
	return nil
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

func zeroContext(rc *agent.RunContext) (*zero.Ctx, error) {
	ctx, ok := rc.Values["zero_ctx"].(*zero.Ctx)
	if !ok || ctx == nil {
		return nil, errors.New("zero context unavailable")
	}
	return ctx, nil
}

func (p *Persona) recordBotMessage(ctx *zero.Ctx, segments message.Message, id int64) error {
	return p.saveMessage(GroupMessage{User: User{UserId: ctx.Event.SelfID, Nickname: "你"}, Content: segments.ExtractPlainText(), MsgType: getMsgType(segments), MsgID: id, CreatedAt: time.Now(), Url: getUrl(segments), FileName: getFileName(segments)})
}

func (p *Persona) readWeb(rc *agent.RunContext, rawURL string) (string, error) {
	source, err := p.loadWebPage(rc, rawURL)
	if err != nil {
		return "", err
	}
	return extractUsefulText(source, p.opts.WebMaxBytes)

	//text := regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]+>`).ReplaceAllString(source, " ")
	//return strings.Join(strings.Fields(html.UnescapeString(text)), " "), nil
}

func extractUsefulText(source string, maxBytes int) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		return "", err
	}

	doc.Find("script, style, noscript, svg, canvas, iframe").Remove()
	doc.Find("nav, footer").Remove()

	// 给链接补充 URL
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if exists && href != "" {
			text := strings.TrimSpace(s.Text())

			if text != "" {
				s.ReplaceWithHtml(
					html.EscapeString(text) +
						" [链接: " +
						html.EscapeString(href) +
						"]",
				)
			}
		}
	})

	text := doc.Find("body").Text()

	lines := strings.Split(text, "\n")

	var result []string
	for _, line := range lines {
		line = html.UnescapeString(line)
		line = strings.Join(strings.Fields(line), " ")

		if line != "" {
			result = append(result, line)
		}
	}

	res := strings.Join(result, "\n")

	if maxBytes > 0 && len(res) > maxBytes {
		res = res[:maxBytes]
	}

	return res, nil
}

type webPageLoader func(context.Context, string) (string, error)

func (p *Persona) loadWebPage(ctx context.Context, rawURL string) (string, error) {
	if p.opts.WebBrowserEnable {
		res, err := loadWebPageWithBrowser(ctx, rawURL, p.opts.WebBrowserAddress)
		if err == nil {
			return res, err
		}
		logrus.Errorf("web browser failed,try get:%v", err)
	}
	if _, err := validatePublicURL(ctx, rawURL); err != nil {
		return "", err
	}
	return loadWebPageWithHTTP(publicHTTPClient(ctx))(ctx, rawURL)
}

func loadWebPageWithHTTP(client *http.Client) webPageLoader {
	return func(ctx context.Context, rawURL string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set(
			"User-Agent",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
				"AppleWebKit/537.36 (KHTML, like Gecko) "+
				"Chrome/131.0.0.0 Safari/537.36",
		)

		req.Header.Set(
			"Accept",
			"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		)

		req.Header.Set(
			"Accept-Language",
			"zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7",
		)

		req.Header.Set(
			"Cache-Control",
			"no-cache",
		)

		req.Header.Set(
			"Pragma",
			"no-cache",
		)

		req.Header.Set(
			"Referer",
			"https://www.cn.bing.com/",
		)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("web returned %s", resp.Status)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		return string(body), nil
	}
}

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

type webSearchProvider struct {
	name     string
	endpoint func(string) string
	parse    func(string, int) []webSearchResult
}

var defaultWebSearchProviders = []webSearchProvider{
	{name: "DuckDuckGo", endpoint: func(query string) string {
		return "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	}, parse: parseDuckDuckGoResults},
	{name: "Bing CN", endpoint: func(query string) string {
		return "https://cn.bing.com/search?q=" + url.QueryEscape(query) + "&setlang=zh-cn"
	}, parse: parseBingResults},
	{name: "Baidu", endpoint: func(query string) string {
		return "https://www.baidu.com/s?wd=" + url.QueryEscape(query)
	}, parse: parseBaiduResults},
}

func (p *Persona) searchWeb(rc *agent.RunContext, query string, limit int) ([]webSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search query cannot be empty")
	}
	if limit <= 0 || limit > 8 {
		limit = 5
	}
	p.searchMu.Lock()
	preferred := ""
	if time.Now().Before(p.preferredSearchUntil) {
		preferred = p.preferredSearchProvider
	}
	p.searchMu.Unlock()

	results, provider, err := searchWebWithPreferredProviderAndLoader(rc, query, limit, p.opts.WebMaxBytes, p.loadWebPage, defaultWebSearchProviders, preferred)
	if err != nil {
		return nil, err
	}
	p.searchMu.Lock()
	p.preferredSearchProvider = provider
	p.preferredSearchUntil = time.Now().Add(p.opts.WebSearchPrefer)
	p.searchMu.Unlock()
	return results, nil
}

func searchWebWithProviders(ctx context.Context, query string, limit, maxBytes int, client *http.Client, providers []webSearchProvider) ([]webSearchResult, error) {
	results, _, err := searchWebWithPreferredProvider(ctx, query, limit, maxBytes, client, providers, "")
	return results, err
}

func searchWebWithPreferredProvider(ctx context.Context, query string, limit, maxBytes int, client *http.Client, providers []webSearchProvider, preferred string) ([]webSearchResult, string, error) {
	return searchWebWithPreferredProviderAndLoader(ctx, query, limit, maxBytes, loadWebPageWithHTTP(client), providers, preferred)
}

func searchWebWithPreferredProviderAndLoader(ctx context.Context, query string, limit, maxBytes int, loader webPageLoader, providers []webSearchProvider, preferred string) ([]webSearchResult, string, error) {
	providers = preferredProviderFirst(providers, preferred)
	errs := make([]error, 0, len(providers))
	for _, provider := range providers {
		results, err := searchWithProviderAndLoader(ctx, query, limit, loader, provider)
		if err == nil {
			return results, provider.name, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", provider.name, err))
	}
	return nil, "", fmt.Errorf("all search providers failed: %w", errors.Join(errs...))
}

func preferredProviderFirst(providers []webSearchProvider, preferred string) []webSearchProvider {
	if preferred == "" || len(providers) < 2 {
		return providers
	}
	ordered := make([]webSearchProvider, 0, len(providers))
	for _, provider := range providers {
		if provider.name == preferred {
			ordered = append(ordered, provider)
			break
		}
	}
	if len(ordered) == 0 {
		return providers
	}
	for _, provider := range providers {
		if provider.name != preferred {
			ordered = append(ordered, provider)
		}
	}
	return ordered
}

func searchWithProvider(ctx context.Context, query string, limit int, client *http.Client, provider webSearchProvider) ([]webSearchResult, error) {
	return searchWithProviderAndLoader(ctx, query, limit, loadWebPageWithHTTP(client), provider)
}

func searchWithProviderAndLoader(ctx context.Context, query string, limit int, loader webPageLoader, provider webSearchProvider) ([]webSearchResult, error) {
	body, err := loader(ctx, provider.endpoint(query))
	if err != nil {
		return nil, err
	}
	logrus.Infof("search results: %s", body)

	results := provider.parse(body, limit)
	if len(results) == 0 {
		return nil, errors.New("returned no parseable results")
	}
	return results, nil
}

var stripHTMLRE = regexp.MustCompile(`<[^>]+>`)

func cleanSearchText(raw string) string {
	return strings.Join(strings.Fields(html.UnescapeString(stripHTMLRE.ReplaceAllString(raw, " "))), " ")
}

const (
	duckDuckGoBaseURL = "https://html.duckduckgo.com/"
	bingBaseURL       = "https://cn.bing.com/"
	baiduBaseURL      = "https://www.baidu.com/"
)

func parseDuckDuckGoResults(source string, limit int) []webSearchResult {
	if !canParseSearchResults(source, limit) {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		return nil
	}

	results := make([]webSearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)

	/*
		优先匹配 result__body。

		其余选择器用于兼容旧版、精简版或不同实验页面。
		即使容器存在嵌套，URL 去重也能避免重复结果。
	*/
	containers := doc.Find(
		"div.result__body," +
			"div.result," +
			"div.results_links," +
			"div.web-result",
	)

	containers.EachWithBreak(func(_ int, item *goquery.Selection) bool {
		anchor := item.Find(
			"a.result__a[href]," +
				"h2.result__title a[href]," +
				"a.result-link[href]",
		).First()

		if anchor.Length() == 0 {
			return true
		}

		rawURL, ok := firstNonEmptyAttr(
			anchor,
			"data-href",
			"href",
		)
		if !ok {
			return true
		}

		result := webSearchResult{
			Title: cleanSelectionText(anchor),
			URL: normalizeParsedSearchURL(
				rawURL,
				duckDuckGoBaseURL,
			),
			Snippet: firstNonEmptySelectionText(
				item,
				".result__snippet",
				".result-snippet",
				".snippet",
			),
		}

		return !appendSearchResult(
			&results,
			seen,
			result,
			limit,
		)
	})

	return results
}

func parseBingResults(source string, limit int) []webSearchResult {
	if !canParseSearchResults(source, limit) {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		return nil
	}

	results := make([]webSearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)

	doc.Find("li.b_algo").EachWithBreak(
		func(_ int, item *goquery.Selection) bool {
			anchor := item.Find("h2 a[href]").First()
			if anchor.Length() == 0 {
				return true
			}

			/*
				data-url、data-href 如果存在，通常比 href 更值得优先使用，
				因为 href 可能是 Bing 的点击统计跳转地址。
			*/
			rawURL, ok := firstNonEmptyAttr(
				anchor,
				"data-url",
				"data-href",
				"href",
			)
			if !ok {
				return true
			}

			result := webSearchResult{
				Title: cleanSelectionText(anchor),
				URL: normalizeParsedSearchURL(
					rawURL,
					bingBaseURL,
				),
				Snippet: firstNonEmptySelectionText(
					item,

					// 优先使用明确的自然结果摘要节点。
					"p.b_algoSlug",

					// 兼容其他 Bing 页面结构。
					".b_caption p",
					".b_snippet",
					".b_paractl",
					"p",
				),
			}

			return !appendSearchResult(
				&results,
				seen,
				result,
				limit,
			)
		},
	)

	return results
}

func parseBaiduResults(source string, limit int) []webSearchResult {
	if !canParseSearchResults(source, limit) {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		return nil
	}

	results := make([]webSearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)

	/*
		先限制在 content_left 中，避免将右侧推荐、相关搜索或其他卡片
		当成自然搜索结果。
	*/
	containers := doc.Find(
		"#content_left > div.result," +
			"#content_left > div.result-op," +
			"#content_left > div.c-container",
	)

	/*
		有些精简页面不存在 content_left，才退回到全页面选择。
	*/
	if containers.Length() == 0 {
		containers = doc.Find(
			"div.result," +
				"div.result-op," +
				"div.c-container",
		)
	}

	containers.EachWithBreak(func(_ int, item *goquery.Selection) bool {
		anchor := item.Find(
			"h3.t a[href]," +
				"h3 a[href]",
		).First()

		if anchor.Length() == 0 {
			return true
		}

		/*
			优先读取可能保存真实落地地址的属性。

			href 经常是：
			https://www.baidu.com/link?url=...

			如果没有真实地址属性，才保留 href 跳转地址。
		*/
		rawURL := firstNonEmptyString(
			attrValue(anchor, "data-landurl"),
			attrValue(anchor, "data-url"),
			attrValue(item, "mu"),
			attrValue(item.Find("[mu]").First(), "mu"),
			attrValue(anchor, "href"),
		)
		if rawURL == "" {
			return true
		}

		result := webSearchResult{
			Title: cleanSelectionText(anchor),
			URL: normalizeParsedSearchURL(
				rawURL,
				baiduBaseURL,
			),
			Snippet: firstNonEmptySelectionText(
				item,
				".c-abstract",
				".c-span-last .content-right",
				".content-right",
				".c-font-normal",
			),
		}

		return !appendSearchResult(
			&results,
			seen,
			result,
			limit,
		)
	})

	return results
}

/*
canParseSearchResults 做最基本的输入和验证页判断。

因为当前 parser 签名不返回 error，所以遇到验证页时只能返回 nil。
长期建议将签名改为：

	func parseXXX(source string, limit int) ([]webSearchResult, error)
*/
func canParseSearchResults(source string, limit int) bool {
	if limit <= 0 || strings.TrimSpace(source) == "" {
		return false
	}

	return !isLikelySearchChallengePage(source)
}

func isLikelySearchChallengePage(source string) bool {
	lowerSource := strings.ToLower(source)

	markers := []string{
		`id="challenge-form"`,
		`name="challenge-form"`,
		`id="b_captcha"`,
		`class="b_captcha"`,
		"百度安全验证",
		"请输入验证码",
		"wappass.baidu.com/static/captcha",
	}

	for _, marker := range markers {
		if strings.Contains(lowerSource, strings.ToLower(marker)) {
			return true
		}
	}

	return false
}

/*
appendSearchResult 统一执行：

  - 清理字段
  - 过滤空标题
  - 过滤无效 URL
  - URL 去重
  - 按有效结果数量控制 limit

返回 true 表示结果数量已经达到 limit。
*/
func appendSearchResult(
	results *[]webSearchResult,
	seen map[string]struct{},
	result webSearchResult,
	limit int,
) bool {
	result.Title = strings.TrimSpace(result.Title)
	result.URL = strings.TrimSpace(result.URL)
	result.Snippet = strings.TrimSpace(result.Snippet)

	if result.Title == "" || result.URL == "" {
		return false
	}

	key := searchResultURLKey(result.URL)
	if key == "" {
		return false
	}

	if _, exists := seen[key]; exists {
		return false
	}

	seen[key] = struct{}{}
	*results = append(*results, result)

	return len(*results) >= limit
}

func cleanSelectionText(selection *goquery.Selection) string {
	if selection == nil || selection.Length() == 0 {
		return ""
	}

	return cleanSearchText(selection.Text())
}

func firstNonEmptySelectionText(
	parent *goquery.Selection,
	selectors ...string,
) string {
	if parent == nil || parent.Length() == 0 {
		return ""
	}

	for _, selector := range selectors {
		selection := parent.Find(selector).First()
		if selection.Length() == 0 {
			continue
		}

		text := cleanSelectionText(selection)
		if text != "" {
			return text
		}
	}

	return ""
}

func firstNonEmptyAttr(
	selection *goquery.Selection,
	names ...string,
) (string, bool) {
	if selection == nil || selection.Length() == 0 {
		return "", false
	}

	for _, name := range names {
		value, exists := selection.Attr(name)
		value = strings.TrimSpace(value)

		if exists && value != "" {
			return value, true
		}
	}

	return "", false
}

func attrValue(
	selection *goquery.Selection,
	name string,
) string {
	if selection == nil || selection.Length() == 0 {
		return ""
	}

	value, exists := selection.Attr(name)
	if !exists {
		return ""
	}

	return strings.TrimSpace(value)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}

	return ""
}

/*
normalizeParsedSearchURL 负责：

  - HTML 实体解码
  - 解析相对地址
  - 解包 DuckDuckGo 的 uddg 跳转参数
  - 调用项目已有的 normalizeSearchURL
  - 限制为 http/https
  - 移除 fragment
*/
func normalizeParsedSearchURL(
	rawURL string,
	baseURL string,
) string {
	rawURL = strings.TrimSpace(html.UnescapeString(rawURL))
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	if !parsed.IsAbs() {
		base, baseErr := url.Parse(baseURL)
		if baseErr != nil {
			return ""
		}

		parsed = base.ResolveReference(parsed)
	}

	/*
		DuckDuckGo HTML 常见跳转格式：

		https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com
	*/
	if isDuckDuckGoHost(parsed.Hostname()) {
		if target := strings.TrimSpace(parsed.Query().Get("uddg")); target != "" {
			targetURL, targetErr := url.Parse(target)
			if targetErr == nil && targetURL.IsAbs() {
				parsed = targetURL
			}
		}
	}

	normalized := strings.TrimSpace(
		normalizeSearchURL(parsed.String()),
	)
	if normalized == "" {
		return ""
	}

	finalURL, err := url.Parse(normalized)
	if err != nil {
		return ""
	}

	switch strings.ToLower(finalURL.Scheme) {
	case "http", "https":
	default:
		return ""
	}

	if finalURL.Hostname() == "" {
		return ""
	}

	finalURL.Fragment = ""

	return finalURL.String()
}

func isDuckDuckGoHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))

	return host == "duckduckgo.com" ||
		host == "html.duckduckgo.com" ||
		strings.HasSuffix(host, ".duckduckgo.com")
}

/*
searchResultURLKey 生成去重 key。

这里不删除全部查询参数，因为某些站点依靠查询参数标识实际页面；
只移除 fragment，并统一 scheme、host 大小写。
*/
func searchResultURLKey(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}

	if parsed.Hostname() == "" {
		return ""
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""

	return parsed.String()
}

func normalizeSearchURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if target := u.Query().Get("uddg"); target != "" {
		return target
	}
	return raw
}

func publicHTTPClient(rc context.Context) *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		_, err := validatePublicURL(rc, req.URL.String())
		return err
	}}
}

func validatePublicURL(ctx context.Context, rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("invalid http(s) URL")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return nil, err
	}
	for _, addr := range addresses {
		if addr.IP.IsLoopback() || addr.IP.IsPrivate() || addr.IP.IsUnspecified() || addr.IP.IsLinkLocalUnicast() {
			return nil, errors.New("private or local addresses are not allowed")
		}
	}
	return u, nil
}
