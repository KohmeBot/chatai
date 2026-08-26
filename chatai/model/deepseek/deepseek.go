package deepseek

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

type deepSeekModel struct {
	model.Config

	apiKeyHeader string
	systemMsg    model.Message
	client       *http.Client

	responseJson bool
}

func NewDeepSeekModel(conf model.Config) model.LargeModel {
	return &deepSeekModel{
		Config:       conf,
		apiKeyHeader: "Bearer " + conf.ApiKey,
		systemMsg: model.Message{
			Role:    "system",
			Content: conf.System,
		},
		responseJson: conf.ResponseJson,
		client:       &http.Client{Timeout: 90 * time.Second},
	}
}

func (m *deepSeekModel) Request(request *model.Request, response *model.Response) error {

	msg := make([]model.Message, 0, len(request.History)+2)
	msg = append(msg, m.systemMsg)
	msg = append(msg, request.History...)
	content := any(request.Question)
	if request.ImageURL != "" {
		content = []model.ContentPart{{Type: "text", Text: request.Question}, {Type: "image_url", ImageURL: &model.ImageURL{URL: request.ImageURL}}}
	}
	if content != nil {
		msg = append(msg, model.Message{Role: "user", Content: content})
	}

	requestBody := reqBody{
		Model:     m.Name,
		Message:   msg,
		MaxTokens: int(m.MaxTokens),
		Thinking:  Option{Type: "disabled"},
		Tools:     request.Tools,
	}

	if m.Thinking {
		requestBody.Thinking = Option{Type: "enabled"}
	}

	if m.responseJson {
		requestBody.ResponseFormat = &Option{Type: "json_object"}
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}

	logrus.Infof("model request provider=deepseek model=%s messages=%d tools=%d image=%t", m.Name, len(msg), len(request.Tools), request.ImageURL != "")

	requestContext := request.Context
	if requestContext == nil {
		requestContext = context.Background()
	}
	req, err := http.NewRequestWithContext(requestContext, "POST", "https://api.deepseek.com/chat/completions", bytes.NewBuffer(jsonData))
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
	logrus.Infof("model response provider=deepseek model=%s status=%d bytes=%d", m.Name, resp.StatusCode, len(buf))
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
