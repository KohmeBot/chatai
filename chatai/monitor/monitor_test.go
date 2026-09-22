package monitor

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
)

func TestRetentionAndSnapshots(t *testing.T) {
	s := New(2)
	start := func(id uint64) {
		s.Observe(agent.Event{RunID: id, Kind: "run_started", Time: time.Now(), Text: "hello", GroupID: 123, UserID: 456})
	}
	start(1)
	start(2)
	s.Observe(agent.Event{RunID: 2, Kind: "model_finished", InputTokens: 20, OutputTokens: 5})
	s.Observe(agent.Event{RunID: 2, Kind: "run_finished", Error: "provider failed", DurationMS: 1234})
	start(3)
	require.Empty(t, s.snapshot("2"))
	require.Len(t, s.snapshot("1"), 1) // Active runs are never evicted.
	start(4)
	require.Empty(t, s.snapshot("4"))
	s.Observe(agent.Event{RunID: 1, Kind: "model_finished", InputTokens: 20, OutputTokens: 5})
	s.Observe(agent.Event{RunID: 1, Kind: "run_finished", Error: "provider failed", DurationMS: 1234})
	r := s.snapshot("1")[0]
	require.Equal(t, "error", r.Status)
	require.EqualValues(t, 20, r.InputTokens)
	require.EqualValues(t, 5, r.OutputTokens)
	require.EqualValues(t, 1234, r.DurationMS)
	r.Events[0].Text = "changed"
	require.Equal(t, "hello", s.snapshot("1")[0].Events[0].Text)
	require.Nil(t, s.snapshot("")[0].Events)
}

func TestCapacityAndConcurrency(t *testing.T) {
	s := New(5)
	s.Observe(agent.Event{RunID: 1, Kind: "run_started", Time: time.Now()})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Observe(agent.Event{RunID: 1, Kind: "model_finished", InputTokens: 1, Text: strings.Repeat("字", 9000)})
				s.snapshot("")
			}
		}()
	}
	wg.Wait()
	s.Observe(agent.Event{RunID: 1, Kind: "run_finished", Error: "final error"})
	r := s.snapshot("1")[0]
	require.True(t, r.Truncated)
	require.EqualValues(t, 1000, r.InputTokens)
	require.Equal(t, "error", r.Status)
	require.Equal(t, "final error", r.Error)
	require.LessOrEqual(t, r.bytes, 512*1024)
}

func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/session", nil)
	r.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(w, r)
	require.Equal(t, 204, w.Code)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	require.True(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
	return cookies[0]
}

func TestAuthenticationAndReadOnlyAPI(t *testing.T) {
	s := New(2)
	s.Observe(agent.Event{RunID: 1, Kind: "run_started", Time: time.Now(), Text: "<script>alert(1)</script>"})
	h := s.Handler("secret")
	for _, path := range []string{"/api/runs", "/api/runs/1", "/api/events"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 401, w.Code)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/session", nil))
	require.Equal(t, 401, w.Code)
	cookie := login(t, h)
	for _, path := range []string{"/api/runs", "/api/runs/1"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(cookie)
		h.ServeHTTP(w, r)
		require.Equal(t, 200, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.NotContains(t, w.Body.String(), "<script>")
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/runs/999", nil)
	r.AddCookie(cookie)
	h.ServeHTTP(w, r)
	require.Equal(t, 404, w.Code)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/runs", nil))
	require.Equal(t, 405, w.Code)
}

func TestSSERefreshAndDisconnect(t *testing.T) {
	s := New(2)
	h := s.Handler("secret")
	cookie := login(t, h)
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/events", nil)
	require.NoError(t, err)
	r.AddCookie(cookie)
	response, err := server.Client().Do(r)
	require.NoError(t, err)
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	require.True(t, scanner.Scan())
	require.Equal(t, "data: refresh", scanner.Text())
	require.True(t, scanner.Scan())
	s.Observe(agent.Event{RunID: 1, Kind: "run_started", Time: time.Now()})
	require.True(t, scanner.Scan())
	require.Equal(t, "data: refresh", scanner.Text())
	cancel()
	response.Body.Close()
	require.Eventually(t, func() bool { s.mu.RLock(); defer s.mu.RUnlock(); return len(s.listeners) == 0 }, time.Second, 10*time.Millisecond)
}
