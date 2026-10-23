package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func (p *Persona) mediaTools() []agent.Tool {
	return []agent.Tool{
		{
			Definition: agent.Function("send_video", "向当前群发送视频，成功后返回 message_id；成功的消息不可重复发送，message_id=0 时需修正后重试", map[string]any{
				"url": stringProperty("视频 URL、本地绝对路径或相对整个 skills 目录的文件路径；本地文件须能被 OneBot 服务访问"),
			}, "url"),
			Namespace: "chat", Risk: agent.ToolRiskMedium, GroupAction: true,
			SearchTerms: []string{"发送消息", "发送视频", "视频", "短视频", "video"}, Handler: p.handleSendVideo,
		},
		{
			Definition: agent.Function("send_record", "向当前群发送语音条（已有音频文件），成功后返回 message_id；成功的消息不可重复发送，message_id=0 时需修正后重试", map[string]any{
				"url": stringProperty("音频 URL、本地绝对路径或相对整个 skills 目录的文件路径；本地文件须能被 OneBot 服务访问"),
			}, "url"),
			Namespace: "chat", Risk: agent.ToolRiskMedium, GroupAction: true,
			SearchTerms: []string{"发送消息", "发送语音", "语音条", "音频", "voice", "record"}, Handler: p.handleSendRecord,
		},
		{
			Definition: agent.Function("send_music", "向当前群发送音乐分享卡片。平台歌曲提供 type 和 id；自定义卡片提供 type=custom、url、audio、title，可附 content 和 image。成功后返回 message_id，成功的消息不可重复发送，message_id=0 时需修正后重试", map[string]any{
				"type":    map[string]any{"type": "string", "enum": []string{"qq", "163", "xm", "custom"}, "description": "音乐平台：QQ音乐、网易云音乐、虾米或自定义；实际支持取决于 OneBot 服务"},
				"id":      stringProperty("平台歌曲 ID，使用字符串以保留完整 ID"),
				"url":     stringProperty("自定义卡片点击跳转的 HTTP/HTTPS URL"),
				"audio":   stringProperty("自定义卡片音频 HTTP/HTTPS URL"),
				"title":   stringProperty("自定义卡片标题"),
				"content": stringProperty("可选，自定义卡片简介或歌手"),
				"image":   stringProperty("可选，自定义卡片封面 HTTP/HTTPS URL"),
			}, "type"),
			Namespace: "chat", Risk: agent.ToolRiskMedium, GroupAction: true,
			SearchTerms: []string{"发送消息", "音乐分享卡片", "分享音乐", "歌曲", "QQ音乐", "网易云音乐", "music"}, Handler: p.handleSendMusic,
		},
	}
}

func (p *Persona) handleSendVideo(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	return p.sendMediaFile(rc, raw, "send_video", message.Video)
}

func (p *Persona) handleSendRecord(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	return p.sendMediaFile(rc, raw, "send_record", message.Record)
}

func (p *Persona) sendMediaFile(rc *agent.RunContext, raw json.RawMessage, name string, segment func(string) message.Segment) (any, error) {
	var input struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.URL) == "" {
		return nil, errors.New("media URL cannot be empty")
	}
	source, err := p.resolveImageSource(input.URL)
	if err != nil {
		return nil, fmt.Errorf("resolve media path: %w", err)
	}
	return p.sendMediaMessage(rc, name, segment(source))
}

func (p *Persona) handleSendMusic(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		URL     string `json:"url"`
		Audio   string `json:"audio"`
		Title   string `json:"title"`
		Content string `json:"content"`
		Image   string `json:"image"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	var segment message.Segment
	switch input.Type {
	case "qq", "163", "xm":
		id := strings.TrimSpace(input.ID)
		if id == "" {
			return nil, errors.New("platform music requires id")
		}
		segment = message.Segment{Type: "music", Data: map[string]string{"type": input.Type, "id": id}}
	case "custom":
		if strings.TrimSpace(input.Title) == "" {
			return nil, errors.New("custom music requires title")
		}
		for field, value := range map[string]string{"url": input.URL, "audio": input.Audio, "image": input.Image} {
			if field == "image" && value == "" {
				continue
			}
			parsed, err := url.ParseRequestURI(value)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return nil, fmt.Errorf("music %s must be an absolute HTTP/HTTPS URL", field)
			}
		}
		segment = message.CustomMusic(input.URL, input.Audio, input.Title)
		if input.Content != "" {
			segment.Data["content"] = input.Content
		}
		if input.Image != "" {
			segment.Data["image"] = input.Image
		}
	default:
		return nil, errors.New("music type must be qq, 163, xm or custom")
	}
	return p.sendMediaMessage(rc, "send_music", segment)
}

func (p *Persona) sendMediaMessage(rc *agent.RunContext, name string, segment message.Segment) (any, error) {
	ctx, err := zeroContext(rc)
	if err != nil {
		return nil, err
	}
	segments := message.Message{segment}
	id := ctx.SendGroupMessage(p.groupID, segments)
	if err := checkMessageID(id); err != nil {
		return nil, err
	}
	markVisibleAction(rc, name)
	p.rememberBotDialogue(rc, segments, id)
	if err := p.recordBotMessage(ctx, segments, id); err != nil {
		return nil, err
	}
	return map[string]any{"message_id": id}, nil
}
