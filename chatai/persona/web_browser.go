package persona

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

const webBrowserTimeout = 30 * time.Second

// loadWebPageWithBrowser renders a page in Chrome and returns the resulting DOM.
// Every HTTP(S) request, including redirects and subresources, is checked before
// Chrome is allowed to issue it so browser mode does not weaken the SSRF guard.
func loadWebPageWithBrowser(parent context.Context, rawURL string, maxBytes int, address string) (string, error) {
	if _, err := validatePublicURL(parent, rawURL); err != nil {
		return "", err
	}

	ctx, cancelTimeout := context.WithTimeout(parent, webBrowserTimeout)
	defer cancelTimeout()

	var allocatorCtx context.Context
	var cancelAllocator context.CancelFunc
	if address = strings.TrimSpace(address); address != "" {
		allocatorCtx, cancelAllocator = chromedp.NewRemoteAllocator(ctx, address)
	} else {
		options := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
		)
		allocatorCtx, cancelAllocator = chromedp.NewExecAllocator(ctx, options...)
	}
	defer cancelAllocator()

	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	var blockedMu sync.Mutex
	var blockedErr error
	chromedp.ListenTarget(browserCtx, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			executorCtx := cdp.WithExecutor(browserCtx, chromedp.FromContext(browserCtx).Target)
			requestURL, err := url.Parse(paused.Request.URL)
			if err == nil && (requestURL.Scheme == "http" || requestURL.Scheme == "https") {
				_, err = validatePublicURL(browserCtx, paused.Request.URL)
			}
			if err != nil {
				blockedMu.Lock()
				if blockedErr == nil {
					blockedErr = fmt.Errorf("blocked browser request to %q: %w", paused.Request.URL, err)
				}
				blockedMu.Unlock()
				_ = fetch.FailRequest(paused.RequestID, "BlockedByClient").Do(executorCtx)
				return
			}
			_ = fetch.ContinueRequest(paused.RequestID).Do(executorCtx)
		}()
	})

	var source, finalURL string
	err := chromedp.Run(browserCtx,
		fetch.Enable(),
		chromedp.Navigate(rawURL),
		chromedp.Location(&finalURL),
		chromedp.OuterHTML("html", &source, chromedp.ByQuery),
	)
	blockedMu.Lock()
	requestErr := blockedErr
	blockedMu.Unlock()
	if requestErr != nil {
		return "", requestErr
	}
	if err != nil {
		return "", fmt.Errorf("browser load failed: %w", err)
	}
	if _, err := validatePublicURL(parent, finalURL); err != nil {
		return "", fmt.Errorf("invalid browser redirect: %w", err)
	}
	if maxBytes > 0 && len(source) > maxBytes {
		source = source[:maxBytes]
	}
	return source, nil
}
