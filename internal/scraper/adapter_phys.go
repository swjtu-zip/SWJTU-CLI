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
	})
}

func parseAdapter_physList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("phys: parse list HTML: %w", err)
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

func parseAdapter_physArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil {
		return nil, err
	}
	if article.Type == "" {
		if doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html))); parseErr == nil {
			article.Type = physPageType(doc)
		}
	}
	return article, nil
}

var physShortDateRE = regexp.MustCompile(`(?:^|\D)(\d{1,2})\s*[-/.]\s*(\d{1,2})(?:\D|$)`)

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
			if publishedAt != "" {
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
