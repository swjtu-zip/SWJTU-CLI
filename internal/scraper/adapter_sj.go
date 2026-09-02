package scraper

import "swjtu-cli/internal/model"

var sjSubPages = []string{
	"index/xwdt", "gggs", "gzcx/cwsj", "zbjs", "zcfg/gjxgfgzd",
	"zlxz/cwsjxgzl", "zlxz/gcsjxgzl", "zlxz/zxsjxgzl",
}

// sjAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("sj", SiteAdapter{
		SubPages:     sjSubPages,
		ParseList:    parseAdapter_sjList,
		ParseArticle: parseAdapter_sjArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_sjList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_sjArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
