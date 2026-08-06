package tongyi

import (
	"bytes"
	"encoding/json"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"time"
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
		client: &http.Client{Timeout: 90 * time.Second},
	}
}

func (m *tongYiModel) Request(request *model.Request, response *model.Response) error {

	msg := make([]model.Message, 0, len(request.History)+2)
	msg = append(msg, m.systemMsg)
	msg = append(msg, request.History...)
	content := any(request.Question)
	if request.ImageURL != "" {
		content = []model.ContentPart{{Type: "text", Text: request.Question}, {Type: "image_url", ImageURL: &model.ImageURL{URL: request.ImageURL}}}
	}
	if request.Question != "" || request.ImageURL != "" {
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

	logrus.Infof("do request: %s", string(jsonData))

	req, err := http.NewRequest("POST", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", bytes.NewBuffer(jsonData))
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
	logrus.Infof("get response: %s", string(buf))
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

	return nil
}
