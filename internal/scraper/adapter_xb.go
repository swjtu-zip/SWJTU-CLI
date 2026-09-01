package scraper

import "swjtu-cli/internal/model"

// xbAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xb", SiteAdapter{
		ParseList:    parseAdapter_xbList,
		ParseArticle: parseAdapter_xbArticle,
	})
}

func parseAdapter_xbList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xbArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
