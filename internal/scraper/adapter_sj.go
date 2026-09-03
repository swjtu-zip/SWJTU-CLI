package scraper

import (
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var sjSubPages = []string{
	"index/xwdt", "gggs", "gzcx/cwsj", "zbjs", "zcfg/gjxgfgzd",
	"zlxz/cwsjxgzl", "zlxz/gcsjxgzl", "zlxz/zxsjxgzl",
}

// sjAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("sj", SiteAdapter{
		SubPages:     sjSubPages,
		ParseList:    parseAdapter_sjList,
		ParseArticle: parseAdapter_sjArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   sjTotalPages,
	})
}

func parseAdapter_sjList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("sj: parse list: %w", err)
	}
	rows := doc.Find(".list-cell, .list-box .list-col")
	if rows.Length() == 0 {
		return nil, fmt.Errorf("sj: response has no audited list rows")
	}
	typ := strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find(".list-cell-tt, .list-col-tt, a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if !isValidSiteNewsURL(fullURL, baseURL) || seen[fullURL] {
			return
		}
		title := cleanTitle(strings.TrimSpace(anchor.AttrOr("title", "")))
		if title == "" {
			title = cleanTitle(anchor.Text())
		}
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		dateText := row.Find(".list-time, .list-col-time, time, .date, .time").First().Text()
		publishedAt := normalizePublishedAt(dateText)
		date := publishedAt
		if len(date) >= 10 {
			date = date[:10]
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	return items, nil
}

func parseAdapter_sjArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil {
		return nil, err
	}
	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if parseErr != nil {
		return article, nil
	}
	if article.Type == "" {
		article.Type = strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	}
	sjAppendOutsideAttachments(article, doc, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func sjTotalPages(slug string) (int, error) {
	site := AllSites["sj"]
	if site == nil {
		return 0, fmt.Errorf("sj: site registration is missing")
	}
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	known := false
	for _, route := range sjSubPages {
		if route == slug {
			known = true
			break
		}
	}
	if !known {
		return 0, fmt.Errorf("sj: %q is not an audited article-list route", slug)
	}
	body, err := fetch(reverseBodaPageURL(site.BaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("sj: fetch %s: %w", slug, err)
	}
	if total := parseTotalPages(body); total > 0 {
		return total, nil
	}
	if items, parseErr := parseAdapter_sjList(body, site.BaseURL); parseErr == nil && len(items) > 0 {
		return 1, nil
	}
	return 0, fmt.Errorf("sj: %s has no pagination or article rows", slug)
}

func sjAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) || link.Closest(".v_news_content, #vsb_content_501").Length() > 0 {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if !isValidNewsURL(resolved) || strings.Contains(resolved, "/info/") || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
			return
		}
		name := strings.TrimSpace(link.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(link.Text())
		}
		if name == "" {
			name = href
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}
