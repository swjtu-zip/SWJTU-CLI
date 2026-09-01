package scraper

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// dag.swjtu.edu.cn uses a home-page-only list template.  Its article rows are
// not one of the generic list shapes: the day and year-month are separate
// elements in each .articlelist3_tr.
func init() {
	registerAdapter("dag", SiteAdapter{
		ParseList:    parseDagNews,
		ParseArticle: parseDagArticle,
	})
}

var (
	dagArticleHrefRE = regexp.MustCompile(`(?i)(?:^|/)info/\d+/\d+\.(?:html?|shtml?)$`)
	dagYearMonthRE   = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})`)
	dagFullDateRE    = regexp.MustCompile(`(20\d{2})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日?`)
	dagNumericDateRE = regexp.MustCompile(`(20\d{2})\s*[-/.]\s*(\d{1,2})\s*[-/.]\s*(\d{1,2})`)
	dagYearRE        = regexp.MustCompile(`(20\d{2})\s*年`)
	dagMonthDayRE    = regexp.MustCompile(`(\d{1,2})\s*月\s*(\d{1,2})\s*日`)
)

func parseDagNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse dag list html: %w", err)
	}

	var items []model.NewsItem
	seen := make(map[string]bool)
	doc.Find(".articlelist3_tr").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a.articlelist1_a_title[href]").First()
		if anchor.Length() == 0 {
			anchor = row.Find("a[href]").FilterFunction(func(_ int, sel *goquery.Selection) bool {
				href, _ := sel.Attr("href")
				return dagArticleHrefRE.MatchString(href)
			}).First()
		}
		if anchor.Length() == 0 {
			return
		}

		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if !dagArticleHrefRE.MatchString(href) {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}

		fullURL := cleanURL(resolveURL(baseURL, href))
		if seen[fullURL] || !isValidNewsURL(fullURL) {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     dagListDate(row),
			ParsedAt: time.Now(),
		})
	})

	if len(items) == 0 {
		return nil, fmt.Errorf("dag: no article rows found")
	}
	return items, nil
}

func dagListDate(row *goquery.Selection) string {
	yearMonth := strings.TrimSpace(row.Find(".xn-index-tzggny").First().Text())
	day := strings.TrimSpace(row.Find(".xn-index-tzggm").First().Text())
	if yearMonth == "" || day == "" {
		return ""
	}

	ym := dagYearMonthRE.FindStringSubmatch(yearMonth)
	if ym == nil {
		return ""
	}
	dayMatch := regexp.MustCompile(`\d{1,2}`).FindString(day)
	if dayMatch == "" {
		return ""
	}
	return normalizeDateParts(ym[1], ym[2], dayMatch)
}

func parseDagArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse dag article html: %w", err)
	}
	if title := strings.TrimSpace(doc.Find(".article_title").First().Text()); title != "" {
		article.Title = title
	}
	if article.Date == "" {
		article.Date = dagArticleDate(doc, article.Title)
	}
	return article, nil
}

func dagArticleDate(doc *goquery.Document, title string) string {
	// The archive's detail template omits the normal news_source/date block.
	// Prefer fields that are explicitly date-bearing, then use the headline:
	// archive notices commonly put their only calendar date in the title.
	for _, selector := range []string{
		`meta[name="date"]`,
		`meta[name="publishdate"]`,
		`meta[name="publishDate"]`,
		`meta[name="publish_date"]`,
		`meta[property="article:published_time"]`,
		`time[datetime]`,
		`[data-date]`,
	} {
		selection := doc.Find(selector).First()
		if selection.Length() == 0 {
			continue
		}
		for _, value := range []string{
			selection.AttrOr("content", ""),
			selection.AttrOr("datetime", ""),
			selection.AttrOr("data-date", ""),
			selection.Text(),
		} {
			if date := dagDateFromText(value); date != "" {
				return date
			}
		}
	}

	for _, candidate := range []string{
		strings.TrimSpace(doc.Find(".article_title").First().Text()),
		title,
		doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""),
		doc.Find("title").First().Text(),
	} {
		if date := dagDateFromText(candidate); date != "" {
			return date
		}
	}
	return ""
}

func dagDateFromText(text string) string {
	text = strings.TrimSpace(text)
	if match := dagFullDateRE.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	if match := dagNumericDateRE.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}

	// A common archive headline is "2026年暑假值班表（7月20日-9月2日）".
	// The year and month/day are separated by the notice subject, so handle
	// that template without scanning the article body for unrelated dates.
	year := dagYearRE.FindStringSubmatch(text)
	monthDay := dagMonthDayRE.FindStringSubmatch(text)
	if year != nil && monthDay != nil {
		return normalizeDateParts(year[1], monthDay[1], monthDay[2])
	}
	return ""
}
