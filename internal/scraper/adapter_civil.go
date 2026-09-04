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

// civil.swjtu.edu.cn is a Visual SiteBuilder site with a different home-page
// layout from the generic Boda list templates. Its home page has a featured
// .innews list and five .education-contanier tab lists. The list pages use a
// similar article-card/list layout, but the date is sometimes split into a
// day and a year-month element.
//
// This includes the article-list surfaces exposed by the desktop and mobile
// navigation, including currently empty lists. Static profile/English pages
// are intentionally not represented as feeds.
var civilSubPages = []string{
	"tzgg",
	"tmyw",
	"bkjx",
	"scientific",
	"xsgz",
	"zsjy",
	"djgz",
	"yjspy",
	"tmxy",
	"bkjx/bkjxgg",
	"bkjx/bkzyjs",
	"bkjx/bkpyfa",
	"bkjx/bkjxgl",
	"bkjx/glygzzd",
	"bkjx/wjbgxz",
	"scientific/kytzgg",
	"scientific/kydtxx",
	"scientific/kycgzs",
	"scientific/kyfczs",
	"scientific/xkjsxg",
	"scientific/ABEgjqk",
	"scientific/ITIgjqk",
	"scientific/glygzzd",
	"scientific/wjybgxz",
	"xsgz/xsgztz",
	"xsgz/xsgzdt",
	"xsgz/fdygzs",
	"xsgz/bjjskx",
	"xsgz/xzfczs",
	"xsgz/xhsc",
	"xsgz/wxycwz",
	"xsgz/xstd",
	"xsgz/xshdhb",
	"xsgz/xyglzd",
	"xsgz/wjybgxz",
	"zsjy/bkzsxg",
	"zsjy/yjszsxg",
	"zsjy/xyzpxx",
	"zsjy/sxszpxx",
	"zsjy/jyzdypx",
	"zsjy/gjzkzs",
	"zsjy/xyglzd",
	"zsjy/wjybgxz",
	"djgz/llyjxx",
	"djgz/zzjsfz",
	"djgz/djhdxx",
	"djgz/zbgzxx",
	"djgz/bmgzjy",
	"djgz/dyzxzs",
	"djgz/ghzj",
	"djgz/xyglzd",
	"djgz/wjybgxz",
	"yjspy/yjsgg",
	"yjspy/yjszyjs",
	"yjspy/yjspyfa",
	"yjspy/yjskcb",
	"yjspy/bsspy",
	"yjspy/bshldz",
	"yjspy/glygzzd",
	"yjspy/wjyxzlb",
	"tmxy/xyjz",
	"tmxy/xyh",
	"tmxy/xyglzd",
	"tmxy/wjybgxz",
	"zsjy/jxjypxzx_j",
	"zgzx/zsgxjyzx",
}

func init() {
	registerAdapter("civil", SiteAdapter{
		SubPages:     civilSubPages,
		ParseList:    parseCivilNews,
		ParseArticle: parseCivilArticle,
		PageURL:      civilPageURL,
		TotalPages:   civilTotalPages,
	})
}

