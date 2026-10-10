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

const journalsCanonicalBaseURL = "https://journals.swjtu.edu.cn"

// The current journals.swjtu.edu.cn origin is a static journal directory. It
// exposes no local article archive today, but keep the detail-path guard here
// so a future link cannot silently turn an external journal portal into local
// news before the site is audited again.
var journalsArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.(?:html?|shtml)$`)

const journalsMaxResourceBytes int64 = 32 << 20

var journalsAttachmentExts = []string{
	".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv",
}

// journalsAdapter is intentionally separate from the other unit adapters.
// The live site is a directory of journals, not a 博达 news site: its valid
// links point to xnjdxb.swjtu.edu.cn, Springer, IET, ScienceDirect, CNKI, or
// are placeholders. SubPages therefore remains empty until the origin has a
// real local article list, and the SDK must not synthesise an index feed for
// a homepage that exposes no local article list.
func init() {
	if site := AllSites["journals"]; site != nil {
		site.BaseURL = journalsCanonicalBaseURL
		site.SubPages = nil
		site.NoIndexFeed = true
	}
	registerAdapter("journals", SiteAdapter{
		ParseList:    parseAdapter_journalsList,
		ParseArticle: parseAdapter_journalsArticle,
		FetchArticle: journalsFetchArticle,
	})
}

func parseAdapter_journalsList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if journalsBlockedResponse(raw) {
		return nil, fmt.Errorf("journals: response is an unavailable, login, WAF, or error page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("journals: parse list HTML: %w", err)
	}

	// Do not pass this page through parseBodaNewsList. The directory cards look
	// like ordinary links to a broad parser, while every valid card leaves this
	// host. Only an explicitly local /info/<column>/<article>.htm link can be a
	// local article row.
	items := journalsListItems(doc, baseURL)
	if len(items) == 0 {
		return nil, fmt.Errorf("journals: current origin is a static journal directory; no public local article list exists")
	}
	return items, nil
}

func journalsListItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	if doc == nil {
		return nil
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		articleURL, ok := journalsArticleURL(baseURL, href)
		if !ok || seen[articleURL] {
			return
		}
		title := strings.TrimSpace(link.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(link.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		row := link.Closest("li, tr, .news-item, .list-item, .item, .news-list").First()
		date, publishedAt := journalsDateFromSelection(row)
		if date == "" {
			date, publishedAt = journalsDateFromSelection(link)
		}
		typeName := journalsTypeFromSelection(row)
		seen[articleURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         articleURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typeName,
			ParsedAt:    time.Now(),
		})
	})
	return items
}

func parseAdapter_journalsArticle(raw []byte, baseURL string) (*model.Article, error) {
	if journalsBlockedResponse(raw) {
		return nil, fmt.Errorf("journals: response is an unavailable, login, WAF, or error page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("journals: parse article HTML: %w", err)
	}
	if !journalsArticleDocument(doc) {
		return nil, fmt.Errorf("journals: response is not a public local article page")
	}

	title := journalsDetailTitle(doc)
	content := journalsDetailContent(doc)
	source, date, publishedAt := journalsDetailMetadata(doc)
	article := &model.Article{
		Title:       title,
		Source:      source,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        journalsDetailType(doc),
	}
	article.Content = extractBodaContentText(content)
	article.Images = journalsContentImages(content, baseURL)
	article.Attachments = journalsContentAttachments(content, baseURL)
	if html, htmlErr := content.Html(); htmlErr == nil {
		article.ContentHTML = html
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("journals: public article has no usable content")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func journalsArticleDocument(doc *goquery.Document) bool {
	return doc != nil && journalsDetailTitle(doc) != "" && journalsDetailContent(doc).Length() > 0
}

func journalsDetailTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{"h1.news_tit", ".article-title", ".news-title", "meta[name=pageTitle]"} {
		found := doc.Find(selector).First()
		if found.Length() == 0 {
			continue
		}
		value := strings.TrimSpace(found.Text())
		if strings.HasPrefix(selector, "meta[") {
			value = strings.TrimSpace(found.AttrOr("content", ""))
		}
		if value != "" {
			return cleanTitle(value)
		}
	}
	return ""
}

func journalsDetailContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	for _, selector := range []string{
		"#vsb_content_501 .v_news_content",
		"#vsb_content .v_news_content",
		".v_news_content",
	} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			return found
		}
	}
	return doc.Find(".__journals_missing_content__").First()
}

func journalsDetailMetadata(doc *goquery.Document) (source, date, publishedAt string) {
	if doc == nil {
		return "", "", ""
	}
	meta := doc.Find(".news_source").First()
	meta.Find("span, .source, .date").EachWithBreak(func(_ int, field *goquery.Selection) bool {
		value := strings.Join(strings.Fields(field.Text()), " ")
		switch {
		case strings.HasPrefix(value, "来源：") || strings.HasPrefix(value, "来源:"):
			source = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "来源："), "来源:"))
		case strings.HasPrefix(value, "日期：") || strings.HasPrefix(value, "日期:"):
			date, publishedAt = journalsNormalizeDate(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "日期："), "日期:")))
		}
		return date == ""
	})
	return source, date, publishedAt
}

func journalsDetailType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{`meta[name="articleType"]`, `[data-article-type]`} {
		field := doc.Find(selector).First()
		if field.Length() == 0 {
			continue
		}
		value := strings.TrimSpace(field.AttrOr("content", ""))
		if value == "" {
			value = strings.TrimSpace(field.AttrOr("data-article-type", ""))
		}
		if value != "" {
			return value
		}
	}
	for _, selector := range []string{".breadcrumb a, .position a, .crumb a"} {
		var last string
		doc.Find(selector).Each(func(_ int, field *goquery.Selection) {
			value := strings.TrimSpace(field.Text())
			if value != "" && value != "首页" && value != "正文" && value != journalsDetailTitle(doc) {
				last = value
			}
		})
		if last != "" {
			return last
		}
	}
	return ""
}

// journalsArticleType is kept as a small compatibility helper for callers
// that used the previous adapter's HTML-only type probe.
func journalsArticleType(raw []byte, current string) string {
	if value := strings.TrimSpace(current); value != "" {
		return value
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return ""
	}
	return journalsDetailType(doc)
}

func journalsDateFromSelection(sel *goquery.Selection) (date, publishedAt string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	for _, selector := range []string{".time", ".date", "time", ".news-date", "[class*=time]"} {
		value := strings.TrimSpace(sel.Find(selector).First().Text())
		if value == "" {
			continue
		}
		if date, publishedAt = journalsNormalizeDate(value); date != "" {
			return date, publishedAt
		}
	}
	return "", ""
}

func journalsNormalizeDate(value string) (date, publishedAt string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	publishedAt = normalizePublishedAt(value)
	if publishedAt == "" {
		return "", ""
	}
	if len(publishedAt) >= len("2006-01-02") {
		date = publishedAt[:len("2006-01-02")]
	}
	return date, publishedAt
}

func journalsTypeFromSelection(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	for _, selector := range []string{"[data-type]", ".category", ".type", ".tag"} {
		field := sel.Find(selector).First()
		if field.Length() == 0 {
			continue
		}
		value := strings.TrimSpace(field.AttrOr("data-type", ""))
		if value == "" {
			value = strings.TrimSpace(field.Text())
		}
		if value != "" {
			return strings.Trim(value, "[]【】()（）")
		}
	}
	return ""
}

func journalsContentImages(content *goquery.Selection, baseURL string) []string {
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
		resolved, ok := journalsResourceURL(baseURL, raw)
		if !ok || seen[resolved] {
			return
		}
		seen[resolved] = true
		image.SetAttr("src", resolved)
		images = append(images, resolved)
	})
	return images
}

func journalsContentAttachments(content *goquery.Selection, baseURL string) []model.Attachment {
	attachments := make([]model.Attachment, 0)
	seen := make(map[string]bool)
	if content == nil || content.Length() == 0 {
		return attachments
	}
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(htmlstd.UnescapeString(link.AttrOr("href", "")))
		if !journalsLooksLikeAttachment(link, href) {
			return
		}
		resolved, ok := journalsResourceURL(baseURL, href)
		if !ok || seen[resolved] {
			return
		}
		name := strings.TrimSpace(link.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(link.Text())
		}
		if name == "" {
			name = href
		}
		seen[resolved] = true
		link.SetAttr("href", resolved)
		attachments = append(attachments, model.Attachment{Name: name, URL: resolved})
	})
	return attachments
}

func journalsLooksLikeAttachment(link *goquery.Selection, href string) bool {
	if link == nil || link.Length() == 0 {
		return false
	}
	if strings.TrimSpace(link.AttrOr("download", "")) != "" {
		return true
	}
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, ext := range journalsAttachmentExts {
		if strings.Contains(lower, ext) || strings.HasSuffix(strings.ToLower(strings.TrimSpace(link.Text())), ext) {
			return true
		}
	}
	return false
}

func journalsArticleURL(baseURL, raw string) (string, bool) {
	resolved, ok := journalsLocalURL(baseURL, raw)
	if !ok {
		return "", false
	}
	parsed, err := url.Parse(resolved)
	if err != nil || !journalsArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), true
}

func journalsLocalURL(baseURL, raw string) (string, bool) {
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
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func journalsFetchArticle(articleURL string) (*model.Article, []byte, error) {
	return journalsFetchArticleAt(articleURL, journalsCanonicalBaseURL)
}

func journalsFetchArticleAt(articleURL, baseURL string) (*model.Article, []byte, error) {
	canonical, ok := journalsArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("journals: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := journalsFetchHTML(canonical)
	if err != nil {
		return nil, nil, fmt.Errorf("journals: fetch article %s: %w", canonical, err)
	}
	article, err := parseAdapter_journalsArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("journals: parse article %s: %w", canonical, err)
	}
	candidateImages := append([]string(nil), article.Images...)
	candidateAttachments := append([]model.Attachment(nil), article.Attachments...)
	article.Images = journalsVerifyImages(candidateImages, canonical)
	article.Attachments = journalsVerifyAttachments(candidateAttachments, canonical)
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		validImages[image] = true
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		validAttachments[attachment.URL] = true
	}
	candidateAttachmentURLs := make(map[string]bool, len(candidateAttachments))
	for _, attachment := range candidateAttachments {
		candidateAttachmentURLs[attachment.URL] = true
	}
	article.ContentHTML = journalsSanitizeContentHTML(article.ContentHTML, baseURL, validImages, candidateAttachmentURLs, validAttachments)
	article.Content = journalsContentTextFromHTML(article.ContentHTML)
	return article, body, nil
}

func journalsFetchHTML(rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid article URL %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if scheme := strings.ToLower(next.URL.Scheme); scheme != "http" && scheme != "https" {
			return fmt.Errorf("journals: refusing non-HTTP article redirect")
		}
		if !strings.EqualFold(next.URL.Hostname(), parsed.Hostname()) {
			return fmt.Errorf("journals: refusing cross-site article redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	contentType := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type")))
	mediaType, _, parseErr := mime.ParseMediaType(contentType)
	if parseErr != nil {
		mediaType = strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	}
	if mediaType != "" && mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return nil, fmt.Errorf("unexpected Content-Type %q", contentType)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, journalsMaxResourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > journalsMaxResourceBytes {
		return nil, fmt.Errorf("article exceeds %d bytes", journalsMaxResourceBytes)
	}
	return body, nil
}

func journalsVerifyImages(images []string, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved, ok := journalsResourceURL(journalsVerificationBaseURL(referer), raw)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := journalsFetchResource(resolved, referer, true)
		if err != nil || !journalsResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func journalsVerifyAttachments(attachments []model.Attachment, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved, ok := journalsResourceURL(journalsVerificationBaseURL(referer), attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := journalsFetchResource(resolved, referer, false)
		if err != nil || !journalsResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func journalsFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	resolved, ok := journalsResourceURL(journalsVerificationBaseURL(referer), rawURL)
	if !ok {
		return nil, "", fmt.Errorf("journals: refusing invalid resource %q", rawURL)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return nil, "", fmt.Errorf("journals: refusing non-HTTP resource %q", resolved)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("journals: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0.0.0 Safari/537.36")
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
			return fmt.Errorf("journals: refusing non-HTTP resource redirect")
		}
		if !strings.EqualFold(next.URL.Hostname(), parsed.Hostname()) {
			return fmt.Errorf("journals: refusing cross-site resource redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("journals: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("journals: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, journalsMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("journals: read resource %s: %w", resolved, err)
	}
	if int64(len(body)) > journalsMaxResourceBytes || !journalsResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("journals: resource %s returned invalid Content-Type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func journalsVerificationBaseURL(referer string) string {
	parsed, err := url.Parse(strings.TrimSpace(referer))
	if err == nil && parsed.Scheme != "" && parsed.Hostname() != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return journalsCanonicalBaseURL
}

func journalsResourceURL(baseURL, raw string) (string, bool) {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) || journalsResourceMarked(raw) {
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
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif", "/pixel.gif", "/loading.gif"} {
		if strings.HasSuffix(strings.ToLower(parsed.Path), placeholder) {
			return "", false
		}
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func journalsResourceMarked(raw string) bool {
	value := strings.ToLower(htmlstd.UnescapeString(strings.TrimSpace(raw)))
	for _, marker := range []string{
		"captcha", "verifycode", "verification", "createimage", "getcode", "codevalue",
		"access-denied", "access_denied", "waf", "login", "验证码", "人机验证", "安全验证",
		"placeholder", "spacer.gif", "blank.gif", "transparent.gif", "pixel.gif", "loading.gif",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func journalsResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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

	magic := journalsResourceMagic(body)
	if image {
		return journalsImageTypeMatchesMagic(mediaType, magic)
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

func journalsImageTypeMatchesMagic(mediaType, magic string) bool {
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

func journalsResourceMagic(body []byte) string {
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

func journalsSanitizeContentHTML(rawHTML, baseURL string, validImages, candidateAttachments, validAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="journals-content-root">` + rawHTML + `</div>`))
	if err != nil {
		return ""
	}
	root := doc.Find("#journals-content-root")
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved, ok := journalsResourceURL(baseURL, image.AttrOr("src", ""))
		if !ok || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		raw := link.AttrOr("href", "")
		if !journalsLooksLikeAttachment(link, raw) {
			return
		}
		resolved, ok := journalsResourceURL(baseURL, raw)
		if !ok {
			link.RemoveAttr("href")
			return
		}
		if !candidateAttachments[resolved] {
			return
		}
		if validAttachments[resolved] {
			link.SetAttr("href", resolved)
			return
		}
		link.RemoveAttr("href")
	})
	html, err := root.Html()
	if err != nil {
		return ""
	}
	return html
}

func journalsContentTextFromHTML(rawHTML string) string {
	if strings.TrimSpace(rawHTML) == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="journals-content-root">` + rawHTML + `</div>`))
	if err != nil {
		return ""
	}
	return extractBodaContentText(doc.Find("#journals-content-root"))
}

func journalsArticleShell(raw []byte) bool {
	if journalsBlockedResponse(raw) {
		return false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	return err == nil && journalsArticleDocument(doc)
}

func journalsBlockedResponse(raw []byte) bool {
	page := strings.ToLower(strings.TrimSpace(string(raw)))
	if page == "" {
		return true
	}
	for _, marker := range []string{
		"access denied", "403 forbidden", "404 not found", "404错误", "500 internal server error",
		"page not found", "cloudflare", "just a moment", "request blocked", "请求被拦截",
		"请输入验证码下载附件", "验证码下载附件", "登录后查看", "cas/login", "webexp.swjtu.edu.cn/login",
	} {
		if strings.Contains(page, marker) {
			return true
		}
	}
	return false
}
