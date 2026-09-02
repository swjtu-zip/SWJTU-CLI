package scraper

import "swjtu-cli/internal/model"

var xczxSubPages = []string{
	"tzxw/tzgg", "tzxw/xwdt", "tzxw/xydt", "xsyt/xsfx", "xsyt/xsjl", "xsyt/zjtd",
	"ktyj/kfkt", "ktyj/yjcg", "ktyj/yjxm", "cyzx/xmch", "cyzx/zxfw",
}

// xczxAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xczx", SiteAdapter{
		SubPages:     xczxSubPages,
		ParseList:    parseAdapter_xczxList,
		ParseArticle: parseAdapter_xczxArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_xczxList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xczxArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
