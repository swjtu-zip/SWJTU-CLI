package scraper

import "swjtu-cli/internal/model"

// glxyAdapter keeps 经济管理学院's list and article hooks independent from
// other 博达 sites. The parser can be tightened here when this site's markup
// changes without changing the shared site registry.
func init() {
	registerAdapter("glxy", SiteAdapter{
		ParseList:    parseGlxyNews,
		ParseArticle: parseGlxyArticle,
	})
}

func parseGlxyNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseGlxyArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
