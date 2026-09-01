package scraper

import "swjtu-cli/internal/model"

// hbbAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("hbb", SiteAdapter{
		ParseList:    parseAdapter_hbbList,
		ParseArticle: parseAdapter_hbbArticle,
	})
}

func parseAdapter_hbbList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_hbbArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
