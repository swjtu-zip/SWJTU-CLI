package scraper

import (
	"fmt"

	"swjtu-cli/internal/model"
)

// journalsAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("journals", SiteAdapter{
		ParseList:    parseAdapter_journalsList,
		ParseArticle: parseAdapter_journalsArticle,
	})
}

func parseAdapter_journalsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("journals: homepage contains no server-rendered news list")
	}
	return items, nil
}

func parseAdapter_journalsArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
