package scraper

import "swjtu-cli/internal/model"

// xczxAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xczx", SiteAdapter{
		ParseList:    parseAdapter_xczxList,
		ParseArticle: parseAdapter_xczxArticle,
	})
}

func parseAdapter_xczxList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xczxArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
