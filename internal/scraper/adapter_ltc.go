package scraper

import (
	"bytes"
	"fmt"
	htmlstd "html"
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

const ltcBaseURL = "https://ltc.swjtu.edu.cn"

const ltcMaxResourceBytes int64 = 32 << 20

// ltcSubPages is the set of article-bearing pages found by walking the live
// top-level navigation, its submenu entries, and every homepage module.  The
// generated numbered pages are physical pages of their parent feed and are
// intentionally not registered as separate feeds.
var ltcSubPages = []string{
	"wqdt",
	"wqtz",
	"jcfc",
	"ggw/gzdt",
	"lltd",
	"jkzc",
	"djgz/djdt",
	"djgz/dwgk1",
	"djgz/gzzd",
	"djgz/jwgz",
	"fwzn",
	"fwzn/hdzx",
	"fwzn/lgbfwkbslc",
	"fwzn/txkbslc",
}

var (
	ltcArticlePathRE   = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	ltcDateTimeRE      = regexp.MustCompile(`(?i)20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?(?:\s+|[Tt])\s*[0-9]{1,2}\s*[:：]\s*[0-9]{2}(?:\s*[:：]\s*[0-9]{2})?`)
	ltcDateRE          = regexp.MustCompile(`(?i)20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?`)
	ltcPageNumberRE    = regexp.MustCompile(`^[0-9]+$`)
	ltcAttachExtRE     = regexp.MustCompile(`(?i)\.(?:docx?|xlsx?|pptx?|pdf|zip|rar|7z|wps|txt|csv)(?:$|[?#&])`)
	ltcResourceShellRE = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|请输入验证码|验证码下载|安全验证|人机验证|请先登录|请登录后|登录页面|页面不存在|您访问的页面不存在|无权限访问|系统错误|访问过于频繁|请求已被阻断|access\s+denied|forbidden|just\s+a\s+moment|cloudflare|captcha|waf`)
)

func init() {
	if site := AllSites["ltc"]; site != nil {
		site.BaseURL = ltcBaseURL
	}
	registerAdapter("ltc", SiteAdapter{
		SubPages:     ltcSubPages,
		ParseList:    parseAdapter_ltcList,
		ParseArticle: parseAdapter_ltcArticle,
		PageURL:      ltcPageURL,
		TotalPages:   ltcTotalPages,
		FetchArticle: ltcFetchArticle,
	})
}

// ltcPageURL maps the logical page number (1 = newest) to the reverse-numbered
// static files emitted by the site's CMS: slug.htm, slug/(N-1).htm, ...,
// slug/1.htm.
func ltcPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if page < 1 || totalPages < 0 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if slug == "" || slug == "index" {
		if page != 1 || totalPages > 1 {
			return ""
		}
		return baseURL + "/index" + extension
	}
	if page == 1 {
		return baseURL + "/" + slug + extension
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	physical := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

func ltcKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	for _, known := range ltcSubPages {
		if slug == known {
			return true
		}
	}
	return false
}

func ltcTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if !ltcKnownFeed(slug) {
		return 0, fmt.Errorf("ltc: %q is not a discovered article feed", slug)
	}
	body, err := fetch(ltcPageURL(ltcBaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("ltc: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("ltc: parse first page for %s: %w", slug, err)
	}
	if err := ltcRejectBlockedPage(doc, "list"); err != nil {
		return 0, err
	}
	if !ltcIsListPage(doc) {
		return 0, fmt.Errorf("ltc: %s is not an article-list shell", slug)
	}
	return ltcPaginationTotal(doc), nil
}

func parseAdapter_ltcList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ltc list: parse html: %w", err)
	}
	if err := ltcRejectBlockedPage(doc, "list"); err != nil {
		return nil, err
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType == "1" {
		return ltcHomepageItems(doc, baseURL)
	}
	if !ltcIsListPage(doc) {
		return nil, fmt.Errorf("ltc list: response is not a discovered article-list shell")
	}

	root := doc.Find(".main_right .conlistComm").First()
	items := make([]model.NewsItem, 0, root.ChildrenFiltered("dl").Length())
	seen := make(map[string]bool)
	localRows := make(map[string]bool)
	missingDate := false
	invalidRow := false
	root.ChildrenFiltered("dl").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("dt a[href*='info/']").Last()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := ltcArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || localRows[fullURL] {
			return
		}
		localRows[fullURL] = true
		title := ltcAnchorTitle(anchor)
		if title == "" {
			invalidRow = true
			return
		}
		date, publishedAt := ltcDateFields(row.Find("dd").First().Text())
		if date == "" {
			date, publishedAt = ltcDateFields(row.Text())
		}
		if date == "" {
			missingDate = true
			invalidRow = true
			return
		}
		itemType := ltcTypeText(row.Find("dt span a").First().Text())
		if itemType == "" {
			itemType = ltcListHeading(doc)
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        itemType,
			ParsedAt:    time.Now(),
		})
	})
	if missingDate {
		return nil, fmt.Errorf("ltc list: local article row has no publication date")
	}
	if invalidRow {
		return nil, fmt.Errorf("ltc list: local article row could not be parsed")
	}
	return items, nil
}

func parseAdapter_ltcArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ltc article: parse html: %w", err)
	}
	if err := ltcRejectBlockedPage(doc, "article"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) != "3" {
		return nil, fmt.Errorf("ltc article: response is not an article page")
	}
	scope := doc.Find(".main_right .article").First()
	if scope.Length() == 0 {
		scope = doc.Find(".article").First()
	}
	if scope.Length() == 0 {
		return nil, fmt.Errorf("ltc article: missing article container")
	}
	title := ltcText(scope.Find("h1").First())
	if title == "" {
		return nil, fmt.Errorf("ltc article: missing detail headline")
	}
	content := ltcArticleContent(scope)
	if content.Length() == 0 || (strings.TrimSpace(content.Text()) == "" && content.Find("img, a, table").Length() == 0) {
		return nil, fmt.Errorf("ltc article: missing or empty body container")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = title
	article.Type = ltcArticleHeading(doc)
	if article.Type == "" {
		return nil, fmt.Errorf("ltc article: missing breadcrumb type")
	}
	date, publishedAt := ltcDateFields(scope.Find(".source").First().Text())
	if date == "" {
		return nil, fmt.Errorf("ltc article: missing publication date in fixed source line")
	}
	article.Date = date
	article.PublishedAt = publishedAt
	if source := ltcSource(scope.Find(".source").First().Text()); source != "" {
		article.Source = source
	}
	article.Content = ltcArticleText(content)
	article.ContentHTML = ltcContentHTML(content, baseURL)
	article.Images = ltcImages(content, baseURL)
	article.Attachments = ltcAttachments(scope, baseURL)
	return article, nil
}

func ltcFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := ltcBaseURL
	if site := AllSites["ltc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return ltcFetchArticleAt(articleURL, baseURL)
}

func ltcFetchArticleAt(articleURL, baseURL string) (*model.Article, []byte, error) {
	fullURL, ok := ltcArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("ltc: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("ltc: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_ltcArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("ltc: parse article %s: %w", fullURL, err)
	}
	article.Images = ltcVerifyImages(article.Images, baseURL, fullURL)
	article.Attachments = ltcVerifyAttachments(article.Attachments, baseURL, fullURL)
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		validImages[image] = true
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		validAttachments[attachment.URL] = true
	}
	article.ContentHTML = ltcSanitizeContentHTML(article.ContentHTML, baseURL, validImages, validAttachments)
	article.Content = ltcContentTextFromHTML(article.ContentHTML)
	return article, body, nil
}

func ltcResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved, ok := ltcSameSiteURL(baseURL, raw)
	if !ok {
		return ""
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return ""
	}
	parsed.Fragment = ""
	lowerPath := strings.ToLower(parsed.Path)
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif", "/loading.gif"} {
		if strings.HasSuffix(lowerPath, placeholder) {
			return ""
		}
	}
	return parsed.String()
}

func ltcVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, image := range images {
		resolved := ltcResourceURL(baseURL, image)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := ltcFetchResource(resolved, baseURL, referer, true, ""); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func ltcVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := ltcResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := ltcFetchResource(resolved, baseURL, referer, false, attachment.Name)
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

func ltcFetchResource(rawURL, baseURL, referer string, image bool, nameOrURL string) ([]byte, string, error) {
	resolved := ltcResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("ltc: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("ltc: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := http.Client{Timeout: 15 * time.Second}
	if defaultClient != nil {
		client = *defaultClient
	}
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if next == nil || ltcResourceURL(baseURL, next.URL.String()) == "" {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("ltc: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("ltc: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	if response.ContentLength > ltcMaxResourceBytes {
		return nil, contentType, fmt.Errorf("ltc: resource %s exceeds size limit", resolved)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, ltcMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("ltc: read resource %s: %w", resolved, err)
	}
	if int64(len(body)) > ltcMaxResourceBytes || !ltcResourceBodyIsUsable(contentType, body, strings.Join([]string{nameOrURL, resolved}, " "), image) {
		return nil, contentType, fmt.Errorf("ltc: resource %s returned invalid Content-Type, shell, magic, or size", resolved)
	}
	return body, contentType, nil
}

func ltcResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if mediaType == "" {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	probe := body
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if ltcResourceShellRE.Match(probe) {
		return false
	}
	magic := ltcResourceMagic(body)
	if image {
		expected := map[string]string{
			"image/jpeg": "jpeg", "image/jpg": "jpeg", "image/png": "png",
			"image/gif": "gif", "image/webp": "webp", "image/bmp": "bmp",
			"image/tiff": "tiff", "image/svg+xml": "svg",
		}[mediaType]
		return strings.HasPrefix(mediaType, "image/") && magic != "" && (expected == "" || expected == magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	accept := func(values ...string) bool {
		for _, value := range values {
			if mediaType == value || mediaType == "application/octet-stream" || mediaType == "application/force-download" || mediaType == "application/download" {
				return true
			}
		}
		return false
	}
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf" && accept("application/pdf")
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "officedocument") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip" && accept("application/zip")
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole" && accept("application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint")
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return (magic == "zip" || magic == "rar" || magic == "7z") && accept("application/zip", "application/x-rar-compressed", "application/x-7z-compressed")
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != "" && accept("application/octet-stream")
	}
}

func ltcResourceMagic(body []byte) string {
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
	lower := bytes.ToLower(trimmed)
	if bytes.HasPrefix(lower, []byte("<svg")) || (bytes.HasPrefix(lower, []byte("<?xml")) && bytes.Contains(lower, []byte("<svg"))) {
		return "svg"
	}
	return ""
}

func ltcSanitizeContentHTML(rawHTML, baseURL string, validImages, validAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="ltc-content">` + rawHTML + `</div>`))
	if err != nil {
		return rawHTML
	}
	content := doc.Find("#ltc-content").First()
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := ltcResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				resolved = candidate
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
	content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		raw := htmlstd.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(raw) {
			anchor.RemoveAttr("href")
			anchor.RemoveAttr("download")
			return
		}
		resolved := cleanURL(resolveURL(baseURL, raw))
		if ltcIsAttachmentLink(anchor) {
			resource := ltcResourceURL(baseURL, resolved)
			if resource == "" || !validAttachments[resource] {
				anchor.RemoveAttr("href")
				anchor.RemoveAttr("download")
				return
			}
			anchor.SetAttr("href", resource)
			return
		}
		parsed, parseErr := url.Parse(resolved)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			anchor.RemoveAttr("href")
			return
		}
		anchor.SetAttr("href", resolved)
	})
	if html, err := content.Html(); err == nil {
		return html
	}
	return rawHTML
}

