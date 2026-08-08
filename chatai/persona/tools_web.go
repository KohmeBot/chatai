package persona

import (
	"encoding/json"

	"github.com/kohmebot/chatai/chatai/agent"
)

func (p *Persona) webTools() []agent.Tool {
	return []agent.Tool{
		{
			Definition: agent.Function("search_web", "联网搜索公开网页，返回标题、链接和摘要；遇到不懂或不确定的信息时使用", map[string]any{
				"query": stringProperty("搜索关键词"),
				"limit": integerProperty("结果数量，默认5，最大8"),
			}, "query"),
			SearchTerms: []string{"联网搜索", "搜索", "搜索网页", "查资料", "最新信息", "互联网", "不懂", "不知道", "陌生概念", "事实核实"},
			Handler:     p.handleSearchWeb,
		},
		{
			Definition:  agent.Function("browse_web", "读取公开网页正文；想进一步浏览结果或搜索结果摘要不足时使用", map[string]any{"url": stringProperty("http 或 https 网页地址")}, "url"),
			SearchTerms: []string{"联网搜索", "搜索", "搜索网页", "查资料", "最新信息", "互联网", "浏览网页", "读取网页", "打开链接", "网页正文", "原文", "URL"},
			Handler:     p.handleBrowseWeb,
		},
	}
}

func (p *Persona) handleSearchWeb(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	return p.searchWeb(rc, input.Query, input.Limit)
}

func (p *Persona) handleBrowseWeb(rc *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	return p.readWeb(rc, input.URL)
}
