package chatai

import (
	"fmt"

	"github.com/kohmebot/plugin/v2/ui"
)

type Config struct {
	System       ui.TextArea           `yaml:"system" jsonschema:"description=Agent 系统提示词"`
	ProviderName string                `yaml:"provider_name" jsonschema:"description=默认供应商,enum=tongyi,enum=deepseek"`
	ModelName    string                `yaml:"model_name" jsonschema:"description=默认模型名称"`
	Providers    []ModelProviderConfig `yaml:"providers" jsonschema:"description=模型供应商配置"`
	Routes       ModelRoutes           `yaml:"routes" jsonschema:"description=按能力选择模型；留空时回退默认模型"`

	MaxTokens   int64       `yaml:"max_tokens" jsonschema:"description=最大输出 Tokens"`
	InputToken  int64       `yaml:"input_token" jsonschema:"description=每人每天 input token 上限"`
	OutputToken int64       `yaml:"output_token" jsonschema:"description=每人每天 output token 上限"`
	LimitTips   ui.TextArea `yaml:"limit_tips" jsonschema:"description=达到限额后的提示"`
	ErrorTips   ui.TextArea `yaml:"error_tips" jsonschema:"description=模型错误提示"`
	Thinking    bool        `yaml:"thinking" jsonschema:"description=默认是否启用深度思考"`

	Agent           AgentConfig      `yaml:"agent" jsonschema:"description=Agent 配置"`
	Repeat          RepeatConfig     `yaml:"repeat" jsonschema:"description=群聊复读配置"`
	Impression      ImpressionConfig `yaml:"impression" jsonschema:"description=Agent 印象工具配置"`
	JoinGroupConfig `yaml:"join_group" jsonschema:"description=加群配置"`
}

type AgentConfig struct {
	MaxSteps               int      `yaml:"max_steps" jsonschema:"description=单次 Agent 最大工具调用轮数,minimum=1,maximum=20"`
	ContextLimit           int      `yaml:"context_limit" jsonschema:"description=上下文工具默认返回的最大消息数,minimum=1,maximum=200"`
	WebBrowserEnable       bool     `yaml:"web_browser_enable" jsonschema:"description=网页搜索和读取是否使用 Chrome 浏览器"`
	WebBrowserAddress      string   `yaml:"web_browser_address" jsonschema:"description=Chrome 远程调试 HTTP 或 WebSocket 地址；留空时启动本机 Chrome"`
	ScheduleMaxSec         int      `yaml:"schedule_max_seconds" jsonschema:"description=定时任务允许的最大延迟秒数"`
	ProgressAfterSeconds   int      `yaml:"progress_after_seconds" jsonschema:"description=Agent 耗时多久后向触发者发送决策进展,minimum=5,maximum=60"`
	ProgressTips           []string `yaml:"progress_tips" jsonschema:"description=Agent 耗时较久时循环发送的固定提示文案"`
	WebSearchPreferSeconds int      `yaml:"web_search_prefer_seconds" jsonschema:"description=搜索 fallback 成功后优先使用该引擎的秒数,minimum=60"`

	UseSearchAPI       bool              `yaml:"use_search_api" jsonschema:"description=是否启用搜索API"`
	SearchProviderName string            `yaml:"search_provider_name" jsonschema:"description=搜索API提供商名称,enum=bocha"`
	SearchProviders    []SearchAPIConfig `yaml:"search_providers" jsonschema:"description=搜索API提供商配置"`
}

type RepeatConfig struct {
	Enable       bool `yaml:"enable" jsonschema:"description=是否启用复读"`
	TriggerCount int  `yaml:"trigger_count" jsonschema:"description=连续相同消息达到多少条时复读,minimum=2,maximum=20"`
}

type ImpressionConfig struct {
	Enable bool `yaml:"enable" jsonschema:"description=是否允许 Agent 主动维护群印象和群友印象"`

	// Deprecated: 仅用于兼容旧配置，周期印象功能已移除。
	LegacyIntervalMinutes int `yaml:"interval_minutes,omitempty" jsonschema:"-"`
	// Deprecated: 仅用于兼容旧配置，周期印象功能已移除。
	LegacyMinMessages int `yaml:"min_messages,omitempty" jsonschema:"-"`
}

type ModelRoutes struct {
	Agent  ModelRouteConfig `yaml:"agent" jsonschema:"description=Agent 决策模型"`
	Vision ModelRouteConfig `yaml:"vision" jsonschema:"description=图片解析模型；不配置即不解析"`
	// Deprecated: 仅用于兼容旧配置，印象现在由 Agent 模型通过工具维护。
	LegacyImpression ModelRouteConfig `yaml:"impression,omitempty" jsonschema:"-"`
	Join             ModelRouteConfig `yaml:"join" jsonschema:"description=入群欢迎模型"`
}

type ModelRouteConfig struct {
	ProviderName string `yaml:"provider_name" jsonschema:"description=供应商名称,enum=tongyi,enum=deepseek"`
	ModelName    string `yaml:"model_name" jsonschema:"description=模型名称"`
	Thinking     *bool  `yaml:"thinking,omitempty" jsonschema:"description=是否启用深度思考"`
	MaxTokens    int64  `yaml:"max_tokens,omitempty" jsonschema:"description=该路由最大输出 Tokens"`
}

func (r ModelRouteConfig) Configured() bool { return r.ProviderName != "" && r.ModelName != "" }

type ModelProviderConfig struct {
	ProviderName string    `yaml:"provider_name" jsonschema:"description=模型提供商名称,enum=tongyi,enum=deepseek"`
	LegacyName   string    `yaml:"model_name,omitempty" jsonschema:"-"`
	ApiKey       ui.Secret `yaml:"api_key"`
}

func (c Config) Model() (string, string) {
	return c.modelFor(ModelRouteConfig{ProviderName: c.ProviderName, ModelName: c.ModelName})
}

func (c Config) modelFor(route ModelRouteConfig) (string, string) {
	provider, name := route.ProviderName, route.ModelName
	if provider == "" || name == "" {
		provider, name = c.ProviderName, c.ModelName
	}
	for _, item := range c.Providers {
		itemProvider := item.ProviderName
		if itemProvider == "" {
			itemProvider = item.LegacyName
		}
		if itemProvider == provider {
			return fmt.Sprintf("%s:%s", provider, name), string(item.ApiKey)
		}
	}
	return fmt.Sprintf("%s:%s", provider, name), ""
}

type JoinGroupConfig struct {
	Enable  bool    `yaml:"enable" jsonschema:"description=是否开启"`
	Trigger ui.Code `yaml:"trigger" jsonschema:"description=触发语句|用%s代替新人昵称"`
}

type SearchAPIConfig struct {
	Name   string    `yaml:"name" jsonschema:"description=搜索API提供商名称,enum=bocha"`
	ApiKey ui.Secret `yaml:"api_key"`
}
