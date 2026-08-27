package tongyi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
)

type tongYiModel struct {
	model.Config

	apiKeyHeader string
	systemMsg    model.Message
	client       *http.Client
	// imageEndpoint is overridden by tests.
	imageEndpoint string
}

const chatCompletionEndpoint = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
const imageGenerationEndpoint = "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"

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
	if isImageModel(m.Name) {
		return m.requestImage(request, response)
	}
	return m.requestChat(request, response)
}

func isImageModel(name string) bool {
	return strings.Contains(strings.ToLower(name), "image")
}

func (m *tongYiModel) requestChat(request *model.Request, response *model.Response) error {

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
	req, err := http.NewRequestWithContext(requestContext, "POST", chatCompletionEndpoint, bytes.NewBuffer(jsonData))
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

func (m *tongYiModel) requestImage(request *model.Request, response *model.Response) error {
	endpoint := m.imageEndpoint
	if endpoint == "" {
		endpoint = imageGenerationEndpoint
	}
	content, err := imageRequestContent(request)
	if err != nil {
		return err
	}
	requestBody := imageReqBody{
		Model: m.Name,
		Input: imageInput{Messages: []imageMessage{{
			Role:    "user",
			Content: content,
		}}},
		Parameters: imageParameters{
			PromptExtend:   true,
			EnableThinking: m.Thinking,
		},
	}
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}

	requestContext := request.Context
	if requestContext == nil {
		requestContext = context.Background()
	}
	logrus.Infof("model request provider=tongyi model=%s image_generation=true input_images=%d", m.Name, countInputImages(content))
	req, err := http.NewRequestWithContext(requestContext, http.MethodPost, endpoint, bytes.NewBuffer(jsonData))
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
	logrus.Infof("model response provider=tongyi model=%s image_generation=true status=%d bytes=%d", m.Name, resp.StatusCode, len(buf))

	responseBody := imageRespBody{}
	if err := json.Unmarshal(buf, &responseBody); err != nil {
		return err
	}
	if responseBody.Code != "" {
		response.ErrorMsg = responseBody.Message
		return nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("tongyi image generation returned HTTP %d", resp.StatusCode)
	}
	if len(responseBody.Output.Choices) == 0 {
		return io.ErrUnexpectedEOF
	}

	outputContent := responseBody.Output.Choices[0].Message.Content
	for _, part := range outputContent {
		if part.Image != "" {
			response.Answer = part.Image
			break
		}
	}
	if response.Answer == "" {
		return io.ErrUnexpectedEOF
	}
	response.Content = outputContent
	return nil
}

func imageRequestContent(request *model.Request) ([]model.ContentPart, error) {
	prompt := request.Question
	images := make([]string, 0, 3)
	if request.ImageURL != "" {
		images = append(images, request.ImageURL)
	}
	if request.Content != nil {
		switch content := request.Content.(type) {
		case string:
			prompt = content
		default:
			encoded, err := json.Marshal(content)
			if err != nil {
				return nil, fmt.Errorf("encode tongyi image request content: %w", err)
			}
			var parts []model.ContentPart
			if err := json.Unmarshal(encoded, &parts); err != nil {
				return nil, fmt.Errorf("tongyi image request content must be a content-part array: %w", err)
			}
			prompt = ""
			textParts := 0
			for _, part := range parts {
				switch part.Type {
				case "text":
					textParts++
					prompt = part.Text
				case "image_url":
					if part.ImageURL == nil || part.ImageURL.URL == "" {
						return nil, fmt.Errorf("tongyi image request contains an empty image_url")
					}
					images = append(images, part.ImageURL.URL)
				case "":
					switch {
					case part.Image != "" && part.Text == "" && part.ImageURL == nil:
						images = append(images, part.Image)
					case part.Text != "" && part.Image == "" && part.ImageURL == nil:
						textParts++
						prompt = part.Text
					default:
						return nil, fmt.Errorf("invalid tongyi image content part")
					}
				default:
					return nil, fmt.Errorf("unsupported tongyi image content type %q", part.Type)
				}
			}
			if textParts != 1 {
				return nil, fmt.Errorf("tongyi image request supports exactly one text content part")
			}
		}
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("tongyi image request requires a text prompt")
	}
	if len(images) > 3 {
		return nil, fmt.Errorf("tongyi image request supports at most 3 input images")
	}
	result := make([]model.ContentPart, 0, len(images)+1)
	for _, image := range images {
		result = append(result, model.ContentPart{Image: image})
	}
	return append(result, model.ContentPart{Text: prompt}), nil
}

func countInputImages(content []model.ContentPart) int {
	count := 0
	for _, part := range content {
		if part.Image != "" {
			count++
		}
	}
	return count
}
