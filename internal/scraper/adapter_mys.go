package scraper

import "swjtu-cli/internal/model"

// mysAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("mys", SiteAdapter{
		ParseList:    parseAdapter_mysList,
		ParseArticle: parseAdapter_mysArticle,
	})
}

func parseAdapter_mysList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_mysArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
