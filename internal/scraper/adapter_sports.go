package scraper

import "swjtu-cli/internal/model"

var sportsSubPages = []string{
	"xwdt", "tzggnew/qb", "dqgzNew/ghgz", "dqgzNew/ltgz",
	"rcpyNew/ggtyjy/jkcs", "rcpyNew/ggtyjy/tzcs",
}

// sportsAdapter keeps this website's existing parser behind its own adapter hook.
func init() {
	registerAdapter("sports", SiteAdapter{
		SubPages:     sportsSubPages,
		ParseList:    parseAdapter_sportsList,
		ParseArticle: parseAdapter_sportsArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_sportsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseSportsNews(html, baseURL)
}

func parseAdapter_sportsArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}
