package scraper

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// dqxy.swjtu.edu.cn (电气工程学院): list items are <div class="item"> blocks
// whose .date holds MM-DD without a year; article pages show a bare timestamp
// in .little-tips with no label. The homepage does not use the .item markup,
// so the generic list parser remains as a fallback.
func init() {
	registerAdapter("dqxy", SiteAdapter{
		ParseList:    parseDqxyNews,
		ParseArticle: parseDqxyArticle,
	})
}

var dqxyListDateRe = regexp.MustCompile(`^(\d{1,2})-(\d{1,2})$`)

// mmddYearCompleter assigns years to MM-DD dates of a reverse-chronological
// news list: start from the current year and roll back one year whenever the
// MM-DD value increases (e.g. 01-15 followed by 12-28).
type mmddYearCompleter struct {
	year int
	last int
}

func newMmddYearCompleter() *mmddYearCompleter {
	return &mmddYearCompleter{year: time.Now().Year(), last: -1}
}

func (c *mmddYearCompleter) complete(month, day string) string {
	m, errM := strconv.Atoi(month)
	d, errD := strconv.Atoi(day)
	if errM != nil || errD != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return ""
	}
	md := m*100 + d
	if c.last >= 0 && md > c.last {
		c.year--
	}
	c.last = md
	return fmt.Sprintf("%04d-%02d-%02d", c.year, m, d)
}

func parseDqxyNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	var items []model.NewsItem
	seen := make(map[string]bool)
	years := newMmddYearCompleter()
	doc.Find("div.item").Each(func(_ int, sel *goquery.Selection) {
		if isInNavArea(sel) {
			return
		}
		a := sel.Find("a[href]").First()
		href, _ := a.Attr("href")
		if href == "" {
			return
		}
		title := strings.TrimSpace(sel.Find(".a_block, .line1").First().Text())
		if title == "" {
			title = strings.TrimSpace(a.AttrOr("title", ""))
		}
		title = strings.Join(strings.Fields(title), " ")
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date := ""
		if m := dqxyListDateRe.FindStringSubmatch(strings.TrimSpace(sel.Find(".date").First().Text())); m != nil {
			date = years.complete(m[1], m[2])
		}
		fullURL := cleanURL(resolveURL(baseURL, href))
		if seen[fullURL] || !isValidNewsURL(fullURL) {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, ParsedAt: time.Now()})
	})
	if len(items) == 0 {
		return parseBodaNewsList(html, baseURL)
	}
	return items, nil
}

func parseDqxyArticle(html []byte, baseURL string) (*model.Article, error) {
	art, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if art.Title == "" || art.Date == "" {
		doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
		if docErr == nil {
			if art.Title == "" {
				art.Title = strings.TrimSpace(doc.Find("h4.else-title").First().Text())
			}
			if art.Date == "" {
				tips := strings.TrimSpace(doc.Find(".little-tips").First().Text())
				if m := bareDateRe.FindStringSubmatch(tips); m != nil {
					art.Date = normalizeDateParts(m[1], m[2], m[3])
				}
			}
		}
	}
	return art, nil
}
