package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/favor"
)

func (p *Persona) memoryTools() []agent.Tool {
	tools := []agent.Tool{
		{
			Definition:  agent.Function("read_user_impression", "兼容工具：读取用户简介和结构化记忆；优先使用read_user_memory", map[string]any{"user_id": integerProperty("用户 QQ 号")}, "user_id"),
			Namespace:   "memory",
			ReadOnly:    true,
			Idempotent:  true,
			Risk:        agent.ToolRiskLow,
			SearchTerms: []string{"用户印象", "对某人的印象", "长期记忆", "印象"},
			Handler:     p.handleReadUserImpression,
		},
		{
			Definition:  agent.Function("read_group_impression", "兼容工具：读取群简介和结构化群记忆；优先使用read_group_memory", map[string]any{}),
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
			Definition: agent.Function("update_user_favor", "兼容工具：修改统一用户记忆内的关系分值，单次最多增减5；优先使用有来源依据的update_user_memory", map[string]any{
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
	if !p.opts.ImpressionUpdateEnable {
		tools = tools[:3] // All memory writes share the same configuration switch.
	}
	if p.opts.ImpressionUpdateEnable {
		tools = append(tools, agent.Tool{
			Definition: agent.Function("update_impression", "兼容工具：更新某位群友简介；群记忆请使用update_group_memory。先读取对应旧印象，把读取到的 content 原样传入 previous_content；content 必须是融合新旧信息后的完整内容，不是追加片段。只记录稳定、长期有用的信息，忽略一次性事件和闲聊", map[string]any{
				"scope": map[string]any{
					"type":        "string",
					"enum":        []string{"user"},
					"description": "user 表示群友印象；群记忆使用 update_group_memory",
				},
				"user_id":          integerProperty("群友 QQ 号；scope=user 时必填"),
				"previous_content": stringProperty("read_user_impression 刚返回的 content，必须原样传入；没有旧印象时传空字符串"),
				"content":          stringProperty("融合原内容后的完整长期印象；群友印象最多300字"),
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
	profile, err := readUserMemory(p.db, userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"scope": "user", "user_id": userID, "content": profile.Summary, "memory": visibleUserProfile(profile, p.groupID, time.Now(), 30)}, nil
}

func (p *Persona) handleReadGroupImpression(_ *agent.RunContext, _ json.RawMessage) (any, error) {
	impression, err := readGroupMemory(p.db, p.groupID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"scope": "group", "group_id": impression.GroupID, "content": impression.Summary, "memory": visibleGroupProfile(impression, time.Now(), 30)}, nil
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
		return nil, errors.New("use read_group_memory and update_group_memory with version and source evidence")
	case "user":
		if err := validateUserID(input.UserID); err != nil {
			return nil, err
		}
		if utf8.RuneCountInString(input.Content) > 300 {
			return nil, errors.New("user impression content must not exceed 300 characters")
		}
		before, err := readUserMemory(p.db, input.UserID)
		if err != nil {
			return nil, err
		}
		if before.Summary != input.PreviousContent {
			return nil, errors.New("user impression changed; read it again before updating")
		}
		if before.Summary == input.Content {
			return map[string]any{"scope": "user", "user_id": input.UserID, "changed": false, "content": input.Content, "reason": input.Reason}, nil
		}
		previous := before.Summary
		before.Summary = input.Content
		if err := writeUserMemory(p.db, before); err != nil {
			return nil, err
		}
		return map[string]any{"scope": "user", "user_id": input.UserID, "changed": true, "before": previous, "after": input.Content, "reason": input.Reason}, nil
	default:
		return nil, fmt.Errorf("scope must be group or user, got %q", input.Scope)
	}
}

func (p *Persona) handleReadUserFavor(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	userID, err := decodeUserID(raw)
	if err != nil {
		return nil, err
	}
	profile, err := readUserMemory(p.db, userID)
	if err != nil {
		return nil, err
	}
	level := favor.GetFavorLevelInfo(profile.Relationship.Score)
	return map[string]any{"user_id": userID, "favor": profile.Relationship.Score, "level": level.Name, "description": level.Desc, "relationship": profile.Relationship, "range": []int64{favor.FavorMin, favor.FavorMax}}, nil
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
	if strings.TrimSpace(input.Reason) == "" || utf8.RuneCountInString(input.Reason) > 200 {
		return nil, errors.New("reason must contain 1 to 200 characters")
	}
	profile, err := readUserMemory(p.db, input.UserID)
	if err != nil {
		return nil, err
	}
	before := profile.Relationship.Score
	profile.Relationship.Score = max(favor.FavorMin, min(favor.FavorMax, before+max(-5, min(5, input.Delta))))
	profile.Relationship.Reason = input.Reason
	if err := writeUserMemory(p.db, profile); err != nil {
		return nil, err
	}
	after := profile.Relationship.Score
	level := favor.GetFavorLevelInfo(after)
	return map[string]any{"user_id": input.UserID, "before": before, "after": after, "actual_delta": after - before, "level": level.Name, "reason": input.Reason}, nil
}
