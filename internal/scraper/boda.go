package scraper

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// ListNews fetches the news list page and parses items.
// Tries .htm first, then .html if that fails.
func (s *BodaSite) ListNews(slug string) ([]model.NewsItem, error) {
	return s.ListNewsPage(slug, 1)
}

// ListNewsWithDates fetches news and fills in missing dates from detail pages.
func (s *BodaSite) ListNewsWithDates(slug string) ([]model.NewsItem, error) {
	items, err := s.ListNewsPage(slug, 1)
	if err != nil {
		return nil, err
	}
	fillMissingDates(items, s.ParseArticleFunc, s.BaseURL)
	return items, nil
}

// fillMissingDates fetches detail pages for items missing dates.
func fillMissingDates(items []model.NewsItem, parseArticle ParseArticleFunc, baseURL string) {
	if parseArticle == nil {
		parseArticle = parseBodaArticle
	}
	for i := range items {
		if items[i].Date != "" {
			continue
		}
		body, err := fetch(items[i].URL)
		if err != nil {
			continue
		}
		art, err := parseArticle(body, baseURL)
		if err != nil || art.Date == "" {
			continue
		}
		items[i].Date = art.Date
		items[i].PublishedAt = art.PublishedAt
	}
}

// ListNewsPage fetches a specific page of news.
// page 1 = first page (slug.htm), page 2+ = slug/{total-page+2-page}.htm
func (s *BodaSite) ListNewsPage(slug string, page int) ([]model.NewsItem, error) {
	if page < 1 {
		return nil, fmt.Errorf("list page must be positive: %d", page)
	}
	if s.ListPageFunc != nil {
		items, _, err := s.ListPageFunc(slug, page)
		return normalizeNewsItems(items), err
	}

	totalPages := 0
	if page > 1 && s.PageURL != nil {
		// Some static sites number generated files from newest to oldest.
		// Let the site's URL function map the user-facing page number.
		var err error
		totalPages, err = s.GetTotalPages(slug)
		if err != nil {
			return nil, fmt.Errorf("get total pages for %q: %w", slug, err)
		}
	}
	listURL := s.listPageURL(slug, page, totalPages, ".htm")
	if listURL == "" {
		return nil, fmt.Errorf("no list URL for %q page %d/%d", slug, page, totalPages)
	}

	body, err := fetch(listURL)
	if err != nil {
		// Try .html extension
		listURL = s.listPageURL(slug, page, totalPages, ".html")
		if listURL == "" {
			return nil, fmt.Errorf("no .html list URL for %q page %d/%d", slug, page, totalPages)
		}
		body, err = fetch(listURL)
		if err != nil {
			return nil, fmt.Errorf("tried .htm and .html: %w", err)
		}
	}
	if s.ParseFunc != nil {
		items, err := s.ParseFunc(body, s.BaseURL)
		return normalizeNewsItems(items), err
	}
	items, err := parseBodaNewsList(body, s.BaseURL)
	return normalizeNewsItems(items), err
}

func normalizeNewsItems(items []model.NewsItem) []model.NewsItem {
	for i := range items {
		if items[i].PublishedAt == "" {
			items[i].PublishedAt = normalizePublishedAt(items[i].Date)
		}
	}
	return items
}

// GetTotalPages extracts total page count from a list page.
func (s *BodaSite) GetTotalPages(slug string) (int, error) {
	if s.ListPageFunc != nil {
		_, totalPages, err := s.ListPageFunc(slug, 1)
		return totalPages, err
	}
	if s.TotalPagesFunc != nil {
		return s.TotalPagesFunc(slug)
	}

	listURL := s.listPageURL(slug, 1, 0, ".htm")
	body, err := fetch(listURL)
	if err != nil {
		listURL = s.listPageURL(slug, 1, 0, ".html")
		body, err = fetch(listURL)
		if err != nil {
			return 0, err
		}
	}
	return parseTotalPages(body), nil
}

func (s *BodaSite) listPageURL(slug string, page, totalPages int, extension string) string {
	if s.PageURL != nil {
		return s.PageURL(s.BaseURL, slug, page, totalPages, extension)
	}
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", s.BaseURL, slug, extension)
	}
	return fmt.Sprintf("%s/%s/%d%s", s.BaseURL, slug, page, extension)
}

// reverseBodaPageURL maps the logical newest-first page number used by the
// scraper to 博达's generated files. These sites publish the newest page as
// slug.htm, then number older pages downward so the last page is slug/1.htm.
// Returning an empty URL when the source total is unknown prevents silently
// requesting a guessed page number.
func reverseBodaPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	physical := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

