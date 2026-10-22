package persona

import (
	"encoding/json"
	"errors"
	"github.com/kohmebot/chatai/chatai/agent"
	"gorm.io/gorm"
	"time"
)

func (p *Persona) groupMemoryTools() []agent.Tool {
	tools := []agent.Tool{{Definition: agent.Function("read_group_memory", "读取当前群的结构化长期记忆、简介及更新版本", map[string]any{}), Namespace: "memory", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow, SearchTerms: []string{"群记忆", "群印象", "群规", "群梗", "群氛围"}, Handler: p.handleReadGroupMemory}}
	if !p.opts.ImpressionUpdateEnable {
		return tools
	}
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":        stringProperty("稳定唯一键，同键替换"),
			"kind":       map[string]any{"type": "string", "enum": []string{"fact", "preference", "boundary", "state", "experience"}},
			"text":       stringProperty("内容，最多200字"),
			"source_id":  integerProperty("read_group_context返回的群成员消息source_id，不是message_id"),
			"confidence": map[string]any{"type": "string", "enum": []string{"explicit", "inferred"}},
			"expires_at": stringProperty("可选RFC3339过期时间；state默认14天"),
		},
		"required": []string{"key", "kind", "text", "source_id", "confidence"},
	}
	tools = append(tools, agent.Tool{
		Definition: agent.Function("update_group_memory", "按版本更新当前群记忆。先read_group_memory及read_group_context核实来源；保存群体事实、共同话题偏好、互动边界、近期状态和共同事件。个人意见不等于群体共识，玩笑不等于群规；forget时同时清理summary。", map[string]any{
			"previous_version": integerProperty("read_group_memory返回的version"),
			"evidence_ids":     map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 1, "maxItems": 20},
			"reason":           stringProperty("本次变化的依据，最多200字"),
			"summary":          stringProperty("可选，替换核心简介，最多600字，可为空以清除"),
			"upsert":           map[string]any{"type": "array", "items": item, "maxItems": 20},
			"forget":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 30},
		}, "previous_version", "evidence_ids", "reason"),
		Namespace: "memory", Risk: agent.ToolRiskMedium,
		SearchTerms: []string{"记住", "纠正记忆", "忘记", "删除记忆", "更新群记忆", "群印象"},
		Handler:     p.handleUpdateGroupMemory,
	})
	return tools
}
func (p *Persona) handleReadGroupMemory(_ *agent.RunContext, _ json.RawMessage) (any, error) {
	profile, err := readGroupMemory(p.db, p.groupID)
	return visibleGroupProfile(profile, time.Now(), 30), err
}
func (p *Persona) handleUpdateGroupMemory(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var change GroupMemoryChange
	if err := json.Unmarshal(raw, &change); err != nil {
		return nil, err
	}
	if len(change.EvidenceIDs) == 0 || len(change.EvidenceIDs) > 20 {
		return nil, errors.New("1 to 20 evidence_ids required")
	}
	selfID, err := runSelfUserID(rc)
	if err != nil {
		return nil, err
	}
	err = p.db.Transaction(func(tx *gorm.DB) error {
		profile, err := readGroupMemory(tx, p.groupID)
		if err != nil {
			return err
		}
		var rows []ChatMessageRecord
		if err = tx.Where("group_id = ? AND id IN ?", p.groupID, change.EvidenceIDs).Find(&rows).Error; err != nil {
			return err
		}
		next, err := applyGroupMemoryChange(profile, change, rows, selfID, time.Now())
		if err != nil {
			return err
		}
		for _, id := range change.EvidenceIDs {
			next.ManualThrough = max(next.ManualThrough, id)
		}
		if err = writeGroupMemory(tx, next); err != nil {
			return err
		}
		return recordGroupMemoryAudit(tx, next, change)
	})
	if err != nil {
		return nil, err
	}
	return p.handleReadGroupMemory(rc, nil)
}
