package scraper

import (
	"bytes"
	"fmt"
	htmlstd "html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const fdcBaseURL = "https://fdc.swjtu.edu.cn"

const fdcMaxResourceBytes = 32 << 20

// These are the article-list routes exposed by the live navigation.  The
// bmjj pages and sdjd/jbxz are static information pages, while the homepage,
// carousel, 专题专栏 and 相关链接 are not list feeds.
var fdcSubPages = []string{
	"tzgg",
	"xwdt",
	"llxx",
	"sdby",
	"sdby/rybz",
	"sdby/yrgs",
	"gzzd",
	"gzzd/sjwj",
	"gzzd/xxwj",
	"sdjd",
	"sdjd/dxal",
}

var (
	fdcArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	fdcPageCounterRE = regexp.MustCompile(`共\s*([0-9]+)\s*条\s*([0-9]+)\s*/\s*([0-9]+)`)
	fdcMetaLabelRE   = regexp.MustCompile(`(信息来源|来源|作者|编辑|责编|摄影|日期|发布时间|文章类型|信息类型|类型|类别|分类|点击数)\s*[:：]\s*`)
	fdcAttachmentRE  = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf|jpe?g|png|gif)(?:$|[?#])`)
)

// fdcAdapter keeps this site's homepage modules, list shell, reverse static
// pagination and article shell validation independent from other 博达 sites.
func init() {
	if site := AllSites["fdc"]; site != nil {
		site.BaseURL = fdcBaseURL
	}
	registerAdapter("fdc", SiteAdapter{
		SubPages:     fdcSubPages,
		ParseList:    parseAdapter_fdcList,
		ParseArticle: parseAdapter_fdcArticle,
		PageURL:      fdcPageURL,
		ListPage:     fdcListPage,
		TotalPages:   fdcTotalPages,
		FetchArticle: fdcFetchArticle,
	})
}

// fdcPageURL maps logical pages (1 = newest) to FDC's generated files:
// slug.htm, slug/{total-1}.htm, ..., slug/1.htm.
func fdcPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if slug == "" {
		slug = "index"
	}
	if baseURL == "" || page < 1 {
		return ""
	}
	if page == 1 {
		return baseURL + "/" + slug + extension
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	generatedPage := totalPages - page + 1
	if generatedPage < 1 {
		return ""
	}
	return baseURL + "/" + slug + "/" + strconv.Itoa(generatedPage) + extension
}

func parseAdapter_fdcList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse fdc list: %w", err)
	}
	if fdcLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("fdc list is an error or verification page")
	}

	if fdcIsHomepage(doc) {
		items := parseFdcHomepage(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("fdc homepage contains no article entries")
		}
		return items, nil
	}

	content := fdcListContent(doc)
	rows := content.Find("dl")
	_, _, _, hasPagination := fdcPaginationFromDocument(doc)
	if content.Length() == 0 || (rows.Length() == 0 && !hasPagination) {
		return nil, fmt.Errorf("fdc page is not a supported article list")
	}
	if rows.Length() == 0 {
		// sdby.htm and sdby/yrgs.htm currently expose a real 0/0 list shell.
		// Preserve that as a legal empty feed, not as a static-page parse.
		return []model.NewsItem{}, nil
	}

	items := parseFdcRows(rows, baseURL, fdcListType(doc))
	if len(items) == 0 {
		// A recognized list can contain only external entries (sdby/rybz does
		// so today).  External links are intentionally not site feed items.
		return []model.NewsItem{}, nil
	}
	return items, nil
}

func parseFdcRows(rows *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("h4 a[href]").First()
		if anchor.Length() == 0 {
			anchor = row.Find("a[href]").First()
		}
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := fdcArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}

		title := anchor.AttrOr("title", "")
		if strings.TrimSpace(title) == "" {
			title = anchor.Text()
		}
		title = cleanTitle(strings.TrimSpace(title))
		if !fdcIsArticleTitle(title) {
			return
		}

		date, publishedAt := fdcDateValues(row.Find("em, time, .date, .time").First().Text())
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	})
	return items
}

