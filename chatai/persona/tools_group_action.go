package persona

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func (p *Persona) groupActionTools() []agent.Tool {
	return []agent.Tool{
		{
			Definition: agent.Function("send_image", "立即向当前群发送一张图片并可带一句话；这是不可自动重试的可见副作用，成功后返回 message_id", map[string]any{
				"url":  stringProperty("发送的图片URL"),
				"text": stringProperty("可选，发送的图片附带的文字"),
			}, "url"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "回复", "图片", "发送图片", "发图"},
			GroupAction: true,
			Handler:     p.handleSendImage,
		},
		{
			Definition: agent.Function("send_message", "立即向当前群发送一句话，可选择引用消息；普通最终文本会由宿主自动发送，仅在必须引用或需要立即执行特殊群聊动作时调用。成功后返回 message_id", map[string]any{
				"text":             stringProperty("要发送的话"),
				"reply_message_id": integerProperty("可选，引用的消息 ID"),
			}, "text"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "回复", "说话", "引用回复"},
			GroupAction: true,
			Handler:     p.handleSendMessage,
		},
		{
			Definition: agent.Function("send_messages", "立即把回复拆成2到5条独立消息依次发送；这是不可自动重试的可见副作用，仅在拆分明显更符合群聊节奏时使用", map[string]any{
				"messages": map[string]any{
					"type": "array", "items": map[string]any{"type": "string"},
					"minItems": 2, "maxItems": 5, "description": "按发送顺序排列的多句话",
				},
				"interval_ms": integerProperty("消息间隔毫秒，默认500，范围0到3000"),
			}, "messages"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "多句话", "分多条回复", "拆分回复", "连续发送", "多段消息"},
			GroupAction: true,
			Handler:     p.handleSendMessages,
		},
		{
			Definition: agent.Function("at_user", "立即在当前群 @ 某人并发送文字；仅在必须明确提醒或指定对象时使用，成功后返回 message_id", map[string]any{
				"user_id": integerProperty("用户 QQ 号"),
				"text":    stringProperty("要说的话"),
			}, "user_id", "text"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "At某人", "@某人", "提醒某人", "指定用户回复"},
			GroupAction: true,
			Handler:     p.handleAtUser,
		},
		{
			Definition:  agent.Function("poke_user", "立即在当前群戳一戳某人；这是娱乐性可见动作，只在语境自然且不会打扰对方时使用", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "戳一戳", "戳某人", "poke"},
			GroupAction: true,
			Handler:     p.handlePokeUser,
		},
	}
}

func (p *Persona) handleSendImage(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		URL  string `json:"url"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.URL) == "" {
		return nil, errors.New("message URL cannot be empty")
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	segments := message.Message{}
	segments = append(segments, message.Image(input.URL))
	if len(strings.TrimSpace(input.Text)) > 0 {
		segments = append(segments, message.Text(input.Text))
	}
	id := ctx.SendGroupMessage(p.groupID, segments)
	markVisibleAction(rc, "send_image")
	if err := p.recordBotMessage(ctx, segments, id); err != nil {
		return nil, err
	}
	return map[string]any{"message_id": id}, nil
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
	markVisibleAction(rc, "send_message")
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
	for _, text := range input.Messages {
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("message text cannot be empty")
		}
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
		segments := message.Message{message.Text(text)}
		id := ctx.SendGroupMessage(p.groupID, segments)
		markVisibleAction(rc, "send_messages")
		if err := p.recordBotMessage(ctx, segments, id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		if i < len(input.Messages)-1 && input.IntervalMS > 0 {
			timer := time.NewTimer(time.Duration(input.IntervalMS) * time.Millisecond)
			if rc.Context == nil {
				<-timer.C
				continue
			}
			select {
			case <-timer.C:
			case <-rc.Done():
				timer.Stop()
				return nil, rc.Err()
			}
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
	markVisibleAction(rc, "at_user")
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
	markVisibleAction(rc, "poke_user")
	return "ok", nil
}

// Group-action handlers mark delivery immediately after the irreversible host
// call. This lets the harness suppress fallback or duplicate actions even when
// a later persistence step fails or a multi-message action is interrupted.
func markVisibleAction(rc *agent.RunContext, toolName string) {
	if rc == nil {
		return
	}
	rc.MarkActionPerformed()
	rc.MarkResponseDelivered("tool:" + toolName)
}
