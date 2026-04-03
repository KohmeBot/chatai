package chatai

type Config struct {
	// Prompt 预输入提示词
	Prompt string `yaml:"prompt"`
	// 使用的模型名称
	ModelName string `yaml:"model_name"`
	// apikey
	ApiKey string `yaml:"api_key"`
	// 最大输出Tokens限制
	MaxTokens int64 `yaml:"max_tokens"`
	// 每个人每天的output token上限
	InputToken int64 `yaml:"input_token"`
	// 每人每天的output token上限
	OutputToken int64 `yaml:"output_token"`
	// 达到上限后的提示词
	LimitTips string `yaml:"limit_tips"`
	// 触发模型违规后的提示词
	ErrorTips string `yaml:"error_tips"`
	// 为qq号指定提示词
	PromptTarget map[int64]string `yaml:"prompt_target"`

	// 控制模型是否联网，如果对应模型支持的话
	Online bool `yaml:"online"`
	// 深度思考，如果对应模型支持的话
	Thinking bool `yaml:"thinking"`

	// 是否开启好感度系统,需要模型支持Json回复
	Favor bool `yaml:"favor"`

	WarmGroupConfig `yaml:"warm_group"`
	JoinGroupConfig `yaml:"join_group"`
	PokeGroupConfig `yaml:"poke_group"`
	OnBootConfig    `yaml:"on_boot"`
}

// WarmGroupConfig 暖群配置
type WarmGroupConfig struct {
	// 是否开启
	Enable bool `yaml:"enable"`
	// 预输入提示词
	Prompt string `yaml:"prompt"`
	// 触发语句,用%d来代替时间(分钟)
	Trigger string `yaml:"trigger"`
	// 冷群间隔(分钟)
	Duration int64 `yaml:"duration"`
	// 开启的群,若为空,则默认为所有群(插件定义内)启用
	Groups []int64 `yaml:"groups"`
	// 禁用时间段(几点到几点)
	DisableTimes []int `yaml:"disable_times"`
}

// JoinGroupConfig 加群配置
type JoinGroupConfig struct {
	// 是否开启
	Enable bool `yaml:"enable"`
	// 预输入提示词
	Prompt string `yaml:"prompt"`
	// 触发语句,用%s来代替新人的NickName
	Trigger string `yaml:"trigger"`
}

type PokeGroupConfig struct {
	// 是否开启
	Enable bool `yaml:"enable"`
	// 预输入提示词
	Prompt string `yaml:"prompt"`
	// 触发语句,用%s来代替NickName
	Trigger string `yaml:"trigger"`
}

// OnBootConfig 启动配置
type OnBootConfig struct {
	// 是否开启
	Enable bool `yaml:"enable"`
	// 预输入提示词
	Prompt string `yaml:"prompt"`
	// 触发语句
	Trigger string `yaml:"trigger"`
}
