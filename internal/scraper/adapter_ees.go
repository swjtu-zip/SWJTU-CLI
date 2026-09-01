package scraper

import "swjtu-cli/internal/model"

// eesAdapter keeps this website's existing parser behind its own adapter hook.
func init() {
	registerAdapter("ees", SiteAdapter{
		ParseList:    parseAdapter_eesList,
		ParseArticle: parseAdapter_eesArticle,
	})
}

func parseAdapter_eesList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseEesNews(html, baseURL)
}

func parseAdapter_eesArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
