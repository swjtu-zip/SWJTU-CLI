package scraper

import "swjtu-cli/internal/model"

var xbSubPages = []string{"xxfb/tzgg", "xxfb/xwdt", "cyxz", "fwzn/gflc", "fwzn/glwj", "zdgf/bgswj", "zdgf/xjwj"}

// xbAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xb", SiteAdapter{
		SubPages:     xbSubPages,
		ParseList:    parseAdapter_xbList,
		ParseArticle: parseAdapter_xbArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_xbList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xbArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
