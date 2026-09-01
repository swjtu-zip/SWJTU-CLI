package scraper

import "swjtu-cli/internal/model"

// mathsAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("maths", SiteAdapter{
		ParseList:    parseAdapter_mathsList,
		ParseArticle: parseAdapter_mathsArticle,
	})
}

func parseAdapter_mathsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_mathsArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
