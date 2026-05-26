package tongyi

import (
	"bytes"
	"encoding/json"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
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
		client: &http.Client{},
	}
}

func (m *tongYiModel) Request(request *model.Request, response *model.Response) error {

	msg := make([]model.Message, 0, len(request.History)+2)
	msg = append(msg, m.systemMsg)
	msg = append(msg, request.History...)
	msg = append(msg, model.Message{
		Role:    "user",
		Content: request.Question,
	})

	var tools []Tool
	if m.Online {
		tools = append(tools,
			Tool{Type: "web_search"},
			Tool{Type: "web_extractor"},
			Tool{Type: "code_interpreter"},
		)
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
	response.Answer = responseBody.Choices[0].Message.Content
	response.InputToken = responseBody.PromptTokens
	response.OutToken = responseBody.CompletionTokens

	return nil
}
