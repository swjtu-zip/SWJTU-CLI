package scraper

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var sjSubPages = []string{
	"index/xwdt", "gggs", "gzcx/cwsj", "gzcx/zxsj", "gzcx/gcsj", "zbjs",
	"zcfg/gjxgfgzd", "zcfg/bwxgfgzd", "zcfg/scsxgfgzd", "zcfg/xxxggzzd",
	"zlxz/cwsjxgzl", "zlxz/gcsjxgzl", "zlxz/zxsjxgzl", "xxyd", "sjsy",
}

const sjMaxResourceBytes int64 = 32 << 20

// sjAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("sj", SiteAdapter{
		SubPages:     sjSubPages,
		ParseList:    parseAdapter_sjList,
		ParseArticle: parseAdapter_sjArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   sjTotalPages,
		FetchArticle: sjFetchArticle,
	})
}

func parseAdapter_sjList(html []byte, baseURL string) ([]model.NewsItem, error) {
	if err := sjResponseError(html); err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("sj: parse list: %w", err)
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
	if pageType != "2" {
		return nil, fmt.Errorf("sj: response is not an article list (pageType %q)", pageType)
	}
	rows := doc.Find(".list-grid > .list-cell, .list-grid > .list-box > .list-col")
	if rows.Length() == 0 {
		return nil, fmt.Errorf("sj: response has no audited list rows")
	}
	typ := strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find(".list-cell-tt, .list-col-tt, a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if !isValidSiteNewsURL(fullURL, baseURL) || seen[fullURL] {
			return
		}
		title := cleanTitle(strings.TrimSpace(anchor.AttrOr("title", "")))
		if title == "" {
			title = cleanTitle(anchor.Text())
		}
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := sjDateValues(row.Find(".list-time, .list-col-time, time, .date, .time").First().Text())
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	return items, nil
}

func parseAdapter_sjArticle(html []byte, baseURL string) (*model.Article, error) {
	if err := sjResponseError(html); err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("sj: parse article: %w", err)
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
	if pageType != "3" {
		return nil, fmt.Errorf("sj: response is not an article (pageType %q)", pageType)
	}
	titleSel := doc.Find(".list-grid .deta-tt").First()
	if titleSel.Length() == 0 {
		titleSel = doc.Find(".deta-tt").First()
	}
	contentSel := doc.Find("#vsb_content > .v_news_content").First()
	if contentSel.Length() == 0 {
		contentSel = doc.Find("#vsb_content .v_news_content").First()
	}
	if titleSel.Length() == 0 || strings.TrimSpace(titleSel.Text()) == "" || contentSel.Length() == 0 || (strings.TrimSpace(contentSel.Text()) == "" && contentSel.Find("img, a, table").Length() == 0) {
		return nil, fmt.Errorf("sj: article headline or body is missing")
	}
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = cleanTitle(strings.Join(strings.Fields(titleSel.Text()), " "))
	article.Type = sjArticleType(doc)
	article.Date, article.PublishedAt = sjDateValues(doc.Find(".deta-fun .deta-time").First().Text())
	article.Content = extractBodaContentText(contentSel)
	if contentHTML, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = contentHTML
	}
	sjAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func sjResponseError(raw []byte) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("sj: parse response: %w", err)
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
	title := strings.ToLower(strings.TrimSpace(doc.Find("title").First().Text()))
	for _, marker := range []string{
		"just a moment", "cloudflare", "access denied", "forbidden", "waf",
		"页面不存在", "您访问的页面不存在", "404 not found", "安全验证", "请求已被阻断",
		"请输入验证码", "验证码下载", "请先登录", "请登录后",
	} {
		if strings.Contains(visible, marker) || strings.Contains(title, marker) {
			return fmt.Errorf("sj: captcha/WAF/error response (%s)", marker)
		}
	}
	return nil
}

func sjArticleType(doc *goquery.Document) string {
	for _, selector := range []string{".list-grid .list-head > p", ".list-grid .navleft-hd p", ".navleft-hd p", ".list-head > p"} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" && value != "正文" && value != "首页" {
			return value
		}
	}
	return ""
}

func sjDateValues(raw string) (string, string) {
	normalized := normalizePublishedAt(raw)
	if len(normalized) < 10 {
		return "", ""
	}
	date := normalized[:10]
	if publishedAtHasClock(normalized) {
		return date, normalized
	}
	return date, ""
}

