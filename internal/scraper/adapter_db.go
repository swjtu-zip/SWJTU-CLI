package scraper

import "swjtu-cli/internal/model"

// dbAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("db", SiteAdapter{
		ParseList:    parseAdapter_dbList,
		ParseArticle: parseAdapter_dbArticle,
	})
}

func parseAdapter_dbList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_dbArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
