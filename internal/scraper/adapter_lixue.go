package scraper

import "swjtu-cli/internal/model"

// lixueAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("lixue", SiteAdapter{
		ParseList:    parseAdapter_lixueList,
		ParseArticle: parseAdapter_lixueArticle,
	})
}

func parseAdapter_lixueList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_lixueArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
