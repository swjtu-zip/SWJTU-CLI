package scraper

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// sic.swjtu.edu.cn is a 博达 site with a template of its own. Its homepage
// and category pages expose article links as info/<column>/<id>.htm, while the
// homepage repeats some of those links in several feature blocks.
func init() {
	registerAdapter("sic", SiteAdapter{
		SubPages: []string{
			"xydt",
			"dqgz/dqdt",
			"dqgz/ghzj",
			"dqgz/xytw",
			"dqgz/jwgz",
			"tzgg/qb",
			"tzgg/dzb",
			"tzgg/yjsjx",
			"tzgg/bkjx",
			"tzgg/ky",
			"tzgg/xsgz",
			"tzgg/sy",
			"tzgg/xshd",
			"tzgg/zsjy",
			"xsgz/xsfc",
			"xsjl",
			"jxhd",
		},
		ParseList:    parseSicNews,
		ParseArticle: parseSicArticle,
		PageURL:      sicPageURL,
		ListPage:     sicListPage,
		FetchArticle: sicFetchArticle,
	})
}

var (
	sicArticlePathRe  = regexp.MustCompile(`(?i)(?:^|/)info/[0-9]+/[0-9]+\.html?$`)
	sicDateRe         = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})`)
	sicYearMonthRe    = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})(?:月)?`)
	sicDayBeforeYMRe  = regexp.MustCompile(`^\s*([0-9]{1,2})\D+(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`)
	sicCompactDayYMRe = regexp.MustCompile(`^\s*([0-9]{1,2})(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`)
	sicYMBeforeDayRe  = regexp.MustCompile(`^\s*(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\D+([0-9]{1,2})\s*(?:日)?\s*$`)
	sicDayOnlyRe      = regexp.MustCompile(`^\s*([0-9]{1,2})\s*(?:日)?\s*$`)
	sicClockRe        = regexp.MustCompile(`(?:^|[^0-9])(?:[01]?[0-9]|2[0-3])\s*[:：]\s*[0-5][0-9](?:\s*[:：]\s*[0-5][0-9])?(?:$|[^0-9])`)
	sicTotalCountRe   = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
)

const (
	sicCanonicalBaseURL = "https://sic.swjtu.edu.cn"
	sicListPageSize     = 10
)

// sicListPage owns the HTTP request for this site because the origin returns
// an access-denied shell when a category page is requested without a browser
// Referer. It also returns the source-reported page count so the shared range
// crawler cannot mistake the largest reverse-numbered href for the page
// count (xydt has 17 pages but its largest href is 16.htm).
func sicListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		page = 1
	}
	firstURL := sicPageURL(sicCanonicalBaseURL, slug, 1, 0, ".htm")
	firstBody, err := sicFetchHTML(firstURL)
	if err != nil {
		return nil, 0, err
	}
	totalPages, err := sicTotalPagesFromHTML(firstBody, slug)
	if err != nil {
		return nil, 0, err
	}
	if page > totalPages {
		return nil, 0, fmt.Errorf("sic: requested page %d but %s has %d pages", page, slug, totalPages)
	}
	if page == 1 {
		items, parseErr := parseSicNews(firstBody, sicCanonicalBaseURL)
		return items, totalPages, parseErr
	}
	listURL := sicPageURL(sicCanonicalBaseURL, slug, page, totalPages, ".htm")
	body, err := sicFetchHTML(listURL)
	if err != nil {
		return nil, 0, err
	}
	items, err := parseSicNews(body, sicCanonicalBaseURL)
	if err != nil {
		return nil, 0, err
	}
	if err := sicValidateListPage(body, slug, page, totalPages, items); err != nil {
		return nil, 0, err
	}
	return items, totalPages, err
}

