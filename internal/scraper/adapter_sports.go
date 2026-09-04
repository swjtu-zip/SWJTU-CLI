package scraper

import (
	"fmt"
	"strings"

	"swjtu-cli/internal/model"
)

const sportsCanonicalBaseURL = "https://sports.swjtu.edu.cn"

// sportsSubPages contains the article-bearing list pages exposed by the
// current navigation, its notification tabs, and the homepage service links.
//
// The pages deliberately left out of this list are not news feeds: profile
// pages (教师名录/体育之星), team and club introductions, static introductions,
// empty research pages, and external-only course/academic-link pages.  The
// source uses the same .news-list markup for some of those pages, so the route
// audit cannot be replaced by a selector-only heuristic.
var sportsSubPages = []string{
	"xwdt",
	"tzggnew/qb",
	"tzggnew/dzbg",
	"tzggnew/jyjx",
	"tzggnew/qtjs",
	"tzggnew/xsgz",
	"tzggnew/jyzp",
	"tzggnew/gsgg",
	"dqgzNew/tzgz",
	"dqgzNew/ghgz",
	"dqgzNew/ltgz",
	"rcpyNew/ggtyjy/jkcs",
	"rcpyNew/ggtyjy/tzcs",
	"xsgzNew/txhd",
	"xsgzNew/zdwj",
	"xytyNew/qtjs",
	"xytyNew/tyst/sthd",
	"ydxy",
	"zsjyNew/zsgz",
	"zsjyNew/jygz",
}

// sportsAdapter keeps this website's independently audited parser behind its
// own adapter hook.  The registry still contains an old HTTP address, while
// the live site redirects/canonicalizes to HTTPS; keep that correction local.
func init() {
	if site := AllSites["sports"]; site != nil {
		site.BaseURL = sportsCanonicalBaseURL
	}
	registerAdapter("sports", SiteAdapter{
		SubPages:     sportsSubPages,
		ParseList:    parseAdapter_sportsList,
		ParseArticle: parseAdapter_sportsArticle,
		PageURL:      sportsPageURL,
		TotalPages:   sportsTotalPages,
	})
}

func parseAdapter_sportsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseSportsNews(html, baseURL)
}

func parseAdapter_sportsArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseSportsArticle(html, baseURL)
}

func sportsPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		if page != 1 {
			return ""
		}
		return baseURL + "/"
	}
	if !sportsKnownFeed(slug) {
		return ""
	}
	return reverseBodaPageURL(baseURL, slug, page, totalPages, extension)
}

func sportsKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	for _, route := range sportsSubPages {
		if route == slug {
			return true
		}
	}
	return false
}

func sportsTotalPages(slug string) (int, error) {
	site := AllSites["sports"]
	if site == nil {
		return 0, fmt.Errorf("sports: site registration is missing")
	}
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if !sportsKnownFeed(slug) {
		return 0, fmt.Errorf("sports: %q is not an audited article-list route", slug)
	}

	listURL := sportsPageURL(site.BaseURL, slug, 1, 0, ".htm")
	body, err := fetch(listURL)
	if err != nil {
		listURL = sportsPageURL(site.BaseURL, slug, 1, 0, ".html")
		body, err = fetch(listURL)
		if err != nil {
			return 0, fmt.Errorf("sports: fetch first page for %s: %w", slug, err)
		}
	}
	return sportsTotalPagesFromHTML(body, slug)
}

func sportsTotalPagesFromHTML(raw []byte, slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if !sportsKnownFeed(slug) {
		return 0, fmt.Errorf("sports: %q is not an audited article-list route", slug)
	}
	doc, err := parseSportsDocument(raw)
	if err != nil {
		return 0, err
	}
	if sportsResponseLooksUnavailable(doc) {
		return 0, fmt.Errorf("sports: %s response is an error, login, or verification page", slug)
	}
	if sportsIsHomepage(doc) {
		return 0, fmt.Errorf("sports: %s resolved to the homepage, not a list page", slug)
	}
	list := sportsNewsList(doc)
	if list.Length() == 0 || sportsIsExcludedListPage(doc) {
		return 0, fmt.Errorf("sports: %s response is not a supported article list", slug)
	}
	if items, rows := sportsArchiveItems(list, sportsListType(doc), sportsCanonicalBaseURL); len(items) == 0 && rows > 0 {
		return 0, fmt.Errorf("sports: %s article list contains no local article rows", slug)
	}
	if total := parseTotalPages(raw); total > 0 {
		return total, nil
	}
	// A real list can be a one-page feed and may render only disabled pager
	// labels.  It is safe to use one here because sportsPageURL rejects page 2
	// when the total is one.
	return 1, nil
}
