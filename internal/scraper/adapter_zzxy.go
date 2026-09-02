package scraper

import (
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var zzxySubPages = []string{"xyyw", "xwdt", "tzgg", "xsgz/xsdt", "djjgh/djgz/djfc", "cgzs", "xsgg"}

// zzxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("zzxy", SiteAdapter{
		SubPages:     zzxySubPages,
		ParseList:    parseAdapter_zzxyList,
		ParseArticle: parseAdapter_zzxyArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_zzxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}

	// zzxy's list is a plain <ul class="list">. The generic Boda parser does
	// not include this selector, and its fallback sees the article link's
	// sibling-free .tit div, losing the adjacent .time and [section] tag.
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".right ul.list > li").Each(func(_ int, row *goquery.Selection) {
		a := row.Find(".tit a[href]").First()
		if a.Length() == 0 {
			return
		}
		title := cleanTitle(a.Text())
		href := strings.TrimSpace(a.AttrOr("href", ""))
		if title == "" || href == "" || !isValidNewsTitle(title) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, href))
		if seen[fullURL] || !isValidNewsURL(fullURL) {
			return
		}

		dateText := strings.TrimSpace(row.Find(".time").Text())
		date, publishedAt := zzxyDateValues(dateText)
		typ := strings.TrimSpace(row.Find(".tag").Text())
		typ = strings.TrimSpace(strings.Trim(typ, "[]【】"))
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt,
			Type: typ, ParsedAt: time.Now(),
		})
	})
	if len(items) > 0 {
		return items, nil
	}
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_zzxyArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil || article == nil {
		return article, err
	}

	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if parseErr == nil {
		if dateText := strings.TrimSpace(doc.Find(".author").First().Text()); dateText != "" {
			if date, publishedAt := zzxyDateValues(dateText); date != "" {
				article.Date = date
				article.PublishedAt = publishedAt
			}
		}
	}
	return article, nil
}

var zzxyDateRe = regexp.MustCompile(`20\d{2}\s*(?:[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}日?)?(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?`)

func zzxyDateValues(text string) (string, string) {
	match := zzxyDateRe.FindString(strings.Join(strings.Fields(text), " "))
	if match == "" {
		return "", ""
	}
	precise := normalizePublishedAt(match)
	if precise == "" {
		return "", ""
	}
	return precise[:10], precise
}
