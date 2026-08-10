package persona

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/PuerkitoBio/goquery"
	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/sirupsen/logrus"
)

const (
	webContentMaxBytes    = 64 * 1024
	webContextRadiusBytes = 4 * 1024
	webMaxQueryMatches    = 512
	webMaxMatchesPerTerm  = 64
)

func (p *Persona) readWeb(rc *agent.RunContext, rawURL, query string) (string, error) {
	source, err := p.loadWebPage(rc, rawURL)
	if err != nil {
		return "", err
	}
	return extractWebMarkdown(source, rawURL, query)
}

// extractWebMarkdown extracts an article when possible and returns a Markdown
// document with source metadata. Pages that do not look like articles fall
// back to converting the cleaned body, then to generic text extraction.
func extractWebMarkdown(source, rawURL, query string) (string, error) {
	pageURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	var articleErr error
	var article readability.Article
	checkDoc, checkErr := goquery.NewDocumentFromReader(strings.NewReader(source))
	if checkErr == nil && readability.CheckDocument(checkDoc.Selection.Get(0)) {
		article, articleErr = readability.FromReader(strings.NewReader(source), pageURL)
	}
	if articleErr == nil && article.Node != nil {
		var content bytes.Buffer
		if err := article.RenderHTML(&content); err == nil {
			body, err := htmltomarkdown.ConvertString(content.String(), converter.WithDomain(rawURL))
			if err == nil && strings.TrimSpace(body) != "" {
				return limitWebContent(formatArticleMarkdown(article, rawURL, body), query, webContentMaxBytes), nil
			}
		}
	}

	text, fallbackErr := extractFallbackMarkdownBody(source, rawURL)
	if fallbackErr != nil {
		if articleErr != nil {
			return "", errors.Join(articleErr, fallbackErr)
		}
		return "", fallbackErr
	}
	return limitWebContent(formatFallbackMarkdown(source, rawURL, text), query, webContentMaxBytes), nil
}

func extractFallbackMarkdownBody(source, rawURL string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err == nil {
		doc.Find("script, style, noscript, svg, canvas, iframe").Remove()
		doc.Find("nav, footer").Remove()
		if body, htmlErr := doc.Find("body").Html(); htmlErr == nil {
			markdown, markdownErr := htmltomarkdown.ConvertString(body, converter.WithDomain(rawURL))
			if markdownErr == nil && strings.TrimSpace(markdown) != "" {
				return strings.TrimSpace(markdown), nil
			}
		}
	}
	return extractUsefulText(source)
}

func formatArticleMarkdown(article readability.Article, rawURL, body string) string {
	title := cleanWebMetadata(article.Title())
	if title == "" {
		title = "网页正文"
	}

	var result strings.Builder
	result.WriteString("# ")
	result.WriteString(title)
	result.WriteString("\n\n- 来源：<")
	result.WriteString(escapeMarkdownURL(rawURL))
	result.WriteString(">")
	writeWebMetadata(&result, "网站", article.SiteName())
	writeWebMetadata(&result, "作者", article.Byline())
	if publishedAt, err := article.PublishedTime(); err == nil {
		writeWebMetadata(&result, "发布时间", publishedAt.Format(time.RFC3339))
	}
	result.WriteString("\n\n## 正文\n\n")
	result.WriteString(strings.TrimSpace(body))
	return result.String()
}

func formatFallbackMarkdown(source, rawURL, text string) string {
	title := "网页内容"
	if doc, err := goquery.NewDocumentFromReader(strings.NewReader(source)); err == nil {
		if pageTitle := cleanWebMetadata(doc.Find("title").First().Text()); pageTitle != "" {
			title = pageTitle
		}
	}
	return "# " + title + "\n\n- 来源：<" + escapeMarkdownURL(rawURL) + ">\n\n## 正文\n\n" + strings.TrimSpace(text)
}

func writeWebMetadata(result *strings.Builder, label, value string) {
	if value = cleanWebMetadata(value); value != "" {
		result.WriteString("\n- ")
		result.WriteString(label)
		result.WriteString("：")
		result.WriteString(value)
	}
}

func cleanWebMetadata(value string) string {
	return strings.Join(strings.Fields(html.UnescapeString(value)), " ")
}

func escapeMarkdownURL(value string) string {
	return strings.ReplaceAll(value, ">", "%3E")
}

