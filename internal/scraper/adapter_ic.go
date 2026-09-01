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

const icCanonicalBaseURL = "https://ic.swjtu.edu.cn"

// The IC site has a small custom front end.  These are the four article-list
// entry points linked by the current navigation/home page.  gywm, rcpy,
// wlxy, and ypwm are profile, programme, campus-planning, or recruitment
// pages; they are deliberately not treated as article feeds.
var icSubPages = []string{
	"xwzx",
	"xwzx/tzgg",
	"xwzx/xygy",
	"xwzx/jcsp",
}

var (
	icArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	icDateTimeRe    = regexp.MustCompile(`20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?\s*(?:[Tt]|\s)+\s*[0-9]{1,2}\s*[:：]\s*[0-9]{2}(?:\s*[:：]\s*[0-9]{2})?`)
	icDateRe        = regexp.MustCompile(`20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?`)
	icTotalCountRe  = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
	icPageCountRe   = regexp.MustCompile(`(?:/|共|第)\s*([0-9]+)\s*页`)
	icFileExtRe     = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
)

func init() {
	if site := AllSites["ic"]; site != nil {
		site.BaseURL = icCanonicalBaseURL
	}
	registerAdapter("ic", SiteAdapter{
		SubPages:     icSubPages,
		ParseList:    parseAdapter_icList,
		ParseArticle: parseAdapter_icArticle,
		PageURL:      icPageURL,
		ListPage:     icListPage,
		FetchArticle: icFetchArticle,
	})
}

// icPageURL maps logical page numbers to the reversed generated filenames
// used by the static site: page 1 is xwzx.htm and the last logical page is
// xwzx/1.htm.
func icPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
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

// icListPage owns the request so that the site receives a browser-like
// Referer and so an access-denied HTML shell cannot be mistaken for a list.
func icListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		page = 1
	}
	firstURL := icPageURL(icCanonicalBaseURL, slug, 1, 0, ".htm")
	firstBody, err := icFetchHTML(firstURL)
	if err != nil {
		return nil, 0, err
	}
	totalPages, err := icTotalPagesFromHTML(firstBody, slug)
	if err != nil {
		return nil, 0, err
	}
	if page == 1 {
		items, parseErr := parseAdapter_icList(firstBody, icCanonicalBaseURL)
		return items, totalPages, parseErr
	}
	listURL := icPageURL(icCanonicalBaseURL, slug, page, totalPages, ".htm")
	body, err := icFetchHTML(listURL)
	if err != nil {
		return nil, 0, err
	}
	items, err := parseAdapter_icList(body, icCanonicalBaseURL)
	return items, totalPages, err
}

func icTotalPages(slug string) (int, error) {
	body, err := icFetchHTML(icPageURL(icCanonicalBaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, err
	}
	return icTotalPagesFromHTML(body, slug)
}

func icTotalPagesFromHTML(body []byte, slug string) (int, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("ic: parse list %q: %w", slug, err)
	}
	if err := icResponseError(doc); err != nil {
		return 0, err
	}
	if icIsHomepage(doc) {
		return 1, nil
	}
	if doc.Find(`meta[name="pageType"]`).Length() > 0 && doc.Find(`meta[name="pageType"]`).AttrOr("content", "") == "3" {
		return 0, fmt.Errorf("ic: %s is an article page, not a list", slug)
	}

	items, parseErr := parseAdapter_icList(body, icCanonicalBaseURL)
	if parseErr != nil {
		return 0, parseErr
	}
	if len(items) == 0 && !icIsExplicitEmptyList(doc) {
		return 0, fmt.Errorf("ic: %s has no article rows or explicit empty marker", slug)
	}

	pageZone := doc.Find(".p_t, .p_pages, .pb_sys_common, .pagination, .paging, .page").First()
	pageText := strings.Join(strings.Fields(pageZone.Text()), "")
	if match := icPageCountRe.FindStringSubmatch(pageText); match != nil {
		if total, convErr := strconv.Atoi(match[1]); convErr == nil && total > 0 {
			return total, nil
		}
	}

	// A few versions only publish "共 N 条".  The first page's real row
	// count gives the exact ceiling because the site uses a fixed page size.
	if countMatch := icTotalCountRe.FindStringSubmatch(strings.Join(strings.Fields(doc.Text()), "")); countMatch != nil {
		totalCount, convErr := strconv.Atoi(countMatch[1])
		if convErr == nil {
			if totalCount == 0 {
				return 1, nil
			}
			if len(items) > 0 {
				return (totalCount + len(items) - 1) / len(items), nil
			}
		}
	}

	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d, .pagination .page").Each(func(_ int, sel *goquery.Selection) {
		if page, convErr := strconv.Atoi(strings.TrimSpace(sel.Text())); convErr == nil && page > maxPage {
			maxPage = page
		}
	})
	if maxPage > 0 {
		return maxPage, nil
	}
	if doc.Find(".p_pages, .pb_sys_common .p_t, .pagination, .paging").Length() > 0 {
		return 1, nil
	}
	return 0, fmt.Errorf("ic: %s has no source pagination", slug)
}

