package chatai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
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
