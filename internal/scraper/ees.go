package scraper

import (
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// parseEesNews parses ees.swjtu.edu.cn list pages.
// Structure: <a href="..."><p class="title fw fs20">title</p><p class="line3 desc">description</p></a>
func parseEesNews(html []byte, baseURL string) ([]model.NewsItem, error) {
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

		// Title from <p class="title"> or <p class="title fw fs20">
		title := strings.TrimSpace(sel.Find("p.title, p[class*='title']").First().Text())
		if title == "" {
			// Fallback: try h3 or h4
			title = strings.TrimSpace(sel.Find("h3, h4").First().Text())
		}
		if title == "" {
			return
		}
		// Clean and validate
		title = cleanTitle(title)
		if !isValidNewsTitle(title) || len(title) > 100 {
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
			Date:     "",
			ParsedAt: time.Now(),
		})
	})

	return items, nil
}
