package scraper

import (
	"fmt"
	"strings"

	"swjtu-cli/internal/model"
)

// The live cwc host currently protects every route — homepage, historical
// /info articles, column pages, and the search endpoint — behind a 302 to the
// webexp login gateway, so the origin exposes no public list slug that can be
// registered safely and SubPages stays empty. Mark NoIndexFeed so the SDK does
// not synthesise an index feed for the login shell. Keep this adapter strict:
// a 200 response containing the login SPA or another unrecognised page must
// not turn into a successful empty feed.
func init() {
	if site := AllSites["cwc"]; site != nil {
		site.NoIndexFeed = true
	}
	registerAdapter("cwc", SiteAdapter{
		ParseList:    parseCwcNewsList,
		ParseArticle: parseCwcArticle,
	})
}
func parseCwcNewsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	if isCwcLoginShell(html) {
		return nil, fmt.Errorf("cwc: received login SPA shell instead of a news list")
	}
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("cwc: response contains no recognised public news list")
	}
	return items, nil
}

func parseCwcArticle(html []byte, baseURL string) (*model.Article, error) {
	if isCwcLoginShell(html) {
		return nil, fmt.Errorf("cwc: received login SPA shell instead of an article")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(article.Title) == "" ||
		(strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0) {
		return nil, fmt.Errorf("cwc: response is not a recognised public article page")
	}
	return article, nil
}

func isCwcLoginShell(html []byte) bool {
	page := strings.ToLower(string(html))
	if strings.Contains(page, "<title>登录</title>") || strings.Contains(page, "<title>login</title>") {
		return true
	}
	return strings.Contains(page, `<div id="app"></div>`) &&
		(strings.Contains(page, "/login/assets/") || strings.Contains(page, "webexp.swjtu.edu.cn/login"))
}
