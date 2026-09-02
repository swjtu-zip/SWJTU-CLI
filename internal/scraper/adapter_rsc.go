package scraper

import "swjtu-cli/internal/model"

var rscSubPages = []string{
	"xwtz/xwNews", "xwtz/tzNotice", "rszp/gljqtzjryzphgs", "rszp/shpyryzphgs",
	"xgxz", "xgxz/zcps", "bszn", "bszn/rszc", "bszn/shbx", "bszn/szrc", "cjwt",
}

// rscAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rsc", SiteAdapter{
		SubPages:     rscSubPages,
		ParseList:    parseAdapter_rscList,
		ParseArticle: parseAdapter_rscArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_rscList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_rscArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
