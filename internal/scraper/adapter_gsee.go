package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// gsee.swjtu.edu.cn uses a Visual SiteBuilder template whose list rows are
// <dl>s under .conlist. The first link in <dt> is a category label and the
// second link is the article; dates are in the matching <dd>.
func init() {
	registerAdapter("gsee", SiteAdapter{
		SubPages: []string{
			"xwzx/xwdt",
			"xwzx/tzgg",
			"xwzx/xsxx",
		},
		ParseList:    parseGseeNews,
		ParseArticle: parseGseeArticle,
		PageURL:      gseePageURL,
	})
}

var (
	gseeArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	gseeFullDateRe    = regexp.MustCompile(`(20[0-9]{2})\s*(?:年|[-/.])\s*([0-9]{1,2})\s*(?:月|[-/.])\s*([0-9]{1,2})\s*日?`)
	gseeShortDateRe   = regexp.MustCompile(`(?:^|[^0-9])([0-9]{1,2})\s*[-/.]\s*([0-9]{1,2})(?:\s*日)?(?:$|[^0-9])`)
)

// gseePageURL maps logical pages to the reverse-numbered static files used
// by the site. For example, xwzx/xwdt.htm is page 1 and xwzx/xwdt/130.htm
// is page 2 when the pagination reports 131 pages.
func gseePageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")

	// Older snapshots and callers used the short names before the site moved
	// the news center below xwzx/. Keep those aliases pointed at the live path.
	switch slug {
	case "xwdt", "tzgg", "xsxx":
		slug = "xwzx/" + slug
	}

	if page <= 1 || totalPages <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		filePage = 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func parseGseeNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse gsee html: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	// Category/list pages have a stable, narrow container. Do not fall back to
	// a document-wide info/ scan: the same pages contain navigation and service
	// links that are not articles.
	rows := doc.Find(".main-right .conlist > dl")
	if rows.Length() == 0 {
		rows = doc.Find(".conlist > dl")
	}
	if rows.Length() > 0 {
		rows.Each(func(_ int, row *goquery.Selection) {
			addGseeListRow(&items, seen, row, baseURL)
		})
		return items, nil
	}

	// index.htm is a dashboard rather than a .conlist page. Its featured
	// links carry complete titles, while the adjacent news/notice lists carry
	// dates (often only MM-DD). Add both sources and merge by canonical URL.
	doc.Find(".main_visual .main_image > ul > li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		title := strings.TrimSpace(row.Find("p").First().Text())
		if title == "" {
			title = gseeAnchorTitle(anchor)
		}
		addGseeItem(&items, seen, baseURL, anchor.AttrOr("href", ""), title, "")
	})

	doc.Find(".news > ul > li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		addGseeItem(&items, seen, baseURL, anchor.AttrOr("href", ""), gseeAnchorTitle(anchor), row.Find("em").First().Text())
	})

	// The notice tabs are all rendered in the HTML, including hidden tabs. They
	// are useful content sections, so retain their unique article links while
	// merging entries repeated in the “全部” tab.
	doc.Find(".notice ul[id^='con_one_'] > li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		addGseeItem(&items, seen, baseURL, anchor.AttrOr("href", ""), gseeAnchorTitle(anchor), row.Find("em").First().Text())
	})

	// Academic entries pair a date-title immediately before each schedule.
	schedules := doc.Find(".acad .schedule")
	if schedules.Length() == 0 {
		// The academic-information page currently renders the same schedule
		// rows directly under .main-right .content, without the .acad wrapper
		// used by the homepage template.
		schedules = doc.Find(".main-right .content .schedule")
	}
	schedules.Each(func(_ int, schedule *goquery.Selection) {
		anchor := schedule.Find(".todo h5 a[href], .todo a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		date := ""
		previous := schedule.Prev()
		if previous.Length() > 0 && previous.HasClass("date-title") {
			date = previous.Find("h4").First().Text()
		}
		addGseeItem(&items, seen, baseURL, anchor.AttrOr("href", ""), gseeAnchorTitle(anchor), date)
	})

	return items, nil
}

func addGseeListRow(items *[]model.NewsItem, seen map[string]int, row *goquery.Selection, baseURL string) {
	anchor := gseeListAnchor(row, baseURL)
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	addGseeItem(items, seen, baseURL, anchor.AttrOr("href", ""), gseeAnchorTitle(anchor), row.Find("dd").First().Text())
}

