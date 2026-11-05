package monitor

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
)

var monitorPreview = flag.Bool("monitor-preview", false, "serve the monitor UI with in-memory demo data")
var monitorPreviewPort = flag.Int("monitor-preview-port", 9086, "localhost preview port (0 selects a free port)")

// TestMonitorPreview is opt-in so normal test runs never wait for a browser.
// go test ./chatai/monitor -run '^TestMonitorPreview$' -v -count=1 -timeout=0 -args -monitor-preview
func TestMonitorPreview(t *testing.T) {
	if !*monitorPreview {
		t.Skip("manual UI preview; enable with -args -monitor-preview")
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *monitorPreviewPort))
	require.NoError(t, err)
	server := &http.Server{Handler: newMonitorPreview().Handler("preview"), ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { _ = server.Close() })
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Logf("\n后台预览：http://%s\n登录口令：preview\n全部为模拟数据；常驻切换仅保存在内存，记忆和任务编辑不提交。\n按 Ctrl+C 停止。", listener.Addr())
	select {
	case <-interrupt:
	case err := <-done:
		if err != http.ErrServerClosed {
			t.Fatal(err)
		}
	}
}

type previewObject = map[string]any

func newMonitorPreview() *Store {
	s := New(100)
	now := time.Now()
	for i, status := range []string{"success", "error", "running"} {
		start := now.Add(-time.Duration(i+1) * 10 * time.Minute)
		events := []agent.Event{
			{Kind: "run_started", Text: []string{"整理本周项目进展并生成摘要", "查询一条暂时不可用的网页", "查找群内最近讨论的资料"}[i], UserName: "预览用户", Model: "demo-model"},
			{Kind: "model_started", Step: 1},
			{Kind: "model_finished", Step: 1, InputTokens: 1240, OutputTokens: 180, DurationMS: 1200, Text: "先读取相关资料，再整理结果。"},
			{Kind: "skill_loaded", Step: 1, Name: "read_skill", Arguments: `{"name":"weekly-summary"}`},
			{Kind: "tool_planned", Step: 1, CallID: "demo-call", Name: "search_context", Arguments: `{"query":"项目进展"}`},
			{Kind: "tool_started", Step: 1, CallID: "demo-call", Name: "search_context"},
		}
		if status != "running" {
			errText := ""
			if status == "error" {
				errText = "模拟错误：请求超时，请稍后重试"
			}
			events = append(events,
				agent.Event{Kind: "tool_finished", Step: 1, CallID: "demo-call", Name: "search_context", Result: `{"items":["完成后台卡片布局调整","补充本地预览入口"]}`, DurationMS: 420, Error: errText},
				agent.Event{Kind: "model_started", Step: 2},
				agent.Event{Kind: "model_finished", Step: 2, Text: "本周完成后台布局调整，并增加了本地预览入口。", InputTokens: 1680, OutputTokens: 230, DurationMS: 1800},
				agent.Event{Kind: "run_finished", DurationMS: 8000, Summary: "模拟任务执行完成", Error: errText})
		}
		for j, e := range events {
			e.RunID, e.GroupID, e.UserID = uint64(i+1), 10001, int64(20001+i)
			e.Time = start.Add(time.Duration(j) * time.Second)
			s.Observe(e)
		}
	}

	skills := []previewObject{
		{"name": "weekly-summary", "description": "整理群聊中的项目进展、待办事项和风险，生成便于阅读的每周摘要。", "source": "目录技能", "path": "demo/skills/weekly-summary", "resident": true},
		{"name": "web-research", "description": "检索资料并核对来源。适用于需要比较多个信息来源、整理引用和提炼结论的任务。较长的说明用于检查卡片底部按钮是否对齐。", "source": "目录技能", "path": "demo/skills/web-research", "resident": false},
		{"name": "quick-note", "description": "记录一条简短的备忘。", "source": "经验技能", "resident": false, "content": "# 快速备忘\n\n提取事项、负责人和截止时间。", "scope": "global", "version": 1, "updated_at": now},
	}
	users := []previewObject{}
	for i, summary := range []string{"喜欢简洁直接的回复，正在维护群聊助手。", "关注前端布局、深浅主题和移动端体验。希望同类操作保持相同位置，并明确展示保存结果。", ""} {
		users = append(users, previewObject{"user_id": fmt.Sprint(20001 + i), "summary": summary, "version": 1, "updated_at": now,
			"relationship": previewObject{"score": 120, "level": "熟悉", "description": "经常参与项目讨论", "reason": "模拟关系数据"},
			"items":        []previewObject{{"key": "reply-style", "kind": "preference", "text": "偏好简短、明确的技术说明", "confidence": "explicit", "source_id": 0, "observed_at": now}},
		})
	}
	memory := previewObject{
		"short_enabled": true, "has_more": false, "users": users,
		"group_memory": previewObject{"summary": "用于后台 UI 预览的项目讨论群，所有内容均为模拟数据。", "version": 1, "updated_at": now, "items": []previewObject{{"key": "topic", "kind": "fact", "text": "主要讨论机器人与后台界面", "confidence": "explicit", "observed_at": now}}},
		"short":        []previewObject{{"user_id": "20001", "note": previewObject{"summary": "正在讨论卡片按钮的一致性。", "dialogue": "用户：请把详情按钮统一放在左下角。", "first_action": "检查各类卡片布局"}, "updated_at": now, "expires_at": now.Add(time.Hour)}},
	}
	tasks := []previewObject{}
	for i, status := range []string{"active", "completed", "failed"} {
		task := previewObject{"id": i + 1, "name": []string{"每日项目摘要", "会议提醒", "资料同步"}[i], "instruction": "汇总当前项目进展，提醒尚未完成的事项。（模拟任务，不会实际执行）", "status": status, "group_id": "10001", "user_id": "20001", "bot_id": "30001", "created_at": now.Add(-24 * time.Hour), "runs": []previewObject{}}
		if status == "active" {
			task["cron"], task["timezone"], task["next_run_at"] = "0 9 * * *", "Asia/Shanghai", now.Add(24*time.Hour)
		} else {
			run := previewObject{"status": "success", "scheduled_at": now.Add(-time.Hour), "started_at": now.Add(-time.Hour), "finished_at": now.Add(-time.Hour + 3*time.Second)}
			if status == "failed" {
				run["status"], run["error"] = "failed", "模拟错误：服务暂时不可用"
			}
			task["runs"] = []previewObject{run}
		}
		tasks = append(tasks, task)
	}
	tools := []previewObject{}
	for _, name := range []string{"search_context", "send_message", "read_skill"} {
		tools = append(tools, previewObject{"namespace": "demo", "read_only": name != "send_message", "risk": "low", "idempotent": true, "group_action": name == "send_message", "definition": previewObject{"type": "function", "function": previewObject{"name": name, "description": "用于检查工具卡片和参数详情的模拟工具。", "parameters": previewObject{"type": "object", "properties": previewObject{"query": previewObject{"type": "string", "description": "待处理内容"}}, "required": []string{"query"}}}}})
	}

	var mu sync.Mutex
	s.Inspect = func(r *http.Request) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		section := r.PathValue("section")
		if r.Method != http.MethodGet {
			if section != "skills" || r.Method != http.MethodPatch {
				return nil, fmt.Errorf("预览模式仅供浏览；记忆和定时任务编辑不会提交")
			}
			var payload struct {
				Name     string `json:"name"`
				Resident bool   `json:"resident"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			for _, skill := range skills {
				if skill["name"] == payload.Name {
					skill["resident"] = payload.Resident
					return previewObject{"resident": payload.Resident}, nil
				}
			}
			return nil, fmt.Errorf("模拟技能不存在")
		}
		var result any
		switch section {
		case "groups":
			result = []string{"10001"}
		case "skills":
			result = previewObject{"enabled": true, "items": skills}
		case "tools":
			result = tools
		case "memory":
			result = memory
		case "schedules":
			result = previewObject{"items": tasks, "has_more": false, "bots": []string{"30001"}}
		case "skill":
			result = previewObject{"content": "# " + r.URL.Query().Get("name") + "\n\n这是本地预览用的模拟 Skill。\n\n## 执行步骤\n\n1. 阅读用户需求\n2. 整理相关资料\n3. 输出简短结论", "triggers": []string{"用户要求整理资料"}, "non_triggers": []string{"闲聊"}, "required_tools": []string{"search_context"}}
		case "skill-files":
			result = []previewObject{{"path": "SKILL.md", "size": 128}, {"path": "references/example.md", "size": 64}}
		case "skill-file":
			result = previewObject{"path": r.URL.Query().Get("file"), "kind": "text", "size": 64, "content": "# 模拟文件\n\n- 卡片布局\n- 详情弹窗\n- 深浅主题"}
		default:
			return nil, fmt.Errorf("未知预览页面：%s", section)
		}
		// Snapshot under the lock: the HTTP encoder runs after Inspect returns.
		data, err := json.Marshal(result)
		return json.RawMessage(data), err
	}
	return s
}

func TestMonitorPreviewData(t *testing.T) {
	h := newMonitorPreview().Handler("secret")
	cookie := login(t, h)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(cookie)
		if method != http.MethodGet {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/", "/glass.css", "/catalog-ui.js", "/api/runs", "/api/runs/1", "/api/inspect/groups", "/api/inspect/skills", "/api/inspect/tools", "/api/inspect/memory", "/api/inspect/schedules", "/api/inspect/skill?name=weekly-summary", "/api/inspect/skill-files", "/api/inspect/skill-file?file=SKILL.md"} {
		w := request("GET", path, "")
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
		if strings.HasPrefix(path, "/api/") {
			require.True(t, json.Valid(w.Body.Bytes()), path)
		}
	}
	w := request("PATCH", "/api/inspect/skills", `{"name":"weekly-summary","resident":false}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"resident":false}`, w.Body.String())
	var catalog struct {
		Items []struct {
			Name     string
			Resident bool
		}
	}
	require.NoError(t, json.Unmarshal(request("GET", "/api/inspect/skills", "").Body.Bytes(), &catalog))
	require.Equal(t, "weekly-summary", catalog.Items[0].Name)
	require.False(t, catalog.Items[0].Resident)
	for _, path := range []string{"memory", "schedules"} {
		w := request("DELETE", "/api/inspect/"+path, `{}`)
		require.NotEqual(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), "预览模式")
	}
}
