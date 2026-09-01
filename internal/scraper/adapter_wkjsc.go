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

// wkjsc.swjtu.edu.cn has four explicit news lists on its home page. The
// generic parser also sees the five "详细>>" links in the slideshow summary,
// so it can return fake titles before it reaches the real dated rows.
func init() {
	registerAdapter("wkjsc", SiteAdapter{
		ParseList:    parseAdapter_wkjscList,
		ParseArticle: parseAdapter_wkjscArticle,
	})
}

var wkjscArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var wkjscDateRE = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?`)

func parseAdapter_wkjscList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse wkjsc list: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	add := func(anchor *goquery.Selection, scope *goquery.Selection) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL := cleanURL(resolveURL(baseURL, href))
		if !wkjscArticleURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}

		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 100 {
			return
		}
		date := wkjscDate(scope)
		if index, ok := seen[fullURL]; ok {
			if items[index].Date == "" && date != "" {
				items[index].Date = date
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, ParsedAt: time.Now()})
	}

	// These lists are the source of truth for the page's four visible feeds.
	doc.Find("ul.news_ul li").Each(func(_ int, row *goquery.Selection) {
		add(row.Find("a[href]").First(), row)
	})
	return items, nil
}

func wkjscArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || !wkjscArticlePathRE.MatchString(parsed.Path) {
		return false
	}
	return !isExternalURL(rawURL, baseURL)
}

func wkjscDate(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(scope.Find(".date").First().Text()), " ")
	if text == "" {
		text = strings.Join(strings.Fields(scope.Text()), " ")
	}
	return normalizeWkjscDate(text)
}

func normalizeWkjscDate(text string) string {
	match := wkjscDateRE.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	return normalizeDateParts(match[1], match[2], match[3])
}

func parseAdapter_wkjscArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return article, nil
	}
	if title := strings.TrimSpace(doc.Find("#lb_title, .divtitle").First().Text()); title != "" {
		article.Title = title
	}
	if meta := strings.Join(strings.Fields(doc.Find("#lb_hits").First().Text()), " "); meta != "" {
		if date := normalizeWkjscDate(meta); date != "" {
			article.Date = date
		}
		if article.Source == "" {
			if match := regexp.MustCompile(`来源\s*[:：]?\s*([^\s\[\]]+)`).FindStringSubmatch(meta); match != nil {
				if source := cleanLabelValue(match[1]); source != "" {
					article.Source = source
				}
			}
		}
	}
	return article, nil
}
