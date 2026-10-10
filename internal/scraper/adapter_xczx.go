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
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var xczxSubPages = []string{"tzxw/tzgg", "tzxw/xwdt"}

var xczxFeedTypes = map[string]string{
	"tzxw/tzgg": "通知公告",
	"tzxw/xwdt": "新闻动态",
}

const (
	xczxCanonicalBaseURL = "https://xczx.swjtu.edu.cn"
	xczxMaxResourceBytes = 32 << 20
)

var (
	xczxArticlePathRE    = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	xczxPageCounterRE    = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)
	xczxResourceShellRE  = regexp.MustCompile(`(?is)(?:<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|captcha|验证码|人机验证|安全验证|cloudflare|just a moment|waf|访问过于频繁|请求已被阻断|页面不存在|404\s+not\s+found|您访问的页面未找到)`)
	xczxSplitDateRE      = regexp.MustCompile(`(20\d{2})\s+(\d{1,2})\s*[/.-]\s*(\d{1,2})`)
	xczxAttachmentNameRE = regexp.MustCompile(`(?i)\.(?:docx?|xlsx?|pptx?|pdf|zip|rar|7z|txt|csv)(?:$|[?#])`)
)

func init() {
	registerAdapter("xczx", SiteAdapter{
		SubPages:     xczxSubPages,
		ParseList:    parseAdapter_xczxList,
		ParseArticle: parseAdapter_xczxArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   xczxTotalPages,
		FetchArticle: xczxFetchArticle,
	})
}

func parseAdapter_xczxList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xczx: parse list HTML: %w", err)
	}
	if xczxBlockedPage(raw, doc) {
		return nil, fmt.Errorf("xczx: list response is an error or verification page")
	}
	if xczxIsHomepage(doc) {
		items := xczxHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("xczx: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := doc.Find(".main_conRCb").First()
	if container.Length() == 0 {
		return nil, fmt.Errorf("xczx: response has no main_conRCb list container")
	}
	items := xczxParseRows(container, baseURL, xczxDocumentType(doc))
	if len(items) == 0 && container.Find("a[href*='/info/'], a[href*='info/']").Length() > 0 {
		return nil, fmt.Errorf("xczx: list has article links but no parseable rows")
	}
	return items, nil
}

func xczxIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	if strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", "")) == "1" {
		return true
	}
	// Column pages reuse the informBox class on an empty sidebar decoration,
	// so the class-based fallback only applies when the page has no article
	// list container and a box actually carries local article links.
	if doc.Find(".main_conRCb").Length() > 0 {
		return false
	}
	return doc.Find(".new_inforBox, .informBox").FilterFunction(func(_ int, box *goquery.Selection) bool {
		return box.Find("a[href*='info/']").Length() > 0
	}).Length() > 0
}

func xczxHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".new_inforBox, .informBox").Each(func(_ int, section *goquery.Selection) {
		typ := xczxHeadingType(section.Find("h2, h3").First().Text())
		section.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			fullURL := cleanURL(resolveURL(baseURL, html.UnescapeString(anchor.AttrOr("href", ""))))
			if !xczxArticleURL(fullURL, baseURL) {
				return
			}
			row := anchor.Closest("li")
			if row.Length() == 0 {
				row = anchor.Parent()
			}
			xczxAppendItem(&items, seen, anchor, row, typ, baseURL)
		})
	})
	return items
}

func xczxDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if anchors := doc.Find(".main_titT a"); anchors.Length() > 0 {
		for i := anchors.Length() - 1; i >= 0; i-- {
			if typ := xczxHeadingType(anchors.Eq(i).Text()); typ != "" {
				return typ
			}
		}
	}
	for _, selector := range []string{".main_titT", ".main_tit h1", ".main_tit h2", ".main_tit h3"} {
		if typ := xczxHeadingType(doc.Find(selector).First().Text()); typ != "" {
			return typ
		}
	}
	return ""
}

func xczxHeadingType(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	for _, label := range xczxFeedTypes {
		if value == label {
			return label
		}
		if strings.Contains(value, label) && !strings.Contains(value, "通知新闻") {
			return label
		}
	}
	return ""
}

func xczxParseRows(container *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	container.Find("li").Each(func(_ int, row *goquery.Selection) {
		xczxAppendItem(&items, seen, row.Find("a[href*='info/']").First(), row, typ, baseURL)
	})
	return items
}

func xczxAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if href == "" || !xczxArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find("em, .title, .news-title, h3, h4").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	dateText := xczxRowDate(row)
	date, publishedAt := xczxDateValues(dateText)
	if typ == "" {
		typ = strings.Trim(strings.TrimSpace(row.Find(".tag, .category, .type").First().Text()), "[]【】")
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func xczxDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(value), " ")
	if match := dateTimeRe.FindString(value); match != "" {
		if published := normalizePublishedAt(match); len(published) >= 10 {
			return published[:10], published
		}
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func xczxRowDate(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	for _, selector := range []string{"span", ".date", ".time", "time"} {
		value := strings.Join(strings.Fields(row.Find(selector).First().Text()), " ")
		if date, publishedAt := xczxDateValues(value); date != "" {
			return strings.TrimSpace(date + " " + publishedAt)
		}
	}
	return ""
}

func xczxTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if xczxFeedTypes[slug] == "" {
		return 0, fmt.Errorf("xczx: %q is not an article-list route", slug)
	}
	baseURL := xczxCanonicalBaseURL
	if site := AllSites["xczx"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("xczx: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xczx: parse first page for %s: %w", slug, err)
	}
	if xczxBlockedPage(body, doc) || doc.Find(".main_conRCb").Length() == 0 {
		return 0, fmt.Errorf("xczx: %s is not an article list page", slug)
	}
	if total := xczxPageCount(doc); total > 0 {
		return total, nil
	}
	return 0, fmt.Errorf("xczx: %s has no source-reported pages", slug)
}

func xczxPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find("[id^='fanye'], .p_pages").Text()), " ")
	if match := xczxPageCounterRE.FindStringSubmatch(text); match != nil {
		if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
			return total
		}
	}
	return 0
}

func parseAdapter_xczxArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xczx: parse article HTML: %w", err)
	}
	if xczxBlockedPage(raw, doc) {
		return nil, fmt.Errorf("xczx: article is an error or verification page")
	}
	title := strings.Join(strings.Fields(doc.Find(".main_contit > h2").First().Text()), " ")
	if title == "" {
		title = strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	}
	if title == "" {
		return nil, fmt.Errorf("xczx: article has no fixed title")
	}
	content := xczxArticleContent(doc)
	if content == nil || content.Length() == 0 {
		return nil, fmt.Errorf("xczx: response has no fixed article body")
	}
	date, publishedAt := xczxDateValues(doc.Find(".main_contit > p").First().Text())
	article := &model.Article{
		Title:       title,
		Type:        xczxDocumentType(doc),
		Date:        date,
		PublishedAt: publishedAt,
	}
	xczxApplyContent(article, content, baseURL)
	xczxAppendOutsideAttachments(article, doc, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func xczxBlockedPage(raw []byte, doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"404", "403", "500", "not found", "页面未找到", "页面不存在", "安全验证", "验证码", "访问被拒绝", "access denied", "just a moment"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(strings.Join(strings.Fields(clone.Find("body").Text()), " "))
	for _, marker := range []string{"您访问的页面未找到", "页面未找到", "页面不存在", "请输入验证码", "安全验证", "请求已被阻断", "access denied", "just a moment", "cloudflare"} {
		if strings.Contains(visible, marker) {
			return true
		}
	}
	return false
}

func xczxArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	for _, selector := range []string{
		".main_conDiv#vsb_content > .v_news_content > #vsb_content > .v_news_content",
		".main_conDiv#vsb_content > .v_news_content",
		".main_conDiv#vsb_content .v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return nil
}

func xczxApplyContent(article *model.Article, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}
	article.Content = extractBodaContentText(content)
	clone := content.Clone()
	seenImages := make(map[string]bool)
	clone.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := xczxImageURL(image, baseURL)
		if resolved == "" {
			image.Remove()
			return
		}
		if !seenImages[resolved] {
			seenImages[resolved] = true
			article.Images = append(article.Images, resolved)
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	seenAttachments := make(map[string]bool)
	clone.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if href == "" || bodaIsPseudoResourceHref(href) {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := xczxResourceURL(baseURL, href)
		if resolved == "" {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		link.SetAttr("href", resolved)
		name := xczxAttachmentName(link, resolved)
		if name != "" && !seenAttachments[resolved] {
			seenAttachments[resolved] = true
			article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		}
	})
	if htmlValue, err := clone.Html(); err == nil {
		article.ContentHTML = htmlValue
	}
}

func xczxImageURL(image *goquery.Selection, baseURL string) string {
	if image == nil || image.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		if value := xczxResourceURL(baseURL, image.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func xczxAttachmentName(link *goquery.Selection, resolved string) string {
	if link == nil || link.Length() == 0 {
		return xczxURLFilename(resolved)
	}
	for _, value := range []string{link.AttrOr("download", ""), link.AttrOr("name", ""), link.AttrOr("title", ""), link.Text()} {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" || value == "下载" || value == "点击下载" || value == "附件" {
			continue
		}
		return value
	}
	return xczxURLFilename(resolved)
}

func xczxURLFilename(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return ""
	}
	return html.UnescapeString(parts[len(parts)-1])
}

func xczxAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[xczxResourceURL(baseURL, attachment.URL)] = true
	}
	doc.Find(".main_content a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) {
			return
		}
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		resolved := xczxResourceURL(baseURL, href)
		if href == "" || resolved == "" || xczxArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
			return
		}
		name := xczxAttachmentName(link, resolved)
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func xczxArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && xczxArticlePathRE.MatchString(parsed.Path) &&
		strings.EqualFold(parsed.Scheme, base.Scheme) && strings.EqualFold(parsed.Host, base.Host)
}

