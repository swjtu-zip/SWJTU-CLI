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

// sie.swjtu.edu.cn has two different homepage news blocks. They both use
// /info/<column>/<id>.htm links, but only the surrounding block carries the
// date; a document-wide fallback loses dates and may mix in utility links.
func init() {
	registerAdapter("sie", SiteAdapter{
		SubPages:     sieSubPages,
		ParseList:    parseAdapter_sieList,
		ParseArticle: parseAdapter_sieArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   sieTotalPages,
	})
}

var sieSubPages = []string{
	"xyxw",
	"tzgg",
	"xysh/xshd",
	"cglx/pxxw",
	"xzzx/xqzb",
	"index/xsst",
}

var sieArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var sieDateRE = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?`)

func parseAdapter_sieList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse sie list: %w", err)
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	add := func(anchor, scope *goquery.Selection, typ string) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		parsed, parseErr := url.Parse(fullURL)
		if parseErr != nil || !sieArticlePathRE.MatchString(parsed.Path) || isExternalURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(scope.Find(".desc").First().Text())
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 240 {
			return
		}
		date := sieListDate(scope)
		if index, ok := seen[fullURL]; ok {
			if items[index].Date == "" && date != "" {
				items[index].Date = date
				items[index].PublishedAt = normalizePublishedAt(date)
			}
			if items[index].Type == "" && typ != "" {
				items[index].Type = typ
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: normalizePublishedAt(date), Type: typ, ParsedAt: time.Now()})
	}

	doc.Find(".index-row1 .c1 .list li").Each(func(_ int, row *goquery.Selection) {
		add(row.Find("a[href]").First(), row, sieSectionType(doc, row))
	})
	doc.Find(".index-row1 .c2 .list dl").Each(func(_ int, row *goquery.Selection) {
		add(row.Find("dd a[href]").First(), row, sieSectionType(doc, row))
	})

	// Archive pages use a separate inner-text-list template. Keep the scan
	// scoped to that content region; the same /info/ URLs also occur in the
	// global navigation and footer.
	doc.Find(".inner-text-list > ul > li, .inner-text-list ul > li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find(".text h6 a[href], .text a[href], a[href]").First()
		add(anchor, row, sieSectionType(doc, row))
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("sie: no article rows found")
	}
	return items, nil
}

func sieSectionType(doc *goquery.Document, row *goquery.Selection) string {
	if row != nil && row.Length() > 0 {
		for _, selector := range []string{".section-title", ".list-title", ".inner-tit h6", ".righttop span:first-child", ".righttop > span:first-child"} {
			value := strings.Join(strings.Fields(row.Closest(".inner-page, .info, .content, .right-con").Find(selector).First().Text()), " ")
			if value != "" && value != "首页" {
				return value
			}
		}
	}
	if doc != nil {
		for _, selector := range []string{`meta[name="pageTitle"]`, ".inner-tit h6", ".righttop > span:first-child"} {
			found := doc.Find(selector).First()
			value := strings.TrimSpace(found.AttrOr("content", ""))
			if value == "" {
				value = strings.Join(strings.Fields(found.Text()), " ")
			}
			if value != "" && value != "首页" {
				return value
			}
		}
	}
	return ""
}

func sieTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	known := false
	for _, route := range sieSubPages {
		if route == slug {
			known = true
			break
		}
	}
	if !known {
		return 0, fmt.Errorf("sie: %q is not an audited article-list route", slug)
	}
	site := AllSites["sie"]
	if site == nil {
		return 0, fmt.Errorf("sie: site registration is missing")
	}
	var body []byte
	var err error
	for _, ext := range []string{".htm", ".html"} {
		body, err = fetch(reverseBodaPageURL(site.BaseURL, slug, 1, 0, ext))
		if err == nil {
			break
		}
	}
	if err != nil {
		return 0, fmt.Errorf("sie: fetch %s: %w", slug, err)
	}
	if _, err := parseAdapter_sieList(body, site.BaseURL); err != nil {
		return 0, fmt.Errorf("sie: %s is not a valid list page: %w", slug, err)
	}
	if total := parseTotalPages(body); total > 0 {
		return total, nil
	}
	return 1, nil
}

func sieListDate(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	if match := sieDateRE.FindStringSubmatch(strings.Join(strings.Fields(scope.Find(".date").First().Text()), " ")); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	yearMonth := strings.Join(strings.Fields(scope.Find("dt .d").First().Text()), " ")
	day := strings.Join(strings.Fields(scope.Find("dt .y").First().Text()), " ")
	ym := regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})`).FindStringSubmatch(yearMonth)
	if ym == nil || day == "" {
		return ""
	}
	dayMatch := regexp.MustCompile(`\d{1,2}`).FindString(day)
	return normalizeDateParts(ym[1], ym[2], dayMatch)
}

func parseAdapter_sieArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if docErr != nil {
		return article, nil
	}
	if title := strings.TrimSpace(doc.Find(".detail-tit").First().Text()); title != "" {
		article.Title = title
	}
	for _, selector := range []string{".detail-meta", ".detail-main .date", ".news_source", ".top-info", "time[datetime]"} {
		scope := doc.Find(selector).First()
		if scope.Length() == 0 {
			continue
		}
		text := strings.Join(strings.Fields(scope.Text()), " ")
		if value := scope.AttrOr("datetime", ""); value != "" {
			text += " " + value
		}
		if match := sieDateRE.FindStringSubmatch(text); match != nil {
			article.Date = normalizeDateParts(match[1], match[2], match[3])
			article.PublishedAt = normalizePublishedAt(text)
			break
		}
	}
	if article.Type == "" {
		article.Type = sieSectionType(doc, nil)
	}
	return article, nil
}
