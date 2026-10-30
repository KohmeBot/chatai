package persona

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestIncomingSegmentsRestoresPreprocessedMention(t *testing.T) {
	for _, native := range []string{
		`[{"type":"at","data":{"qq":"100"}}]`,
		`"[CQ:at,qq=100]"`,
		`[{"type":"at","data":{"qq":"100"}},{"type":"text","data":{"text":" "}}]`,
	} {
		t.Run(native, func(t *testing.T) {
			event := &zero.Event{SelfID: 100, IsToMe: true, NativeMessage: json.RawMessage(native)}
			segments := incomingSegments(event)
			require.NotEmpty(t, segments, "a bare @ must not hit newMessage's empty-message guard")
			require.Equal(t, MsgTypeAt, getMsgType(segments))
			require.Contains(t, getText(segments), "@100")
			require.Empty(t, event.Message, "do not mutate the event shared with other plugins")
		})
	}
}

func TestIncomingSegmentsPreservesMixedContentAndFallsBack(t *testing.T) {
	want := message.Message{message.At(100), message.At(200), message.Text("看看"), message.Image("pic")}
	native, err := json.Marshal(want)
	require.NoError(t, err)
	event := &zero.Event{NativeMessage: native, Message: want[1:]}
	require.Equal(t, want, incomingSegments(event))
	for _, native := range []string{"", "null", "[]", "invalid"} {
		event.NativeMessage = json.RawMessage(native)
		require.Equal(t, event.Message, incomingSegments(event))
	}
	require.Empty(t, incomingSegments(&zero.Event{}), "real empty events remain empty")
}

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

func TestGetTextPreservesAllMentionsInOrder(t *testing.T) {
	segments := message.Message{message.At(100), message.At(200), message.Text("你觉得怎么样")}
	require.Equal(t, " @100  @200 你觉得怎么样", getText(segments))
	formatted := formatMessage(GroupMessage{MsgType: MsgTypeAt, TargetUser: User{UserId: 100, Nickname: selfNickname}, Content: getText(segments)})
	require.Contains(t, formatted, "@200")
	require.Equal(t, "前 @200 中 @300 后", getText(message.Message{message.Text("前"), message.At(200), message.Text("中"), message.At(300), message.Text("后")}))
	require.Equal(t, " @全体成员 ", getText(message.Message{message.AtAll()}))
	require.Equal(t, "原文 & 内容", getText(message.Message{message.Text("原文 & 内容")}))
	require.Contains(t, getText(message.Message{message.Image("pic"), message.At(200)}), "@200")
}
