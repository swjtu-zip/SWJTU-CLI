package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var (
	eseArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.(?:htm|html)$`)
	eseFullDateRe    = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?`)
	eseYearMonthRe   = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`)
	eseMonthDayRe    = regexp.MustCompile(`^\s*([0-9]{1,2})\s*[-/.]\s*([0-9]{1,2})\s*$`)
	eseYearRe        = regexp.MustCompile(`20[0-9]{2}`)
)

// parseEseNews handles the ESE home page and its static category pages.
//
// The home page has several independent feeds: s1 (环境要闻), s2 (通知公告),
// s3 (学术动态), and s6 (团学动态). s3 renders the same four articles once in
// a picture strip and once in a dated list. s5/s7 are platform/culture
// carousels rather than news feeds, so they are intentionally not collected.
// Category pages use ordinary list-like containers and are handled by the
// narrow fallback selectors below.
func parseEseNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse ESE list: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	add := func(anchor *goquery.Selection) {
		addEseItem(&items, seen, anchor, baseURL)
	}

	if eseHasHomeSections(doc) {
		// s1-ban contains real, dated 环境要闻 cards; it is distinct from a
		// generic decorative .banner and is therefore retained.
		for _, selector := range []string{
			"#s1 .s1-ban a[href]",
			"#s1 .s1-lt a[href]",
			"#s2 .s2-lt a[href]",
			"#s3 .s3-lt a[href]",
			"#s3 .s3-ban a[href]",
			"#s6 .s6-lt a[href]",
			".s1 .s1-ban a[href]",
			".s1 .s1-lt a[href]",
			".s2 .s2-lt a[href]",
			".s3 .s3-lt a[href]",
			".s3 .s3-ban a[href]",
			".s6 .s6-lt a[href]",
		} {
			doc.Find(selector).Each(func(_ int, anchor *goquery.Selection) {
				add(anchor)
			})
		}
	} else {
		foundListRegion := false
		for _, selector := range eseListSelectors {
			region := doc.Find(selector)
			if region.Length() == 0 {
				continue
			}
			foundListRegion = true
			region.Each(func(_ int, anchor *goquery.Selection) {
				add(anchor)
			})
		}
		// A future ESE list template may rename its wrapper. The strict URL,
		// navigation, and decoration checks still make this fallback bounded.
		if !foundListRegion || len(items) == 0 {
			doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
				add(anchor)
			})
		}
	}

	return items, nil
}

var eseListSelectors = []string{
	"main a[href]",
	".main a[href]",
	".news-list a[href]",
	".news_list a[href]",
	".list_news a[href]",
	".newsList a[href]",
	".list-box a[href]",
	".list_box a[href]",
	".right-list a[href]",
	".list a[href]",
	"ul li a[href]",
}

func eseHasHomeSections(doc *goquery.Document) bool {
	return doc.Find("#s1, #s2, #s3, #s6, .s1, .s2, .s3, .s6").Length() > 0
}

func addEseItem(items *[]model.NewsItem, seen map[string]int, anchor *goquery.Selection, baseURL string) {
	if anchor == nil || anchor.Length() == 0 || eseIsNonContentAnchor(anchor) {
		return
	}

	fullURL, ok := eseArticleURL(baseURL, anchor.AttrOr("href", ""))
	if !ok {
		return
	}
	title := eseAnchorTitle(anchor)
	if !eseUsableTitle(title) {
		return
	}
	date := eseAnchorDate(anchor)

	if index, ok := seen[fullURL]; ok {
		// Feature cards can omit a date while the duplicate list row carries
		// one. Merge only fields explicitly present in the source HTML.
		if (*items)[index].Date == "" && date != "" {
			(*items)[index].Date = date
		}
		if (*items)[index].Title == "" && title != "" {
			(*items)[index].Title = title
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

func eseArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, href))
	u, err := url.Parse(fullURL)
	if err != nil || u.Hostname() == "" || !eseArticlePathRe.MatchString(u.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return "", false
	}
	if !isValidNewsURL(fullURL) {
		return "", false
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.RawFragment = ""
	return cleanURL(u.String()), true
}

func eseIsNonContentAnchor(anchor *goquery.Selection) bool {
	if isInNavArea(anchor) {
		return true
	}
	for _, selector := range []string{
		".banner", ".carousel", ".focus", ".hero", ".slider",
		".pagination", ".p_pages", ".p_no", ".breadcrumb", ".crumb",
		".sidebar", ".side", ".widget",
	} {
		if anchor.Closest(selector).Length() > 0 {
			return true
		}
	}
	return false
}

func eseAnchorTitle(anchor *goquery.Selection) string {
	for _, attr := range []string{"title", "data-title", "aria-label"} {
		if title := eseCleanTitle(anchor.AttrOr(attr, "")); eseUsableTitle(title) {
			return title
		}
	}

	for _, selector := range []string{
		".s1-rtx h3",
		".s1-txt h3",
		".s6-tx h3",
		".s3-tx p",
		".news-title",
		".article-title",
		".title",
		"[class*='title']",
		".line1",
		".line2",
		".xw-ul-tt",
		".a_block",
		"h1",
		"h2",
		"h3",
		"h4",
		"h5",
		"p",
	} {
		candidate := eseCleanTitle(anchor.Find(selector).First().Text())
		if eseUsableTitle(candidate) {
			return candidate
		}
	}

	return eseCleanTitle(anchor.Text())
}

func eseCleanTitle(title string) string {
	return strings.TrimSpace(cleanTitle(strings.Join(strings.Fields(title), " ")))
}

func eseUsableTitle(title string) bool {
	if title == "" || len(title) > 100 || utf8.RuneCountInString(title) > 100 {
		return false
	}
	// isValidNewsTitle intentionally filters a recurring university-wide
	// masthead article. On ESE it is an actual 环境要闻 entry, so retain that
	// one real article while keeping the normal navigation filtering.
	if !isValidNewsTitle(title) && !strings.HasPrefix(title, "习近平给四所交通大学全体师生") {
		return false
	}
	switch title {
	case "环境要闻", "通知公告", "学术动态", "教学平台", "科研平台", "团学动态", "学院文化", "快速链接":
		return false
	}
	return true
}

func eseAnchorDate(anchor *goquery.Selection) string {
	for _, selector := range eseDateSelectors {
		if date := eseDateFromSelection(anchor.Find(selector).First()); date != "" {
			return date
		}
	}

	card := anchor.Closest("li, article, .item, .news-item, .list-item")
	if card.Length() > 0 && card.Get(0) != anchor.Get(0) {
		for _, selector := range eseDateSelectors {
			if date := eseDateFromSelection(card.Find(selector).First()); date != "" {
				return date
			}
		}
	}
	return ""
}

var eseDateSelectors = []string{
	"time",
	".s6-tx p",
	".s1-txt > span",
	".s2-date",
	".s1-date",
	".s3-date",
	".publish-date",
	".publish-time",
	".news-date",
	".list-date",
	".date",
	".time",
	"[class*='date']",
	"[class*='time']",
}

func eseDateFromSelection(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	if value := strings.TrimSpace(sel.AttrOr("datetime", "")); value != "" {
		if date := parseEseDateText(value); date != "" {
			return date
		}
	}

	if sel.HasClass("s2-date") {
		monthDay := strings.TrimSpace(sel.Find("b").First().Text())
		yearText := sel.Find("span").First().Text()
		if match := eseMonthDayRe.FindStringSubmatch(monthDay); match != nil {
			if year := eseYearRe.FindString(yearText); year != "" {
				return normalizeDateParts(year, match[1], match[2])
			}
		}
	}

	if sel.HasClass("s1-date") {
		day := strings.TrimSpace(sel.Find("b").First().Text())
		yearMonth := strings.TrimSpace(sel.Find("span").First().Text())
		if day != "" {
			if match := eseYearMonthRe.FindStringSubmatch(yearMonth); match != nil {
				return normalizeDateParts(match[1], match[2], day)
			}
		}
	}

	if sel.HasClass("s3-date") {
		month := strings.TrimSpace(sel.Find("h3 b").First().Text())
		day := strings.TrimSpace(sel.Find("h3 span").First().Text())
		year := strings.TrimSpace(sel.Find("p").First().Text())
		if month != "" && day != "" && eseYearRe.MatchString(year) {
			return normalizeDateParts(eseYearRe.FindString(year), month, day)
		}
	}

	return parseEseDateText(sel.Text())
}

func parseEseDateText(text string) string {
	match := eseFullDateRe.FindStringSubmatch(strings.Join(strings.Fields(text), " "))
	if match == nil {
		return ""
	}
	return normalizeDateParts(match[1], match[2], match[3])
}

// parseEseArticle keeps the common article parser for body/attachment fields
// and adds the ESE detail-page heading/date forms.
func parseEseArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if docErr != nil {
		return article, nil
	}
	if title := eseDetailTitle(doc); title != "" {
		article.Title = title
	}
	if date := parseEseDateText(article.Date); date != "" {
		article.Date = date
	} else if date := eseDetailDate(doc); date != "" {
		article.Date = date
	}
	return article, nil
}

func eseDetailTitle(doc *goquery.Document) string {
	for _, selector := range []string{
		".article-title h1, .article-title h2, .article-title h3",
		".arti-title h1, .arti-title h2, .arti-title h3",
		".detail-title h1, .detail-title h2, .detail-title h3",
		".cont-tit h1, .cont-tit h2, .cont-tit h3",
		"h1.news_tit",
		"main > h1, main > h2, main > h3",
		".left-news-details > h1, .left-news-details > h2, .left-news-details > h3",
	} {
		var title string
		doc.Find(selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			if isInNavArea(sel) || sel.Closest(".v_news_content, #vsb_content_501, .news-content-wrapper").Length() > 0 {
				return true
			}
			candidate := eseCleanTitle(sel.Text())
			if eseUsableTitle(candidate) {
				title = candidate
				return false
			}
			return true
		})
		if title != "" {
			return title
		}
	}
	return ""
}

func eseDetailDate(doc *goquery.Document) string {
	for _, selector := range []string{
		".top-info",
		".news_source",
		".tips",
		".little-tips",
		".article-meta",
		".article-info",
	} {
		if date := parseEseDateText(doc.Find(selector).First().Text()); date != "" {
			return date
		}
	}
	return ""
}
