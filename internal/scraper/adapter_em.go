package scraper

import "swjtu-cli/internal/model"

// emAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("em", SiteAdapter{
		ParseList:    parseAdapter_emList,
		ParseArticle: parseAdapter_emArticle,
	})
}

func parseAdapter_emList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_emArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
