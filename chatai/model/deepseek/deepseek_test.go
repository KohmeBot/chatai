package deepseek

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// Verify the actual serialized request against DeepSeek's vision wire format
// without sending images or credentials to an external service.
func TestVisionRequest(t *testing.T) {
	for _, imageURL := range []string{
		"https://example.com/cat.png",
		"data:image/png;base64,aW1hZ2U=",
	} {
		t.Run(strings.Split(imageURL, ":")[0], func(t *testing.T) {
			m := NewDeepSeekModel(model.Config{
				Name: "deepseek-flash", ApiKey: "test-key",
				System: "分析图片", MaxTokens: 1024,
			}).(*deepSeekModel)
			m.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "https://api.deepseek.com/chat/completions", r.URL.String())
				require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				var payload map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(body, &payload))
				require.JSONEq(t, `"deepseek-flash"`, string(payload["model"]))
				expected, err := json.Marshal([]any{
					map[string]any{"role": "system", "content": "分析图片"},
					map[string]any{"role": "user", "content": []any{
						map[string]any{"type": "text", "text": "图里有什么？"},
						map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
					}},
				})
				require.NoError(t, err)
				require.JSONEq(t, string(expected), string(payload["messages"]))
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"choices":[{"message":{"role":"assistant","content":"一只猫"}}],
						"usage":{"prompt_tokens":128,"completion_tokens":16}
					}`)),
				}, nil
			})}
			var response model.Response
			require.NoError(t, m.Request(&model.Request{Question: "图里有什么？", ImageURL: imageURL}, &response))
			require.Empty(t, response.ErrorMsg)
			require.Equal(t, "一只猫", response.Answer)
			require.EqualValues(t, 128, response.InputToken)
			require.EqualValues(t, 16, response.OutToken)
		})
	}
}
