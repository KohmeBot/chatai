package model

import "gorm.io/gorm"

type Key struct {
	GroupId int64
	UserId  int64
}

type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	// ReasoningContent 仅承载供应商在响应中明确返回的思考内容。
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Request struct {
	// 输入的问题
	Question string
	// 历史记录
	History []Message
	// Tools 是本轮可由模型选择调用的函数。
	Tools []Tool
	// ImageURL 非空时以多模态消息附在 Question 后。
	ImageURL string
}

type Response struct {
	// 返回的结果
	Answer string
	// 本次调用的输入token数量
	InputToken int64
	// 本次调用的输出token数量
	OutToken int64
	// 错误信息
	ErrorMsg  string
	ToolCalls []ToolCall
	Reasoning string
}

func Text(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	return ""
}

type LargeModel interface {
	Request(request *Request, response *Response) error
}

type Config struct {
	Name         string
	ApiKey       string
	System       string
	Online       bool
	MaxTokens    int64
	Thinking     bool
	ResponseJson bool

	DB *gorm.DB
}
