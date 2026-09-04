package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// scai.swjtu.edu.cn uses its own static page template instead of the usual
// 博达 list markup. Its home page has several article sections and a separate
// five-slide banner.
func init() {
	registerAdapter("scai", SiteAdapter{
		SubPages:     scaiSubPages(),
		ParseList:    parseScaiAdapterList,
		ParseArticle: parseScaiArticle,
		PageURL:      scaiPageURL,
		ListPage:     scaiListPage,
		TotalPages:   scaiTotalPages,
	})
}

var (
	scaiJavaDateRe        = regexp.MustCompile("(?i)\\b(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)\\s+(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\\s+\\d{1,2}\\s+\\d{2}:\\d{2}:\\d{2}\\s+[A-Z]{2,5}\\s+\\d{4}\\b")
	scaiNumericDateRe     = regexp.MustCompile("(20\\d{2})[\\s\\x{a0}]*[-/.年][\\s\\x{a0}]*(\\d{1,2})[\\s\\x{a0}]*[-/.月][\\s\\x{a0}]*(\\d{1,2})")
	scaiYearMonthRe       = regexp.MustCompile("(20\\d{2})[\\s\\x{a0}]*[-/.年][\\s\\x{a0}]*([01]?\\d)")
	scaiDayRe             = regexp.MustCompile(`^\s*([0-3]?\d)\s*$`)
	scaiPublishLabelRe    = regexp.MustCompile("(?:发布日期|发布时间|发表日期|发表时间)\\s*[:：]?")
	scaiBlockedPageRe     = regexp.MustCompile("(?i)(?:您的访问请求可能对网站造成安全威胁|您的请求可能存在威胁，已被拦截|网站防火墙|访问被拒绝|请求已被阻断)")
	scaiUnavailablePageRe = regexp.MustCompile("(?i)(?:统一身份认证|用户登录|请登录|cas登录|enable javascript|javascript required|\\blogin\\b|\\bsign\\s+in\\b|loading\\.\\.\\.)")
	scaiSPAShellRe        = regexp.MustCompile(`(?is)<(?:div|main)[^>]*\bid\s*=\s*["'](?:app|root)["'][^>]*>\s*</(?:div|main)>`)
	scaiAttachmentNumRe   = regexp.MustCompile(`^\s*\d+\.\s*`)
	scaiTotalPageRe       = regexp.MustCompile(`(?s)共\s*[【\[]?\s*(\d+)\s*[】\]]?\s*页`)
	scaiCurrentPageRe     = regexp.MustCompile(`(?s)当前第\s*[【\[]?\s*(\d+)\s*[】\]]?\s*页`)
	scaiGotoPageRe        = regexp.MustCompile(`(?i)gotoPage\s*\(\s*(\d+)\s*\)`)
)

var (
	errScaiBlockedPage     = errors.New("scai: received the site's WAF block page")
	errScaiUnavailablePage = errors.New("scai: received a login, SPA, or unavailable page")
	errScaiNoArticles      = errors.New("scai: no article list or homepage content surface found")
	errScaiNoArticleDetail = errors.New("scai: article detail has no headline and content surface")
)

// scaiFeedRoute describes a public article-list route. The site uses opaque
// module IDs instead of the static slug.htm convention assumed by the
// shared Boda client. A few navigation entries are parents which silently
// resolve to a child module; only the canonical child route is registered so
// the feed list does not contain duplicate columns.
type scaiFeedRoute struct {
	slug string
	mid  string
	tid  string
}

