package scraper

import "swjtu-cli/internal/model"

// xybAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xyb", SiteAdapter{
		ParseList:    parseAdapter_xybList,
		ParseArticle: parseAdapter_xybArticle,
	})
}

func parseAdapter_xybList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xybArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
