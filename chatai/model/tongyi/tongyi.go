package tongyi

import (
	"bytes"
	"encoding/json"
	"github.com/kohmebot/chatai/chatai/model"
	"io"
	"net/http"
	"strings"
)

type tongYiModel struct {
	// 使用的通义模型名称
	tongYiModelName string
	// apikey
	apikey string
	// 预输入设定
	system string
	// 最大token
	maxTokens int
	online    bool

	apiKeyHeader string
	systemMsg    model.Message
	client       *http.Client
}

func NewTongYiModel(name string, apikey string, system string, online bool, maxTokens int64) model.LargeModel {
	name = strings.TrimPrefix(name, "tongyi:")
	return &tongYiModel{
		tongYiModelName: name,
		apikey:          apikey,
		system:          system,
		online:          online,
		maxTokens:       int(maxTokens),
		apiKeyHeader:    "Bearer " + apikey,
		systemMsg: model.Message{
			Role:    "system",
			Content: system,
		},
		client: &http.Client{},
	}
}

func (m *tongYiModel) Request(request *model.Request, response *model.Response) error {

	msg := make([]model.Message, len(request.History)+2)
	copy(msg[2:], request.History)
	msg[0] = m.systemMsg
	msg[1] = model.Message{
		Role:    "user",
		Content: request.Question,
	}
	requestBody := reqBody{
		Model:        m.tongYiModelName,
		Message:      msg,
		EnableSearch: m.online,
		MaxTokens:    m.maxTokens,
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
	if responseBody.Error.Code != "" {
		response.ErrorMsg = responseBody.Error.Message
		return nil
	}
	response.Answer = responseBody.Choices[0].Message.Content
	response.InputToken = responseBody.PromptTokens
	response.OutToken = responseBody.CompletionTokens

	return nil
}