var scaiFeedRoutes = []scaiFeedRoute{
	{slug: "focus", mid: "860AF115971C57D6"},
	{slug: "notice", mid: "F4DFBF47DF89091A"},
	{slug: "news", mid: "12D0D8DF28CC1D1D"},
	{slug: "notice/files", mid: "F4DFBF47DF89091A", tid: "360"},

	{slug: "party/notice", mid: "B7EB5FEBE48C6A1F"},
	{slug: "party/dynamics", mid: "639B795ABD71EEA4"},
	{slug: "party/topics", mid: "FF4D47A2192F9EFD"},
	{slug: "party/discipline", mid: "7FD9BCBF38B1ECB2"},
	{slug: "party/development", mid: "8A38FE4A5578623F"},
	{slug: "party/care", mid: "76302D048147C5E2"},

	{slug: "research/academic", mid: "A58752D2B803263D"},
	{slug: "research/notice", mid: "94FB6132BF38AC22"},

	{slug: "undergraduate", mid: "B730BEB095B31840"},
	{slug: "undergraduate/notice", mid: "B730BEB095B31840", tid: "350"},
	{slug: "undergraduate/program", mid: "B730BEB095B31840", tid: "351"},
	{slug: "undergraduate/regulations", mid: "B730BEB095B31840", tid: "352"},
	{slug: "undergraduate/downloads", mid: "B730BEB095B31840", tid: "353"},
	{slug: "undergraduate/news", mid: "B730BEB095B31840", tid: "367"},
	{slug: "graduate", mid: "6A69B0E32021446B"},
	{slug: "graduate/notice", mid: "6A69B0E32021446B", tid: "362"},
	{slug: "graduate/training", mid: "6A69B0E32021446B", tid: "363"},
	{slug: "graduate/degree", mid: "6A69B0E32021446B", tid: "364"},
	{slug: "graduate/exit", mid: "6A69B0E32021446B", tid: "365"},
	{slug: "graduate/downloads", mid: "6A69B0E32021446B", tid: "366"},

	{slug: "exchange", mid: "99D2369E2EFC0315"},
	{slug: "exchange/teacher", mid: "99D2369E2EFC0315", tid: "368"},
	{slug: "exchange/student", mid: "99D2369E2EFC0315", tid: "369"},
	{slug: "exchange/inbound", mid: "99D2369E2EFC0315", tid: "370"},
	{slug: "exchange/conference", mid: "99D2369E2EFC0315", tid: "371"},

	{slug: "recruitment/jobs", mid: "B289DCB5E1C69F7C"},
	{slug: "recruitment/publicity", mid: "E2F71BA86B774E97"},
	{slug: "students/notice", mid: "F3D3909EB1861B5D"},
	{slug: "students/news", mid: "9B0C69EAC8A7258F"},
	{slug: "students/publicity", mid: "8EEAD67FC6D491AC"},
	{slug: "students/competition", mid: "2142F5BFAC6C18A3"},

	{slug: "employment/info", mid: "8C9FAC060A7B2F96"},
	{slug: "employment/cooperation", mid: "45CE7200B910DCD0"},
	{slug: "employment/admission", mid: "460A52E2C81B6D0B"},
	{slug: "employment/equivalence", mid: "4A788489F15AE757"},
	{slug: "alumni/news", mid: "756E215058F52683"},
	{slug: "alumni/graduation", mid: "E1CE0987AEFFA8CA"},
}

func scaiSubPages() []string {
	pages := make([]string, 0, len(scaiFeedRoutes))
	for _, route := range scaiFeedRoutes {
		pages = append(pages, route.slug)
	}
	return pages
}

func scaiRouteForSlug(slug string) (scaiFeedRoute, bool) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || strings.EqualFold(slug, "index") {
		return scaiFeedRoute{slug: "index"}, true
	}
	for _, route := range scaiFeedRoutes {
		if strings.EqualFold(slug, route.slug) || strings.EqualFold(slug, route.mid) {
			return route, true
		}
	}
	switch slug {
	case "焦点新闻":
		return scaiFeedRoutes[0], true
	case "通知公告":
		return scaiFeedRoutes[1], true
	case "学院新闻":
		return scaiFeedRoutes[2], true
	case "党建通知公告":
		return scaiFeedRoutes[4], true
	case "科研公告":
		return scaiFeedRoutes[11], true
	case "本科生教育":
		return scaiFeedRoutes[12], true
	case "研究生教育":
		return scaiFeedRoutes[18], true
	case "国际交流":
		return scaiFeedRoutes[24], true
	}
	return scaiFeedRoute{}, false
}

