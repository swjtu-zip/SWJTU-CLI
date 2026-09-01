package scraper

import (
	"fmt"

	"swjtu-cli/internal/model"
)

// ottAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("ott", SiteAdapter{
		ParseList:    parseAdapter_ottList,
		ParseArticle: parseAdapter_ottArticle,
	})
}

func parseAdapter_ottList(html []byte, baseURL string) ([]model.NewsItem, error) {
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	filtered := items[:0]
	for _, item := range items {
		// The homepage mixes the institute's own articles with links copied
		// from the university news site. Only same-site entries belong to this
		// adapter; otherwise stale university links become false articles for
		// the technology-transfer feed.
		if !isExternalURL(item.URL, baseURL) {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("ott: homepage contains no same-site news entries")
	}
	return filtered, nil
}

func parseAdapter_ottArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
