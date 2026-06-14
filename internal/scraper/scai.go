package scraper

import (
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var scaiDateRe = regexp.MustCompile(`\d{4}/\d{2}/\d{2}`)

// parseScaiNews parses scai.swjtu.edu.cn list pages.
// Structure: <a href="...newsDetail..." title="title"><div class="top-news-item"><div class="calendar"><p class="day">date</p></div><h3>title</h3></div></a>
func parseScaiNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}

	var items []model.NewsItem
	seen := make(map[string]bool)

	doc.Find("a[href*='newsDetail']").Each(func(i int, sel *goquery.Selection) {
		if isInNavArea(sel) {
			return
		}
		href, _ := sel.Attr("href")
		if href == "" {
			return
		}

		// Title from title attr or h3
		title := strings.TrimSpace(sel.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(sel.Find("h3").First().Text())
		}
		if title == "" || !isValidNewsTitle(title) || len(title) > 100 {
			return
		}

		// Date from calendar
		date := ""
		dayText := strings.TrimSpace(sel.Find(".calendar .day").First().Text())
		if dayText != "" {
			// Format: "Fri Jun 12 00:00:00 CST 2026" -> "2026-06-12"
			if m := scaiDateRe.FindString(dayText); m != "" {
				date = strings.ReplaceAll(m, "/", "-")
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
