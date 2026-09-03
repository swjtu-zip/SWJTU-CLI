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

	typ := strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	// Both the home page and archive pages use li rows. The home page has a
	// small recommendation block in addition to the real news list; URL
	// de-duplication below keeps a row from being archived twice.
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
		fullURL := cleanURL(resolveURL(baseURL, href))
		if !isValidSiteNewsURL(fullURL, baseURL) {
			return
		}

		// Archive rows use .line1; recommendation cards use .line2. Do not
		// use the whole anchor while a scoped title exists because it also
		// contains the date.
		title := ""
		for _, selector := range []string{".line1", ".line2", "p.line2", "h3", "h4"} {
			if value := strings.TrimSpace(sel.Find(selector).First().Text()); value != "" {
				title = value
				break
			}
		}
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

		// The current template calls the date container .time; older home
		// cards use .time-box or a semantic <time> element.
		date := ""
		for _, selector := range []string{".time", ".time-box", "time", ".date"} {
			if value := strings.TrimSpace(sel.Find(selector).First().Text()); value != "" {
				date = value
				break
			}
		}
		if date == "" {
			// Home-page notice tabs use an unclassed sibling span rather than
			// the archive page's .time div.
			sel.Find("span").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
				value := strings.TrimSpace(candidate.Text())
				if bareDateRe.FindString(value) != "" || dateTimeRe.FindString(value) != "" {
					date = value
					return false
				}
				return true
			})
		}
		publishedAt := normalizePublishedAt(date)
		if publishedAt != "" && len(publishedAt) >= 10 {
			date = publishedAt[:10]
		}

		if seen[fullURL] {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	})

	return items, nil
}
