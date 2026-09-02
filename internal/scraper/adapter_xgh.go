package scraper

import "swjtu-cli/internal/model"

var xghSubPages = []string{"xwzx1", "tzgg", "jcdt", "jgfc", "szyl", "ghdj", "gzzd", "hdyx1"}

// xghAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xgh", SiteAdapter{
		SubPages:     xghSubPages,
		ParseList:    parseAdapter_xghList,
		ParseArticle: parseAdapter_xghArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_xghList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xghArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