func icFetchHTML(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ic: create request %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", icCanonicalBaseURL+"/")
	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ic: fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ic: fetch %s: status %d", rawURL, resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml+xml") {
		return nil, fmt.Errorf("ic: %s returned non-HTML content type %q", rawURL, contentType)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ic: read %s: %w", rawURL, err)
	}
	return body, nil
}

func icFetchArticle(articleURL string) (*model.Article, []byte, error) {
	body, err := icFetchHTML(articleURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_icArticle(body, icCanonicalBaseURL)
	if err != nil {
		return nil, nil, err
	}
	return article, body, nil
}

func parseAdapter_icList(rawHTML []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(rawHTML)))
	if err != nil {
		return nil, fmt.Errorf("ic: parse list HTML: %w", err)
	}
	if err := icResponseError(doc); err != nil {
		return nil, err
	}
	if icIsHomepage(doc) {
		return icParseHomepage(doc, baseURL)
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")); pageType == "3" {
		return nil, fmt.Errorf("ic: article shell passed to list parser")
	}
	if icLooksLikeResourcePage(doc) {
		return nil, fmt.Errorf("ic: resource page is not an article list")
	}

	listType := icSectionType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if icIsNonContentAnchor(anchor) {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL, ok := icArticleURL(baseURL, href)
		if !ok {
			return
		}
		title := icAnchorTitle(anchor)
		if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
			return
		}
		date := icDateForAnchor(anchor)
		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     icDateOnly(date),
			Type:     listType,
			ParsedAt: time.Now(),
		}
		if precise := normalizePublishedAt(date); precise != "" {
			item.PublishedAt = precise
			if len(precise) >= 10 {
				item.Date = precise[:10]
			}
		}
		if index, exists := seen[fullURL]; exists {
			if items[index].Title == "" {
				items[index].Title = item.Title
			}
			if items[index].Date == "" && item.Date != "" {
				items[index].Date = item.Date
				items[index].PublishedAt = item.PublishedAt
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, item)
	})

	if len(items) > 0 {
		return items, nil
	}
	if icIsExplicitEmptyList(doc) {
		return []model.NewsItem{}, nil
	}
	return nil, fmt.Errorf("ic: page is not a supported article list")
}

func icParseHomepage(doc *goquery.Document, baseURL string) ([]model.NewsItem, error) {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	addRows := func(rows *goquery.Selection, typ string) {
		rows.Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find(".title a[href]").First()
			if anchor.Length() == 0 {
				anchor = row.Find("a[href]").First()
			}
			if anchor.Length() == 0 {
				return
			}
			fullURL, ok := icArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok || seen[fullURL] {
				return
			}
			title := icAnchorTitle(anchor)
			if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
				return
			}
			date := icDateForHomeRow(row)
			item := model.NewsItem{Title: title, URL: fullURL, Date: icDateOnly(date), Type: typ, ParsedAt: time.Now()}
			if precise := normalizePublishedAt(date); precise != "" {
				item.PublishedAt = precise
				item.Date = precise[:10]
			}
			seen[fullURL] = true
			items = append(items, item)
		})
	}

	// The source renders all notice rows in the page, including rows hidden by
	// CSS tabs in newer revisions.  Walk every row rather than only the first
	// visible tab.  The campus homepage also repeats more-links; selecting the
	// .title anchor and URL-deduplicating avoids duplicate articles.
	addRows(doc.Find(".index_new > ul > li, .index_new ul > li"), "新闻动态")
	addRows(doc.Find(".index_notice > ul > li, .index_notice ul > li"), "通知公告")
	if len(items) == 0 {
		return nil, fmt.Errorf("ic: homepage contains no article modules")
	}
	return items, nil
}

