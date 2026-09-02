package scraper

import (
	"fmt"

	"swjtu-cli/internal/model"
)

// sroAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("sro", SiteAdapter{
		ParseList:    parseAdapter_sroList,
		ParseArticle: parseAdapter_sroArticle,
	})
}

func parseAdapter_sroList(html []byte, baseURL string) ([]model.NewsItem, error) {
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("sro: homepage contains no server-rendered news; the search/feed component is dynamic")
	}
	return items, nil
}

func parseAdapter_sroArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
