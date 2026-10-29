package chatai

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/chatai/chatai/skill"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMonitorSkillDetailsAndFiles(t *testing.T) {
	directory, err := skill.NewDirectory(t.TempDir())
	require.NoError(t, err)
	body := "---\nname: demo\ndescription: Demo\ntriggers: [report]\nnon_triggers: [chat]\ntools: [search_web]\n---\n# Complete skill"
	require.NoError(t, directory.Save("demo", "SKILL.md", body, false))
	require.NoError(t, directory.Save("demo", "refs/help.md", "# Help", false))
	c := &ChatPlugin{skills: &skill.Service{Directory: directory}, personaMap: map[int64]*persona.Persona{10: {}}}
	request := func(section, query string) (any, error) {
		r := httptest.NewRequest("GET", "/api/inspect/"+section+"?group=10&name=demo"+query, nil)
		r.SetPathValue("section", section)
		return c.inspectMonitor(r)
	}
	value, err := request("skill", "")
	require.NoError(t, err)
	detail := value.(map[string]any)
	require.Equal(t, body, detail["content"])
	require.Equal(t, []string{"report"}, detail["triggers"])
	require.Equal(t, []string{"chat"}, detail["non_triggers"])
	require.Equal(t, []string{"search_web"}, detail["required_tools"])
	value, err = request("skill-files", "")
	require.NoError(t, err)
	require.Len(t, value.([]skill.FileInfo), 2)
	value, err = request("skill-file", "&file=refs/help.md")
	require.NoError(t, err)
	require.Equal(t, "# Help", value.(map[string]any)["content"])
	_, err = request("skill-file", "&file=../SKILL.md")
	require.Error(t, err)
	delete(c.personaMap, 10)
	_, err = request("skill-file", "&file=SKILL.md")
	require.Error(t, err)
}

func TestMonitorResidentSkillSettings(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "skills.db")), &gorm.Config{})
	require.NoError(t, err)
	sql, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sql.Close() })
	require.NoError(t, db.AutoMigrate(skill.Models()...))
	directory, err := skill.NewDirectory(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, directory.Save("demo", "SKILL.md", "---\nname: demo\ndescription: Demo\n---\nBody", false))
	s := skill.NewService(db, nil, skill.Options{Enabled: true})
	s.Directory = directory
	c := &ChatPlugin{db: db, skills: s, personaMap: map[int64]*persona.Persona{10: {}}}
	request := func(method, body string) (any, error) {
		r := httptest.NewRequest(method, "/api/inspect/skills?group=10", strings.NewReader(body))
		r.SetPathValue("section", "skills")
		return c.inspectMonitor(r)
	}
	for _, body := range []string{`{}`, `{"name":"demo"}`, `{"name":"demo","resident":"yes"}`, `{"name":"demo","resident":true} {}`, `{"name":"missing","resident":true}`} {
		_, err = request("PATCH", body)
		require.Error(t, err)
	}
	for _, value := range []string{"true", "false"} {
		_, err = request("PATCH", `{"name":"demo","resident":`+value+`}`)
		require.NoError(t, err)
		data, err := request("GET", "")
		require.NoError(t, err)
		items := data.(map[string]any)["items"].([]map[string]any)
		require.Len(t, items, 1)
		require.Equal(t, value == "true", items[0]["resident"])
	}
}
