package persona

import (
	"context"
	"errors"
	"fmt"
	"github.com/kohmebot/chatai/chatai/pkg/search"
	"github.com/sirupsen/logrus"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/kohmebot/chatai/chatai/agent"
)

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

type webSearchProvider struct {
	name     string
	endpoint func(string) string
	parse    func(string, int) []webSearchResult
}

var defaultWebSearchProviders = []webSearchProvider{
	{name: "DuckDuckGo", endpoint: func(query string) string {
		return "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	}, parse: parseDuckDuckGoResults},
	{name: "Bing CN", endpoint: func(query string) string {
		return "https://cn.bing.com/search?q=" + url.QueryEscape(query) + "&setlang=zh-cn"
	}, parse: parseBingResults},
	{name: "Baidu", endpoint: func(query string) string {
		return "https://www.baidu.com/s?wd=" + url.QueryEscape(query)
	}, parse: parseBaiduResults},
}

func (p *Persona) searchWeb(rc *agent.RunContext, query string, limit int) ([]webSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search query cannot be empty")
	}
	if limit <= 0 {
		limit = 5
	}

	if p.opts.SearchAPI != nil {
		rsp, err := p.opts.SearchAPI.DoRequest(rc, search.Request{
			Query: query,
			Limit: limit,
		})
		if err == nil {
			results := make([]webSearchResult, 0, len(rsp.Results))
			for _, r := range rsp.Results {
				results = append(results, webSearchResult{
					Title:   r.Title,
					URL:     r.URL,
					Snippet: r.Snippet,
				})
			}
			return results, nil
		}

		logrus.Errorf("search api error: %w", err)
	}

	p.searchMu.Lock()
	preferred := ""
	if time.Now().Before(p.preferredSearchUntil) {
		preferred = p.preferredSearchProvider
	}
	p.searchMu.Unlock()

	results, provider, err := searchWebWithPreferredProviderAndLoader(rc, query, limit, p.opts.WebMaxBytes, p.loadWebPage, defaultWebSearchProviders, preferred)
	if err != nil {
		return nil, err
	}
	p.searchMu.Lock()
	p.preferredSearchProvider = provider
	p.preferredSearchUntil = time.Now().Add(p.opts.WebSearchPrefer)
	p.searchMu.Unlock()
	return results, nil
}

func searchWebWithProviders(ctx context.Context, query string, limit, maxBytes int, client *http.Client, providers []webSearchProvider) ([]webSearchResult, error) {
	results, _, err := searchWebWithPreferredProvider(ctx, query, limit, maxBytes, client, providers, "")
	return results, err
}

func searchWebWithPreferredProvider(ctx context.Context, query string, limit, maxBytes int, client *http.Client, providers []webSearchProvider, preferred string) ([]webSearchResult, string, error) {
	return searchWebWithPreferredProviderAndLoader(ctx, query, limit, maxBytes, loadWebPageWithHTTP(client), providers, preferred)
}

func searchWebWithPreferredProviderAndLoader(ctx context.Context, query string, limit, maxBytes int, loader webPageLoader, providers []webSearchProvider, preferred string) ([]webSearchResult, string, error) {
	providers = preferredProviderFirst(providers, preferred)
	errs := make([]error, 0, len(providers))
	for _, provider := range providers {
		results, err := searchWithProviderAndLoader(ctx, query, limit, loader, provider)
		if err == nil {
			return results, provider.name, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", provider.name, err))
	}
	return nil, "", fmt.Errorf("all search providers failed: %w", errors.Join(errs...))
}

func preferredProviderFirst(providers []webSearchProvider, preferred string) []webSearchProvider {
	if preferred == "" || len(providers) < 2 {
		return providers
	}
	ordered := make([]webSearchProvider, 0, len(providers))
	for _, provider := range providers {
		if provider.name == preferred {
			ordered = append(ordered, provider)
			break
		}
	}
	if len(ordered) == 0 {
		return providers
	}
	for _, provider := range providers {
		if provider.name != preferred {
			ordered = append(ordered, provider)
		}
	}
	return ordered
}

func searchWithProvider(ctx context.Context, query string, limit int, client *http.Client, provider webSearchProvider) ([]webSearchResult, error) {
	return searchWithProviderAndLoader(ctx, query, limit, loadWebPageWithHTTP(client), provider)
}

func searchWithProviderAndLoader(ctx context.Context, query string, limit int, loader webPageLoader, provider webSearchProvider) ([]webSearchResult, error) {
	body, err := loader(ctx, provider.endpoint(query))
	if err != nil {
		return nil, err
	}
	//logrus.Infof("search results: %s", body)

	results := provider.parse(body, limit)
	if len(results) == 0 {
		return nil, errors.New("returned no parseable results")
	}
	return results, nil
}

var stripHTMLRE = regexp.MustCompile(`<[^>]+>`)

func cleanSearchText(raw string) string {
	return strings.Join(strings.Fields(html.UnescapeString(stripHTMLRE.ReplaceAllString(raw, " "))), " ")
}

const (
	duckDuckGoBaseURL = "https://html.duckduckgo.com/"
	bingBaseURL       = "https://cn.bing.com/"
	baiduBaseURL      = "https://www.baidu.com/"
)

func parseDuckDuckGoResults(source string, limit int) []webSearchResult {
	if !canParseSearchResults(source, limit) {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		return nil
	}

	results := make([]webSearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)

	/*
		优先匹配 result__body。

		其余选择器用于兼容旧版、精简版或不同实验页面。
		即使容器存在嵌套，URL 去重也能避免重复结果。
	*/
	containers := doc.Find(
		"div.result__body," +
			"div.result," +
			"div.results_links," +
			"div.web-result",
	)

	containers.EachWithBreak(func(_ int, item *goquery.Selection) bool {
		anchor := item.Find(
			"a.result__a[href]," +
				"h2.result__title a[href]," +
				"a.result-link[href]",
		).First()

		if anchor.Length() == 0 {
			return true
		}

		rawURL, ok := firstNonEmptyAttr(
			anchor,
			"data-href",
			"href",
		)
		if !ok {
			return true
		}

		result := webSearchResult{
			Title: cleanSelectionText(anchor),
			URL: normalizeParsedSearchURL(
				rawURL,
				duckDuckGoBaseURL,
			),
			Snippet: firstNonEmptySelectionText(
				item,
				".result__snippet",
				".result-snippet",
				".snippet",
			),
		}

		return !appendSearchResult(
			&results,
			seen,
			result,
			limit,
		)
	})

	return results
}

func parseBingResults(source string, limit int) []webSearchResult {
	if !canParseSearchResults(source, limit) {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		return nil
	}

	results := make([]webSearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)

	doc.Find("li.b_algo").EachWithBreak(
		func(_ int, item *goquery.Selection) bool {
			anchor := item.Find("h2 a[href]").First()
			if anchor.Length() == 0 {
				return true
			}

			/*
				data-url、data-href 如果存在，通常比 href 更值得优先使用，
				因为 href 可能是 Bing 的点击统计跳转地址。
			*/
			rawURL, ok := firstNonEmptyAttr(
				anchor,
				"data-url",
				"data-href",
				"href",
			)
			if !ok {
				return true
			}

			result := webSearchResult{
				Title: cleanSelectionText(anchor),
				URL: normalizeParsedSearchURL(
					rawURL,
					bingBaseURL,
				),
				Snippet: firstNonEmptySelectionText(
					item,

					// 优先使用明确的自然结果摘要节点。
					"p.b_algoSlug",

					// 兼容其他 Bing 页面结构。
					".b_caption p",
					".b_snippet",
					".b_paractl",
					"p",
				),
			}

			return !appendSearchResult(
				&results,
				seen,
				result,
				limit,
			)
		},
	)

	return results
}

func parseBaiduResults(source string, limit int) []webSearchResult {
	if !canParseSearchResults(source, limit) {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		return nil
	}

	results := make([]webSearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)

	/*
		先限制在 content_left 中，避免将右侧推荐、相关搜索或其他卡片
		当成自然搜索结果。
	*/
	containers := doc.Find(
		"#content_left > div.result," +
			"#content_left > div.result-op," +
			"#content_left > div.c-container",
	)

	/*
		有些精简页面不存在 content_left，才退回到全页面选择。
	*/
	if containers.Length() == 0 {
		containers = doc.Find(
			"div.result," +
				"div.result-op," +
				"div.c-container",
		)
	}

	containers.EachWithBreak(func(_ int, item *goquery.Selection) bool {
		anchor := item.Find(
			"h3.t a[href]," +
				"h3 a[href]",
		).First()

		if anchor.Length() == 0 {
			return true
		}

		/*
			优先读取可能保存真实落地地址的属性。

			href 经常是：
			https://www.baidu.com/link?url=...

			如果没有真实地址属性，才保留 href 跳转地址。
		*/
		rawURL := firstNonEmptyString(
			attrValue(anchor, "data-landurl"),
			attrValue(anchor, "data-url"),
			attrValue(item, "mu"),
			attrValue(item.Find("[mu]").First(), "mu"),
			attrValue(anchor, "href"),
		)
		if rawURL == "" {
			return true
		}

		result := webSearchResult{
			Title: cleanSelectionText(anchor),
			URL: normalizeParsedSearchURL(
				rawURL,
				baiduBaseURL,
			),
			Snippet: firstNonEmptySelectionText(
				item,
				".c-abstract",
				".c-span-last .content-right",
				".content-right",
				".c-font-normal",
			),
		}

		return !appendSearchResult(
			&results,
			seen,
			result,
			limit,
		)
	})

	return results
}

/*
canParseSearchResults 做最基本的输入和验证页判断。

因为当前 parser 签名不返回 error，所以遇到验证页时只能返回 nil。
长期建议将签名改为：

	func parseXXX(source string, limit int) ([]webSearchResult, error)
*/
func canParseSearchResults(source string, limit int) bool {
	if limit <= 0 || strings.TrimSpace(source) == "" {
		return false
	}

	return !isLikelySearchChallengePage(source)
}

func isLikelySearchChallengePage(source string) bool {
	lowerSource := strings.ToLower(source)

	markers := []string{
		`id="challenge-form"`,
		`name="challenge-form"`,
		`id="b_captcha"`,
		`class="b_captcha"`,
		"百度安全验证",
		"请输入验证码",
		"wappass.baidu.com/static/captcha",
	}

	for _, marker := range markers {
		if strings.Contains(lowerSource, strings.ToLower(marker)) {
			return true
		}
	}

	return false
}

/*
appendSearchResult 统一执行：

  - 清理字段
  - 过滤空标题
  - 过滤无效 URL
  - URL 去重
  - 按有效结果数量控制 limit

返回 true 表示结果数量已经达到 limit。
*/
func appendSearchResult(
	results *[]webSearchResult,
	seen map[string]struct{},
	result webSearchResult,
	limit int,
) bool {
	result.Title = strings.TrimSpace(result.Title)
	result.URL = strings.TrimSpace(result.URL)
	result.Snippet = strings.TrimSpace(result.Snippet)

	if result.Title == "" || result.URL == "" {
		return false
	}

	key := searchResultURLKey(result.URL)
	if key == "" {
		return false
	}

	if _, exists := seen[key]; exists {
		return false
	}

	seen[key] = struct{}{}
	*results = append(*results, result)

	return len(*results) >= limit
}

func cleanSelectionText(selection *goquery.Selection) string {
	if selection == nil || selection.Length() == 0 {
		return ""
	}

	return cleanSearchText(selection.Text())
}

func firstNonEmptySelectionText(
	parent *goquery.Selection,
	selectors ...string,
) string {
	if parent == nil || parent.Length() == 0 {
		return ""
	}

	for _, selector := range selectors {
		selection := parent.Find(selector).First()
		if selection.Length() == 0 {
			continue
		}

		text := cleanSelectionText(selection)
		if text != "" {
			return text
		}
	}

	return ""
}

func firstNonEmptyAttr(
	selection *goquery.Selection,
	names ...string,
) (string, bool) {
	if selection == nil || selection.Length() == 0 {
		return "", false
	}

	for _, name := range names {
		value, exists := selection.Attr(name)
		value = strings.TrimSpace(value)

		if exists && value != "" {
			return value, true
		}
	}

	return "", false
}

func attrValue(
	selection *goquery.Selection,
	name string,
) string {
	if selection == nil || selection.Length() == 0 {
		return ""
	}

	value, exists := selection.Attr(name)
	if !exists {
		return ""
	}

	return strings.TrimSpace(value)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}

	return ""
}

/*
normalizeParsedSearchURL 负责：

  - HTML 实体解码
  - 解析相对地址
  - 解包 DuckDuckGo 的 uddg 跳转参数
  - 调用项目已有的 normalizeSearchURL
  - 限制为 http/https
  - 移除 fragment
*/
func normalizeParsedSearchURL(
	rawURL string,
	baseURL string,
) string {
	rawURL = strings.TrimSpace(html.UnescapeString(rawURL))
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	if !parsed.IsAbs() {
		base, baseErr := url.Parse(baseURL)
		if baseErr != nil {
			return ""
		}

		parsed = base.ResolveReference(parsed)
	}

	/*
		DuckDuckGo HTML 常见跳转格式：

		https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com
	*/
	if isDuckDuckGoHost(parsed.Hostname()) {
		if target := strings.TrimSpace(parsed.Query().Get("uddg")); target != "" {
			targetURL, targetErr := url.Parse(target)
			if targetErr == nil && targetURL.IsAbs() {
				parsed = targetURL
			}
		}
	}

	normalized := strings.TrimSpace(
		normalizeSearchURL(parsed.String()),
	)
	if normalized == "" {
		return ""
	}

	finalURL, err := url.Parse(normalized)
	if err != nil {
		return ""
	}

	switch strings.ToLower(finalURL.Scheme) {
	case "http", "https":
	default:
		return ""
	}

	if finalURL.Hostname() == "" {
		return ""
	}

	finalURL.Fragment = ""

	return finalURL.String()
}

func isDuckDuckGoHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))

	return host == "duckduckgo.com" ||
		host == "html.duckduckgo.com" ||
		strings.HasSuffix(host, ".duckduckgo.com")
}

/*
searchResultURLKey 生成去重 key。

这里不删除全部查询参数，因为某些站点依靠查询参数标识实际页面；
只移除 fragment，并统一 scheme、host 大小写。
*/
func searchResultURLKey(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}

	if parsed.Hostname() == "" {
		return ""
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""

	return parsed.String()
}

func normalizeSearchURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if target := u.Query().Get("uddg"); target != "" {
		return target
	}
	return raw
}
