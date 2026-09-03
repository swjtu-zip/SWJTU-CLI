package scraper

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

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
		TotalPages:   sportsTotalPages,
	})
}

func parseAdapter_sportsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseSportsNews(html, baseURL)
}

func parseAdapter_sportsArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil {
		return nil, err
	}
	if article.Type == "" {
		if doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html))); parseErr == nil {
			article.Type = strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
		}
	}
	return article, nil
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
	known := false
	for _, route := range sportsSubPages {
		if route == slug {
			known = true
			break
		}
	}
	if !known {
		return 0, fmt.Errorf("sports: %q is not an audited article-list route", slug)
	}
	body, err := fetch(reverseBodaPageURL(site.BaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("sports: fetch %s: %w", slug, err)
	}
	if total := parseTotalPages(body); total > 0 {
		return total, nil
	}
	if items, parseErr := parseSportsNews(body, site.BaseURL); parseErr == nil && len(items) > 0 {
		return 1, nil
	}
	return 0, fmt.Errorf("sports: %s has no pagination or article rows", slug)
}
