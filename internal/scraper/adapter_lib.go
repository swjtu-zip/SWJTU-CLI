package scraper

import (
	"fmt"

	"swjtu-cli/internal/model"
)

// libAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("lib", SiteAdapter{
		ParseList:    parseAdapter_libList,
		ParseArticle: parseAdapter_libArticle,
	})
}

func parseAdapter_libList(html []byte, baseURL string) ([]model.NewsItem, error) {
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("lib: homepage is a Vue application shell without static news rows")
	}
	return items, nil
}

func parseAdapter_libArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
