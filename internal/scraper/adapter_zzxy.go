package scraper

import "swjtu-cli/internal/model"

// zzxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("zzxy", SiteAdapter{
		ParseList:    parseAdapter_zzxyList,
		ParseArticle: parseAdapter_zzxyArticle,
	})
}

func parseAdapter_zzxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_zzxyArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
