package persona

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/kohmebot/chatai/chatai/agent"
	"github.com/sirupsen/logrus"
)

func (p *Persona) readWeb(rc *agent.RunContext, rawURL string) (string, error) {
	source, err := p.loadWebPage(rc, rawURL)
	if err != nil {
		return "", err
	}
	return extractUsefulText(source, p.opts.WebMaxBytes)

	//text := regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]+>`).ReplaceAllString(source, " ")
	//return strings.Join(strings.Fields(html.UnescapeString(text)), " "), nil
}

func extractUsefulText(source string, maxBytes int) (string, error) {
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

	if maxBytes > 0 && len(res) > maxBytes {
		res = res[:maxBytes]
	}

	return res, nil
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