// ListNewsRange fetches news from a date range by paginating.
// from/to format: "2026-01-01", "2026-06-14", etc.
func (s *BodaSite) ListNewsRange(slug string, from, to string, maxPages int) ([]model.NewsItem, error) {
	var allItems []model.NewsItem
	seen := make(map[string]bool)

	fromTime := parseDate(from)
	toTime := parseDate(to)

	// Fetch page 1 first
	items, err := s.ListNewsPage(slug, 1)
	if err != nil {
		return nil, err
	}

	// Every registered site gets an implicit homepage feed named index.  It is
	// a preview page, not a reverse-numbered archive; requiring a pagination
	// total here made otherwise healthy index feeds fail with "total page count
	// unavailable" when their adapter intentionally had no homepage total.
	totalPages, totalPagesErr := 1, error(nil)
	if !(strings.Trim(slug, "/") == "index" && s.PageURL != nil && s.ListPageFunc == nil && s.TotalPagesFunc == nil) {
		totalPages, totalPagesErr = s.GetTotalPages(slug)
	}
	usingFallbackPages := totalPagesErr != nil || totalPages == 0

	for _, item := range items {
		t := parseDate(item.Date)
		if !t.IsZero() && (t.Before(fromTime) || !t.Before(toTime.Add(24*time.Hour))) {
			continue
		}
		if !seen[item.URL] {
			seen[item.URL] = true
			allItems = append(allItems, item)
		}
	}

	if totalPages == 0 {
		// A reverse-numbered PageURL cannot safely construct an older physical
		// page without knowing the source total. Returning an error is safer
		// than silently crawling a guessed prefix. A normal URL or a custom
		// ListPageFunc can walk forward until an empty page; use the caller's
		// cap (or a deliberately high safety cap) for that case.
		if s.PageURL != nil && s.ListPageFunc == nil {
			if totalPagesErr != nil {
				return allItems, fmt.Errorf("total page count unavailable for custom page URL: %w", totalPagesErr)
			}
			return allItems, fmt.Errorf("total page count unavailable for custom page URL")
		}
		if maxPages > 0 {
			totalPages = maxPages
		} else {
			totalPages = 1000
		}
	}
	if maxPages > 0 && totalPages > maxPages {
		totalPages = maxPages
	}

	// Check if we need more pages.  An explicitly reported multi-page list must
	// still be walked when page one happens to be empty; otherwise a transient
	// empty first response can hide every later page.
	shouldPaginate := totalPages > 1
	if len(items) > 0 {
		lastDate := parseDate(items[len(items)-1].Date)
		shouldPaginate = lastDate.IsZero() || !lastDate.Before(fromTime)
	}
	if shouldPaginate {
		// Every page-numbered source gets the logical page number here. Reverse
		// generated filenames are mapped by the adapter's PageURL hook, which
		// receives the same logical number and the source total.
		for p := 2; p <= totalPages; p++ {
			realPage := p
			pageItems, err := s.ListNewsPage(slug, realPage)
			if err != nil {
				// An unknown total is only a cap, not evidence that every page
				// was fetched.  An HTTP/parser error therefore must be visible
				// to the caller instead of returning a plausible partial result.
				return allItems, fmt.Errorf("list page %d/%d: %w", p, totalPages, err)
			}
			if len(pageItems) == 0 {
				if !usingFallbackPages && p < totalPages {
					return allItems, fmt.Errorf("list page %d/%d returned no articles", p, totalPages)
				}
				break
			}

			shouldStop := false
			for _, item := range pageItems {
				t := parseDate(item.Date)
				if !t.IsZero() && t.Before(fromTime) {
					shouldStop = true
					break
				}
				if !t.IsZero() && !t.Before(toTime.Add(24*time.Hour)) {
					continue
				}
				if !seen[item.URL] {
					seen[item.URL] = true
					allItems = append(allItems, item)
				}
			}
			if shouldStop {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	return allItems, nil
}

// parseTotalPages extracts the total page count from pagination HTML.
func parseTotalPages(html []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}

	// Pattern 1: explicit counters such as "第1/78页", "共78页", or the
	// older table-based "共1175条 1/59" counter emitted in #fanye cells.
	pageCountRe := regexp.MustCompile(`(?:/|共|第)\s*(\d+)\s*页`)
	pageFractionRe := regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)
	pagination := doc.Find(".p_t, .paging, .page, .p_pages, [id^='fanye'], .fanye")
	for i := 0; i < pagination.Length(); i++ {
		value := pagination.Eq(i).Text()
		compact := strings.Join(strings.Fields(value), "")
		if match := pageCountRe.FindStringSubmatch(compact); match != nil {
			if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
				return total
			}
		}
		if match := pageFractionRe.FindStringSubmatch(value); match != nil {
			if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
				return total
			}
		}
	}

	// Pattern 2: Last page link
	// <span class="p_last p_fun"><a href="1.htm">尾页</a></span>
	lastLink := doc.Find(".p_last a, .p_last").AttrOr("href", "")
	if lastLink == "" {
		// Try text content of p_last
		lastLink = doc.Find(".p_last").Text()
	}
	lastPageHint := trailingPageNumber(lastLink)

	// Pattern 3: Find the highest page number in pagination links
	maxPage := 0
	pagination.Find(".p_no a, .p_no_d, .p_no, a").Each(func(i int, sel *goquery.Selection) {
		href, _ := sel.Attr("href")
		text := strings.TrimSpace(sel.Text())
		// Try to extract page number from href or text. Handle an all-digit
		// value too; the old character loop skipped that common case.
		for _, value := range []string{href, text} {
			if num := trailingPageNumber(value); num > maxPage {
				maxPage = num
			}
		}
	})

	if maxPage > 0 {
		if lastPageHint > maxPage {
			maxPage = lastPageHint
		}
		return maxPage
	}
	if lastPageHint > 0 {
		return lastPageHint
	}

	// Pattern 4: "共X条每页Y条" format, calculate pages
	pageInfo := doc.Find(".paging, .page, .p_pages").Text()
	if strings.Contains(pageInfo, "共") && strings.Contains(pageInfo, "每页") {
		total := 0
		perPage := 0
		fmt.Sscanf(pageInfo, "共%d条每页%d条", &total, &perPage)
		if total > 0 && perPage > 0 {
			return (total + perPage - 1) / perPage
		}
	}

	return 0
}

func trailingPageNumber(value string) int {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "/")
	match := regexp.MustCompile(`(?:^|/)(\d+)(?:\.(?:htm|html))?(?:[?#].*)?$`).FindStringSubmatch(value)
	if match == nil {
		return 0
	}
	n, _ := strconv.Atoi(match[1])
	return n
}

// GetArticle fetches a single article page and parses its content.
func (s *BodaSite) GetArticle(articleURL string) (*model.Article, error) {
	art, _, err := s.GetArticleSnapshot(articleURL)
	return art, err
}

// GetArticleSnapshot fetches and parses an article while retaining the
// original response body. The snapshot is used by archival clients that need
// to preserve both the source page and the normalized article fields.
func (s *BodaSite) GetArticleSnapshot(articleURL string) (*model.Article, []byte, error) {
	if s.ArticleFetchFunc != nil {
		art, body, err := s.ArticleFetchFunc(articleURL)
		if err != nil {
			return nil, nil, err
		}
		if art == nil {
			return nil, nil, fmt.Errorf("article adapter returned nil article")
		}
		normalizeArticlePublishedAt(art)
		art.RawHTML = string(body)
		return art, body, nil
	}

	body, err := fetch(articleURL)
	if err != nil {
		return nil, nil, err
	}
	parse := parseBodaArticle
	if s.ParseArticleFunc != nil {
		parse = s.ParseArticleFunc
	}
	art, err := parse(body, s.BaseURL)
	if err != nil {
		return nil, nil, err
	}
	enrichPublishedAtFromHTML(body, art)
	normalizeArticlePublishedAt(art)
	art.RawHTML = string(body)
	return art, body, nil
}

// navSelectors are CSS selectors for non-content areas to exclude.
var navSelectors = []string{
	"header", "nav", "footer", ".header", ".nav", ".footer",
	".menu", ".top", ".share", ".search", ".crumbs",
	".swiper", ".slider", ".gotop", "script", "style",
	".banner", ".slide", ".carousel", ".focus", ".hero",
	".sidebar", ".side", ".widget", ".login", ".user-info",
}

// cleanTitle removes date prefixes and other noise from titles.
func cleanTitle(title string) string {
	// Collapse newlines and excessive whitespace into single space
	title = strings.Join(strings.Fields(title), " ")
	// Remove date prefixes like "07/16", "2025-08-12", "06-01", "01 2026.06"
	datePrefixes := []string{
		`^\d{2}/\d{2}`,                   // 07/16
		`^\d{4}-\d{2}-\d{2}`,             // 2025-08-12
		`^\d{2}\s+\d{4}\.\d{2}`,          // 01 2026.06
		`^\d{1,2}[./-]\d{1,2}\s*20\d{2}`, // 09.30 2024
		`^\d{1,2}[./-]\d{1,2}20\d{2}`,    // 09.302024
	}
	for _, pattern := range datePrefixes {
		re := regexp.MustCompile(pattern)
		title = re.ReplaceAllString(title, "")
	}
	// Remove trailing date patterns (full and short)
	trailingDates := []string{
		`\s*\d{4}[-/年.]\d{1,2}[-/月.]\d{1,2}[日]?\s*$`, // 2025-08-12
		`\s+\d{1,2}-\d{1,2}\s*$`,                     // 01-08
		`\s+\d{1,2}/\d{1,2}\s*$`,                     // 01/08
	}
	for _, pattern := range trailingDates {
		re := regexp.MustCompile(pattern)
		title = re.ReplaceAllString(title, "")
	}
	// Remove ">>" suffix
	title = strings.TrimSuffix(title, ">>")
	// Clean up whitespace
	title = strings.TrimSpace(title)
	return title
}

