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

	// 控制模型是否联网，如果对应模型支持的话
	Online bool `mapstructure:"online"`
}
