package chatai

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/chatai/chatai/schedule"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func schedulePlugin(t *testing.T) *ChatPlugin {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tasks.db")), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	require.NoError(t, db.AutoMigrate(schedule.Models()...))
	return &ChatPlugin{db: db, scheduler: schedule.New(db), personaMap: map[int64]*persona.Persona{10: {}, 20: {}}}
}

func TestMonitorScheduleCRUD(t *testing.T) {
	c := schedulePlugin(t)
	request := func(method, group, body string) (any, error) {
		r := httptest.NewRequest(method, "/api/inspect/schedules?group="+group, strings.NewReader(body))
		r.SetPathValue("section", "schedules")
		return c.inspectMonitor(r)
	}
	value, err := request("POST", "10", `{"name":"日报","instruction":"汇总消息","cron":"0 9 * * *","timezone":"Asia/Shanghai","user_id":"123","bot_id":"456"}`)
	require.NoError(t, err)
	task := value.(*schedule.Task)
	require.True(t, task.NextRunAt.After(time.Now()))
	_, err = request("DELETE", "20", `{"task_id":1}`)
	require.Error(t, err)
	for _, body := range []string{
		`{"instruction":"x","delay_seconds":10,"user_id":"bad","bot_id":"456"}`,
		`{"instruction":"x","delay_seconds":10,"user_id":"123","bot_id":"456","extra":true}`,
		`{"instruction":"x","delay_seconds":10,"user_id":"123","bot_id":"456"} {}`,
	} {
		_, err = request("POST", "10", body)
		require.Error(t, err)
	}
	_, err = request("POST", "99", `{}`)
	require.Error(t, err)
	value, err = request("GET", "10", "")
	require.NoError(t, err)
	require.Len(t, value.(map[string]any)["items"], 1)
	_, err = request("DELETE", "10", `{"task_id":1}`)
	require.NoError(t, err)
	value, err = request("GET", "10", "")
	require.NoError(t, err)
	require.Empty(t, value.(map[string]any)["items"])
}
