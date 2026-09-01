package scraper

import "swjtu-cli/internal/model"

// sportsAdapter keeps this website's existing parser behind its own adapter hook.
func init() {
	registerAdapter("sports", SiteAdapter{
		ParseList:    parseAdapter_sportsList,
		ParseArticle: parseAdapter_sportsArticle,
	})
}

func parseAdapter_sportsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseSportsNews(html, baseURL)
}

func parseAdapter_sportsArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
