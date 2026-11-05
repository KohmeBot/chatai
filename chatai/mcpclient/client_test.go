package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/plugin/v2/ui"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	for _, name := range []string{"echo", "fail", "slow"} {
		s.AddTool(&mcp.Tool{Name: name, Description: name, InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if name == "slow" {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(2 * time.Second):
					return &mcp.CallToolResult{}, nil
				}
			}
			return &mcp.CallToolResult{IsError: name == "fail", Content: []mcp.Content{&mcp.TextContent{Text: string(r.Params.Arguments)}}}, nil
		})
	}
	return s
}

func TestStdioHelper(t *testing.T) {
	if os.Getenv("CHATAI_MCP_TEST_HELPER") != "1" {
		return
	}
	if err := testServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestTransports(t *testing.T) {
	for _, transport := range []string{"streamable_http", "sse", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			c := ServerConfig{Name: "test", Transport: transport, TimeoutSeconds: 5}
			if transport == "stdio" {
				c.Command = os.Args[0]
				c.Args = []string{"-test.run=^TestStdioHelper$"}
				c.Env = map[string]ui.Secret{"CHATAI_MCP_TEST_HELPER": "1"}
			} else {
				s := testServer()
				var h http.Handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
				if transport == "sse" {
					h = mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return s }, nil)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer test" {
						w.WriteHeader(401)
						return
					}
					h.ServeHTTP(w, r)
				}))
				defer server.Close()
				c.URL = server.URL
				c.Headers = map[string]ui.Secret{"Authorization": "Bearer test"}
			}
			tools, err := Discover(context.Background(), []ServerConfig{c})
			if err != nil {
				t.Fatal(err)
			}
			if len(tools) != 3 {
				t.Fatalf("got %d tools", len(tools))
			}
			for _, tool := range tools {
				if len(tool.Definition.Function.Name) > 64 {
					t.Fatal("name too long")
				}
				if strings.Contains(tool.Definition.Function.Name, "_slow_") {
					continue
				}
				result, err := tool.Handler(&agent.RunContext{Context: context.Background()}, json.RawMessage(`{"message":"hello"}`))
				if strings.Contains(tool.Definition.Function.Name, "_fail_") {
					if err == nil {
						t.Fatal("tool error lost")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(result)
				if !strings.Contains(string(data), "hello") {
					t.Fatalf("result: %s", data)
				}
			}
			c.AllowedTools = []string{"echo"}
			tools, err = Discover(context.Background(), []ServerConfig{c})
			if err != nil || len(tools) != 1 {
				t.Fatalf("allowlist: %v %d", err, len(tools))
			}
			c.AllowedTools = []string{"missing"}
			if _, err = Discover(context.Background(), []ServerConfig{c}); err == nil {
				t.Fatal("missing allowed tool accepted")
			}
		})
	}
}

func TestCancellationAndValidation(t *testing.T) {
	s := testServer()
	server := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	defer server.Close()
	c := ServerConfig{Name: "test", Transport: "streamable_http", URL: server.URL, AllowedTools: []string{"slow"}}
	tools, err := Discover(context.Background(), []ServerConfig{c})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err = tools[0].Handler(&agent.RunContext{Context: ctx}, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected cancellation")
	}
	if time.Since(started) > time.Second {
		t.Fatal("cancellation did not promptly return")
	}
	if _, err = Discover(context.Background(), []ServerConfig{c, c}); err == nil {
		t.Fatal("duplicate server accepted")
	}
	for _, bad := range []ServerConfig{{Name: "bad name"}, {Name: "ok", Transport: "bad"}, {Name: "ok", Transport: "stdio"}, {Name: "ok", Transport: "sse", URL: "file:///x"}} {
		if err := bad.validate(); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}
