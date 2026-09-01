package scraper

import "swjtu-cli/internal/model"

// hzcAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("hzc", SiteAdapter{
		ParseList:    parseAdapter_hzcList,
		ParseArticle: parseAdapter_hzcArticle,
	})
}

func parseAdapter_hzcList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_hzcArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