// scaiPageURL maps the browser's logical page number to the query-string
// route emitted by page-module.html. It deliberately keeps tid on sub-tab
// requests; the site's own gotoPage JavaScript drops it, which otherwise
// sends page 2 of every sub-tab back to the aggregate module.
func scaiPageURL(baseURL, slug string, page, _ int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if extension == "" {
		extension = ".htm"
	}
	route, ok := scaiRouteForSlug(slug)
	if !ok {
		// Keep the shared fallback behavior for an opaque caller-supplied slug.
		pathSlug := strings.Trim(slug, "/")
		if page <= 1 {
			return baseURL + "/" + pathSlug + extension
		}
		return baseURL + "/" + pathSlug + "/" + strconv.Itoa(page) + extension
	}
	if route.slug == "index" {
		return baseURL + "/index.html"
	}
	query := url.Values{"mid": []string{route.mid}}
	if route.tid != "" {
		query.Set("tid", route.tid)
	}
	if page > 1 {
		query.Set("page", strconv.Itoa(page))
		// The live site is served by a cache layer which has intermittently
		// returned page 1 for the canonical page=2 URL. An ignored, stable
		// adapter parameter gives each logical page its own cache key while
		// leaving the site's page parameter untouched.
		query.Set("swjtu_adapter_page", strconv.Itoa(page))
	}
	return baseURL + "/web/page-module.html?" + query.Encode()
}

func scaiPageURLWithRetry(baseURL, slug string, page, attempt int) string {
	pageURL := scaiPageURL(baseURL, slug, page, 0, ".html")
	if attempt <= 0 {
		return pageURL
	}
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return pageURL
	}
	query := parsed.Query()
	query.Set("scai_adapter_retry", strconv.Itoa(attempt))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// scaiListPage verifies the page number echoed by the live HTML. The site's
// edge cache has been observed to serve page 1/2 for another requested page;
// accepting that response would silently deduplicate away most of a feed.
// Bounded query-parameter retries give the cache a fresh key, and a
// persistent mismatch becomes a diagnostic error instead of bad data.
func scaiListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("scai: list page must be positive: %d", page)
	}
	route, ok := scaiRouteForSlug(slug)
	if !ok {
		return nil, 0, fmt.Errorf("scai: unknown feed %q", slug)
	}
	site := AllSites["scai"]
	if site == nil {
		return nil, 0, errors.New("scai: site registration is missing")
	}

	const maxAttempts = 5
	var body []byte
	lastReportedPage := 0
	for attempt := 0; attempt <= maxAttempts; attempt++ {
		var err error
		body, err = fetch(scaiPageURLWithRetry(site.BaseURL, slug, page, attempt))
		if err != nil {
			return nil, 0, err
		}
		if scaiIsBlockedPage(body) {
			return nil, 0, errScaiBlockedPage
		}
		reportedPage := scaiCurrentPageFromHTML(body)
		if reportedPage == 0 || reportedPage == page {
			break
		}
		lastReportedPage = reportedPage
		if attempt == maxAttempts {
			return nil, 0, fmt.Errorf("scai: requested page %d but page-module reported page %d after %d attempts", page, lastReportedPage, maxAttempts+1)
		}
	}

	items, err := parseScaiAdapterList(body, site.BaseURL)
	if err != nil {
		return nil, 0, err
	}
	if route.slug == "index" {
		return items, 1, nil
	}
	totalPages := scaiTotalPagesFromHTML(body)
	if totalPages == 0 && scaiHasPaginationMarker(body) {
		// Keep the valid zero-item page usable by the shared range walker.
		totalPages = 1
	}
	if totalPages == 0 {
		return nil, 0, errors.New("scai: page-module HTML has no total-page marker")
	}
	return items, totalPages, nil
}

