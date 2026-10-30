package search

import "context"

type Request struct {
	Query       string
	OriQuestion string
	Limit       int
}

type Response struct {
	Results []SearchResult
}

type SearchResult struct {
	Title   string
	URL     string
	Snippet string
	Summary string
	Date    string
}

type Searcher interface {
	DoRequest(ctx context.Context, req Request) (Response, error)
}
