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
		SubPages:     dqxySubPages,
		ParseList:    parseDqxyNews,
		ParseArticle: parseDqxyArticle,
		PageURL:      dqxyPageURL,
		TotalPages:   dqxyTotalPages,
	})
}

// dqxySubPages contains the site's actual news/notice list entry points. The
// site has two student-work trees (the current *_more tree and the older
// xsgz1 tree); both are linked by the live home-page navigation and expose
// independent article archives.
var dqxySubPages = []string{
	"xwdt/qb",
	"xwdt/xyxw",
	"xwdt/djdt",
	"tzgg/qb",
	"tzgg/dzbg",
	"tzgg/bksjy",
	"tzgg/yjsjy",
	"tzgg/kygz",
	"tzgg/xsgz",
	"tzgg/syjx",
	"tzgg/gsgg",
	"tzgg/qt",
	"xsgz_more/tzgg",
	"xsgz_more/xsdj",
	"xsgz_more/xfjs",
	"xsgz_more/xgdt",
	"xsgz_more/xsfc",
	"xsgz1/xsdj",
	"xsgz1/xsfc1",
	"dqgz1/ghgz",
	"dqgz1/ltgz",
	"dqgz1/tzgz",
	"cyxz/zlwj/djgz",
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
	Type      string
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
// dqxy. A later page cannot be reverse-mapped without a known, valid total;
// return an empty URL instead of repeating the newest page or clamping to the
// oldest page.
func dqxyPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	if page < 1 {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages <= 1 || page > totalPages {
		return ""
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func dqxyTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if !dqxyKnownSubPage(slug) {
		return 0, fmt.Errorf("dqxy: %q is not an audited article-list route", slug)
	}
	site := AllSites["dqxy"]
	if site == nil {
		return 0, fmt.Errorf("dqxy: site registration is missing")
	}

	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(dqxyPageURL(site.BaseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if total := dqxyTotalPagesFromHTML(body); total > 0 {
			return total, nil
		}
		lastErr = fmt.Errorf("dqxy: %s has no recognized pagination", slug)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("dqxy: unable to read page count for %s", slug)
	}
	return 0, lastErr
}

func dqxyKnownSubPage(slug string) bool {
	for _, known := range dqxySubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func dqxyTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil || doc.Find(".tzgg_page_base").Length() == 0 {
		return 0
	}
	if total := parseTotalPages(raw); total > 0 {
		return total
	}
	if doc.Find(".p_pages").Length() > 0 {
		// The page controls may contain only the disabled logical page 1 on a
		// legal empty/single-page list. It is still exactly one page.
		return 1
	}
	return 0
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
			Type:     row.Type,
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
				if parsed.Type == "" {
					parsed.Type = "学院新闻"
				}
				rows = append(rows, parsed)
			}
		}
	})
	return rows
}

// collectDqxyListRows scopes the primary extraction to the content list. The
// fallback is limited to the document's main content. In particular, do
// not scan arbitrary div.item nodes: home-page/navigation templates can use
// that class for unrelated blocks containing article-looking links.
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
		blocks = doc.Find("main .item")
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
	row := dqxyNewsRow{URL: fullURL, Title: title, Type: dqxyRowType(container)}
	setDqxyDate(&row, dateText)
	return row, true
}

func dqxyRowType(container *goquery.Selection) string {
	if container == nil || container.Length() == 0 {
		return ""
	}
	typeText := strings.TrimSpace(container.Find(".lanmu").First().Text())
	if typeText == "" {
		return ""
	}
	typeText = strings.Join(strings.Fields(typeText), " ")
	typeText = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(typeText, "【"), "["))
	typeText = strings.TrimSuffix(strings.TrimSuffix(typeText, "】"), "]")
	return strings.TrimSpace(typeText)
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
	if typ := dqxyArticleType(doc); typ != "" {
		art.Type = typ
	}
	art.Images = filterDqxyImages(art.Images)
	appendDqxyAttachments(art, doc, baseURL)
	return art, nil
}

// The legacy dqxy template places CMS file-type icons and spacer GIFs inside
// the same writing container as real article images. They are presentation
// artifacts, not downloadable article resources; keep them in the rendered
// HTML but do not make the archive request them.
func filterDqxyImages(images []string) []string {
	filtered := images[:0]
	for _, image := range images {
		lower := strings.ToLower(strings.TrimSpace(image))
		if lower == "" || strings.Contains(lower, "/system/resource/images/filetypeimages/") ||
			strings.HasSuffix(lower, "/doc.gif") || strings.HasSuffix(lower, "/pdf.gif") ||
			strings.HasSuffix(lower, "/rar.gif") || strings.HasSuffix(lower, "/ecblank.gif") ||
			strings.HasSuffix(lower, "/spacer.gif") || dqxyIsNullPlaceholderImage(lower) {
			continue
		}
		filtered = append(filtered, image)
	}
	return filtered
}

func dqxyIsNullPlaceholderImage(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	path := strings.ToLower(u.Path)
	if !strings.Contains(path, ".files/") || !strings.HasSuffix(path, "null") {
		return false
	}
	return strings.HasPrefix(pathpkg.Base(path), "file")
}

func dqxyArticleType(doc *goquery.Document) string {
	var typ string
	doc.Find("main.news_detail_page .top-nav a").Each(func(_ int, sel *goquery.Selection) {
		candidate := strings.Join(strings.Fields(sel.Text()), " ")
		if candidate == "" || candidate == "首页" || candidate == "学院首页" {
			return
		}
		typ = candidate
	})
	return typ
}

// dqxy places its CMS download links in a UL after the #vsb_content section,
// not inside .v_news_content. Scan only the article writing area, require the
// CMS download endpoint and an enclosing attachment label, and leave ordinary
// body links (including external notices) untouched.
func appendDqxyAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("main.news_detail_page .writing a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		lowerHref := strings.ToLower(href)
		if href == "" || !strings.Contains(lowerHref, "download.jsp") || !strings.Contains(lowerHref, "downloadattachurl") || isInNavArea(sel) {
			return
		}
		listItem := sel.Closest("li")
		if listItem.Length() == 0 || !strings.Contains(listItem.Text(), "附件") {
			return
		}
		name := strings.Join(strings.Fields(sel.Text()), " ")
		if name == "" {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}