func xczxResourceURL(baseURL, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	base, baseErr := url.Parse(baseURL)
	if err != nil || baseErr != nil || parsed.Host == "" || base.Host == "" {
		return ""
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return ""
	}
	if !strings.EqualFold(parsed.Host, base.Host) {
		return ""
	}
	path := strings.ToLower(parsed.Path)
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif", "/loading.gif"} {
		if strings.HasSuffix(path, placeholder) {
			return ""
		}
	}
	return resolved
}

func xczxFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := xczxSiteBaseURL()
	fullURL := cleanURL(resolveURL(baseURL, html.UnescapeString(strings.TrimSpace(articleURL))))
	if !xczxArticleURL(fullURL, baseURL) {
		return nil, nil, fmt.Errorf("xczx: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("xczx: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_xczxArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("xczx: parse article %s: %w", fullURL, err)
	}
	article.Images = xczxVerifyImages(article.Images, baseURL)
	article.Attachments = xczxVerifyAttachments(article.Attachments, baseURL)
	xczxSanitizeContentHTML(article, baseURL)
	return article, body, nil
}

func xczxSiteBaseURL() string {
	if site := AllSites["xczx"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return xczxCanonicalBaseURL
}

func xczxVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved := xczxResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := xczxFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func xczxVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := xczxResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := xczxFetchResource(resolved, baseURL, false)
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

func xczxSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"xczx-content\">" + article.ContentHTML + "</div>"))
	if err != nil {
		return
	}
	verifiedImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		verifiedImages[xczxResourceURL(baseURL, image)] = true
	}
	verifiedAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		verifiedAttachments[xczxResourceURL(baseURL, attachment.URL)] = true
	}
	doc.Find("#xczx-content img").Each(func(_ int, image *goquery.Selection) {
		resolved := xczxImageURL(image, baseURL)
		if resolved == "" || !verifiedImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	doc.Find("#xczx-content a[href]").Each(func(_ int, link *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := xczxResourceURL(baseURL, href)
		if resolved == "" || !verifiedAttachments[resolved] {
			link.Remove()
			return
		}
		link.SetAttr("href", resolved)
	})
	if wrapper := doc.Find("#xczx-content").First(); wrapper.Length() > 0 {
		if htmlValue, err := wrapper.Html(); err == nil {
			article.ContentHTML = htmlValue
		}
	}
}

func xczxFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	resolved := xczxResourceURL(referer, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("xczx: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("xczx: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Referer", strings.TrimRight(referer, "/")+"/")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Host, via[0].URL.Host) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("xczx: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("xczx: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, xczxMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("xczx: read resource %s: %w", resolved, err)
	}
	if len(body) > xczxMaxResourceBytes || !xczxResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("xczx: resource %s returned invalid type, shell, signature, or size", resolved)
	}
	return body, contentType, nil
}

func xczxResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	probe := body
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if xczxResourceShellRE.Match(probe) {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	magic := xczxResourceMagic(body)
	if image {
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
	case xczxAttachmentNameRE.MatchString(lowerName) && strings.HasPrefix(mediaType, "text/"):
		return false
	case strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func xczxIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func xczxResourceMagic(body []byte) string {
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
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 {
		probe := trimmed
		if len(probe) > 8192 {
			probe = probe[:8192]
		}
		lower := strings.ToLower(string(probe))
		if strings.HasPrefix(lower, "<svg") || (strings.HasPrefix(lower, "<?xml") && strings.Contains(lower, "<svg")) {
			return "svg"
		}
	}
	return ""
}
