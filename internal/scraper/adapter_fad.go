package scraper

import (
	"fmt"

	"swjtu-cli/internal/model"
)

// fadAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("fad", SiteAdapter{
		ParseList:    parseAdapter_fadList,
		ParseArticle: parseAdapter_fadArticle,
	})
}

func parseAdapter_fadList(html []byte, baseURL string) ([]model.NewsItem, error) {
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("fad: homepage is a redirect/landing page without a static news list")
	}
	return items, nil
}

func parseAdapter_fadArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
