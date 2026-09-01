package scraper

import "swjtu-cli/internal/model"

// xinliAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xinli", SiteAdapter{
		ParseList:    parseAdapter_xinliList,
		ParseArticle: parseAdapter_xinliArticle,
	})
}

func parseAdapter_xinliList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xinliArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