// sicTotalPagesFromHTML calculates pages from the displayed article total and
// the number of entries on the newest page. Static/empty shells are not
// silently treated as article lists: the homepage is the sole intentional
// exception and has exactly one page.
func sicTotalPagesFromHTML(body []byte, slug string) (int, error) {
	if strings.Trim(slug, "/") == "index" {
		if _, err := parseSicNews(body, sicCanonicalBaseURL); err != nil {
			return 0, err
		}
		return 1, nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("sic: parse first page for %s: %w", slug, err)
	}
	if err := sicResponseError(doc); err != nil {
		return 0, err
	}
	total, ok, err := sicSourceArticleTotal(doc, slug)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("sic: %s has no source article total", slug)
	}
	items, err := parseSicNews(body, sicCanonicalBaseURL)
	if err != nil {
		return 0, err
	}
	totalPages := (total + sicListPageSize - 1) / sicListPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if err := sicValidateListPageItems(slug, 1, totalPages, total, len(items)); err != nil {
		return 0, err
	}
	return totalPages, nil
}

func sicSourceArticleTotal(doc *goquery.Document, slug string) (int, bool, error) {
	if doc == nil {
		return 0, false, fmt.Errorf("sic: %s has an empty document", slug)
	}
	countText := strings.TrimSpace(doc.Find(".p_t").First().Text())
	match := sicTotalCountRe.FindStringSubmatch(countText)
	if match == nil {
		return 0, false, nil
	}
	total, err := strconv.Atoi(match[1])
	if err != nil || total < 0 {
		return 0, false, fmt.Errorf("sic: invalid article total %q for %s", match[1], slug)
	}
	return total, true, nil
}

func sicValidateListPage(body []byte, slug string, page, totalPages int, items []model.NewsItem) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("sic: parse page %d for %s: %w", page, slug, err)
	}
	if err := sicResponseError(doc); err != nil {
		return err
	}
	total, ok, err := sicSourceArticleTotal(doc, slug)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("sic: page %d/%d for %s has no source article total", page, totalPages, slug)
	}
	expectedPages := (total + sicListPageSize - 1) / sicListPageSize
	if expectedPages < 1 {
		expectedPages = 1
	}
	if expectedPages != totalPages {
		return fmt.Errorf("sic: page %d for %s reports %d pages, expected %d", page, slug, expectedPages, totalPages)
	}
	return sicValidateListPageItems(slug, page, totalPages, total, len(items))
}

func sicValidateListPageItems(slug string, page, totalPages, total, itemCount int) error {
	if page < 1 || page > totalPages {
		return fmt.Errorf("sic: invalid page %d/%d for %s", page, totalPages, slug)
	}
	remaining := total - (page-1)*sicListPageSize
	if remaining < 0 {
		remaining = 0
	}
	expected := remaining
	if expected > sicListPageSize {
		expected = sicListPageSize
	}
	if total == 0 {
		expected = 0
	}
	if itemCount != expected {
		return fmt.Errorf("sic: page %d/%d for %s returned %d articles, expected %d", page, totalPages, slug, itemCount, expected)
	}
	return nil
}

func sicFetchHTML(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("sic: create request %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", sicCanonicalBaseURL+"/")
	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sic: fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sic: fetch %s: status %d", rawURL, resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml+xml") {
		return nil, fmt.Errorf("sic: %s returned non-HTML content type %q", rawURL, contentType)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sic: read %s: %w", rawURL, err)
	}
	return body, nil
}

func sicFetchArticle(articleURL string) (*model.Article, []byte, error) {
	body, err := sicFetchHTML(articleURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseSicArticle(body, sicCanonicalBaseURL)
	if err != nil {
		return nil, nil, err
	}
	return article, body, nil
}

// sicPageURL maps the logical page number to the reversed filenames emitted
// by the static site: page 1 is xydt.htm, page 2 is xydt/<total-1>.htm, and
// the last page is xydt/1.htm.
func sicPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page <= 1 || totalPages <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, totalPages-page+1, extension)
}

