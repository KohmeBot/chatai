// Package mcpclient adapts remote MCP tools to the Agent registry.
package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/plugin/v2/ui"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ServerConfig struct {
	Name           string               `yaml:"name"`
	Transport      string               `yaml:"transport" jsonschema:"enum=stdio,enum=streamable_http,enum=sse"`
	Command        string               `yaml:"command,omitempty"`
	Args           []string             `yaml:"args,omitempty"`
	Env            map[string]ui.Secret `yaml:"env,omitempty"`
	Dir            string               `yaml:"dir,omitempty"`
	URL            string               `yaml:"url,omitempty"`
	Headers        map[string]ui.Secret `yaml:"headers,omitempty"`
	AllowedTools   []string             `yaml:"allowed_tools,omitempty"`
	TimeoutSeconds int                  `yaml:"timeout_seconds,omitempty" jsonschema:"minimum=1,maximum=900"`
}

var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,24}$`)

func (c ServerConfig) validate() error {
	if !validName.MatchString(c.Name) {
		return fmt.Errorf("MCP server name must be 1..24 letters, digits, underscores or hyphens")
	}
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 900 {
		return fmt.Errorf("MCP %s: timeout_seconds must be 0..900", c.Name)
	}
	switch c.Transport {
	case "stdio":
		if c.Command == "" || c.URL != "" {
			return fmt.Errorf("MCP %s: stdio requires command and no url", c.Name)
		}
	case "streamable_http", "sse":
		u, err := url.Parse(c.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || c.Command != "" {
			return fmt.Errorf("MCP %s: HTTP transport requires an http(s) URL without userinfo and no command", c.Name)
		}
	default:
		return fmt.Errorf("MCP %s: unsupported transport %q", c.Name, c.Transport)
	}
	return nil
}

func (c ServerConfig) timeout() time.Duration {
	if c.TimeoutSeconds == 0 {
		return 60 * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

type headerTransport struct {
	headers map[string]ui.Secret
	origin  *url.URL
	ctx     context.Context
}

type responseBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *responseBody) Close() error { defer b.cleanup(); return b.ReadCloser.Close() }

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != t.origin.Scheme || r.URL.Host != t.origin.Host {
		return nil, fmt.Errorf("MCP cross-origin request refused")
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	r = r.Clone(ctx)
	for k, v := range t.headers {
		r.Header.Set(k, string(v))
	}
	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		cleanup()
		return nil, err
	}
	response.Body = &responseBody{response.Body, cleanup}
	return response, nil
}

func connect(ctx context.Context, c ServerConfig) (*mcp.ClientSession, error) {
	var transport mcp.Transport
	if c.Transport == "stdio" {
		cmd := exec.Command(c.Command, c.Args...)
		cmd.Dir = c.Dir
		cmd.Env = os.Environ()
		for k, v := range c.Env {
			cmd.Env = append(cmd.Env, k+"="+string(v))
		}
		transport = &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}
	} else {
		origin, _ := url.Parse(c.URL)
		client := &http.Client{Transport: headerTransport{c.Headers, origin, ctx}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		if c.Transport == "sse" {
			transport = &mcp.SSEClientTransport{Endpoint: c.URL, HTTPClient: client}
		} else {
			transport = &mcp.StreamableClientTransport{Endpoint: c.URL, HTTPClient: client}
		}
	}
	return mcp.NewClient(&mcp.Implementation{Name: "chatai", Version: "1.0.0"}, nil).Connect(ctx, transport, nil)
}

// Discover is atomic: callers register tools only after every server succeeds.
// Sessions are scoped to discovery/calls so no plugin shutdown hook is required.
func Discover(ctx context.Context, configs []ServerConfig) ([]agent.Tool, error) {
	names := map[string]bool{}
	for _, c := range configs {
		if err := c.validate(); err != nil {
			return nil, err
		}
		if names[c.Name] {
			return nil, fmt.Errorf("duplicate MCP server %q", c.Name)
		}
		names[c.Name] = true
	}
	var tools []agent.Tool
	for _, c := range configs {
		found, err := discover(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("MCP %s discovery: %w", c.Name, err)
		}
		tools = append(tools, found...)
	}
	return tools, nil
}

func discover(ctx context.Context, c ServerConfig) ([]agent.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	session, err := connect(ctx, c)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	allowed := map[string]bool{}
	for _, name := range c.AllowedTools {
		allowed[name] = false
	}
	var tools []agent.Tool
	seen := map[string]bool{}
	for remote, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		if _, ok := allowed[remote.Name]; len(allowed) > 0 && !ok {
			continue
		}
		if len(c.AllowedTools) > 0 {
			allowed[remote.Name] = true
		}
		tool, err := adapt(c, remote)
		if err != nil {
			return nil, err
		}
		if seen[tool.Definition.Function.Name] {
			return nil, fmt.Errorf("duplicate tool %q", remote.Name)
		}
		seen[tool.Definition.Function.Name] = true
		tools = append(tools, tool)
	}
	for name, found := range allowed {
		if !found {
			return nil, fmt.Errorf("allowed tool %q not found", name)
		}
	}
	return tools, nil
}

func adapt(c ServerConfig, t *mcp.Tool) (agent.Tool, error) {
	raw, err := json.Marshal(t.InputSchema)
	if err != nil {
		return agent.Tool{}, err
	}
	var schema map[string]any
	if err = json.Unmarshal(raw, &schema); err != nil || schema == nil {
		return agent.Tool{}, fmt.Errorf("invalid schema for %q", t.Name)
	}
	// Hash the original name to avoid collisions after sanitizing or truncating.
	name := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(t.Name, "_")
	if len(name) > 16 {
		name = name[:16]
	}
	hash := sha256.Sum256([]byte(t.Name))
	local := fmt.Sprintf("mcp_%s_%s_%x", c.Name, name, hash[:6])
	return agent.Tool{
		Definition: model.Tool{Type: "function", Function: model.ToolFunction{Name: local, Description: fmt.Sprintf("[MCP %s / %s] %s", c.Name, t.Name, t.Description), Parameters: schema}},
		Namespace:  "mcp_" + c.Name, Risk: agent.ToolRiskHigh,
		SearchTerms: []string{"mcp", c.Name, t.Name, t.Description},
		Handler: func(rc *agent.RunContext, args json.RawMessage) (any, error) {
			if rc == nil || rc.Context == nil {
				return nil, fmt.Errorf("MCP call requires run context")
			}
			var input map[string]any
			if err := json.Unmarshal(args, &input); err != nil || input == nil {
				return nil, fmt.Errorf("MCP arguments must be a JSON object")
			}
			ctx, cancel := context.WithTimeout(rc.Context, c.timeout())
			defer cancel()
			session, err := connect(ctx, c)
			if err != nil {
				return nil, err
			}
			defer session.Close()
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: t.Name, Arguments: input})
			if err != nil {
				return nil, err
			}
			if result.IsError {
				var messages []string
				for _, content := range result.Content {
					if text, ok := content.(*mcp.TextContent); ok {
						messages = append(messages, text.Text)
					}
				}
				return nil, fmt.Errorf("MCP tool %s failed: %s", t.Name, strings.Join(messages, "\n"))
			}
			return result, nil
		},
	}, nil
}
