package scraper

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const (
	xbCanonicalBaseURL = "https://xb.swjtu.edu.cn"
	xbMaxResourceBytes = 32 << 20
)

var xbSubPages = []string{"xxfb/tzgg", "xxfb/xwdt", "cyxz", "fwzn/gflc", "fwzn/glwj", "zdgf/bgswj", "zdgf/xjwj"}

var xbFeedTypes = map[string]string{
	"xxfb/tzgg":  "通知公告",
	"xxfb/xwdt":  "新闻动态",
	"cyxz":       "常用下载",
	"fwzn/gflc":  "规范流程",
	"fwzn/glwj":  "管理文件",
	"zdgf/bgswj": "办公室文件",
	"zdgf/xjwj":  "校级文件",
}

var (
	xbArticlePathRE     = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	xbYearMonthRE       = regexp.MustCompile(`^(20\d{2})\s*[-/.]\s*(\d{1,2})$`)
	xbPublicationDateRE = regexp.MustCompile(`发表时间\s*[:：]\s*([0-9]{4}\s*[-/.年]\s*[0-9]{1,2}\s*[-/.月]\s*[0-9]{1,2}\s*(?:日|号)?(?:[ Tt]+[0-9]{1,2}:[0-9]{2}(?::[0-9]{2})?)?)`)
	xbErrorPageRE       = regexp.MustCompile(`(?i)404\s*(?:错误|not\s+found)|403\s+forbidden|500\s+(?:错误|error)|页面未找到|访问被拒绝|请求被拦截|安全验证|人机验证|请输入验证码|cloudflare|just\s+a\s+moment|please\s+verify|access\s+denied|request\s+blocked|\bwaf\b`)
	xbResourceShellRE   = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|附件下载|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|您访问的页面不存在|无权限访问|安全验证|安全威胁|请求已被阻断|cloudflare|just\s+a\s+moment|please\s+verify|access\s+denied`)
)

func init() {
	registerAdapter("xb", SiteAdapter{
		SubPages:     xbSubPages,
		ParseList:    parseAdapter_xbList,
		ParseArticle: parseAdapter_xbArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   xbTotalPages,
		FetchArticle: xbFetchArticle,
	})
}

func parseAdapter_xbList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xb: parse list HTML: %w", err)
	}
	if xbLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("xb: response is an error or verification page")
	}
	if xbIsHomepage(doc) {
		items := xbHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("xb: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := doc.Find("main .cont-diff-right .news-list-box").First()
	if container.Length() == 0 {
		container = doc.Find(".news-list-box").First()
	}
	if container.Length() == 0 {
		return nil, fmt.Errorf("xb: response has no news-list-box container")
	}
	typ := xbDocumentType(doc)
	if typ == "" {
		return nil, fmt.Errorf("xb: article list has no recognized section type")
	}
	return xbParseRows(container, baseURL, typ), nil
}

func xbLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	if doc.Find(".prompt_down, .error-page, .error-container, #captcha, .challenge-form").Length() > 0 {
		return true
	}
	text := strings.Join(strings.Fields(doc.Find("title, body").Text()), " ")
	return xbErrorPageRE.MatchString(text)
}

func xbIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" ||
		doc.Find(".list-box-div ul.scien-list").Length() > 0
}

func xbHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".list-box-div").Each(func(_ int, box *goquery.Selection) {
		typ := xbCanonicalType(box.Find(".plane-title .tz").First().Text())
		if typ == "" {
			typ = xbCanonicalType(box.Find(".plane-title").First().Text())
		}
		if typ == "" {
			return
		}
		box.Find("ul.scien-list > li").Each(func(_ int, row *goquery.Selection) {
			xbAppendItem(&items, seen, row.Find("a[href]").First(), row, typ, baseURL)
		})
	})
	return items
}

// xbDocumentType reads only the section controls that the xb templates use.
// The article headline and meta pageTitle are intentionally not considered a
// type: on detail pages pageTitle is the headline itself.
func xbDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{
		"main .cont-left .cont-right-ul li.active .line1",
		"main .cont-left > .block-title",
		"main .cont-diff-right > .block-title",
		`meta[name="pageTitle"]`,
	} {
		var value string
		if selector == `meta[name="pageTitle"]` {
			value = doc.Find(selector).AttrOr("content", "")
		} else {
			value = doc.Find(selector).First().Text()
		}
		if typ := xbCanonicalType(value); typ != "" {
			return typ
		}
	}
	return ""
}

func xbCanonicalType(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	switch value {
	case "新闻动态", "通知公告", "常用下载", "规范流程", "管理文件", "办公室文件", "校级文件":
		return value
	default:
		return ""
	}
}

func xbParseRows(container *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	rows := container.Find(".new-list_des")
	if rows.Length() == 0 {
		rows = container.Find("li")
	}
	if rows.Length() == 0 {
		rows = container.Find("a[href*='info/']").Parent()
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			anchor = row.Find("a[href]").First()
		}
		xbAppendItem(&items, seen, anchor, row, typ, baseURL)
	})
	return items
}

func xbAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL, ok := xbSameOriginURL(baseURL, href)
	if !ok || !xbArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".new-l_b_title, .line1, .title, .news-title, h3, h4, em").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := xbListDate(row)
	if typ == "" {
		typ = xbCanonicalType(row.Find(".tag, .category, .type").First().Text())
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func xbListDate(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	day := strings.TrimSpace(row.Find(".new-list_date_top").First().Text())
	yearMonth := strings.Join(strings.Fields(row.Find(".new-list_date_bottom").First().Text()), "")
	if day != "" && yearMonth != "" {
		if match := xbYearMonthRE.FindStringSubmatch(yearMonth); len(match) == 3 {
			date := normalizeDateParts(match[1], match[2], day)
			if date != "" {
				return date, ""
			}
		}
	}
	for _, selector := range []string{".scien-list-time", "time", ".date", ".time"} {
		if date, publishedAt := xbDateValues(row.Find(selector).First().Text()); date != "" {
			return date, publishedAt
		}
	}
	return "", ""
}

func xbDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(value), " ")
	if match := dateTimeRe.FindString(value); match != "" {
		if published := normalizePublishedAt(match); len(published) >= 10 {
			return published[:10], published
		}
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, ""
	}
	return "", ""
}

func xbTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if xbFeedTypes[slug] == "" {
		return 0, fmt.Errorf("xb: %q is not an article-list route", slug)
	}
	baseURL := xbCanonicalBaseURL
	if site := AllSites["xb"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("xb: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xb: parse first page for %s: %w", slug, err)
	}
	if xbLooksLikeErrorPage(doc) || doc.Find(".news-list-box").Length() == 0 || xbDocumentType(doc) == "" {
		return 0, fmt.Errorf("xb: %s is not a news list page", slug)
	}
	if total := xbPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func xbPageCount(doc *goquery.Document) int {
	maxPage := 0
	if doc == nil {
		return 0
	}
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, node *goquery.Selection) {
		value, err := strconv.Atoi(strings.TrimSpace(node.Text()))
		if err == nil && value > maxPage {
			maxPage = value
		}
	})
	return maxPage
}

func parseAdapter_xbArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xb: parse article HTML: %w", err)
	}
	if xbLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("xb: article is an error or verification page")
	}
	title := strings.Join(strings.Fields(doc.Find(`form[name="_newscontent_fromname"] .left-top .news-title`).First().Text()), " ")
	if title == "" {
		return nil, fmt.Errorf("xb: response has no fixed article title")
	}
	typ := xbDocumentType(doc)
	if typ == "" {
		return nil, fmt.Errorf("xb: response has no fixed article type")
	}
	content := xbDetailContent(doc)
	if content == nil || content.Length() == 0 {
		return nil, fmt.Errorf("xb: response has no fixed article body")
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = title
	article.Type = typ
	article.Date, article.PublishedAt = xbFixedArticleDate(doc)
	xbApplyFixedContent(article, content, baseURL)
	article.Attachments = xbKeepLocalAttachments(article.Attachments, baseURL)
	xbAppendContentAttachments(article, content, baseURL)
	xbAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func xbDetailContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	for _, selector := range []string{
		`form[name="_newscontent_fromname"] #vsb_content_501 .v_news_content`,
		`form[name="_newscontent_fromname"] #vsb_content .v_news_content`,
		// Legacy articles (pre-2023) suffix the wrapper id with an arbitrary
		// column number, e.g. vsb_content_4.
		`form[name="_newscontent_fromname"] div[id^="vsb_content"] .v_news_content`,
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return nil
}

