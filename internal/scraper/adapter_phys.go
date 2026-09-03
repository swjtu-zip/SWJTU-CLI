package scraper

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var physSubPages = []string{
	"xwgg/xydt",
	"kxyj/xsdt",
	"rcpy/bkspy",
	"rcpy/yjspy",
	"xsfz/gqtgz",
	"xsfz/xgdt",
	"xsfz/xgtz",
	"xygz/xyjy",
	"dqgz/jjgz",
	"dqgz/pyfz",
	"dqgz/xxjy",
}

// physAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("phys", SiteAdapter{
		SubPages:     physSubPages,
		ParseList:    parseAdapter_physList,
		ParseArticle: parseAdapter_physArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   physTotalPages,
	})
}

func parseAdapter_physList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("phys: parse list HTML: %w", err)
	}
	if physIsHomepage(doc) {
		items := physHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("phys: homepage contains no local article modules")
		}
		return items, nil
	}

	// The live archive uses .newlist1.  Once that container is present, an
	// empty result is a valid empty tab; falling through to the generic parser
	// would make a broken/partial response look successful or leak navigation.
	if doc.Find(".newlist1").Length() > 0 {
		items := parsePhysInfoRows(doc, baseURL)
		physAnnotateItems(doc, items, baseURL, physPageType(doc))
		return items, nil
	}
	if items := parsePhysInfoRows(doc, baseURL); len(items) > 0 {
		physAnnotateItems(doc, items, baseURL, physPageType(doc))
		return items, nil
	}

	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	physAnnotateItems(doc, items, baseURL, physPageType(doc))
	return items, nil
}

func physTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if !physKnownFeed(slug) {
		return 0, fmt.Errorf("phys: %q is not an article-list route", slug)
	}
	body, err := fetch(fmt.Sprintf("http://phys.swjtu.edu.cn/%s.htm", slug))
	if err != nil {
		return 0, fmt.Errorf("phys: fetch first page for %s: %w", slug, err)
	}
	return physTotalPagesFromHTML(body, slug)
}

func physKnownFeed(slug string) bool {
	for _, known := range physSubPages {
		if slug == known {
			return true
		}
	}
	return false
}

func physTotalPagesFromHTML(raw []byte, slug string) (int, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("phys: parse pagination for %s: %w", slug, err)
	}
	if physIsHomepage(doc) || doc.Find(".newlist1").Length() == 0 {
		return 0, fmt.Errorf("phys: %s response is not a list page", slug)
	}
	if total := parseTotalPages(raw); total > 0 {
		return total, nil
	}
	// A recognized archive with no pager is a one-page feed, not an unknown
	// page count.  Any page after it is rejected by reverseBodaPageURL.
	return 1, nil
}

func parseAdapter_physArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil {
		return nil, err
	}
	if physHasLegacyPlaceholder(article.Date) || physHasLegacyPlaceholder(article.PublishedAt) {
		article.Date = ""
		article.PublishedAt = ""
	}
	if article.Type == "" {
		if doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html))); parseErr == nil {
			article.Type = physPageType(doc)
		}
	}
	return article, nil
}

var physShortDateRE = regexp.MustCompile(`(?:^|\D)(\d{1,2})\s*[-/.]\s*(\d{1,2})(?:\D|$)`)

const physLegacyPlaceholderDate = "2017-04-24"

func parsePhysInfoRows(doc *goquery.Document, baseURL string) []model.NewsItem {
	if doc == nil {
		return nil
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("a[href*='info/']").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if fullURL == "" || seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) {
			return
		}
		row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
		if row.Length() == 0 {
			row = anchor.Parent()
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(row.Find(".xw-ul-tt, .line1, .txts, .news-title, h3, h4, p").First().Text())
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := physDateValues(row)
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			ParsedAt:    time.Now(),
		})
	})
	return items
}

func physIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).FilterFunction(func(_ int, sel *goquery.Selection) bool {
		return strings.TrimSpace(sel.AttrOr("content", "")) == "1"
	}).Length() > 0 && doc.Find(".home1, .home3, .home4").Length() > 0
}

func physHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	doc.Find(".home1 a[href*='info/'], .home3 a[href*='info/'], .home4 a[href*='info/']").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if fullURL == "" || !isValidSiteNewsURL(fullURL, baseURL) {
			return
		}
		row := anchor.Closest("li, dt, dd, .item, .news-item, .list-item").First()
		if row.Length() == 0 {
			row = anchor
		}
		title := physRowTitle(anchor, row)
		if !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := physDateValues(row)
		typ := physModuleType(row)
		if index, exists := seen[fullURL]; exists {
			// The home1 hero carousel may contain a shortened/duplicate copy of
			// the right-hand list. Keep the complete title and precise date.
			if physTitleIsTruncated(items[index].Title) && !physTitleIsTruncated(title) {
				items[index].Title = title
			}
			if items[index].Date == "" && date != "" {
				items[index].Date = date
				items[index].PublishedAt = publishedAt
			}
			if items[index].Type == "" && typ != "" {
				items[index].Type = typ
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	return items
}

func physRowTitle(anchor, row *goquery.Selection) string {
	if row != nil && row.Length() > 0 {
		for _, selector := range []string{"h3", "h4", ".xw-ul-tt", ".news-title"} {
			if title := cleanTitle(row.Find(selector).First().Text()); title != "" {
				return title
			}
		}
	}
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	return cleanTitle(anchor.AttrOr("title", anchor.Text()))
}

func physModuleType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	if column := row.Closest(".left, .right").First(); column.Length() > 0 {
		if typ := physCleanType(column.Find(".title .con h3").First().Text()); typ != "" {
			return typ
		}
	}
	if section := row.Closest(".home1, .home3, .home4").First(); section.Length() > 0 {
		return physCleanType(section.Find(".title .con h3").First().Text())
	}
	return ""
}

func physTitleIsTruncated(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasSuffix(value, "...") || strings.HasSuffix(value, "…") || strings.HasSuffix(value, "...")
}

func physAnnotateItems(doc *goquery.Document, items []model.NewsItem, baseURL, pageType string) {
	for i := range items {
		anchor := physFindAnchor(doc, items[i].URL, baseURL)
		if anchor != nil && anchor.Length() > 0 {
			row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
			if row.Length() == 0 {
				row = anchor.Parent()
			}
			if date, publishedAt := physDateValues(row); date != "" {
				items[i].Date = date
				items[i].PublishedAt = publishedAt
			}
			if typ := physRowType(row); typ != "" {
				items[i].Type = typ
			}
		}
		if items[i].Type == "" {
			items[i].Type = pageType
		}
		if date, publishedAt := physNormalizeDate(items[i].Date); date != "" {
			items[i].Date = date
			if items[i].PublishedAt == "" && publishedAt != "" {
				items[i].PublishedAt = publishedAt
			}
		}
	}
}

func physFindAnchor(doc *goquery.Document, itemURL, baseURL string) *goquery.Selection {
	var found *goquery.Selection
	doc.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if cleanURL(resolveURL(baseURL, anchor.AttrOr("href", ""))) == itemURL {
			found = anchor
			return false
		}
		return true
	})
	return found
}

func physPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if title := strings.TrimSpace(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).Last().AttrOr("content", "")); title != "" && title != "首页" {
		return physCleanType(title)
	}
	if typ := bodaArticleSectionType(doc, ""); typ != "" {
		return typ
	}
	for _, selector := range []string{".list-title", ".list-head", ".ny-title", ".main_titT", "h1", "h2"} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			value := strings.Join(strings.Fields(selection.Eq(i).Text()), " ")
			if value != "" && value != "首页" && value != "新闻公告" && value != "当前位置" {
				return value
			}
		}
	}
	return ""
}

func physRowType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	for _, selector := range []string{".type", ".category", ".column", ".lanmu", ".source", ".tag"} {
		if value := physCleanType(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func physCleanType(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "[]【】〖〗")
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "栏目："), "栏目:"))
}

func physDateValues(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	raw := ""
	for _, selector := range []string{".time", ".date", "time", "em", ".xw-ul-date"} {
		raw = strings.TrimSpace(row.Find(selector).First().Text())
		if raw != "" {
			break
		}
	}
	if raw == "" {
		row.Find("p, span").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
			value := strings.TrimSpace(candidate.Text())
			if normalizePublishedAt(value) != "" {
				raw = value
				return false
			}
			return true
		})
	}
	if raw == "" {
		raw = strings.TrimSpace(row.Text())
	}
	return physNormalizeDate(raw)
}

func physNormalizeDate(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		if physHasLegacyPlaceholder(publishedAt) {
			return "", ""
		}
		if publishedAtHasClock(publishedAt) {
			return publishedAt[:10], publishedAt
		}
		return publishedAt[:10], ""
	}
	if match := physShortDateRE.FindStringSubmatch(raw); len(match) == 3 {
		return fmt.Sprintf("%02s-%02s", match[1], match[2]), ""
	}
	return raw, ""
}

func physHasLegacyPlaceholder(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), physLegacyPlaceholderDate)
}
