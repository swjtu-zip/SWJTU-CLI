package scraper

import "swjtu-cli/internal/model"

// rvlAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rvl", SiteAdapter{
		ParseList:    parseAdapter_rvlList,
		ParseArticle: parseAdapter_rvlArticle,
	})
}

func parseAdapter_rvlList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_rvlArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