// isValidNewsTitle filters out navigation items and too-short titles.
func isValidNewsTitle(title string) bool {
	if len(title) < 6 {
		return false
	}
	skip := []string{"首页", "更多", "查看更多", "了解更多", "查看全部", "Read More", "More", "登录", "注册", "搜索", "English",
		"学院概况", "师资队伍", "人才培养", "党建工作", "学生工作", "招生就业",
		"科学研究", "交流合作", "校友工作", "诚聘英才", "联系我们", "下载专区",
		"学院简介", "现任领导", "组织机构", "教师名录", "本科生", "研究生",
		"通知公告", "学院新闻", "学院动态", "新闻动态", "新闻公告",
		"本科教育", "研究生教育", "实验平台", "系所介绍", "团委工作",
		"查看详情", "详细>>", "详情>>", "更多>>", ">>",
		"院务公开", "联系方式", "学院通知", "唐臣要闻", "基层动态", "学院要闻",
		"诚聘海内外英才", "设计艺术学院", "临床医学",
		"新闻动态\\ NEWS INFORMATION", "新闻动态\\ NEWS",
	}
	// Prefix-based skip patterns
	skipPrefixes := []string{
		"习近平给四所交通大学全体师生",
		"习近平总书记给四所交通大学全体师生",
	}
	for _, s := range skip {
		// These are navigation labels. Matching the whole title avoids
		// discarding legitimate notices such as "本科生转专业...".
		if title == s {
			return false
		}
	}
	for _, p := range skipPrefixes {
		if strings.HasPrefix(title, p) {
			return false
		}
	}
	// Filter out titles that are just section headers
	if strings.HasSuffix(title, ">>") || strings.HasSuffix(title, "》") {
		return false
	}
	return true
}

// isValidNewsURL filters out non-news URLs.
func isValidNewsURL(href string) bool {
	// Skip external domains (use :// prefix for domain boundary matching)
	if strings.Contains(href, "mp.weixin.qq.com") ||
		strings.Contains(href, "://jiuye.swjtu.edu.cn") ||
		strings.Contains(href, "://cas.swjtu.edu.cn") ||
		strings.Contains(href, "://one.swjtu.edu.cn") ||
		strings.Contains(href, "://i.swjtu.edu.cn") {
		return false
	}
	// Skip OA login pages
	if strings.Contains(href, "user/login") ||
		strings.Contains(href, "login.html") ||
		strings.Contains(href, "cas/login") {
		return false
	}
	return true
}

// isValidSiteNewsURL applies the invariant that a shared Boda list parser
// must never turn an external card, login link, or pseudo URL into a local
// article.  Individual adapters may intentionally allow a different host,
// but the generic parser has the site's canonical base URL available and
// should be conservative by default.
func isValidSiteNewsURL(href, baseURL string) bool {
	if !isValidNewsURL(href) {
		return false
	}
	candidate, err := url.Parse(strings.TrimSpace(href))
	if err != nil || candidate.Hostname() == "" {
		return false
	}
	if scheme := strings.ToLower(candidate.Scheme); scheme != "http" && scheme != "https" {
		return false
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Hostname() == "" {
		return false
	}
	return strings.EqualFold(candidate.Hostname(), base.Hostname())
}

// isExternalURL checks if a URL is from a different domain than the site.
func isExternalURL(href, baseURL string) bool {
	if !strings.HasPrefix(href, "http") {
		return false
	}
	// Extract domain from baseURL
	baseDomain := extractDomain(baseURL)
	hrefDomain := extractDomain(href)
	return baseDomain != hrefDomain && hrefDomain != ""
}

func extractDomain(u string) string {
	// Simple domain extraction
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")
	if idx := strings.Index(u, "/"); idx > 0 {
		u = u[:idx]
	}
	// Remove port
	if idx := strings.Index(u, ":"); idx > 0 {
		u = u[:idx]
	}
	return u
}

// cleanURL removes trailing %7D and other artifacts.
func cleanURL(u string) string {
	for {
		switch {
		case strings.HasSuffix(u, "%7D"):
			u = strings.TrimSuffix(u, "%7D")
		case strings.HasSuffix(u, "%7d"):
			u = strings.TrimSuffix(u, "%7d")
		case strings.HasSuffix(u, "}"):
			u = strings.TrimSuffix(u, "}")
		default:
			return u
		}
	}
}

// isInNavArea checks if a selection is inside a navigation/header/footer area.
func isInNavArea(sel *goquery.Selection) bool {
	for _, navSel := range navSelectors {
		if sel.Closest(navSel).Length() > 0 {
			return true
		}
	}
	return false
}

// bodaListType returns the section label published by a list page.  A
// pageType=1 document is a homepage, where pageTitle normally names the site
// rather than the individual card's column, so it must not be copied to every
// homepage item.  Site-specific adapters can still replace this value when a
// page contains several independently labelled modules.
func bodaListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	if pageType == "1" || pageType == "3" {
		return ""
	}
	for _, selector := range []string{
		`meta[name="pageTitle"], meta[name="pagetitle"]`,
		".main_titT a", ".main_titT h3", ".con_tit h3", ".list-title", ".list-head h1", ".list-head h2", ".list-head h3",
	} {
		value := strings.Join(strings.Fields(doc.Find(selector).Last().Text()), " ")
		if strings.HasPrefix(selector, "meta[") {
			value = strings.Join(strings.Fields(doc.Find(selector).Last().AttrOr("content", "")), " ")
		}
		if value != "" && value != "首页" && value != "网站首页" {
			return value
		}
	}
	return ""
}

func bodaRowType(row *goquery.Selection, defaultType string) string {
	if row != nil && row.Length() > 0 {
		for _, selector := range []string{".lanmu", ".category", ".type", ".tag"} {
			value := strings.Join(strings.Fields(row.Find(selector).First().Text()), " ")
			value = strings.Trim(value, "[]【】()（）")
			if value != "" && !strings.Contains(value, "更多") {
				return value
			}
		}
	}
	return strings.TrimSpace(defaultType)
}

func parseBodaNewsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	defaultType := bodaListType(doc)
	// A page with a real, but currently empty, list is a valid result.  A
	// login/static shell with no recognisable list is not: returning nil error
	// for the latter makes a broken tab look successfully crawled.
	recognizedList := doc.Find("li.flex, .list_news, .news_cont, .list_zx, .list_sd2, .xw-ul, .notice, .news-list, .newsList, .lists, .list-item, .news-item, .center-news-top, .top-news-item, div.news-list").Length() > 0

	// Pattern 1: Standard list with <li class="flex">
	// <li class="flex"><span class="time">date</span><a href="url">title</a></li>
	doc.Find("li.flex").Each(func(i int, sel *goquery.Selection) {
		if isInNavArea(sel) {
			return
		}
		date := strings.TrimSpace(sel.Find("span.time").Text())
		a := sel.Find("a[href]")
		if a.Length() == 0 {
			return
		}
		title := cleanTitle(a.Text())
		href, _ := a.Attr("href")
		if title == "" || href == "" || !isValidNewsTitle(title) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, href))
		if seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: normalizePublishedAt(date),
			Type:        bodaRowType(sel, defaultType),
			ParsedAt:    time.Now(),
		})
	})

	// Pattern 2: News sections with specific class names.
	if len(items) == 0 {
		doc.Find(".list_news li, .news_cont li, .list_zx li, .list_sd2 li, .xw-ul li, .notice li, .news-list li, .newsList li, ul.xw-ul li, ul.news-list li, .lists li, .list-item, .news-item, .center-news-top, .top-news-item, .item, div.news-list").Each(func(i int, sel *goquery.Selection) {
			if isInNavArea(sel) {
				return
			}
			// sel might be the <a> tag itself (e.g. .list-item) or a <li> containing <a>
			a := sel
			href, _ := sel.Attr("href")
			if href == "" {
				a = sel.Find("a[href]")
				if a.Length() == 0 {
					return
				}
				href, _ = a.Attr("href")
			}
			// Try title from attribute first, then specific class, then link text
			title := strings.TrimSpace(a.AttrOr("title", ""))
			if title == "" {
				// Clone the selection and remove known non-title elements before extracting text
				titleSel := sel.Find(".xw-ul-tt, .line1, .txts, .a_block, .news-title, h3, h4, .info, p.line2").First()
				if titleSel.Length() > 0 {
					// Remove category labels and other noise
					titleSel.Find(".lanmu, .source, .date, .time").Remove()
					title = strings.TrimSpace(titleSel.Text())
				}
			}
			// Try <p> tag inside <a> (ese.swjtu.edu.cn pattern)
			if title == "" {
				pTag := a.Find("p").First()
				if pTag.Length() > 0 {
					title = strings.TrimSpace(pTag.Text())
				}
			}
			if title == "" {
				title = strings.TrimSpace(a.Text())
				if utf8.RuneCountInString(title) > 100 {
					title = strings.TrimSpace(sel.Find(".xw-ul-tt, .line1, .txts, .a_block").Text())
				}
			}
			// If still empty, try img title/alt attribute
			if title == "" {
				img := a.Find("img").First()
				if img.Length() > 0 {
					title = strings.TrimSpace(img.AttrOr("title", ""))
					if title == "" {
						title = strings.TrimSpace(img.AttrOr("alt", ""))
					}
				}
			}
			// Clean title: remove date prefixes and other noise
			title = cleanTitle(title)
			if title == "" || href == "" || !isValidNewsTitle(title) {
				return
			}
			if utf8.RuneCountInString(title) > 100 || strings.Contains(title, "来源") {
				return
			}
			// Try multiple date selectors
			date := ""
			dateSelectors := []string{".time", ".time span", "span.time", "em", ".date", ".xw-ul-date font", ".xw-ul-date span", ".calendar .date", ".date-label", ".time-box", "time"}
			for _, dateSel := range dateSelectors {
				date = strings.TrimSpace(sel.Find(dateSel).Text())
				if date != "" {
					break
				}
			}
			// Try <time> sibling (sports.swjtu.edu.cn pattern)
			if date == "" {
				parent := sel.Parent()
				if parent.Length() > 0 {
					date = strings.TrimSpace(parent.Find("time").Text())
				}
			}
			// Special handling for ese date format: <div class="s2-date"><b>06-09</b><span>/ 2026</span></div>
			if date == "" {
				s2date := a.Find(".s2-date, .s3-date, .s1-date")
				if s2date.Length() > 0 {
					day := strings.TrimSpace(s2date.Find("b").Text())
					year := strings.TrimSpace(s2date.Find("span").Text())
					year = strings.TrimPrefix(year, "/")
					year = strings.TrimSpace(year)
					if day != "" && year != "" {
						date = year + "-" + day
					}
				}
			}
			// Special handling for ctt date format: <div class="news-time"><div class="day">24</div><div class="month">2025.11</div></div>
			if date == "" {
				day := strings.TrimSpace(sel.Find(".news-time .day, .xw-ul-date span").Text())
				ym := strings.TrimSpace(sel.Find(".news-time .month, .xw-ul-date font").Text())
				if day != "" && ym != "" {
					date = ym + "/" + day
				}
			}
			// Clean up date: remove newlines and extra spaces
			date = strings.Join(strings.Fields(date), " ")
			// Validate date format
			if len(date) > 20 || strings.Count(date, " ") > 3 {
				date = ""
			}
			fullURL := cleanURL(resolveURL(baseURL, href))
			if seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) {
				return
			}
			seen[fullURL] = true
			items = append(items, model.NewsItem{
				Title:       title,
				URL:         fullURL,
				Date:        date,
				PublishedAt: normalizePublishedAt(date),
				Type:        bodaRowType(sel, defaultType),
				ParsedAt:    time.Now(),
			})
		})
	}

	// Pattern 3: Links with <a href="info/xxx/xxxx.htm"> inside content areas only.
	// More restrictive than before - must be in a content section and have reasonable title.
	if len(items) == 0 {
		doc.Find("a[href*='info/']").Each(func(i int, sel *goquery.Selection) {
			if isInNavArea(sel) {
				return
			}
			href, _ := sel.Attr("href")
			title := strings.TrimSpace(sel.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(sel.Text())
			}
			// If still empty, try img title/alt attribute
			if title == "" {
				img := sel.Find("img").First()
				if img.Length() > 0 {
					title = strings.TrimSpace(img.AttrOr("title", ""))
					if title == "" {
						title = strings.TrimSpace(img.AttrOr("alt", ""))
					}
				}
			}
			// Clean title: remove date prefixes and other noise
			title = cleanTitle(title)
			if title == "" || href == "" || !strings.Contains(href, ".htm") {
				return
			}
			if !isValidNewsTitle(title) {
				return
			}
			if utf8.RuneCountInString(title) > 100 || strings.Contains(title, "来源") || strings.Contains(title, "日期") {
				return
			}
			fullURL := cleanURL(resolveURL(baseURL, href))
			if seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) {
				return
			}
			seen[fullURL] = true

			date := ""
			parent := sel.Parent()
			if parent.Length() > 0 {
				date = strings.TrimSpace(parent.Find(".time, span.time, em, .date").Text())
			}
			// Validate date format
			if len(date) > 20 || strings.Count(date, " ") > 3 {
				date = ""
			}

			items = append(items, model.NewsItem{
				Title:       title,
				URL:         fullURL,
				Date:        date,
				PublishedAt: normalizePublishedAt(date),
				Type:        bodaRowType(sel, defaultType),
				ParsedAt:    time.Now(),
			})
		})
	}

	// Pattern 4: Non-博达 CMS sites (e.g. scai.swjtu.edu.cn)
	// Links with page-newsDetail.html?nid=UUID format
	if len(items) == 0 {
		doc.Find("a[href*='newsDetail']").Each(func(i int, sel *goquery.Selection) {
			if isInNavArea(sel) {
				return
			}
			href, _ := sel.Attr("href")
			title := strings.TrimSpace(sel.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(sel.Text())
			}
			if title == "" || href == "" {
				return
			}
			if !isValidNewsTitle(title) || utf8.RuneCountInString(title) > 100 {
				return
			}
			fullURL := cleanURL(resolveURL(baseURL, href))
			if seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) {
				return
			}
			seen[fullURL] = true

			// Try to find date from sibling/parent
			date := ""
			parent := sel.Parent()
			if parent.Length() > 0 {
				date = strings.TrimSpace(parent.Find(".date-label, .date, .calendar .date, span[class*='date']").Text())
				if date == "" {
					// Check for calendar pattern: <p class="day">12</p><p class="date">2026-06</p>
					day := strings.TrimSpace(parent.Find(".calendar .day, .day").Text())
					yearMonth := strings.TrimSpace(parent.Find(".calendar .date, .date").Text())
					if day != "" && yearMonth != "" {
						date = yearMonth + "-" + day
					}
				}
			}
			// Validate date format
			if len(date) > 20 || strings.Count(date, " ") > 3 {
				date = ""
			}

			items = append(items, model.NewsItem{
				Title:       title,
				URL:         fullURL,
				Date:        date,
				PublishedAt: normalizePublishedAt(date),
				Type:        bodaRowType(sel, defaultType),
				ParsedAt:    time.Now(),
			})
		})
	}

	// Pattern 5: design.swjtu.edu.cn style - news in <a> tags with title attr inside content divs
	if len(items) == 0 {
		doc.Find("a[title][href*='.htm']").Each(func(i int, sel *goquery.Selection) {
			if isInNavArea(sel) {
				return
			}
			href, _ := sel.Attr("href")
			title := strings.TrimSpace(sel.AttrOr("title", ""))
			if title == "" || href == "" {
				return
			}
			if !isValidNewsTitle(title) || utf8.RuneCountInString(title) > 100 {
				return
			}
			fullURL := cleanURL(resolveURL(baseURL, href))
			if seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) {
				return
			}
			seen[fullURL] = true

			date := ""
			parent := sel.Parent()
			if parent.Length() > 0 {
				date = strings.TrimSpace(parent.Find(".time, .date, em, span[class*='time']").Text())
			}
			// Validate date format
			if len(date) > 20 || strings.Count(date, " ") > 3 {
				date = ""
			}

			items = append(items, model.NewsItem{
				Title:       title,
				URL:         fullURL,
				Date:        date,
				PublishedAt: normalizePublishedAt(date),
				Type:        bodaRowType(sel, defaultType),
				ParsedAt:    time.Now(),
			})
		})
	}

	if len(items) == 0 && !recognizedList {
		return nil, fmt.Errorf("response contains no recognisable news list")
	}
	return items, nil
}

