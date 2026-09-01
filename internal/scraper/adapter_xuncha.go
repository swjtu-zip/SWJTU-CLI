package scraper

import "swjtu-cli/internal/model"

// xunchaAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xuncha", SiteAdapter{
		ParseList:    parseAdapter_xunchaList,
		ParseArticle: parseAdapter_xunchaArticle,
	})
}

func parseAdapter_xunchaList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xunchaArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
