package scraper

import "swjtu-cli/internal/model"

var xybSubPages = []string{
	"xwdt1", "xwdt1/djgz", "xwdt1/tzgg", "xwdt1/xwzx", "xwdt1/xwzx/jj", "xwdt1/xwzx/xy",
	"xyzh/qnxyh", "xyzh/xyfc", "xyzh/xyll",
}

// xybAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xyb", SiteAdapter{
		SubPages:     xybSubPages,
		ParseList:    parseAdapter_xybList,
		ParseArticle: parseAdapter_xybArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_xybList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xybArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
