package persona

import (
	"testing"
	"time"

	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/stretchr/testify/require"
)

func TestBuiltinToolSearchFindsWebAndTimeRangeTools(t *testing.T) {
	p := &Persona{tools: agent.NewRegistry()}
	p.registerBuiltinTools()

	web := p.tools.Search("遇到不懂的内容需要联网搜索", 5)
	require.Contains(t, searchNames(web), "search_web")

	history := p.tools.Search("查询多个时间段的历史消息", 5)
	require.Contains(t, searchNames(history), "read_messages_by_time")
}

func TestParseToolTimeSupportsLocalAndRFC3339(t *testing.T) {
	local, err := parseToolTime("2026-08-06 09:30", false)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 8, 6, 9, 30, 0, 0, time.Local), local)

	rfc3339, err := parseToolTime("2026-08-06T09:30:00+08:00", false)
	require.NoError(t, err)
	require.Equal(t, 9, rfc3339.Hour())

	dateEnd, err := parseToolTime("2026-08-06", true)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 8, 7, 0, 0, 0, 0, time.Local), dateEnd)
}

func searchNames(results []agent.SearchResult) []string {
	names := make([]string, len(results))
	for i := range results {
		names[i] = results[i].Name
	}
	return names
}
