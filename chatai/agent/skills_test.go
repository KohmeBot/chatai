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

func TestSkillToolNames(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"run_skill_script", "send_image", "other_tool"} {
		require.NoError(t, registry.Register(Tool{
			Definition: Function(name, "图片 脚本", map[string]any{}),
			Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
				t.Fatal("discovering tools must not execute them")
				return nil, nil
			},
		}))
	}
	for _, tc := range []struct {
		name, content string
		want          []string
	}{
		{"declared", "---\nname: demo\ntools: [send_image, missing_tool, run_skill_script, send_image]\n---\nother_tool", []string{"run_skill_script", "send_image"}},
		{"empty", "---\ntools: []\n---\nsend_image", nil},
		{"fallback", "---\nname: demo\ndescription: other_tool\n---\n调用 `run_skill_script`，再 send_image(url)。", []string{"run_skill_script", "send_image"}},
		{"null", "---\ntools: null\n---\nsend_image", []string{"send_image"}},
		{"reference", "参考文档：`send_image`，重复 send_image。", []string{"send_image"}},
		{"whole names only", "send_image_extra prefix_send_image x-send_image send_images SEND_IMAGE 图片 脚本", nil},
		{"crlf", "---\r\ntools:\r\n  - send_image\r\n---\r\n", []string{"send_image"}},
		{"invalid declaration", "---\ntools: send_image\n---\nrun_skill_script", nil},
		{"unclosed header", "---\ntools: [send_image]\nrun_skill_script", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ElementsMatch(t, tc.want, registry.skillToolNames(tc.content))
		})
	}
}

func TestReadSkillActivatesToolsForNextDecision(t *testing.T) {
	for _, declared := range []bool{false, true} {
		for _, readFails := range []bool{false, true} {
			registry := NewRegistry()
			content := "---\nname: demo\ndescription: demo\n"
			if declared {
				content += "tools: [run_skill_script, send_image]\n"
			}
			content += "---\n调用 run_skill_script，然后 send_image。"
			require.NoError(t, registry.Register(Tool{
				Definition: Function("read_skill", "read", map[string]any{}),
				Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
					result := map[string]any{"name": "demo", "file": "SKILL.md", "content": content}
					if readFails {
						return result, errors.New("read failed")
					}
					return result, nil
				},
			}))
			executed := 0
			for _, name := range []string{"run_skill_script", "send_image"} {
				require.NoError(t, registry.Register(Tool{
					Definition: Function(name, name, map[string]any{}),
					Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
						executed++
						return "ok", nil
					},
				}))
			}
			llm := &scriptedModel{steps: []model.Response{
				{ToolCalls: []model.ToolCall{call("read", "read_skill", `{"name":"demo"}`)}},
				{ToolCalls: []model.ToolCall{call("run", "run_skill_script", `{"name":"demo","file":"scripts/main.py"}`)}},
				{Answer: "done"},
			}}
			runner := &Runner{Tools: registry, Model: llm, Skills: &catalogSearcher{items: []SkillSummary{{Name: "demo", Description: "demo"}}}}
			_, err := runner.Run(&RunContext{}, "demo", "", "")
			require.NoError(t, err)
			require.NotContains(t, toolNames(llm.requests[0].Tools), "run_skill_script")
			if readFails {
				require.NotContains(t, toolNames(llm.requests[1].Tools), "run_skill_script")
				require.NotContains(t, toolNames(llm.requests[1].Tools), "send_image")
				require.Zero(t, executed)
			} else {
				require.Contains(t, toolNames(llm.requests[1].Tools), "run_skill_script")
				require.Contains(t, toolNames(llm.requests[1].Tools), "send_image")
				require.Equal(t, 1, executed) // Loading never runs the sending tool.
				var payload struct {
					Result struct {
						LoadedTools []string `json:"loaded_tools"`
					} `json:"result"`
				}
				history := llm.requests[1].History
				require.NoError(t, json.Unmarshal([]byte(history[len(history)-1].Content.(string)), &payload))
				require.Equal(t, []string{"run_skill_script", "send_image"}, payload.Result.LoadedTools)
			}
			// Reusing the Runner and registry does not leak activation into a new run.
			fresh := &scriptedModel{steps: []model.Response{{Answer: "done"}}}
			runner.Model = fresh
			_, err = runner.Run(&RunContext{}, "another task", "", "")
			require.NoError(t, err)
			require.NotContains(t, toolNames(fresh.requests[0].Tools), "run_skill_script")
			require.NotContains(t, toolNames(fresh.requests[0].Tools), "send_image")
		}
	}
}
