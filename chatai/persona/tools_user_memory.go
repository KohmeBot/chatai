package persona

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"gorm.io/gorm"
)

func (p *Persona) structuredMemoryTools() []agent.Tool {
	tools := []agent.Tool{{
		Definition: agent.Function("read_user_memory", "读取结构化用户记忆：稳定认识、偏好、边界、近期状态、共同经历及关系好感度；返回version供更新使用", map[string]any{"user_id": integerProperty("用户QQ号")}, "user_id"),
		Namespace:  "memory", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow,
		SearchTerms: []string{"用户记忆", "群友印象", "长期记忆", "关系", "好感度", "偏好"},
		Handler:     p.handleReadUserMemory,
	}}
	if !p.opts.ImpressionUpdateEnable {
		return tools
	}
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":        stringProperty("稳定唯一键，同键替换"),
			"kind":       map[string]any{"type": "string", "enum": []string{"fact", "preference", "boundary", "state", "experience"}},
			"text":       stringProperty("内容，最多200字"),
			"source_id":  integerProperty("read_user_context返回的本人发言source_id，不是message_id"),
			"confidence": map[string]any{"type": "string", "enum": []string{"explicit", "inferred"}},
			"expires_at": stringProperty("可选RFC3339过期时间；state默认14天"),
		},
		"required": []string{"key", "kind", "text", "source_id", "confidence"},
	}
	tools = append(tools, agent.Tool{
		Definition: agent.Function("update_user_memory", "按版本更新结构化用户记忆。明确记住、更正、遗忘时使用。先read_user_memory及read_user_context核实本人消息；forget按key删除，同时清理summary对应内容；不得伪造关系", map[string]any{
			"user_id":          integerProperty("用户QQ号"),
			"previous_version": integerProperty("read_user_memory返回的version"),
			"evidence_ids":     map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 1, "maxItems": 20},
			"reason":           stringProperty("本次变化的依据，最多200字"),
			"summary":          stringProperty("可选，替换核心简介，最多600字，可为空以清除"),
			"upsert":           map[string]any{"type": "array", "items": item, "maxItems": 20},
			"forget":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 30},
			"relationship": map[string]any{"type": "object", "properties": map[string]any{
				"delta":       integerProperty("好感变化，-5到5，普通闲聊通常0"),
				"description": stringProperty("关系和互动方式，最多200字"),
			}, "required": []string{"delta", "description"}},
		}, "user_id", "previous_version", "evidence_ids", "reason"),
		Namespace: "memory", Risk: agent.ToolRiskMedium,
		SearchTerms: []string{"记住", "纠正记忆", "忘记", "删除记忆", "更新用户记忆", "关系变化"},
		Handler:     p.handleUpdateUserMemory,
	})
	return tools
}

func (p *Persona) handleReadUserMemory(_ *agent.RunContext, raw json.RawMessage) (any, error) {
	userID, err := decodeUserID(raw)
	if err != nil {
		return nil, err
	}
	profile, err := readUserMemory(p.db, userID)
	if err != nil {
		return nil, err
	}
	return visibleUserProfile(profile, p.groupID, time.Now(), 30), nil
}

func (p *Persona) handleUpdateUserMemory(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var change MemoryChange
	if err := json.Unmarshal(raw, &change); err != nil {
		return nil, err
	}
	if err := validateUserID(change.UserID); err != nil {
		return nil, err
	}
	if len(change.EvidenceIDs) == 0 || len(change.EvidenceIDs) > 20 {
		return nil, errors.New("1 to 20 evidence_ids required")
	}
	selfID, err := runSelfUserID(rc)
	if err != nil {
		return nil, err
	}
	var next UserProfile
	err = p.db.Transaction(func(tx *gorm.DB) error {
		profile, err := readUserMemory(tx, change.UserID)
		if err != nil {
			return err
		}
		var rows []ChatMessageRecord
		if err := tx.Where("group_id = ? AND id IN ?", p.groupID, change.EvidenceIDs).Find(&rows).Error; err != nil {
			return err
		}
		next, err = applyMemoryChange(profile, change, rows, p.groupID, selfID, time.Now())
		if err != nil {
			return err
		}
		// Do not let an older backlog resurrect explicitly corrected/forgotten
		// information or count the same relationship change a second time.
		for _, id := range change.EvidenceIDs {
			next.ManualThrough = max(next.ManualThrough, id)
		}
		if err := writeUserMemory(tx, next); err != nil {
			return err
		}
		return recordMemoryAudit(tx, p.groupID, next, change)
	})
	if err != nil {
		return nil, err
	}
	next.Version++
	return visibleUserProfile(next, p.groupID, time.Now(), 30), nil
}