// parseSicNews parses both the sic homepage and its static category pages.
// The stable signal is the local info/<column>/<id>.htm article path; all
// other links on these pages are navigation, banner, or utility links.
func parseSicNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse sic html: %w", err)
	}
	if err := sicResponseError(doc); err != nil {
		return nil, err
	}
	if sicIsHomepage(doc) {
		items := parseSicHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("sic: homepage contains no article modules")
		}
		return items, nil
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")); pageType == "3" {
		return nil, fmt.Errorf("sic: article shell passed to list parser")
	}

	itemType := sicSectionType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		if sicIsNonContentAnchor(sel) {
			return
		}

		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if !sicIsArticleHref(href) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, href))
		if isExternalURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}

		title := sicTitle(sel)
		if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := sicDateValuesForAnchor(sel)
		typ := sicTypeForAnchor(sel, itemType)

		if index, ok := seen[fullURL]; ok {
			// A feature image can be the first occurrence of an article and
			// carry no date; a later text link often has the date. Retain one
			// item while filling fields from the better duplicate.
			if items[index].Title == "" {
				items[index].Title = title
			}
			if items[index].Date == "" && date != "" {
				items[index].Date = date
				items[index].PublishedAt = publishedAt
			}
			if items[index].PublishedAt == "" && publishedAt != "" {
				items[index].PublishedAt = publishedAt
			}
			if sicTypeIsBetter(typ, items[index].Type) {
				items[index].Type = typ
			}
			return
		}

		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	})

	if total, ok, totalErr := sicSourceArticleTotal(doc, itemType); totalErr != nil {
		return nil, totalErr
	} else if ok {
		if err := sicValidateParsedItemCount(itemType, total, len(items)); err != nil {
			return nil, err
		}
	}
	if len(items) == 0 {
		if total, ok, _ := sicSourceArticleTotal(doc, itemType); ok && total == 0 {
			return items, nil
		}
		return nil, fmt.Errorf("sic: %s is not an article list or explicit empty tab", itemType)
	}
	return items, nil
}

func parseSicHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		if sicIsNonContentAnchor(sel) || !sicIsArticleHref(sel.AttrOr("href", "")) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(sel.AttrOr("href", ""))))
		if isExternalURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}
		title := sicTitle(sel)
		if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
			return
		}
		typ := sicHomepageType(sel)
		if typ == "" {
			return
		}
		date, publishedAt := sicDateValuesForAnchor(sel)
		item := model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		}
		if index, ok := seen[fullURL]; ok {
			if items[index].Title == "" {
				items[index].Title = item.Title
			}
			if items[index].Date == "" && item.Date != "" {
				items[index].Date = item.Date
				items[index].PublishedAt = item.PublishedAt
			}
			if items[index].PublishedAt == "" && item.PublishedAt != "" {
				items[index].PublishedAt = item.PublishedAt
			}
			if sicTypeIsBetter(item.Type, items[index].Type) {
				items[index].Type = item.Type
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, item)
	})
	return items
}

func sicValidateParsedItemCount(slug string, total, itemCount int) error {
	if total < 0 {
		return fmt.Errorf("sic: invalid article total %d for %s", total, slug)
	}
	if total == 0 {
		if itemCount != 0 {
			return fmt.Errorf("sic: %s reports no articles but parser returned %d", slug, itemCount)
		}
		return nil
	}
	if itemCount == 0 {
		return fmt.Errorf("sic: %s reports %d articles but parser returned none", slug, total)
	}
	if total <= sicListPageSize && itemCount != total {
		return fmt.Errorf("sic: %s reports %d articles but parser returned %d", slug, total, itemCount)
	}
	if total > sicListPageSize && (itemCount < 1 || itemCount > sicListPageSize) {
		return fmt.Errorf("sic: %s page returned %d articles for total %d; expected between 1 and %d", slug, itemCount, total, sicListPageSize)
	}
	return nil
}

