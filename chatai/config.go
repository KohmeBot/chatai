package chatai

import (
	"fmt"
	"github.com/kohmebot/plugin/v2/ui"
)

type Config struct {
	// System 预输入提示词
	System ui.TextArea `yaml:"system" jsonschema:"description=系统提示词"`
	// 使用的模型名称
	ModelName string `yaml:"model_name" jsonschema:"description=使用的模型名称"`
	// 使用的供应商名称
	ProviderName string `yaml:"provider_name" jsonschema:"description=使用的供应商名称,enum=tongyi,enum=deepseek"`
	// 模型供应商配置
	Providers []ModelProviderConfig `yaml:"providers" jsonschema:"description=模型供应商配置"`

	// 最大输出Tokens限制
	MaxTokens int64 `yaml:"max_tokens" jsonschema:"description=最大输出Tokens限制"`
	// 每个人每天的input token上限
	InputToken int64 `yaml:"input_token" jsonschema:"description=每个人每天的input token上限"`
	// 每人每天的output token上限
	OutputToken int64 `yaml:"output_token" jsonschema:"description=每个人每天的output token上限"`
	// 达到上限后的提示词
	LimitTips ui.TextArea `yaml:"limit_tips" jsonschema:"description=达到上限后的提示词"`
	// 触发模型违规后的提示词
	ErrorTips ui.TextArea `yaml:"error_tips" jsonschema:"description=触发模型违规后的提示词"`

	// 控制模型是否联网，如果对应模型支持的话
	Online bool `yaml:"online" jsonschema:"description=模型是否联网，如果对应模型支持的话"`
	// 深度思考，如果对应模型支持的话
	Thinking bool `yaml:"thinking" jsonschema:"description=深度思考，如果对应模型支持的话"`

	// 触发发言欲
	Threshold float64 `yaml:"threshold" jsonschema:"description=触发发言欲,minimum=0,maximum=300"`
	// 允许自动发言的群
	SpeakGroups []int64 `yaml:"speak_groups" jsonschema:"description=允许自动发言的群"`

	JoinGroupConfig `yaml:"join_group" jsonschema:"description=加群配置"`
}

func (c Config) Model() (name string, apiKey string) {
	name = fmt.Sprintf("%s:%s", c.ProviderName, c.ModelName)
	for _, provider := range c.Providers {
		if provider.ProviderName == c.ProviderName {
			return name, string(provider.ApiKey)
		}
	}
	return name, ""
}

type ModelProviderConfig struct {
	ProviderName string    `yaml:"model_name" jsonschema:"description=模型提供商名称,enum=tongyi,enum=deepseek"`
	ApiKey       ui.Secret `yaml:"api_key"`
}

// JoinGroupConfig 加群配置
type JoinGroupConfig struct {
	// 是否开启
	Enable bool `yaml:"enable" jsonschema:"description=是否开启"`
	// 触发语句,用%s来代替新人的NickName
	Trigger ui.Code `yaml:"trigger" jsonschema:"description=触发语句|用%s来代替新人的NickName"`
}
