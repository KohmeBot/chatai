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

func TestOriginalRequestsHaveSeparateBudget(t *testing.T) {
	s := New(1)
	s.Observe(agent.Event{RunID: 1, Kind: "run_started", Time: time.Now()})
	body := `{"messages":"` + strings.Repeat("原文", 200000) + `"}`
	s.Observe(agent.Event{RunID: 1, Kind: "model_request", Request: body})
	args := strings.Repeat("a", 20000)
	s.Observe(agent.Event{RunID: 1, Kind: "tool_started", Arguments: args})
	s.Observe(agent.Event{RunID: 1, Kind: "model_request", Request: strings.Repeat("x", 8*1024*1024)})
	s.Observe(agent.Event{RunID: 1, Kind: "run_finished", Text: "done"})
	r := s.snapshot("1")[0]
	require.Equal(t, body, r.Events[1].Request)
	require.Equal(t, args, r.Events[2].Arguments)
	require.True(t, r.Truncated)
	require.Equal(t, "run_finished", r.Events[3].Kind)
	require.LessOrEqual(t, r.requestBytes, 8*1024*1024)
}
