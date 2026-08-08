package bocha

import "time"

type WebSearchRequest struct {
	Query string `json:"query"`
	Count int    `json:"count"`
}

type WebSearchResponse struct {
	Data struct {
		WebPages struct {
			WebSearchUrl          string `json:"webSearchUrl"`
			TotalEstimatedMatches int    `json:"totalEstimatedMatches"`
			Value                 []struct {
				Id               string      `json:"id"`
				Name             string      `json:"name"`
				Url              string      `json:"url"`
				DisplayUrl       string      `json:"displayUrl"`
				Snippet          string      `json:"snippet"`
				SiteName         string      `json:"siteName"`
				SiteIcon         string      `json:"siteIcon"`
				Summary          string      `json:"summary"`
				DatePublished    time.Time   `json:"datePublished"`
				DateLastCrawled  time.Time   `json:"dateLastCrawled"`
				CachedPageUrl    interface{} `json:"cachedPageUrl"`
				Language         interface{} `json:"language"`
				IsFamilyFriendly interface{} `json:"isFamilyFriendly"`
				IsNavigational   interface{} `json:"isNavigational"`
			} `json:"value"`
			SomeResultsRemoved bool `json:"someResultsRemoved"`
		} `json:"webPages"`
	} `json:"data"`
}
