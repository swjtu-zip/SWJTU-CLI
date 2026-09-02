package scraper

import (
	"fmt"

	"swjtu-cli/internal/model"
)

// xgAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xg", SiteAdapter{
		ParseList:    parseAdapter_xgList,
		ParseArticle: parseAdapter_xgArticle,
	})
}

func parseAdapter_xgList(html []byte, baseURL string) ([]model.NewsItem, error) {
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("xg: homepage is a redirect shell; no static news list is available")
	}
	return items, nil
}

func parseAdapter_xgArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
