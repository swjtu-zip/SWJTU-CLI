package scraper

import "swjtu-cli/internal/model"

// cmAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("cm", SiteAdapter{
		ParseList:    parseAdapter_cmList,
		ParseArticle: parseAdapter_cmArticle,
	})
}

func parseAdapter_cmList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_cmArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