// gseeListAnchor deliberately prefers a direct dt child. The category link
// is nested inside <span>, while the article link is the direct child of dt.
func gseeListAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	var fallback *goquery.Selection
	anchors := row.Find("dt a[href]")
	if anchors.Length() == 0 {
		anchors = row.Find("a[href]")
	}
	anchors.EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := gseeArticleURL(baseURL, anchor.AttrOr("href", "")); !ok {
			return true
		}
		title := gseeAnchorTitle(anchor)
		if title == "" || isGseeCategoryTitle(title) {
			return true
		}
		if anchor.Parent().Length() > 0 && anchor.Parent().Get(0).Data != "span" {
			fallback = anchor
			return false
		}
		if fallback == nil {
			fallback = anchor
		}
		return true
	})
	return fallback
}

func addGseeItem(items *[]model.NewsItem, seen map[string]int, baseURL, href, title, dateText string) {
	fullURL, ok := gseeArticleURL(baseURL, href)
	if !ok {
		return
	}

	title = gseeNormalizeTitle(title)
	if title == "" || isGseeCategoryTitle(title) || !isValidNewsTitle(title) || utf8.RuneCountInString(title) > 200 {
		return
	}
	date := gseeDateFromText(dateText, true)

	if index, exists := seen[fullURL]; exists {
		if gseePreferTitle((*items)[index].Title, title) {
			(*items)[index].Title = title
		}
		if gseeDateRank(date) > gseeDateRank((*items)[index].Date) {
			(*items)[index].Date = date
		}
		return
	}

	seen[fullURL] = len(*items)
	*items = append(*items, model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     date,
		ParsedAt: time.Now(),
	})
}

func gseeArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !gseeArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func gseeAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	if title == "" {
		image := anchor.Find("img").First()
		title = strings.TrimSpace(image.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(image.AttrOr("alt", ""))
		}
	}
	return gseeNormalizeTitle(title)
}

func gseeNormalizeTitle(title string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
}

func isGseeCategoryTitle(title string) bool {
	title = gseeNormalizeTitle(title)
	if title == "[新闻动态]" || title == "[通知公告]" {
		return true
	}
	return len(title) >= 2 && strings.HasPrefix(title, "[") && strings.HasSuffix(title, "]")
}

func gseePreferTitle(current, candidate string) bool {
	current = gseeNormalizeTitle(current)
	candidate = gseeNormalizeTitle(candidate)
	if current == "" {
		return candidate != ""
	}
	currentTruncated := strings.HasSuffix(current, "...") || strings.HasSuffix(current, "…")
	candidateTruncated := strings.HasSuffix(candidate, "...") || strings.HasSuffix(candidate, "…")
	if currentTruncated != candidateTruncated {
		return currentTruncated
	}
	return utf8.RuneCountInString(candidate) > utf8.RuneCountInString(current)
}

func gseeDateRank(date string) int {
	if regexp.MustCompile(`^20[0-9]{2}-[0-9]{2}-[0-9]{2}$`).MatchString(date) {
		return 2
	}
	if regexp.MustCompile(`^[0-9]{2}-[0-9]{2}$`).MatchString(date) {
		return 1
	}
	return 0
}

// gseeDateFromText returns a full ISO date whenever the source provides a
// year. A homepage value such as 09-01 remains 09-01: no year is inferred.
func gseeDateFromText(text string, allowShort bool) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.Join(strings.Fields(text), " ")
	if match := gseeFullDateRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	if !allowShort {
		return ""
	}
	if match := gseeShortDateRe.FindStringSubmatch(text); match != nil {
		month, monthErr := strconv.Atoi(match[1])
		day, dayErr := strconv.Atoi(match[2])
		if monthErr == nil && dayErr == nil && month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			return fmt.Sprintf("%02d-%02d", month, day)
		}
	}
	return ""
}

// parseGseeArticle keeps the shared article/resource extraction but owns the
// gsee-specific h3/source hooks. The live article template uses .article h3
// and .article .source instead of the generic .news_tit/.news_source names.
func parseGseeArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse gsee article: %w", err)
	}

	if title := gseeNormalizeTitle(doc.Find(".article h3").First().Text()); title != "" {
		article.Title = title
	}
	if date := gseeDateFromText(doc.Find(".article .source").First().Text(), false); date != "" {
		article.Date = date
	}
	return article, nil
}
