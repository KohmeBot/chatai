package persona

import (
	"encoding/json"
	"fmt"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"html"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	MsgTypeText    = "text"
	MsgTypeImg     = "image"
	MsgTypeAt      = "at"
	MsgTypePoke    = "poke"
	MsgTypeReply   = "reply"
	MsgTypeForward = "forward"
	MsgTypeRecord  = "record"
	MsgTypeJson    = "json"
)

func HasMsgType(typ string) bool {
	return slices.Contains([]string{
		MsgTypeText,
		MsgTypeImg,
		MsgTypeAt,
		MsgTypePoke,
		MsgTypeReply,
		MsgTypeForward,
		MsgTypeRecord,
		MsgTypeJson,
	}, typ)
}

func MsgTypeString(typ string) string {
	switch typ {
	case MsgTypeText:
		return "文本"
	case MsgTypeImg:
		return "图片或表情包"
	case MsgTypeAt:
		return "At"
	case MsgTypePoke:
		return "戳一戳"
	case MsgTypeReply:
		return "回复"
	case MsgTypeForward:
		return "转发(搬屎)"
	case MsgTypeRecord:
		return "语音"
	case MsgTypeJson:
		return "分享内容(搬屎)"
	}
	return ""
}

type User struct {
	UserId   int64
	Nickname string
}

func (u User) String() string {
	return fmt.Sprintf("%s(%d)", u.Nickname, u.UserId)
}

type GroupMessage struct {
	User          User      // 发言人
	TargetUser    User      // 群内@对方的ID或者是reply的人
	QuotedUser    User      // 被引用消息的发言人
	Content       string    // 文字内容
	QuotedContent string    // 被引用消息的文字内容
	MsgType       string    // text/image/poke/mixed
	MsgID         int64     // 消息ID,可以定位消息
	QuotedMsgID   int64     // 被引用消息ID
	CreatedAt     time.Time // 创建时间
	Url           string    // url
	FileName      string    // file name
	QuotedURL     string    // 被引用消息中的图片 URL
	Refer         bool      // 是否已引用

}

func (g GroupMessage) ContentEqual(msg GroupMessage) bool {
	if g.MsgType != msg.MsgType {
		return false
	}
	switch g.MsgType {
	case MsgTypeText:
		return g.Content == msg.Content
	case MsgTypeImg:
		sameImage := (g.Url != "" && g.Url == msg.Url) || (g.FileName != "" && g.FileName == msg.FileName)
		return g.Content == msg.Content && sameImage
	case MsgTypeAt:
		return g.Content == msg.Content && g.TargetUser == msg.TargetUser
	case MsgTypePoke:
		return g.TargetUser == msg.TargetUser
	case MsgTypeReply:
		return g.Content == msg.Content && g.TargetUser == msg.TargetUser
	case MsgTypeForward:
		// TODO 还没实现
		return false
	case MsgTypeRecord:
		// TODO 还没实现
		return false
	case MsgTypeJson:
		// TODO 还没实现
		return false
	}

	return false
}

func (g GroupMessage) IsEmpty() bool {
	return g == GroupMessage{}
}

func newMessage(ctx *zero.Ctx) GroupMessage {
	segments := ctx.Event.Message
	quoted := getQuotedMessage(ctx, segments)

	msgType := getMsgType(segments)

	if ctx.Event.SubType == MsgTypePoke {
		msgType = MsgTypePoke
	}

	if !HasMsgType(msgType) {
		return GroupMessage{}
	}

	msgId, _ := ctx.Event.MessageID.(int64)

	msg := GroupMessage{
		User: User{
			UserId:   ctx.Event.UserID,
			Nickname: ctx.CardOrNickName(ctx.Event.UserID),
		},
		TargetUser: User{},
		Url:        firstNonEmpty(getUrl(segments), getUrl(quoted.Elements)),
		Content:    getText(segments),
		FileName:   firstNonEmpty(getFileName(segments), getFileName(quoted.Elements)),
		MsgType:    msgType,
		MsgID:      msgId,
		CreatedAt:  time.Now(),
	}
	msg.QuotedMsgID = replyMessageID(segments)
	msg.QuotedContent = getText(quoted.Elements)
	msg.QuotedURL = getUrl(quoted.Elements)
	if quoted.Sender != nil {
		msg.QuotedUser = User{UserId: quoted.Sender.ID, Nickname: quoted.Sender.Name()}
	}

	target := getTargetID(ctx)
	if target > 0 {
		msg.TargetUser = User{
			UserId:   target,
			Nickname: ctx.CardOrNickName(target),
		}
	}

	if ctx.Event.IsToMe {
		msg.TargetUser = User{
			Nickname: "你",
			UserId:   ctx.Event.SelfID,
		}
	}
	return msg
}