// extractBodaContentText handles the block shapes used by the shared detail
// templates. Paragraphs are the common case, but older units frequently put
// prose in plain divs, lists, or table cells. Avoid adding a parent list item
// when it contains a more precise child block, otherwise the same text would
// be emitted twice.
func extractBodaContentText(contentSel *goquery.Selection) string {
	if contentSel == nil || contentSel.Length() == 0 {
		return ""
	}
	textSel := contentSel.Clone()
	textSel.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if !bodaLooksLikeAttachmentLink(sel, href) {
			return
		}
		if row := sel.Closest("li"); row.Length() > 0 && strings.Contains(row.Text(), "附件") {
			row.Remove()
			return
		}
		sel.Remove()
	})

	var parts []string
	appendText := func(sel *goquery.Selection) {
		value := strings.Join(strings.Fields(sel.Text()), " ")
		if value != "" {
			parts = append(parts, value)
		}
	}

	textSel.Find("p, h1, h2, h3, h4, li, td, th, div").Each(func(_ int, sel *goquery.Selection) {
		node := sel.Get(0)
		if node == nil {
			return
		}
		switch strings.ToLower(node.Data) {
		case "p", "h1", "h2", "h3", "h4":
			appendText(sel)
		case "li":
			if sel.Find("p, h1, h2, h3, h4, li, td, th").Length() > 0 {
				return
			}
			appendText(sel)
		case "td", "th":
			if sel.Find("p, h1, h2, h3, h4, li, td, th").Length() > 0 {
				return
			}
			appendText(sel)
		case "div":
			if sel.Closest("p, h1, h2, h3, h4, li, td, th").Length() > 0 {
				return
			}
			if sel.Find("p, h1, h2, h3, h4, li, td, th, div").Length() > 0 {
				return
			}
			// A leaf div containing a hyperlink is commonly a related-link or
			// attachment row outside the prose paragraphs. Keep plain text divs,
			// but do not turn those utility links into article body text.
			if sel.Find("a, img").Length() > 0 {
				return
			}
			appendText(sel)
		}
	})

	if len(parts) == 0 {
		// A few otherwise standard templates put plain text directly in the
		// content wrapper. Keep that text rather than reporting a false empty
		// body; image-only pages still correctly remain empty here.
		return strings.Join(strings.Fields(textSel.Text()), " ")
	}
	return strings.Join(parts, "\n\n")
}

// bodaResourceURL resolves a content image only when it is an actual HTTP(S)
// resource. This drops base64 placeholders and javascript/file pseudo-URLs,
// while retaining legitimate images hosted on a CDN or another unit domain.
func bodaResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	for _, prefix := range []string{"data:", "javascript:", "file:", "blob:"} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return ""
	}
	path := strings.ToLower(parsed.Path)
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif"} {
		if strings.HasSuffix(path, placeholder) {
			return ""
		}
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	return resolved
}

var bodaAttachmentExts = []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"}

