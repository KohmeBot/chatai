package chatai

type Config struct {
	// System 预输入提示词
	System string `yaml:"system"`
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

	// 控制模型是否联网，如果对应模型支持的话
	Online bool `yaml:"online"`
	// 深度思考，如果对应模型支持的话
	Thinking bool `yaml:"thinking"`

	// 触发发言欲
	Threshold float64 `yaml:"threshold"`

	JoinGroupConfig `yaml:"join_group"`
}

// JoinGroupConfig 加群配置
type JoinGroupConfig struct {
	// 是否开启
	Enable bool `yaml:"enable"`
	// 触发语句,用%s来代替新人的NickName
	Trigger string `yaml:"trigger"`
}
