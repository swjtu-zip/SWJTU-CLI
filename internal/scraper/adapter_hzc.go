package scraper

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const hzcCanonicalBaseURL = "https://hzc.swjtu.edu.cn"

// hzcSubPages contains the local article lists confirmed from the homepage,
// navigation and the hidden news tabs.  xwdt/dw is deliberately absent: its
// rows point to news.swjtu.edu.cn and therefore are external-only.  The
// parent pages for 部门概况、对外合作、继续教育、对口支援 and 定点帮扶, along
// with 继续教育's description pages and 定点帮扶's policy page, are
// landing/static pages and are intentionally not feeds.
var hzcSubPages = []string{
	"xwdt/zx",
	"xwdt/zh",
	"tzgs",
	"xzzq/xzzq",
	"dwhz/hzqk",
	"dwhz/zdwj",
	"jxjy/xxgs",
	"jxjy/zdwj",
	"ddbf/rwfc",
	"zbjs/djyd",
}

var (
	hzcArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.(?:html?|shtml)$`)
	hzcDateTimeRE    = regexp.MustCompile(`20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?\s*(?:[Tt]|\s)+\s*[0-9]{1,2}\s*[:：]\s*[0-9]{2}(?:\s*[:：]\s*[0-9]{2})?`)
	hzcDateRE        = regexp.MustCompile(`(20[0-9]{2})\s*(?:年|[-/.])\s*([0-9]{1,2})\s*(?:月|[-/.])\s*([0-9]{1,2})\s*(?:日|号)?`)
	hzcAttachmentRE  = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
)

const hzcMaxResourceBytes = 32 << 20

func init() {
	if site := AllSites["hzc"]; site != nil {
		site.BaseURL = hzcCanonicalBaseURL
	}
	registerAdapter("hzc", SiteAdapter{
		SubPages:     hzcSubPages,
		ParseList:    parseAdapter_hzcList,
		ParseArticle: parseAdapter_hzcArticle,
		PageURL:      hzcPageURL,
		ListPage:     hzcListPage,
		FetchArticle: hzcFetchArticle,
	})
}

// hzcPageURL maps logical pages (1 = newest) to hzc's reverse-numbered
// generated files.  For a 38-page list, logical page 2 is /37.htm and the
// oldest logical page is /1.htm.
func hzcPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		return ""
	}
	if slug == "" || slug == "index" || slug == "home" {
		if page != 1 {
			return ""
		}
		return baseURL + "/index" + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	filePage := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func hzcKnownFeed(slug string) bool {
	for _, known := range hzcSubPages {
		if known == strings.Trim(strings.TrimSpace(slug), "/") {
			return true
		}
	}
	return false
}

// hzcFetchListHTML fetches a generated list page, tolerating the .html
// spelling the CMS occasionally emits for archived pages.
func hzcFetchListHTML(pageURL string) ([]byte, error) {
	body, err := fetch(pageURL)
	if err == nil {
		return body, nil
	}
	if strings.HasSuffix(strings.ToLower(pageURL), ".htm") {
		if body, fallbackErr := fetch(strings.TrimSuffix(pageURL, ".htm") + ".html"); fallbackErr == nil {
			return body, nil
		}
	}
	return nil, err
}

// hzcListPage owns the GET-and-parse path for hzc article lists. hzc columns
// syndicate news.swjtu.edu.cn rows alongside local /info/ articles, and a
// physical page can consist of external rows only: the live xwdt/zx archive
// has such pages in the middle of its 39-page run, with local articles both
// before and after them. Feeding the shared walker those empty pages would
// abort the crawl with "returned no articles" (or, with a break-on-empty
// rule, stop it even earlier) and silently drop every older local article.
// The walker therefore compacts logical page numbers onto physical pages
// that actually contain local articles and skips the external-only ones. It
// reports total 0 (not exposed) so the shared range walker applies its
// empty-page-break cap and stops cleanly once the physical archive is
// exhausted.
func hzcListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("hzc: list page must be positive: %d", page)
	}
	site := AllSites["hzc"]
	if site == nil {
		return nil, 0, fmt.Errorf("hzc: site registration is missing")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	if baseURL == "" {
		baseURL = hzcCanonicalBaseURL
	}
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" || slug == "home" {
		if page != 1 {
			return nil, 1, fmt.Errorf("hzc: homepage has no page %d", page)
		}
		body, err := hzcFetchListHTML(baseURL + "/index.htm")
		if err != nil {
			return nil, 1, fmt.Errorf("hzc: read homepage: %w", err)
		}
		items, err := parseAdapter_hzcList(body, baseURL)
		if err != nil {
			return nil, 1, err
		}
		return items, 1, nil
	}
	if !hzcKnownFeed(slug) {
		return nil, 0, fmt.Errorf("hzc: %q is not an article-list route", slug)
	}
	scan, err := hzcEnsureListScan(baseURL, slug)
	if err != nil {
		return nil, 0, err
	}
	for len(scan.nonEmpty) < page && !scan.done {
		if err := hzcScanNextListPosition(baseURL, slug, scan); err != nil {
			return nil, 0, err
		}
	}
	if page > len(scan.nonEmpty) {
		return []model.NewsItem{}, 0, nil
	}
	return scan.pages[scan.nonEmpty[page-1]], 0, nil
}

// hzcListScan caches one walk over a slug's reverse-numbered physical pages
// so a range crawl fetches each page at most once. Scans expire with the
// shared fetch-cache TTL so long-lived processes pick up newly published
// articles instead of serving a stale first page forever.
type hzcListScan struct {
	fetchedAt time.Time
	total     int                      // physical page count reported by position-1 pagination
	pages     map[int][]model.NewsItem // parsed local items keyed by physical position (1 = newest)
	nonEmpty  []int                    // physical positions holding at least one local article
	scanned   int                      // leading physical positions already scanned
	done      bool                     // scanned through the last physical page
}

var hzcListWalks = struct {
	sync.Mutex
	scans map[string]*hzcListScan
}{scans: make(map[string]*hzcListScan)}

func hzcEnsureListScan(baseURL, slug string) (*hzcListScan, error) {
	key := baseURL + "|" + slug
	hzcListWalks.Lock()
	defer hzcListWalks.Unlock()
	if scan, ok := hzcListWalks.scans[key]; ok && time.Since(scan.fetchedAt) < cacheTTL {
		return scan, nil
	}
	scan := &hzcListScan{total: 1, pages: make(map[int][]model.NewsItem)}
	body, err := hzcFetchListHTML(hzcPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return nil, fmt.Errorf("hzc: read first page for %s: %w", slug, err)
	}
	items, err := parseAdapter_hzcList(body, baseURL)
	if err != nil {
		return nil, err
	}
	if doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(body))); docErr == nil {
		if total := hzcPaginationTotal(doc); total > 0 {
			scan.total = total
		}
	}
	scan.fetchedAt = time.Now()
	scan.scanned = 1
	scan.pages[1] = items
	if len(items) > 0 {
		scan.nonEmpty = append(scan.nonEmpty, 1)
	}
	hzcListWalks.scans[key] = scan
	return scan, nil
}

func hzcScanNextListPosition(baseURL, slug string, scan *hzcListScan) error {
	if scan.scanned >= scan.total {
		scan.done = true
		return nil
	}
	pos := scan.scanned + 1
	pageURL := hzcPageURL(baseURL, slug, pos, scan.total, ".htm")
	if pageURL == "" {
		return fmt.Errorf("hzc: no list URL for %s physical page %d/%d", slug, pos, scan.total)
	}
	body, err := hzcFetchListHTML(pageURL)
	if err != nil {
		return fmt.Errorf("hzc: read %s physical page %d/%d: %w", slug, pos, scan.total, err)
	}
	items, err := parseAdapter_hzcList(body, baseURL)
	if err != nil {
		return err
	}
	if doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(body))); docErr == nil {
		if middleTotal := hzcPaginationTotal(doc); middleTotal > 1 && middleTotal != scan.total {
			return fmt.Errorf("hzc: %s physical page %d reported total %d, want %d", slug, pos, middleTotal, scan.total)
		}
	}
	scan.scanned = pos
	scan.pages[pos] = items
	if len(items) > 0 {
		scan.nonEmpty = append(scan.nonEmpty, pos)
	}
	return nil
}

func parseAdapter_hzcList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("hzc: parse list HTML: %w", err)
	}
	if err := hzcRejectErrorPage(doc, "list"); err != nil {
		return nil, err
	}
	if hzcIsHomepage(doc) {
		items := hzcHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("hzc: homepage article modules contained no local articles")
		}
		return items, nil
	}
	if hzcPageType(doc) != "2" {
		return nil, fmt.Errorf("hzc: response is not a list page")
	}
	list := doc.Find(".conlistComm").First()
	if list.Length() == 0 {
		return nil, fmt.Errorf("hzc: response is a static page, not an article list")
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	defaultType := hzcCurrentType(doc)
	list.Find("dl").Each(func(_ int, row *goquery.Selection) {
		hzcAppendRow(&items, seen, row, defaultType, baseURL)
	})
	// A real .conlistComm without rows is a legal empty list.  Do not turn it
	// into an error or accidentally scan unrelated navigation links.
	return items, nil
}

func hzcHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	foundModule := false
	doc.Find(".box").Each(func(_ int, box *goquery.Selection) {
		title := strings.Join(strings.Fields(box.Find(".box-header .box-title, .box-header h2, .box-header h3").First().Text()), " ")
		typ := ""
		switch {
		case strings.Contains(title, "新闻动态"):
			typ = "新闻动态"
		case strings.Contains(title, "通知公告"):
			typ = "通知公告"
		case strings.Contains(title, "下载专区"):
			typ = "下载专区"
		}
		if typ == "" {
			return
		}
		foundModule = true
		box.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			href := anchor.AttrOr("href", "")
			row := anchor.Closest("li")
			if row.Length() == 0 {
				row = anchor.Parent()
			}
			dateText := row.Text()
			if _, publishedAt := hzcDateValues(dateText); publishedAt == "" {
				dateText = anchor.Parent().Text()
			}
			titleText := strings.TrimSpace(anchor.AttrOr("title", ""))
			if titleText == "" {
				titleText = strings.TrimSpace(anchor.Text())
			}
			hzcAppendItem(&items, seen, href, titleText, dateText, typ, baseURL)
		})
	})
	if !foundModule {
		return nil
	}
	return items
}

func hzcAppendRow(items *[]model.NewsItem, seen map[string]bool, row *goquery.Selection, defaultType, baseURL string) {
	anchor := row.Find("dt > a[href*='info/'], dt a[href*='info/']").First()
	if anchor.Length() == 0 {
		return
	}
	typ := hzcRowType(row)
	if typ == "" {
		typ = defaultType
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	hzcAppendItem(items, seen, anchor.AttrOr("href", ""), title, row.Find("dd").Text(), typ, baseURL)
}

func hzcAppendItem(items *[]model.NewsItem, seen map[string]bool, href, title, dateText, typ, baseURL string) {
	fullURL, ok := hzcArticleURL(baseURL, href)
	if !ok || seen[fullURL] {
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		// The caller can only reach this branch for a row whose title attribute
		// is absent; it is filled by the caller's anchor text when applicable.
		return
	}
	title = cleanTitle(title)
	if !hzcValidTitle(title) {
		return
	}
	date, publishedAt := hzcDateValues(dateText)
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        strings.TrimSpace(typ),
		ParsedAt:    time.Now(),
	})
}

// hzcValidTitle is deliberately narrower than the shared navigation-title
// heuristic.  The URL is already constrained to an /info/ article and this
// site has legitimate notices whose titles end in a Chinese closing quote
// (for example a title ending in “》”), which the generic heuristic treats as
// a section heading.
func hzcValidTitle(title string) bool {
	if len([]rune(strings.TrimSpace(title))) < 2 {
		return false
	}
	switch strings.TrimSpace(title) {
	case "首页", "更多", "更多>>", "通知公告", "新闻动态", "下载专区":
		return false
	default:
		return true
	}
}

func hzcRowType(row *goquery.Selection) string {
	text := strings.TrimSpace(row.Find("dt > span a, dt span a").First().Text())
	text = strings.TrimSpace(strings.Trim(text, "[]"))
	return text
}

func hzcCurrentType(doc *goquery.Document) string {
	if current := strings.Join(strings.Fields(doc.Find(".current h3").First().Text()), " "); current != "" {
		return current
	}
	if current := strings.TrimSpace(doc.Find(".menu .submenu_cur a").First().AttrOr("title", "")); current != "" {
		return current
	}
	return strings.Join(strings.Fields(doc.Find(".menu h3").First().Text()), " ")
}

func hzcDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "", ""
	}
	if match := hzcDateTimeRE.FindString(value); match != "" {
		publishedAt := normalizePublishedAt(match)
		if len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	if match := hzcDateRE.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func hzcPaginationTotal(doc *goquery.Document) int {
	total := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		page, err := strconv.Atoi(strings.TrimSpace(sel.Text()))
		if err == nil && page > total {
			total = page
		}
	})
	if total < 1 && doc.Find(".p_pages").Length() > 0 {
		return 1
	}
	return total
}

func hzcArticleURL(baseURL, href string) (string, bool) {
	if strings.TrimSpace(href) == "" {
		return "", false
	}
	resolved := resolveURL(baseURL, href)
	u, err := url.Parse(resolved)
	if err != nil || u.Host == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Host, base.Host) {
		return "", false
	}
	if !hzcArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	return u.String(), true
}

func hzcIsHomepage(doc *goquery.Document) bool {
	return hzcPageType(doc) == "1" && doc.Find(".info-panel, #carousel-example-generic").Length() > 0
}

func hzcPageType(doc *goquery.Document) string {
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).First().AttrOr("content", ""))
}

func hzcRejectErrorPage(doc *goquery.Document, kind string) error {
	if doc == nil {
		return fmt.Errorf("hzc: nil %s response", kind)
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	text := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
	title := strings.ToLower(strings.TrimSpace(doc.Find("title").First().Text()))
	for _, marker := range []string{"access denied", "cloudflare", "just a moment", "请输入验证码下载附件", "应用维护中", "安全验证"} {
		if strings.Contains(text, marker) || strings.Contains(title, marker) {
			return fmt.Errorf("hzc: %s response is an error/captcha page", kind)
		}
	}
	return nil
}

func parseAdapter_hzcArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("hzc: parse article HTML: %w", err)
	}
	if err := hzcRejectErrorPage(doc, "article"); err != nil {
		return nil, err
	}
	if hzcPageType(doc) != "3" {
		return nil, fmt.Errorf("hzc: response is not an article page")
	}
	articleShell := doc.Find(".article").First()
	if articleShell.Length() == 0 {
		return nil, fmt.Errorf("hzc: article shell is missing")
	}
	titleSel := articleShell.ChildrenFiltered("h3").First()
	if titleSel.Length() == 0 {
		titleSel = articleShell.Find("h3").First()
	}
	if titleSel.Length() == 0 || strings.TrimSpace(titleSel.Text()) == "" {
		return nil, fmt.Errorf("hzc: article title is missing")
	}
	if articleShell.Find(".v_news_content").First().Length() == 0 {
		return nil, fmt.Errorf("hzc: article content shell is missing")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, fmt.Errorf("hzc: parse article: %w", err)
	}
	article.Title = strings.TrimSpace(titleSel.Text())
	// The shared parser has deliberately broad fallbacks for legacy Boda
	// pages.  hzc has a fixed metadata line, so clear the fallback first: a
	// notice's body may contain an event/deadline date which is not its
	// publication date.
	article.Date = ""
	article.PublishedAt = ""
	if date, publishedAt := hzcDateValues(articleShell.Find(".wz_sx").First().Text()); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	article.Type = hzcCurrentType(doc)
	contentSel := articleShell.Find(".v_news_content").First()
	article.Content = extractBodaContentText(contentSel)
	article.Images = hzcContentImages(contentSel, baseURL)
	article.ContentHTML = hzcContentHTML(contentSel, baseURL)
	article.Attachments = nil
	hzcAppendArticleAttachments(article, articleShell, baseURL)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("hzc: article has no body or resource")
	}
	article.RawHTML = string(raw)
	return article, nil
}

func hzcAppendArticleAttachments(article *model.Article, shell *goquery.Selection, baseURL string) {
	if article == nil || shell == nil || shell.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	shell.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" {
			return
		}
		if !hzcAttachmentLink(anchor, href, baseURL) {
			return
		}
		linkText := strings.Join(strings.Fields(anchor.Text()), " ")
		parentText := strings.Join(strings.Fields(anchor.Parent().Text()), " ")
		lowerHref := strings.ToLower(href)
		isDownloadEndpoint := strings.Contains(lowerHref, "download.jsp") || strings.Contains(lowerHref, "downloadattachurl")
		if isDownloadEndpoint && !strings.Contains(linkText, "附件") && !strings.Contains(parentText, "附件") {
			return
		}
		resolved := hzcAttachmentURL(baseURL, href)
		if resolved == "" {
			return
		}
		if seen[resolved] {
			return
		}
		name := strings.TrimSpace(anchor.AttrOr("download", ""))
		if name == "" {
			name = linkText
		}
		name = strings.TrimSpace(strings.Trim(name, "【】[]"))
		if name == "" {
			name = resolved
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func hzcFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := hzcCanonicalBaseURL
	if site := AllSites["hzc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	return hzcFetchArticleAt(articleURL, baseURL)
}

func hzcFetchArticleAt(articleURL, baseURL string) (*model.Article, []byte, error) {
	fullURL, ok := hzcArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("hzc: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := hzcFetchHTML(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("hzc: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_hzcArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("hzc: parse article %s: %w", fullURL, err)
	}

	candidateImages := append([]string(nil), article.Images...)
	candidateAttachments := append([]model.Attachment(nil), article.Attachments...)
	article.Images = hzcVerifyImages(candidateImages, baseURL, fullURL)
	article.Attachments = hzcVerifyAttachments(candidateAttachments, baseURL, fullURL)
	article.ContentHTML = hzcSanitizeContentHTML(article.ContentHTML, baseURL, fullURL, article.Images, candidateAttachments, article.Attachments)
	article.Content = hzcContentTextFromHTML(article.ContentHTML)
	return article, body, nil
}

func hzcFetchHTML(rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || !hzcHTTPURL(parsed) {
		return nil, fmt.Errorf("refusing non-HTTP article URL %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create article request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	client := hzcHTTPClient()
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if !hzcHTTPURL(next.URL) || (len(via) > 0 && !hzcURLAuthorityEqual(parsed, next.URL)) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch article %s: %w", rawURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("article %s: HTTP %d", rawURL, response.StatusCode)
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml+xml") {
		return nil, fmt.Errorf("article %s returned non-HTML Content-Type %q", rawURL, contentType)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read article %s: %w", rawURL, err)
	}
	return body, nil
}

func hzcHTTPClient() http.Client {
	if defaultClient != nil {
		return *defaultClient
	}
	return http.Client{Timeout: 15 * time.Second}
}

func hzcHTTPURL(parsed *url.URL) bool {
	if parsed == nil || parsed.Host == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "http" || scheme == "https"
}

func hzcURLAuthorityEqual(left, right *url.URL) bool {
	return left != nil && right != nil && hzcHTTPURL(left) && hzcHTTPURL(right) &&
		strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func hzcContentImages(contentSel *goquery.Selection, baseURL string) []string {
	images := make([]string, 0)
	seen := make(map[string]bool)
	if contentSel == nil || contentSel.Length() == 0 {
		return images
	}
	contentSel.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := hzcResourceURL(baseURL, hzcImageSource(image))
		if resolved == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		images = append(images, resolved)
	})
	return images
}

func hzcImageSource(image *goquery.Selection) string {
	if image == nil || image.Length() == 0 {
		return ""
	}
	var fallback string
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		value := strings.TrimSpace(image.AttrOr(attr, ""))
		if value == "" {
			continue
		}
		if fallback == "" {
			fallback = value
		}
		if !hzcResourceMarked(value) {
			return value
		}
	}
	return fallback
}

func hzcResourceMarked(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(html.UnescapeString(raw)))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return true
	}
	for _, marker := range []string{
		"captcha", "verifycode", "verification", "createimage", "getcode", "codevalue",
		"access-denied", "access_denied", "waf", "login", "验证码", "人机验证", "安全验证",
		"spacer.gif", "blank.gif", "transparent.gif", "placeholder", "loading.gif", "pixel.gif",
	} {
		if strings.Contains(raw, marker) {
			return true
		}
	}
	return false
}

func hzcResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" || hzcResourceMarked(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || !hzcHTTPURL(parsed) {
		return ""
	}
	parsed.Fragment = ""
	return cleanURL(parsed.String())
}

func hzcAttachmentURL(baseURL, raw string) string {
	resolved := hzcResourceURL(baseURL, raw)
	if resolved == "" {
		return ""
	}
	base, baseErr := url.Parse(baseURL)
	target, targetErr := url.Parse(resolved)
	if baseErr != nil || targetErr != nil || !hzcHTTPURL(base) || !hzcHTTPURL(target) ||
		!strings.EqualFold(base.Host, target.Host) {
		return ""
	}
	return resolved
}

func hzcAttachmentLink(link *goquery.Selection, href, baseURL string) bool {
	if link == nil || link.Length() == 0 || strings.TrimSpace(href) == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	if hzcAttachmentURL(baseURL, href) == "" {
		return false
	}
	if strings.TrimSpace(link.AttrOr("download", "")) != "" {
		return true
	}
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") || hzcAttachmentRE.MatchString(lower) {
		return true
	}
	for _, selector := range []string{".attachment", ".attachments", ".news-attachment", ".download", ".annex"} {
		if ancestor := link.Closest(selector); ancestor.Length() > 0 && strings.Contains(ancestor.Text(), "附件") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(strings.Join(strings.Fields(link.Text()), " ")), "下载")
}

func hzcAttachmentName(link *goquery.Selection, resolved string) string {
	for _, attr := range []string{"download", "data-filename", "data-name", "name"} {
		if value := strings.Join(strings.Fields(html.UnescapeString(link.AttrOr(attr, ""))), " "); value != "" {
			return value
		}
	}
	if value := strings.Join(strings.Fields(link.Text()), " "); value != "" {
		return strings.Trim(strings.TrimSpace(value), "【】[]")
	}
	if parsed, err := url.Parse(resolved); err == nil {
		if index := strings.LastIndex(parsed.Path, "/"); index >= 0 && index+1 < len(parsed.Path) {
			if value, err := url.PathUnescape(parsed.Path[index+1:]); err == nil && value != "" {
				return value
			}
		}
	}
	return "附件"
}

func hzcVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved := hzcResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := hzcFetchResource(resolved, baseURL, referer, resolved, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func hzcVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := hzcAttachmentURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := hzcFetchResource(resolved, baseURL, referer, strings.Join([]string{attachment.Name, resolved}, " "), false)
		if err != nil {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func hzcFetchResource(rawURL, baseURL, referer, nameOrURL string, image bool) ([]byte, string, error) {
	resolved := hzcResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("hzc: refusing invalid resource %q", rawURL)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || !hzcHTTPURL(parsed) {
		return nil, "", fmt.Errorf("hzc: refusing non-HTTP resource %q", resolved)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("hzc: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", referer)
	}
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := hzcHTTPClient()
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if !hzcHTTPURL(next.URL) || (len(via) > 0 && !hzcURLAuthorityEqual(parsed, next.URL)) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("hzc: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("hzc: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	if response.ContentLength > hzcMaxResourceBytes {
		return nil, contentType, fmt.Errorf("hzc: resource %s exceeds size limit", resolved)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, hzcMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("hzc: read resource %s: %w", resolved, err)
	}
	if int64(len(body)) > hzcMaxResourceBytes || !hzcResourceBodyIsUsable(contentType, body, nameOrURL, image) {
		return nil, contentType, fmt.Errorf("hzc: resource %s returned an invalid Content-Type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func hzcResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	probe := bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	textProbe := strings.ToLower(string(probe))
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "cloudflare", "just a moment", "cf-chl-",
		"请输入验证码", "验证码下载附件", "请先登录", "请登录后", "请求已被阻断", "安全威胁", "应用维护中",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}
	magic := hzcResourceMagic(body)
	if image {
		switch mediaType {
		case "image/jpeg", "image/jpg", "image/pjpeg":
			return magic == "jpeg"
		case "image/png":
			return magic == "png"
		case "image/gif":
			return magic == "gif"
		case "image/webp":
			return magic == "webp"
		case "image/bmp":
			return magic == "bmp"
		case "image/tiff":
			return magic == "tiff"
		case "image/svg+xml":
			return magic == "svg"
		default:
			return false
		}
	}

	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf" && hzcBinaryContentTypeOK(mediaType, "pdf")
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "officedocument") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip" && hzcBinaryContentTypeOK(mediaType, "office-zip")
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole" && hzcBinaryContentTypeOK(mediaType, "office-ole")
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return (magic == "zip" || magic == "rar" || magic == "7z") && hzcBinaryContentTypeOK(mediaType, "archive")
	case strings.Contains(lowerName, ".rtf") || mediaType == "application/rtf" || mediaType == "text/rtf":
		return magic == "rtf" && hzcBinaryContentTypeOK(mediaType, "rtf")
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return !strings.Contains(textProbe, "<html")
	default:
		return magic != "" && hzcBinaryContentTypeOK(mediaType, "binary")
	}
}

func hzcBinaryContentTypeOK(mediaType, kind string) bool {
	if mediaType == "application/octet-stream" || mediaType == "application/force-download" || mediaType == "application/download" {
		return true
	}
	switch kind {
	case "pdf":
		return mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf")
	case "office-zip":
		return mediaType == "application/zip" || strings.Contains(mediaType, "officedocument") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml")
	case "office-ole":
		return mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint"
	case "archive":
		return mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed"
	case "rtf":
		return mediaType == "application/rtf" || mediaType == "text/rtf"
	case "binary":
		return true
	default:
		return false
	}
}

func hzcResourceMagic(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x03, 0x04}) || bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x05, 0x06}) || bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x07, 0x08})):
		return "zip"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}):
		return "ole"
	case len(body) >= 3 && bytes.Equal(body[:3], []byte{0xff, 0xd8, 0xff}):
		return "jpeg"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "png"
	case len(body) >= 6 && (bytes.Equal(body[:6], []byte("GIF87a")) || bytes.Equal(body[:6], []byte("GIF89a"))):
		return "gif"
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		return "webp"
	case len(body) >= 2 && bytes.Equal(body[:2], []byte("BM")):
		return "bmp"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{'I', 'I', '*', 0}) || bytes.Equal(body[:4], []byte{'M', 'M', 0, '*'})):
		return "tiff"
	case len(body) >= 7 && bytes.Equal(body[:7], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00}):
		return "rar"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x01, 0x00}):
		return "rar"
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	probe := trimmed
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if bytes.HasPrefix(trimmed, []byte("{\\rtf")) {
		return "rtf"
	}
	if bytes.Contains(bytes.ToLower(probe), []byte("<svg")) {
		return "svg"
	}
	return ""
}

func hzcSanitizeContentHTML(raw, baseURL, referer string, images []string, candidateAttachments, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	validImages := make(map[string]bool, len(images))
	for _, image := range images {
		if resolved := hzcResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		if resolved := hzcAttachmentURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}
	candidateSet := make(map[string]bool, len(candidateAttachments))
	for _, attachment := range candidateAttachments {
		if resolved := hzcAttachmentURL(baseURL, attachment.URL); resolved != "" {
			candidateSet[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"hzc-content-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	root := doc.Find("#hzc-content-root").First()
	root.Find("script, style, noscript").Remove()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := hzcResourceURL(baseURL, hzcImageSource(image))
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(html.UnescapeString(link.AttrOr("href", "")))
		if href == "" {
			link.Remove()
			return
		}
		if bodaIsPseudoResourceHref(href) && !strings.HasPrefix(href, "#") {
			link.Remove()
			return
		}
		if hzcAttachmentLink(link, href, baseURL) {
			resolved := hzcAttachmentURL(baseURL, href)
			if resolved == "" || !candidateSet[resolved] || !validAttachments[resolved] {
				link.Remove()
				return
			}
			link.SetAttr("href", resolved)
			return
		}
		resolved := hzcResourceURL(baseURL, href)
		if resolved == "" {
			if strings.HasPrefix(href, "#") {
				return
			}
			link.Remove()
			return
		}
		link.SetAttr("href", resolved)
	})
	rendered, err := root.Html()
	if err != nil {
		return ""
	}
	_ = referer // kept in the signature to make the sanitizer's call-site explicit
	return rendered
}

func hzcContentHTML(contentSel *goquery.Selection, baseURL string) string {
	if contentSel == nil || contentSel.Length() == 0 {
		return ""
	}
	clone := contentSel.Clone()
	clone.Find("script, style, noscript").Remove()
	clone.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := hzcResourceURL(baseURL, hzcImageSource(image))
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	clone.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(html.UnescapeString(link.AttrOr("href", "")))
		if href == "" || (bodaIsPseudoResourceHref(href) && !strings.HasPrefix(href, "#")) {
			link.Remove()
			return
		}
		if hzcAttachmentLink(link, href, baseURL) {
			if resolved := hzcAttachmentURL(baseURL, href); resolved != "" {
				link.SetAttr("href", resolved)
			} else {
				link.Remove()
			}
			return
		}
		if resolved := hzcResourceURL(baseURL, href); resolved != "" {
			link.SetAttr("href", resolved)
		}
	})
	rendered, err := clone.Html()
	if err != nil {
		return ""
	}
	return rendered
}

func hzcContentTextFromHTML(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"hzc-text-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	return extractBodaContentText(doc.Find("#hzc-text-root").First())
}