func bodaIsPseudoResourceHref(href string) bool {
	lower := strings.ToLower(strings.TrimSpace(href))
	for _, prefix := range []string{"#", "data:", "file:", "javascript:", "mailto:", "tel:", "blob:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func bodaLooksLikeAttachmentLink(sel *goquery.Selection, href string) bool {
	if sel == nil || sel.Length() == 0 || href == "" {
		return false
	}
	if bodaIsPseudoResourceHref(href) {
		return false
	}
	if strings.TrimSpace(sel.AttrOr("download", "")) != "" {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(sel.AttrOr("name", "")))
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, ext := range bodaAttachmentExts {
		if strings.HasSuffix(name, ext) || strings.Contains(lower, ext) {
			return true
		}
	}
	// A generic <li> is not enough evidence: ordinary article links are often
	// placed in list items whose surrounding prose happens to mention “附件”.
	// Keep only explicit attachment/download containers here; real file URLs
	// and CMS download endpoints are already covered above.
	for _, selector := range []string{".attachment", ".attachments", ".news-attachment", ".download"} {
		if ancestor := sel.Closest(selector); ancestor.Length() > 0 && strings.Contains(ancestor.Text(), "附件") {
			return true
		}
	}
	return false
}

func parseBodaArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	art := &model.Article{}

	// Remember which element the headline came from: the meta line (date,
	// source) usually sits in its immediate neighborhood, and confining the
	// generic date search to that zone avoids picking up event dates from
	// the article body (时间：2026年9月16日 in a notice, etc.).
	var titleSel *goquery.Selection
	if sel := doc.Find("h1.news_tit").First(); sel.Length() > 0 {
		art.Title = strings.TrimSpace(sel.Text())
		titleSel = sel
	}
	if art.Title == "" {
		if sel := doc.Find(".news-title").First(); sel.Length() > 0 {
			art.Title = strings.TrimSpace(sel.Text())
			titleSel = sel
		}
	}
	if art.Title == "" {
		if sel := doc.Find("h1").First(); sel.Length() > 0 {
			art.Title = strings.TrimSpace(sel.Text())
			titleSel = sel
		}
	}
	// Newer 博达 templates (news.swjtu.edu.cn and many unit sites) put the
	// headline in .art-tit/.cont-tit instead of an h1.
	if art.Title == "" {
		for _, sel := range []string{".art-tit h1", ".art-tit h2", ".art-tit h3", ".cont-tit h1", ".cont-tit h2", ".cont-tit h3", ".article-title", ".arti-title", ".detail-title"} {
			if found := doc.Find(sel).First(); found.Length() > 0 {
				if title := strings.TrimSpace(found.Text()); title != "" {
					art.Title = title
					titleSel = found
					break
				}
			}
		}
	}
	// 博达 pages almost always carry a clean pageTitle meta.
	if art.Title == "" {
		art.Title = strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	}
	if art.Title == "" {
		art.Title = stripSiteSuffix(strings.TrimSpace(doc.Find("title").First().Text()))
	}

	// Source + date
	sourceBlock := doc.Find(".news_source")
	sourceBlock.Find("span").Each(func(i int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())
		if strings.HasPrefix(text, "来源：") || strings.HasPrefix(text, "来源:") {
			art.Source = strings.TrimPrefix(strings.TrimPrefix(text, "来源："), "来源:")
		}
		if strings.HasPrefix(text, "日期：") || strings.HasPrefix(text, "日期:") {
			art.Date = strings.TrimPrefix(strings.TrimPrefix(text, "日期："), "日期:")
		}
		if sel.HasClass("date") {
			art.Date = strings.TrimSpace(sel.Text())
		}
	})

	// Fallback: top-info pattern (civil.swjtu.edu.cn etc.)
	if art.Date == "" {
		topInfo := doc.Find(".top-info")
		if topInfo.Length() > 0 {
			topInfo.Find("span").Each(func(i int, sel *goquery.Selection) {
				text := strings.TrimSpace(sel.Text())
				// Check for date pattern YYYY-MM-DD
				if art.Date == "" && len(text) >= 10 && text[4] == '-' && text[7] == '-' {
					art.Date = text[:10]
				}
				if strings.HasPrefix(text, "来源：") || strings.HasPrefix(text, "来源:") {
					art.Source = strings.TrimPrefix(strings.TrimPrefix(text, "来源："), "来源:")
				}
			})
		}
	}

	// bksy.swjtu.edu.cn uses .tips instead of the older .news_source block.
	if tips := doc.Find(".tips"); tips.Length() > 0 {
		if art.Date == "" {
			dateText := strings.TrimSpace(tips.Find(".left-details").First().Text())
			if match := regexp.MustCompile(`\d{4}[-/.]\d{1,2}[-/.]\d{1,2}`).FindString(dateText); match != "" {
				art.Date = strings.ReplaceAll(strings.ReplaceAll(match, "/", "-"), ".", "-")
			}
		}
		if art.Source == "" {
			source := strings.TrimSpace(tips.Find(".middle2").First().Text())
			source = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(source, "来源："), "来源:"))
			art.Source = source
		}
	}

	// Content body
	contentSel := doc.Find("#vsb_content_501 .v_news_content")
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".v_news_content")
	}
	if contentSel.Length() == 0 {
		contentSel = doc.Find("#vsb_content_501")
	}
	// scai.swjtu.edu.cn and other newer templates use their own wrappers.
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".news-content-wrapper")
	}
	if contentSel.Length() == 0 {
		contentSel = doc.Find("[id^='vsb_content']")
	}
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".content-main")
	}
	// Some templates render the article twice (desktop + mobile copies);
	// keep only the first so text isn't duplicated and zone exclusion works.
	contentSel = contentSel.First()

	// Generic labeled-text fallback: many unit templates render the meta line
	// as plain text (日期：2026/08/21, 信息来源：xxx, 作者：xxx) without any
	// of the containers handled above. Runs after contentSel is known so the
	// date search can be confined to the meta zone around the article.
	fillGenericMeta(doc, art, titleSel, contentSel)
	attachmentURLs := make(map[string]bool, len(art.Attachments))
	for _, attachment := range art.Attachments {
		attachmentURLs[attachment.URL] = true
	}
	if contentSel.Length() > 0 {
		art.Content = extractBodaContentText(contentSel)

		imageSeen := make(map[string]bool)
		contentSel.Find("img").Each(func(i int, sel *goquery.Selection) {
			var src string
			for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
				if value := strings.TrimSpace(sel.AttrOr(attr, "")); value != "" {
					src = value
					break
				}
			}
			resolved := bodaResourceURL(baseURL, src)
			if resolved == "" || imageSeen[resolved] {
				return
			}
			imageSeen[resolved] = true
			art.Images = append(art.Images, resolved)
			sel.SetAttr("src", resolved)
		})

		// Extract attachments
		// Pattern 1: Links with file extensions in href
		// Pattern 2: Links with "download" attribute (博达CMS stores .docx/.xlsx as .png internally)
		attachExts := []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"}
		contentSel.Find("a[href]").Each(func(i int, sel *goquery.Selection) {
			href, _ := sel.Attr("href")
			if href == "" {
				return
			}
			hrefLower := strings.ToLower(href)
			downloadName, hasDownload := sel.Attr("download")
			nameAttr, hasName := sel.Attr("name")
			linkText := strings.TrimSpace(sel.Text())

			// Determine if this is an attachment
			isAttach := false
			attName := ""
			if bodaLooksLikeAttachmentLink(sel, href) {
				isAttach = true
			}

			// Check download attribute first (博达CMS pattern)
			if hasDownload && downloadName != "" {
				isAttach = true
				attName = downloadName
			} else if hasName {
				for _, ext := range attachExts {
					if strings.HasSuffix(strings.ToLower(nameAttr), ext) {
						isAttach = true
						attName = nameAttr
						break
					}
				}
			}

			// Check href for file extensions
			if !isAttach {
				for _, ext := range attachExts {
					if strings.Contains(hrefLower, ext) {
						isAttach = true
						if linkText != "" {
							attName = linkText
						} else {
							attName = href
						}
						break
					}
				}
			}
			if isAttach && attName == "" {
				attName = linkText
				if attName == "" {
					attName = href
				}
			}

			if isAttach && attName != "" {
				resolved := resolveURL(baseURL, href)
				if attachmentURLs[resolved] {
					return
				}
				attachmentURLs[resolved] = true
				art.Attachments = append(art.Attachments, model.Attachment{
					Name: attName,
					URL:  resolved,
				})
				sel.SetAttr("href", resolved)
			}
		})
		if html, htmlErr := contentSel.Html(); htmlErr == nil {
			art.ContentHTML = html
		}
	}

	// Some Visual SiteBuilder templates render the attachment list next to,
	// rather than inside, .v_news_content (bksy.swjtu.edu.cn is one example).
	doc.Find(".left-news-details li a[href]").Each(func(_ int, sel *goquery.Selection) {
		parentText := strings.TrimSpace(sel.Parent().Text())
		if !strings.Contains(parentText, "附件") {
			return
		}
		href, _ := sel.Attr("href")
		name := strings.TrimSpace(sel.Text())
		if href == "" || name == "" {
			return
		}
		resolved := resolveURL(baseURL, href)
		if attachmentURLs[resolved] {
			return
		}
		attachmentURLs[resolved] = true
		art.Attachments = append(art.Attachments, model.Attachment{Name: name, URL: resolved})
	})

	authorBlock := doc.Find(".xxx")
	if authorBlock.Length() > 0 {
		authorText := authorBlock.Text()
		for _, part := range strings.Split(authorText, " ") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "作者：") {
				art.Author = strings.TrimPrefix(part, "作者：")
			}
			if strings.HasPrefix(part, "摄影：") {
				art.Photographer = strings.TrimPrefix(part, "摄影：")
			}
			if strings.HasPrefix(part, "编辑：") {
				art.Editor = strings.TrimPrefix(part, "编辑：")
			}
		}
	}

	doc.Find(".news_sxp li a").Each(func(i int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())
		href, _ := sel.Attr("href")
		if href == "" {
			return
		}
		fullURL := resolveURL(baseURL, href)
		if strings.HasPrefix(text, "上一篇") {
			art.PrevURL = fullURL
			art.PrevTitle = text
		} else if strings.HasPrefix(text, "下一篇") {
			art.NextURL = fullURL
			art.NextTitle = text
		}
	})

	return art, nil
}

// parseBodaArticleWithSectionType keeps the shared body/resource parser while
// filling the section metadata exposed by the many slightly different Boda
// detail templates. A site adapter still owns the entry point; this helper
// only centralizes the conservative DOM fallback for templates that use the
// same breadcrumb conventions.
func parseBodaArticleWithSectionType(raw []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil || article == nil || article.Type != "" {
		return article, err
	}
	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if parseErr != nil {
		return article, nil
	}
	article.Type = bodaArticleSectionType(doc, article.Title)
	return article, nil
}

