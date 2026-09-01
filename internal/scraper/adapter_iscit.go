package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// iscit.swjtu.edu.cn uses a 博达 template whose homepage cards link to
// /info/<column>/<id>.htm, while its detail page puts the publication date in
// .newxq-boxy rather than in the common .news_source/.top-info blocks.
func init() {
	registerAdapter("iscit", SiteAdapter{
		ParseList:    parseIscitNews,
		ParseArticle: parseIscitArticle,
	})
}

var (
	iscitArticlePathRe  = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	iscitFullDateRe     = regexp.MustCompile(`(20[0-9]{2})\s*(?:年|[-/.])\s*([0-9]{1,2})\s*(?:月|[-/.])\s*([0-9]{1,2})\s*日?`)
	iscitMonthDayYearRe = regexp.MustCompile(`(?:^|[^0-9])([0-9]{1,2})\s*[-/.]\s*([0-9]{1,2})\s*(20[0-9]{2})(?:$|[^0-9])`)
)

// parseIscitNews only accepts the site's article URL shape. The homepage also
// contains navigation, image/banner, list-return, and external links; those
// must not become news items merely because they have a title attribute.
func parseIscitNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse iscit list: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if iscitIsNonContentAnchor(anchor) {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL := cleanURL(resolveURL(baseURL, href))
		if !iscitIsArticleURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}

		title := iscitListTitle(anchor)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date := iscitListDate(anchor)

		if index, ok := seen[fullURL]; ok {
			if items[index].Title == "" {
				items[index].Title = title
			}
			if items[index].Date == "" && date != "" {
				items[index].Date = date
			}
			return
		}

		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			ParsedAt: time.Now(),
		})
	})

	return items, nil
}

func iscitIsNonContentAnchor(anchor *goquery.Selection) bool {
	for _, selector := range []string{
		"header",
		"nav",
		"footer",
		".nav_top",
		".navigation-down",
		".menu-mc",
		".foot-bg",
		".banner-pc",
		".banner-mc",
	} {
		if anchor.Closest(selector).Length() > 0 {
			return true
		}
	}
	return false
}

func iscitIsArticleURL(fullURL, baseURL string) bool {
	ref, err := url.Parse(strings.TrimSpace(fullURL))
	if err != nil || !iscitArticlePathRe.MatchString(ref.Path) {
		return false
	}
	return !isExternalURL(fullURL, baseURL)
}

func iscitListTitle(anchor *goquery.Selection) string {
	if title := strings.TrimSpace(anchor.AttrOr("title", "")); title != "" {
		return cleanTitle(title)
	}

	for _, selector := range []string{".title", "h1", "h2", "h3", "h4", "h5"} {
		if title := strings.TrimSpace(anchor.Find(selector).First().Text()); title != "" {
			return cleanTitle(title)
		}
	}

	parent := anchor.Parent()
	for depth := 0; parent.Length() > 0 && depth < 4; depth++ {
		if parent.HasClass("title") {
			if title := strings.TrimSpace(parent.Text()); title != "" {
				return cleanTitle(title)
			}
		}
		for _, selector := range []string{".title", "h1", "h2", "h3", "h4", "h5"} {
			if title := strings.TrimSpace(parent.Find(selector).First().Text()); title != "" {
				return cleanTitle(title)
			}
		}
		parent = parent.Parent()
	}

	return cleanTitle(strings.TrimSpace(anchor.Text()))
}

func iscitListDate(anchor *goquery.Selection) string {
	scope := anchor
	for depth := 0; scope.Length() > 0 && depth < 4; depth++ {
		for _, selector := range []string{".date", ".time", "time"} {
			dateBlock := scope.Find(selector).First()
			if dateBlock.Length() > 0 {
				if date := normalizeIscitDate(dateBlock.Text()); date != "" {
					return date
				}
			}
		}
		if scope.HasClass("date") || scope.HasClass("time") || scope.Is("time") {
			if date := normalizeIscitDate(scope.Text()); date != "" {
				return date
			}
		}
		scope = scope.Parent()
	}
	return ""
}

func normalizeIscitDate(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if match := iscitFullDateRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	if match := iscitMonthDayYearRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[3], match[1], match[2])
	}
	return ""
}

func parseIscitArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse iscit article: %w", err)
	}

	// The actual detail template uses h2, which is more authoritative than a
	// document title that may include the site name.
	if title := strings.TrimSpace(doc.Find(".newxq-boxy h2").First().Text()); title != "" {
		article.Title = title
	}

	// In the audited page this p contains: date, an empty span, and 阅读量.
	// Restrict the search to the detail header so dates mentioned in the body
	// or in the related-news sidebar cannot be mistaken for publication time.
	for _, selector := range []string{".newxq-boxy p", ".newxq-boxy .date", ".article-meta", ".news-info"} {
		if date := normalizeIscitDate(doc.Find(selector).First().Text()); date != "" {
			article.Date = date
			break
		}
	}
	if article.Date == "" {
		article.Date = normalizeIscitDate(article.Date)
	}

	return article, nil
}
