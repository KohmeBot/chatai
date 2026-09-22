// Package monitor serves the embedded, read-only Agent observability console.
package monitor

import (
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
)

//go:embed index.html
var page []byte

type Run struct {
	UserName     string        `json:"user_name,omitempty"`
	ID           string        `json:"id"`
	GroupID      string        `json:"group_id"`
	UserID       string        `json:"user_id"`
	Model        string        `json:"model"`
	Prompt       string        `json:"prompt"`
	Status       string        `json:"status"`
	StartedAt    time.Time     `json:"started_at"`
	DurationMS   int64         `json:"duration_ms"`
	InputTokens  int64         `json:"input_tokens"`
	OutputTokens int64         `json:"output_tokens"`
	Error        string        `json:"error,omitempty"`
	Truncated    bool          `json:"truncated"`
	Events       []agent.Event `json:"events,omitempty"`
	bytes        int
}

type Store struct {
	mu        sync.RWMutex
	runs      map[uint64]*Run
	limit     int
	listeners map[chan struct{}]struct{}
}

func New(limit int) *Store {
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	return &Store{runs: make(map[uint64]*Run), limit: limit, listeners: make(map[chan struct{}]struct{})}
}

func clip(s string) string {
	if len(s) <= 8192 {
		return s
	}
	return strings.ToValidUTF8(s[:8192], "") + "\n[内容已截断]"
}

func (s *Store) Observe(e agent.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.runs[e.RunID]
	if e.Kind == "run_started" {
		if len(s.runs) >= s.limit {
			var oldest uint64
			for id, item := range s.runs {
				if item.Status != "running" && (oldest == 0 || id < oldest) {
					oldest = id
				}
			}
			// Keep active runs; refuse new observations if every slot is active.
			if oldest == 0 {
				return
			}
			delete(s.runs, oldest)
		}
		r = &Run{UserName: clip(e.UserName), ID: strconv.FormatUint(e.RunID, 10), GroupID: strconv.FormatInt(e.GroupID, 10), UserID: strconv.FormatInt(e.UserID, 10), Model: e.Model, Prompt: clip(e.Text), StartedAt: e.Time, Status: "running"}
		s.runs[e.RunID] = r
	}
	if r == nil {
		return
	}
	if e.Kind == "model_finished" {
		r.InputTokens += e.InputTokens
		r.OutputTokens += e.OutputTokens
	}
	if e.Kind == "run_finished" {
		r.Status = "success"
		if e.Error != "" {
			r.Status = "error"
		}
		r.Error = clip(e.Error)
		r.DurationMS = e.DurationMS
	}
	for _, field := range []*string{&e.UserName, &e.Text, &e.Arguments, &e.Result, &e.Reasoning, &e.Error, &e.Name, &e.Model, &e.CallID} {
		if len(*field) > 8192 {
			r.Truncated = true
		}
		*field = clip(*field)
	}
	data, _ := json.Marshal(e)
	if len(r.Events) < 512 && r.bytes+len(data) <= 512*1024 {
		r.Events = append(r.Events, e)
		r.bytes += len(data)
	} else {
		r.Truncated = true
	}
	for listener := range s.listeners {
		select {
		case listener <- struct{}{}:
		default:
		}
	}
}

func (s *Store) snapshot(id string) []Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Run, 0, len(s.runs))
	for _, r := range s.runs {
		if id != "" && r.ID != id {
			continue
		}
		copy := *r
		copy.Events = nil
		if id != "" {
			copy.Events = append([]agent.Event(nil), r.Events...)
		}
		if copy.Status == "running" {
			copy.DurationMS = time.Since(copy.StartedAt).Milliseconds()
		}
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// Handler requires a non-empty access token. Only the static login shell is public.
func (s *Store) Handler(token string) http.Handler {
	hash := sha256.Sum256([]byte(token))
	cookieValue := hex.EncodeToString(hash[:])
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, r *http.Request) {
		provided := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
		if token == "" || subtle.ConstantTimeCompare(provided[:], hash[:]) != 1 {
			http.Error(w, "访问口令错误", 401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "chatai_monitor", Value: cookieValue, Path: "/api/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
		w.WriteHeader(http.StatusNoContent)
	})
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("chatai_monitor")
			if token == "" || err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(cookieValue)) != 1 {
				http.Error(w, "请先登录", 401)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("DELETE /api/session", auth(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "chatai_monitor", Path: "/api/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /api/runs", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.snapshot(""))
	}))
	mux.HandleFunc("GET /api/runs/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		items := s.snapshot(r.PathValue("id"))
		if len(items) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items[0])
	}))
	mux.HandleFunc("GET /api/events", auth(s.stream))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}

func (s *Store) stream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.listeners[ch] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.listeners, ch); s.mu.Unlock() }()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	controller := http.NewResponseController(w)
	write := func(message string) bool {
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err := fmt.Fprint(w, message)
		if err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	if !write("data: refresh\n\n") {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if !write("data: refresh\n\n") {
				return
			}
		case <-ticker.C:
			if !write(": heartbeat\n\n") {
				return
			}
		}
	}
}
