package chatai

import (
	"encoding/json"
	"testing"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/plugin/v2"
	"github.com/stretchr/testify/require"
	zero "github.com/wdvxdr1123/ZeroBot"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type privateChatTestEngine struct {
	*zero.Engine
	messageMatchers []*zero.Matcher
}

func (e *privateChatTestEngine) OnMessage(rules ...zero.Rule) *zero.Matcher {
	matcher := e.Engine.OnMessage(rules...)
	e.messageMatchers = append(e.messageMatchers, matcher)
	return matcher
}

type privateChatTestEnv struct {
	plugin.Env
	groups plugin.Groups
}

func (e privateChatTestEnv) Groups() plugin.Groups { return e.groups }

type privateChatTestGroups struct{ plugin.Groups }

func (privateChatTestGroups) Rule() zero.Rule {
	return func(ctx *zero.Ctx) bool { return ctx.Event.GroupID == 200 }
}

func TestPrivateMessageRouting(t *testing.T) {
	var conf Config
	if err := yaml.Unmarshal([]byte("private_chat:\n  enable: true\n  allowed_users: [100]\n"), &conf); err != nil {
		t.Fatal(err)
	}
	engine := &privateChatTestEngine{Engine: zero.New()}
	t.Cleanup(engine.Delete)
	c := &ChatPlugin{conf: conf, env: privateChatTestEnv{groups: privateChatTestGroups{}}}
	c.SetOnMessage(engine)
	c.setOnPrivateMessage(engine, persona.Options{})
	groupMatcher, privateMatcher := engine.messageMatchers[0], engine.messageMatchers[1]
	if privateMatcher.Priority <= groupMatcher.Priority || privateMatcher.Block {
		t.Fatal("private chat must run after normal matchers without blocking")
	}
	matches := func(m *zero.Matcher, event *zero.Event) bool {
		ctx := &zero.Ctx{Event: event}
		for _, rule := range m.Rules {
			if !rule(ctx) {
				return false
			}
		}
		return true
	}
	for _, tt := range []struct {
		name, detail           string
		botID, userID, groupID int64
		wantGroup, wantPrivate bool
	}{
		{"allowlisted", "private", 42, 100, 0, false, true},
		{"temporary private", "private", 42, 100, 200, false, true},
		{"unlisted private", "private", 42, 101, 200, false, false},
		{"group", "group", 42, 100, 200, true, false},
		{"self message", "private", 100, 100, 0, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			event := &zero.Event{PostType: "message", DetailType: tt.detail, SelfID: tt.botID, UserID: tt.userID, GroupID: tt.groupID}
			if group, private := matches(groupMatcher, event), matches(privateMatcher, event); group != tt.wantGroup || private != tt.wantPrivate {
				t.Fatalf("matched group=%v private=%v, want group=%v private=%v", group, private, tt.wantGroup, tt.wantPrivate)
			}
		})
	}
}

func TestPrivateChatRequiresOptInAndAllowlist(t *testing.T) {
	for _, config := range []PrivateChatConfig{
		{},
		{AllowedUsers: []int64{100}},
		{Enable: true},
	} {
		if config.allows(42, 100) {
			t.Fatalf("private chat allowed without explicit opt-in and allowlist: %+v", config)
		}
	}
}

func TestAgentToolsShareCatalogAcrossGroupsAndPrivateSessions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&agent.ToolPreference{}))
	prefs, err := agent.LoadToolPreferences(db)
	require.NoError(t, err)
	tool := func(name string) agent.Tool {
		return agent.Tool{
			Definition: agent.Function(name, name, nil),
			Handler:    func(*agent.RunContext, json.RawMessage) (any, error) { return true, nil },
		}
	}
	c := &ChatPlugin{db: db, toolPreferences: prefs}
	require.NoError(t, c.RegisterAgentTool(tool("plugin_example")))
	opts := persona.Options{ToolPreferences: prefs}
	require.NoError(t, c.initializeToolCatalog(opts, []agent.Tool{tool("mcp_example")}))
	group := c.newAgentPersona(200, nil, opts)
	engine := &privateChatTestEngine{Engine: zero.New()}
	t.Cleanup(engine.Delete)
	lookup := c.setOnPrivateMessage(engine, opts)
	private := lookup(persona.PrivateSession{BotID: 42, UserID: 100})

	require.NoError(t, c.RegisterAgentTool(tool("late_example")))
	require.Error(t, c.RegisterAgentTool(tool("read_private_context")))
	privateTool := tool("private_example")
	privateTool.Scope = agent.ToolScopePrivate
	require.NoError(t, c.RegisterAgentTool(privateTool))
	require.Contains(t, c.toolCatalog.Names(), "private_example")
	require.Contains(t, private.ToolNames(), "private_example")
	require.NotContains(t, group.ToolNames(), "private_example")
	for _, names := range [][]string{group.ToolNames(), private.ToolNames(), c.toolCatalog.Names()} {
		require.Subset(t, names, []string{"plugin_example", "mcp_example", "late_example"})
	}
	require.Subset(t, c.toolCatalog.Names(), []string{"read_group_context", "read_private_context"})
	require.Subset(t, c.toolCatalog.NamesInScope(agent.ToolScopeGroup), []string{"read_group_context", "read_user_memory", "send_message"})
	require.NotContains(t, c.toolCatalog.NamesInScope(agent.ToolScopeGroup), "read_private_context")
	require.Subset(t, c.toolCatalog.NamesInScope(agent.ToolScopePrivate), []string{"read_group_context", "read_private_context", "read_user_memory", "send_message"})
	require.NoError(t, c.setToolDisabled("mcp_example", true))
	later := lookup(persona.PrivateSession{BotID: 42, UserID: 101})
	otherGroup := c.newAgentPersona(201, nil, opts)
	for _, p := range []*persona.Persona{group, otherGroup, private, later} {
		require.NotContains(t, p.ToolNames(), "mcp_example")
	}
	require.NoError(t, c.setToolDisabled("mcp_example", false))
	for _, p := range []*persona.Persona{group, otherGroup, private, later} {
		require.Contains(t, p.ToolNames(), "mcp_example")
	}
}
