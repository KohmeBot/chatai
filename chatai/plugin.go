package chatai

import (
	"errors"
	"fmt"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/favor"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/model/factory"
	"github.com/kohmebot/chatai/chatai/persona"
	"github.com/kohmebot/chatai/chatai/pkg/search"
	factory2 "github.com/kohmebot/chatai/chatai/pkg/search/factory"
	"github.com/kohmebot/chatai/chatai/skill"
	"github.com/kohmebot/plugin/v2"
	"github.com/sirupsen/logrus"
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
	return c.NewProviderModel(c.conf.ProviderName, c.conf.ModelName, system, online, thinking, responseJSON)
}

// NewProviderModel 使用指定的供应商和模型名创建供外部插件调用的模型。
// API Key 从 providers 中对应的供应商配置读取。
func (c *ChatPlugin) NewProviderModel(providerName, modelName, system string, online, thinking, responseJSON bool) model.LargeModel {
	name, key := c.conf.modelFor(ModelRouteConfig{ProviderName: providerName, ModelName: modelName})
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
	tables := []any{
		&UsageRecord{}, &favor.FavorRecord{}, &persona.UserImpression{}, &persona.GroupImpression{},
		&persona.ChatMessageRecord{}, &model.TokenUsage{},
	}
	tables = append(tables, skill.Models()...)
	for _, table := range tables {
		if err := db.AutoMigrate(table); err != nil {
			return err
		}
	}
	if c.conf.Agent.MaxSteps <= 0 {
		c.conf.Agent.MaxSteps = 8
	}
	if c.conf.Agent.MaxToolCalls <= 0 {
		c.conf.Agent.MaxToolCalls = 16
	}
	if c.conf.Agent.RunTimeoutSeconds <= 0 {
		c.conf.Agent.RunTimeoutSeconds = 180
	}
	if c.conf.Agent.ProgressAfterSeconds <= 0 {
		c.conf.Agent.ProgressAfterSeconds = 15
	}
	if c.conf.Agent.WebSearchPreferSeconds <= 0 {
		c.conf.Agent.WebSearchPreferSeconds = 3600
	}
	if c.conf.Skills.CandidateMinSteps <= 0 {
		c.conf.Skills.CandidateMinSteps = 4
	}
	if c.conf.Skills.ActivationEvidence < 2 {
		c.conf.Skills.ActivationEvidence = 3
	}
	if c.conf.Skills.GlobalMinGroups < 1 {
		c.conf.Skills.GlobalMinGroups = 2
	}
	if c.conf.Skills.DefaultTTLDays <= 0 {
		c.conf.Skills.DefaultTTLDays = 30
	}
	if c.conf.Skills.MaxActive <= 0 {
		c.conf.Skills.MaxActive = 20
	}
	if c.conf.Skills.MaxCandidates <= 0 {
		c.conf.Skills.MaxCandidates = 50
	}
	if c.conf.Skills.ReflectionWorkers <= 0 {
		c.conf.Skills.ReflectionWorkers = 1
	}
	if c.conf.Repeat.TriggerCount < 2 {
		c.conf.Repeat.TriggerCount = 3
	}
	var skillService *skill.Service
	if c.conf.Skills.Enable {
		skillRoute := c.conf.Routes.Skill
		if !skillRoute.Configured() {
			skillRoute = c.conf.Routes.Agent
		}
		reflectionModel := c.routeModel(skillRoute, skill.ReflectionSystemPrompt(), true)
		skillService = skill.NewService(db, reflectionModel, skill.Options{
			Enabled: true, AutoActivateGenerated: c.conf.Skills.AutoActivateGenerated,
			CandidateMinSteps:  c.conf.Skills.CandidateMinSteps,
			ActivationEvidence: c.conf.Skills.ActivationEvidence, GlobalMinGroups: c.conf.Skills.GlobalMinGroups,
			DefaultTTLDays: c.conf.Skills.DefaultTTLDays,
			MaxActive:      c.conf.Skills.MaxActive, MaxCandidates: c.conf.Skills.MaxCandidates,
			ReflectionWorkers: c.conf.Skills.ReflectionWorkers,
		})
		definitions := make([]skill.Definition, 0, len(c.conf.Skills.Global))
		for _, item := range c.conf.Skills.Global {
			definitions = append(definitions, skill.Definition{
				Name: item.Name, Description: item.Description, Triggers: item.Triggers, NonTriggers: item.NonTriggers,
				Markdown: string(item.Markdown), LegacyInstructions: item.LegacyInstructions,
				LegacyRequiredTools: item.LegacyRequiredTools, LegacySuccessChecks: item.LegacySuccessChecks,
			})
		}
		if err := skillService.Initialize(definitions); err != nil {
			return fmt.Errorf("initialize global skills: %w", err)
		}
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
			MaxToolCalls:     c.conf.Agent.MaxToolCalls,
			RunTimeout:       time.Duration(c.conf.Agent.RunTimeoutSeconds) * time.Second,
			ContextLimit:     c.conf.Agent.ContextLimit,
			WebBrowserEnable: c.conf.Agent.WebBrowserEnable, WebBrowserAddress: c.conf.Agent.WebBrowserAddress,
			ScheduleMaxSec: c.conf.Agent.ScheduleMaxSec, ProgressAfter: time.Duration(c.conf.Agent.ProgressAfterSeconds) * time.Second,
			ProgressTips: c.conf.Agent.ProgressTips, WebSearchPrefer: time.Duration(c.conf.Agent.WebSearchPreferSeconds) * time.Second,
			RepeatEnable: c.conf.Repeat.Enable,
			RepeatCount:  c.conf.Repeat.TriggerCount, ImpressionUpdateEnable: c.conf.Impression.Enable,
			ExtraTools: c.extraTools,
			Skills:     skillService,
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
func (c *ChatPlugin) Version() string      { return "v1.2.10" }
