package persona

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/kohmebot/chatai/chatai/agent"
)

func (p *Persona) searchSkillFilesTool() agent.Tool {
	return agent.Tool{
		Definition: agent.Function("search_skill_files", "按路径关键词搜索 skills 内的文件或文件夹，不检索技能路由元数据、不读取正文。path 相对于整个 skills 目录，absolute_path 是本机绝对路径；结果按路径排序，truncated=true 时缩小 directory 或 query", map[string]any{
			"query":     stringProperty("路径包含的关键词，不区分大小写；空字符串列出全部"),
			"directory": stringProperty("搜索范围，相对于整个 skills 目录，默认 .；例如 meme/assets"),
			"type":      map[string]any{"type": "string", "enum": []string{"file", "directory"}, "description": "可选，仅文件或仅文件夹；省略则返回两者"},
			"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "description": "默认50，最多200"},
		}, "query"),
		Namespace: "skill", ReadOnly: true, Idempotent: true, Risk: agent.ToolRiskLow,
		SearchTerms: []string{"搜索文件", "搜索文件夹", "表情包", "图片路径", "查找资源", "skill files"},
		Handler: func(rc *agent.RunContext, raw json.RawMessage) (any, error) {
			var in struct {
				Query     string `json:"query"`
				Directory string `json:"directory"`
				Type      string `json:"type"`
				Limit     int    `json:"limit"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, err
			}
			return p.opts.Skills.Directory.SearchFiles(skillContext(rc), in.Query, in.Directory, in.Type, in.Limit)
		},
	}
}

func (p *Persona) resolveImageSource(source string) (string, error) {
	source = strings.TrimSpace(source)
	// Preserve URLs and protocol sources such as base64:// and file://.
	if filepath.IsAbs(source) {
		return source, nil
	}
	if parsed, err := url.Parse(source); err == nil && parsed.Scheme != "" {
		return source, nil
	}
	if p.opts.Skills != nil && p.opts.Skills.Directory != nil {
		return p.opts.Skills.Directory.ResolveFile(source)
	}
	return source, nil
}
