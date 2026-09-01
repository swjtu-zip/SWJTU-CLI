package scraper

import "swjtu-cli/internal/model"

// ygbAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("ygb", SiteAdapter{
		ParseList:    parseAdapter_ygbList,
		ParseArticle: parseAdapter_ygbArticle,
	})
}

func parseAdapter_ygbList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_ygbArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
