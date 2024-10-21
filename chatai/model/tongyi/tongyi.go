package tongyi

import (
	"bytes"
	"chatai/chatai/model"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type tongYiModel struct {
	// 使用的通义模型名称
	tongYiModelName string
	// apikey
	apikey string
	// 预输入设定
	system string

	online bool

	apiKeyHeader string
	systemMsg    model.Message
	client       *http.Client
}

func NewTongYiModel(name string, apikey string, system string, online bool) model.LargeModel {
	return &tongYiModel{
		tongYiModelName: name,
		apikey:          apikey,
		system:          system,
		online:          online,
		apiKeyHeader:    "Bearer " + apikey,
		systemMsg: model.Message{
			Role:    "system",
			Content: system,
		},
		client: &http.Client{},
	}
}

func (m *tongYiModel) Request(Request *model.Request, response *model.Response) error {

	msg := make([]model.Message, len(Request.History)+1)
	copy(msg[1:], Request.History)
	msg[0] = m.systemMsg
	requestBody := reqBody{
		Model:   m.tongYiModelName,
		Message: msg,
	}
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
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
	if err != nil {
		return err
	}
	if responseBody.FinishReason != "stop" {
		return fmt.Errorf("model unexpoect finish: %s", responseBody.FinishReason)
	}
	response.Answer = responseBody.Choices[0].Message[0].Content
	response.InputToken = responseBody.PromptTokens
	response.OutToken = responseBody.CompletionTokens

	return nil
}
