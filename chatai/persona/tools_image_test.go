package persona

import (
	"errors"
	"testing"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

type imageToolTestModel struct {
	request  *model.Request
	answer   string
	errorMsg string
	err      error
}

func (m *imageToolTestModel) Request(request *model.Request, response *model.Response) error {
	m.request = request
	response.Answer = m.answer
	response.ErrorMsg = m.errorMsg
	return m.err
}

func TestImageToolIsRegisteredOnlyWithVisionModel(t *testing.T) {
	withoutVision := &Persona{tools: agent.NewRegistry()}
	withoutVision.registerBuiltinTools()
	require.NotContains(t, searchNames(withoutVision.tools.Search("分析图片", 8)), "analyze_image")

	withVision := &Persona{
		opts:  Options{VisionModel: &imageToolTestModel{}},
		tools: agent.NewRegistry(),
	}
	withVision.registerBuiltinTools()
	require.Contains(t, searchNames(withVision.tools.Search("理解图片 URL", 8)), "analyze_image")
}

func TestAnalyzeImageCallsVisionModel(t *testing.T) {
	vision := &imageToolTestModel{answer: "图片中是一只橘猫。"}
	p := &Persona{opts: Options{VisionModel: vision}}

	result, err := p.handleAnalyzeImage(nil, []byte(`{"url":" https://example.com/cat.png?size=large ","prompt":"图里有什么？"}`))
	require.NoError(t, err)
	require.Equal(t, "图片中是一只橘猫。", result)
	require.Equal(t, "https://example.com/cat.png?size=large", vision.request.ImageURL)
	require.Equal(t, "图里有什么？", vision.request.Question)
	require.Empty(t, vision.request.Tools)
}

func TestAnalyzeImageUsesDefaultPrompt(t *testing.T) {
	vision := &imageToolTestModel{answer: "分析结果"}
	p := &Persona{opts: Options{VisionModel: vision}}

	_, err := p.handleAnalyzeImage(nil, []byte(`{"url":"https://example.com/image.jpg"}`))
	require.NoError(t, err)
	require.Equal(t, defaultImageAnalysisPrompt, vision.request.Question)
}

func TestAnalyzeImageRejectsInvalidURLBeforeCallingModel(t *testing.T) {
	vision := &imageToolTestModel{answer: "不应调用"}
	p := &Persona{opts: Options{VisionModel: vision}}

	_, err := p.handleAnalyzeImage(nil, []byte(`{"url":"file:///etc/passwd"}`))
	require.ErrorContains(t, err, "only absolute http(s) URLs")
	require.Nil(t, vision.request)
}

func TestAnalyzeImageReturnsModelErrors(t *testing.T) {
	t.Run("request error", func(t *testing.T) {
		p := &Persona{opts: Options{VisionModel: &imageToolTestModel{err: errors.New("request failed")}}}
		_, err := p.handleAnalyzeImage(nil, []byte(`{"url":"https://example.com/image.jpg"}`))
		require.ErrorContains(t, err, "request failed")
	})

	t.Run("provider error", func(t *testing.T) {
		p := &Persona{opts: Options{VisionModel: &imageToolTestModel{errorMsg: "unsupported image"}}}
		_, err := p.handleAnalyzeImage(nil, []byte(`{"url":"https://example.com/image.jpg"}`))
		require.ErrorContains(t, err, "unsupported image")
	})
}