func getMsgType(msgs message.Message) string {
	var msgType string
	for _, m := range msgs {
		msgType = m.Type
		if msgType != MsgTypeText {
			// 找到第一个非text的消息
			break
		}
	}
	return msgType
}

func getUrl(msgs message.Message) string {
	for _, m := range msgs {
		if m.Type == MsgTypeImg {
			if imageURL := m.Data["url"]; imageURL != "" {
				return imageURL
			}
			if file := m.Data["file"]; strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
				return file
			}
		}
	}
	return ""
}

// repeatMessage 将接收事件里的图片转换为可发送的图片段。入站 file 通常是
// OneBot 实现的临时缓存标识，而 url 才能跨消息重新上传。
func repeatMessage(msgs message.Message) message.Message {
	result := make(message.Message, 0, len(msgs))
	for _, segment := range msgs {
		if segment.Type != MsgTypeImg {
			result = append(result, segment)
			continue
		}
		file := firstNonEmpty(segment.Data["url"], segment.Data["file"])
		image := message.Image(file)
		if summary := segment.Data["summary"]; summary != "" {
			image.Data["summary"] = summary
		}
		result = append(result, image)
	}
	return result
}

func getText(msgs message.Message) string {
	if getMsgType(msgs) == MsgTypeJson {
		for _, sg := range msgs {
			data := sg.Data["data"]
			parsed := parseQQMiniCard(data)
			if parsed == "" {
				return data
			}
			return parsed

		}
	}

	return msgs.ExtractPlainText()
}
func getFileName(msgs message.Message) string {
	for _, m := range msgs {
		if m.Type == MsgTypeImg {
			return m.Data["file"]
		}
	}
	return ""
}

func getTargetID(ctx *zero.Ctx) int64 {
	if ctx.Event.TargetID != 0 {
		return ctx.Event.TargetID
	}
	return getTargetIDFromMsgs(ctx, ctx.Event.Message)
}

func getTargetIDFromMsgs(ctx *zero.Ctx, msgs message.Message) int64 {
	var targetID int64
	// 优先找at
	for _, segment := range msgs {
		if segment.Type == "at" {
			targetID, _ = strconv.ParseInt(segment.Data["qq"], 10, 64)
			break
		}
	}
	if targetID != 0 {
		return targetID
	}

	// 找引用的消息
	for _, segment := range msgs {
		if segment.Type == "reply" {
			msgId, _ := strconv.ParseInt(segment.Data["id"], 10, 64)
			m := ctx.GetMessage(msgId)
			if m.Sender != nil {
				targetID = m.Sender.ID
			}
			break
		}
	}

	return targetID
}

func getQuotedMessage(ctx *zero.Ctx, msgs message.Message) zero.Message {
	for _, segment := range msgs {
		if segment.Type != MsgTypeReply {
			continue
		}
		messageID, err := strconv.ParseInt(segment.Data["id"], 10, 64)
		if err == nil && messageID > 0 {
			return ctx.GetMessage(messageID, true)
		}
	}
	return zero.Message{}
}

