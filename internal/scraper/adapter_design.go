package scraper

import "swjtu-cli/internal/model"

// designAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("design", SiteAdapter{
		ParseList:    parseAdapter_designList,
		ParseArticle: parseAdapter_designArticle,
	})
}

func parseAdapter_designList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_designArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
