package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

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

func zeroContext(rc *agent.RunContext) (*zero.Ctx, error) {
	if rc == nil {
		return nil, errors.New("zero context unavailable")
	}
	ctx, ok := rc.Values["zero_ctx"].(*zero.Ctx)
	if !ok || ctx == nil {
		return nil, errors.New("zero context unavailable")
	}
	return ctx, nil
}

// SendHostMessage checks delivery for host-generated group messages as well.
// A failed send enters the Agent with the original content and failure count.
func (p *Persona) SendHostMessage(ctx *zero.Ctx, segments message.Message) error {
	id := ctx.SendGroupMessage(p.groupID, segments)
	if err := checkMessageID(id); err != nil {
		instruction := "向当前群发送以下宿主生成的消息，保持事实和数字不变。此前发送失败，请修正格式后重试：" + encodeSegments(segments)
		return p.runWithDeliveryFailure(ctx, GroupMessage{User: User{UserId: ctx.Event.UserID}}, instruction, fmt.Errorf("宿主消息发送失败: %w", err))
	}
	return p.recordBotMessage(ctx, segments, id)
}

func (p *Persona) recordBotMessage(ctx *zero.Ctx, segments message.Message, id int64) error {
	if err := checkMessageID(id); err != nil {
		return err
	}
	return p.saveMessage(GroupMessage{RawSegments: encodeSegments(segments), User: User{UserId: ctx.Event.SelfID, Nickname: selfNickname}, Content: segments.ExtractPlainText(), MsgType: getMsgType(segments), MsgID: id, CreatedAt: time.Now(), Url: getUrl(segments), FileName: getFileName(segments)})
}

func checkMessageID(id int64) error {
	if id != 0 {
		return nil
	}
	logrus.Warn("[Agent][发送失败] message_id=0")
	return agent.ErrMessageDeliveryFailed
}
