package scraper

import "swjtu-cli/internal/model"

// dwtzbAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("dwtzb", SiteAdapter{
		ParseList:    parseAdapter_dwtzbList,
		ParseArticle: parseAdapter_dwtzbArticle,
	})
}

func parseAdapter_dwtzbList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_dwtzbArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
