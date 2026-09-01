package scraper

import (
	"fmt"
	"net/url"
	pathpkg "path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// The dqxy site uses reverse-numbered static pages: xwdt/qb.htm is page 1,
// while the link labelled page 2 is xwdt/qb/20.htm when there are 21 pages.
// Its list page also has a distinct .tzgg_page_base/.item layout, so keep the
// hooks here instead of routing this site through the generic 博达 parser.
func init() {
	registerAdapter("dqxy", SiteAdapter{
		ParseList:    parseDqxyNews,
		ParseArticle: parseDqxyArticle,
		PageURL:      dqxyPageURL,
	})
}

var (
	dqxyFullDateRe    = regexp.MustCompile(`(20\d{2})\s*(?:年|[-/.])\s*(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*日?`)
	dqxyShortDateRe   = regexp.MustCompile(`^(\d{1,2})\s*[-/.]\s*(\d{1,2})$`)
	dqxyArticlePathRe = regexp.MustCompile(`^/info/[^/]+/\d+\.html?$`)
	dqxyArticleIDRe   = regexp.MustCompile(`^\d+$`)
	dqxyCategoryRe    = regexp.MustCompile(`^(?:【[^】]+】|\[[^]]+\])\s*`)
)

type dqxyNewsRow struct {
	URL       string
	Title     string
	Date      string
	MonthDay  int
	Year      int
	ExactDate bool
}

type dqxyLatestMeta struct {
	Title string
	Date  string
}

// dqxyPageURL maps logical page numbers to the generated filenames used by
// dqxy. Returning page 1 for an unknown total is intentional: without the
// pagination total there is no safe way to reverse-map a later page.
func dqxyPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	if page <= 1 || totalPages <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		filePage = 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func parseDqxyNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse dqxy html: %w", err)
	}

	// The latest-update block repeats a few list entries. It is not used as a
	// second source of items, but its full dates and un-truncated titles repair
	// the corresponding rows in the main list.
	latest := collectDqxyLatestMeta(doc)
	rows := collectDqxyListRows(doc, baseURL)
	if len(rows) == 0 {
		// The homepage uses a separate eight-item "学院新闻" card block;
		// it is outside the list-page .item container and must be handled by
		// the homepage adapter explicitly.
		rows = collectDqxyHomeRows(doc, baseURL)
	}
	if len(rows) == 0 {
		// The home page has no .tzgg_page_base list. Its latest-update block is
		// still a safe, bounded source of article links when those links resolve
		// to /info/<channel>/<id>.htm.
		rows = collectDqxyLatestRows(doc, baseURL)
	}
	if len(rows) == 0 {
		// Keep a narrow future-template fallback: only article links inside the
		// main element qualify, never arbitrary navigation/banner anchors.
		rows = collectDqxyMainArticleRows(doc, baseURL)
	}

	for i := range rows {
		id := dqxyArticleID(rows[i].URL)
		if meta, ok := latest[id]; ok {
			if meta.Title != "" {
				rows[i].Title = meta.Title
			}
			if meta.Date != "" {
				setDqxyDate(&rows[i], meta.Date)
			}
		}
	}
	completeDqxyDates(rows)

	items := make([]model.NewsItem, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.Title == "" || !isValidNewsTitle(row.Title) || row.URL == "" {
			continue
		}
		key := dqxyArticleID(row.URL)
		if key == "" {
			key = row.URL
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, model.NewsItem{
			Title:    row.Title,
			URL:      row.URL,
			Date:     row.Date,
			ParsedAt: time.Now(),
		})
	}
	return items, nil
}

func collectDqxyHomeRows(doc *goquery.Document, baseURL string) []dqxyNewsRow {
	rows := make([]dqxyNewsRow, 0)
	doc.Find(".home-school-news .mid-section > li").Each(func(_ int, row *goquery.Selection) {
		if a := firstDqxyArticleLink(row, baseURL); a != nil {
			if parsed, ok := newDqxyRow(row, a, baseURL); ok {
				rows = append(rows, parsed)
			}
		}
	})
	return rows
}

// collectDqxyListRows scopes the primary extraction to the content list. The
// broad div.item fallback is only used when the known container is absent and
// still requires a valid /info/... article URL.
func collectDqxyListRows(doc *goquery.Document, baseURL string) []dqxyNewsRow {
	var blocks *goquery.Selection
	for _, selector := range []string{
		"main.tzgg_page .tzgg_page_base > div.item",
		".tzgg_page_base > div.item",
	} {
		if found := doc.Find(selector); found.Length() > 0 {
			blocks = found
			break
		}
	}
	if blocks == nil {
		blocks = doc.Find("div.item")
	}

	rows := make([]dqxyNewsRow, 0, blocks.Length())
	blocks.Each(func(_ int, block *goquery.Selection) {
		if isInNavArea(block) {
			return
		}
		if a := firstDqxyArticleLink(block, baseURL); a != nil {
			if row, ok := newDqxyRow(block, a, baseURL); ok {
				rows = append(rows, row)
			}
		}
	})
	return rows
}

