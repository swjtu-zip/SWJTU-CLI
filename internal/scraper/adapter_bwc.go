package scraper

import "swjtu-cli/internal/model"

// bwcAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("bwc", SiteAdapter{
		ParseList:    parseAdapter_bwcList,
		ParseArticle: parseAdapter_bwcArticle,
	})
}

func parseAdapter_bwcList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_bwcArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}
