package persona

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
)

const defaultImageAnalysisPrompt = "请分析这张图片，准确说明主要内容、可见文字、关键细节以及不确定之处。只返回图片理解结果。"

func (p *Persona) imageTools() []agent.Tool {
	if p.opts.VisionModel == nil {
		return nil
	}
	return []agent.Tool{
		{
			Definition: agent.Function("analyze_image", "使用视觉模型理解 HTTP/HTTPS 图片 URL；可描述画面、识别文字、分析截图或回答与图片有关的问题", map[string]any{
				"url":    stringProperty("要理解的图片 HTTP/HTTPS URL"),
				"prompt": stringProperty("可选的分析要求或关于图片的问题；不填则全面描述图片"),
			}, "url"),
			Namespace:   "vision",
			ReadOnly:    true,
			Idempotent:  true,
			Risk:        agent.ToolRiskMedium,
			SearchTerms: []string{"图片理解", "分析图片", "识别图片", "识图", "看图", "视觉模型", "图片URL", "image", "vision", "OCR", "截图"},
			Handler:     p.handleAnalyzeImage,
		},
	}
}

func (p *Persona) handleAnalyzeImage(runCtx *agent.RunContext, raw json.RawMessage) (any, error) {
	var input struct {
		URL    string `json:"url"`
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if p.opts.VisionModel == nil {
		return nil, errors.New("vision model is not configured")
	}

	imageURL, err := normalizeImageURL(input.URL)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		prompt = defaultImageAnalysisPrompt
	}

	response := new(model.Response)
	request := &model.Request{Question: prompt, ImageURL: imageURL}
	if runCtx != nil {
		request.Context = runCtx.Context
	}
	if err := p.opts.VisionModel.Request(request, response); err != nil {
		return nil, err
	}
	if response.ErrorMsg != "" {
		return nil, errors.New(response.ErrorMsg)
	}
	answer := strings.TrimSpace(response.Answer)
	if answer == "" {
		return nil, errors.New("vision model returned an empty response")
	}
	return answer, nil
}

func normalizeImageURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid image URL: only absolute http(s) URLs are supported")
	}
	return u.String(), nil
}
