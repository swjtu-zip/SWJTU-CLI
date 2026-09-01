package scraper

import "swjtu-cli/internal/model"

// clxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("clxy", SiteAdapter{
		ParseList:    parseAdapter_clxyList,
		ParseArticle: parseAdapter_clxyArticle,
	})
}

func parseAdapter_clxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_clxyArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
