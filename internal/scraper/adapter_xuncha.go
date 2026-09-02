package scraper

import "swjtu-cli/internal/model"

var xunchaSubPages = []string{"gzdt", "zcfg", "zcfg/tlztxx", "xxgongkai", "zyjh"}

// xunchaAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xuncha", SiteAdapter{
		SubPages:     xunchaSubPages,
		ParseList:    parseAdapter_xunchaList,
		ParseArticle: parseAdapter_xunchaArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_xunchaList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xunchaArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
