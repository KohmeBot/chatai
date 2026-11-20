package chatai

import (
	"fmt"
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

func TestMonitorPrivateScheduleCRUD(t *testing.T) {
	c := schedulePlugin(t)
	c.conf.PrivateChat = PrivateChatConfig{Enable: true, AllowedUsers: []int64{123}}
	request := func(method, query, body string) (any, error) {
		r := httptest.NewRequest(method, "/api/inspect/schedules?"+query, strings.NewReader(body))
		r.SetPathValue("section", "schedules")
		return c.inspectMonitor(r)
	}
	const body = `{"name":"提醒","instruction":"提醒我","delay_seconds":10,"user_id":"123","bot_id":"456"}`
	group, err := request("POST", "group=10", body)
	require.NoError(t, err)
	private, err := request("POST", "scope=private", body)
	require.NoError(t, err)
	require.True(t, private.(*schedule.Task).IsPrivate())
	_, err = request("POST", "scope=private", strings.Replace(body, "123", "999", 1))
	require.Error(t, err)
	_, err = request("GET", "group=10&scope=invalid", "")
	require.Error(t, err)
	_, err = request("DELETE", "scope=private", fmt.Sprintf(`{"task_id":%d}`, group.(*schedule.Task).ID))
	require.Error(t, err)
	deletePrivate := fmt.Sprintf(`{"task_id":%d}`, private.(*schedule.Task).ID)
	_, err = request("DELETE", "group=10", deletePrivate)
	require.Error(t, err)
	value, err := request("GET", "scope=private", "")
	require.NoError(t, err)
	tasks := value.(map[string]any)["items"].([]schedule.Task)
	require.Len(t, tasks, 1)
	require.Equal(t, private.(*schedule.Task).ID, tasks[0].ID)
	_, err = request("DELETE", "scope=private", deletePrivate)
	require.NoError(t, err)
	value, err = request("GET", "scope=private", "")
	require.NoError(t, err)
	require.Empty(t, value.(map[string]any)["items"])
}

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