// scaiTotalPages reads the page count from the same page-module HTML that the
// browser displays. The shared parser only knows older .p_pages markup, while
// SCAI emits the count in an HTML comment and repeats the last page in
// gotoPage(N). There is no separate count API on this site.
func scaiTotalPages(slug string) (int, error) {
	route, ok := scaiRouteForSlug(slug)
	if !ok {
		return 0, fmt.Errorf("scai: unknown feed %q", slug)
	}
	if route.slug == "index" {
		return 1, nil
	}
	site := AllSites["scai"]
	if site == nil {
		return 0, errors.New("scai: site registration is missing")
	}
	body, err := fetch(scaiPageURL(site.BaseURL, slug, 1, 0, ".html"))
	if err != nil {
		return 0, err
	}
	if scaiIsBlockedPage(body) {
		return 0, errScaiBlockedPage
	}
	if total := scaiTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	if scaiHasPaginationMarker(body) {
		// The shared range walker treats zero as an unknown count for custom
		// adapters and returns an error before it can preserve a valid empty
		// result. Page 1 is still a real, fetchable page for these feeds.
		return 1, nil
	}
	return 0, errors.New("scai: page-module HTML has no total-page marker")
}

func scaiTotalPagesFromHTML(raw []byte) int {
	if match := scaiTotalPageRe.FindSubmatch(raw); match != nil {
		pageCount, _ := strconv.Atoi(string(match[1]))
		return pageCount
	}
	maxPage := 0
	for _, match := range scaiGotoPageRe.FindAllSubmatch(raw, -1) {
		page, _ := strconv.Atoi(string(match[1]))
		if page > maxPage {
			maxPage = page
		}
	}
	return maxPage
}

func scaiCurrentPageFromHTML(raw []byte) int {
	match := scaiCurrentPageRe.FindSubmatch(raw)
	if match == nil {
		return 0
	}
	page, _ := strconv.Atoi(string(match[1]))
	return page
}

func scaiHasPaginationMarker(raw []byte) bool {
	return scaiTotalPageRe.Match(raw) || scaiGotoPageRe.Match(raw)
}

// parseScaiAdapterList extracts each article from its own link and its
// nearest item container. On scai, dates are siblings of ordinary links but
// descendants of featured links, so extracting them from the whole page
// would misalign dates and titles.
func parseScaiAdapterList(html []byte, baseURL string) ([]model.NewsItem, error) {
	if scaiIsBlockedPage(html) {
		return nil, errScaiBlockedPage
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		if sel.HasClass("detail-btn") || isInNavArea(sel) || scaiIsBannerLink(sel) {
			return
		}

		href, _ := sel.Attr("href")
		fullURL, ok := scaiArticleURL(baseURL, href)
		if !ok || seen[fullURL] {
			return
		}

		title := scaiListTitle(sel)
		if !scaiIsArticleTitle(title) {
			return
		}

		seen[fullURL] = true
		date, publishedAt := scaiListDateInfo(sel)
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        scaiListTypeForAnchor(sel, doc),
			ParsedAt:    time.Now(),
		})
	})
	if len(items) == 0 {
		if scaiIsUnavailablePage(html) {
			return nil, errScaiUnavailablePage
		}
		if !scaiHasContentSurface(doc) {
			return nil, errScaiNoArticles
		}
	}

	return items, nil
}

func scaiHasContentSurface(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".list-big-title, .list-top, .list-bottom-item, .big-title, .s-title, .top-news, .news-center, .bottom-list-1, .bottom-list-2, .bottom-list-3, .banner.swiper-container").Length() > 0
}

func scaiIsBannerLink(sel *goquery.Selection) bool {
	for _, selector := range []string{
		".banner",
		".swiper-container",
		".swiper-wrapper",
		".swiper-slide",
		".carousel",
		".slider",
		".focus",
		".hero",
	} {
		if sel.Closest(selector).Length() > 0 {
			return true
		}
	}
	return false
}

func scaiArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !strings.EqualFold(path.Base(parsed.Path), "page-newsdetail.html") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if strings.TrimSpace(parsed.Query().Get("nid")) == "" || !isValidNewsURL(fullURL) {
		return "", false
	}

	// A fragment does not identify another article and would otherwise defeat
	// de-duplication if a template adds one.
	parsed.Fragment = ""
	return parsed.String(), true
}

