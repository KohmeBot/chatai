package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

type catalogSearcher struct {
	scriptedSkillSearcher
	items []SkillSummary
	err   error
	query string
	limit int
}

func (s *catalogSearcher) ListSkillSummaries() ([]SkillSummary, error) { return s.items, s.err }
func (s *catalogSearcher) SearchActiveSkills(_ int64, query string, limit int) ([]SkillMatch, error) {
	s.query, s.limit = query, limit
	return s.matches, nil
}

func TestRunnerCatalogAllowsMultipleSkillsBeforeToolSearch(t *testing.T) {
	registry := NewRegistry()
	var reads []string
	require.NoError(t, registry.Register(Tool{
		Definition: Function("read_skill", "read", map[string]any{}),
		Handler: func(_ *RunContext, raw json.RawMessage) (any, error) {
			var in struct {
				Name string `json:"name"`
			}
			require.NoError(t, json.Unmarshal(raw, &in))
			reads = append(reads, in.Name)
			return "BODY_" + in.Name, nil
		},
	}))
	searcher := &catalogSearcher{items: []SkillSummary{
		{Name: "extract", Description: "提取表格", Path: "extract/SKILL.md"},
		{Name: "report", Description: "生成报告", Path: "report/SKILL.md"},
	}}
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("a", "read_skill", `{"name":"extract"}`), call("b", "read_skill", `{"name":"report"}`)}},
		{Answer: "done"},
	}}
	rc := &RunContext{GroupID: 1}
	_, err := (&Runner{Tools: registry, Model: llm, Skills: searcher}).Run(rc, "提取并生成报告", "", "")
	require.NoError(t, err)
	require.Equal(t, []string{"extract", "report"}, reads)
	require.Contains(t, llm.requests[0].Question, "extract/SKILL.md")
	require.Contains(t, llm.requests[0].Question, "report/SKILL.md")
	require.NotContains(t, llm.requests[0].Question, "BODY_")
	require.Empty(t, rc.Trace().UsedSkillIDs)
}

func TestCatalogBudgetAndFailureKeepDiscoveryAvailable(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"read_skill", "search_skills", "list_skills"} {
		require.NoError(t, registry.Register(Tool{Definition: Function(name, name, map[string]any{}), Handler: func(_ *RunContext, _ json.RawMessage) (any, error) { return nil, nil }}))
	}
	searcher := &catalogSearcher{items: []SkillSummary{{Name: "large", Description: strings.Repeat("长", 9000)}}}
	runner := &Runner{Tools: registry, Skills: searcher}
	active := map[string]bool{}
	prompt := runner.skillCatalogPrompt(active)
	require.LessOrEqual(t, utf8.RuneCountInString(prompt), skillCatalogBudget)
	require.Contains(t, prompt, "已截断")
	require.True(t, active["list_skills"])
	searcher.err = errors.New("unavailable")
	active = map[string]bool{}
	require.Contains(t, runner.skillCatalogPrompt(active), "加载失败")
	require.True(t, active["search_skills"])
}

func TestSearchUsesMultipleSkillsAndFocusedQuery(t *testing.T) {
	searcher := &catalogSearcher{scriptedSkillSearcher: scriptedSkillSearcher{matches: []SkillMatch{
		{ID: 1, Name: "one", Markdown: "first"}, {ID: 2, Name: "two", Markdown: "second"},
	}}}
	llm := &scriptedModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{call("search", "search_tools", `{"query":"report"}`)}}, {Answer: "done"},
	}}
	rc := &RunContext{GroupID: 1}
	_, err := (&Runner{Tools: NewRegistry(), Model: llm, Skills: searcher}).Run(rc, "report", "unrelated historical messages", "")
	require.NoError(t, err)
	require.Equal(t, "report", searcher.query)
	require.Equal(t, 5, searcher.limit)
	require.Equal(t, []uint{1, 2}, rc.Trace().UsedSkillIDs)
}
