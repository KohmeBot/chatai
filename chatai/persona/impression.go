package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
)

type impressionResult struct {
	Group string           `json:"group_impression"`
	Users []UserImpression `json:"user_impressions"`
}

func (p *Persona) impressionLoop() {
	ticker := time.NewTicker(p.opts.ImpressionEvery)
	defer ticker.Stop()
	for range ticker.C {
		_ = p.generateImpressions()
	}
}

func (p *Persona) generateImpressions() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	rows, cursor, err := p.impressionMessages(100)
	if err != nil {
		return err
	}
	if len(rows) < p.opts.ImpressionMin {
		return nil
	}
	messages := make([]GroupMessage, len(rows))
	for i := range rows {
		messages[i] = rows[i].message()
	}
	old, err := new(GroupImpression).Get(p.db, p.groupID)
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf(`请周期性总结长期印象。只保留稳定、未来交流有帮助的信息，忽略一次性事件和闲聊。
当前群印象：%s
本周期消息：
%s
严格返回 JSON：{"group_impression":"融合后的群印象，400字内，无变化则空字符串","user_impressions":[{"userId":123,"content":"融合后的用户印象，300字内"}]}`,
		old.String(), formatMessages(messages))
	res := new(model.Response)
	if err := p.opts.ImpressionModel.Request(&model.Request{Question: prompt}, res); err != nil {
		return err
	}
	if res.ErrorMsg != "" {
		return errors.New(res.ErrorMsg)
	}
	raw := strings.TrimSpace(res.Answer)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	var result impressionResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return err
	}
	if result.Group != "" {
		if err := new(GroupImpression).Update(p.db, GroupImpression{GroupID: p.groupID, Content: result.Group}); err != nil {
			return err
		}
	}
	for _, item := range result.Users {
		if item.UserID != 0 && item.Content != "" {
			if err := new(UserImpression).Update(p.db, item); err != nil {
				return err
			}
		}
	}
	return p.advanceImpressionCursor(cursor, rows[len(rows)-1].ID)
}