func extractUsefulText(source string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		return "", err
	}

	doc.Find("script, style, noscript, svg, canvas, iframe").Remove()
	doc.Find("nav, footer").Remove()

	// 给链接补充 URL
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if exists && href != "" {
			text := strings.TrimSpace(s.Text())

			if text != "" {
				s.ReplaceWithHtml(
					html.EscapeString(text) +
						" [链接: " +
						html.EscapeString(href) +
						"]",
				)
			}
		}
	})

	text := doc.Find("body").Text()

	lines := strings.Split(text, "\n")

	var result []string
	for _, line := range lines {
		line = html.UnescapeString(line)
		line = strings.Join(strings.Fields(line), " ")

		if line != "" {
			result = append(result, line)
		}
	}

	res := strings.Join(result, "\n")

	return res, nil
}

type webQueryTerm struct {
	text   string
	weight int
}

type webQueryMatch struct {
	start  int
	end    int
	weight int
}

type webContextWindow struct {
	start int
	end   int
	score int
}

// limitWebContent leaves small pages untouched. For large pages it returns
// the metadata plus the most relevant, non-overlapping context windows.
func limitWebContent(content, query string, maxBytes int) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}

	header, body := splitWebMarkdownBody(content)
	matches := findWebQueryMatches(body, webQueryTerms(query))
	if len(matches) == 0 {
		fallback := header + "> 网页内容较长，未找到与查询“" + displayWebQuery(query) + "”直接匹配的片段，以下返回正文开头。\n\n" + body
		return truncateWebContent(fallback, maxBytes, "\n\n> 网页内容已截取")
	}

	radius := min(webContextRadiusBytes, max(64, maxBytes/8))
	windows := rankWebContextWindows(body, matches, radius)
	selected := make([]webContextWindow, 0, len(windows))
	usedBytes := len(header) + len(displayWebQuery(query)) + 128
	for _, candidate := range windows {
		if overlapsWebWindow(candidate, selected) {
			continue
		}
		windowBytes := candidate.end - candidate.start + 32
		if usedBytes+windowBytes > maxBytes {
			continue
		}
		selected = append(selected, candidate)
		usedBytes += windowBytes
	}
	if len(selected) == 0 {
		selected = append(selected, windows[0])
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].start < selected[j].start })

	var result strings.Builder
	result.WriteString(header)
	result.WriteString("> 网页内容较长，以下是与查询“")
	result.WriteString(displayWebQuery(query))
	result.WriteString("”相关的上下文片段。")
	previousEnd := 0
	for _, window := range selected {
		if window.start > previousEnd {
			result.WriteString("\n\n> ……已省略无关内容……")
		}
		result.WriteString("\n\n")
		result.WriteString(strings.TrimSpace(body[window.start:window.end]))
		previousEnd = window.end
	}
	if previousEnd < len(body) {
		result.WriteString("\n\n> ……已省略无关内容……")
	}
	return truncateWebContent(result.String(), maxBytes, "\n\n> 网页内容已按查询截取")
}

func splitWebMarkdownBody(content string) (string, string) {
	const marker = "\n\n## 正文\n\n"
	index := strings.Index(content, marker)
	if index < 0 {
		return "", content
	}
	bodyStart := index + len(marker)
	return content[:bodyStart], content[bodyStart:]
}

func webQueryTerms(query string) []webQueryTerm {
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	if normalized == "" {
		return nil
	}

	terms := make(map[string]int)
	add := func(term string, weight int) {
		term = strings.TrimSpace(term)
		if len([]rune(term)) < 2 {
			return
		}
		if weight > terms[term] {
			terms[term] = weight
		}
	}
	add(normalized, len([]rune(normalized))*4)
	fields := strings.FieldsFunc(normalized, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, field := range fields {
		runes := []rune(field)
		add(field, len(runes)*2)
		if containsHanRune(runes) && len(runes) > 2 {
			for i := 0; i+2 <= len(runes); i++ {
				add(string(runes[i:i+2]), 1)
			}
		}
	}

	result := make([]webQueryTerm, 0, len(terms))
	for term, weight := range terms {
		result = append(result, webQueryTerm{text: term, weight: weight})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].weight == result[j].weight {
			return len(result[i].text) > len(result[j].text)
		}
		return result[i].weight > result[j].weight
	})
	return result
}