func xbFixedArticleDate(doc *goquery.Document) (string, string) {
	if doc == nil {
		return "", ""
	}
	meta := doc.Find(`form[name="_newscontent_fromname"] .left-top .news-sub-title`).First()
	if meta.Length() == 0 {
		return "", ""
	}
	value := strings.Join(strings.Fields(meta.Text()), " ")
	// Prefer the complete clock-bearing match; the labelled expression also
	// accepts date-only text and may otherwise stop before the optional clock.
	if match := dateTimeRe.FindString(value); match != "" {
		return xbDateValues(match)
	}
	if match := xbPublicationDateRE.FindStringSubmatch(value); len(match) == 2 {
		return xbDateValues(match[1])
	}
	// Keep the source position fixed at .news-sub-title, but tolerate a
	// template variant that omits the literal “发表时间” label.
	if match := bareDateRe.FindString(value); match != "" {
		return xbDateValues(match)
	}
	return "", ""
}

func xbApplyFixedContent(article *model.Article, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}
	article.Content = extractBodaContentText(content)
	article.Images = nil
	clone := content.Clone()
	seenImages := make(map[string]bool)
	clone.Find("img").Each(func(_ int, image *goquery.Selection) {
		var raw string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				raw = value
				break
			}
		}
		if raw == "" {
			return
		}
		resolved, ok := xbSameOriginURL(baseURL, raw)
		if !ok || seenImages[resolved] {
			return
		}
		seenImages[resolved] = true
		article.Images = append(article.Images, resolved)
		image.SetAttr("src", resolved)
	})
	clone.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if !bodaLooksLikeAttachmentLink(link, strings.TrimSpace(link.AttrOr("href", ""))) {
			return
		}
		if resolved, ok := xbSameOriginURL(baseURL, link.AttrOr("href", "")); ok {
			link.SetAttr("href", resolved)
		}
	})
	if html, err := clone.Html(); err == nil {
		article.ContentHTML = html
	}
}

