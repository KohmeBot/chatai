package persona

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func (p *Persona) chatActionTools() []agent.Tool {
	target, audience, rhythm := "当前群", "群友", "群聊节奏"
	imageURL := "图片URL、本地绝对路径或 search_skill_files 返回的相对整个 skills 目录的文件 path；不要使用 Skill 入口路径"
	text := "要发送的话；JSON 换行只转义一次，解码后应是真实换行"
	batch := "按发送顺序排列的多句话；JSON 换行只转义一次"
	quote := "可选，引用的消息 ID"
	if p.private != nil {
		target, audience, rhythm = "当前私聊", "当前用户", "对话节奏"
		imageURL = "公开的 HTTP/HTTPS 图片 URL，直接返回 PNG、JPEG、GIF 或 WebP，最大 5 MiB"
		if p.opts.Skills != nil && p.opts.Skills.Directory != nil {
			imageURL += "；也可使用 search_skill_files 返回的技能资源路径"
		}
		quote = "可选，引用本次私聊中的消息 ID"
	} else {
		text += "。需逐字保留代码、路径或转义符时使用 send_raw_message"
		batch += "，需逐字保留转义符时使用 send_raw_message"
	}
	tools := []agent.Tool{
		{
			Definition: agent.Function("send_image", "立即向"+target+"发送一张图片并可带一句话；成功的消息不可重复发送，message_id=0 时需修正后重试，成功后返回 message_id", map[string]any{
				"url":  stringProperty(imageURL),
				"text": stringProperty("可选，发送的图片附带的文字"),
			}, "url"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "回复", "图片", "发送图片", "发图"},
			GroupAction: true,
			Handler:     p.handleSendImage,
		},
		{
			Definition: agent.Function("send_message", "向"+target+"发送一句消息，可选择引用消息；普通文字回复也必须通过本工具发送。参数 text 只能包含面向"+audience+"的内容，不能包含内部分析或决策过程。成功后返回 message_id。", map[string]any{
				"text":             stringProperty(text),
				"reply_message_id": integerProperty(quote),
			}, "text"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "回复", "说话", "引用回复"},
			GroupAction: true,
			Handler:     p.handleSendMessage,
		},
		{
			Definition: agent.Function("send_messages", "立即把回复拆成2到5条独立消息依次发送；成功的消息不可重复发送，message_id=0 时需修正后重试，仅在拆分明显更符合"+rhythm+"时使用", map[string]any{
				"messages": map[string]any{
					"type": "array", "items": map[string]any{"type": "string"},
					"minItems": 2, "maxItems": 5, "description": batch,
				},
				"interval_ms": integerProperty("消息间隔毫秒，默认500，范围0到3000"),
			}, "messages"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "多句话", "分多条回复", "拆分回复", "连续发送", "多段消息"},
			GroupAction: true,
			Handler:     p.handleSendMessages,
		},
	}
	if p.private != nil {
		return tools
	}
	return append(tools,
		agent.Tool{
			Definition: agent.Function("at_user", "立即在当前群 @ 某人并发送文字；仅在必须明确提醒或指定对象时使用，成功后返回 message_id", map[string]any{
				"user_id": integerProperty("用户 QQ 号"),
				"text":    stringProperty("要说的话"),
			}, "user_id", "text"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "At某人", "@某人", "提醒某人", "指定用户回复"},
			Scope:       agent.ToolScopeGroup,
			GroupAction: true,
			Handler:     p.handleAtUser,
		},
		agent.Tool{
			Definition:  agent.Function("poke_user", "立即在当前群戳一戳某人；这是娱乐性可见动作，只在语境自然且不会打扰对方时使用", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"),
			Namespace:   "chat",
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"发送消息", "戳一戳", "戳某人", "poke"},
			Scope:       agent.ToolScopeGroup,
			GroupAction: true,
			Handler:     p.handlePokeUser,
		},
	)
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
	if err := p.validateChatText(input.Text); err != nil {
		return nil, err
	}
	source := strings.TrimSpace(input.URL)
	if p.private == nil {
		var err error
		source, err = p.resolveImageSource(source)
		if err != nil {
			return nil, fmt.Errorf("resolve image path: %w", err)
		}
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	var upload string
	if p.private != nil {
		upload, err = p.privateImage(rc.Context, source)
		if err != nil {
			return nil, err
		}
	}
	segments := message.Message{}
	segments = append(segments, message.Image(source))
	if len(strings.TrimSpace(input.Text)) > 0 {
		segments = append(segments, message.Text(input.Text))
	}
	outgoing := segments
	if upload != "" {
		outgoing = append(message.Message{message.Image(upload)}, segments[1:]...)
	}
	id := p.sendMessage(ctx, outgoing)
	if err := checkMessageID(id); err != nil {
		return nil, err
	}
	markVisibleAction(rc, "send_image")
	p.rememberBotDialogue(rc, segments, id)
	if err := p.recordBotMessage(ctx, segments, id); err != nil {
		return nil, err
	}
	return map[string]any{"message_id": id}, nil
}

const maxPrivateImageBytes = 5 << 20

func (p *Persona) privateImage(ctx context.Context, source string) (string, error) {
	if _, err := normalizeImageURL(source); err == nil || p.opts.Skills == nil || p.opts.Skills.Directory == nil {
		return fetchPrivateImage(ctx, source)
	}
	data, err := p.opts.Skills.ReadAsset(source, maxPrivateImageBytes)
	if err != nil {
		return "", err
	}
	return encodePrivateImage(data)
}

func fetchPrivateImage(ctx context.Context, source string) (string, error) {
	source, err := normalizeImageURL(source)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return "", err
	}
	if req.URL.User != nil {
		return "", errors.New("image URL cannot include credentials")
	}

	// Connect to a checked address without letting OneBot refetch the URL.
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		dialer := &net.Dialer{}
		for _, addr := range addresses {
			if addr.IsGlobalUnicast() && !addr.IsPrivate() {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
				if err == nil {
					return conn, nil
				}
			}
		}
		return nil, errors.New("could not connect to a public image host")
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch image: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("image URL returned HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxPrivateImageBytes+1))
	if err != nil {
		return "", err
	}
	return encodePrivateImage(data)
}

func encodePrivateImage(data []byte) (string, error) {
	if len(data) > maxPrivateImageBytes {
		return "", errors.New("image exceeds 5 MiB")
	}
	switch http.DetectContentType(data) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return "base64://" + base64.StdEncoding.EncodeToString(data), nil
	default:
		return "", errors.New("expected a PNG, JPEG, GIF or WebP image")
	}
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
	if err := p.validateChatText(input.Text); err != nil {
		return nil, err
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	segments := message.Message{}
	if p.private != nil && input.ReplyID != 0 {
		if _, err := p.privateMessageByID(input.ReplyID); err != nil {
			return nil, fmt.Errorf("verify reply in current private conversation: %w", err)
		}
		segments = append(segments, message.Reply(input.ReplyID))
	} else if input.ReplyID > 0 {
		segments = append(segments, message.Reply(input.ReplyID))
		if u := ctx.GetMessage(input.ReplyID).Sender; u != nil {
			segments = append(segments, message.At(u.ID))
			segments = append(segments, message.Text(" "))
		}
	}
	segments = append(segments, message.Text(input.Text))
	id := p.sendMessage(ctx, segments)
	if err := checkMessageID(id); err != nil {
		return nil, err
	}
	markVisibleAction(rc, "send_message")
	p.rememberBotDialogue(rc, segments, id)
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
		if err := p.validateChatText(text); err != nil {
			return nil, err
		}
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
		id := p.sendMessage(ctx, segments)
		if err := checkMessageID(id); err != nil {
			return map[string]any{"message_ids": ids, "failed_index": i, "unsent_messages": input.Messages[i:]}, fmt.Errorf("batch stopped at index %d; %d messages already sent: %w", i, len(ids), err)
		}
		markVisibleAction(rc, "send_messages")
		p.rememberBotDialogue(rc, segments, id)
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
	if err := validateMessageText(input.Text); err != nil {
		return nil, err
	}
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	segments := message.Message{message.At(input.UserID), message.Text(" " + input.Text)}
	id := ctx.SendGroupMessage(p.groupID, segments)
	if err := checkMessageID(id); err != nil {
		return nil, err
	}
	markVisibleAction(rc, "at_user")
	p.rememberBotDialogue(rc, segments, id)
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