func scaiListTitle(sel *goquery.Selection) string {
	title := strings.TrimSpace(sel.AttrOr("title", ""))
	if title == "" {
		for _, selector := range []string{".title", "h1, h2, h3, h4, h5"} {
			if heading := sel.Find(selector).First(); heading.Length() > 0 {
				title = strings.TrimSpace(heading.Text())
				if title != "" {
					break
				}
			}
		}
	}
	if title == "" {
		title = strings.TrimSpace(sel.Text())
	}
	return strings.Join(strings.Fields(title), " ")
}

func scaiIsArticleTitle(title string) bool {
	if title == "" || !isValidNewsTitle(title) {
		return false
	}
	// The old parser used len(title), which counts UTF-8 bytes and discarded
	// legitimate long Chinese headlines. Bound by runes instead.
	if utf8.RuneCountInString(title) > 200 {
		return false
	}
	return !strings.HasPrefix(title, "了解详情")
}

func scaiListDate(anchor *goquery.Selection) string {
	date, _ := scaiListDateInfo(anchor)
	return date
}

func scaiListDateInfo(anchor *goquery.Selection) (string, string) {
	if anchor == nil || anchor.Length() == 0 {
		return "", ""
	}
	// Featured cards keep their date inside the article link.
	if date, publishedAt := scaiDateInfoInSelection(anchor); date != "" {
		return date, publishedAt
	}

	// Ordinary section rows put the date next to the link in .list-item.
	for _, selector := range []string{".list-item", ".list-top-item", ".item-wrapper", ".news-top-wrapper", ".top-news-item"} {
		if item := anchor.Closest(selector); item.Length() > 0 {
			if date, publishedAt := scaiDateInfoInSelection(item); date != "" {
				return date, publishedAt
			}
		}
	}
	return "", ""
}

func scaiListTypeForAnchor(anchor *goquery.Selection, doc *goquery.Document) string {
	if anchor != nil && anchor.Length() > 0 {
		// Home-page sections share a parent .container, so selecting the
		// first heading there labels every item as the first section. Find
		// the nearest section heading before falling back to that container.
		for _, ancestorSelector := range []string{".notice", ".learn", ".bottom-list-1", ".bottom-list-2", ".bottom-list-3", ".top-news", ".container"} {
			container := anchor.Closest(ancestorSelector)
			if container.Length() == 0 {
				continue
			}
			if value := scaiHeadingType(container); value != "" {
				return value
			}
		}
	}
	return scaiListType(doc)
}

func scaiListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-big-title", ".big-title span", ".news-title-wrapper", ".banner .title-wrapper h1"} {
		values := doc.Find(selector)
		for i := values.Length() - 1; i >= 0; i-- {
			value := strings.Join(strings.Fields(values.Eq(i).Text()), " ")
			if selector == ".list-big-title" {
				value = scaiListHeadingText(values.Eq(i))
			}
			if value != "" && value != "首页" && value != "更多" {
				return value
			}
		}
	}
	return ""
}

