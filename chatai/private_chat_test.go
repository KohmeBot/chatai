package chatai

import (
	"testing"

	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/plugin/v2"
	zero "github.com/wdvxdr1123/ZeroBot"
	"gopkg.in/yaml.v3"
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
