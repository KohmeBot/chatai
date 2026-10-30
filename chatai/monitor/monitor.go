// Package monitor serves the embedded Agent observability and memory console.
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
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

//go:embed index.html
var page []byte

//go:embed memory-ui.js
var memoryUI []byte

//go:embed schedule-ui.js
var scheduleUI []byte

//go:embed catalog-ui.js
var catalogUI []byte

//go:embed run-overview.js
var runOverviewUI []byte

//go:embed run-dag.js
var runDAGUI []byte

//go:embed glass.css
var glassCSS []byte

//go:embed glass-motion.js
var glassMotion []byte

//go:embed glass-controls.js
var glassControls []byte

type Run struct {
	MetricsVersion int           `json:"metrics_version"`
	ModelCalls     int           `json:"model_calls"`
	ToolSkillCount int           `json:"tool_skill_count"`
	Summary        string        `json:"summary,omitempty"`
	UserName       string        `json:"user_name,omitempty"`
	ID             string        `json:"id"`
	GroupID        string        `json:"group_id"`
	UserID         string        `json:"user_id"`
	Model          string        `json:"model"`
	Prompt         string        `json:"prompt"`
	Status         string        `json:"status"`
	StartedAt      time.Time     `json:"started_at"`
	DurationMS     int64         `json:"duration_ms"`
	InputTokens    int64         `json:"input_tokens"`
	OutputTokens   int64         `json:"output_tokens"`
	Error          string        `json:"error,omitempty"`
	Truncated      bool          `json:"truncated"`
	Events         []agent.Event `json:"events,omitempty"`
	persisted      int
}

type Store struct {
	// Inspect is installed before serving; memory supports authenticated edits.
	Inspect   func(*http.Request) (any, error)
	mu        sync.RWMutex
	runs      map[uint64]*Run
	limit     int
	listeners map[chan struct{}]struct{}
	db        *gorm.DB
	session   string
	retention time.Duration
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
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
	// Ignore legacy request observations as well as no longer producing them.
	if e.Kind == "model_request" {
		return
	}
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
			if oldest == 0 && s.db == nil {
				return
			}
			if old := s.runs[oldest]; old != nil && (s.db == nil || old.persisted == len(old.Events)) {
				delete(s.runs, oldest)
			}
		}
		r = &Run{UserName: clip(e.UserName), ID: strconv.FormatUint(e.RunID, 10), GroupID: strconv.FormatInt(e.GroupID, 10), UserID: strconv.FormatInt(e.UserID, 10), Model: e.Model, Prompt: clip(e.Text), StartedAt: e.Time, Status: "running"}
		if s.db != nil {
			r.ID = s.session + "-" + r.ID
		}
		s.runs[e.RunID] = r
	}
	if r == nil {
		return
	}
	r.countEvent(e)
	if e.Summary != "" {
		r.Summary = clip(e.Summary)
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
	for _, field := range []*string{&e.Summary, &e.UserName, &e.Text, &e.Arguments, &e.Result, &e.Reasoning, &e.Error, &e.Name, &e.Model, &e.CallID} {
		if len(*field) > 8192 {
			r.Truncated = true
		}
		*field = clip(*field)
	}
	// Bound individual payloads, but retain every event so the timeline stays complete.
	r.Events = append(r.Events, e)
	if s.db != nil {
		if err := s.persist(r); err != nil {
			logrus.Errorf("[ChatAI][Monitor] persist run %s (will retry): %v", r.ID, err)
		}
		s.trimCache()
	}
	for listener := range s.listeners {
		select {
		case listener <- struct{}{}:
		default:
		}
	}
}

// Count attempts, including pending/failed calls. read_skill already counts as
// a tool call; only skills loaded by other paths add another resource read.
func (r *Run) countEvent(e agent.Event) {
	r.MetricsVersion = 1
	switch e.Kind {
	case "model_started":
		r.ModelCalls++
	case "tool_started":
		r.ToolSkillCount++
	case "skill_loaded":
		if e.Name != "read_skill" {
			r.ToolSkillCount++
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
	mux.HandleFunc("GET /run-dag.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(runDAGUI)
	})
	mux.HandleFunc("GET /catalog-ui.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(catalogUI)
	})
	mux.HandleFunc("GET /glass-motion.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(glassMotion)
	})
	mux.HandleFunc("GET /glass-controls.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(glassControls)
	})
	mux.HandleFunc("GET /run-overview.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(runOverviewUI)
	})
	mux.HandleFunc("GET /schedule-ui.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(scheduleUI)
	})
	mux.HandleFunc("GET /memory-ui.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(memoryUI)
	})
	mux.HandleFunc("GET /glass.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(glassCSS)
	})
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
		items, err := s.readSnapshot("")
		if err != nil {
			http.Error(w, "读取运行记录失败，请稍后重试", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}))
	mux.HandleFunc("GET /api/runs/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		items, err := s.readSnapshot(r.PathValue("id"))
		if err != nil {
			http.Error(w, "读取运行记录失败，请稍后重试", 500)
			return
		}
		if len(items) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items[0])
	}))
	mux.HandleFunc("GET /api/events", auth(s.stream))
	mux.HandleFunc("GET /api/retention", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"persistent": s.db != nil, "days": int(s.retention / (24 * time.Hour))})
	}))
	for _, route := range []struct{ method, section string }{{"PATCH", "skills"}, {"PATCH", "memory"}, {"DELETE", "memory"}, {"POST", "schedules"}, {"DELETE", "schedules"}} {
		mux.HandleFunc(route.method+" /api/inspect/"+route.section, auth(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "仅支持同站 JSON 请求", http.StatusForbidden)
				return
			}
			if s.Inspect == nil {
				http.Error(w, "运行数据尚未就绪", 503)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
			r.SetPathValue("section", route.section)
			value, err := s.Inspect(r)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(value)
		}))
	}
	mux.HandleFunc("GET /api/inspect/{section}", auth(func(w http.ResponseWriter, r *http.Request) {
		if s.Inspect == nil {
			http.Error(w, "运行数据尚未就绪", 503)
			return
		}
		value, err := s.Inspect(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(value)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https://q1.qlogo.cn https://p.qlogo.cn; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
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
