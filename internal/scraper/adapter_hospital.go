package scraper

import "swjtu-cli/internal/model"

// hospitalAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("hospital", SiteAdapter{
		ParseList:    parseAdapter_hospitalList,
		ParseArticle: parseAdapter_hospitalArticle,
	})
}

func parseAdapter_hospitalList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_hospitalArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
