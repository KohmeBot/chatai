package deepseek

import (
	"bytes"
	"encoding/json"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
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
		client:       &http.Client{},
	}
}

func (m *deepSeekModel) Request(request *model.Request, response *model.Response) error {

	msg := make([]model.Message, len(request.History)+2)
	copy(msg[2:], request.History)
	msg[0] = m.systemMsg
	msg[1] = model.Message{
		Role:    "user",
		Content: request.Question,
	}

	requestBody := reqBody{
		Model:     m.Name,
		Message:   msg,
		MaxTokens: int(m.MaxTokens),
	}

	if m.Thinking {
		requestBody.Thinking = &Option{Type: "enabled"}
	}

	if m.responseJson {
		requestBody.ResponseFormat = &Option{Type: "json_object"}
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}

	logrus.Infof("do request: %s", string(jsonData))

	req, err := http.NewRequest("POST", "https://api.deepseek.com", bytes.NewBuffer(jsonData))
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
