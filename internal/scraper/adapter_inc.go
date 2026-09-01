package scraper

import "swjtu-cli/internal/model"

// incAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("inc", SiteAdapter{
		ParseList:    parseAdapter_incList,
		ParseArticle: parseAdapter_incArticle,
	})
}

func parseAdapter_incList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_incArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
