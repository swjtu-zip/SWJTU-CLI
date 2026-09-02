package scraper

import "swjtu-cli/internal/model"

var rvlSubPages = []string{
	"djgz/xwdt", "rsgz/tzgg", "kxyj/kygg", "kxyj/kylw", "kxyj/kyxm/gjjxm",
	"kxyj/kyzl", "kxyj/ryjl", "xsgz/tzgg", "tzgg", "sysaq", "cpyc",
}

// rvlAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rvl", SiteAdapter{
		SubPages:     rvlSubPages,
		ParseList:    parseAdapter_rvlList,
		ParseArticle: parseAdapter_rvlArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_rvlList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_rvlArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
