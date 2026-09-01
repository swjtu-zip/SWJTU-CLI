package scraper

import (
	"fmt"
	"strings"

	"swjtu-cli/internal/model"
)

func init() {
	registerAdapter("cwc", SiteAdapter{
		ParseList:    parseCwcNewsList,
		ParseArticle: parseCwcArticle,
	})
}

// parseCwcNewsList handles the CWC site separately because its current entry
// page can be a login SPA shell rather than a server-rendered news list.
func parseCwcNewsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	if isCwcLoginShell(html) {
		return nil, fmt.Errorf("cwc: received login SPA shell instead of a news list")
	}
	return parseBodaNewsList(html, baseURL)
}

func parseCwcArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticle(html, baseURL)
}

func isCwcLoginShell(html []byte) bool {
	page := strings.ToLower(string(html))
	return strings.Contains(page, `<div id="app"></div>`) &&
		(strings.Contains(page, "/login/assets/") || strings.Contains(page, "<title>登录</title>"))
}
