package scraper

import (
	"fmt"

	"swjtu-cli/internal/model"
)

// jwcAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("jwc", SiteAdapter{
		ParseList:    parseAdapter_jwcList,
		ParseArticle: parseAdapter_jwcArticle,
	})
}

func parseAdapter_jwcList(html []byte, baseURL string) ([]model.NewsItem, error) {
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("jwc: homepage contains no server-rendered news; the VATUU feed is dynamic")
	}
	return items, nil
}

func parseAdapter_jwcArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
