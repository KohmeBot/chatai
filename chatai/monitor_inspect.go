package chatai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/chatai/chatai/schedule"
	"github.com/kohmebot/chatai/chatai/skill"
	zero "github.com/wdvxdr1123/ZeroBot"
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
	case "schedules":
		if c.scheduler == nil {
			return nil, fmt.Errorf("定时任务未启用")
		}
		if r.Method == http.MethodGet {
			offset := 0
			if raw := r.URL.Query().Get("offset"); raw != "" {
				offset, err = strconv.Atoi(raw)
			}
			if err != nil || offset < 0 || offset > 1000000 {
				return nil, fmt.Errorf("无效分页参数")
			}
			items, err := c.scheduler.List(id, 0, offset)
			bots := []string{}
			zero.RangeBot(func(id int64, _ *zero.Ctx) bool { bots = append(bots, strconv.FormatInt(id, 10)); return true })
			sort.Strings(bots)
			return map[string]any{"items": items, "bots": bots, "has_more": len(items) == 50}, err
		}
		var input struct {
			schedule.Input
			TaskID uint   `json:"task_id"`
			UserID string `json:"user_id"`
			BotID  string `json:"bot_id"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return nil, fmt.Errorf("无效任务数据: %w", err)
		}
		if decoder.Decode(new(any)) != io.EOF {
			return nil, fmt.Errorf("请求只能包含一个 JSON 对象")
		}
		if r.Method == http.MethodDelete {
			if input.TaskID == 0 {
				return nil, fmt.Errorf("无效任务 ID")
			}
			if err := c.scheduler.Delete(id, 0, input.TaskID); err != nil {
				return nil, err
			}
			return map[string]bool{"deleted": true}, nil
		}
		if r.Method != http.MethodPost {
			return nil, fmt.Errorf("不支持的请求方法")
		}
		user, err := strconv.ParseInt(input.UserID, 10, 64)
		if err != nil || user <= 0 {
			return nil, fmt.Errorf("无效用户 ID")
		}
		bot, err := strconv.ParseInt(input.BotID, 10, 64)
		if err != nil || bot <= 0 {
			return nil, fmt.Errorf("无效机器人 ID")
		}
		return c.scheduler.Create(id, user, bot, input.Input)
	case "tools":
		if r.Method == http.MethodPatch {
			var input struct {
				Name     string `json:"name"`
				Disabled *bool  `json:"disabled"`
			}
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				return nil, fmt.Errorf("无效工具设置: %w", err)
			}
			if decoder.Decode(new(any)) != io.EOF || input.Disabled == nil {
				return nil, fmt.Errorf("需要单个 JSON 对象和 disabled 布尔值")
			}
			if err := p.SetToolDisabled(input.Name, *input.Disabled); err != nil {
				return nil, err
			}
			return map[string]bool{"disabled": *input.Disabled}, nil
		}
		return p.ToolCatalog(), nil
	case "memory":
		if r.Method != http.MethodGet {
			var edit persona.MemoryEdit
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&edit); err != nil {
				return nil, fmt.Errorf("无效记忆数据：%w", err)
			}
			if decoder.Decode(new(any)) != io.EOF {
				return nil, fmt.Errorf("请求只能包含一个 JSON 对象")
			}
			var userID int64
			if edit.Target != "group" && edit.Target != "group_profile" && edit.Target != "group_item" && edit.Target != "group_item_create" {
				userID, err = strconv.ParseInt(edit.UserID, 10, 64)
				if err != nil || userID <= 0 {
					return nil, fmt.Errorf("无效用户 ID")
				}
			}
			if err := p.EditMemory(userID, edit, r.Method == http.MethodDelete); err != nil {
				return nil, err
			}
			return map[string]bool{"ok": true}, nil
		}
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
		name := r.URL.Query().Get("name")
		metadata, err := c.skills.Directory.List()
		if err != nil {
			return nil, err
		}
		for _, m := range metadata {
			if m.Name == name {
				content, err := c.skills.Directory.Read(name, "SKILL.md")
				return map[string]any{"content": content, "triggers": m.Triggers, "non_triggers": m.NonTriggers, "required_tools": m.Tools}, err
			}
		}
		return nil, fmt.Errorf("未找到可用 Skill")
	case "skill-files", "skill-file":
		if c.skills == nil || c.skills.Directory == nil {
			return nil, fmt.Errorf("Skills 未启用")
		}
		name := r.URL.Query().Get("name")
		if section == "skill-files" {
			return c.skills.Directory.Files(r.Context(), name)
		}
		return c.skills.Directory.MonitorPreview(name, r.URL.Query().Get("file"))
	case "skills":
		if r.Method != http.MethodGet {
			if r.Method != http.MethodPatch {
				return nil, fmt.Errorf("不支持的请求方法")
			}
			var input struct {
				Name     string `json:"name"`
				ID       uint   `json:"id"`
				Resident *bool  `json:"resident"`
				Disabled *bool  `json:"disabled"`
			}
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				return nil, fmt.Errorf("无效技能设置: %w", err)
			}
			if decoder.Decode(new(any)) != io.EOF || (input.Resident == nil) == (input.Disabled == nil) {
				return nil, fmt.Errorf("需要单个 JSON 对象，且只指定 resident 或 disabled 布尔值")
			}
			if input.Disabled != nil {
				if err := c.skills.SetDisabled(id, input.Name, input.ID, *input.Disabled); err != nil {
					return nil, err
				}
				return map[string]bool{"disabled": *input.Disabled}, nil
			}
			if err := c.skills.SetResident(id, input.Name, input.ID, *input.Resident); err != nil {
				return nil, err
			}
			return map[string]bool{"resident": *input.Resident}, nil
		}
		items := []map[string]any{}
		if c.skills == nil {
			return map[string]any{"enabled": false, "items": items}, nil
		}
		var summaries []skill.Metadata
		var err error
		if c.skills.Directory != nil {
			summaries, err = c.skills.Directory.List()
		}
		if err != nil {
			return nil, err
		}
		disabled, err := c.skills.DisabledKeys()
		if err != nil {
			return nil, err
		}
		resident, err := c.skills.ResidentKeys()
		if err != nil {
			return nil, err
		}
		for _, s := range summaries {
			item := map[string]any{"disabled": disabled[skill.ResidentKey(s.Name, 0)], "resident": resident[skill.ResidentKey(s.Name, 0)], "name": s.Name, "description": s.Description, "path": s.Path, "source": "目录 Skill", "triggers": s.Triggers, "non_triggers": s.NonTriggers}
			files, err := c.skills.Directory.Files(r.Context(), s.Name)
			if err == nil {
				item["has_scripts"], item["has_resources"] = monitorSkillFiles(files)
			}
			// Unreadable projects remain visible, with unknown file categories.
			items = append(items, item)
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
			items = append(items, map[string]any{"disabled": disabled[skill.ResidentKey("", s.ID)], "id": s.ID, "resident": resident[skill.ResidentKey("", s.ID)], "name": s.Name, "description": s.Description, "source": s.Source, "content": s.MarkdownText(), "triggers": s.Triggers(), "non_triggers": s.NonTriggers(), "required_tools": s.RequiredTools(), "scope": s.Scope, "version": s.Version, "updated_at": s.UpdatedAt})
			items[len(items)-1]["has_scripts"] = false
			items[len(items)-1]["has_resources"] = false
		}
		return map[string]any{"enabled": true, "items": items}, nil
	}
	return nil, fmt.Errorf("未知数据分类")
}

// Classify attached files without reading their contents. Scripts include files
// in scripts/ and common executable source files elsewhere in the project.
func monitorSkillFiles(files []skill.FileInfo) (scripts, resources bool) {
	for _, file := range files {
		name := strings.ToLower(file.Path)
		if name == "skill.md" {
			continue
		}
		isScript := strings.HasPrefix(name, "scripts/")
		switch path.Ext(name) {
		case ".py", ".js", ".mjs", ".cjs", ".ts", ".sh", ".bash", ".ps1", ".bat", ".cmd", ".rb", ".pl":
			isScript = true
		}
		if isScript {
			scripts = true
		} else {
			resources = true
		}
	}
	return
}