// parseFdcHomepage scopes extraction to the three .inform/news-list panels.
// It deliberately does not inspect the carousel, mobile navigation, empty
// 专题专栏 panel or external 相关链接 panel.
func parseFdcHomepage(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	sections := doc.Find("main.index-main .inform")
	if sections.Length() == 0 {
		sections = doc.Find(".index-main .inform")
	}
	sections.Each(func(_ int, section *goquery.Selection) {
		typ := strings.Join(strings.Fields(section.Find(".caption-title").First().Text()), " ")
		if typ == "" {
			return
		}
		section.Find("ul.news-list > li").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			fullURL, ok := fdcArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok || seen[fullURL] {
				return
			}
			title := anchor.AttrOr("title", "")
			if strings.TrimSpace(title) == "" {
				title = row.Find(".content").First().Text()
			}
			if strings.TrimSpace(title) == "" {
				title = anchor.Text()
			}
			title = cleanTitle(strings.TrimSpace(title))
			if !fdcIsArticleTitle(title) {
				return
			}
			date, publishedAt := fdcDateValues(row.Find("time, em, .date").First().Text())
			seen[fullURL] = true
			items = append(items, model.NewsItem{
				Title:       title,
				URL:         fullURL,
				Date:        date,
				PublishedAt: publishedAt,
				Type:        typ,
				ParsedAt:    time.Now(),
			})
		})
	})
	return items
}

func parseAdapter_fdcArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse fdc article: %w", err)
	}
	if fdcLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("fdc article is an error or verification page")
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("fdc page is not an article (pageType %q)", pageType)
	}

	scope := doc.Find(".main_right .content .article").First()
	if scope.Length() == 0 {
		scope = doc.Find(".main_right .article, .article").First()
	}
	if scope.Length() == 0 {
		return nil, fmt.Errorf("fdc article has no article container")
	}
	content := scope.Find(".art_con .v_news_content").First()
	if content.Length() == 0 {
		content = scope.Find("[id^='vsb_content']").Find(".v_news_content").First()
	}
	if content.Length() == 0 {
		return nil, fmt.Errorf("fdc article has no content container")
	}
	if strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a").Length() == 0 {
		return nil, fmt.Errorf("fdc article has empty content")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	// Do not let the generic parser's broad fallbacks promote a date or type
	// found elsewhere in the document. FDC exposes both in fixed shell nodes.
	article.Date = ""
	article.PublishedAt = ""
	article.Type = ""
	article.Title = strings.Join(strings.Fields(scope.ChildrenFiltered("h1").First().Text()), " ")
	if article.Title == "" {
		return nil, fmt.Errorf("fdc article has no title")
	}
	if typ := fdcCurrentType(doc); typ != "" {
		article.Type = typ
	}

	if source := scope.Find(".source, .news_source").First(); source.Length() > 0 {
		fields := fdcMetaFields(flattenedText(source))
		article.Source = fields["来源"]
		if article.Source == "" {
			article.Source = fields["信息来源"]
		}
		article.Author = fields["作者"]
		article.Editor = fields["编辑"]
		if article.Editor == "" {
			article.Editor = fields["责编"]
		}
		article.Photographer = fields["摄影"]
		dateValue := fields["日期"]
		if dateValue == "" {
			dateValue = fields["发布时间"]
		}
		if date, publishedAt := fdcDateValues(dateValue); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		}
	}

	// FDC's CMS puts its download.jsp list after .v_news_content.  The
	// shared parser intentionally scopes body resources more narrowly, so
	// append only explicit file/download links inside this article shell.
	appendFdcAttachments(article, scope, baseURL)
	fdcNormalizeArticlePublishedAt(article)
	return article, nil
}

func fdcIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	return pageType == "1" || doc.Find("main.index-main, .index-main, .news-swiper").Length() > 0
}

func fdcListContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	return doc.Find(".main_right .content").First()
}

func fdcListType(doc *goquery.Document) string {
	return fdcCurrentType(doc)
}

func fdcCurrentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	typ := strings.Join(strings.Fields(doc.Find(".main_right .current h3").First().Text()), " ")
	if typ == "首页" || typ == "正文" {
		return ""
	}
	return typ
}

func fdcIsArticleTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return false
	}
	for _, label := range []string{"首页", "更多", "MORE +", "通知公告", "新闻动态", "理论学习", "师德榜样", "规章制度", "师德监督", "查看详情"} {
		if title == label {
			return false
		}
	}
	return true
}

func fdcArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !fdcArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	return fullURL, true
}

func fdcDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(raw), " ")
	if raw == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(raw); len(normalized) >= 10 {
		date := normalized[:10]
		if publishedAtHasClock(normalized) {
			return date, normalized
		}
		return date, ""
	}
	return "", ""
}

// fdcNormalizeArticlePublishedAt deliberately does not turn a date-only
// source node into a PublishedAt timestamp.  The shared helper is useful for
// older adapters, but its date-only fallback is too permissive for this site's
// fixed .source node.
func fdcNormalizeArticlePublishedAt(article *model.Article) {
	if article == nil {
		return
	}
	if normalized := normalizePublishedAt(article.Date); len(normalized) >= 10 {
		article.Date = normalized[:10]
	}
	if normalized := normalizePublishedAt(article.PublishedAt); publishedAtHasClock(normalized) {
		article.PublishedAt = normalized
	} else {
		article.PublishedAt = ""
	}
}

func fdcMetaFields(text string) map[string]string {
	text = strings.Join(strings.Fields(text), " ")
	fields := make(map[string]string)
	matches := fdcMetaLabelRE.FindAllStringSubmatchIndex(text, -1)
	for i, match := range matches {
		if len(match) < 4 {
			continue
		}
		label := text[match[2]:match[3]]
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		fields[label] = strings.TrimSpace(text[match[1]:end])
	}
	return fields
}

func appendFdcAttachments(article *model.Article, scope *goquery.Selection, baseURL string) {
	if article == nil || scope == nil || scope.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := htmlstd.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
		if fdcIsPseudoResourceHref(href) || !fdcLooksLikeAttachmentLink(anchor, href) {
			return
		}
		resolved := fdcResourceURL(baseURL, href)
		if resolved == "" {
			return
		}
		if seen[resolved] {
			return
		}
		name := fdcAttachmentName(anchor, resolved)
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func fdcLooksLikeAttachmentLink(anchor *goquery.Selection, href string) bool {
	if anchor == nil || anchor.Length() == 0 || href == "" {
		return false
	}
	href = htmlstd.UnescapeString(strings.TrimSpace(href))
	if fdcIsPseudoResourceHref(href) {
		return false
	}
	lowerHref := strings.ToLower(href)
	if strings.Contains(lowerHref, "download.jsp") || strings.Contains(lowerHref, "downloadattachurl") {
		return true
	}
	if _, ok := anchor.Attr("download"); ok {
		return true
	}
	if !fdcAttachmentRE.MatchString(href) {
		return false
	}
	name := strings.Join(strings.Fields(htmlstd.UnescapeString(anchor.Text())), " ")
	if strings.HasSuffix(strings.ToLower(href), ".jpg") || strings.HasSuffix(strings.ToLower(href), ".jpeg") || strings.HasSuffix(strings.ToLower(href), ".png") || strings.HasSuffix(strings.ToLower(href), ".gif") {
		if name == "" && anchor.Find("img").Length() > 0 {
			return false
		}
		return strings.Contains(name, "附件") || fdcAttachmentRE.MatchString(name)
	}
	return true
}

func fdcIsPseudoResourceHref(href string) bool {
	href = strings.ToLower(strings.TrimSpace(htmlstd.UnescapeString(href)))
	if bodaIsPseudoResourceHref(href) {
		return true
	}
	return href == "vurl" || href == "vsbhref" || strings.HasPrefix(href, "vsb:")
}

func fdcAttachmentName(anchor *goquery.Selection, resolved string) string {
	for _, attribute := range []string{"download", "data-filename", "data-name", "name"} {
		if value := strings.TrimSpace(anchor.AttrOr(attribute, "")); value != "" {
			return strings.Join(strings.Fields(htmlstd.UnescapeString(value)), " ")
		}
	}
	if value := strings.Join(strings.Fields(htmlstd.UnescapeString(anchor.Text())), " "); value != "" {
		return value
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return ""
	}
	name := path.Base(parsed.Path)
	if name == "." || name == "/" || name == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	return name
}

// fdcFetchArticle is the live detail path. FDC's download.jsp has been
// observed returning HTTP 200 HTML verification pages, so every same-origin
// image and attachment is checked for status, media type, and file signature
// before it is exposed to callers.
func fdcFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := fdcSiteBaseURL()
	canonicalURL, ok := fdcArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("fdc: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonicalURL)
	if err != nil {
		return nil, nil, fmt.Errorf("fdc: fetch article %s: %w", canonicalURL, err)
	}
	article, err := parseAdapter_fdcArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("fdc: parse article %s: %w", canonicalURL, err)
	}

	images := append([]string{}, article.Images...)
	images = append(images, fdcContentImageCandidates(article.ContentHTML, baseURL)...)
	article.Images = fdcVerifyImages(images, canonicalURL)
	article.Attachments = fdcVerifyAttachments(article.Attachments, canonicalURL)
	article.ContentHTML = fdcSanitizeContentHTML(article.ContentHTML, canonicalURL, article.Images, article.Attachments)
	return article, body, nil
}

func fdcVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := fdcResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		seen[resolved] = true
		if _, _, err := fdcFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		verified = append(verified, resolved)
	}
	return verified
}

func fdcVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := fdcResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := fdcFetchResource(resolved, baseURL, false)
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

func fdcFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := fdcResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("fdc: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("fdc: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Referer", baseURL)
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Host, via[len(via)-1].URL.Host) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fdc: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode != http.StatusOK {
		return nil, contentType, fmt.Errorf("fdc: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	if response.ContentLength > fdcMaxResourceBytes {
		return nil, contentType, fmt.Errorf("fdc: resource %s exceeds %d bytes", resolved, fdcMaxResourceBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, fdcMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("fdc: read resource %s: %w", resolved, err)
	}
	if len(body) > fdcMaxResourceBytes || !fdcResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("fdc: resource %s returned invalid Content-Type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func fdcResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" || mediaType == "text/html" || mediaType == "application/xhtml+xml" || strings.Contains(mediaType, "json") || strings.HasSuffix(mediaType, "+xml") || mediaType == "application/xml" {
		return false
	}
	if fdcResourceLooksLikeShell(body) {
		return false
	}
	magic := fdcResourceMagic(body)
	if image {
		if !strings.HasPrefix(mediaType, "image/") || !fdcIsImageMagic(magic) {
			return false
		}
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
			return true
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
	case strings.Contains(lowerName, ".rtf") || mediaType == "application/rtf":
		return bytes.HasPrefix(bytes.TrimSpace(body), []byte(`{\rtf`))
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return !bytes.Contains(body, []byte{0})
	default:
		return magic != ""
	}
}

func fdcResourceLooksLikeShell(body []byte) bool {
	probe := bytes.TrimSpace(body)
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	lower := strings.ToLower(string(probe))
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
		return true
	}
	for _, marker := range []string{
		"请输入验证码下载附件", "请输入验证码", "验证码下载附件", "页面不存在", "access denied",
		"request blocked", "请求已被阻断", "网站防火墙", "安全验证", "请先登录", "登录页面",
		"cloudflare", "just a moment", "404 not found",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func fdcResourceMagic(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x03, 0x04}):
		return "zip"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x05, 0x06}):
		return "zip"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x07, 0x08}):
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
		probe := strings.ToLower(string(trimmed))
		if strings.HasPrefix(probe, "<svg") || (strings.HasPrefix(probe, "<?xml") && strings.Contains(probe, "<svg")) {
			return "svg"
		}
	}
	return ""
}

func fdcIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

