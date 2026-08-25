package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/favor"
)

func (p *Persona) memoryTools() []agent.Tool {
	tools := []agent.Tool{
		{
			Definition:  agent.Function("read_user_impression", "读取对某个用户的长期印象", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"),
			Namespace:   "memory",
			ReadOnly:    true,
			Idempotent:  true,
			Risk:        agent.ToolRiskLow,
			SearchTerms: []string{"用户印象", "对某人的印象", "长期记忆", "印象"},
			Handler:     p.handleReadUserImpression,
		},
		{
			Definition:  agent.Function("read_group_impression", "读取对当前群的长期印象", map[string]any{}),
			Namespace:   "memory",
			ReadOnly:    true,
			Idempotent:  true,
			Risk:        agent.ToolRiskLow,
			SearchTerms: []string{"群聊印象", "群印象", "长期记忆", "印象"},
			Handler:     p.handleReadGroupImpression,
		},
		{
			Definition:  agent.Function("read_user_favor", "读取指定用户当前的好感度、等级和说明", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"),
			Namespace:   "memory",
			ReadOnly:    true,
			Idempotent:  true,
			Risk:        agent.ToolRiskLow,
			SearchTerms: []string{"读取好感度", "查询好感度", "关系等级", "亲密度", "好感度"},
			Handler:     p.handleReadUserFavor,
		},
		{
			Definition: agent.Function("update_user_favor", "按增量修改指定用户的好感度；单次增加最多60，减少最多30", map[string]any{
				"user_id": integerProperty("用户 QQ 号"),
				"delta":   integerProperty("增加或减少的数值，负数表示减少"),
				"reason":  stringProperty("修改原因"),
			}, "user_id", "delta", "reason"),
			Namespace:   "memory",
			Risk:        agent.ToolRiskHigh,
			SearchTerms: []string{"修改好感度", "增加好感度", "减少好感度", "关系变化", "亲密度", "好感度"},
			Handler:     p.handleUpdateUserFavor,
		},
	}
	if p.opts.ImpressionUpdateEnable {
		tools = append(tools, agent.Tool{
			Definition: agent.Function("update_impression", "更新当前群或某位群友的长期印象。先读取对应旧印象，把读取到的 content 原样传入 previous_content；content 必须是融合新旧信息后的完整内容，不是追加片段。只记录稳定、长期有用的信息，忽略一次性事件和闲聊", map[string]any{
				"scope": map[string]any{
					"type":        "string",
					"enum":        []string{"group", "user"},
					"description": "group 表示当前群印象，user 表示群友印象",
				},
				"user_id":          integerProperty("群友 QQ 号；scope=user 时必填"),
				"previous_content": stringProperty("read_group_impression 或 read_user_impression 刚返回的 content，必须原样传入；没有旧印象时传空字符串"),
				"content":          stringProperty("融合原内容后的完整长期印象；群印象最多400字，群友印象最多300字"),
				"reason":           stringProperty("本次更新所依据的长期有效信息"),
			}, "scope", "previous_content", "content", "reason"),
			Namespace:   "memory",
			Idempotent:  true,
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"更新印象", "更新长期印象", "修改印象", "记录印象", "记住群友", "长期记忆", "群友印象", "用户印象", "群聊印象", "群印象", "印象"},
			Handler:     p.handleUpdateImpression,
		})
	}
	return tools
}

func (p *Persona) handleReadUserImpression(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	userID, err := decodeUserID(raw)
	if err != nil {
		return nil, err
	}
	impression, err := new(UserImpression).Get(p.db, userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"scope": "user", "user_id": impression.UserID, "content": impression.Content}, nil
}

func (p *Persona) handleReadGroupImpression(_ *agent.RunContext, _ json.RawMessage) (any, error) {
	impression, err := new(GroupImpression).Get(p.db, p.groupID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"scope": "group", "group_id": impression.GroupID, "content": impression.Content}, nil
}

func (p *Persona) handleUpdateImpression(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		Scope           string `json:"scope"`
		UserID          int64  `json:"user_id"`
		PreviousContent string `json:"previous_content"`
		Content         string `json:"content"`
		Reason          string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	input.Scope = strings.ToLower(strings.TrimSpace(input.Scope))
	input.Content = strings.TrimSpace(input.Content)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Content == "" {
		return nil, errors.New("content must not be empty")
	}
	if input.Reason == "" {
		return nil, errors.New("reason must not be empty")
	}

	switch input.Scope {
	case "group":
		if utf8.RuneCountInString(input.Content) > 400 {
			return nil, errors.New("group impression content must not exceed 400 characters")
		}
		before, err := new(GroupImpression).Get(p.db, p.groupID)
		if err != nil {
			return nil, err
		}
		if before.Content != input.PreviousContent {
			return nil, errors.New("group impression changed; read it again before updating")
		}
		if before.Content == input.Content {
			return map[string]any{"scope": "group", "group_id": p.groupID, "changed": false, "content": input.Content, "reason": input.Reason}, nil
		}
		if err := new(GroupImpression).Update(p.db, GroupImpression{GroupID: p.groupID, Content: input.Content}); err != nil {
			return nil, err
		}
		return map[string]any{"scope": "group", "group_id": p.groupID, "changed": true, "before": before.Content, "after": input.Content, "reason": input.Reason}, nil
	case "user":
		if err := validateUserID(input.UserID); err != nil {
			return nil, err
		}
		if utf8.RuneCountInString(input.Content) > 300 {
			return nil, errors.New("user impression content must not exceed 300 characters")
		}
		before, err := new(UserImpression).Get(p.db, input.UserID)
		if err != nil {
			return nil, err
		}
		if before.Content != input.PreviousContent {
			return nil, errors.New("user impression changed; read it again before updating")
		}
		if before.Content == input.Content {
			return map[string]any{"scope": "user", "user_id": input.UserID, "changed": false, "content": input.Content, "reason": input.Reason}, nil
		}
		if err := new(UserImpression).Update(p.db, UserImpression{UserID: input.UserID, Content: input.Content}); err != nil {
			return nil, err
		}
		return map[string]any{"scope": "user", "user_id": input.UserID, "changed": true, "before": before.Content, "after": input.Content, "reason": input.Reason}, nil
	default:
		return nil, fmt.Errorf("scope must be group or user, got %q", input.Scope)
	}
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
