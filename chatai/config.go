package chatai

type Config struct {
	// Prompt 预输入提示词
	Prompt string `mapstructure:"prompt"`
	// 使用的模型名称
	ModelName string `mapstructure:"model_name"`
	// apikey
	ApiKey string `mapstructure:"api_key"`

	// 每个人每天的output token上限
	InputToken int64 `mapstructure:"input_token"`
	// 每人每天的output token上限
	OutputToken int64 `mapstructure:"output_token"`
	// 达到上限后的提示词
	LimitTips string `mapstructure:"limit_tips"`
	// 触发模型违规后的提示词
	ErrorTips string `mapstructure:"error_tips"`

	// 控制模型是否联网，如果对应模型支持的话
	Online bool `mapstructure:"online"`

	WarmGroupConfig `mapstructure:"warm_group_config"`
	JoinGroupConfig `mapstructure:"join_group_config"`
}

// WarmGroupConfig 暖群配置
type WarmGroupConfig struct {
	// 是否开启
	Enable bool `mapstructure:"enable"`
	// 预输入提示词
	Prompt string `mapstructure:"prompt"`
	// 触发语句,用%d来代替时间(分钟)
	Trigger string `mapstructure:"trigger"`
	// 冷群间隔(分钟)
	Duration int64 `mapstructure:"duration"`
	// 开启的群,若为空,则默认为所有群(插件定义内)启用
	Groups []int64 `mapstructure:"groups"`
}

// JoinGroupConfig 加群配置
type JoinGroupConfig struct {
	// 是否开启
	Enable bool `mapstructure:"enable"`
	// 预输入提示词
	Prompt string `mapstructure:"prompt"`
	// 触发语句,用%s来代替新人的NickName
	Trigger string `mapstructure:"trigger"`
}
