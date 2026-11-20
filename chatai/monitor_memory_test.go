package chatai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/stretchr/testify/require"
)

func TestMonitorPrivateMemoryRoutes(t *testing.T) {
	c := schedulePlugin(t)
	require.NoError(t, c.db.AutoMigrate(&persona.UserMemory{}, &persona.PrivateUserMemory{}))
	shared := persona.UserMemory{UserID: 123, Version: 1, ProfileJSON: `{"summary":"群聊画像"}`}
	require.NoError(t, c.db.Create(&shared).Error)
	private := persona.PrivateUserMemory{
		BotID:       456,
		UserID:      123,
		Version:     1,
		ProfileJSON: `{"summary":"私聊画像"}`,
	}
	require.NoError(t, c.db.Create(&private).Error)
	request := func(method, query, body string) (any, error) {
		r := httptest.NewRequest(method, "/api/inspect/memory?"+query, strings.NewReader(body))
		r.SetPathValue("section", "memory")
		return c.inspectMonitor(r)
	}
	loadPrivate := func() map[string]any {
		value, err := request(http.MethodGet, "scope=private", "")
		require.NoError(t, err)
		rows := value.(map[string]any)["users"].([]map[string]any)
		require.Len(t, rows, 1)
		return rows[0]
	}
	value, err := request(http.MethodGet, "scope=private", "")
	require.NoError(t, err)
	require.Equal(t, false, value.(map[string]any)["short_enabled"])
	c.conf.Agent.ConversationMemory.Enable = true
	c.conf.PrivateChat = PrivateChatConfig{Enable: true, AllowedUsers: []int64{123}}
	value, err = request(http.MethodGet, "scope=private", "")
	require.NoError(t, err)
	require.Equal(t, true, value.(map[string]any)["short_enabled"])
	require.Empty(t, value.(map[string]any)["short"])
	require.Equal(t, "私聊画像", loadPrivate()["summary"])
	require.Equal(t, "456", loadPrivate()["bot_id"])
	const edit = `{"target":"profile","user_id":"123","bot_id":"456","version":1,"summary":"更新后的私聊画像"}`
	_, err = request(http.MethodPatch, "group=10", edit)
	require.Error(t, err)
	_, err = request(http.MethodPatch, "scope=private", strings.Replace(edit, `"456"`, `"789"`, 1))
	require.Error(t, err)
	_, err = request(http.MethodPatch, "scope=private", edit)
	require.NoError(t, err)
	require.Equal(t, "更新后的私聊画像", loadPrivate()["summary"])
	_, err = request(http.MethodDelete, "scope=private", `{"target":"profile","user_id":"123","bot_id":"456","version":2}`)
	require.NoError(t, err)
	require.Empty(t, loadPrivate()["summary"])
	require.NoError(t, c.db.First(&shared, "user_id = ?", 123).Error)
	require.Equal(t, `{"summary":"群聊画像"}`, shared.ProfileJSON)
}
