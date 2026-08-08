package chatai

import (
	"errors"
	"github.com/kohmebot/chatai/chatai/pkg/search"
	factory2 "github.com/kohmebot/chatai/chatai/pkg/search/factory"
	"github.com/sirupsen/logrus"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/favor"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/model/factory"
	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/plugin/v2"
	zero "github.com/wdvxdr1123/ZeroBot"
	"gorm.io/gorm"
)

type ChatPlugin struct {
	conf Config
	env  plugin.Env

	joinGroupModel model.LargeModel
	otherModel     model.LargeModel
	personaMap     map[int64]*persona.Persona
	extraTools     []agent.Tool
	db             *gorm.DB
}

func NewPlugin() plugin.Plugin         { return &ChatPlugin{personaMap: make(map[int64]*persona.Persona)} }
func (c *ChatPlugin) ConfigModel() any { return new(Config) }

func (c *ChatPlugin) DoRequest(req string) (string, error) {
	return c.DoRequestWithModel(req, c.otherModel)
}
func (c *ChatPlugin) DoRequestWithModel(req string, m model.LargeModel) (string, error) {
	response := new(model.Response)
	if err := m.Request(&model.Request{Question: req}, response); err != nil {
		return "", err
	}
	if response.ErrorMsg != "" {
		return "", errors.New(response.ErrorMsg)
	}
	return response.Answer, nil
}

// 兼容旧 SDK：未指定路由时仍可直接创建默认模型。
func (c *ChatPlugin) NewModel(system string, online, thinking, responseJSON bool) model.LargeModel {
	name, key := c.conf.Model()
	return factory.NewLargeModel(model.Config{Name: name, ApiKey: key, System: system, Online: online, MaxTokens: c.conf.MaxTokens, Thinking: thinking, ResponseJson: responseJSON, DB: c.db})
}
func (c *ChatPlugin) NewDefaultModel(online, thinking, responseJSON bool) model.LargeModel {
	return c.NewModel(string(c.conf.System), online, thinking, responseJSON)
}

// RegisterAgentTool 是稳定的工具扩展入口，其他插件可在初始化阶段注册自定义工具。
func (c *ChatPlugin) RegisterAgentTool(tool agent.Tool) error {
	for _, p := range c.personaMap {
		if err := p.RegisterTool(tool); err != nil {
			return err
		}
	}
	c.extraTools = append(c.extraTools, tool)
	return nil
}

func (c *ChatPlugin) routeModel(route ModelRouteConfig, system string, responseJSON bool) model.LargeModel {
	name, key := c.conf.modelFor(route)
	thinking := c.conf.Thinking
	if route.Thinking != nil {
		thinking = *route.Thinking
	}
	maxTokens := c.conf.MaxTokens
	if route.MaxTokens > 0 {
		maxTokens = route.MaxTokens
	}
	return factory.NewLargeModel(model.Config{Name: name, ApiKey: key, System: system, MaxTokens: maxTokens, Thinking: thinking, ResponseJson: responseJSON, DB: c.db})
}

func (c *ChatPlugin) OnInit(engine plugin.Engine, env plugin.Env) error {
	c.env = env
	if err := env.GetConf(&c.conf); err != nil {
		return err
	}
	db, err := env.GetDB()
	if err != nil {
		return err
	}
	c.db = db
	for _, table := range []any{
		&UsageRecord{}, &favor.FavorRecord{}, &persona.UserImpression{}, &persona.GroupImpression{},
		&persona.ChatMessageRecord{}, &model.TokenUsage{},
	} {
		if err := db.AutoMigrate(table); err != nil {
			return err
		}
	}
	if c.conf.Agent.MaxSteps <= 0 {
		c.conf.Agent.MaxSteps = 8
	}
	if c.conf.Agent.ProgressAfterSeconds <= 0 {
		c.conf.Agent.ProgressAfterSeconds = 15
	}
	if c.conf.Agent.WebSearchPreferSeconds <= 0 {
		c.conf.Agent.WebSearchPreferSeconds = 3600
	}
	if c.conf.Repeat.TriggerCount < 2 {
		c.conf.Repeat.TriggerCount = 3
	}
	c.personaMap = make(map[int64]*persona.Persona)
	for group := range env.Groups().RangeGroup() {
		var vision model.LargeModel
		if c.conf.Routes.Vision.Configured() {
			// 带图事件由视觉模型直接完成整轮 Agent 决策，因此它需要与文本
			// Agent 相同的人设、上下文规则和工具调用说明。
			vision = c.routeModel(c.conf.Routes.Vision, string(c.conf.System)+"\n"+persona.AgentRules(), false)
		}
		var searcher search.Searcher
		if c.conf.Agent.UseSearchAPI {
			for _, p := range c.conf.Agent.SearchProviders {
				if p.Name == c.conf.Agent.SearchProviderName {
					searcher, err = factory2.NewSearcher(p.Name, string(p.ApiKey))
					if err != nil {
						logrus.Errorf("Failed to create searcher: %v", err)
						searcher = nil
					}
					break
				}
			}

		}

		c.personaMap[group] = persona.NewPersona(group, env, db, persona.Options{
			AgentModel:  c.routeModel(c.conf.Routes.Agent, string(c.conf.System)+"\n"+persona.AgentRules(), false),
			VisionModel: vision, MaxSteps: c.conf.Agent.MaxSteps,
			ContextLimit: c.conf.Agent.ContextLimit, WebMaxBytes: c.conf.Agent.WebMaxBytes,
			WebBrowserEnable: c.conf.Agent.WebBrowserEnable, WebBrowserAddress: c.conf.Agent.WebBrowserAddress,
			ScheduleMaxSec: c.conf.Agent.ScheduleMaxSec, ProgressAfter: time.Duration(c.conf.Agent.ProgressAfterSeconds) * time.Second,
			ProgressTips: c.conf.Agent.ProgressTips, WebSearchPrefer: time.Duration(c.conf.Agent.WebSearchPreferSeconds) * time.Second,
			RepeatEnable: c.conf.Repeat.Enable,
			RepeatCount:  c.conf.Repeat.TriggerCount, ImpressionUpdateEnable: c.conf.Impression.Enable,
			ExtraTools: c.extraTools,
			SearchAPI:  searcher,
		})
	}
	c.joinGroupModel = c.routeModel(c.conf.Routes.Join, string(c.conf.System), false)
	c.otherModel = c.routeModel(c.conf.Routes.Agent, string(c.conf.System), false)
	c.SetOnMessage(engine)
	c.SetOnJoinGroup(engine)
	c.SetOnUsage(engine)
	return nil
}

func (c *ChatPlugin) OnBoot()              {}
func (c *ChatPlugin) OnHelp(ctx *zero.Ctx) {}
func (c *ChatPlugin) Name() string         { return "chatai" }
func (c *ChatPlugin) Version() string      { return "v1.0.21" }
