package scraper

import "swjtu-cli/internal/model"

// oalmAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("oalm", SiteAdapter{
		ParseList:    parseAdapter_oalmList,
		ParseArticle: parseAdapter_oalmArticle,
	})
}

func parseAdapter_oalmList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_oalmArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
