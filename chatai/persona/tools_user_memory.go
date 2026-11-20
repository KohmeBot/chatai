package persona

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"gorm.io/gorm"
)

type privateUserMemoryResult struct {
	UserMemory        UserProfile `json:"user_memory"`
	PrivateUserMemory UserProfile `json:"private_user_memory"`
}

func (p *Persona) privateProfileTools() []agent.Tool {
	tools := []agent.Tool{{
		Definition: agent.Function("read_user_memory", "读取当前用户的群聊画像 user_memory 和私聊画像 private_user_memory；更新时使用 private_user_memory.version。", map[string]any{}),
		Namespace:  "memory", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow,
		SearchTerms: []string{"用户画像", "用户记忆", "偏好", "长期记忆"},
		Handler: func(_ *agent.RunContext, _ json.RawMessage) (any, error) {
			groupProfile, err := readUserMemory(p.db, p.private.UserID)
			if err != nil {
				return nil, err
			}
			privateProfile, err := readPrivateUserMemory(p.db, *p.private)
			if err != nil {
				return nil, err
			}
			now := time.Now()
			return privateUserMemoryResult{
				UserMemory:        visibleUserProfile(groupProfile, 0, now, 30),
				PrivateUserMemory: visiblePrivateUserProfile(privateProfile, now, 30),
			}, nil
		},
	}}
	if p.opts.ImpressionUpdateEnable {
		tools = append(tools, p.userMemoryUpdateTool())
	}
	return tools
}

func (p *Persona) structuredMemoryTools() []agent.Tool {
	tools := []agent.Tool{{
		Definition: agent.Function("read_user_memory", "读取结构化用户记忆：稳定认识、偏好、边界、近期状态、共同经历及关系好感度；返回version供更新使用", map[string]any{"user_id": integerProperty("用户QQ号")}, "user_id"),
		Namespace:  "memory", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow,
		SearchTerms: []string{"用户记忆", "群友印象", "长期记忆", "关系", "好感度", "偏好"},
		Handler:     p.handleReadUserMemory,
	}}
	if p.opts.ImpressionUpdateEnable {
		tools = append(tools, p.userMemoryUpdateTool())
	}
	return tools
}

func (p *Persona) userMemoryUpdateTool() agent.Tool {
	description := "按版本更新结构化用户记忆。明确记住、更正、遗忘时使用。先read_user_memory及read_user_context核实本人消息；forget按key删除，同时清理summary对应内容；共同经历必须真实，不能把群友之间的互动说成与你的经历；关系变化须有实际依据，不因消息数量或用户要求刷好感度"
	version := "read_user_memory返回的version"
	source := "read_user_context返回的本人发言source_id，不是message_id"
	required := []string{"user_id", "previous_version", "evidence_ids", "reason"}
	if p.private != nil {
		description = "按版本更新当前用户的私聊画像。明确记住、更正、遗忘时使用。先读read_user_memory，使用私聊版本和当前用户的私聊发言作依据。forget按私聊画像的key删除，同时清理私聊summary中的对应内容。共同经历及关系变化须有实际依据。"
		version = "read_user_memory返回的private_user_memory.version"
		source = "当前用户私聊发言的source_id，可通过read_private_context查询，不是message_id"
		required = required[1:]
	}
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":        stringProperty("稳定唯一键，同键替换"),
			"kind":       map[string]any{"type": "string", "enum": []string{"fact", "preference", "boundary", "state", "experience"}},
			"text":       stringProperty("内容，最多200字"),
			"source_id":  integerProperty(source),
			"confidence": map[string]any{"type": "string", "enum": []string{"explicit", "inferred"}},
			"expires_at": stringProperty("可选RFC3339过期时间；state默认14天"),
		},
		"required": []string{"key", "kind", "text", "source_id", "confidence"},
	}
	return agent.Tool{
		Definition: agent.Function("update_user_memory", description, map[string]any{
			"user_id":          integerProperty("用户QQ号"),
			"previous_version": integerProperty(version),
			"evidence_ids":     map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 1, "maxItems": 20},
			"reason":           stringProperty("本次变化的依据，最多200字"),
			"summary":          stringProperty("可选，替换核心简介，最多600字，可为空以清除"),
			"upsert":           map[string]any{"type": "array", "items": item, "maxItems": 20},
			"forget":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 30},
			"relationship": map[string]any{"type": "object", "properties": map[string]any{
				"delta":       integerProperty("好感变化，-5到5，普通闲聊通常0"),
				"description": stringProperty("关系和互动方式，最多200字"),
			}, "required": []string{"delta", "description"}},
		}, required...),
		Namespace: "memory", Risk: agent.ToolRiskMedium,
		SearchTerms: []string{"记住", "纠正记忆", "忘记", "删除记忆", "更新用户记忆", "关系变化"},
		Handler:     p.handleUpdateUserMemory,
	}
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
	if p.private != nil {
		if change.UserID != 0 && change.UserID != p.private.UserID {
			return nil, errors.New("user_id must match the current private chat")
		}
		change.UserID = p.private.UserID
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
		profile, err := p.readWritableUserMemory(tx, change.UserID)
		if err != nil {
			return err
		}
		rows, err := p.userMemoryEvidence(tx, change.EvidenceIDs)
		if err != nil {
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
		if err := saveUserMemory(tx, next, p.private); err != nil {
			return err
		}
		if p.private != nil {
			return recordUserMemoryAudit(tx, 0, p.private.BotID, next, change)
		}
		return recordMemoryAudit(tx, p.groupID, next, change)
	})
	if err != nil {
		return nil, err
	}
	next.Version++
	return p.visibleWritableUserMemory(next, time.Now(), 30), nil
}

func (p *Persona) readWritableUserMemory(db *gorm.DB, userID int64) (UserProfile, error) {
	if p.private != nil {
		return readPrivateUserMemory(db, *p.private)
	}
	return readUserMemory(db, userID)
}

func (p *Persona) visibleWritableUserMemory(profile UserProfile, now time.Time, limit int) UserProfile {
	if p.private != nil {
		return visiblePrivateUserProfile(profile, now, limit)
	}
	return visibleUserProfile(profile, p.groupID, now, limit)
}

func (p *Persona) userMemoryEvidence(db *gorm.DB, ids []uint) ([]ChatMessageRecord, error) {
	var rows []ChatMessageRecord
	if p.private == nil {
		err := db.Where("group_id = ? AND id IN ?", p.groupID, ids).Find(&rows).Error
		return rows, err
	}
	var privateRows []PrivateChatMessageRecord
	if err := db.Where("bot_id = ? AND user_id = ? AND id IN ?", p.private.BotID, p.private.UserID, ids).Find(&privateRows).Error; err != nil {
		return nil, err
	}
	for _, privateRow := range privateRows {
		row := messageRecord(0, privateRow.Message)
		row.ID = privateRow.ID
		rows = append(rows, row)
	}
	return rows, nil
}
