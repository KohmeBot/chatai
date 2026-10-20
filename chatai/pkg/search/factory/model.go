package factory

import (
	"fmt"
	"github.com/kohmebot/chatai/chatai/pkg/search"
	"github.com/kohmebot/chatai/chatai/pkg/search/bocha"
	"github.com/kohmebot/chatai/chatai/pkg/search/deepseek"
)

func NewSearcher(name string, apiKey string) (search.Searcher, error) {

	switch name {
	case "bocha":
		return &bocha.API{APIKey: apiKey}, nil
	case "deepseek":
		return &deepseek.API{APIKey: apiKey}, nil
	default:
		return nil, fmt.Errorf("unknown searcher name: %s", name)
	}

}
