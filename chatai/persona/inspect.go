package persona

import (
	"sort"
	"strconv"
	"time"

	"gorm.io/gorm"
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
		row := map[string]any{"user_id": strconv.FormatInt(id, 10), "note": p.boundedConversation(state.Note), "updated_at": state.Updated, "expires_at": state.Updated.Add(p.opts.ConversationWindow)}
		if p.private != nil {
			row["bot_id"] = strconv.FormatInt(p.private.BotID, 10)
		}
		out = append(out, row)
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
		users = append(users, memorySnapshotUser(profile, row.UpdatedAt))
	}
	group, err := readGroupMemory(p.db, p.groupID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"short_enabled": p.opts.ConversationMemory, "short": p.ShortMemorySnapshot(), "users": users, "group_memory": visibleGroupProfile(group, time.Now(), 30), "group_impression": group.Summary, "has_more": more, "offset": offset}, nil
}

func PrivateMemorySnapshot(db *gorm.DB, offset int, shortEnabled bool, livePersonas []*Persona) (any, error) {
	var rows []PrivateUserMemory
	if err := db.Order("updated_at DESC").Order("bot_id ASC").Order("user_id ASC").Offset(offset).Limit(51).Find(&rows).Error; err != nil {
		return nil, err
	}
	more := len(rows) > 50
	if more {
		rows = rows[:50]
	}
	users := make([]map[string]any, 0, len(rows))
	now := time.Now()
	for _, row := range rows {
		profile, err := decodeUserMemory(row.userMemory(), row.UserID, nil)
		if err != nil {
			return nil, err
		}
		profile = visiblePrivateUserProfile(profile, now, 30)
		user := memorySnapshotUser(profile, row.UpdatedAt)
		user["bot_id"] = strconv.FormatInt(row.BotID, 10)
		users = append(users, user)
	}
	short := []map[string]any{}
	if shortEnabled {
		for _, p := range livePersonas {
			if p.private != nil {
				short = append(short, p.ShortMemorySnapshot()...)
			}
		}
		sort.Slice(short, func(i, j int) bool {
			if short[i]["bot_id"] != short[j]["bot_id"] {
				return short[i]["bot_id"].(string) < short[j]["bot_id"].(string)
			}
			return short[i]["user_id"].(string) < short[j]["user_id"].(string)
		})
	}
	return map[string]any{"short_enabled": shortEnabled, "short": short, "users": users, "has_more": more, "offset": offset}, nil
}

func memorySnapshotUser(profile UserProfile, updatedAt time.Time) map[string]any {
	return map[string]any{"user_id": strconv.FormatInt(profile.UserID, 10), "summary": profile.Summary, "items": profile.Items, "relationship": profile.Relationship, "version": profile.Version, "updated_at": updatedAt}
}