func sjTotalPages(slug string) (int, error) {
	site := AllSites["sj"]
	if site == nil {
		return 0, fmt.Errorf("sj: site registration is missing")
	}
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	known := false
	for _, route := range sjSubPages {
		if route == slug {
			known = true
			break
		}
	}
	if !known {
		return 0, fmt.Errorf("sj: %q is not an audited article-list route", slug)
	}
	body, err := fetch(reverseBodaPageURL(site.BaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("sj: fetch %s: %w", slug, err)
	}
	if total := parseTotalPages(body); total > 0 {
		return total, nil
	}
	if items, parseErr := parseAdapter_sjList(body, site.BaseURL); parseErr == nil && len(items) > 0 {
		return 1, nil
	}
	return 0, fmt.Errorf("sj: %s has no pagination or article rows", slug)
}

func sjBaseURL() string {
	if site := AllSites["sj"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return "https://sj.swjtu.edu.cn"
}

func sjArticleURL(baseURL, raw string) (string, bool) {
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	path := strings.ToLower(parsed.Path)
	if !strings.HasPrefix(path, "/info/") || !(strings.HasSuffix(path, ".htm") || strings.HasSuffix(path, ".html")) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func sjResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return ""
	}
	lower := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{"captcha", "verify", "verification", "createimage", "getcode", "access-denied", "access_denied", "waf", "login", "placeholder", "transparent", "spacer", "blank"} {
		if strings.Contains(lower, marker) {
			return ""
		}
	}
	parsed.Fragment = ""
	return parsed.String()
}

func sjFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := sjBaseURL()
	fullURL, ok := sjArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("sj: refusing non-local article URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("sj: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_sjArticle(body, baseURL)
	if err != nil {
		return nil, body, err
	}
	candidateImages := append([]string(nil), article.Images...)
	candidateAttachments := append([]model.Attachment(nil), article.Attachments...)
	article.Images = sjVerifyImages(candidateImages, baseURL, fullURL)
	article.Attachments = sjVerifyAttachments(candidateAttachments, baseURL, fullURL)
	article.ContentHTML = sjSanitizeContentHTML(article.ContentHTML, baseURL, article.Images, article.Attachments)
	if root, parseErr := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"sj-content-root\">" + article.ContentHTML + "</div>")); parseErr == nil {
		article.Content = extractBodaContentText(root.Find("#sj-content-root").First())
	}
	return article, body, nil
}

func sjVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := sjResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := sjFetchResource(resolved, baseURL, referer, true)
		if err != nil || !sjResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func sjVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := sjResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := sjFetchResource(resolved, baseURL, referer, false)
		if err != nil || !sjResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = fmt.Sprintf("%d", len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func sjFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	resolved := sjResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("sj: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := http.Client{}
	if defaultClient != nil {
		client = *defaultClient
	}
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL == nil || !strings.EqualFold(next.URL.Hostname(), sjURLHost(baseURL)) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("sj: resource HTTP %d", response.StatusCode)
	}
	if response.ContentLength > sjMaxResourceBytes {
		return nil, contentType, fmt.Errorf("sj: resource exceeds size limit")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, sjMaxResourceBytes+1))
	if err != nil || int64(len(body)) > sjMaxResourceBytes {
		return nil, contentType, fmt.Errorf("sj: resource read/size failure")
	}
	return body, contentType, nil
}

func sjURLHost(raw string) string {
	parsed, _ := url.Parse(raw)
	if parsed == nil {
		return ""
	}
	return parsed.Hostname()
}

func sjResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "" || mediaType == "text/html" || mediaType == "application/xhtml+xml" || mediaType == "application/json" || mediaType == "text/xml" || mediaType == "application/xml" {
		return false
	}
	probe := bytes.ToLower(bytes.TrimSpace(body))
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	for _, marker := range []string{"<!doctype html", "<html", "<head", "<body", "请输入验证码", "验证码下载", "请先登录", "页面不存在", "404 not found", "access denied", "cloudflare", "just a moment", "安全验证", "请求已被阻断"} {
		if bytes.Contains(probe, []byte(marker)) {
			return false
		}
	}
	magic := sjResourceMagic(body)
	if image {
		if !strings.HasPrefix(mediaType, "image/") {
			return false
		}
		switch {
		case strings.Contains(mediaType, "jpeg"):
			return magic == "jpeg"
		case strings.Contains(mediaType, "png"):
			return magic == "png"
		case strings.Contains(mediaType, "gif"):
			return magic == "gif"
		case strings.Contains(mediaType, "webp"):
			return magic == "webp"
		case strings.Contains(mediaType, "svg"):
			return magic == "svg"
		default:
			return magic == "jpeg" || magic == "png" || magic == "gif" || magic == "webp" || magic == "bmp" || magic == "tiff" || magic == "svg"
		}
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf":
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func sjResourceMagic(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x03, 0x04}):
		return "zip"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x05, 0x06}):
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
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'R', 'a', 'r', '!', 0x1a, 0x07}):
		return "rar"
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	}
	probe := bytes.ToLower(bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})))
	if bytes.Contains(probe, []byte("<svg")) {
		return "svg"
	}
	return ""
}

func sjSanitizeContentHTML(rawHTML, baseURL string, validImages []string, validAttachments []model.Attachment) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"sj-content-root\">" + rawHTML + "</div>"))
	if err != nil {
		return rawHTML
	}
	root := doc.Find("#sj-content-root").First()
	root.Find("script, style, noscript").Remove()
	imageSet := make(map[string]bool, len(validImages))
	for _, value := range validImages {
		if resolved := sjResourceURL(baseURL, value); resolved != "" {
			imageSet[resolved] = true
		}
	}
	attachmentSet := make(map[string]bool, len(validAttachments))
	for _, value := range validAttachments {
		if resolved := sjResourceURL(baseURL, value.URL); resolved != "" {
			attachmentSet[resolved] = true
		}
	}
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := sjResourceURL(baseURL, image.AttrOr("src", ""))
		if !imageSet[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := sjResourceURL(baseURL, href)
		if !attachmentSet[resolved] {
			if row := link.Closest("li"); row.Length() > 0 && strings.Contains(row.Text(), "附件") {
				row.Remove()
			} else {
				link.Remove()
			}
			return
		}
		link.SetAttr("href", resolved)
	})
	result, htmlErr := root.Html()
	if htmlErr != nil {
		return rawHTML
	}
	return result
}

func sjAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) || link.Closest(".v_news_content, #vsb_content_501").Length() > 0 {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if !isValidNewsURL(resolved) || strings.Contains(resolved, "/info/") || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
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
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}