func scaiHeadingType(container *goquery.Selection) string {
	if container == nil || container.Length() == 0 {
		return ""
	}
	if heading := container.Find(".list-big-title").First(); heading.Length() > 0 {
		if active := heading.Find("a.active").First(); active.Length() > 0 {
			if value := strings.Join(strings.Fields(active.Text()), " "); value != "" {
				return value
			}
		}
		if value := scaiListHeadingText(heading); value != "" {
			return value
		}
	}
	for _, selector := range []string{".s-title span", ".big-title span", ".title-wrapper h1", ".title-wrapper h2"} {
		if value := strings.Join(strings.Fields(container.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func scaiListHeadingText(heading *goquery.Selection) string {
	if heading == nil || heading.Length() == 0 {
		return ""
	}
	if active := heading.Find("a.active").First(); active.Length() > 0 {
		if value := strings.Join(strings.Fields(active.Text()), " "); value != "" {
			return value
		}
	}
	clone := heading.Clone()
	clone.Find("a, span").Remove()
	return strings.Join(strings.Fields(clone.Text()), " ")
}

func scaiDateInSelection(sel *goquery.Selection) string {
	date, _ := scaiDateInfoInSelection(sel)
	return date
}

func scaiPublishedAtInSelection(sel *goquery.Selection) string {
	_, publishedAt := scaiDateInfoInSelection(sel)
	return publishedAt
}

func scaiDateInfoInSelection(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	// The same live template has two renderings: some responses contain a
	// complete Java Date in .day, while others split it into .date=2026/08
	// and .day=03. Check the calendar as a unit before checking individual
	// date nodes so the latter is not discarded as an incomplete date.
	var calendarDate, calendarPublishedAt string
	sel.Find(".calendar").EachWithBreak(func(_ int, calendar *goquery.Selection) bool {
		if date, publishedAt := scaiDateInfoFromText(calendar.Text()); date != "" {
			calendarDate, calendarPublishedAt = date, publishedAt
			return false
		}
		month := strings.TrimSpace(calendar.Find(".date").First().Text())
		day := strings.TrimSpace(calendar.Find(".day").First().Text())
		if monthMatch := scaiYearMonthRe.FindStringSubmatch(month); monthMatch != nil {
			if dayMatch := scaiDayRe.FindStringSubmatch(day); dayMatch != nil {
				calendarDate = normalizeDateParts(monthMatch[1], monthMatch[2], dayMatch[1])
				return calendarDate == ""
			}
		}
		return true
	})
	if calendarDate != "" {
		return calendarDate, calendarPublishedAt
	}
	for _, selector := range []string{
		".calendar .day",
		".calendar .date",
		".date-label",
		"time[datetime]",
		"time",
		"[data-date]",
	} {
		if dateSel := sel.Find(selector).First(); dateSel.Length() > 0 {
			for _, attr := range []string{"datetime", "data-date"} {
				if value := dateSel.AttrOr(attr, ""); value != "" {
					if date, publishedAt := scaiDateInfoFromText(value); date != "" {
						return date, publishedAt
					}
				}
			}
			if date, publishedAt := scaiDateInfoFromText(dateSel.Text()); date != "" {
				return date, publishedAt
			}
		}
	}
	return "", ""
}

func scaiDateFromText(text string) string {
	date, _ := scaiDateInfoFromText(text)
	return date
}

func scaiDateInfoFromText(text string) (string, string) {
	text = strings.Join(strings.Fields(text), " ")
	if match := scaiJavaDateRe.FindString(text); match != "" {
		if parsed, err := time.Parse("Mon Jan 02 15:04:05 MST 2006", match); err == nil {
			return parsed.Format("2006-01-02"), parsed.Format("2006-01-02 15:04:05")
		}
	}
	if publishedAt := normalizePublishedAt(text); len(publishedAt) >= len("2006-01-02") {
		return publishedAt[:len("2006-01-02")], publishedAt
	}
	if match := scaiNumericDateRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func scaiIsBlockedPage(raw []byte) bool {
	return scaiBlockedPageRe.Match(raw)
}

func scaiIsUnavailablePage(raw []byte) bool {
	return scaiUnavailablePageRe.Match(raw) || scaiSPAShellRe.Match(raw)
}

// parseScaiArticle keeps the shared content/image/attachment extraction but
// owns scai's detail-page metadata hook. The detail template renders a Java
// Date after 发布日期, which is normalized before entering the model.
func parseScaiArticle(html []byte, baseURL string) (*model.Article, error) {
	if scaiIsBlockedPage(html) {
		return nil, errScaiBlockedPage
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}
	if !scaiHasArticleDetailSurface(doc) {
		if scaiIsUnavailablePage(html) {
			return nil, errScaiUnavailablePage
		}
		return nil, errScaiNoArticleDetail
	}
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	if title := scaiArticleTitle(doc); title != "" {
		article.Title = title
	}
	if source := scaiArticleSource(doc); source != "" {
		article.Source = source
	}
	if typ := scaiArticleType(doc); typ != "" {
		article.Type = typ
	}
	if date, publishedAt := scaiArticleDateInfo(doc); date != "" {
		article.Date = date
		if publishedAt != "" {
			article.PublishedAt = publishedAt
		}
	} else if date := scaiDateFromText(article.Date); date != "" {
		article.Date = date
	}
	article.Content, article.ContentHTML, article.Images, article.Attachments = scaiArticleContent(doc, baseURL)
	return article, nil
}

func scaiHasArticleDetailSurface(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	content := scaiArticleContentSelection(doc)
	if content == nil || content.Length() == 0 {
		return false
	}
	if scaiArticleTitle(doc) != "" {
		return true
	}
	// Keep the synthetic/legacy detail variant supported when its headline is
	// outside .news-content-wrapper, while still requiring a detail class.
	for _, selector := range []string{"h1.news-title", ".article-title", ".detail-title"} {
		if found := doc.Find(selector).First(); found.Length() > 0 && scaiIsArticleTitle(strings.Join(strings.Fields(found.Text()), " ")) {
			return true
		}
	}
	return false
}

func scaiArticleSource(doc *goquery.Document) string {
	for _, selector := range []string{".news-content-wrapper .news-info", ".news-info", ".news_source", ".news-source"} {
		found := doc.Find(selector).First()
		if found.Length() == 0 {
			continue
		}
		var source string
		found.Find("span, div, p").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			text := strings.Join(strings.Fields(sel.Text()), " ")
			if strings.HasPrefix(text, "来源：") || strings.HasPrefix(text, "来源:") {
				source = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "来源："), "来源:"))
				return false
			}
			return true
		})
		if source != "" {
			return source
		}
	}
	return ""
}

