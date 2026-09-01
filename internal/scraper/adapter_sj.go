package scraper

import "swjtu-cli/internal/model"

// sjAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("sj", SiteAdapter{
		ParseList:    parseAdapter_sjList,
		ParseArticle: parseAdapter_sjArticle,
	})
}

func parseAdapter_sjList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_sjArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
