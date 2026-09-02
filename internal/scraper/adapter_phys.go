package scraper

import "swjtu-cli/internal/model"

var physSubPages = []string{
	"xwgg/xydt",
	"kxyj/xsdt",
	"kxyj/kytd",
	"rcpy/bkspy",
	"rcpy/yjspy",
	"xsfz/gqtgz",
	"xsfz/xgdt",
	"xsfz/xgtz",
	"xygz/xyjy",
	"dqgz/jjgz",
	"dqgz/pyfz",
	"dqgz/xxjy",
}

// physAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("phys", SiteAdapter{
		SubPages:     physSubPages,
		ParseList:    parseAdapter_physList,
		ParseArticle: parseAdapter_physArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_physList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_physArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
