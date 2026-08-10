package persona

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	recent := p.tools.Search("总结半小时前到现在的聊天内容", 5)
	require.Contains(t, searchNames(recent), "read_group_context")
}

func TestBuiltinToolSearchFindsGroupMemberTools(t *testing.T) {
	p := &Persona{tools: agent.NewRegistry()}
	p.registerBuiltinTools()

	info := p.tools.Search("查询群成员资料和群名片", 5)
	require.Contains(t, searchNames(info), "get_group_member_info")

	list := p.tools.Search("获取群成员名单", 5)
	require.Contains(t, searchNames(list), "get_group_member_list")
}

func TestBuiltinToolSearchFindsImpressionUpdateWhenEnabled(t *testing.T) {
	p := &Persona{opts: Options{ImpressionUpdateEnable: true}, tools: agent.NewRegistry()}
	p.registerBuiltinTools()

	results := p.tools.Search("记住这位群友并更新长期印象", 5)
	require.Contains(t, searchNames(results), "update_impression")
}

func TestBuiltinToolDoesNotRegisterImpressionUpdateWhenDisabled(t *testing.T) {
	p := &Persona{tools: agent.NewRegistry()}
	p.registerBuiltinTools()

	results := p.tools.Search("更新印象", 8)
	require.NotContains(t, searchNames(results), "update_impression")
}

func TestUpdateImpressionValidatesArgumentsBeforeDatabaseAccess(t *testing.T) {
	p := &Persona{groupID: 12345}

	_, err := p.handleUpdateImpression(nil, []byte(`{"scope":"user","user_id":0,"content":"长期印象","reason":"用户明确表达"}`))
	require.ErrorContains(t, err, "user_id must be positive")

	_, err = p.handleUpdateImpression(nil, []byte(`{"scope":"group","content":"   ","reason":"群内长期惯例"}`))
	require.ErrorContains(t, err, "content must not be empty")

	_, err = p.handleUpdateImpression(nil, []byte(`{"scope":"other","content":"长期印象","reason":"长期事实"}`))
	require.ErrorContains(t, err, "scope must be group or user")
}

func TestGroupMemberToolsValidateCurrentGroupBeforeUsingContext(t *testing.T) {
	p := &Persona{groupID: 12345}
	rc := &agent.RunContext{}

	_, err := p.handleGetGroupMemberInfo(rc, []byte(`{"group_id":54321,"user_id":10001}`))
	require.ErrorContains(t, err, "current group (12345)")

	_, err = p.handleGetGroupMemberList(rc, []byte(`{"group_id":0}`))
	require.ErrorContains(t, err, "group_id must be positive")

	_, err = p.handleGetGroupMemberInfo(rc, []byte(`{"group_id":12345,"user_id":0}`))
	require.ErrorContains(t, err, "user_id must be positive")
}

func TestDecodeContextQuerySupportsRelativeTimeAndPagination(t *testing.T) {
	p := &Persona{opts: Options{ContextLimit: 50}}
	before := time.Now()
	query, err := p.decodeContextQuery([]byte(`{"last_minutes":30,"limit":20,"offset":40,"keyword":"发布"}`), 123)
	after := time.Now()
	require.NoError(t, err)
	require.Equal(t, int64(123), query.UserID)
	require.Equal(t, 20, query.Limit)
	require.Equal(t, 40, query.Offset)
	require.Equal(t, "发布", query.Keyword)
	require.NotNil(t, query.Start)
	require.NotNil(t, query.End)
	require.WithinDuration(t, before.Add(-30*time.Minute), *query.Start, after.Sub(before)+time.Second)
	require.WithinDuration(t, before, *query.End, after.Sub(before)+time.Second)
}

