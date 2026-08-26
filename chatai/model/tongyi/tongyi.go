package tongyi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
)

type tongYiModel struct {
	model.Config

	apiKeyHeader string
	systemMsg    model.Message
	client       *http.Client
}

func NewTongYiModel(conf model.Config) model.LargeModel {
	return &tongYiModel{
		Config:       conf,
		apiKeyHeader: "Bearer " + conf.ApiKey,
		systemMsg: model.Message{
			Role:    "system",
			Content: conf.System,
		},
		client: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (m *tongYiModel) Request(request *model.Request, response *model.Response) error {

	msg := make([]model.Message, 0, len(request.History)+2)
	if m.systemMsg.Content != "" {
		msg = append(msg, m.systemMsg)
	}
	msg = append(msg, request.History...)
	content := any(request.Question)
	if request.ImageURL != "" {
		content = []model.ContentPart{{Type: "text", Text: request.Question}, {Type: "image_url", ImageURL: &model.ImageURL{URL: request.ImageURL}}}
	}
	if request.Content != nil {
		content = request.Content
	}

	if content != nil {
		msg = append(msg, model.Message{Role: "user", Content: content})
	}

	tools := request.Tools
	if m.Online {
		// Agent 的联网能力由 browse_web 工具统一提供，避免供应商私有工具
		// 与标准 function calling 混用时产生不兼容结果。
	}

	requestBody := reqBody{
		Model:          m.Name,
		Message:        msg,
		EnableSearch:   m.Online,
		EnableThinking: m.Thinking,
		MaxTokens:      int(m.MaxTokens),
		Tools:          tools,
	}
	if m.ResponseJson {
		requestBody.ResponseFormat = &ResponseFormat{Type: "json_object"}
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}

	logrus.Infof("model request provider=tongyi model=%s messages=%d tools=%d image=%t", m.Name, len(msg), len(tools), request.ImageURL != "")

	requestContext := request.Context
	if requestContext == nil {
		requestContext = context.Background()
	}
	req, err := http.NewRequestWithContext(requestContext, "POST", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", m.apiKeyHeader)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	responseBody := respBody{}
	err = json.Unmarshal(buf, &responseBody)
	logrus.Infof("model response provider=tongyi model=%s status=%d bytes=%d", m.Name, resp.StatusCode, len(buf))
	if err != nil {
		return err
	}
	if responseBody.Error.Code != "" {
		response.ErrorMsg = responseBody.Error.Message
		return nil
	}
	if len(responseBody.Choices) == 0 {
		return io.ErrUnexpectedEOF
	}
	response.Answer = model.Text(responseBody.Choices[0].Message.Content)
	response.ToolCalls = responseBody.Choices[0].Message.ToolCalls
	response.Reasoning = responseBody.Choices[0].Message.ReasoningContent
	response.InputToken = responseBody.PromptTokens
	response.OutToken = responseBody.CompletionTokens
	response.Content = responseBody.Choices[0].Message.Content

	return nil
}
