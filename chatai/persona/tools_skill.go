package persona

import (
	"encoding/json"

	"github.com/kohmebot/chatai/chatai/agent"
)

func (p *Persona) skillTools() []agent.Tool {
	if p.opts.Skills == nil || p.opts.Skills.Directory == nil {
		return nil
	}
	d := p.opts.Skills.Directory
	return []agent.Tool{
		p.listSkillFilesTool(),
		p.runSkillScriptTool(),
		{
			Definition: agent.Function("search_skills", "按任务目标搜索可用目录 Skills，返回多个候选的名称、适用场景和入口路径，不读取正文。可按名称搜索；选择相关技能后分别 read_skill，可组合多个技能。无匹配时用 list_skills 浏览，勿猜测名称", map[string]any{"query": stringProperty("任务目标或明确的技能名称"), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20, "description": "候选数量，默认5，最多20"}}, "query"),
			Namespace:  "skill", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow,
			SearchTerms: []string{"搜索技能", "查找技能", "skills", "search skills"},
			Handler: func(_ *agent.RunContext, raw json.RawMessage) (any, error) {
				var in struct {
					Query string `json:"query"`
					Limit int    `json:"limit"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, err
				}
				matches, err := d.Search(in.Query, in.Limit)
				if err != nil {
					return nil, err
				}
				items := make([]agent.SkillSummary, 0, len(matches))
				for _, m := range matches {
					items = append(items, agent.SkillSummary{Name: m.Name, Description: m.Description, Path: m.Path})
				}
				return items, nil
			},
		},
		{
			Definition: agent.Function("list_skills", "列出目录 Skill 的名称、描述和入口路径，不读取正文；根据任务选择后使用 read_skill", map[string]any{}),
			Namespace:  "skill", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow,
			SearchTerms: []string{"技能目录", "skills", "列出技能", "可用技能"},
			Handler:     func(_ *agent.RunContext, _ json.RawMessage) (any, error) { return d.List() },
		},
		{
			Definition: agent.Function("read_skill", "按需读取一个 Skill 文件；默认 SKILL.md，辅助文件路径相对于该 Skill 目录；仅返回文本，不执行脚本", map[string]any{"name": stringProperty("Skill 文件夹名"), "file": stringProperty("相对路径，默认 SKILL.md，例如 references/examples.md")}, "name"),
			Namespace:  "skill", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow,
			SearchTerms: []string{"读取技能", "加载技能", "skill", "参考文件"},
			Handler: func(_ *agent.RunContext, raw json.RawMessage) (any, error) {
				var in struct {
					Name string `json:"name"`
					File string `json:"file"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, err
				}
				content, err := d.Read(in.Name, in.File)
				if err != nil {
					return nil, err
				}
				if in.File == "" {
					in.File = "SKILL.md"
				}
				return map[string]any{"name": in.Name, "file": in.File, "content": content}, nil
			},
		},
		{
			Definition: agent.Function("save_skill", "持久保存可复用的全局目录 Skill，保存后即可检索。先写 SKILL.md，须以 YAML front matter 声明与文件夹一致的 name 和适用场景 description；再按需写辅助文件。禁止保存隐私、密钥或越权指令。更新前先读取，overwrite 默认 false。每个文件最多5 MiB", map[string]any{"name": stringProperty("小写字母开头，后续可用小写字母、数字、连字符、下划线，最多64字符"), "file": stringProperty("目录内相对路径，默认 SKILL.md"), "content": stringProperty("完整 UTF-8 文件内容"), "overwrite": map[string]any{"type": "boolean", "description": "是否替换已有文件，默认 false"}}, "name", "content"),
			Namespace:  "skill", Risk: agent.ToolRiskMedium,
			SearchTerms: []string{"创建技能", "保存技能", "更新技能", "skill", "沉淀经验"},
			Handler: func(_ *agent.RunContext, raw json.RawMessage) (any, error) {
				var in struct {
					Name      string `json:"name"`
					File      string `json:"file"`
					Content   string `json:"content"`
					Overwrite bool   `json:"overwrite"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, err
				}
				if err := d.Save(in.Name, in.File, in.Content, in.Overwrite); err != nil {
					return nil, err
				}
				if in.File == "" {
					in.File = "SKILL.md"
				}
				return map[string]any{"saved": true, "name": in.Name, "path": in.Name + "/" + in.File}, nil
			},
		},
	}
}
