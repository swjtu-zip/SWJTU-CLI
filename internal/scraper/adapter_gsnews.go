package scraper

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// gsnews.swjtu.edu.cn uses the 博达 CMS, but its navigation leaves are not
// interchangeable with the other sites.  In particular, generated list
// files are numbered from oldest to newest while the public page presents
// the newest page as page 1.  Keep the route inventory, pagination, list
// filtering, and article enrichment in this adapter.
func init() {
	registerAdapter("gsnews", SiteAdapter{
		SubPages:     gsnewsSubPages,
		ParseList:    parseAdapter_gsnewsList,
		ParseArticle: parseAdapter_gsnewsArticle,
		PageURL:      gsnewsPageURL,
		TotalPages:   gsnewsTotalPages,
		FetchArticle: gsnewsFetchArticle,
	})
}

// The first feeds are homepage/news modules.  The remaining entries are the
// actual navigation leaves that expose an article list, including the
// secondary links reachable from article sidebars and hidden notice tabs.
// Static profile pages, external招生/研工 links, service-card pages, empty
// tabs, and the two download-only pages are deliberately not feeds.  The
// footer's 教学日历 page is a real one-page article list and is included even
// though it is not part of the primary navigation.
var gsnewsSubPages = []string{
	"xwdtnew/rdxw",
	"xwdtnew/xsdt",
	"tzggnew/qb",
	"tzggnew/pygz",
	"tzggnew/xwgz",
	"tzggnew/sxsj",
	"tzggnew/zhxx",
	"gzlc/pygz",
	"gzzdnew/pygz",
	"gzzdnew/xwgz",
	"gzzdnew/sxsj",
	"gzzdnew/gjjl",
	"pygznew/pyfa",
	"pygznew/jxgl",
	"pygznew/kcsz",
	"pygznew/sxsj",
	"pygznew/gjjl",
	"pygznew/jxgg",
	"pygznew/jcjs",
	"pygznew/zldd",
	"xwgznew/xwsy",
	"xwgznew/xwsqd",
	"xwgznew/dsdw",
	"xwgznew/pycy",
	"xwgznew/tdxl",
	"xzzx/pygz",
	"xzzx/xwgz",
	"xzzx/sxsj",
	"xzzx/gjjl",
	"xzzx/lxsxg",
	"zhxxnew/xkjs",
	"zhxxnew/cydjt",
	"zsgz/sszs",
	"zsgz/bszs",
	"zsgz/zsxc",
	"jxrl",
}

// Audited live totals.  The HTML pagination is authoritative at runtime;
// these values only keep a reverse-page URL usable when a page-count request
// is temporarily unavailable (and make an out-of-range request a miss rather
// than silently repeating page 1).
var gsnewsPageTotals = map[string]int{
	"xwdtnew/rdxw":  9,
	"xwdtnew/xsdt":  2,
	"tzggnew/qb":    60,
	"tzggnew/pygz":  34,
	"tzggnew/xwgz":  5,
	"tzggnew/sxsj":  3,
	"tzggnew/zhxx":  19,
	"xzzx/pygz":     2,
	"xzzx/xwgz":     1,
	"xzzx/sxsj":     1,
	"xzzx/gjjl":     1,
	"xzzx/lxsxg":    1,
	"zsgz/sszs":     12,
	"zsgz/bszs":     5,
	"gzlc/pygz":     1,
	"gzzdnew/pygz":  1,
	"gzzdnew/xwgz":  1,
	"gzzdnew/sxsj":  1,
	"gzzdnew/gjjl":  1,
	"pygznew/pyfa":  1,
	"pygznew/jxgl":  1,
	"pygznew/kcsz":  1,
	"pygznew/sxsj":  1,
	"pygznew/gjjl":  1,
	"pygznew/jxgg":  1,
	"pygznew/jcjs":  1,
	"pygznew/zldd":  1,
	"xwgznew/xwsy":  1,
	"xwgznew/xwsqd": 1,
	"xwgznew/dsdw":  1,
	"xwgznew/pycy":  1,
	"xwgznew/tdxl":  1,
	"zhxxnew/xkjs":  1,
	"zhxxnew/cydjt": 1,
	"zsgz/zsxc":     1,
	"jxrl":          1,
}

