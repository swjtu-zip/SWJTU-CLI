package scraper

import "swjtu-cli/internal/model"

// fdcAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("fdc", SiteAdapter{
		ParseList:    parseAdapter_fdcList,
		ParseArticle: parseAdapter_fdcArticle,
	})
}

func parseAdapter_fdcList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_fdcArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