func sicHomepageType(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if anchor.Closest(".part1").Length() > 0 {
		return "学院动态"
	}
	if anchor.Closest(".part2").Length() > 0 {
		if row := anchor.Closest("li"); row.Length() > 0 {
			if typ := cleanTitle(strings.TrimSpace(row.Find("b").First().Text())); typ != "" && typ != "全部" {
				return typ
			}
		}
		if slide := anchor.Closest(".swiper-slide"); slide.Length() > 0 {
			if typ := cleanTitle(strings.TrimSpace(slide.Find(".Cotitle h5, .Btitle h5").First().Text())); typ != "" {
				return sicNoticeType(typ)
			}
		}
		return "通知公告"
	}
	if anchor.Closest(".part3").Length() > 0 {
		return "学术交流"
	}
	if anchor.Closest(".part4").Length() > 0 {
		return "教学活动"
	}
	return ""
}

func sicNoticeType(value string) string {
	value = cleanTitle(value)
	if value == "全部" {
		return "通知公告"
	}
	return value
}

func sicTypeForAnchor(anchor *goquery.Selection, fallback string) string {
	if typ := sicHomepageType(anchor); typ != "" {
		return typ
	}
	if row := anchor.Closest("li"); row.Length() > 0 {
		if typ := cleanTitle(strings.TrimSpace(row.Find("b").First().Text())); typ != "" {
			return sicNoticeType(typ)
		}
	}
	return fallback
}

func sicTypeIsBetter(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	current = strings.TrimSpace(current)
	if candidate == "" || candidate == current {
		return false
	}
	return current == "" || sicTypeIsGeneric(current)
}

func sicTypeIsGeneric(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "全部", "通知公告", "首页新闻":
		return true
	default:
		return false
	}
}

func sicIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".part1, .part2, .part3, .part4").Length() > 0
}

func sicResponseError(doc *goquery.Document) error {
	if doc == nil {
		return fmt.Errorf("sic: empty response")
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	body := strings.ToLower(strings.Join(strings.Fields(doc.Find("body").First().Text()), " "))
	if strings.Contains(title, "access denied") || strings.Contains(title, "404") || strings.Contains(title, "not found") ||
		strings.Contains(title, "访问限制") || strings.Contains(title, "访问受限") || strings.Contains(title, "错误") ||
		strings.Contains(body, "access denied") || strings.Contains(body, "暂无权限访问") || strings.Contains(body, "无权访问") ||
		strings.Contains(body, "验证码") {
		return fmt.Errorf("sic: source returned an access-denied, error, or verification shell")
	}
	return nil
}

// sicSectionType reads the category heading from both list and detail pages.
// The detail template has a generic h1 (学院动态) before the real headline;
// retaining this value as Type keeps the feed/category metadata correct while
// parseSicArticle separately selects the headline from .show01 h5.
func sicSectionType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	var value string
	for _, selector := range []string{
		"#nav_slide .n_title h1",
		".n_title h1",
		".margin_auto_1",
	} {
		value = cleanTitle(strings.TrimSpace(doc.Find(selector).First().Text()))
		if value != "" {
			break
		}
	}
	if value == "" {
		value = stripSiteSuffix(cleanTitle(strings.TrimSpace(doc.Find("title").First().Text())))
	}
	if value == "" || strings.Contains(value, "集成电路科学与工程学院") {
		return ""
	}
	if value == "全部" {
		return "通知公告"
	}
	return value
}

func sicIsArticleHref(href string) bool {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return false
	}
	return sicArticlePathRe.MatchString(ref.Path)
}

// The generic nav helper treats every .swiper as non-content, but sic uses
// swipers for its real academic and notice sections. Exclude only the actual
// header/banner containers here.
func sicIsNonContentAnchor(sel *goquery.Selection) bool {
	for _, selector := range []string{
		"header",
		"nav",
		"footer",
		".bannerBox",
		"#swiper",
		".navBox",
		".m_nav_list",
		".foot-nav",
	} {
		if sel.Closest(selector).Length() > 0 {
			return true
		}
	}
	return false
}

