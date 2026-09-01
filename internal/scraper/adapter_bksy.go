package scraper

import "swjtu-cli/internal/model"

// bksyAdapter keeps this website's existing parser behind its own adapter hook.
func init() {
	registerAdapter("bksy", SiteAdapter{
		ParseList:    parseAdapter_bksyList,
		ParseArticle: parseAdapter_bksyArticle,
		PageURL:      bksyPageURL,
	})
}

func parseAdapter_bksyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBksyNews(html, baseURL)
}

func parseAdapter_bksyArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
