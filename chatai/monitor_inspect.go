package chatai

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/kohmebot/chatai/chatai/skill"
)

func (c *ChatPlugin) inspectMonitor(r *http.Request) (any, error) {
	section := r.PathValue("section")
	if section == "groups" {
		groups := []string{}
		for id := range c.personaMap {
			groups = append(groups, strconv.FormatInt(id, 10))
		}
		sort.Strings(groups)
		return groups, nil
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("group"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("请选择有效群聊")
	}
	p := c.personaMap[id]
	if p == nil {
		return nil, fmt.Errorf("该群未启用 Agent")
	}
	switch section {
	case "tools":
		return p.ToolCatalog(), nil
	case "memory":
		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			offset, err = strconv.Atoi(raw)
			if err != nil || offset < 0 || offset > 1000000 {
				return nil, fmt.Errorf("无效分页参数")
			}
		}
		return p.MemorySnapshot(offset)
	case "skill":
		if c.skills == nil || c.skills.Directory == nil {
			return nil, fmt.Errorf("Skills 未启用")
		}
		content, err := c.skills.Directory.Read(r.URL.Query().Get("name"), "SKILL.md")
		return map[string]any{"content": content}, err
	case "skills":
		items := []map[string]any{}
		if c.skills == nil {
			return map[string]any{"enabled": false, "items": items}, nil
		}
		summaries, err := c.skills.ListSkillSummaries()
		if err != nil {
			return nil, err
		}
		for _, s := range summaries {
			items = append(items, map[string]any{"name": s.Name, "description": s.Description, "path": s.Path, "source": "目录 Skill"})
		}
		var records []skill.Record
		err = c.db.Where("status = ? AND (scope = ? OR (scope = ? AND group_id = ?))", skill.StatusActive, skill.ScopeGlobal, skill.ScopeGroup, id).Order("name").Find(&records).Error
		if err != nil {
			return nil, err
		}
		for _, s := range records {
			if s.Source == skill.SourceGenerated && !s.ExpiresAt.After(time.Now()) {
				continue
			}
			items = append(items, map[string]any{"name": s.Name, "description": s.Description, "source": s.Source, "content": s.MarkdownText(), "triggers": s.Triggers(), "required_tools": s.RequiredTools()})
		}
		return map[string]any{"enabled": true, "items": items}, nil
	}
	return nil, fmt.Errorf("未知数据分类")
}
