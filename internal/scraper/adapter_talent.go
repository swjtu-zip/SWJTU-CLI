package scraper

import "swjtu-cli/internal/model"

// talentAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("talent", SiteAdapter{
		ParseList:    parseAdapter_talentList,
		ParseArticle: parseAdapter_talentArticle,
	})
}

func parseAdapter_talentList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_talentArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
