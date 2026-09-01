package scraper

import "swjtu-cli/internal/model"

// kfjtAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("kfjt", SiteAdapter{
		ParseList:    parseAdapter_kfjtList,
		ParseArticle: parseAdapter_kfjtArticle,
	})
}

func parseAdapter_kfjtList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_kfjtArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