var (
	gsnewsArticlePathRE  = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	gsnewsDateRE         = regexp.MustCompile(`20[0-9]{2}\s*(?:[-/.年]\s*[0-9]{1,2}\s*[-/.月]\s*[0-9]{1,2}\s*日?|年\s*[0-9]{1,2}\s*月\s*[0-9]{1,2}\s*日?)(?:\s*[Tt ]\s*[0-9]{1,2}:[0-9]{2}(?::[0-9]{2})?)?`)
	gsnewsDayYearMonthRE = regexp.MustCompile(`([0-9]{1,2})\s*(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`)
	gsnewsPageNumberRE   = regexp.MustCompile(`^[0-9]+$`)
	gsnewsAttachmentRE   = regexp.MustCompile(`(?i)(?:download\.jsp|\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf|od[st]))(?:$|[?#])`)
)

const (
	gsnewsCanonicalBaseURL = "https://gsnews.swjtu.edu.cn"
	gsnewsMaxResourceBytes = 32 << 20
)

// gsnewsPageURL maps logical pages (1 = newest) to generated files.  The
// unsuffixed URL is always logical page 1; logical page N is stored at
// totalPages-N+1.htm.
func gsnewsPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages <= 1 {
		totalPages = gsnewsPageTotals[slug]
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	physical := totalPages - page + 1
	if physical < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

func gsnewsTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" || slug == "home" {
		return 1, nil
	}
	if !gsnewsKnownFeed(slug) {
		return 0, fmt.Errorf("gsnews: %q is not an audited article-list route", slug)
	}

	baseURL := gsnewsBaseURL()
	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(gsnewsPageURL(baseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if err := gsnewsResponseError(body); err != nil {
			lastErr = err
			continue
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			lastErr = err
			continue
		}
		if doc.Find("main.news-list-page-main ul.news-list").Length() == 0 {
			lastErr = fmt.Errorf("gsnews: %s has no recognized news list", slug)
			continue
		}
		if total := gsnewsPaginationTotal(doc); total > 0 {
			return total, nil
		}
		return 1, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("gsnews: unable to read page count for %s", slug)
	}
	return 0, lastErr
}

func gsnewsKnownFeed(slug string) bool {
	for _, known := range gsnewsSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func gsnewsBaseURL() string {
	if site := AllSites["gsnews"]; site != nil {
		if baseURL := strings.TrimRight(strings.TrimSpace(site.BaseURL), "/"); baseURL != "" {
			return baseURL
		}
	}
	return gsnewsCanonicalBaseURL
}

func gsnewsPaginationTotal(doc *goquery.Document) int {
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		value := strings.TrimSpace(sel.Text())
		if !gsnewsPageNumberRE.MatchString(value) {
			return
		}
		page, err := strconv.Atoi(value)
		if err == nil && page > maxPage {
			maxPage = page
		}
	})
	return maxPage
}

func parseAdapter_gsnewsList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("gsnews: parse list HTML: %w", err)
	}
	if err := gsnewsResponseError(raw); err != nil {
		return nil, err
	}

	if gsnewsIsHome(doc) {
		items := parseGsnewsHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("gsnews: homepage has no recognized article modules")
		}
		return items, nil
	}

	list := doc.Find("main.news-list-page-main ul.news-list").First()
	if list.Length() == 0 {
		return nil, fmt.Errorf("gsnews: page has no recognized article list")
	}
	typ := gsnewsPageType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	list.Find("li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		gsnewsAppendItem(&items, seen, anchor, row, baseURL, typ)
	})
	// An empty <ul class="news-list"> with a valid page shell is a legitimate
	// empty tab, not a parser failure. Direct resource links in 下载中心 are
	// intentionally ignored because only /info/... URLs are article details.
	return items, nil
}

func gsnewsIsHome(doc *goquery.Document) bool {
	return doc.Find("main.index-main").Length() > 0 ||
		(doc.Find("#content-1").Length() > 0 && doc.Find(".msg-tabNav").Length() > 0)
}

func parseGsnewsHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	// The news module contains both a slider and a three-row list.  The list
	// is the stable source of dates; the slider is deliberately not parsed.
	doc.Find("main.index-main .news-list li").Each(func(_ int, row *goquery.Selection) {
		gsnewsAppendItem(&items, seen, row.Find("a[href]").First(), row, baseURL, "热点新闻")
	})

	noticeTypes := []string{"通知公告", "培养工作", "学位工作", "实习实践", "招生工作", "综合信息"}
	doc.Find("main.index-main .tabcons").Each(func(index int, panel *goquery.Selection) {
		typ := "通知公告"
		if index < len(noticeTypes) {
			typ = noticeTypes[index]
		}
		panel.Find("li").Each(func(_ int, row *goquery.Selection) {
			gsnewsAppendItem(&items, seen, row.Find("a[href]").First(), row, baseURL, typ)
		})
	})
	return items
}

