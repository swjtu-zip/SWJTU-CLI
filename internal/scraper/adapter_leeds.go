package scraper

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// leedsSubPages contains the article-bearing tabs confirmed in the current
// Leeds navigation and list pages.  Profile/contact/complaint pages are
// intentionally not feeds.
var leedsSubPages = []string{
	"index/xyxw",
	"xytz/dzbg",
	"xytz/jwjx",
	"xytz/xszc",
	"xxgs/dzbg",
	"xxgs/jwjx",
	"xxgs/xszc",
	"index/hdyg",
	"djysz/djyd",
	"djysz/jszj",
}

// Leeds has its own article template.  In particular, the publish date is
// rendered in .m-textq1 .info .date instead of one of the metadata blocks
// understood by the generic 博达 article parser.  Keep both hooks attached to
// the site so future Leeds list changes do not have to be folded into the
// shared parser.
func init() {
	registerAdapter("leeds", SiteAdapter{
		SubPages:     leedsSubPages,
		ParseList:    parseLeedsNews,
		ParseArticle: parseLeedsArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseLeedsNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse leeds list html: %w", err)
	}
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}

	// The Leeds list template exposes the section name in the breadcrumb, but
	// the shared Boda list parser deliberately leaves Type empty.  It also
	// preserves source date text verbatim.  Enrich both fields here, including
	// homepage rows whose section is determined from their own module.
	pageType := leedsPageType(doc)
	for i := range items {
		if date, publishedAt := leedsItemDate(doc, items[i].URL, items[i].Date); date != "" {
			items[i].Date = date
			items[i].PublishedAt = publishedAt
		}
		if items[i].Type == "" {
			items[i].Type = leedsItemType(doc, items[i].URL)
		}
		if items[i].Type == "" {
			items[i].Type = pageType
		}
	}
	return items, nil
}

func parseLeedsArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse leeds article html: %w", err)
	}

	// Scope the lookup to the article header.  The same page contains dates in
	// the “最新阅读” sidebar, which must never replace the publish date.
	if date, publishedAt := leedsArticleDate(doc); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	return article, nil
}

func leedsArticleDate(doc *goquery.Document) (string, string) {
	for _, selector := range []string{
		".m-textq1 > .info .date",
		".m-textq1 .info .date",
	} {
		date := strings.TrimSpace(doc.Find(selector).First().Text())
		if date == "" {
			continue
		}
		if publishedAt := normalizePublishedAt(date); publishedAt != "" {
			return publishedAt[:10], publishedAt
		}
		if match := bareDateRe.FindStringSubmatch(date); match != nil {
			if normalized := normalizeDateParts(match[1], match[2], match[3]); normalized != "" {
				return normalized, normalized
			}
		}
	}
	return "", ""
}

func leedsPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if typ := bodaArticleSectionType(doc, ""); typ != "" {
		return typ
	}
	for _, selector := range []string{".list-title", ".list-head", ".m-textq1 > h1", ".m-textq1 > h2", ".m-textq1 > h3"} {
		candidate := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if candidate != "" && candidate != "首页" {
			return candidate
		}
	}
	return ""
}

func leedsItemType(doc *goquery.Document, itemURL string) string {
	anchor := leedsFindItemAnchor(doc, itemURL)
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
	if row.Length() == 0 {
		row = anchor.Parent()
	}
	for _, selector := range []string{".type", ".category", ".column", ".lanmu", ".source", ".tag"} {
		if value := leedsCleanType(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func leedsFindItemAnchor(doc *goquery.Document, itemURL string) *goquery.Selection {
	var found *goquery.Selection
	doc.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		for _, base := range []string{"http://leeds.swjtu.edu.cn", "https://leeds.swjtu.edu.cn"} {
			resolved := cleanURL(resolveURL(base, anchor.AttrOr("href", "")))
			if resolved == itemURL {
				found = anchor
				return false
			}
		}
		return true
	})
	return found
}

func leedsItemDate(doc *goquery.Document, itemURL, fallback string) (string, string) {
	raw := ""
	if anchor := leedsFindItemAnchor(doc, itemURL); anchor != nil && anchor.Length() > 0 {
		row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
		if row.Length() == 0 {
			row = anchor.Parent()
		}
		for _, selector := range []string{".date", ".time", "time", ".xw-ul-date", "em"} {
			raw = strings.TrimSpace(row.Find(selector).First().Text())
			if raw != "" {
				break
			}
		}
	}
	if raw == "" {
		raw = fallback
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	return strings.TrimSpace(raw), ""
}

func leedsCleanType(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "[]【】〖〗")
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "栏目："), "栏目:"))
}
