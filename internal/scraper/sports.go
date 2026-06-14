package scraper

import (
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// parseSportsNews parses sports.swjtu.edu.cn list pages.
// Structure: <time>2026/04/07</time> <a href="..."><img></a> <p class="line2">title</p>
func parseSportsNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}

	var items []model.NewsItem
	seen := make(map[string]bool)

	// Find list items that contain both <a> and <p class="line2">
	doc.Find("li").Each(func(i int, sel *goquery.Selection) {
		if isInNavArea(sel) {
			return
		}
		a := sel.Find("a[href*='info/']").First()
		if a.Length() == 0 {
			return
		}
		href, _ := a.Attr("href")
		if href == "" {
			return
		}

		// Title from <p class="line2">
		title := strings.TrimSpace(sel.Find("p.line2").Text())
		if title == "" {
			// Fallback: try <a> title attr or text
			title = strings.TrimSpace(a.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(a.Text())
			}
		}
		// Clean and validate
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) || len(title) > 100 {
			return
		}

		// Date from <time>
		date := strings.TrimSpace(sel.Find("time").Text())
		// Normalize date format
		date = strings.ReplaceAll(date, "/", "-")

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
