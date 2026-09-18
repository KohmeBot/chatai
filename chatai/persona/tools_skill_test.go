package persona

import (
	"encoding/json"
	"testing"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/skill"
	"github.com/stretchr/testify/require"
)

func TestSkillToolsSaveReadAndDiscover(t *testing.T) {
	d, err := skill.NewDirectory(t.TempDir())
	require.NoError(t, err)
	s := skill.NewService(nil, nil, skill.Options{Enabled: true})
	s.Directory = d
	p := &Persona{opts: Options{Skills: s}, tools: agent.NewRegistry()}
	p.registerBuiltinTools()
	require.Contains(t, searchNames(p.tools.Search("保存技能", 5)), "save_skill")
	registered := map[string]agent.Tool{}
	for _, tool := range p.skillTools() {
		registered[tool.Definition.Function.Name] = tool
	}
	content := "---\nname: demo\ndescription: 演示技能\n---\n按需读取 references/demo.md"
	raw, err := json.Marshal(map[string]any{"name": "demo", "content": content})
	require.NoError(t, err)
	_, err = registered["save_skill"].Handler(nil, raw)
	require.NoError(t, err)
	_, err = registered["save_skill"].Handler(nil, raw)
	require.Error(t, err)
	result, err := registered["read_skill"].Handler(nil, json.RawMessage(`{"name":"demo"}`))
	require.NoError(t, err)
	require.Equal(t, content, result.(map[string]any)["content"])
	result, err = registered["list_skills"].Handler(nil, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Len(t, result.([]skill.Metadata), 1)
	disabled := &Persona{tools: agent.NewRegistry()}
	disabled.registerBuiltinTools()
	require.NotContains(t, disabled.ToolNames(), "save_skill")
}