func replyMessageID(msgs message.Message) int64 {
	for _, segment := range msgs {
		if segment.Type == MsgTypeReply {
			messageID, _ := strconv.ParseInt(segment.Data["id"], 10, 64)
			return messageID
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func formatTime(t time.Time) string {
	elapsed := time.Since(t)
	switch {
	case elapsed < 20*time.Second:
		return "刚刚"
	case elapsed < time.Minute:
		return fmt.Sprintf("%d秒前", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%d分%d秒前", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%d小时%d分前", int(elapsed.Hours()), int(elapsed.Minutes())%60)
	default:
		return t.Format("01-02 15:04")
	}
}

func formatMessages(msgs []GroupMessage) string {
	var builder strings.Builder
	for _, msg := range msgs {
		str := formatMessage(msg)
		if str == "" {
			continue
		}
		builder.WriteString(str)
		builder.WriteString("\n")
	}
	return builder.String()

}

func formatMessage(msg GroupMessage) string {
	var builder strings.Builder
	u := msg.User

	target := msg.TargetUser
	if target.UserId <= 0 {
		target = User{
			UserId:   0,
			Nickname: "某人",
		}
	}
	var content string
	var action string
	switch msg.MsgType {
	case MsgTypeText:
		action = "说"
	case MsgTypeImg:
		action = "发了一张图或表情包"
	case MsgTypeAt:

		action = fmt.Sprintf("@%s 说", target.Nickname)
	case MsgTypePoke:

		action = fmt.Sprintf("戳了戳%s", target.Nickname)
	case MsgTypeReply:

		action = fmt.Sprintf("回复%s", target.Nickname)
	case MsgTypeForward:
		action = "转了一条消息(搬屎)"
	case MsgTypeRecord:
		action = "发了条语音"
	case MsgTypeJson:
		action = "分享了一条内容(搬屎)"
	}
	if action == "" {
		return ""
	}
	content = msg.Content
	if runeLen(content) > 500 {
		content = string([]rune(content)[:500]) + "..."
	}
	if msg.Refer {
		builder.WriteString("[已回复] ")
	}
	// <ID> [5-15 11:11] 某某: XXX
	builder.WriteString(fmt.Sprintf("<%d> [%s] %s [%s]", msg.MsgID, formatTime(msg.CreatedAt), u.String(), action))
	if content != "" {
		builder.WriteString(fmt.Sprintf(": %s", content))
	}
	if msg.QuotedMsgID > 0 || msg.QuotedContent != "" || msg.QuotedURL != "" {
		quotedUser := msg.QuotedUser
		if quotedUser.UserId <= 0 {
			quotedUser = target
		}
		quotedContent := msg.QuotedContent
		if runeLen(quotedContent) > 500 {
			quotedContent = string([]rune(quotedContent)[:500]) + "..."
		}
		builder.WriteString(fmt.Sprintf("\n  └─ 引用 <%d> %s", msg.QuotedMsgID, quotedUser.String()))
		if quotedContent != "" {
			builder.WriteString(": " + quotedContent)
		}
		if msg.QuotedURL != "" {
			builder.WriteString(" [图片: " + msg.QuotedURL + "]")
		}
	}
	return builder.String()
}
func runeLen(s string) int {
	return len([]rune(s))
}

func parseQQMiniCard(raw string) string {
	// 1. HTML 实体解码
	// &#44; -> ,
	// &#91; -> [
	// &amp; -> &
	raw = html.UnescapeString(raw)

	// 2. 直接尝试解析
	var data struct {
		Meta struct {
			Detail struct {
				Title    string `json:"title"`
				Desc     string `json:"desc"`
				URL      string `json:"url"`
				QQDocURL string `json:"qqdocurl"`
			} `json:"detail_1"`
		} `json:"meta"`
	}

	err := json.Unmarshal([]byte(raw), &data)

	// 如果传入的是 {\"ver\":\"...\"} 这种被额外转义过的 JSON
	if err != nil {
		raw = strings.ReplaceAll(raw, `\"`, `"`)
		err = json.Unmarshal([]byte(raw), &data)
	}

	if err != nil {
		return ""
	}

	detail := data.Meta.Detail

	jumpURL := detail.QQDocURL
	if jumpURL == "" {
		jumpURL = detail.URL
	}

	return fmt.Sprintf(
		"标题：%s\n描述：%s\n跳转链接：%s",
		detail.Title,
		detail.Desc,
		jumpURL,
	)
}