func scaiArticleContent(doc *goquery.Document, baseURL string) (string, string, []string, []model.Attachment) {
	content := scaiArticleContentSelection(doc)
	if content == nil || content.Length() == 0 {
		return "", "", nil, nil
	}

	attachments := scaiArticleAttachments(content, baseURL)

	htmlContent := content.Clone()
	seenImages := make(map[string]bool)
	var images []string
	htmlContent.Find("img").Each(func(_ int, sel *goquery.Selection) {
		src := scaiImageSource(sel)
		if src == "" {
			return
		}
		resolved := scaiResolveContentURL(baseURL, src)
		if !seenImages[resolved] {
			seenImages[resolved] = true
			images = append(images, resolved)
		}
		sel.SetAttr("src", resolved)
	})
	htmlContent.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if href == "" {
			return
		}
		resolved := scaiResolveContentURL(baseURL, href)
		if resolved != "" && !strings.HasPrefix(resolved, "#") && !strings.HasPrefix(strings.ToLower(resolved), "javascript:") {
			sel.SetAttr("href", resolved)
		}
	})

	textContent := content.Clone()
	textContent.Find(".news-file, .relative-list, script, style, noscript").Remove()
	var parts []string
	textContent.Find("p, h1, h2, h3, h4, h5").Each(func(_ int, sel *goquery.Selection) {
		text := strings.Join(strings.Fields(sel.Text()), " ")
		if text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) == 0 {
		if text := strings.Join(strings.Fields(textContent.Text()), " "); text != "" {
			parts = append(parts, text)
		}
	}
	plainText := strings.Join(parts, "\n\n")
	contentHTML, _ := htmlContent.Html()
	return plainText, contentHTML, images, attachments
}

func scaiArticleContentSelection(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	content := doc.Find(".news-content-wrapper .content-main").First()
	if content.Length() == 0 {
		content = doc.Find(".content-main").First()
	}
	return content
}

func scaiImageSource(sel *goquery.Selection) string {
	for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
		if value := strings.TrimSpace(sel.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return strings.TrimSpace(sel.AttrOr("src", ""))
}

func scaiResolveContentURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "#") || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
		return raw
	}
	return cleanURL(resolveURL(baseURL, raw))
}

