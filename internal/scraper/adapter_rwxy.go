package scraper

import "swjtu-cli/internal/model"

var rwxySubPages = []string{
	"xyxw", "dzbg", "xshd", "xsgz/tzgg", "xsgz/xgdt", "xsgz/jyxx", "jwgz/jwdt",
	"szll1/cbx", "szll1/ylx", "szll1/zwx", "djgz/djdt", "djgz/xygh", "rcpy/yjsjy", "rcpy/bshldz",
}

// rwxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rwxy", SiteAdapter{
		SubPages:     rwxySubPages,
		ParseList:    parseAdapter_rwxyList,
		ParseArticle: parseAdapter_rwxyArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_rwxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_rwxyArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