// fdcResourceURL is intentionally same-origin. Ordinary external article
// hyperlinks are handled by fdcContentLinkURL and are never downloaded.
func fdcResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || fdcIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	base, baseErr := url.Parse(baseURL)
	if err != nil || baseErr != nil || parsed.Host == "" || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"/captcha", "/verify", "/validate", "/login", "/signin", "/placeholder",
		"/transparent", "/spacer", "/blank", "/loading", "/pixel.gif", "createimage",
		"getcode", "access-denied", "request-blocked", "/waf",
	} {
		if strings.Contains(resourceText, marker) {
			return ""
		}
	}
	parsed.Fragment = ""
	return parsed.String()
}

func fdcContentImageCandidates(raw, baseURL string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"fdc-content-root\">" + raw + "</div>"))
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	images := make([]string, 0)
	doc.Find("#fdc-content-root img").Each(func(_ int, image *goquery.Selection) {
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			resolved := fdcResourceURL(baseURL, image.AttrOr(attr, ""))
			if resolved == "" {
				continue
			}
			if !seen[resolved] {
				seen[resolved] = true
				images = append(images, resolved)
			}
			break
		}
	})
	return images
}

func fdcContentLinkURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "#") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") {
		return raw
	}
	if fdcIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	return resolved
}

func fdcSanitizeContentHTML(raw, baseURL string, images []string, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	imageSet := make(map[string]bool, len(images))
	for _, image := range images {
		if resolved := fdcResourceURL(baseURL, image); resolved != "" {
			imageSet[resolved] = true
		}
	}
	attachmentSet := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		if resolved := fdcResourceURL(baseURL, attachment.URL); resolved != "" {
			attachmentSet[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"fdc-content-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	root := doc.Find("#fdc-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			candidate := fdcResourceURL(baseURL, image.AttrOr(attr, ""))
			if candidate != "" && imageSet[candidate] {
				resolved = candidate
				break
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
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if href == "" {
			return
		}
		if fdcIsPseudoResourceHref(href) {
			lower := strings.ToLower(href)
			if !strings.HasPrefix(lower, "#") && !strings.HasPrefix(lower, "mailto:") && !strings.HasPrefix(lower, "tel:") {
				link.RemoveAttr("href")
			}
			return
		}
		if fdcLooksLikeAttachmentLink(link, href) {
			resolved := fdcResourceURL(baseURL, href)
			if resolved == "" || !attachmentSet[resolved] {
				link.Remove()
				return
			}
			link.SetAttr("href", resolved)
			return
		}
		if resolved := fdcContentLinkURL(baseURL, href); resolved != "" {
			link.SetAttr("href", resolved)
		} else {
			link.RemoveAttr("href")
		}
	})
	rendered, err := root.Html()
	if err != nil {
		return ""
	}
	return rendered
}

func fdcPaginationFromDocument(doc *goquery.Document) (totalItems, currentPage, totalPages int, ok bool) {
	if doc == nil {
		return 0, 0, 0, false
	}
	values := []string{doc.Find("[id^='fanye']").Text()}
	if values[0] == "" {
		values = append(values, doc.Find(".main_right").Text())
	}
	for _, value := range values {
		if match := fdcPageCounterRE.FindStringSubmatch(strings.Join(strings.Fields(value), " ")); match != nil {
			totalItems, _ = strconv.Atoi(match[1])
			currentPage, _ = strconv.Atoi(match[2])
			totalPages, _ = strconv.Atoi(match[3])
			return totalItems, currentPage, totalPages, true
		}
	}
	return 0, 0, 0, false
}

func fdcTotalPagesFromHTML(html []byte) int {
	_, _, totalPages, ok := fdcPaginationFromHTML(html)
	if !ok {
		return 0
	}
	return totalPages
}

func fdcPaginationFromHTML(html []byte) (totalItems, currentPage, totalPages int, ok bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0, 0, 0, false
	}
	return fdcPaginationFromDocument(doc)
}

