package scraper

import "swjtu-cli/internal/model"

// rwxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rwxy", SiteAdapter{
		ParseList:    parseAdapter_rwxyList,
		ParseArticle: parseAdapter_rwxyArticle,
	})
}

func parseAdapter_rwxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_rwxyArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
