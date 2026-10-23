package persona

import (
	"encoding/json"
	"testing"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func TestMediaToolsSendAndPersistSegments(t *testing.T) {
	p := &Persona{groupID: 123, db: memoryTestDB(t)}
	require.NoError(t, p.db.AutoMigrate(&ChatMessageRecord{}))
	for _, tc := range []struct {
		name       string
		handler    agent.Handler
		args, want string
	}{
		{"video", p.handleSendVideo, `{"url":"https://example.com/a.mp4"}`, `[{"type":"video","data":{"file":"https://example.com/a.mp4"}}]`},
		{"record", p.handleSendRecord, `{"url":"/tmp/voice.wav"}`, `[{"type":"record","data":{"file":"/tmp/voice.wav"}}]`},
		{"music", p.handleSendMusic, `{"type":"163","id":"9007199254740993"}`, `[{"type":"music","data":{"type":"163","id":"9007199254740993"}}]`},
		{"custom", p.handleSendMusic, `{"type":"custom","url":"https://example.com/song","audio":"https://example.com/a.mp3","title":"歌曲","content":"歌手","image":"https://example.com/cover.jpg"}`, `[{"type":"music","data":{"type":"custom","url":"https://example.com/song","audio":"https://example.com/a.mp3","title":"歌曲","content":"歌手","image":"https://example.com/cover.jpg"}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &deliveryTestCaller{response: &zero.APIResponse{Status: "ok", Data: gjson.Parse(`{"message_id":-42}`)}}
			zero.APICallers.Store(987654325, caller)
			t.Cleanup(func() { zero.APICallers.Delete(987654325) })
			ctx := zero.GetBot(987654325)
			ctx.Event = &zero.Event{SelfID: 987654325, GroupID: 123}
			rc := &agent.RunContext{Values: map[string]any{"zero_ctx": ctx}}
			result, err := tc.handler(rc, json.RawMessage(tc.args))
			require.NoError(t, err)
			require.Equal(t, int64(-42), result.(map[string]any)["message_id"])
			require.True(t, rc.ResponseDelivered())
			require.True(t, rc.ActionPerformed())
			require.Len(t, caller.requests, 1)
			require.Equal(t, "send_group_msg", caller.requests[0].Action)
			require.Equal(t, int64(123), caller.requests[0].Params["group_id"])
			encoded, err := json.Marshal(caller.requests[0].Params["message"])
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(encoded))
			rows, err := p.recentMessages(1)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.JSONEq(t, tc.want, rows[0].RawSegments)
		})
	}
}

func TestMediaToolsValidateBeforeSending(t *testing.T) {
	p := &Persona{}
	for _, handler := range []agent.Handler{p.handleSendVideo, p.handleSendRecord} {
		_, err := handler(nil, json.RawMessage(`{"url":"  "}`))
		require.ErrorContains(t, err, "cannot be empty")
		_, err = handler(nil, json.RawMessage(`{`))
		require.Error(t, err)
	}
	for _, args := range []string{
		`{"type":"unknown"}`, `{"type":"qq"}`, `{"type":"custom","title":"song"}`,
		`{"type":"custom","url":"https://example.com","audio":"file:///tmp/a","title":"song"}`,
		`{"type":"custom","url":"https://example.com","audio":"https://example.com/a","title":" "}`,
		`{"type":"custom","url":"https://example.com","audio":"https://example.com/a","title":"song","image":"bad"}`,
	} {
		_, err := p.handleSendMusic(nil, json.RawMessage(args))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "context", "must reject arguments before accessing context")
	}
}
