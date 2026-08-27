package tongyi

import "github.com/kohmebot/chatai/chatai/model"

type reqBody struct {
	Model          string          `json:"model"`
	Message        []model.Message `json:"messages"`
	EnableSearch   bool            `json:"enable_search"`
	EnableThinking bool            `json:"enable_thinking"`
	MaxTokens      int             `json:"max_tokens"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Tools          []model.Tool    `json:"tools,omitempty"`
}

type ResponseFormat struct {
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

type imageReqBody struct {
	Model      string          `json:"model"`
	Input      imageInput      `json:"input"`
	Parameters imageParameters `json:"parameters"`
}

type imageInput struct {
	Messages []imageMessage `json:"messages"`
}

type imageMessage struct {
	Role    string              `json:"role"`
	Content []model.ContentPart `json:"content"`
}

type imageParameters struct {
	PromptExtend   bool `json:"prompt_extend"`
	EnableThinking bool `json:"enable_thinking"`
}

type imageRespBody struct {
	Output    imageOutput `json:"output"`
	Usage     imageUsage  `json:"usage"`
	RequestID string      `json:"request_id"`
	Code      string      `json:"code"`
	Message   string      `json:"message"`
}

type imageOutput struct {
	Choices []imageChoice `json:"choices"`
}

type imageChoice struct {
	Message imageMessage `json:"message"`
}

type imageUsage struct {
	InputImageCount  int `json:"input_image_count"`
	OutputImageCount int `json:"output_image_count"`
}
