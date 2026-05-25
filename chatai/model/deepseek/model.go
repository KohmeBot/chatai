package deepseek

import "github.com/kohmebot/chatai/chatai/model"

type reqBody struct {
	Model          string          `json:"model"`
	Message        []model.Message `json:"messages"`
	Thinking       Option          `json:"thinking,omitempty"`
	MaxTokens      int             `json:"max_tokens"`
	ResponseFormat *Option         `json:"response_format,omitempty"`
}

type Option struct {
	Type string `json:"type"`
}

type respBody struct {
	Error   `json:"error"`
	Choices []Choice `json:"choices"`
	Usage   `json:"usage"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Choice struct {
	Message model.Message `json:"message"`
}

type Usage struct {
	CompletionTokens int64 `json:"completion_tokens"`
	PromptTokens     int64 `json:"prompt_tokens"`
}
