package persona

import (
	"sort"
	"strconv"
	"time"
)

func (p *Persona) ToolCatalog() []map[string]any { return p.tools.Catalog() }

// ShortMemorySnapshot follows the same expiry and size rules as eventPrompt.
func (p *Persona) ShortMemorySnapshot() []map[string]any {
	p.conversationMu.Lock()
	defer p.conversationMu.Unlock()
	out := []map[string]any{}
	if !p.opts.ConversationMemory {
		return out
	}
	now := time.Now()
	for id, state := range p.conversations {
		if now.Sub(state.Updated) >= p.opts.ConversationWindow {
			continue
		}
		out = append(out, map[string]any{"user_id": strconv.FormatInt(id, 10), "note": boundedConversation(state.Note, p.opts.ConversationMaxChars), "updated_at": state.Updated, "expires_at": state.Updated.Add(p.opts.ConversationWindow)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["user_id"].(string) < out[j]["user_id"].(string) })
	return out
}

func (p *Persona) MemorySnapshot(offset int) (any, error) {
	var rows []UserMemory
	if err := p.db.Order("updated_at DESC").Order("user_id ASC").Offset(offset).Limit(51).Find(&rows).Error; err != nil {
		return nil, err
	}
	more := len(rows) > 50
	if more {
		rows = rows[:50]
	}
	users := []map[string]any{}
	for _, row := range rows {
		profile, err := readUserMemory(p.db, row.UserID)
		if err != nil {
			return nil, err
		}
		profile = visibleUserProfile(profile, p.groupID, time.Now(), int(^uint(0)>>1))
		users = append(users, map[string]any{"user_id": strconv.FormatInt(row.UserID, 10), "summary": profile.Summary, "items": profile.Items, "relationship": profile.Relationship, "version": profile.Version, "updated_at": row.UpdatedAt})
	}
	group, err := readGroupMemory(p.db, p.groupID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"short_enabled": p.opts.ConversationMemory, "short": p.ShortMemorySnapshot(), "users": users, "group_memory": visibleGroupProfile(group, time.Now(), 30), "group_impression": group.Summary, "has_more": more, "offset": offset}, nil
}
