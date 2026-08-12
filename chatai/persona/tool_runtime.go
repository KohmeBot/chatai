package persona

import (
	"encoding/json"
	"errors"
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
	ctx, ok := rc.Values["zero_ctx"].(*zero.Ctx)
	if !ok || ctx == nil {
		return nil, errors.New("zero context unavailable")
	}
	return ctx, nil
}

func (p *Persona) recordBotMessage(ctx *zero.Ctx, segments message.Message, id int64) error {
	return p.saveMessage(GroupMessage{User: User{UserId: ctx.Event.SelfID, Nickname: selfNickname}, Content: segments.ExtractPlainText(), MsgType: getMsgType(segments), MsgID: id, CreatedAt: time.Now(), Url: getUrl(segments), FileName: getFileName(segments)})
}