func fdcSiteBaseURL() string {
	if site := AllSites["fdc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return fdcBaseURL
}

// fdcListPage owns the GET-and-parse path because the generated filenames are
// reverse-numbered.  It also checks the logical page echoed by the source;
// otherwise a WAF/SPA fallback can make every requested page look successful.
func fdcListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("fdc: list page must be positive: %d", page)
	}
	baseURL := fdcSiteBaseURL()
	firstURL := fdcPageURL(baseURL, slug, 1, 0, ".htm")
	if firstURL == "" {
		return nil, 0, fmt.Errorf("fdc: no first-page URL for %q", slug)
	}
	firstBody, err := fdcFetchListPage(firstURL)
	if err != nil {
		return nil, 0, fmt.Errorf("fdc: read first page for %s: %w", slug, err)
	}
	firstItems, err := parseAdapter_fdcList(firstBody, baseURL)
	if err != nil {
		return nil, 0, err
	}

	if strings.EqualFold(strings.Trim(slug, "/"), "index") {
		if page != 1 {
			return nil, 1, fmt.Errorf("fdc: homepage has no page %d", page)
		}
		return firstItems, 1, nil
	}

	_, firstCurrent, totalPages, hasPagination := fdcPaginationFromHTML(firstBody)
	if !hasPagination {
		totalPages = 1
	} else if totalPages == 0 {
		if page != 1 {
			return nil, 0, fmt.Errorf("fdc: %s is an explicit empty list; page %d is out of range", slug, page)
		}
		if firstCurrent != 0 {
			return nil, 0, fmt.Errorf("fdc: %s reported invalid empty-list page %d", slug, firstCurrent)
		}
		return firstItems, 0, nil
	} else if firstCurrent != 1 {
		return nil, totalPages, fmt.Errorf("fdc: %s first page echoed %d/%d", slug, firstCurrent, totalPages)
	}

	if page == 1 {
		return firstItems, totalPages, nil
	}
	if page > totalPages {
		return nil, totalPages, fmt.Errorf("fdc: %s page %d is out of range 1..%d", slug, page, totalPages)
	}

	pageURL := fdcPageURL(baseURL, slug, page, totalPages, ".htm")
	body, err := fdcFetchListPage(pageURL)
	if err != nil {
		return nil, totalPages, fmt.Errorf("fdc: read %s page %d: %w", slug, page, err)
	}
	items, err := parseAdapter_fdcList(body, baseURL)
	if err != nil {
		return nil, totalPages, err
	}
	_, currentPage, reportedTotalPages, hasPagination := fdcPaginationFromHTML(body)
	if hasPagination {
		if currentPage != page || reportedTotalPages != totalPages {
			return nil, totalPages, fmt.Errorf("fdc: %s requested logical page %d, source echoed %d/%d", slug, page, currentPage, reportedTotalPages)
		}
	}
	return items, totalPages, nil
}

func fdcTotalPages(slug string) (int, error) {
	_, totalPages, err := fdcListPage(slug, 1)
	return totalPages, err
}

func fdcPaginationIsPresent(doc *goquery.Document) bool {
	_, _, _, ok := fdcPaginationFromDocument(doc)
	return ok
}

func fdcFetchListPage(pageURL string) ([]byte, error) {
	body, err := fetch(pageURL)
	if err == nil {
		return body, nil
	}
	if strings.HasSuffix(strings.ToLower(pageURL), ".htm") {
		if fallback, fallbackErr := fetch(strings.TrimSuffix(pageURL, ".htm") + ".html"); fallbackErr == nil {
			return fallback, nil
		}
	}
	return nil, err
}

func fdcLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{
		"404", "403", "500", "access denied", "not found", "页面不存在", "请求错误",
		"请求被阻断", "安全验证", "滑块验证", "验证码", "access forbidden", "login", "登录",
	} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	body := strings.ToLower(strings.Join(strings.Fields(doc.Find("body").First().Text()), " "))
	for _, marker := range []string{
		"请输入验证码下载附件", "请输入验证码", "页面不存在", "access denied", "404 not found",
		"请求已被阻断", "请求被阻断", "网站防火墙", "安全验证", "请先登录", "登录页面",
		"无权限访问", "访问被拒绝", "cloudflare", "just a moment", "request blocked",
	} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	if doc.Find("#app, #root").Length() > 0 && doc.Find(".main_right, .article, .v_news_content, .news-list").Length() == 0 {
		return true
	}
	return false
}
