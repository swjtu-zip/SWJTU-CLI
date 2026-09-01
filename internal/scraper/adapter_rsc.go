package scraper

import "swjtu-cli/internal/model"

// rscAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rsc", SiteAdapter{
		ParseList:    parseAdapter_rscList,
		ParseArticle: parseAdapter_rscArticle,
	})
}

func parseAdapter_rscList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_rscArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
