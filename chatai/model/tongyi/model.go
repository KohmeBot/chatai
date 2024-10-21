package tongyi

import "github.com/kohmebot/chatai/chatai/model"

type reqBody struct {
	Model        string          `json:"model"`
	Message      []model.Message `json:"messages"`
	EnableSearch bool            `json:"enable_search"`
}

type respBody struct {
	FinishReason string   `json:"finish_reason"`
	Choices      []choice `json:"choices"`
	usage        `json:"usage"`
}

type choice struct {
	Message []model.Message `json:"message"`
}

type usage struct {
	CompletionTokens int64 `json:"completion_tokens"`
	PromptTokens     int64 `json:"prompt_tokens"`
}
