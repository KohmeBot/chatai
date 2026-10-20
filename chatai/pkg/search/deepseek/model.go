package deepseek

import "time"

type WebSearchRequest struct {
	Query       string `json:"query"`
	Count       int    `json:"count"`
	OriQuestion string `json:"ori_question,omitempty"`
}

type WebSearchResponse struct {
	Results []struct {
		RerankScore float64   `json:"rerank_score"`
		Title       string    `json:"title"`
		Url         string    `json:"url"`
		Snippet     string    `json:"snippet"`
		Summary     string    `json:"summary"`
		Content     string    `json:"content"`
		SiteName    string    `json:"site_name"`
		SiteIcon    string    `json:"site_icon"`
		Date        time.Time `json:"date"`
		Authority   float64   `json:"authority"`
	} `json:"results"`
}
