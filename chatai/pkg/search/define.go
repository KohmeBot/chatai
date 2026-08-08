package search

import "context"

type Request struct {
	Query string
	Limit int
}

type Response struct {
	Results []SearchResult
}

type SearchResult struct {
	Title   string
	URL     string
	Snippet string
	Summary string
}

type Searcher interface {
	DoRequest(ctx context.Context, req Request) (Response, error)
}
