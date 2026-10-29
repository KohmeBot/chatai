package agent

import (
	"encoding/json"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

type residentSearcher struct {
	scriptedSkillSearcher
	resident []SkillMatch
}

func (s *residentSearcher) ResidentSkills(int64) ([]SkillMatch, error) { return s.resident, nil }

func TestResidentSkillAvailableInFirstRequestAndIsRunLocal(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Tool{Definition: Function("resident_tool", "tool", map[string]any{}), Handler: func(_ *RunContext, _ json.RawMessage) (any, error) {
		t.Fatal("preloading must not execute tools")
		return nil, nil
	}}))
	searcher := &residentSearcher{resident: []SkillMatch{
		{Name: "demo", Path: "demo/SKILL.md", Markdown: "---\ntools: [resident_tool]\n---\nRESIDENT_BODY"},
		{ID: 42, Name: "db", Markdown: "DATABASE_BODY"},
		{ID: 43, Name: "unavailable", Markdown: "UNAVAILABLE_BODY", RequiredTools: []string{"missing_tool"}},
	}}
	llm := &scriptedModel{steps: []model.Response{{Answer: "done"}, {Answer: "done"}}}
	runner := &Runner{Tools: registry, Model: llm, Skills: searcher}
	rc := &RunContext{GroupID: 10}
	_, err := runner.Run(rc, "prompt", "", "")
	require.NoError(t, err)
	require.Len(t, llm.requests, 1)
	require.Contains(t, llm.requests[0].Question, "RESIDENT_BODY")
	require.Contains(t, llm.requests[0].Question, "DATABASE_BODY")
	require.NotContains(t, llm.requests[0].Question, "UNAVAILABLE_BODY")
	encoded, err := json.Marshal(llm.requests[0].Tools)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "resident_tool")
	require.Equal(t, []uint{42}, rc.Trace().UsedSkillIDs)
	searcher.resident = nil
	_, err = runner.Run(&RunContext{GroupID: 10}, "prompt", "", "")
	require.NoError(t, err)
	require.NotContains(t, llm.requests[1].Question, "RESIDENT_BODY")
	encoded, err = json.Marshal(llm.requests[1].Tools)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "resident_tool")
}