func TestDecodeContextQueryRejectsConflictingRelativeAndAbsoluteStart(t *testing.T) {
	p := &Persona{opts: Options{ContextLimit: 50}}
	_, err := p.decodeContextQuery([]byte(`{"last_minutes":30,"since":"2026-08-07 10:00"}`), 0)
	require.ErrorContains(t, err, "cannot be used together")
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

func TestSearchWebFallsBackInProviderOrder(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/duckduckgo":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case "/bing":
			fmt.Fprint(w, `<html>no parseable results</html>`)
		case "/baidu":
			fmt.Fprint(w, `<div class="result c-container"><h3 class="t"><a href="https://example.com/result">备用结果</a></h3><div>摘要</div></div>`)
		}
	}))
	defer server.Close()

	providers := []webSearchProvider{
		{name: "DuckDuckGo", endpoint: func(string) string { return server.URL + "/duckduckgo" }, parse: parseDuckDuckGoResults},
		{name: "Bing CN", endpoint: func(string) string { return server.URL + "/bing" }, parse: parseBingResults},
		{name: "Baidu", endpoint: func(string) string { return server.URL + "/baidu" }, parse: parseBaiduResults},
	}
	results, err := searchWebWithProviders(context.Background(), "测试", 5, server.Client(), providers)
	require.NoError(t, err)
	require.Equal(t, []string{"/duckduckgo", "/bing", "/baidu"}, paths)
	require.Equal(t, []webSearchResult{{Title: "备用结果", URL: "https://example.com/result"}}, results)
}

func TestSearchWebStopsAfterFirstSuccessfulProvider(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, `<li class="b_algo"><h2><a href="https://example.com">标题</a></h2><div><p>搜索摘要</p></div></li>`)
	}))
	defer server.Close()

	providers := []webSearchProvider{
		{name: "Bing CN", endpoint: func(string) string { return server.URL }, parse: parseBingResults},
		{name: "Baidu", endpoint: func(string) string { return server.URL }, parse: parseBaiduResults},
	}
	results, err := searchWebWithProviders(context.Background(), "测试", 5, server.Client(), providers)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "搜索摘要", results[0].Snippet)
	require.Equal(t, 1, requests)
}

func TestSearchWebMovesPreferredProviderFirst(t *testing.T) {
	providers := []webSearchProvider{{name: "first"}, {name: "second"}, {name: "third"}}
	ordered := preferredProviderFirst(providers, "third")
	require.Equal(t, []string{"third", "first", "second"}, []string{ordered[0].name, ordered[1].name, ordered[2].name})
	require.Equal(t, []string{"first", "second", "third"}, []string{providers[0].name, providers[1].name, providers[2].name}, "must not mutate the default order")
}

func TestBrowserLoaderRejectsPrivateURLBeforeStartingChrome(t *testing.T) {
	_, err := loadWebPageWithBrowser(context.Background(), "http://127.0.0.1/private", "")
	require.ErrorContains(t, err, "private or local addresses are not allowed")
}

func TestSearchWebUsesConfiguredPageLoader(t *testing.T) {
	var loadedURL string
	loader := func(_ context.Context, rawURL string) (string, error) {
		loadedURL = rawURL
		return `<li class="b_algo"><h2><a href="https://example.com">标题</a></h2><p>摘要</p></li>`, nil
	}
	providers := []webSearchProvider{{
		name:     "browser",
		endpoint: func(query string) string { return "https://search.example/?q=" + url.QueryEscape(query) },
		parse:    parseBingResults,
	}}

	results, provider, err := searchWebWithPreferredProviderAndLoader(context.Background(), "浏览器搜索", 5, loader, providers, "")
	require.NoError(t, err)
	require.Equal(t, "browser", provider)
	require.Equal(t, "https://search.example/?q=%E6%B5%8F%E8%A7%88%E5%99%A8%E6%90%9C%E7%B4%A2", loadedURL)
	require.Equal(t, []webSearchResult{{Title: "标题", URL: "https://example.com", Snippet: "摘要"}}, results)
}

func searchNames(results []agent.SearchResult) []string {
	names := make([]string, len(results))
	for i := range results {
		names[i] = results[i].Name
	}
	return names
}