func gsnewsAppendItem(items *[]model.NewsItem, seen map[string]int, anchor, scope *goquery.Selection, baseURL, typ string) {
	if anchor == nil || anchor.Length() == 0 || scope == nil || scope.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if !gsnewsArticleURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		for _, selector := range []string{".content", ".right-content", ".title", "p", "h1", "h2", "h3"} {
			if value := strings.TrimSpace(anchor.Find(selector).First().Text()); value != "" {
				title = value
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	// The URL is already constrained to the site's /info/ article namespace
	// and the anchor is inside a recognized news-list.  Do not apply the
	// shared navigation-title heuristic here: valid notices on this site can
	// end in a Chinese quote mark or exceed the generic title-length limit.
	if title == "" {
		return
	}
	date, publishedAt := gsnewsDateFromScope(scope)
	item := model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: strings.TrimSpace(typ), ParsedAt: time.Now()}
	if index, ok := seen[fullURL]; ok {
		if (*items)[index].Date == "" && item.Date != "" {
			(*items)[index].Date = item.Date
			(*items)[index].PublishedAt = item.PublishedAt
		}
		// The homepage's 全部 panel is a duplicate of the more precise tab;
		// retain the latter as the type when it is available.
		if ((*items)[index].Type == "通知公告" || (*items)[index].Type == "") && item.Type != "" && item.Type != "通知公告" {
			(*items)[index].Type = item.Type
		}
		return
	}
	seen[fullURL] = len(*items)
	*items = append(*items, item)
}

func gsnewsArticleURL(rawURL, baseURL string) bool {
	_, ok := gsnewsArticleURLValue(rawURL, baseURL)
	return ok
}

func gsnewsArticleURLValue(rawURL, baseURL string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, html.UnescapeString(strings.TrimSpace(rawURL))))
	parsed, err := url.Parse(fullURL)
	if err != nil || !gsnewsArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Host == "" || parsed.Host == "" {
		return "", false
	}
	if !gsnewsSameAuthority(parsed, base) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func gsnewsSameAuthority(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Hostname(), right.Hostname()) &&
		strings.EqualFold(left.Port(), right.Port())
}

func gsnewsPageType(doc *goquery.Document) string {
	for _, selector := range []string{
		"main.news-list-page-main section .current-title",
		"main.news-list-page-main .current-title",
		"main.news-list-page-main aside .lm-title",
	} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func gsnewsDateFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}
	raw := strings.Join(strings.Fields(scope.Find("time, .time, .date").First().Text()), " ")
	if raw == "" {
		raw = strings.Join(strings.Fields(scope.Text()), " ")
	}
	if match := gsnewsDayYearMonthRE.FindStringSubmatch(raw); match != nil {
		date := normalizeDateParts(match[2], match[3], match[1])
		return date, date
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" && len(publishedAt) >= 10 {
		return publishedAt[:10], publishedAt
	}
	if match := gsnewsDateRE.FindString(raw); match != "" {
		publishedAt := normalizePublishedAt(match)
		if publishedAt != "" && len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	return "", ""
}

