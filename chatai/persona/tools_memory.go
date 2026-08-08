package persona

import (
	"encoding/json"
	"errors"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/favor"
)

func (p *Persona) memoryTools() []agent.Tool {
	return []agent.Tool{
		{
			Definition:  agent.Function("read_user_impression", "读取对某个用户的长期印象", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"),
			SearchTerms: []string{"用户印象", "对某人的印象", "长期记忆", "印象"},
			Handler:     p.handleReadUserImpression,
		},
		{
			Definition:  agent.Function("read_group_impression", "读取对当前群的长期印象", map[string]any{}),
			SearchTerms: []string{"群聊印象", "群印象", "长期记忆", "印象"},
			Handler:     p.handleReadGroupImpression,
		},
		{
			Definition:  agent.Function("read_user_favor", "读取指定用户当前的好感度、等级和说明", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"),
			SearchTerms: []string{"读取好感度", "查询好感度", "关系等级", "亲密度", "好感度"},
			Handler:     p.handleReadUserFavor,
		},
		{
			Definition: agent.Function("update_user_favor", "按增量修改指定用户的好感度；单次增加最多60，减少最多30", map[string]any{
				"user_id": integerProperty("用户 QQ 号"),
				"delta":   integerProperty("增加或减少的数值，负数表示减少"),
				"reason":  stringProperty("修改原因"),
			}, "user_id", "delta", "reason"),
			SearchTerms: []string{"修改好感度", "增加好感度", "减少好感度", "关系变化", "亲密度", "好感度"},
			Handler:     p.handleUpdateUserFavor,
		},
	}
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