func collectDqxyLatestRows(doc *goquery.Document, baseURL string) []dqxyNewsRow {
	latest := doc.Find(".latest-update").First()
	if latest.Length() == 0 {
		return nil
	}
	rows := make([]dqxyNewsRow, 0, latest.Find("li").Length())
	latest.Find("li").Each(func(_ int, li *goquery.Selection) {
		if isInNavArea(li) {
			return
		}
		if a := firstDqxyArticleLink(li, baseURL); a != nil {
			if row, ok := newDqxyRow(li, a, baseURL); ok {
				rows = append(rows, row)
			}
		}
	})
	return rows
}

func collectDqxyMainArticleRows(doc *goquery.Document, baseURL string) []dqxyNewsRow {
	main := doc.Find("main").First()
	if main.Length() == 0 {
		return nil
	}
	rows := make([]dqxyNewsRow, 0)
	main.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if isInNavArea(a) {
			return
		}
		if !isDqxyArticleLink(a, baseURL) {
			return
		}
		if row, ok := newDqxyRow(a, a, baseURL); ok {
			rows = append(rows, row)
		}
	})
	return rows
}

func newDqxyRow(container, a *goquery.Selection, baseURL string) (dqxyNewsRow, bool) {
	href, ok := a.Attr("href")
	if !ok || strings.TrimSpace(href) == "" {
		return dqxyNewsRow{}, false
	}
	fullURL := canonicalDqxyURL(resolveURL(baseURL, href))
	if !isDqxyArticleURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
		return dqxyNewsRow{}, false
	}

	title := dqxyTitle(container, a)
	if title == "" {
		return dqxyNewsRow{}, false
	}

	dateText := dqxyDateText(container)
	if dateText == "" && container != a {
		dateText = dqxyDateText(a)
	}
	row := dqxyNewsRow{URL: fullURL, Title: title}
	setDqxyDate(&row, dateText)
	return row, true
}

func firstDqxyArticleLink(container *goquery.Selection, baseURL string) *goquery.Selection {
	var found *goquery.Selection
	container.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if isDqxyArticleLink(a, baseURL) {
			found = a
			return false
		}
		return true
	})
	return found
}

func isDqxyArticleLink(a *goquery.Selection, baseURL string) bool {
	href, ok := a.Attr("href")
	if !ok || strings.TrimSpace(href) == "" {
		return false
	}
	return isDqxyArticleURL(canonicalDqxyURL(resolveURL(baseURL, href)), baseURL)
}

func dqxyTitle(container, a *goquery.Selection) string {
	title := ""
	if titleSel := container.Find(".a_block, .line1, .desc").First(); titleSel.Length() > 0 {
		title = titleSel.Text()
		if label := strings.TrimSpace(titleSel.Find(".lanmu").First().Text()); label != "" {
			title = strings.TrimSpace(strings.TrimPrefix(title, label))
		}
	}
	if title == "" {
		title = a.AttrOr("title", "")
	}
	if title == "" {
		title = a.Text()
	}
	title = strings.Join(strings.Fields(title), " ")
	title = dqxyCategoryRe.ReplaceAllString(title, "")
	return cleanTitle(strings.TrimSpace(title))
}

func dqxyDateText(container *goquery.Selection) string {
	for _, selector := range []string{".date", "time", ".time"} {
		if value := strings.TrimSpace(container.Find(selector).First().Text()); value != "" {
			return strings.Join(strings.Fields(value), " ")
		}
	}
	for current := container.Parent(); current.Length() > 0; current = current.Parent() {
		for _, selector := range []string{".date", "time", ".time"} {
			if value := strings.TrimSpace(current.Find(selector).First().Text()); value != "" {
				return strings.Join(strings.Fields(value), " ")
			}
		}
		if current.Is("li, .item, main, body") {
			break
		}
	}
	return ""
}

