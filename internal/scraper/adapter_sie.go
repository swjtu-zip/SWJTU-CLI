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
		ParseList:    parseAdapter_sieList,
		ParseArticle: parseAdapter_sieArticle,
	})
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
	add := func(anchor, scope *goquery.Selection) {
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
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, ParsedAt: time.Now()})
	}

	doc.Find(".index-row1 .c1 .list li").Each(func(_ int, row *goquery.Selection) {
		add(row.Find("a[href]").First(), row)
	})
	doc.Find(".index-row1 .c2 .list dl").Each(func(_ int, row *goquery.Selection) {
		add(row.Find("dd a[href]").First(), row)
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("sie: no article rows found")
	}
	return items, nil
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
			break
		}
	}
	return article, nil
}
