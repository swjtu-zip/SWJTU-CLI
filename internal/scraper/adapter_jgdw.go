package scraper

import "swjtu-cli/internal/model"

// jgdwAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("jgdw", SiteAdapter{
		ParseList:    parseAdapter_jgdwList,
		ParseArticle: parseAdapter_jgdwArticle,
	})
}

func parseAdapter_jgdwList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_jgdwArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
