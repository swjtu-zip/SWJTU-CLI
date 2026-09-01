package scraper

import "swjtu-cli/internal/model"

// jzxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("jzxy", SiteAdapter{
		ParseList:    parseAdapter_jzxyList,
		ParseArticle: parseAdapter_jzxyArticle,
	})
}

func parseAdapter_jzxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_jzxyArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
