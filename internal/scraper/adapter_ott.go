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

	"github.com/PuerkitoBio/goquery"
	"swjtu-cli/internal/model"
)

var ottSubPages = []string{
	"xwdt1/rdxw", "xwdt1/gzdt", "tzgong/tzgg", "kjhz/hzdt", "kjhz/xqfb",
	"cgzh/gsgg", "jszyzx/zxdt", "jszyzx/jsjlr", "zcfg/dfzc", "zcfg/gjzc",
	"zcfg/xxzc", "bslc__/xmsb",
}

var ottMonthRE = regexp.MustCompile(`(20\d{2})\s*[./-]\s*(\d{1,2})`)

var (
	ottArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	ottPublicationRE = regexp.MustCompile(`(?:发布日期|发布时间|发表日期|发表时间|日期)\s*[:：]?\s*((?:20\d{2})\s*[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}(?:日)?(?:\s+[0-9]{1,2}:[0-9]{2}(?::[0-9]{2})?)?)`)
	ottSourceRE      = regexp.MustCompile(`来源\s*[:：]\s*(.*?)\s+(?:发布日期|发布时间|发表日期|发表时间|日期|浏览次数)`)
)

const ottCanonicalBaseURL = "https://ott.swjtu.edu.cn"

const ottMaxResourceBytes int64 = 32 << 20

// ottAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("ott", SiteAdapter{
		SubPages:     ottSubPages,
		ParseList:    parseAdapter_ottList,
		ParseArticle: parseAdapter_ottArticle,
		PageURL:      ottPageURL,
		FetchArticle: ottFetchArticle,
	})
}

func ottPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 || (page > 1 && totalPages < 1) || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	physical := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

func parseAdapter_ottList(html []byte, baseURL string) ([]model.NewsItem, error) {
	if items := parseOTTListRows(html, baseURL); items != nil {
		return items, nil
	}
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	filtered := items[:0]
	for _, item := range items {
		// The homepage mixes the institute's own articles with links copied
		// from the university news site. Only same-site entries belong to this
		// adapter; otherwise stale university links become false articles for
		// the technology-transfer feed.
		if isValidSiteNewsURL(item.URL, baseURL) {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("ott: homepage contains no same-site news entries")
	}
	return filtered, nil
}

func parseAdapter_ottArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("ott: parse article HTML: %w", err)
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", "")); pageType != "3" {
		return nil, fmt.Errorf("ott: refusing non-article response (pageType=%q)", pageType)
	}
	titleSel := doc.Find("h1.article-tt, .article-tt").First()
	if titleSel.Length() == 0 || strings.TrimSpace(titleSel.Text()) == "" {
		return nil, fmt.Errorf("ott: article shell has no fixed .article-tt title")
	}
	contentSel := doc.Find(".article-p #vsb_content .v_news_content").First()
	if contentSel.Length() == 0 {
		contentSel = doc.Find("#vsb_content .v_news_content").First()
	}
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("ott: article shell has no .article-p .v_news_content body")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	// OTT has a fixed detail template. Do not let the shared parser infer a
	// date from the prose, where notices routinely contain event/deadline dates.
	article.Title = strings.Join(strings.Fields(titleSel.Text()), " ")
	article.Type = ottSectionType(doc)
	article.Source, article.Date, article.PublishedAt = ottArticleMeta(doc)
	article.Content = extractBodaContentText(contentSel)
	article.Images = ottContentImages(contentSel, baseURL)
	article.Attachments = ottContentAttachments(contentSel, baseURL)
	ottAddOutsideAttachments(article, doc, baseURL)
	if contentHTML, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = contentHTML
	}
	return article, nil
}

func ottArticleMeta(doc *goquery.Document) (source, date, publishedAt string) {
	if doc == nil {
		return "", "", ""
	}
	meta := strings.Join(strings.Fields(doc.Find(".article-sm").First().Text()), " ")
	if match := ottSourceRE.FindStringSubmatch(meta); len(match) > 1 {
		source = strings.TrimSpace(match[1])
	}
	if match := ottPublicationRE.FindStringSubmatch(meta); len(match) > 1 {
		publishedAt = normalizePublishedAt(match[1])
		if len(publishedAt) >= len("2006-01-02") {
			date = publishedAt[:len("2006-01-02")]
		}
	}
	return source, date, publishedAt
}

