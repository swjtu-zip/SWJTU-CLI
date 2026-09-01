package scraper

import "swjtu-cli/internal/model"

// newsAdapter keeps this website's existing parser behind its own adapter hook.
func init() {
	registerAdapter("news", SiteAdapter{
		ParseList:    parseAdapter_newsList,
		ParseArticle: parseAdapter_newsArticle,
	})
}

func parseAdapter_newsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_newsArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
