package scraper

import "swjtu-cli/internal/model"

var talentSubPages = []string{"xwzx/rczc", "xwzx/tzgg", "xwzx/xwdt", "xwzx/xzzx", "jrjd/jrjdsy"}

// talentAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("talent", SiteAdapter{
		SubPages:     talentSubPages,
		ParseList:    parseAdapter_talentList,
		ParseArticle: parseAdapter_talentArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_talentList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_talentArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
