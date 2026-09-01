package scraper

import "swjtu-cli/internal/model"

// scyshjAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("scyshj", SiteAdapter{
		ParseList:    parseAdapter_scyshjList,
		ParseArticle: parseAdapter_scyshjArticle,
	})
}

func parseAdapter_scyshjList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_scyshjArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