func ltcContentTextFromHTML(rawHTML string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="ltc-content">` + rawHTML + `</div>`))
	if err != nil {
		return ""
	}
	return ltcArticleText(doc.Find("#ltc-content").First())
}

func ltcIsHomepage(doc *goquery.Document) bool {
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" &&
		doc.Find(".iosSlider .slider").Length() > 0 &&
		doc.Find(".second").Length() > 0 &&
		doc.Find(".bgeee").Length() > 0 &&
		doc.Find(".third").Length() > 0
}

func ltcIsListPage(doc *goquery.Document) bool {
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "2" &&
		doc.Find(".main_right .current h3").Length() > 0 &&
		doc.Find(".main_right .conlistComm").Length() > 0
}

func ltcRejectBlockedPage(doc *goquery.Document, kind string) error {
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	text := strings.Join(strings.Fields(clone.Text()), " ")
	lowerText := strings.ToLower(text)
	title := strings.ToLower(ltcText(doc.Find("title").First()))
	for _, marker := range []string{
		"验证码", "安全验证", "访问过于频繁", "系统错误", "页面不存在", "您访问的页面不存在",
		"无权限访问", "请先登录", "请登录后", "access denied", "forbidden", "cloudflare",
		"just a moment", "captcha", "waf", "request blocked",
	} {
		marker = strings.ToLower(marker)
		if strings.Contains(lowerText, marker) || strings.Contains(title, marker) {
			return fmt.Errorf("ltc %s: source returned verification/block page (%s)", kind, marker)
		}
	}
	return nil
}

func ltcHomepageItems(doc *goquery.Document, baseURL string) ([]model.NewsItem, error) {
	if !ltcIsHomepage(doc) {
		return nil, fmt.Errorf("ltc list: pageType=1 response is not the known homepage shell")
	}
	items := make([]model.NewsItem, 0)
	byURL := make(map[string]int)
	add := func(row *goquery.Selection, itemType string) {
		var chosen *goquery.Selection
		var title string
		row.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			if _, ok := ltcArticleURL(baseURL, anchor.AttrOr("href", "")); !ok {
				return
			}
			candidate := ltcAnchorTitle(anchor)
			if candidate == "" {
				candidate = ltcText(row.Find(".text1, .title-link, .type").First())
			}
			if candidate == "" {
				return
			}
			chosen = anchor
			title = candidate
		})
		if chosen == nil || title == "" {
			return
		}
		fullURL, ok := ltcArticleURL(baseURL, chosen.AttrOr("href", ""))
		if !ok {
			return
		}
		date, publishedAt := ltcDateFields(row.Find(".date, em, time").First().Text())
		item := model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        strings.TrimSpace(itemType),
			ParsedAt:    time.Now(),
		}
		if index, exists := byURL[fullURL]; exists {
			if items[index].Date == "" && item.Date != "" {
				items[index].Date = item.Date
				items[index].PublishedAt = item.PublishedAt
			}
			if (strings.HasSuffix(items[index].Title, "...") || strings.HasSuffix(items[index].Title, "…")) && len(item.Title) > len(items[index].Title) {
				items[index].Title = item.Title
			}
			if items[index].Type == "" && item.Type != "" {
				items[index].Type = item.Type
			}
			return
		}
		byURL[fullURL] = len(items)
		items = append(items, item)
	}

	doc.Find(".iosSlider .slider .item").Each(func(_ int, row *goquery.Selection) {
		add(row, "头条新闻")
	})
	doc.Find(".second .l").Each(func(_ int, section *goquery.Selection) {
		typ := ltcText(section.Find(".title .h3").First())
		section.Find(".section, ul.common-list > li").Each(func(_ int, row *goquery.Selection) { add(row, typ) })
	})
	doc.Find(".second .r").Each(func(_ int, section *goquery.Selection) {
		typ := ltcText(section.Find(".title .h3").First())
		section.Find("ul.common-list1 > li").Each(func(_ int, row *goquery.Selection) { add(row, typ) })
	})
	doc.Find(".bgeee .dtbox").Each(func(_ int, section *goquery.Selection) {
		heading := section.Find("h3.title, .title .h3").First().Clone()
		heading.Find("a, span").Remove()
		typ := ltcText(heading)
		section.Find("ul.list > li").Each(func(_ int, row *goquery.Selection) { add(row, typ) })
	})
	doc.Find(".third .visual-slider").Each(func(_ int, section *goquery.Selection) {
		typ := ltcText(section.Find(".title .h3").First())
		if typ == "" {
			typ = "乐龄天地"
		}
		section.Find(".main-image3-list > li").Each(func(_ int, row *goquery.Selection) { add(row, typ) })
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("ltc homepage contains no local article modules")
	}
	return items, nil
}

func ltcAnchorTitle(anchor *goquery.Selection) string {
	if title := ltcTextFromAttr(anchor, "title"); title != "" {
		return title
	}
	return ltcText(anchor)
}

func ltcTextFromAttr(sel *goquery.Selection, name string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(sel.AttrOr(name, ""))), " ")
}

func ltcText(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	return strings.Join(strings.Fields(strings.TrimSpace(sel.Text())), " ")
}

func ltcTypeText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.TrimSpace(strings.Trim(value, "[]【】"))
	return value
}

func ltcListHeading(doc *goquery.Document) string {
	return ltcTypeText(ltcText(doc.Find(".main_right .current h3").First()))
}

func ltcArticleHeading(doc *goquery.Document) string {
	return ltcTypeText(ltcText(doc.Find(".main_right .current h3").First()))
}

func ltcDateFields(value string) (string, string) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "", ""
	}
	if match := ltcDateTimeRE.FindString(value); match != "" {
		publishedAt := normalizePublishedAt(match)
		if len(publishedAt) >= len("2006-01-02") {
			return publishedAt[:10], publishedAt
		}
	}
	if match := ltcDateRE.FindString(value); match != "" {
		date := normalizePublishedAt(match)
		if len(date) >= len("2006-01-02") {
			return date[:10], date
		}
	}
	return "", ""
}

func ltcArticleContent(scope *goquery.Selection) *goquery.Selection {
	content := scope.Find("#vsb_content_501 .v_news_content").First()
	if content.Length() == 0 {
		content = scope.Find(".v_news_content").First()
	}
	return content
}

func ltcArticleText(content *goquery.Selection) string {
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	const blocks = "p,h1,h2,h3,h4,h5,h6,li,td,th,blockquote,pre"
	parts := make([]string, 0)
	clone.Find(blocks).Each(func(_ int, sel *goquery.Selection) {
		if sel.ParentsFiltered(blocks).Length() > 0 {
			return
		}
		if value := ltcText(sel); value != "" {
			parts = append(parts, value)
		}
	})
	if len(parts) == 0 {
		return ltcText(clone)
	}
	return strings.Join(parts, "\n\n")
}

func ltcContentHTML(content *goquery.Selection, baseURL string) string {
	clone := content.Clone()
	clone.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		image.SetAttr("src", resolveURL(baseURL, image.AttrOr("src", "")))
	})
	clone.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if fullURL, ok := ltcSameSiteURL(baseURL, anchor.AttrOr("href", "")); ok && ltcIsAttachmentLink(anchor) {
			anchor.SetAttr("href", fullURL)
		}
	})
	html, err := clone.Html()
	if err != nil {
		return ""
	}
	return html
}

func ltcImages(content *goquery.Selection, baseURL string) []string {
	images := make([]string, 0)
	seen := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		fullURL := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := ltcResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				fullURL = candidate
				break
			}
		}
		if fullURL == "" || seen[fullURL] {
			return
		}
		seen[fullURL] = true
		images = append(images, fullURL)
	})
	return images
}

func ltcAttachments(scope *goquery.Selection, baseURL string) []model.Attachment {
	attachments := make([]model.Attachment, 0)
	seen := make(map[string]bool)
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if !ltcIsAttachmentLink(anchor) {
			return
		}
		fullURL, ok := ltcSameSiteURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		name := ltcTextFromAttr(anchor, "download")
		if name == "" {
			name = ltcTextFromAttr(anchor, "name")
		}
		if name == "" {
			name = ltcText(anchor)
		}
		if name == "" {
			return
		}
		seen[fullURL] = true
		attachments = append(attachments, model.Attachment{Name: name, URL: fullURL})
	})
	return attachments
}

func ltcIsAttachmentLink(anchor *goquery.Selection) bool {
	href := htmlstd.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
	value := strings.Join([]string{
		href,
		anchor.AttrOr("download", ""),
		anchor.AttrOr("name", ""),
		anchor.AttrOr("title", ""),
		anchor.Text(),
	}, " ")
	if ltcAttachExtRE.MatchString(value) {
		return true
	}
	lowerHref := strings.ToLower(href)
	if !strings.Contains(lowerHref, "download.jsp") {
		return false
	}
	return strings.Contains(lowerHref, "downloadattachurl") ||
		strings.Contains(lowerHref, "wbfileid=") ||
		strings.Contains(anchor.Text(), "附件") ||
		strings.Contains(anchor.Text(), "下载") ||
		strings.TrimSpace(anchor.AttrOr("download", "")) != ""
}

func ltcArticleURL(baseURL, href string) (string, bool) {
	fullURL, ok := ltcSameSiteURL(baseURL, href)
	if !ok {
		return "", false
	}
	u, err := url.Parse(fullURL)
	if err != nil || !ltcArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	return u.String(), true
}

func ltcSameSiteURL(baseURL, href string) (string, bool) {
	if strings.TrimSpace(href) == "" {
		return "", false
	}
	fullURL := resolveURL(baseURL, strings.TrimSpace(href))
	u, err := url.Parse(fullURL)
	if err != nil || u.Host == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return u.String(), true
}

func ltcSource(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	for _, label := range []string{"来源：", "来源:"} {
		if index := strings.Index(value, label); index >= 0 {
			value = strings.TrimSpace(value[index+len(label):])
			for _, next := range []string{"发布者：", "发布者:", "日期：", "日期:"} {
				if cut := strings.Index(value, next); cut >= 0 {
					value = strings.TrimSpace(value[:cut])
				}
			}
			return value
		}
	}
	return ""
}

func ltcPaginationTotal(doc *goquery.Document) int {
	pages := doc.Find(".main_right .p_pages").First()
	if pages.Length() == 0 {
		pages = doc.Find(".p_pages").First()
	}
	if pages.Length() == 0 {
		return 1
	}
	maxPage := 0
	pages.Find("a, span").Each(func(_ int, sel *goquery.Selection) {
		value := strings.TrimSpace(sel.Text())
		if !ltcPageNumberRE.MatchString(value) {
			return
		}
		page, err := strconv.Atoi(value)
		if err == nil && page > maxPage {
			maxPage = page
		}
	})
	if maxPage == 0 {
		maxPage = 1
	}
	return maxPage
}
