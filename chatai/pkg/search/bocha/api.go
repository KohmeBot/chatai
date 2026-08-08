package bocha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/kohmebot/chatai/chatai/pkg/search"
	"io"
	"net/http"
)

const webSearchURL = "https://api.bocha.cn/v1/web-search"

type API struct {
	APIKey string
}

func (a *API) DoRequest(ctx context.Context, req search.Request) (search.Response, error) {
	rsp, err := a.doRequest(ctx, WebSearchRequest{
		Query: req.Query,
		Count: req.Limit,
	})

	if err != nil {
		return search.Response{}, err
	}

	resp := search.Response{}

	for _, v := range rsp.Data.WebPages.Value {
		resp.Results = append(resp.Results, search.SearchResult{
			Title:   v.Name,
			URL:     v.Url,
			Snippet: v.Snippet,
			Summary: v.Summary,
		})
	}

	return resp, nil

}

func (a *API) doRequest(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	var result WebSearchResponse

	// 1. 序列化请求参数
	body, err := json.Marshal(request)
	if err != nil {
		return result, fmt.Errorf("marshal web search request: %w", err)
	}

	// 2. 创建 HTTP Request
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		webSearchURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return result, fmt.Errorf("create web search request: %w", err)
	}

	// 3. 设置请求头
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 4. 发请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("do web search request: %w", err)
	}
	defer resp.Body.Close()

	// 5. 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, fmt.Errorf("read web search response: %w", err)
	}

	// 6. 处理非 2xx
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf(
			"web search request failed: status=%d body=%s",
			resp.StatusCode,
			string(respBody),
		)
	}

	// 7. JSON 反序列化
	if err := json.Unmarshal(respBody, &result); err != nil {
		return result, fmt.Errorf(
			"unmarshal web search response: %w, body=%s",
			err,
			string(respBody),
		)
	}

	return result, nil
}
