package scraper

import "swjtu-cli/internal/model"

// chemAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("chem", SiteAdapter{
		ParseList:    parseAdapter_chemList,
		ParseArticle: parseAdapter_chemArticle,
	})
}

func parseAdapter_chemList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_chemArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