func sicTitle(sel *goquery.Selection) string {
	title := strings.TrimSpace(sel.AttrOr("title", ""))
	if title == "" {
		for _, selector := range []string{
			".nr h5",
			".text h5",
			".text-title",
			".title",
			"h1",
			"h2",
			"h3",
			"h4",
			"h5",
			"p",
		} {
			candidate := strings.TrimSpace(sel.Find(selector).First().Text())
			if candidate != "" {
				title = candidate
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSpace(sel.Text())
	}
	return cleanTitle(title)
}

var sicDateSelectors = []string{
	".time",
	".date",
	"time",
	".news-time",
	".time_box",
	".time-container",
	".list-time",
	".list-col-time",
	".date-box",
	".publish-time",
	"i",
}

func sicDateForAnchor(anchor *goquery.Selection) string {
	date, _ := sicDateValuesForAnchor(anchor)
	return date
}

func sicDateValuesForAnchor(anchor *goquery.Selection) (string, string) {
	if date, publishedAt := sicDateValuesFromSelection(anchor); date != "" {
		return date, publishedAt
	}

	// On the academic-exchange cards the date is a sibling of the headline
	// anchor inside .con; on category pages it is normally in the same <li>.
	parent := anchor.Parent()
	for depth := 0; parent.Length() > 0 && depth < 4; depth++ {
		if sicIsDateContext(parent) {
			if date, publishedAt := sicDateValuesFromSelection(parent); date != "" {
				return date, publishedAt
			}
		}
		parent = parent.Parent()
	}
	return "", ""
}

func sicIsDateContext(sel *goquery.Selection) bool {
	if sel.Is("li") {
		return true
	}
	for _, class := range []string{
		"con",
		"part1L",
		"part1RT",
		"list-cell",
		"list-col",
		"list-item",
		"news-item",
		"news-desc",
		"text",
		"info",
	} {
		if sel.HasClass(class) {
			return true
		}
	}
	return false
}

func sicDateFromSelection(sel *goquery.Selection) string {
	date, _ := sicDateValuesFromSelection(sel)
	return date
}

func sicDateValuesFromSelection(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	for _, selector := range sicDateSelectors {
		var date, publishedAt string
		if sel.Is(selector) {
			date, publishedAt = sicDateValuesFromDateBlock(sel)
			if date != "" {
				return date, publishedAt
			}
		}
		sel.Find(selector).EachWithBreak(func(_ int, dateSel *goquery.Selection) bool {
			date, publishedAt = sicDateValuesFromDateBlock(dateSel)
			return date == ""
		})
		if date != "" {
			return date, publishedAt
		}
	}
	return "", ""
}

func sicDateFromDateBlock(sel *goquery.Selection) string {
	date, _ := sicDateValuesFromDateBlock(sel)
	return date
}

func sicDateValuesFromDateBlock(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	text := strings.Join(strings.Fields(sel.Text()), " ")
	if date, publishedAt := sicDateValuesFromText(text); date != "" {
		return date, publishedAt
	}

	day := strings.TrimSpace(sel.Find(".day, .date-day").First().Text())
	yearMonth := strings.TrimSpace(sel.Find(".year, .date-year").First().Text())
	if date := normalizeSicYearMonthDay(yearMonth, day); date != "" {
		return date, ""
	}

	if match := sicDayBeforeYMRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[2], match[3], match[1]), ""
	}
	if match := sicCompactDayYMRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[2], match[3], match[1]), ""
	}
	if match := sicYMBeforeDayRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}

	var childDay, childYearMonth string
	sel.Children().Each(func(_ int, child *goquery.Selection) {
		childText := strings.TrimSpace(child.Text())
		if childYearMonth == "" && sicYearMonthRe.MatchString(childText) {
			childYearMonth = childText
		}
		if childDay == "" && sicDayOnlyRe.MatchString(childText) {
			childDay = childText
		}
	})
	if date := normalizeSicYearMonthDay(childYearMonth, childDay); date != "" {
		return date, ""
	}
	return "", ""
}

func sicDateValuesFromText(text string) (string, string) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "", ""
	}
	if publishedAt := normalizePublishedAt(text); publishedAt != "" && len(publishedAt) >= 10 {
		if sicClockRe.MatchString(text) {
			return publishedAt[:10], publishedAt
		}
		return publishedAt[:10], ""
	}
	if date := normalizeSicDate(text); date != "" {
		return date, ""
	}
	return "", ""
}

