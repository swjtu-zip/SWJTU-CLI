package scraper

import (
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// parseEseNews parses ese.swjtu.edu.cn list pages.
// Two structures:
// 1. s2 section: <a><div class="s2-date"><b>06-09</b><span>/ 2026</span></div><p>title</p></a>
// 2. s6 section: <a><div class="pic">...</div><div class="s6-tx"><h3>title</h3><p>date</p></div></a>
func parseEseNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}

	var items []model.NewsItem
	seen := make(map[string]bool)

	doc.Find("a[href*='info/']").Each(func(i int, sel *goquery.Selection) {
		if isInNavArea(sel) {
			return
		}
		href, _ := sel.Attr("href")
		if href == "" || !strings.Contains(href, ".htm") {
			return
		}

		title := ""
		date := ""

		// Try s6-tx pattern first: <h3>title</h3><p>date</p>
		h3 := sel.Find(".s6-tx h3, h3").First()
		if h3.Length() > 0 {
			title = strings.TrimSpace(h3.Text())
			// Date from <p> in s6-tx
			pDate := sel.Find(".s6-tx p, .s5-tx p").First()
			if pDate.Length() > 0 {
				dateText := strings.TrimSpace(pDate.Text())
				if datePattern.MatchString(dateText) {
					date = dateText
				}
			}
		}

		// Fallback: s2 pattern with <p>title</p>
		if title == "" {
			pTag := sel.Find("p").First()
			if pTag.Length() > 0 {
				title = strings.TrimSpace(pTag.Text())
			}
		}

		if title == "" || !isValidNewsTitle(title) || len(title) > 100 {
			return
		}

		// Date from s2-date if not already set
		if date == "" {
			dateBlock := sel.Find(".s2-date, .s3-date, .s1-date").First()
			if dateBlock.Length() > 0 {
				day := strings.TrimSpace(dateBlock.Find("b").Text())
				year := strings.TrimSpace(dateBlock.Find("span").Text())
				year = strings.TrimPrefix(year, "/")
				year = strings.TrimSpace(year)
				if day != "" && year != "" {
					date = year + "-" + day
				}
			}
		}

		fullURL := cleanURL(resolveURL(baseURL, href))
		if seen[fullURL] || !isValidNewsURL(fullURL) {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			ParsedAt: time.Now(),
		})
	})

	return items, nil
}