// bodaArticleSectionType extracts a section label from detail-page layouts.
// It intentionally ignores generic breadcrumb terminals such as 首页/正文 and
// the article headline itself; an absent type is preferable to storing a
// navigation label as the article type.
func bodaArticleSectionType(doc *goquery.Document, articleTitle string) string {
	selectors := []string{
		".list-banner-tt",
		".article_category_bg h2",
		".n_left12138 > h2",
		".inner_right .local > h2",
		".page-content .sidebar > .title",
		".news-list-wrap > .hd > h3",
		".right > .title > h3",
		".banner-title .big-title",
		".main_right .current > h3",
		".navleft-hd p",
		".list-head > p",
		".main_titT a",
		".loaction a",
		".boxRight .top_left span",
		".el-breadcrumb .container",
		".current > h3",
	}
	for _, selector := range selectors {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(selection.Eq(i).Text())
			if selector == ".main_titT a" || selector == ".loaction a" || selector == ".boxRight .top_left span" || selector == ".el-breadcrumb .container" {
				parts := strings.Split(candidate, ">")
				for j := len(parts) - 1; j >= 0; j-- {
					if value := bodaTypeCandidate(parts[j], articleTitle); value != "" {
						return value
					}
				}
				continue
			}
			if value := bodaTypeCandidate(candidate, articleTitle); value != "" {
				return value
			}
		}
	}
	return ""
}

func bodaTypeCandidate(value, articleTitle string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = strings.Trim(value, " >|\t\r\n")
	if value == "" || value == strings.Join(strings.Fields(articleTitle), " ") {
		return ""
	}
	switch value {
	case "首页", "正文", "文章详情", "新闻中心", "新闻资讯", "网站首页", "LINK":
		return ""
	}
	if strings.Contains(value, "当前位置") || strings.Contains(value, "点击数") || len([]rune(value)) > 40 {
		return ""
	}
	return value
}

