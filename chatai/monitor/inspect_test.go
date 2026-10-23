package monitor

import (
	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInspectRequiresSession(t *testing.T) {
	s := New(1)
	called := false
	s.Inspect = func(r *http.Request) (any, error) {
		called = true
		return map[string]string{"section": r.PathValue("section")}, nil
	}
	h := s.Handler("secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/inspect/tools", nil))
	require.Equal(t, 401, w.Code)
	require.False(t, called)
	r := httptest.NewRequest("GET", "/api/inspect/tools", nil)
	r.AddCookie(login(t, h))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 200, w.Code)
	require.JSONEq(t, `{"section":"tools"}`, w.Body.String())
}

func TestScheduleMutationRequiresSessionAndJSON(t *testing.T) {
	s := New(1)
	called := false
	s.Inspect = func(r *http.Request) (any, error) {
		called = true
		require.Equal(t, "schedules", r.PathValue("section"))
		return map[string]bool{"ok": true}, nil
	}
	h := s.Handler("secret")
	cookie := login(t, h)
	for _, method := range []string{"POST", "DELETE"} {
		for _, tc := range []struct {
			auth              bool
			contentType, site string
			status            int
		}{
			{false, "application/json", "same-origin", 401}, {true, "text/plain", "same-origin", 403}, {true, "application/json", "cross-site", 403}, {true, "application/json", "same-origin", 200},
		} {
			called = false
			r := httptest.NewRequest(method, "/api/inspect/schedules?group=10", strings.NewReader(`{}`))
			if tc.auth {
				r.AddCookie(cookie)
			}
			r.Header.Set("Content-Type", tc.contentType)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.status == 200, called)
		}
	}
}

func TestMemoryMutationRequiresSessionAndJSON(t *testing.T) {
	s := New(1)
	called := false
	s.Inspect = func(r *http.Request) (any, error) {
		called = true
		require.Equal(t, "memory", r.PathValue("section"))
		return map[string]bool{"ok": true}, nil
	}
	h := s.Handler("secret")
	cookie := login(t, h)
	for _, method := range []string{"PATCH", "DELETE"} {
		for _, tc := range []struct {
			loggedIn          bool
			contentType, site string
			status            int
		}{
			{false, "application/json", "same-origin", 401},
			{true, "text/plain", "same-origin", 403},
			{true, "application/json", "cross-site", 403},
			{true, "application/json", "same-origin", 200},
		} {
			called = false
			r := httptest.NewRequest(method, "/api/inspect/memory?group=10", strings.NewReader(`{"target":"group"}`))
			if tc.loggedIn {
				r.AddCookie(cookie)
			}
			r.Header.Set("Content-Type", tc.contentType)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.status == 200, called)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/inspect/memory", nil)
	r.AddCookie(cookie)
	h.ServeHTTP(w, r)
	require.Equal(t, 405, w.Code)
}

func TestOriginalRequestsAreDiscardedAndLongToolArgumentsKeepEvents(t *testing.T) {
	s := New(1)
	s.Observe(agent.Event{RunID: 1, Kind: "run_started", Time: time.Now()})
	body := `{"messages":"` + strings.Repeat("原文", 200000) + `"}`
	s.Observe(agent.Event{RunID: 1, Kind: "model_request", Text: body})
	args := strings.Repeat("a", 20000)
	s.Observe(agent.Event{RunID: 1, Kind: "tool_started", Arguments: args})
	s.Observe(agent.Event{RunID: 1, Kind: "tool_started", Arguments: strings.Repeat("x", 8*1024*1024)})
	s.Observe(agent.Event{RunID: 1, Kind: "run_finished", Text: "done", Summary: "本轮处理已结束。"})
	r := s.snapshot("1")[0]
	require.Len(t, r.Events, 4)
	require.Equal(t, clip(args), r.Events[1].Arguments)
	require.Equal(t, clip(strings.Repeat("x", 8*1024*1024)), r.Events[2].Arguments)
	require.Equal(t, "本轮处理已结束。", r.Summary)
	require.True(t, r.Truncated)
	require.Equal(t, "run_finished", r.Events[3].Kind)
}
