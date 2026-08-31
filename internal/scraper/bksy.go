package scraper

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

func bksyPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	if page <= 1 || totalPages <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, totalPages-page+1, extension)
}

// parseBksyNews parses the public undergraduate school website.
//
// The site uses the standard Visual SiteBuilder article URLs, but its list
// markup is different from the other sites in this repository:
// category pages use ul.right-list, while index.htm uses #content-1.
func parseBksyNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}

	var items []model.NewsItem
	seen := make(map[string]bool)

	listItems := doc.Find("#content-1 > ul > li")
	if listItems.Length() == 0 {
		listItems = doc.Find("ul.right-list > li")
	}
	if listItems.Length() > 0 {
		listItems.Each(func(_ int, sel *goquery.Selection) {
			addBksyNewsItem(&items, seen, sel, baseURL)
		})
		return items, nil
	}

	// Keep a small fallback for future template changes. The class is used by
	// the current homepage and still scopes extraction to article links.
	doc.Find("a.line1.item[href], a[href*='info/']").Each(func(_ int, sel *goquery.Selection) {
		addBksyNewsItem(&items, seen, sel, baseURL)
	})

	return items, nil
}

func addBksyNewsItem(items *[]model.NewsItem, seen map[string]bool, sel *goquery.Selection, baseURL string) {
	if isInNavArea(sel) {
		return
	}
	a := sel
	if _, ok := sel.Attr("href"); !ok {
		a = sel.Find("a[href]").First()
	}
	if a.Length() == 0 {
		return
	}

	href, _ := a.Attr("href")
	if href == "" {
		return
	}

	title := strings.TrimSpace(a.Find(".content, .line1.news, .line1").First().Text())
	if title == "" {
		title = strings.TrimSpace(a.AttrOr("title", ""))
	}
	if title == "" {
		title = strings.TrimSpace(a.Text())
	}
	title = cleanTitle(title)
	if title == "" || utf8.RuneCountInString(title) > 100 || isBksyNavigationTitle(title) || !isValidNewsTitle(title) {
		return
	}

	date := strings.TrimSpace(a.Find("time, .time").First().Text())
	date = strings.Join(strings.Fields(date), " ")
	if len(date) > 20 || strings.Count(date, " ") > 3 {
		date = ""
	}

	fullURL := cleanURL(resolveURL(baseURL, href))
	if seen[fullURL] || !isValidNewsURL(fullURL) {
		return
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     date,
		ParsedAt: time.Now(),
	})
}

func isBksyNavigationTitle(title string) bool {
	switch title {
	case "全部", "教学运行", "教学建设", "教师发展", "通识教育", "实践教学", "实验教学", "创新创业", "质量建设", "管理规定":
		return true
	default:
		return false
	}
}
