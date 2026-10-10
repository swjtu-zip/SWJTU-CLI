package scraper

import (
	"bytes"
	"fmt"
	htmlstd "html"
	"io"
	"mime"
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

const icMaxResourceBytes int64 = 32 << 20

// The IC site has a small custom front end.  These are the two article-list
// entry points linked by the current navigation/home page.  The campus
// gallery and video center are media surfaces rather than /info article
// lists, while gywm, rcpy, wlxy, and ypwm are profile, programme,
// campus-planning, or recruitment pages; none are news feeds.  xwzx.htm
// itself is now a column hub without pagination; the reverse-numbered news
// list continues at xwzx/xwjj.htm (see icPageURL).
var icSubPages = []string{
	"xwzx",
	"xwzx/tzgg",
}

var (
	icArticlePathRe   = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	icDateTimeRe      = regexp.MustCompile(`20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?\s*(?:[Tt]|\s)+\s*[0-9]{1,2}\s*[:：]\s*[0-9]{2}(?:\s*[:：]\s*[0-9]{2})?`)
	icDateRe          = regexp.MustCompile(`20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?`)
	icTotalCountRe    = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
	icPageCountRe     = regexp.MustCompile(`(?:/|共|第)\s*([0-9]+)\s*页`)
	icFileExtRe       = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
	icResourceShellRe = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|404\s+not\s+found|access\s+denied|cloudflare|just\s+a\s+moment|安全验证|请求已被阻断|暂无权限访问`)
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
// used by the static site: page 1 is the list's slug.htm and the last logical
// page is slug/1.htm.  The xwzx.htm column hub lost its own pagination when
// the site was reorganized; the reverse-numbered news list it used to carry
// now lives at xwzx/xwjj.htm, so the xwzx feed follows that list.
func icPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if slug == "xwzx" {
		slug = "xwzx/xwjj"
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		return ""
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

func icBaseURL() string {
	if site := AllSites["ic"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return icCanonicalBaseURL
}

func icIsKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "index" {
		return true
	}
	for _, candidate := range icSubPages {
		if slug == candidate {
			return true
		}
	}
	return false
}

// icListPage owns the request so that the site receives a browser-like
// Referer and so an access-denied HTML shell cannot be mistaken for a list.
func icListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("ic: page must be positive: %d", page)
	}
	if !icIsKnownFeed(slug) {
		return nil, 0, fmt.Errorf("ic: unknown or non-article feed %q", slug)
	}
	baseURL := icBaseURL()
	firstURL := icPageURL(baseURL, slug, 1, 0, ".htm")
	firstBody, err := icFetchHTML(firstURL)
	if err != nil {
		return nil, 0, err
	}
	totalPages, err := icTotalPagesFromHTML(firstBody, slug)
	if err != nil {
		return nil, 0, err
	}
	if page > totalPages {
		return nil, totalPages, fmt.Errorf("ic: page %d is out of range 1..%d for %s", page, totalPages, slug)
	}
	if page == 1 {
		items, parseErr := parseAdapter_icList(firstBody, baseURL)
		return items, totalPages, parseErr
	}
	listURL := icPageURL(baseURL, slug, page, totalPages, ".htm")
	body, err := icFetchHTML(listURL)
	if err != nil {
		return nil, 0, err
	}
	items, err := parseAdapter_icList(body, baseURL)
	return items, totalPages, err
}

func icTotalPages(slug string) (int, error) {
	if !icIsKnownFeed(slug) {
		return 0, fmt.Errorf("ic: unknown or non-article feed %q", slug)
	}
	body, err := icFetchHTML(icPageURL(icBaseURL(), slug, 1, 0, ".htm"))
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

	items, parseErr := parseAdapter_icList(body, icBaseURL())
	if parseErr != nil {
		return 0, parseErr
	}
	if len(items) == 0 && !icIsExplicitEmptyList(doc) {
		return 0, fmt.Errorf("ic: %s has no article rows or explicit empty marker", slug)
	}

	pageZone := doc.Find(".pb_sys_common, .p_t, .p_pages, .pagination, .paging, .page").First()
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
			if rows := icSourceRowCount(doc); rows > 0 {
				return (totalCount + rows - 1) / rows, nil
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

// icSourceRowCount counts source rows, not parsed local articles.  The live
// list can include external notice links which are intentionally filtered;
// using len(parsedItems) as the page size would then overestimate the number
// of pages and either duplicate or skip reverse-numbered files.
func icSourceRowCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	for _, selector := range []string{
		".news-list-page ul.news-list > li",
		".news-list-page .news-list > li",
		".news-list > li",
		".index_new2 > ul > li",
		".notice_1 > ul > li",
		".conlistComm > dl",
	} {
		if rows := doc.Find(selector).Length(); rows > 0 {
			return rows
		}
	}
	return 0
}

func icFetchHTML(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ic: create request %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if parsed, parseErr := url.Parse(rawURL); parseErr == nil && parsed.Scheme != "" && parsed.Host != "" {
		req.Header.Set("Referer", parsed.Scheme+"://"+parsed.Host+"/")
	} else {
		req.Header.Set("Referer", icCanonicalBaseURL+"/")
	}
	client := icHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ic: fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
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
	baseURL := icBaseURL()
	fullURL, ok := icArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("ic: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := icFetchHTML(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_icArticle(body, baseURL)
	if err != nil {
		return nil, nil, err
	}
	candidateImages := append([]string(nil), article.Images...)
	candidateAttachments := append([]model.Attachment(nil), article.Attachments...)
	article.Images = icVerifyImages(candidateImages, baseURL, fullURL)
	article.Attachments = icVerifyAttachments(candidateAttachments, baseURL, fullURL)
	article.ContentHTML = icSanitizeContentHTML(article.ContentHTML, baseURL, candidateImages, candidateAttachments, article.Images, article.Attachments)
	icDeduplicateResources(article)
	return article, body, nil
}

func icHTTPClient() http.Client {
	if defaultClient != nil {
		return *defaultClient
	}
	return http.Client{}
}

func icVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := icResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := icFetchResource(resolved, baseURL, referer, true)
		if err != nil || !icResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func icVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := icResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := icFetchResource(resolved, baseURL, referer, false)
		if err != nil || !icResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func icFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	resolved := icResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("ic: refusing invalid resource %q", rawURL)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", fmt.Errorf("ic: refusing non-HTTP resource %q", resolved)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("ic: create resource request: %w", err)
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
	client := icHTTPClient()
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next == nil || (next.URL.Scheme != "http" && next.URL.Scheme != "https") {
			return fmt.Errorf("ic: refusing non-HTTP resource redirect")
		}
		if len(via) > 0 && !icSameAuthority(parsed, via[len(via)-1].URL) {
			return fmt.Errorf("ic: refusing cross-site resource redirect")
		}
		if !icSameAuthority(parsed, next.URL) {
			return fmt.Errorf("ic: refusing cross-site resource redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("ic: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("ic: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	if response.ContentLength > icMaxResourceBytes {
		return nil, contentType, fmt.Errorf("ic: resource %s exceeds %d bytes", resolved, icMaxResourceBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, icMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("ic: read resource %s: %w", resolved, err)
	}
	if int64(len(body)) > icMaxResourceBytes || !icResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("ic: resource %s returned invalid Content-Type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func icResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(base.Host, parsed.Host) {
		return ""
	}
	lower := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"captcha", "verifycode", "verification", "createimage", "getcode", "access-denied", "waf",
		"/login", "/signin", "placeholder", "transparent", "spacer", "blank", "loading", "pixel",
	} {
		if strings.Contains(lower, marker) {
			return ""
		}
	}
	parsed.Fragment = ""
	return cleanURL(parsed.String())
}

func icSameAuthority(left, right *url.URL) bool {
	return left != nil && right != nil && strings.EqualFold(left.Host, right.Host)
}

func icResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType == "" {
		return false
	}
	switch mediaType {
	case "text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml":
		return false
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if icResourceShellRe.Match(probe) {
		return false
	}

	magic := icResourceMagic(body)
	if image {
		return icImageTypeMatchesMagic(mediaType, magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "application/octet-stream"
	case strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func icImageTypeMatchesMagic(mediaType, magic string) bool {
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
		return false
	}
}

func icResourceMagic(body []byte) string {
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
	if len(trimmed) > 0 {
		probe := trimmed
		if len(probe) > 4096 {
			probe = probe[:4096]
		}
		if bytes.Contains(bytes.ToLower(probe), []byte("<svg")) {
			return "svg"
		}
	}
	return ""
}

func icSanitizeContentHTML(rawHTML, baseURL string, candidateImages []string, candidateAttachments []model.Attachment, validImages []string, validAttachments []model.Attachment) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"ic-content-root\">" + rawHTML + "</div>"))
	if err != nil {
		return rawHTML
	}
	root := doc.Find("#ic-content-root").First()
	root.Find("script, style, noscript").Remove()
	candidateImagesSet := make(map[string]bool, len(candidateImages))
	for _, image := range candidateImages {
		if resolved := icResourceURL(baseURL, image); resolved != "" {
			candidateImagesSet[resolved] = true
		}
	}
	validImagesSet := make(map[string]bool, len(validImages))
	for _, image := range validImages {
		if resolved := icResourceURL(baseURL, image); resolved != "" {
			validImagesSet[resolved] = true
		}
	}
	candidateAttachmentsSet := make(map[string]bool, len(candidateAttachments))
	for _, attachment := range candidateAttachments {
		if resolved := icResourceURL(baseURL, attachment.URL); resolved != "" {
			candidateAttachmentsSet[resolved] = true
		}
	}
	validAttachmentsSet := make(map[string]bool, len(validAttachments))
	for _, attachment := range validAttachments {
		if resolved := icResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachmentsSet[resolved] = true
		}
	}
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var resolved string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if raw := strings.TrimSpace(image.AttrOr(attr, "")); raw != "" {
				resolved = icResourceURL(baseURL, raw)
				if resolved != "" {
					break
				}
			}
		}
		if resolved == "" || !candidateImagesSet[resolved] || !validImagesSet[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vsbhref", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			link.RemoveAttr("href")
			return
		}
		resolved := icResourceURL(baseURL, href)
		if resolved != "" && candidateAttachmentsSet[resolved] {
			if !validAttachmentsSet[resolved] {
				link.RemoveAttr("href")
				return
			}
			link.SetAttr("href", resolved)
			return
		}
		if resolved == "" && icLooksLikeAttachment(link) {
			link.RemoveAttr("href")
			return
		}
		if resolved != "" {
			link.SetAttr("href", resolved)
		}
	})
	if html, err := root.Html(); err == nil {
		return html
	}
	return rawHTML
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
	contentSel := icArticleContent(doc)
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("ic: response is not an article page: fixed body is missing")
	}

	article, err := parseBodaArticle(rawHTML, baseURL)
	if err != nil {
		return nil, err
	}
	var headline string
	for _, selector := range []string{".show01 h5", ".show01 h1", ".show01 .title", ".show01 .news-title", ".show_1 h5", ".show_1 h1", ".show_1 .title", ".show_1 .news-title"} {
		if value := cleanTitle(strings.TrimSpace(doc.Find(selector).First().Text())); value != "" {
			headline = value
			break
		}
	}
	if headline == "" {
		// 博达 pages almost always carry a clean pageTitle meta; the current
		// template renders the headline in .show_1 .title, but keep the meta
		// as a fallback the way parseBodaArticle already trusts it.
		headline = cleanTitle(strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")))
	}
	if headline == "" {
		return nil, fmt.Errorf("ic: article headline is missing")
	}
	article.Title = headline
	article.Type = icSectionType(doc)
	article.Source = icArticleSource(doc)
	article.Date, article.PublishedAt = icArticleDateFields(doc)
	article.Content = extractBodaContentText(contentSel)
	icResolveContentResources(contentSel, baseURL)
	if html, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = html
	}
	normalizeArticlePublishedAt(article)
	icExtractAttachments(doc, article, baseURL)
	icDeduplicateResources(article)
	return article, nil
}

func icArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	for _, selector := range []string{
		".show02 .v_news_content",
		".show02 #vsb_content .v_news_content",
		"#vsb_content .v_news_content",
		".article-content .v_news_content",
		".v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return doc.Find(".__ic_missing_content__").First()
}

func icArticleSource(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	meta := doc.Find(".show01, .show_1, .article-meta, .news_source").First()
	var source string
	meta.Find("em, span, [data-source], .source").EachWithBreak(func(_ int, field *goquery.Selection) bool {
		value := strings.TrimSpace(strings.Join(strings.Fields(field.Text()), " "))
		for _, prefix := range []string{"来源：", "来源:"} {
			if strings.HasPrefix(value, prefix) {
				source = strings.TrimSpace(strings.TrimPrefix(value, prefix))
				return false
			}
		}
		return true
	})
	return source
}

func icArticleDateFields(doc *goquery.Document) (string, string) {
	if doc == nil {
		return "", ""
	}
	for _, selector := range []string{
		`meta[name="date"]`, `meta[name="publishdate"]`, `meta[name="publishDate"]`,
		`meta[name="publish_date"]`, `meta[property="article:published_time"]`,
	} {
		meta := doc.Find(selector).First()
		if meta.Length() == 0 {
			continue
		}
		if date, published := icDateFieldsFromValue(meta.AttrOr("content", ""), true); date != "" {
			return date, published
		}
	}

	// The current template renders the meta row as .show_1 .sourse with a
	// bare date (no 发布日期 label) next to an icon, e.g. "2026-09-29".
	if row := doc.Find(".show_1 .sourse").First(); row.Length() > 0 {
		if precise := icPreciseDate(row.Text()); precise != "" {
			if publishedAtHasClock(precise) {
				return precise[:10], precise
			}
			return precise[:10], ""
		}
	}

	meta := doc.Find(".show01, .show_1, .article-meta, .news_source").First()
	for _, selector := range []string{
		"time[datetime]", "[data-date]", ".date", ".time", "[class*='date']", "[class*='time']", "em", "span",
	} {
		var date, published string
		meta.Find(selector).EachWithBreak(func(_ int, field *goquery.Selection) bool {
			trusted := selector == "time[datetime]" || selector == "[data-date]" || strings.Contains(strings.ToLower(field.AttrOr("class", "")), "date") || strings.Contains(strings.ToLower(field.AttrOr("class", "")), "time")
			value := field.AttrOr("datetime", "")
			if value == "" {
				value = field.AttrOr("data-date", "")
			}
			if value == "" {
				value = field.Text()
			}
			date, published = icDateFieldsFromValue(value, trusted)
			return date == ""
		})
		if date != "" {
			return date, published
		}
	}
	return "", ""
}

func icDateFieldsFromValue(raw string, trusted bool) (string, string) {
	raw = strings.Join(strings.Fields(htmlstd.UnescapeString(strings.TrimSpace(raw))), " ")
	if raw == "" {
		return "", ""
	}
	if !trusted {
		lower := strings.ToLower(raw)
		labels := []string{"发布日期", "发布时间", "发表时间", "日期", "时间"}
		value := ""
		for _, label := range labels {
			if index := strings.Index(lower, strings.ToLower(label)); index >= 0 {
				value = strings.TrimSpace(raw[index+len(label):])
				break
			}
		}
		if value == "" {
			return "", ""
		}
		raw = strings.TrimLeft(value, " ：:：")
	}
	precise := icPreciseDate(raw)
	if precise == "" || len(precise) < 10 {
		return "", ""
	}
	if publishedAtHasClock(precise) {
		return precise[:10], precise
	}
	return precise[:10], ""
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
	for _, selector := range []string{"#nav_slide .n_title h1", ".n_title h1", ".position #curType", ".position", ".current", ".nav_class li.active a"} {
		value = cleanTitle(strings.TrimSpace(doc.Find(selector).First().Text()))
		if value != "" {
			break
		}
	}
	if value == "" {
		// The current template's breadcrumb (.local2) only tracks the top
		// column, e.g. 首页 > 新闻中心; use its last segment.
		if crumb := doc.Find(".local2 a").Last(); crumb.Length() > 0 {
			value = cleanTitle(strings.TrimSpace(crumb.Text()))
		}
	}
	switch value {
	case "新闻中心", "NEWS", "NEWS新闻动态", "新闻聚焦":
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
		resolved := icResourceURL(baseURL, href)
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
	for depth, parent := 0, anchor; parent.Length() > 0 && depth < 2; depth, parent = depth+1, parent.Parent() {
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

func icResolveContentResources(content *goquery.Selection, baseURL string) {
	if content == nil || content.Length() == 0 {
		return
	}
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		var resolved string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if raw := strings.TrimSpace(image.AttrOr(attr, "")); raw != "" {
				resolved = bodaResourceURL(baseURL, raw)
				if resolved != "" {
					break
				}
			}
		}
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vsbhref", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			link.RemoveAttr("href")
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		parsed, err := url.Parse(resolved)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
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
