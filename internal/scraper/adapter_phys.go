package scraper

import "swjtu-cli/internal/model"

// physAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("phys", SiteAdapter{
		ParseList:    parseAdapter_physList,
		ParseArticle: parseAdapter_physArticle,
	})
}

func parseAdapter_physList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_physArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