func normalizeSicYearMonthDay(yearMonth, day string) string {
	ym := sicYearMonthRe.FindStringSubmatch(strings.TrimSpace(yearMonth))
	d := sicDayOnlyRe.FindStringSubmatch(strings.TrimSpace(day))
	if ym == nil || d == nil {
		return ""
	}
	return normalizeDateParts(ym[1], ym[2], d[1])
}

func normalizeSicDate(text string) string {
	match := sicDateRe.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	return normalizeDateParts(match[1], match[2], match[3])
}

// parseSicArticle keeps the common article/body handling but owns the sic
// entry point and normalizes the site's timestamp-bearing metadata line.
func parseSicArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if docErr != nil {
		return nil, fmt.Errorf("sic: parse article html: %w", docErr)
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")); pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("sic: page type %q is not an article", pageType)
	}

	// SIC detail pages put the real headline in .show01 h5; the shared
	// parser can otherwise see the breadcrumb/sidebar heading "学院动态"
	// first and return that as the article title.
	for _, selector := range []string{".show01 h5", ".show01 h1", ".show01 .title"} {
		if title := cleanTitle(strings.TrimSpace(doc.Find(selector).First().Text())); title != "" {
			article.Title = title
			break
		}
	}
	if article.Title == "" {
		return nil, fmt.Errorf("sic: article headline is missing")
	}
	content := doc.Find(".show02 .v_news_content, .show02 #vsb_content, .show02 [id^='vsb_content'], .v_news_content").First()
	if content.Length() == 0 {
		return nil, fmt.Errorf("sic: article body container is missing")
	}
	if strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a, object, iframe").Length() == 0 {
		return nil, fmt.Errorf("sic: article body is empty")
	}
	if typ := sicSectionType(doc); typ != "" {
		article.Type = typ
	}

	// The SIC source puts its precise timestamp in the show01 metadata
	// line using <em>, not the shared parser's older <span> shape.
	if meta := doc.Find(".show01").First(); meta.Length() > 0 {
		if precise := normalizePublishedAt(flattenedText(meta)); precise != "" {
			article.PublishedAt = precise
			article.Date = precise[:10]
		}
	}
	if article.PublishedAt == "" {
		for _, selector := range []string{".news_source", ".article-meta", ".article-info", ".news-info", ".tips", ".top-info"} {
			if meta := doc.Find(selector).First(); meta.Length() > 0 {
				if precise := normalizePublishedAt(flattenedText(meta)); precise != "" && len(precise) > 10 {
					article.PublishedAt = precise
					article.Date = precise[:10]
					break
				}
			}
		}
	}
	if normalized := normalizeSicDate(article.Date); normalized != "" {
		article.Date = normalized
	}
	if article.Date == "" {
		if docErr == nil {
			for _, selector := range []string{
				".news_source",
				".article-meta",
				".article-info",
				".news-info",
				".tips",
				".top-info",
			} {
				text := strings.TrimSpace(doc.Find(selector).First().Text())
				if normalized := normalizeSicDate(text); normalized != "" {
					article.Date = normalized
					break
				}
			}
		}
	}
	normalizeArticlePublishedAt(article)
	sicDeduplicateResources(article)
	return article, nil
}

func sicDeduplicateResources(article *model.Article) {
	if article == nil {
		return
	}
	seenImages := make(map[string]bool, len(article.Images))
	images := article.Images[:0]
	for _, image := range article.Images {
		if image == "" || seenImages[image] {
			continue
		}
		seenImages[image] = true
		images = append(images, image)
	}
	article.Images = images

	seenAttachments := make(map[string]bool, len(article.Attachments))
	attachments := article.Attachments[:0]
	for _, attachment := range article.Attachments {
		if attachment.URL == "" || seenAttachments[attachment.URL] {
			continue
		}
		seenAttachments[attachment.URL] = true
		attachments = append(attachments, attachment)
	}
	article.Attachments = attachments
}