func collectDqxyLatestMeta(doc *goquery.Document) map[string]dqxyLatestMeta {
	result := make(map[string]dqxyLatestMeta)
	doc.Find(".latest-update li").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("a[href]").First()
		if a.Length() == 0 {
			return
		}
		id := dqxyArticleID(a.AttrOr("href", ""))
		if id == "" {
			return
		}
		meta := dqxyLatestMeta{Title: dqxyTitle(a, a)}
		if date := parseDqxyFullDate(li.Find("time").First().Text()); date != "" {
			meta.Date = date
		}
		if meta.Title != "" || meta.Date != "" {
			result[id] = meta
		}
	})
	return result
}

func setDqxyDate(row *dqxyNewsRow, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if match := dqxyFullDateRe.FindStringSubmatch(text); match != nil {
		month, _ := strconv.Atoi(match[2])
		day, _ := strconv.Atoi(match[3])
		if date := normalizeDateParts(match[1], match[2], match[3]); date != "" {
			row.Date = date
			row.Year, row.MonthDay = yearAndMonthDay(match[1], month, day)
			row.ExactDate = true
			return
		}
	}
	if match := dqxyShortDateRe.FindStringSubmatch(text); match != nil {
		month, _ := strconv.Atoi(match[1])
		day, _ := strconv.Atoi(match[2])
		if month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			row.MonthDay = month*100 + day
		}
	}
}

func yearAndMonthDay(year string, month, day int) (int, int) {
	parsedYear, _ := strconv.Atoi(year)
	return parsedYear, month*100 + day
}

// completeDqxyDates fills MM-DD only when the same HTML page supplies a
// full-date anchor (the latest-update block or a full date in a row). It never
// uses the process clock as a publication-year guess. If no anchor exists,
// Date remains empty and can safely be resolved from the article page later.
func completeDqxyDates(rows []dqxyNewsRow) {
	anchor := -1
	for i := range rows {
		if rows[i].ExactDate {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		return
	}

	year, lastMonthDay := rows[anchor].Year, rows[anchor].MonthDay
	for i := anchor + 1; i < len(rows); i++ {
		if rows[i].ExactDate {
			year, lastMonthDay = rows[i].Year, rows[i].MonthDay
			continue
		}
		if rows[i].MonthDay == 0 {
			continue
		}
		if rows[i].MonthDay > lastMonthDay {
			year--
		}
		rows[i].Year = year
		rows[i].Date = fmt.Sprintf("%04d-%02d-%02d", year, rows[i].MonthDay/100, rows[i].MonthDay%100)
		lastMonthDay = rows[i].MonthDay
	}

	year, nextMonthDay := rows[anchor].Year, rows[anchor].MonthDay
	for i := anchor - 1; i >= 0; i-- {
		if rows[i].ExactDate {
			year, nextMonthDay = rows[i].Year, rows[i].MonthDay
			continue
		}
		if rows[i].MonthDay == 0 {
			continue
		}
		if rows[i].MonthDay < nextMonthDay {
			year++
		}
		rows[i].Year = year
		rows[i].Date = fmt.Sprintf("%04d-%02d-%02d", year, rows[i].MonthDay/100, rows[i].MonthDay%100)
		nextMonthDay = rows[i].MonthDay
	}
}

func parseDqxyFullDate(text string) string {
	match := dqxyFullDateRe.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return ""
	}
	return normalizeDateParts(match[1], match[2], match[3])
}

func isDqxyArticleURL(rawURL, baseURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" || !dqxyArticlePathRe.MatchString(strings.ToLower(u.Path)) {
		return false
	}
	if baseURL == "" {
		return true
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" {
		return false
	}
	return strings.EqualFold(u.Hostname(), base.Hostname())
}

func canonicalDqxyURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return cleanURL(rawURL)
	}
	u.Path = pathpkg.Clean(u.Path)
	u.RawQuery = ""
	u.Fragment = ""
	u.RawFragment = ""
	return cleanURL(u.String())
}

func dqxyArticleID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	name := pathpkg.Base(u.Path)
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".html"), ".htm")
	if !dqxyArticleIDRe.MatchString(name) {
		return ""
	}
	return name
}

func parseDqxyArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse dqxy article html: %w", err)
	}
	art, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	if title := strings.TrimSpace(doc.Find("h4.else-title").First().Text()); title != "" {
		art.Title = strings.Join(strings.Fields(title), " ")
	}
	// .little-tips is the dqxy detail-page metadata line. Prefer it even if a
	// wider generic fallback happened to find a different event date in the
	// page's surrounding markup.
	if date := parseDqxyFullDate(doc.Find(".little-tips").First().Text()); date != "" {
		art.Date = date
	} else if date := parseDqxyFullDate(art.Date); date != "" {
		art.Date = date
	}
	return art, nil
}