func parseAdapter_gsnewsArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("gsnews: parse article HTML: %w", err)
	}
	if err := gsnewsResponseError(raw); err != nil {
		return nil, err
	}
	detail := doc.Find("main.news-detail-page-main").First()
	if detail.Length() == 0 {
		return nil, fmt.Errorf("gsnews: page has no recognized article content")
	}
	titleSel := detail.Find(".intro-part .title").First()
	contentSel := detail.Find(".intro-part .desc #vsb_content .v_news_content, .intro-part .desc .v_news_content").First()
	if titleSel.Length() == 0 || contentSel.Length() == 0 {
		return nil, fmt.Errorf("gsnews: page has no fixed article title/body")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = strings.Join(strings.Fields(titleSel.Text()), " ")
	if article.Title == "" {
		return nil, fmt.Errorf("gsnews: article title is empty")
	}
	article.Type = gsnewsPageTypeFromArticle(doc)
	if article.Type == "" {
		return nil, fmt.Errorf("gsnews: article type is empty")
	}
	rawDate := strings.Join(strings.Fields(detail.Find(".intro-part .little-tips .fbsj").First().Text()), " ")
	article.Date, article.PublishedAt = gsnewsDateFromText(rawDate)
	if article.Date == "" {
		return nil, fmt.Errorf("gsnews: article publication date is empty")
	}
	// Keep the body anchored to this site's detail container.  The shared
	// parser remains useful for common fields, but it must not select a
	// navigation/related-content .v_news_content if the template changes.
	article.Content = extractBodaContentText(contentSel)
	if bodyHTML, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = bodyHTML
	}
	appendGsnewsAttachments(article, doc, baseURL)
	resolveGsnewsContentHTML(article, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func gsnewsPageTypeFromArticle(doc *goquery.Document) string {
	for _, selector := range []string{
		"main.news-detail-page-main section .current-title",
		"main.news-detail-page-main .current-title",
		"main.news-detail-page-main aside li.active a",
	} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func gsnewsDateFromText(raw string) (string, string) {
	raw = strings.Join(strings.Fields(raw), " ")
	for _, prefix := range []string{"发布时间：", "发布时间:", "日期：", "日期:"} {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, prefix))
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" && len(publishedAt) >= 10 {
		return publishedAt[:10], publishedAt
	}
	if match := gsnewsDateRE.FindString(raw); match != "" {
		publishedAt := normalizePublishedAt(match)
		if publishedAt != "" && len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	return "", ""
}

func appendGsnewsAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	// Some pages leave .provenance empty and render the actual attachment UL
	// directly beside it inside the article section.  Scan that whole section,
	// then keep only download/file URLs so page-turn and external links do not
	// become attachments.
	doc.Find("main.news-detail-page-main section a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" || !gsnewsAttachmentRE.MatchString(strings.ToLower(href)) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if seen[resolved] {
			return
		}
		name := strings.TrimSpace(anchor.Text())
		if name == "" {
			name = strings.TrimSpace(anchor.AttrOr("download", ""))
		}
		if name == "" {
			name = resolved
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func gsnewsFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := gsnewsBaseURL()
	fullURL, ok := gsnewsArticleURLValue(articleURL, baseURL)
	if !ok {
		return nil, nil, fmt.Errorf("gsnews: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("gsnews: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_gsnewsArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("gsnews: parse article %s: %w", fullURL, err)
	}
	article.Images = gsnewsVerifyImages(article.Images, baseURL, fullURL)
	article.Attachments = gsnewsVerifyAttachments(article.Attachments, baseURL, fullURL)
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		validImages[image] = true
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		validAttachments[attachment.URL] = true
	}
	article.ContentHTML = gsnewsSanitizeContentHTML(article.ContentHTML, baseURL, validImages, validAttachments)
	if content := gsnewsContentTextFromHTML(article.ContentHTML); content != "" {
		article.Content = content
	}
	return article, body, nil
}

func gsnewsVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := gsnewsResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := gsnewsFetchResource(resolved, baseURL, referer, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func gsnewsVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := gsnewsResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := gsnewsFetchResource(resolved, baseURL, referer, false)
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

func gsnewsResourceURL(baseURL, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" || !gsnewsSameAuthority(parsed, mustParseGsnewsURL(baseURL)) {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	lowerPath := strings.ToLower(parsed.Path)
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif"} {
		if strings.HasSuffix(lowerPath, placeholder) {
			return ""
		}
	}
	return resolved
}

func mustParseGsnewsURL(raw string) *url.URL {
	parsed, _ := url.Parse(strings.TrimSpace(raw))
	return parsed
}

func gsnewsFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	resolved := gsnewsResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("gsnews: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("gsnews: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", referer)
	}
	client := http.Client{}
	if defaultClient != nil {
		client = *defaultClient
	}
	baseParsed := mustParseGsnewsURL(baseURL)
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if next == nil || baseParsed == nil || !gsnewsSameAuthority(next.URL, baseParsed) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("gsnews: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("gsnews: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, gsnewsMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("gsnews: read resource %s: %w", resolved, err)
	}
	if len(body) > gsnewsMaxResourceBytes || !gsnewsResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("gsnews: resource %s returned invalid Content-Type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func gsnewsResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType == "" {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	lowerProbe := bytes.ToLower(bytes.TrimSpace(probe))
	for _, marker := range [][]byte{
		[]byte("<!doctype html"), []byte("<html"), []byte("<head"), []byte("<body"),
		[]byte("验证码"), []byte("安全验证"), []byte("captcha"), []byte("waf"),
		[]byte("access denied"), []byte("just a moment"), []byte("cloudflare"),
		[]byte("页面不存在"), []byte("404 not found"), []byte("请先登录"), []byte("请登录后"),
		[]byte("请求已被阻断"),
	} {
		if bytes.Contains(lowerProbe, marker) {
			return false
		}
	}

	magic := gsnewsResourceMagic(body)
	if image {
		return gsnewsImageTypeMatchesMagic(mediaType, magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return gsnewsBinaryContentTypeOK(mediaType, "pdf") && magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return gsnewsBinaryContentTypeOK(mediaType, "office-zip") && magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return gsnewsBinaryContentTypeOK(mediaType, "office-ole") && magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return gsnewsBinaryContentTypeOK(mediaType, "archive") && (magic == "zip" || magic == "rar" || magic == "7z")
	case strings.Contains(lowerName, ".rtf") || mediaType == "application/rtf" || mediaType == "text/rtf":
		return gsnewsBinaryContentTypeOK(mediaType, "rtf") && magic == "rtf"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func gsnewsBinaryContentTypeOK(mediaType, kind string) bool {
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
	default:
		return false
	}
}

func gsnewsImageTypeMatchesMagic(mediaType, magic string) bool {
	expected := map[string]string{
		"image/jpeg": "jpeg", "image/jpg": "jpeg", "image/pjpeg": "jpeg",
		"image/png": "png", "image/gif": "gif", "image/webp": "webp",
		"image/bmp": "bmp", "image/tiff": "tiff", "image/svg+xml": "svg",
	}[mediaType]
	return expected != "" && magic == expected
}

func gsnewsResourceMagic(body []byte) string {
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
	if bytes.HasPrefix(trimmed, []byte("{\\rtf")) {
		return "rtf"
	}
	probe := trimmed
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	lower := bytes.ToLower(probe)
	if bytes.HasPrefix(lower, []byte("<svg")) || (bytes.HasPrefix(lower, []byte("<?xml")) && bytes.Contains(lower, []byte("<svg"))) {
		return "svg"
	}
	return ""
}

func gsnewsSanitizeContentHTML(rawHTML, baseURL string, validImages, validAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="gsnews-content">` + rawHTML + `</div>`))
	if err != nil {
		return rawHTML
	}
	content := doc.Find("#gsnews-content").First()
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		var resolved string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := gsnewsResourceURL(baseURL, image.AttrOr(attr, "")); value != "" {
				resolved = value
				break
			}
		}
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		raw := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(raw) {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		resolved := cleanURL(resolveURL(baseURL, raw))
		if bodaLooksLikeAttachmentLink(link, raw) {
			if !validAttachments[resolved] {
				link.RemoveAttr("href")
				link.RemoveAttr("download")
				return
			}
		}
		if parsed, parseErr := url.Parse(resolved); parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	if htmlText, err := content.Html(); err == nil {
		return htmlText
	}
	return rawHTML
}

func gsnewsContentTextFromHTML(rawHTML string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="gsnews-content">` + rawHTML + `</div>`))
	if err != nil {
		return ""
	}
	return extractBodaContentText(doc.Find("#gsnews-content").First())
}

func resolveGsnewsContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(article.ContentHTML))
	if err != nil {
		return
	}
	doc.Find("img[src]").Each(func(_ int, sel *goquery.Selection) {
		sel.SetAttr("src", resolveURL(baseURL, strings.TrimSpace(sel.AttrOr("src", ""))))
	})
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		sel.SetAttr("href", resolveURL(baseURL, strings.TrimSpace(sel.AttrOr("href", ""))))
	})
	if html, err := doc.Find("body").Html(); err == nil && strings.TrimSpace(html) != "" {
		article.ContentHTML = html
		return
	}
	if html, err := doc.Html(); err == nil {
		article.ContentHTML = html
	}
}

func gsnewsResponseError(raw []byte) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("gsnews: invalid HTML response: %w", err)
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"404", "not found", "页面不存在", "访问错误", "access denied", "waf"} {
		if strings.Contains(title, marker) {
			return fmt.Errorf("gsnews: received error page (%s)", marker)
		}
	}
	mainText := strings.ToLower(strings.Join(strings.Fields(doc.Find("main").First().Text()), " "))
	for _, marker := range []string{"验证码", "安全验证", "access denied", "waf challenge"} {
		if strings.Contains(mainText, marker) {
			return fmt.Errorf("gsnews: received verification/error page (%s)", marker)
		}
	}
	return nil
}
