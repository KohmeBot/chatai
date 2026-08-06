package persona

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestRepeatMessageUsesImageURLInsteadOfInboundCacheFile(t *testing.T) {
	incoming := message.Message{
		message.Text("看看 "),
		{Type: MsgTypeImg, Data: map[string]string{
			"file":    "8A7C-temporary-cache.image",
			"url":     "https://example.com/image.jpg",
			"summary": "[图片]",
		}},
	}

	repeated := repeatMessage(incoming)
	require.Equal(t, message.Text("看看 "), repeated[0])
	require.Equal(t, MsgTypeImg, repeated[1].Type)
	require.Equal(t, "https://example.com/image.jpg", repeated[1].Data["file"])
	require.Equal(t, "[图片]", repeated[1].Data["summary"])
	require.NotContains(t, repeated[1].Data, "url")
	require.Equal(t, "8A7C-temporary-cache.image", incoming[1].Data["file"], "must not mutate the received event")
}

func TestRepeatMessageFallsBackToFileWhenImageHasNoURL(t *testing.T) {
	incoming := message.Message{message.Image("base64://aGVsbG8=")}
	repeated := repeatMessage(incoming)
	require.Equal(t, "base64://aGVsbG8=", repeated[0].Data["file"])
}

func TestGetURLRecognizesOutboundHTTPImageFile(t *testing.T) {
	require.Equal(t, "https://example.com/image.jpg", getUrl(message.Message{message.Image("https://example.com/image.jpg")}))
}