func scaiArticleAttachments(content *goquery.Selection, baseURL string) []model.Attachment {
	var attachments []model.Attachment
	seen := make(map[string]bool)
	content.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if href == "" || !scaiIsAttachmentLink(sel, href) {
			return
		}
		resolved := resolveURL(baseURL, href)
		if seen[resolved] {
			return
		}
		name := strings.Join(strings.Fields(sel.Text()), " ")
		name = scaiAttachmentNumRe.ReplaceAllString(name, "")
		if name == "" {
			name = strings.TrimSpace(sel.AttrOr("download", ""))
		}
		if name == "" {
			name = path.Base(strings.TrimSuffix(strings.Split(resolved, "?")[0], "/"))
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		attachment := model.Attachment{Name: name, URL: resolved}
		attachment.Size = strings.TrimSpace(sel.AttrOr("data-size", ""))
		attachments = append(attachments, attachment)
	})
	return attachments
}

func scaiIsAttachmentLink(sel *goquery.Selection, href string) bool {
	if sel.Closest(".news-file").Length() > 0 {
		return true
	}
	if strings.TrimSpace(sel.AttrOr("download", "")) != "" {
		return true
	}
	lower := strings.ToLower(href)
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

// scaiArticleTitle is intentionally scoped to the article content wrapper.
// SCAI repeats the school name in the document title and header; when a
// detail-page variant does not expose the headline through a generic selector,
// the unscoped fallback can therefore return the school name as the article
// title. The real detail template puts the headline under .content inside
// .news-content-wrapper.
func scaiArticleTitle(doc *goquery.Document) string {
	for _, selector := range []string{
		".news-content-wrapper .content > h1.news-title",
		".news-content-wrapper .content > .news-title",
		".news-content-wrapper .content > h1",
		".news-content-wrapper .content > h2",
		".news-content-wrapper .content > .article-title",
		".news-content-wrapper .content > .detail-title",
		".news-content-wrapper .content > .title",
		".news-content-wrapper .content .news-title",
	} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			title := strings.Join(strings.Fields(found.Text()), " ")
			if scaiIsArticleTitle(title) {
				return title
			}
		}
	}
	return ""
}

func scaiArticleDate(doc *goquery.Document) string {
	date, _ := scaiArticleDateInfo(doc)
	return date
}

func scaiArticlePublishedAt(doc *goquery.Document) string {
	_, publishedAt := scaiArticleDateInfo(doc)
	return publishedAt
}

func scaiArticleDateInfo(doc *goquery.Document) (string, string) {
	if doc == nil {
		return "", ""
	}
	for _, selector := range []string{
		"meta[property=\"article:published_time\"]",
		"meta[name=\"publishdate\"]",
		"meta[name=\"publish_date\"]",
		".news_source",
		".news-source",
		".news-info",
		".news-meta",
		".article-meta",
		".article-info",
		".publish-info",
		".publish-date",
		".little-tips",
	} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			if value := found.AttrOr("content", ""); value != "" {
				if date, publishedAt := scaiDateInfoFromText(value); date != "" {
					return date, publishedAt
				}
			}
			if date, publishedAt := scaiDateInfoFromText(found.Text()); date != "" {
				return date, publishedAt
			}
		}
	}

	// If a copy of the template has no stable metadata class, only inspect
	// small elements with an explicit publish-date label. This prevents an
	// event date in the article body from becoming the publish date.
	var date, publishedAt string
	doc.Find("body *").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		text := strings.Join(strings.Fields(sel.Text()), " ")
		if utf8.RuneCountInString(text) > 256 || !scaiPublishLabelRe.MatchString(text) {
			return true
		}
		date, publishedAt = scaiDateInfoFromText(text)
		return date == ""
	})
	return date, publishedAt
}

func scaiArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{
		".banner .title-wrapper h1",
		".banner h1",
		".breadcrumb .on a",
		".breadcrumb a",
		".news-title-wrapper",
	} {
		values := doc.Find(selector)
		for i := values.Length() - 1; i >= 0; i-- {
			value := strings.Join(strings.Fields(values.Eq(i).Text()), " ")
			if value != "" && value != "首页" && value != "更多" && value != "返回列表" {
				return value
			}
		}
	}
	return ""
}