func xbAppendContentAttachments(article *model.Article, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved, ok := xbSameOriginURL(baseURL, href)
		if !ok || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: xbAttachmentName(link, href), URL: resolved})
	})
}

func xbKeepLocalAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	local := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved, ok := xbSameOriginURL(baseURL, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		attachment.URL = resolved
		seen[resolved] = true
		local = append(local, attachment)
	}
	return local
}

func xbAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" || xbArticleURL(cleanURL(resolveURL(baseURL, href)), baseURL) || !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved, ok := xbSameOriginURL(baseURL, href)
		if !ok || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: xbAttachmentName(link, href), URL: resolved})
	})
}

func xbAttachmentName(link *goquery.Selection, href string) string {
	name := strings.Join(strings.Fields(link.AttrOr("download", "")), " ")
	if name == "" {
		name = strings.Join(strings.Fields(link.AttrOr("name", "")), " ")
	}
	if name == "" {
		name = strings.Join(strings.Fields(link.Text()), " ")
	}
	if name == "" {
		name = href
	}
	return name
}

func xbArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && xbArticlePathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}

func xbSameOriginURL(baseURL, rawURL string) (string, bool) {
	resolved := cleanURL(resolveURL(baseURL, strings.TrimSpace(rawURL)))
	parsed, err := url.Parse(resolved)
	base, baseErr := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || baseErr != nil || parsed.Hostname() == "" || base.Hostname() == "" {
		return "", false
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return "", false
	}
	if !strings.EqualFold(parsed.Hostname(), base.Hostname()) || parsed.Port() != base.Port() {
		return "", false
	}
	return resolved, true
}