func containsHanRune(runes []rune) bool {
	for _, r := range runes {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func findWebQueryMatches(body string, terms []webQueryTerm) []webQueryMatch {
	lowerBody := strings.ToLower(body)
	matches := make([]webQueryMatch, 0)
	for _, term := range terms {
		termMatches := 0
		for offset := 0; offset < len(lowerBody) && len(matches) < webMaxQueryMatches && termMatches < webMaxMatchesPerTerm; {
			index := strings.Index(lowerBody[offset:], term.text)
			if index < 0 {
				break
			}
			start := offset + index
			end := start + len(term.text)
			if end > len(body) {
				end = len(body)
			}
			matches = append(matches, webQueryMatch{start: start, end: end, weight: term.weight})
			termMatches++
			offset = start + max(1, len(term.text))
		}
		if len(matches) >= webMaxQueryMatches {
			break
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].start < matches[j].start })
	return matches
}

func rankWebContextWindows(body string, matches []webQueryMatch, radius int) []webContextWindow {
	windows := make([]webContextWindow, 0, len(matches))
	seen := make(map[[2]int]struct{}, len(matches))
	for _, match := range matches {
		start := max(0, match.start-radius)
		end := min(len(body), match.end+radius)
		start, end = expandWebWindowToParagraphs(body, start, end)
		key := [2]int{start, end}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		score := 0
		for _, other := range matches {
			if other.start >= start && other.start < end {
				score += other.weight
			}
		}
		windows = append(windows, webContextWindow{start: start, end: end, score: score})
	}
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].score == windows[j].score {
			return windows[i].start < windows[j].start
		}
		return windows[i].score > windows[j].score
	})
	return windows
}

func expandWebWindowToParagraphs(body string, start, end int) (int, int) {
	const boundarySearchBytes = 512
	if start > 0 {
		searchStart := max(0, start-boundarySearchBytes)
		if boundary := strings.LastIndex(body[searchStart:start], "\n\n"); boundary >= 0 {
			start = searchStart + boundary + 2
		}
	}
	if end < len(body) {
		searchEnd := min(len(body), end+boundarySearchBytes)
		if boundary := strings.Index(body[end:searchEnd], "\n\n"); boundary >= 0 {
			end += boundary
		}
	}
	for start < end && !utf8.RuneStart(body[start]) {
		start++
	}
	for end > start && end < len(body) && !utf8.RuneStart(body[end]) {
		end--
	}
	return start, end
}

func overlapsWebWindow(candidate webContextWindow, selected []webContextWindow) bool {
	for _, existing := range selected {
		if candidate.start < existing.end && existing.start < candidate.end {
			return true
		}
	}
	return false
}

func displayWebQuery(query string) string {
	query = strings.Join(strings.Fields(query), " ")
	runes := []rune(query)
	if len(runes) > 80 {
		query = string(runes[:80]) + "…"
	}
	return strings.ReplaceAll(query, "”", "」")
}

func truncateWebContent(content string, maxBytes int, notice string) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}

	limit := maxBytes
	appendNotice := maxBytes > len(notice)
	if appendNotice {
		limit -= len(notice)
	}
	for limit > 0 && !utf8.ValidString(content[:limit]) {
		limit--
	}
	content = strings.TrimRight(content[:limit], " \t\r\n")
	if appendNotice {
		content += notice
	}
	return content
}

type webPageLoader func(context.Context, string) (string, error)

func (p *Persona) loadWebPage(ctx context.Context, rawURL string) (string, error) {
	if p.opts.WebBrowserEnable {
		res, err := loadWebPageWithBrowser(ctx, rawURL, p.opts.WebBrowserAddress)
		if err == nil {
			return res, err
		}
		logrus.Errorf("web browser failed,try get:%v", err)
	}
	if _, err := validatePublicURL(ctx, rawURL); err != nil {
		return "", err
	}
	return loadWebPageWithHTTP(publicHTTPClient(ctx))(ctx, rawURL)
}

func loadWebPageWithHTTP(client *http.Client) webPageLoader {
	return func(ctx context.Context, rawURL string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set(
			"User-Agent",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
				"AppleWebKit/537.36 (KHTML, like Gecko) "+
				"Chrome/131.0.0.0 Safari/537.36",
		)

		req.Header.Set(
			"Accept",
			"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		)

		req.Header.Set(
			"Accept-Language",
			"zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7",
		)

		req.Header.Set(
			"Cache-Control",
			"no-cache",
		)

		req.Header.Set(
			"Pragma",
			"no-cache",
		)

		req.Header.Set(
			"Referer",
			"https://www.cn.bing.com/",
		)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("web returned %s", resp.Status)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		return string(body), nil
	}
}

func publicHTTPClient(rc context.Context) *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		_, err := validatePublicURL(rc, req.URL.String())
		return err
	}}
}

func validatePublicURL(ctx context.Context, rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("invalid http(s) URL")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return nil, err
	}
	for _, addr := range addresses {
		if addr.IP.IsLoopback() || addr.IP.IsPrivate() || addr.IP.IsUnspecified() || addr.IP.IsLinkLocalUnicast() {
			return nil, errors.New("private or local addresses are not allowed")
		}
	}
	return u, nil
}
