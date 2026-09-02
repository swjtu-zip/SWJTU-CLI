package scraper

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var rwxySubPages = []string{
	"xyxw", "dzbg", "xshd", "xsgz/tzgg", "xsgz/xgdt", "xsgz/xzfc", "xsgz/jyxx", "jwgz/jwdt",
	"szll1/cbx", "szll1/ylx", "szll1/zwx", "djgz/djdt", "djgz/xygh", "rcpy/yjsjy", "rcpy/bshldz",
}

// rwxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rwxy", SiteAdapter{
		SubPages:     rwxySubPages,
		ParseList:    parseAdapter_rwxyList,
		ParseArticle: parseAdapter_rwxyArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_rwxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("rwxy parse list: %w", err)
	}
	if rows := doc.Find(".list-news > ul > li"); rows.Length() > 0 {
		items := make([]model.NewsItem, 0, rows.Length())
		seen := make(map[string]bool, rows.Length())
		typ := rwxyListType(doc)
		rows.Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			href := strings.TrimSpace(anchor.AttrOr("href", ""))
			fullURL := cleanURL(resolveURL(baseURL, href))
			if href == "" || !isValidNewsURL(fullURL) || seen[fullURL] {
				return
			}
			// The outer anchor contains an invalid nested More anchor. The
			// headline is the visible text in .time; its date is in the
			// HTML comment immediately before that text.
			timeBlock := row.Find(".time").First()
			title := cleanTitle(strings.TrimSpace(timeBlock.Text()))
			if title == "" || title == "More →" || !isValidNewsTitle(title) {
				return
			}
			dateText, _ := timeBlock.Html()
			date, publishedAt := rwxyDateValues(dateText)
			seen[fullURL] = true
			items = append(items, model.NewsItem{
				Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt,
				Type: typ, ParsedAt: time.Now(),
			})
		})
		return items, nil
	}
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_rwxyArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil || article == nil {
		return article, err
	}
	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if parseErr != nil {
		return article, nil
	}
	if meta := doc.Find("form[name='_newscontent_fromname'] .title .info, form[name='_newscontent_fromname'] .info").First(); meta.Length() > 0 {
		if date, publishedAt := rwxyDateValues(meta.Text()); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		}
	}
	return article, nil
}

var rwxyDateRe = regexp.MustCompile(`20\d{2}\s*(?:[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}日?)(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?`)

func rwxyDateValues(text string) (string, string) {
	match := rwxyDateRe.FindString(strings.Join(strings.Fields(text), " "))
	if match == "" {
		return "", ""
	}
	precise := normalizePublishedAt(match)
	if precise == "" {
		return "", ""
	}
	return precise[:10], precise
}

func rwxyListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".loaction a", ".sidebar .title span", ".sidebar .title"} {
		values := doc.Find(selector)
		for i := values.Length() - 1; i >= 0; i-- {
			value := strings.Join(strings.Fields(values.Eq(i).Text()), " ")
			if value != "" && value != "首页" {
				return value
			}
		}
	}
	return ""
}