func ottContentImages(content *goquery.Selection, baseURL string) []string {
	images := make([]string, 0)
	seen := make(map[string]bool)
	if content == nil || content.Length() == 0 {
		return images
	}
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		var raw string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(htmlstd.UnescapeString(image.AttrOr(attr, ""))); value != "" {
				raw = value
				break
			}
		}
		resolved, ok := ottResourceURL(baseURL, raw)
		if !ok || seen[resolved] {
			image.Remove()
			return
		}
		seen[resolved] = true
		images = append(images, resolved)
		image.SetAttr("src", resolved)
	})
	return images
}

func ottContentAttachments(content *goquery.Selection, baseURL string) []model.Attachment {
	attachments := make([]model.Attachment, 0)
	seen := make(map[string]bool)
	if content == nil || content.Length() == 0 {
		return attachments
	}
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		ottAppendAttachment(&attachments, seen, link, baseURL)
	})
	return attachments
}

func ottAddOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find(`form[name="_newscontent_fromname"] a[href]`).Each(func(_ int, link *goquery.Selection) {
		if link.Closest(".v_news_content").Length() > 0 {
			return
		}
		ottAppendAttachment(&article.Attachments, seen, link, baseURL)
	})
}

func ottAppendAttachment(attachments *[]model.Attachment, seen map[string]bool, link *goquery.Selection, baseURL string) {
	if attachments == nil || link == nil || link.Length() == 0 {
		return
	}
	href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
	if !bodaLooksLikeAttachmentLink(link, href) {
		return
	}
	resolved, ok := ottResourceURL(baseURL, href)
	if !ok || seen[resolved] {
		return
	}
	name := strings.Join(strings.Fields(link.AttrOr("download", "")), " ")
	if name == "" {
		name = strings.Join(strings.Fields(link.Text()), " ")
	}
	if name == "" {
		name = resolved
	}
	seen[resolved] = true
	*attachments = append(*attachments, model.Attachment{Name: name, URL: resolved})
	link.SetAttr("href", resolved)
}

func ottFetchArticle(articleURL string) (*model.Article, []byte, error) {
	canonical, ok := ottArticleURL(ottCanonicalBaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("ott: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonical)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_ottArticle(body, ottCanonicalBaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("ott article %s: %w", canonical, err)
	}
	candidates := append([]model.Attachment(nil), article.Attachments...)
	article.Images = ottVerifyImages(article.Images, canonical)
	article.Attachments = ottVerifyAttachments(article.Attachments, canonical)
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		validImages[image] = true
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		validAttachments[attachment.URL] = true
	}
	candidateAttachments := make(map[string]bool, len(candidates))
	for _, attachment := range candidates {
		candidateAttachments[attachment.URL] = true
	}
	article.ContentHTML = ottSanitizeContentHTML(article.ContentHTML, ottCanonicalBaseURL, validImages, candidateAttachments, validAttachments)
	return article, body, nil
}

func ottArticleURL(baseURL, raw string) (string, bool) {
	resolved, ok := ottLocalURL(baseURL, raw)
	if !ok {
		return "", false
	}
	parsed, err := url.Parse(resolved)
	if err != nil || !ottArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), true
}

func ottResourceURL(baseURL, raw string) (string, bool) {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return "", false
	}
	resolved, ok := ottLocalURL(baseURL, raw)
	if !ok {
		return "", false
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return "", false
	}
	pathQuery := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{"/captcha", "verifycode", "verification", "createimage", "getcode", "login"} {
		if strings.Contains(pathQuery, marker) {
			return "", false
		}
	}
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif"} {
		if strings.HasSuffix(strings.ToLower(parsed.Path), placeholder) {
			return "", false
		}
	}
	return resolved, true
}