func parseAdapter_icArticle(rawHTML []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(rawHTML)))
	if err != nil {
		return nil, fmt.Errorf("ic: parse article HTML: %w", err)
	}
	if err := icResponseError(doc); err != nil {
		return nil, err
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")); pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("ic: page type %q is not an article", pageType)
	}

	article, err := parseBodaArticle(rawHTML, baseURL)
	if err != nil {
		return nil, err
	}
	var headline string
	for _, selector := range []string{".show01 h5", ".show01 h1", ".show01 .title", ".show01 .news-title"} {
		if value := cleanTitle(strings.TrimSpace(doc.Find(selector).First().Text())); value != "" {
			headline = value
			break
		}
	}
	if headline == "" {
		return nil, fmt.Errorf("ic: article headline is missing")
	}
	article.Title = headline
	article.Type = icSectionType(doc)

	if meta := doc.Find(".show01").First(); meta.Length() > 0 {
		meta.Find("em, span").Each(func(_ int, field *goquery.Selection) {
			value := strings.TrimSpace(field.Text())
			if strings.HasPrefix(value, "来源：") || strings.HasPrefix(value, "来源:") {
				article.Source = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "来源："), "来源:"))
			}
		})
		if precise := icPreciseDate(flattenedText(meta)); precise != "" {
			article.PublishedAt = precise
			article.Date = precise[:10]
		}
	}
	if article.Date == "" {
		if precise := icPreciseDate(flattenedText(doc.Find(".show01 p, .article-meta, .news_source").First())); precise != "" {
			article.PublishedAt = precise
			article.Date = precise[:10]
		}
	}
	normalizeArticlePublishedAt(article)
	icExtractAttachments(doc, article, baseURL)
	icDeduplicateResources(article)
	return article, nil
}

func icResponseError(doc *goquery.Document) error {
	if doc == nil {
		return fmt.Errorf("ic: empty response")
	}
	title := strings.ToLower(strings.TrimSpace(doc.Find("title").First().Text()))
	body := strings.Join(strings.Fields(doc.Find("body").Text()), " ")
	if strings.Contains(title, "访问限制") || strings.Contains(title, "access denied") ||
		strings.Contains(body, "本系统仅限") || strings.Contains(body, "暂无权限访问该资源") ||
		(strings.Contains(body, "校园网") && strings.Contains(body, "暂无权限")) {
		return fmt.Errorf("ic: source returned an access-denied/WAF shell")
	}
	return nil
}

func icIsHomepage(doc *goquery.Document) bool {
	return doc.Find(".index_new, .index_notice").Length() > 0 &&
		doc.Find(".app header, .head, meta[name='pageTitle']").Length() > 0
}

func icIsExplicitEmptyList(doc *goquery.Document) bool {
	text := strings.Join(strings.Fields(doc.Text()), "")
	if icTotalCountRe.MatchString(text) {
		return strings.Contains(text, "共0条")
	}
	return strings.Contains(text, "暂无数据") || strings.Contains(text, "暂无文章") || strings.Contains(text, "没有相关内容")
}

func icLooksLikeResourcePage(doc *goquery.Document) bool {
	if doc.Find("a[href*='info/']").Length() > 0 {
		return false
	}
	resourceLinks := 0
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.ToLower(strings.TrimSpace(sel.AttrOr("href", "")))
		if icFileExtRe.MatchString(href) || strings.Contains(href, "download") || strings.Contains(href, "download.jsp") {
			resourceLinks++
		}
	})
	return resourceLinks > 0
}

func icArticleURL(baseURL, href string) (string, bool) {
	if _, err := url.Parse(strings.TrimSpace(href)); err != nil {
		return "", false
	}
	full := cleanURL(resolveURL(baseURL, href))
	resolved, err := url.Parse(full)
	if err != nil || !icArticlePathRe.MatchString(resolved.Path) {
		return "", false
	}
	if isExternalURL(full, baseURL) || !strings.EqualFold(resolved.Host, mustURLHost(baseURL)) {
		return "", false
	}
	return full, true
}

func mustURLHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func icIsNonContentAnchor(sel *goquery.Selection) bool {
	for _, selector := range []string{
		"header", "nav", "footer", ".head", ".nav", ".menu-mc", ".search",
		".banner", ".index_guid", ".logo", ".recommend", ".related",
		".sidebar", ".side", ".share", ".breadcrumb", ".crumbs",
	} {
		if sel.Closest(selector).Length() > 0 {
			return true
		}
	}
	return false
}

func icAnchorTitle(anchor *goquery.Selection) string {
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		for _, selector := range []string{".title", ".news-title", ".txt", "h5", "h4", "h3", "p"} {
			if candidate := strings.TrimSpace(anchor.Find(selector).First().Text()); candidate != "" {
				title = candidate
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	return cleanTitle(title)
}

func icDateForHomeRow(row *goquery.Selection) string {
	dateBlock := row.Find(".date").First()
	if dateBlock.Length() > 0 {
		year := strings.TrimSpace(dateBlock.Find(".year").First().Text())
		monthDay := strings.TrimSpace(dateBlock.Find(".moth, .month").First().Text())
		if year != "" && monthDay != "" {
			return year + "-" + monthDay
		}
		if value := strings.TrimSpace(dateBlock.Text()); value != "" {
			return value
		}
	}
	for _, selector := range []string{".date", ".time", "time"} {
		if value := strings.TrimSpace(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func icDateForAnchor(anchor *goquery.Selection) string {
	if value := icDateForHomeRow(anchor); value != "" {
		return value
	}
	parent := anchor.Parent()
	for depth := 0; parent.Length() > 0 && depth < 5; depth++ {
		if value := icDateForHomeRow(parent); value != "" {
			return value
		}
		parent = parent.Parent()
	}
	return ""
}

func icDateOnly(value string) string {
	if precise := normalizePublishedAt(value); len(precise) >= 10 {
		return precise[:10]
	}
	return ""
}

func icPreciseDate(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if match := icDateTimeRe.FindString(value); match != "" {
		return normalizePublishedAt(match)
	}
	if match := icDateRe.FindString(value); match != "" {
		return normalizePublishedAt(match)
	}
	return ""
}

func icSectionType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	var value string
	for _, selector := range []string{"#nav_slide .n_title h1", ".n_title h1", ".position #curType", ".position", ".current"} {
		value = cleanTitle(strings.TrimSpace(doc.Find(selector).First().Text()))
		if value != "" {
			break
		}
	}
	switch value {
	case "新闻中心", "NEWS", "NEWS新闻动态":
		return "新闻动态"
	case "视频中心", "精彩视频":
		return "精彩视频"
	case "校园光影", "校园光影相册":
		return "校园光影"
	}
	return value
}

func icExtractAttachments(doc *goquery.Document, article *model.Article, baseURL string) {
	if doc == nil || article == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" || !icLooksLikeAttachment(anchor) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if resolved == "" || seen[resolved] {
			return
		}
		name := strings.TrimSpace(anchor.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(anchor.AttrOr("name", ""))
		}
		if name == "" {
			name = strings.TrimSpace(anchor.Text())
		}
		if name == "" {
			name = resolved
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func icLooksLikeAttachment(anchor *goquery.Selection) bool {
	href := strings.ToLower(strings.TrimSpace(anchor.AttrOr("href", "")))
	name := strings.ToLower(strings.TrimSpace(anchor.AttrOr("name", "")))
	download := strings.TrimSpace(anchor.AttrOr("download", ""))
	if strings.HasPrefix(strings.TrimLeft(href, "/"), "info/") {
		return false
	}
	if download != "" || icFileExtRe.MatchString(href) || icFileExtRe.MatchString(name) || strings.Contains(href, "download") {
		return true
	}
	for depth, parent := 0, anchor; parent.Length() > 0 && depth < 4; depth, parent = depth+1, parent.Parent() {
		if parent.Is("body") || parent.Is("html") {
			break
		}
		text := strings.Join(strings.Fields(parent.Text()), "")
		if strings.Contains(text, "附件") && !strings.Contains(href, "/info/") {
			return true
		}
	}
	return false
}

func icDeduplicateResources(article *model.Article) {
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