func xbSiteBaseURL() string {
	if site := AllSites["xb"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return xbCanonicalBaseURL
}

func xbFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := xbSiteBaseURL()
	canonicalURL, ok := xbSameOriginURL(baseURL, articleURL)
	if !ok || !xbArticleURL(canonicalURL, baseURL) {
		return nil, nil, fmt.Errorf("xb: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonicalURL)
	if err != nil {
		return nil, nil, fmt.Errorf("xb: fetch article %s: %w", canonicalURL, err)
	}
	article, err := parseAdapter_xbArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("xb: parse article %s: %w", canonicalURL, err)
	}
	article.Images = xbVerifyImages(article.Images, canonicalURL)
	article.Attachments = xbVerifyAttachments(article.Attachments, canonicalURL)
	xbSanitizeContentHTML(article, canonicalURL)
	return article, body, nil
}

func xbVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, image := range images {
		resolved, ok := xbSameOriginURL(baseURL, image)
		if !ok || seen[resolved] {
			continue
		}
		if _, _, err := xbFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func xbVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved, ok := xbSameOriginURL(baseURL, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		body, _, err := xbFetchResource(resolved, baseURL, false)
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

func xbFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	resolved, ok := xbSameOriginURL(referer, rawURL)
	if !ok {
		return nil, "", fmt.Errorf("xb: refusing non-local resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("xb: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}
		if _, sameOrigin := xbSameOriginURL(referer, next.URL.String()); sameOrigin {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("xb: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("xb: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	if response.ContentLength > xbMaxResourceBytes {
		return nil, contentType, fmt.Errorf("xb: resource %s exceeds %d bytes", resolved, xbMaxResourceBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, xbMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("xb: read resource %s: %w", resolved, err)
	}
	if len(body) > xbMaxResourceBytes || !xbResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("xb: resource %s returned invalid Content-Type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func xbResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" || mediaType == "text/html" || mediaType == "application/xhtml+xml" || mediaType == "application/json" || mediaType == "text/xml" || mediaType == "application/xml" || strings.HasSuffix(mediaType, "+xml") {
		return false
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if xbResourceShellRE.Match(probe) {
		return false
	}

	magic := xbResourceMagic(body)
	if image {
		return xbImageMediaMatchesMagic(mediaType, magic)
	}

	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip" && (mediaType == "application/octet-stream" || mediaType == "application/zip" || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"))
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole" && (mediaType == "application/octet-stream" || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint")
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values"
	case strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return mediaType == "application/octet-stream" && magic != ""
	}
}

func xbImageMediaMatchesMagic(mediaType, magic string) bool {
	switch mediaType {
	case "image/jpeg", "image/jpg":
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
		return strings.HasPrefix(mediaType, "image/") && (magic == "jpeg" || magic == "png" || magic == "gif" || magic == "webp" || magic == "bmp" || magic == "tiff" || magic == "svg")
	}
}

func xbResourceMagic(body []byte) string {
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
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	if len(trimmed) >= 4 && strings.HasPrefix(strings.ToLower(string(trimmed)), "<svg") {
		return "svg"
	}
	return ""
}

func xbSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		if resolved, ok := xbSameOriginURL(baseURL, image); ok {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved, ok := xbSameOriginURL(baseURL, attachment.URL); ok {
			validAttachments[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="xb-content-root">` + article.ContentHTML + `</div>`))
	if err != nil {
		article.ContentHTML = ""
		return
	}
	root := doc.Find("#xb-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var raw string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				raw = value
				break
			}
		}
		resolved, ok := xbSameOriginURL(baseURL, raw)
		if !ok || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		resolved, sameOrigin := xbSameOriginURL(baseURL, href)
		if !sameOrigin || !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		if validAttachments[resolved] {
			link.SetAttr("href", resolved)
			return
		}
		// Keep the visible label while removing an unverified local URL. A
		// cross-origin PDF/guide is a normal external link and is left intact.
		link.RemoveAttr("href")
	})
	if html, err := root.Html(); err == nil {
		article.ContentHTML = html
	}
}
