package scraper

import "swjtu-cli/internal/model"

// ggAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("gg", SiteAdapter{
		ParseList:    parseAdapter_ggList,
		ParseArticle: parseAdapter_ggArticle,
	})
}

func parseAdapter_ggList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_ggArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