var (
	civilArticlePathRe   = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	civilResourcePathRe  = regexp.MustCompile(`(?i)^/__local/.+\.(?:doc|docx|xls|xlsx|ppt|pptx|pdf|zip|rar|7z|txt|csv)$`)
	civilResourceQueryRe = regexp.MustCompile(`(?i)^\.(?:doc|docx|xls|xlsx|ppt|pptx|pdf|zip|rar|7z|txt|csv)$`)
	civilDateRe          = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})`)
	civilYearMonthRe     = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`) // used with a separate day element
	civilDayRe           = regexp.MustCompile(`^([0-9]{1,2})$`)
	civilSplitDateRe     = regexp.MustCompile(`^([0-9]{1,2})\s*20[0-9]{2}\s*[-/.年]\s*[0-9]{1,2}$`)
	civilDateTimeRe      = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})(?:日|号)?\s+([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?`)
	civilCountRe         = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
	civilPerPageRe       = regexp.MustCompile(`每页\s*([0-9]+)\s*条`)
	civilSourceValueRe   = regexp.MustCompile(`(?:信息来源|来源)\s*[:：]\s*(.*?)(?:\s*(?:分类|文章类型|信息类型|类型|作者|编辑|责编|摄影|阅读量|日期|发布时间)\s*[:：]|$)`)
	civilTypeValueRe     = regexp.MustCompile(`(?:文章类型|信息类型|类型|分类)\s*[:：]\s*(.*?)(?:\s*(?:来源|作者|编辑|责编|摄影|阅读量|日期|发布时间)\s*[:：]|$)`)
)

// civilPageURL maps the logical page number shown by civil.swjtu.edu.cn to
// the generated file. Page 1 is the unsuffixed page; generated files count
// backwards, so logical page 2 is totalPages-1.htm.
func civilPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if baseURL == "" || slug == "" || page < 1 || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages <= 1 {
		return ""
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func parseCivilNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse civil html: %w", err)
	}
	if err := civilResponseError(doc); err != nil {
		return nil, err
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	defaultType := civilListPageType(doc)
	add := func(item, anchor *goquery.Selection) {
		addCivilNewsItem(&items, seen, item, anchor, baseURL, defaultType)
	}

	// The home page's featured items use .line2. Do not scan every info/ link:
	// the banner, scenery cards, footer and navigation all reuse that URL form.
	doc.Find(".preface-new-box .innews > li").Each(func(_ int, sel *goquery.Selection) {
		if anchor := sel.Find("a.line2[href]").First(); anchor.Length() > 0 {
			add(sel, anchor)
		}
	})

	// The five home-page category panels and the category list pages use line1
	// for article links. Restricting to the article class avoids the links in
	// the site's navigation even when they point at an info/*.htm page.
	doc.Find("a.line1[href]").Each(func(_ int, sel *goquery.Selection) {
		add(civilListItem(sel), sel)
	})

	// Current civil list pages use a few different list container class names
	// over their lifetime. This fallback remains scoped to list-like regions;
	// it is deliberately not a document-wide info/ scan on the home page.
	doc.Find(".news-list a[href], .news_list a[href], .list_news a[href], .news-cont a[href], .news-list-box a[href], .list-box a[href], .right-list a[href]").Each(func(_ int, sel *goquery.Selection) {
		add(civilListItem(sel), sel)
	})

	// If a future list page drops the class names, article URLs are still safe
	// to use as a last resort: the URL validator and the explicit decoration
	// exclusions below prevent banner/footer/navigation leakage.
	if len(items) == 0 {
		doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
			add(civilListItem(sel), sel)
		})
	}

	if len(items) == 0 {
		if civilExplicitEmptyList(doc) {
			return items, nil
		}
		return nil, civilListParseError(doc)
	}
	return items, nil
}

func civilTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" || slug == "home" {
		return 1, nil
	}
	if !civilKnownSubPage(slug) {
		return 0, fmt.Errorf("civil: %q is not a registered article-list route", slug)
	}

	site := AllSites["civil"]
	if site == nil || strings.TrimSpace(site.BaseURL) == "" {
		return 0, fmt.Errorf("civil: site registration is missing its base URL")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		listURL := civilPageURL(baseURL, slug, 1, 0, extension)
		body, err := fetch(listURL)
		if err != nil {
			lastErr = err
			continue
		}
		pages, err := civilTotalPagesFromHTML(body)
		if err == nil {
			return pages, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("civil: no page-count response for %q", slug)
	}
	return 0, fmt.Errorf("civil: unable to determine total pages for %q: %w", slug, lastErr)
}

func civilKnownSubPage(slug string) bool {
	for _, known := range civilSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func civilTotalPagesFromHTML(raw []byte) (int, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("civil: parse pagination HTML: %w", err)
	}
	if err := civilResponseError(doc); err != nil {
		return 0, err
	}
	if !civilListSurface(doc) {
		return 0, fmt.Errorf("civil: response is not a recognized list (missing news_list container)")
	}

	total, perPage, hasCount := civilPaginationValues(doc)
	if hasCount {
		if total == 0 {
			return 1, nil
		}
		if perPage > 0 {
			return (total + perPage - 1) / perPage, nil
		}
		if pages := civilDisplayedPageCount(doc); pages > 0 {
			return pages, nil
		}
		return 0, fmt.Errorf("civil: list reports %d entries but no page-size or logical page count", total)
	}
	if pages := civilDisplayedPageCount(doc); pages > 0 {
		return pages, nil
	}
	return 0, fmt.Errorf("civil: recognized list has no total-item/page counter")
}

func civilListSurface(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".news_list, .news-list, .list_news, .news-cont, .news-list-box, .list-box, .right-list").Length() > 0
}

func civilArticleSurface(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".v_news_content, #vsb_content_501, [id^='vsb_content'], .news-content-wrapper, .content-main").Length() > 0
}

func civilPaginationSelection(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		empty, _ := goquery.NewDocumentFromReader(strings.NewReader(""))
		return empty.Find("__civil_no_pagination__")
	}
	for _, selector := range []string{".pagination .pb_sys_common", ".pagination", ".pb_sys_common", ".p_pages"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			return found
		}
	}
	return doc.Find("__civil_no_pagination__")
}

func civilPaginationValues(doc *goquery.Document) (int, int, bool) {
	pagination := civilPaginationSelection(doc)
	if pagination.Length() == 0 {
		return 0, 0, false
	}
	text := cleanCivilText(pagination.Text())
	countMatch := civilCountRe.FindStringSubmatch(text)
	if len(countMatch) < 2 {
		return 0, 0, false
	}
	total, err := strconv.Atoi(countMatch[1])
	if err != nil || total < 0 {
		return 0, 0, false
	}
	perPage := 0
	if perPageMatch := civilPerPageRe.FindStringSubmatch(text); len(perPageMatch) > 1 {
		perPage, _ = strconv.Atoi(perPageMatch[1])
	}
	return total, perPage, true
}

func civilDisplayedPageCount(doc *goquery.Document) int {
	pagination := civilPaginationSelection(doc)
	maxPage := 0
	pagination.Find(".p_no a, .p_no_d, .p_no").Each(func(_ int, sel *goquery.Selection) {
		if page, err := strconv.Atoi(strings.TrimSpace(sel.Text())); err == nil && page > maxPage {
			maxPage = page
		}
	})
	return maxPage
}

func civilExplicitEmptyList(doc *goquery.Document) bool {
	if !civilListSurface(doc) || civilPaginationSelection(doc).Length() == 0 {
		return false
	}
	total, _, ok := civilPaginationValues(doc)
	return ok && total == 0
}

func civilListParseError(doc *goquery.Document) error {
	if !civilListSurface(doc) {
		return fmt.Errorf("civil: response is not a recognized article list (missing news_list container; possible static, SPA, WAF, or login shell)")
	}
	if total, _, ok := civilPaginationValues(doc); ok && total > 0 {
		return fmt.Errorf("civil: list reports %d entries but contains no recognized article/resource rows", total)
	}
	return fmt.Errorf("civil: list contains no recognized article/resource rows and no explicit zero-item pagination")
}

func civilResponseError(doc *goquery.Document) error {
	if doc == nil {
		return fmt.Errorf("civil: empty response")
	}
	title := strings.ToLower(cleanCivilText(doc.Find("title").First().Text()))
	for _, marker := range []string{"captcha", "验证码", "登录页", "login", "access denied", "403", "404", "500", "系统错误", "系统提示", "error"} {
		if strings.Contains(title, marker) {
			return fmt.Errorf("civil: classified error page (title contains %q: %s)", marker, title)
		}
	}
	if civilListSurface(doc) || civilArticleSurface(doc) {
		return nil
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(cleanCivilText(clone.Text()))
	for _, marker := range []string{"验证码", "captcha", "安全验证", "安全威胁", "请求已被阻断", "没有访问当前栏目的权限", "您没有访问", "请先登录", "请登录后", "access denied", "403 forbidden", "404 not found"} {
		if strings.Contains(visible, marker) {
			return fmt.Errorf("civil: classified error/verification page (visible marker %q)", marker)
		}
	}
	return nil
}

func civilListItem(anchor *goquery.Selection) *goquery.Selection {
	// On category pages the title and split date live in the same
	// .news_list_des card, while the anchor itself is nested in a <p>. Taking
	// only the parent loses the date; keep the card as the parse scope.
	for _, selector := range []string{".news_list_des", ".news-list-des"} {
		if item := anchor.Closest(selector); item.Length() > 0 {
			return item
		}
	}
	if item := anchor.Closest("li"); item.Length() > 0 {
		return item
	}
	return anchor.Parent()
}

func addCivilNewsItem(items *[]model.NewsItem, seen map[string]bool, item, anchor *goquery.Selection, baseURL, defaultType string) {
	if anchor == nil || anchor.Length() == 0 || isCivilDecoration(anchor) {
		return
	}

	href, ok := anchor.Attr("href")
	if !ok {
		return
	}
	fullURL, ok := civilArticleURL(baseURL, href)
	if !ok || seen[fullURL] {
		return
	}

	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".line1, .line2, .news-title, .article-title").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanCivilTitle(title)
	if title == "" || utf8.RuneCountInString(title) > 100 || !civilValidNewsTitle(title, item, anchor) {
		return
	}

	date, publishedAt := civilDateValuesFromItem(item)
	if date == "" {
		date, publishedAt = civilDateValuesFromItem(anchor)
	} else if publishedAt == "" {
		_, publishedAt = civilDateValuesFromItem(anchor)
	}
	typ := civilListType(item, anchor)
	if typ == "" {
		typ = defaultType
	}

	seen[fullURL] = true
	*items = append(*items, model.NewsItem{
		Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now(),
	})
}

func civilValidNewsTitle(title string, item, anchor *goquery.Selection) bool {
	if isValidNewsTitle(title) {
		return true
	}
	// The shared title filter excludes these national-news headlines because
	// they are hero cards on the civil home page. On tmyw.htm they are genuine
	// paginated list rows and must not disappear from the feed.
	if civilIsHeroAnnouncement(title) && !civilIsHomeFeatured(item, anchor) {
		return true
	}
	return false
}

func civilIsHeroAnnouncement(title string) bool {
	for _, prefix := range []string{
		"习近平给四所交通大学全体师生",
		"习近平总书记给四所交通大学全体师生",
	} {
		if strings.HasPrefix(title, prefix) {
			return true
		}
	}
	return false
}

func civilIsHomeFeatured(item, anchor *goquery.Selection) bool {
	for _, current := range []*goquery.Selection{item, anchor} {
		if current != nil && current.Length() > 0 && current.Closest(".preface-new-box").Length() > 0 {
			return true
		}
	}
	return false
}

func civilListType(item, anchor *goquery.Selection) string {
	for _, current := range []*goquery.Selection{item, anchor} {
		if current == nil || current.Length() == 0 {
			continue
		}
		for _, selector := range []string{".list", ".tabcons", ".tabCon"} {
			if panel := current.Closest(selector); panel.Length() > 0 {
				if heading := panel.Find("h4, h3.intit").First(); heading.Length() > 0 {
					value := strings.Join(strings.Fields(heading.Text()), " ")
					value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(value, "更多>>"), "更多"))
					if value != "" {
						return value
					}
				}
			}
		}
		if panel := current.Closest(".preface-new-box"); panel.Length() > 0 {
			if heading := panel.Find("h3.intit").First(); heading.Length() > 0 {
				value := strings.Join(strings.Fields(heading.Text()), " ")
				value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(value, "更多>>"), "更多"))
				if value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func civilListPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{`meta[name="pageTitle"]`, `meta[name="pagetitle"]`} {
		if value := cleanCivilText(doc.Find(selector).First().AttrOr("content", "")); value != "" {
			// pageType=1 is the home page, whose pageTitle is the site name;
			// that is not a content type. Category/list pages use pageType=2
			// and expose their actual tab label here.
			pageType := cleanCivilText(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
			if pageType != "1" && value != "土木工程学院" && value != "网站首页" {
				return value
			}
		}
	}
	for _, selector := range []string{".right .title h3", ".contents .title h3", ".news_list h3"} {
		if heading := doc.Find(selector).First(); heading.Length() > 0 {
			if value := cleanCivilText(heading.Text()); value != "" && !isCivilHeading(value) {
				return value
			}
		}
	}
	return ""
}

func isCivilDecoration(sel *goquery.Selection) bool {
	if isInNavArea(sel) {
		return true
	}
	return sel.Closest(".banner, .swiper, .swiper-slide, .carousel, .focus, .hero, .scenery-box, .recruit, .news-img-box, .footer").Length() > 0
}

func civilArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !isValidNewsURL(fullURL) {
		return "", false
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return "", false
	}

	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Hostname() == "" {
		return "", false
	}
	parsed.Fragment = ""
	fullURL = cleanURL(parsed.String())

	if strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		if civilArticlePathRe.MatchString(parsed.Path) || civilLocalResourceURL(parsed) || civilDownloadURL(parsed) {
			return fullURL, true
		}
		return "", false
	}

	// One legacy list (yjspy/yjskcb) still points to the historical Vatuu
	// detail host. Keep the real URL so a subsequent fetch can report its
	// actual HTTP failure; do not rewrite it into a fabricated local article.
	if civilLegacyArticleURL(parsed) {
		return fullURL, true
	}
	return "", false
}

func civilLocalResourceURL(parsed *url.URL) bool {
	if parsed == nil || !strings.HasPrefix(strings.ToLower(parsed.Path), "/__local/") {
		return false
	}
	if civilResourcePathRe.MatchString(parsed.Path) {
		return true
	}
	return civilResourceQueryRe.MatchString(parsed.Query().Get("e"))
}

func civilDownloadURL(parsed *url.URL) bool {
	if parsed == nil || !strings.EqualFold(parsed.Path, "/system/_content/download.jsp") {
		return false
	}
	return strings.Contains(strings.ToLower(parsed.Query().Get("urltype")), "downloadattachurl")
}

func civilLegacyArticleURL(parsed *url.URL) bool {
	if parsed == nil || !strings.EqualFold(parsed.Hostname(), "civil.ix2.com") || !strings.EqualFold(parsed.Path, "/vatuu/WebAction") {
		return false
	}
	query := parsed.Query()
	return strings.EqualFold(query.Get("setAction"), "newsDetail") && strings.TrimSpace(query.Get("newsId")) != ""
}

func cleanCivilText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func cleanCivilTitle(value string) string {
	value = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff', '\u2060':
			return -1
		default:
			return r
		}
	}, value)
	return cleanTitle(value)
}

func civilDateFromItem(sel *goquery.Selection) string {
	date, _ := civilDateValuesFromItem(sel)
	return date
}

func civilDateValuesFromItem(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}

	// The homepage uses <time> and the simple tab lists use an unclassed
	// sibling <span> containing YYYY-MM-DD. Check those first.
	if date, publishedAt := civilDateValuesFromCandidates(sel.Find("time")); date != "" {
		return date, publishedAt
	}
	if date, publishedAt := civilDateValuesFromCandidates(sel.ChildrenFiltered("span, time")); date != "" {
		return date, publishedAt
	}

	// List pages have used .date/.time, .news-time, and class names containing
	// date/time. A date scope is safer than searching the whole li, whose title
	// or summary may mention unrelated dates.
	if date, publishedAt := civilDateValuesFromCandidates(sel.Find(".date, .time, .news-date, .news-time, [class*='date'], [class*='time']")); date != "" {
		return date, publishedAt
	}

	return "", ""
}

func civilDateFromCandidates(candidates *goquery.Selection) string {
	date, _ := civilDateValuesFromCandidates(candidates)
	return date
}

func civilDateValuesFromCandidates(candidates *goquery.Selection) (string, string) {
	var date, publishedAt string
	candidates.EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
		date, publishedAt = civilDateValuesFromScope(candidate)
		return date == ""
	})
	return date, publishedAt
}

func civilDateFromScope(scope *goquery.Selection) string {
	date, _ := civilDateValuesFromScope(scope)
	return date
}

func civilDateValuesFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}
	text := cleanCivilText(scope.Text())
	if datetime := cleanCivilText(scope.AttrOr("datetime", "")); datetime != "" {
		text = strings.TrimSpace(text + " " + datetime)
	}
	if match := civilDateTimeRe.FindStringSubmatch(text); match != nil {
		date := normalizeCivilDate(match[1], match[2], match[3])
		if date == "" {
			return "", ""
		}
		second := match[6]
		if second == "" {
			second = "00"
		}
		return date, fmt.Sprintf("%s %02s:%s:%s", date, match[4], match[5], second)
	}
	if match := civilDateRe.FindStringSubmatch(text); match != nil {
		return normalizeCivilDate(match[1], match[2], match[3]), ""
	}

	// Some list pages render the date as "07" plus "2026-04" instead of a
	// single full date. Only combine parts found in the same date scope.
	year, month, day := "", "", ""
	if match := civilYearMonthRe.FindStringSubmatch(text); match != nil {
		year, month = match[1], match[2]
	}
	scope.Find(".day, [class*='day']").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if match := civilDayRe.FindStringSubmatch(strings.TrimSpace(sel.Text())); match != nil {
			day = match[1]
			return false
		}
		return true
	})
	if day == "" {
		// Handle an unclassed "07 2026-04" date block without interpreting
		// digits from the article title as a day. The live civil template may
		// omit whitespace between adjacent date divs, producing "072026-04".
		if match := civilSplitDateRe.FindStringSubmatch(text); match != nil {
			day = match[1]
		}
	}
	if year != "" && month != "" && day != "" {
		return normalizeCivilDate(year, month, day), ""
	}
	return "", ""
}

func normalizeCivilDate(year, month, day string) string {
	y, yearErr := strconv.Atoi(year)
	m, monthErr := strconv.Atoi(month)
	d, dayErr := strconv.Atoi(day)
	if yearErr != nil || monthErr != nil || dayErr != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return ""
	}
	date := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if date.Year() != y || int(date.Month()) != m || date.Day() != d {
		return ""
	}
	return date.Format("2006-01-02")
}

func parseCivilArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse civil article html: %w", err)
	}
	if err := civilResponseError(doc); err != nil {
		return nil, err
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	if title := civilArticleTitle(doc); title != "" {
		article.Title = title
	} else {
		article.Title = cleanCivilTitle(article.Title)
	}
	if date := civilArticleDate(doc); date != "" {
		article.Date = date
	}
	if publishedAt := civilArticlePublishedAt(doc); publishedAt != "" {
		article.PublishedAt = publishedAt
	}
	// The civil template always renders these labels in .top-info, even when
	// their values are empty. Override generic whole-page fallback values so
	// an empty "来源：" cannot become the next label, e.g. "分类".
	if civilArticleMetadata(doc).Length() > 0 {
		article.Source = civilArticleSource(doc)
		article.Type = civilArticleType(doc)
	}
	addCivilArticleAttachments(article, doc, baseURL)
	if strings.TrimSpace(article.Title) == "" {
		return nil, fmt.Errorf("civil: article response has no recognized title (possible static, SPA, WAF, or login shell)")
	}
	if strings.TrimSpace(article.Content) == "" && strings.TrimSpace(article.ContentHTML) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("civil: article %q has no recognized body/content", article.Title)
	}
	return article, nil
}

func civilArticleTitle(doc *goquery.Document) string {
	selectors := []string{
		".top-title h1, .top-title h2, .top-title h3",
		".top-tit h1, .top-tit h2, .top-tit h3",
		".article-title, .detail-title, .news-title",
		"h3",
	}
	for _, selector := range selectors {
		var title string
		doc.Find(selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			candidate := cleanCivilTitle(strings.TrimSpace(sel.Text()))
			if candidate != "" && utf8.RuneCountInString(candidate) <= 100 && !isCivilHeading(candidate) {
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

func isCivilHeading(text string) bool {
	for _, heading := range []string{"土木要闻", "新闻列表", "通知公告", "教育教学", "科研工作", "学生工作", "招生就业"} {
		if text == heading {
			return true
		}
	}
	return false
}

func civilArticleMetadata(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{".top-info", ".article-info", ".detail-info", ".news-info", ".news_source"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			return found
		}
	}
	return doc.Find("__civil_no_metadata__")
}

func civilMetadataText(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	return strings.Join(strings.Fields(flattenedText(sel)), " ")
}

func civilMetadataValue(doc *goquery.Document, valueRe *regexp.Regexp) string {
	for _, selector := range []string{".top-info", ".article-info", ".detail-info", ".news-info", ".news_source"} {
		var value string
		doc.Find(selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			match := valueRe.FindStringSubmatch(civilMetadataText(sel))
			if len(match) > 1 {
				value = strings.TrimSpace(match[1])
			}
			return value == ""
		})
		if value != "" {
			return value
		}
	}
	return ""
}

func civilArticleDate(doc *goquery.Document) string {
	for _, selector := range []string{".top-info", ".article-info", ".detail-info", ".news-info", ".news_source"} {
		var date string
		doc.Find(selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			text := strings.Join(strings.Fields(sel.Text()), " ")
			if match := civilDateRe.FindStringSubmatch(text); match != nil {
				date = normalizeCivilDate(match[1], match[2], match[3])
			}
			return date == ""
		})
		if date != "" {
			return date
		}
	}
	return ""
}

func civilArticlePublishedAt(doc *goquery.Document) string {
	for _, selector := range []string{".top-info", ".article-info", ".detail-info", ".news-info", ".news_source"} {
		var publishedAt string
		doc.Find(selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			match := civilDateTimeRe.FindStringSubmatch(civilMetadataText(sel))
			if len(match) == 0 {
				return true
			}
			date := normalizeCivilDate(match[1], match[2], match[3])
			if date == "" {
				return true
			}
			second := match[6]
			if second == "" {
				second = "00"
			}
			publishedAt = fmt.Sprintf("%s %02s:%s:%s", date, match[4], match[5], second)
			return false
		})
		if publishedAt != "" {
			return publishedAt
		}
	}
	return ""
}

func civilArticleSource(doc *goquery.Document) string {
	return civilMetadataValue(doc, civilSourceValueRe)
}

func civilArticleType(doc *goquery.Document) string {
	return civilMetadataValue(doc, civilTypeValueRe)
}

func addCivilArticleAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}

	// civil puts the attachment <ul> after .v_news_content. The generic Boda
	// parser deliberately scopes links to the content body, so these links
	// need a site-specific pass. Requiring the CMS download endpoint and an
	// enclosing "附件" list item keeps ordinary body links out.
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		lowerHref := strings.ToLower(href)
		if href == "" || !strings.Contains(lowerHref, "download.jsp") || !strings.Contains(lowerHref, "downloadattachurl") || isInNavArea(sel) {
			return
		}
		listItem := sel.Closest("li")
		if listItem.Length() == 0 || !strings.Contains(listItem.Text(), "附件") {
			return
		}
		name := strings.TrimSpace(sel.Text())
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