var (
	labelDateRe         = regexp.MustCompile(`(?:^|[\s\x{a0}：:，。；;、|])(?:日期|发布时间|发布日期|发表日期|发表时间|发布于|时间)[\s\x{a0}]*[:：][\s\x{a0}]*(20\d{2})[\s\x{a0}]*[-/.年][\s\x{a0}]*(\d{1,2})[\s\x{a0}]*[-/.月][\s\x{a0}]*(\d{1,2})`)
	javaDateRe          = regexp.MustCompile(`(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{2} \d{2}:\d{2}:\d{2} CST \d{4}`)
	dateTimeRe          = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*(?:日|号)?[ Tt]+(\d{1,2}):(\d{2})(?::(\d{2}))?`)
	bareDateRe          = regexp.MustCompile(`(20\d{2})[\s\x{a0}]*[-/.年][\s\x{a0}]*(\d{1,2})[\s\x{a0}]*[-/.月][\s\x{a0}]*(\d{1,2})`)
	labelSourceRe       = regexp.MustCompile(`(?:信息来源|来源)\s*[:：][\s\x{a0}]*([^\s\x{a0}，。；;:：<>（）()]{2,40})`)
	labelAuthorRe       = regexp.MustCompile(`作者\s*[:：][\s\x{a0}]*([^\s\x{a0}，。；;:：<>（）()]{2,30})`)
	labelEditorRe       = regexp.MustCompile(`(?:编辑|责编)\s*[:：][\s\x{a0}]*([^\s\x{a0}，。；;:：<>（）()]{2,30})`)
	labelPhotographerRe = regexp.MustCompile(`摄影\s*[:：][\s\x{a0}]*([^\s\x{a0}，。；;:：<>（）()]{2,30})`)
	labelTypeRe         = regexp.MustCompile(`(?:文章类型|信息类型|类型|类别|分类)\s*[:：]\s*([^\s\x{a0}，。；;:：<>（）()]{2,40})`)
	siteSuffixRe        = regexp.MustCompile(`[-–—_][^-–—_]{0,60}$`)
	siteWordsRe         = regexp.MustCompile(`大学|学院|研究院|新闻网|党委|宣传|财务|审计|工会|处|部|中心|图书馆|档案馆|医院|集团|实验室|管委会|办公室|期刊社`)
)

// labelWords are tokens that appear as field labels on article pages. When a
// captured value is one of these, the label was empty and the regex consumed
// the next label instead (e.g. "来源：作者：张三" in flattened text).
var labelWords = map[string]bool{
	"来源": true, "信息来源": true, "作者": true, "编辑": true, "责编": true,
	"摄影": true, "日期": true, "时间": true, "审核": true, "点击": true,
	"点击量": true, "浏览": true, "阅读量": true, "发布": true, "发布日期": true, "发布时间": true,
	"发表时间": true, "发布者": true, "供稿": true, "撰稿": true, "文字": true, "图片": true,
}

var labelValueNoise = regexp.MustCompile(`(20\d{2}年.*|https?$|^\d)`)

var labelCutRe = regexp.MustCompile(`(日期|时间|来源|编辑|责编|审核|点击|浏览|摄影)`)

func cleanLabelValue(value string) string {
	value = strings.TrimSpace(value)
	if cut := labelCutRe.FindStringIndex(value); cut != nil {
		value = value[:cut[0]]
	}
	if labelWords[value] || labelValueNoise.MatchString(value) {
		return ""
	}
	return value
}

// normalizePublishedAt retains time-of-day precision while keeping the
// legacy Article.Date field date-only. An empty result means the source did
// not contain a complete calendar date.
func normalizePublishedAt(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02 15:04",
		"2006/01/02 15:04:05", "2006/01/02 15:04",
		"2006.01.02 15:04:05", "2006.01.02 15:04",
		"2006年1月2日 15时04分05秒", "2006年1月2日 15时04分",
		"2006年1月2日 15:04:05", "2006年1月2日 15:04",
		"Mon Jan 02 15:04:05 MST 2006",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02 15:04:05")
		}
	}
	if match := dateTimeRe.FindStringSubmatch(value); match != nil {
		second := match[6]
		if second == "" {
			second = "00"
		}
		return fmt.Sprintf("%s-%02s-%02s %02s:%s:%s", match[1], match[2], match[3], match[4], match[5], second)
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	return ""
}

func normalizeArticlePublishedAt(article *model.Article) {
	if article == nil {
		return
	}
	if article.PublishedAt == "" {
		article.PublishedAt = normalizePublishedAt(article.Date)
	} else {
		article.PublishedAt = normalizePublishedAt(article.PublishedAt)
	}
	if article.Date != "" {
		if date := normalizePublishedAt(article.Date); len(date) >= 10 {
			article.Date = date[:10]
		}
	}
	if article.Date == "" && len(article.PublishedAt) >= 10 {
		article.Date = article.PublishedAt[:10]
	}
}

func enrichPublishedAtFromHTML(body []byte, article *model.Article) {
	if article == nil || publishedAtHasClock(article.PublishedAt) {
		return
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return
	}
	var titleSel, contentSel *goquery.Selection
	for _, selector := range []string{"h1.news_tit", ".news-title", "h1", ".art-tit h1", ".cont-tit h1", ".article-title", ".detail-title"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			titleSel = found
			break
		}
	}
	for _, selector := range []string{"#vsb_content_501 .v_news_content", ".v_news_content", ".news-content-wrapper", ".content-main", "[id^='vsb_content']"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			contentSel = found
			break
		}
	}
	if value := findDateTimeInMetaZone(titleSel, contentSel); value != "" {
		if precise := normalizePublishedAt(value); publishedAtHasClock(precise) {
			article.PublishedAt = precise
			return
		}
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	if value := javaDateRe.FindString(clone.Text()); value != "" {
		if parsed, err := time.Parse("Mon Jan 02 15:04:05 MST 2006", value); err == nil {
			article.PublishedAt = parsed.Format("2006-01-02 15:04:05")
		}
	}
}

func publishedAtHasClock(value string) bool {
	return len(strings.TrimSpace(value)) > len("2006-01-02") && strings.Contains(value, ":")
}

// stripSiteSuffix removes a trailing "- 西南交通大学xx学院" style suffix from
// a <title> tag while leaving dashes inside the headline itself untouched.
func stripSiteSuffix(title string) string {
	suffix := siteSuffixRe.FindString(title)
	if suffix != "" && siteWordsRe.MatchString(suffix) {
		return strings.TrimSpace(strings.TrimSuffix(title, suffix))
	}
	return title
}

func normalizeDateParts(year, month, day string) string {
	m, errM := strconv.Atoi(month)
	d, errD := strconv.Atoi(day)
	if errM != nil || errD != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return ""
	}
	return fmt.Sprintf("%s-%02d-%02d", year, m, d)
}

// fillGenericMeta extracts date/source/author fields using the Chinese
// labels (日期：, 信息来源：, 作者：…) that most unit templates render
// without any stable CSS container. Fields that were already filled by the
// site-specific selectors above are kept.
//
// The date search is deliberately confined to the "meta zone" — the
// neighborhood of the headline and the content container — because article
// bodies are full of event dates (时间：2026年9月16日 in a notice) that must
// never be mistaken for the publish date.
func fillGenericMeta(doc *goquery.Document, art *model.Article, titleSel, contentSel *goquery.Selection) {
	text := ""
	visibleText := func() string {
		if text == "" {
			clone := doc.Clone()
			clone.Find("script, style, noscript").Remove()
			text = clone.Text()
		}
		return text
	}
	if art.Date == "" {
		art.Date = findDateInMetaZone(titleSel, contentSel)
	}
	if art.PublishedAt == "" {
		if value := findDateTimeInMetaZone(titleSel, contentSel); value != "" {
			art.PublishedAt = normalizePublishedAt(value)
		}
	}
	if art.Date == "" {
		for _, sel := range []string{`meta[name="publishdate"]`, `meta[name="publish_date"]`, `meta[name="PubDate"]`, `meta[property="article:published_time"]`} {
			content := strings.TrimSpace(doc.Find(sel).AttrOr("content", ""))
			if m := bareDateRe.FindStringSubmatch(content); m != nil {
				if date := normalizeDateParts(m[1], m[2], m[3]); date != "" {
					art.Date = date
					break
				}
			}
		}
	}
	// scai.swjtu.edu.cn renders "发布日期：Tue Jul 07 00:00:00 CST 2026";
	// this format is distinctive enough to match against the whole page.
	if art.Date == "" {
		if m := javaDateRe.FindString(visibleText()); m != "" {
			if parsed, err := time.Parse("Mon Jan 02 15:04:05 MST 2006", m); err == nil {
				art.Date = parsed.Format("2006-01-02")
				art.PublishedAt = parsed.Format("2006-01-02 15:04:05")
			}
		}
	}
	if art.Source == "" {
		if m := labelSourceRe.FindStringSubmatch(visibleText()); m != nil {
			art.Source = cleanLabelValue(m[1])
		}
	}
	if art.Author == "" {
		if m := labelAuthorRe.FindStringSubmatch(visibleText()); m != nil {
			art.Author = cleanLabelValue(m[1])
		}
	}
	if art.Editor == "" {
		if m := labelEditorRe.FindStringSubmatch(visibleText()); m != nil {
			art.Editor = cleanLabelValue(m[1])
		}
	}
	if art.Photographer == "" {
		if m := labelPhotographerRe.FindStringSubmatch(visibleText()); m != nil {
			art.Photographer = cleanLabelValue(m[1])
		}
	}
	if art.Type == "" {
		if m := labelTypeRe.FindStringSubmatch(visibleText()); m != nil {
			art.Type = cleanLabelValue(m[1])
		}
	}
	normalizeArticlePublishedAt(art)
}

var (
	scriptBlockRe   = regexp.MustCompile(`(?is)<script[\s\S]*?</script>|<style[\s\S]*?</style>|<noscript[\s\S]*?</noscript>`)
	tagStripRe      = regexp.MustCompile(`<[^>]*>`)
	spaceCollapseRe = regexp.MustCompile(`[\s\x{a0}]+`)
)

// flattenedText renders a selection as plain text with a space wherever a
// tag boundary was, so label regexes can tell "时间：" (a label) apart from
// "截止时间：" (a word inside a sentence). Selection.Text() glues sibling
// elements together and loses that distinction.
func flattenedText(sel *goquery.Selection) string {
	inner, err := sel.Html()
	if err != nil {
		return sel.Text()
	}
	text := scriptBlockRe.ReplaceAllString(inner, " ")
	text = tagStripRe.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	return spaceCollapseRe.ReplaceAllString(text, " ")
}

// findDateInMetaZone looks for a publish date in the ancestors of the
// headline and of the content container (with the article body itself
// excluded), so dates mentioned inside the article text are never picked.
func findDateInMetaZone(titleSel, contentSel *goquery.Selection) string {
	zones := make([]string, 0, 8)
	contentText := ""
	collect := func(sel *goquery.Selection, exclude string) {
		count := 0
		sel.Parents().EachWithBreak(func(_ int, parent *goquery.Selection) bool {
			if count >= 4 {
				return false
			}
			count++
			text := flattenedText(parent)
			if exclude != "" {
				text = strings.Replace(text, exclude, "", 1)
			}
			zones = append(zones, text)
			return true
		})
	}
	if contentSel != nil && contentSel.Length() > 0 {
		contentText = flattenedText(contentSel)
		collect(contentSel, contentText)
	}
	if titleSel != nil && titleSel.Length() > 0 {
		collect(titleSel, "")
	}
	// Labeled dates first: they are the strongest signal even in wider zones.
	for _, zone := range zones {
		if m := labelDateRe.FindStringSubmatch(zone); m != nil {
			if date := normalizeDateParts(m[1], m[2], m[3]); date != "" {
				return date
			}
		}
	}
	// Then bare dates, but only in the tightest zones (the meta line sits
	// right next to the headline; wider zones contain related-news dates).
	for _, zone := range zones[:min(2, len(zones))] {
		if m := bareDateRe.FindStringSubmatch(zone); m != nil {
			if date := normalizeDateParts(m[1], m[2], m[3]); date != "" {
				return date
			}
		}
	}
	return ""
}

// findDateTimeInMetaZone is the precision-preserving counterpart to
// findDateInMetaZone. It searches the same narrow headline/meta ancestors so
// times mentioned in the article body cannot be mistaken for publication
// times.
func findDateTimeInMetaZone(titleSel, contentSel *goquery.Selection) string {
	zones := make([]string, 0, 8)
	contentText := ""
	collect := func(sel *goquery.Selection, exclude string) {
		count := 0
		sel.Parents().EachWithBreak(func(_ int, parent *goquery.Selection) bool {
			if count >= 4 {
				return false
			}
			count++
			text := flattenedText(parent)
			if exclude != "" {
				text = strings.Replace(text, exclude, "", 1)
			}
			zones = append(zones, text)
			return true
		})
	}
	if contentSel != nil && contentSel.Length() > 0 {
		contentText = flattenedText(contentSel)
		collect(contentSel, contentText)
	}
	if titleSel != nil && titleSel.Length() > 0 {
		collect(titleSel, "")
	}
	for _, zone := range zones {
		if match := dateTimeRe.FindString(zone); match != "" {
			return match
		}
		if match := javaDateRe.FindString(zone); match != "" {
			return match
		}
	}
	return ""
}

func resolveURL(base, href string) string {
	href = cleanURL(href)
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	u, err := url.Parse(base)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return u.ResolveReference(ref).String()
}