func ottLocalURL(baseURL, raw string) (string, bool) {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return "", false
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func ottVerifyImages(images []string, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	baseURL := ottVerificationBaseURL(referer)
	for _, raw := range images {
		resolved, ok := ottResourceURL(baseURL, raw)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := ottFetchResource(resolved, referer, true)
		if err != nil || !ottResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func ottVerifyAttachments(attachments []model.Attachment, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	baseURL := ottVerificationBaseURL(referer)
	for _, attachment := range attachments {
		resolved, ok := ottResourceURL(baseURL, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := ottFetchResource(resolved, referer, false)
		if err != nil || !ottResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func ottFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	resolved, ok := ottResourceURL(ottVerificationBaseURL(referer), rawURL)
	if !ok {
		return nil, "", fmt.Errorf("ott: refusing invalid resource %q", rawURL)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return nil, "", fmt.Errorf("ott: refusing non-HTTP resource %q", resolved)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("ott: create resource request: %w", err)
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
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if scheme := strings.ToLower(next.URL.Scheme); scheme != "http" && scheme != "https" {
			return fmt.Errorf("ott: refusing non-HTTP resource redirect")
		}
		if !strings.EqualFold(next.URL.Hostname(), parsed.Hostname()) {
			return fmt.Errorf("ott: refusing cross-site resource redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("ott: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("ott: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, ottMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("ott: read resource %s: %w", resolved, err)
	}
	if int64(len(body)) > ottMaxResourceBytes || !ottResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("ott: resource %s returned invalid Content-Type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func ottVerificationBaseURL(referer string) string {
	parsed, err := url.Parse(strings.TrimSpace(referer))
	if err == nil && parsed.Scheme != "" && parsed.Hostname() != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return ottCanonicalBaseURL
}

func ottResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil && parsed != "" {
		mediaType = strings.ToLower(parsed)
	}
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
	textProbe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(probe))), "\ufeff")
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "请输入验证码", "验证码下载附件",
		"captcha", "请先登录", "请登录后", "登录页面", "页面不存在", "access denied",
		"just a moment", "cloudflare", "请求已被阻断", "安全威胁", "系统错误",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	magic := ottResourceMagic(body)
	if image {
		return ottImageTypeMatchesMagic(mediaType, magic)
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

func ottImageTypeMatchesMagic(mediaType, magic string) bool {
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

func ottResourceMagic(body []byte) string {
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
		lower := bytes.ToLower(probe)
		if bytes.Contains(lower, []byte("<svg")) {
			return "svg"
		}
	}
	return ""
}

func ottSanitizeContentHTML(rawHTML, baseURL string, validImages, candidateAttachments, validAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="ott-content-root">` + rawHTML + `</div>`))
	if err != nil {
		return rawHTML
	}
	root := doc.Find("#ott-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var resolved string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if raw := htmlstd.UnescapeString(strings.TrimSpace(image.AttrOr(attr, ""))); raw != "" {
				if candidate, ok := ottResourceURL(baseURL, raw); ok {
					resolved = candidate
					break
				}
			}
		}
		if resolved == "" || !validImages[resolved] {
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
		resolved, ok := ottResourceURL(baseURL, href)
		if !ok || !candidateAttachments[resolved] {
			return
		}
		if !validAttachments[resolved] {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	result, err := root.Html()
	if err != nil {
		return rawHTML
	}
	return result
}

// parseOTTListRows handles the list template used by the article-bearing OTT
// sections. Returning nil means the response was another template and lets
// the conservative shared parser try its own scoped patterns.
func parseOTTListRows(raw []byte, baseURL string) []model.NewsItem {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil
	}
	rows := doc.Find(".lb-list > li")
	if rows.Length() == 0 {
		return nil
	}
	typ := ottSectionType(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool)
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL := resolveURL(baseURL, anchor.AttrOr("href", ""))
		if fullURL == "" || isExternalURL(fullURL, baseURL) || !isValidNewsURL(fullURL) || seen[fullURL] {
			return
		}
		title := cleanTitle(row.Find(".lb-list-tt").First().Text())
		if title == "" {
			title = cleanTitle(anchor.Text())
		}
		if !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := ottListDate(row.Find(".lb-list-date"))
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	return items
}

func ottSectionType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-banner-tt", ".list-left-tt"} {
		if value := ottSectionLabel(doc.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func ottSectionLabel(raw string) string {
	value := strings.Join(strings.Fields(raw), " ")
	if value == "" {
		return ""
	}
	words := strings.Fields(value)
	if len(words)%2 == 0 {
		half := len(words) / 2
		if strings.Join(words[:half], " ") == strings.Join(words[half:], " ") {
			return strings.Join(words[:half], " ")
		}
	}
	return value
}

func ottListDate(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	day := strings.TrimSpace(sel.Clone().Children().Remove().End().Text())
	if day == "" {
		day = strings.TrimSpace(sel.Text())
	}
	monthText := strings.Join(strings.Fields(sel.Find("span").First().Text()), " ")
	match := ottMonthRE.FindStringSubmatch(monthText)
	if match == nil {
		return "", ""
	}
	dayMatch := regexp.MustCompile(`\d{1,2}`).FindString(day)
	if dayMatch == "" {
		return "", ""
	}
	date := normalizeDateParts(match[1], match[2], dayMatch)
	return date, normalizePublishedAt(date)
}
