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

// emSubPages contains the article-list routes exposed by the site's menus and
// by the additional lists linked from the home page. Profile, contact, map,
// restricted-IP and other static pages are deliberately not feeds.
var emSubPages = []string{
	"xwdt",
	"dqgz",
	"dqgz/djgz",
	"dqgz/jwgz",
	"dqgz/ghgz",
	"dqgz/ltgz",
	"rcpy",
	"rcpy/xwdt",
	"rcpy/zszl",
	"rcpy/tzgg",
	"rcpy/glgd",
	"rcpy/wdxz",
	"rcpy/zyjj",
	"jypx3/pxfw",
	"jypx3/pxdt",
	"tzgg",
	"hqbz",
	"hqbz/hqfw",
	"hqbz/hqfw/bmfc",
	"hqbz/hqfw/tzgg",
	"hqbz/bwks",
	"gzdt",
	"xqfg",
	"xyyd",
	"xyyd/xyhd",
}

var (
	emArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	emDateRe        = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?`)
	emClockRe       = regexp.MustCompile(`[0-9]{1,2}\s*(?::|时)[0-9]{1,2}`)
	emPageCountRe   = regexp.MustCompile(`(?:/|共|第)\s*([0-9]+)\s*页`)
)

const emMaxResourceBytes = 32 << 20

var emHomeTabTypes = []string{
	"校区公告",
	"党政办公",
	"人才培养",
	"教育培训",
	"后勤保障",
	"其他",
}

func init() {
	registerAdapter("em", SiteAdapter{
		SubPages:     emSubPages,
		ParseList:    parseAdapter_emList,
		ParseArticle: parseAdapter_emArticle,
		PageURL:      emPageURL,
		TotalPages:   emTotalPages,
		FetchArticle: emFetchArticle,
	})
}

// emPageURL maps the logical page number (1 = newest) to em's reverse-numbered
// generated files. For example, xwdt's logical page 2 is xwdt/70.htm and its
// last page is xwdt/1.htm.
func emPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
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

// emTotalPages reads the first page and uses the displayed logical page
// numbers. The shared parser cannot be used because em's hrefs contain the
// reverse generated filename rather than the logical page number.
func emTotalPages(slug string) (int, error) {
	site, ok := AllSites["em"]
	if !ok || site == nil {
		return 0, fmt.Errorf("em site is not registered")
	}

	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(emPageURL(site.BaseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if total := emPageCountFromHTML(body); total > 0 {
			return total, nil
		}
		lastErr = fmt.Errorf("em list %q has no page count", slug)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("em list %q has no page count", slug)
	}
	return 0, lastErr
}

func emPageCountFromHTML(html []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}

	for _, text := range []string{doc.Find(".p_t").Text(), doc.Find(".p_pages").Text()} {
		if match := emPageCountRe.FindStringSubmatch(strings.Join(strings.Fields(text), "")); match != nil {
			if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
				return total
			}
		}
	}

	// Use the text of the page controls, never the href. In the live HTML the
	// href for displayed page 2 is 70.htm on a 71-page feed.
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, page *goquery.Selection) {
		value, err := strconv.Atoi(strings.TrimSpace(page.Text()))
		if err == nil && value > maxPage {
			maxPage = value
		}
	})
	if maxPage > 0 {
		return maxPage
	}

	// A legitimate one-page or empty list still exposes the list shell and
	// disabled page controls. It has one logical page.
	if doc.Find(".p_t, .p_pages").Length() > 0 {
		return 1
	}
	return 0
}

func parseAdapter_emList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse em list: %w", err)
	}
	if emLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("em list is an error or verification page")
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", "")); pageType != "" && pageType != "1" && pageType != "2" {
		return nil, fmt.Errorf("em page is not a list (pageType %q)", pageType)
	}

	if emHasHomeLayout(doc) {
		items := parseEmHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("em home page contains no article entries")
		}
		return items, nil
	}

	list := doc.Find("section.n_titu.bgss, section.TextList.bgss").First()
	if list.Length() == 0 {
		return nil, fmt.Errorf("em page is not a supported article list")
	}

	items := parseEmListRows(list, emPageType(doc), baseURL)
	if len(items) > 0 {
		return items, nil
	}
	if emIsExplicitEmptyList(doc) {
		return []model.NewsItem{}, nil
	}
	return nil, fmt.Errorf("em list contains no valid article rows")
}

func emHasHomeLayout(doc *goquery.Document) bool {
	return doc.Find(".newsBox, .NoticeBox, .gzdtBox, .campusBox").Length() > 0 &&
		doc.Find("#tabs-container1").Length() > 0
}

func parseEmHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)

	appendRows := func(rows *goquery.Selection, typ string) {
		rows.Each(func(_ int, row *goquery.Selection) {
			anchor := emRowArticleAnchor(row, baseURL)
			if anchor == nil || anchor.Length() == 0 {
				return
			}
			item, ok := emNewsItemFromRow(row, anchor, typ, baseURL)
			if !ok || seen[item.URL] {
				return
			}
			seen[item.URL] = true
			items = append(items, item)
		})
	}

	// Parse the visible home lists only. The news carousel repeats the same
	// four rows in a swiper, so the newsText list is the canonical copy.
	appendRows(doc.Find(".newsBox .newsText > li"), "校区要闻")

	doc.Find("#tabs-container1 .swiper-slide").Each(func(i int, slide *goquery.Selection) {
		if i >= len(emHomeTabTypes) {
			return
		}
		appendRows(slide.ChildrenFiltered("ul").First().ChildrenFiltered("li"), emHomeTabTypes[i])
	})

	appendRows(doc.Find(".gzdtBox .gzdtCon > ul > li"), "工作动态")
	appendRows(doc.Find(".campusBox .campusCon > ul > li"), "校区风光")

	return items
}

func parseEmListRows(list *goquery.Selection, pageType, baseURL string) []model.NewsItem {
	rows := list.Find("ul > li")
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := emRowArticleAnchor(row, baseURL)
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		item, ok := emNewsItemFromRow(row, anchor, pageType, baseURL)
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	})
	return items
}

func emRowArticleAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	var result *goquery.Selection
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := emArticleURL(baseURL, anchor.AttrOr("href", "")); !ok {
			return true
		}
		result = anchor
		return false
	})
	return result
}

func emNewsItemFromRow(row, anchor *goquery.Selection, typ, baseURL string) (model.NewsItem, bool) {
	fullURL, ok := emArticleURL(baseURL, anchor.AttrOr("href", ""))
	if !ok {
		return model.NewsItem{}, false
	}

	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		for _, selector := range []string{"h5", "h6", ".line1", ".textline", ".title", "p"} {
			if value := strings.TrimSpace(anchor.Find(selector).First().Text()); value != "" {
				title = value
				break
			}
		}
	}
	title = cleanTitle(title)
	if !emIsValidNewsTitle(title) {
		return model.NewsItem{}, false
	}

	date, publishedAt := emDateFromRow(row)
	item := model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     date,
		Type:     strings.TrimSpace(typ),
		ParsedAt: time.Now(),
	}
	if publishedAt != "" {
		item.PublishedAt = publishedAt
	}
	return item, true
}

func emIsValidNewsTitle(title string) bool {
	if title == "" || len([]rune(title)) > 160 {
		return false
	}
	if isValidNewsTitle(title) {
		return true
	}
	// The shared guard treats every title ending in the Chinese book
	// delimiter as navigation noise. em has real list entries such as
	// “【党章党规】《中国共产党支部工作条例（试行）》”, so allow a
	// sufficiently substantive row title while retaining the normal filters.
	return strings.HasSuffix(title, "》") && len([]rune(title)) > 8
}

func emDateFromRow(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	if dateBox := row.Find(".date, .data").First(); dateBox.Length() > 0 {
		day := strings.TrimSpace(dateBox.Find("i").First().Text())
		year := strings.TrimSpace(dateBox.Find("b").First().Text())
		if day != "" && year != "" {
			if date, published := emDateValues(year + "-" + day); date != "" {
				return date, published
			}
		}
		if date, published := emDateValues(dateBox.Text()); date != "" {
			return date, published
		}
	}

	for _, selector := range []string{"time", ".text > i", ".date", ".data", "i"} {
		if value := strings.TrimSpace(row.Find(selector).First().Text()); value != "" {
			if date, published := emDateValues(value); date != "" {
				return date, published
			}
		}
	}
	return "", ""
}

func emDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(raw); normalized != "" {
		date := normalized[:10]
		if emClockRe.MatchString(raw) {
			return date, normalized
		}
		return date, ""
	}
	if match := emDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func emPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if title := emBreadcrumbType(doc); title != "" {
		return title
	}
	if title := strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")); title != "" {
		return title
	}
	for _, selector := range []string{"#nav_slide .n_title h1", ".n_title h1", ".n_left .con h1"} {
		if title := strings.TrimSpace(doc.Find(selector).First().Text()); title != "" {
			return strings.Join(strings.Fields(title), " ")
		}
	}
	return ""
}

func emBreadcrumbType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	var breadcrumb []string
	doc.Find(".mianbao a").Each(func(_ int, anchor *goquery.Selection) {
		if value := strings.Join(strings.Fields(anchor.Text()), " "); value != "" && value != "首页" {
			breadcrumb = append(breadcrumb, value)
		}
	})
	if len(breadcrumb) == 0 {
		return ""
	}
	return breadcrumb[len(breadcrumb)-1]
}

func emIsExplicitEmptyList(doc *goquery.Document) bool {
	if doc == nil || doc.Find(".p_pages").Length() == 0 {
		return false
	}
	return strings.Contains(strings.Join(strings.Fields(doc.Find(".p_t").Text()), ""), "共0条")
}

func emArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !isValidNewsURL(fullURL) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}

	if emArticlePathRe.MatchString(parsed.Path) {
		parsed.Fragment = ""
		return parsed.String(), true
	}
	// The 管理规定 list uses the same site's legacy Visual SiteBuilder
	// content endpoint instead of /info/<channel>/<id>.htm.
	query := parsed.Query()
	if strings.EqualFold(parsed.Path, "/zzz_content.jsp") &&
		strings.Contains(strings.ToLower(query.Get("urltype")), "newscontenturl") &&
		query.Get("wbnewsid") != "" {
		parsed.Fragment = ""
		return parsed.String(), true
	}
	return "", false
}

func parseAdapter_emArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse em article html: %w", err)
	}
	if emLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("em article is an error or verification page")
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", "")); pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("em page is not an article (pageType %q)", pageType)
	}
	title := emArticleTitle(doc)
	content := emArticleContent(doc)
	if title == "" || content.Length() == 0 {
		return nil, fmt.Errorf("em page is not a supported article")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse em article: %w", err)
	}

	article.Title = title
	// The show01/source line is authoritative. Never fall back to body text:
	// notices commonly contain event and deadline dates.
	meta := emArticleMeta(doc)
	article.Date, article.PublishedAt = emDateValues(flattenedText(meta))
	if article.Date == "" {
		article.Date, article.PublishedAt = emDateValues(meta.Text())
	}
	if article.Date != "" {
		// Keep the date-only precision exposed by the source. normalize below
		// may turn it into the same date string, but never invents a clock.
		article.Date = strings.TrimSpace(article.Date)
	}
	if typ := emArticleType(doc); typ != "" {
		article.Type = typ
	}
	emFinalizeArticleContent(article, content, baseURL)
	emAppendAttachments(article, doc, baseURL)
	emAppendPrevNext(article, doc, baseURL)
	normalizeArticlePublishedAt(article)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("em article has no content or resources")
	}
	return article, nil
}

func emArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".show01 h5", ".article h1", ".article h2", ".article h3", `meta[name="pageTitle"], meta[name="pagetitle"]`} {
		if strings.HasPrefix(selector, "meta[") {
			if title := cleanTitle(doc.Find(selector).Last().AttrOr("content", "")); title != "" {
				return title
			}
			continue
		}
		if title := cleanTitle(doc.Find(selector).First().Text()); title != "" {
			return title
		}
	}
	return ""
}

func emArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	for _, selector := range []string{".show02 .v_news_content", ".article .v_news_content", ".v_news_content", "[id^='vsb_content'] .v_news_content"} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return &goquery.Selection{}
}

func emArticleMeta(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	for _, selector := range []string{".show01 > p", ".show01 p", ".article .source", ".article .news_source", ".news_source", ".source"} {
		if meta := doc.Find(selector).First(); meta.Length() > 0 {
			return meta
		}
	}
	return &goquery.Selection{}
}

func emFinalizeArticleContent(article *model.Article, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}
	article.Content = extractBodaContentText(content)
	article.Images = nil
	seen := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		var raw string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				raw = value
				break
			}
		}
		resolved := emResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Images = append(article.Images, resolved)
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	if html, err := content.Html(); err == nil {
		article.ContentHTML = html
	}
}

func emFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := emBaseURL()
	fullURL, ok := emArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("em: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("em: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_emArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("em: parse article %s: %w", fullURL, err)
	}
	article.Images = emVerifyImages(article.Images, baseURL, fullURL)
	article.Attachments = emVerifyAttachments(article.Attachments, baseURL, fullURL)
	emSanitizeContentHTML(article, baseURL)
	return article, body, nil
}

func emBaseURL() string {
	if site := AllSites["em"]; site != nil {
		if base := strings.TrimRight(strings.TrimSpace(site.BaseURL), "/"); base != "" {
			return base
		}
	}
	return "https://em.swjtu.edu.cn"
}

func emResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return ""
	}
	lower := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{"/captcha", "/verify", "/login", "/signin", "/placeholder", "/transparent", "/spacer", "/blank", "/loading", "/pixel.gif"} {
		if strings.Contains(lower, marker) {
			return ""
		}
	}
	return parsed.String()
}

func emFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	resolved := emResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("em: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", err
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
	base, _ := url.Parse(baseURL)
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if next == nil || base == nil || !strings.EqualFold(next.URL.Hostname(), base.Hostname()) {
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
		return nil, contentType, fmt.Errorf("em: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, emMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, err
	}
	if len(body) > emMaxResourceBytes || !emResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("em: resource %s has invalid type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func emVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved := emResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := emFetchResource(resolved, baseURL, referer, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func emVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := emResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := emFetchResource(resolved, baseURL, referer, false)
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

func emResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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
	probe := bytes.ToLower(bytes.TrimSpace(body))
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	for _, marker := range [][]byte{[]byte("<!doctype html"), []byte("<html"), []byte("<head"), []byte("<body"), []byte("验证码"), []byte("安全验证"), []byte("captcha"), []byte("waf"), []byte("access denied"), []byte("just a moment"), []byte("cloudflare"), []byte("页面不存在"), []byte("404 not found"), []byte("请先登录"), []byte("请登录后"), []byte("请求已被阻断")} {
		if bytes.Contains(probe, marker) {
			return false
		}
	}

	magic := emResourceMagic(body)
	if image {
		return emImageTypeMatchesMagic(mediaType, magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return (mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf") || mediaType == "application/octet-stream" || mediaType == "application/force-download" || mediaType == "application/download") && magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return (mediaType == "application/zip" || strings.Contains(mediaType, "officedocument") || strings.Contains(mediaType, "ml") || mediaType == "application/octet-stream") && magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return (mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint" || mediaType == "application/octet-stream") && magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return strings.HasPrefix(mediaType, "text/") && mediaType != "text/html" && mediaType != "text/xml"
	default:
		return magic != ""
	}
}

func emImageTypeMatchesMagic(mediaType, magic string) bool {
	expected := map[string]string{"image/jpeg": "jpeg", "image/jpg": "jpeg", "image/pjpeg": "jpeg", "image/png": "png", "image/gif": "gif", "image/webp": "webp", "image/bmp": "bmp", "image/tiff": "tiff", "image/svg+xml": "svg"}
	return expected[mediaType] != "" && expected[mediaType] == magic
}

func emResourceMagic(body []byte) string {
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
	lower := bytes.ToLower(trimmed)
	if bytes.HasPrefix(lower, []byte("{\\rtf")) {
		return "rtf"
	}
	if bytes.HasPrefix(lower, []byte("<svg")) || (bytes.HasPrefix(lower, []byte("<?xml")) && bytes.Contains(lower, []byte("<svg"))) {
		return "svg"
	}
	return ""
}

func emSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		if resolved := emResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved := emResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"em-content-root\">" + article.ContentHTML + "</div>"))
	if err != nil {
		return
	}
	content := doc.Find("#em-content-root").First()
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := emResourceURL(baseURL, image.AttrOr("src", ""))
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		raw := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
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
		parsed, parseErr := url.Parse(resolved)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	if html, err := content.Html(); err == nil {
		article.ContentHTML = html
		article.Content = extractBodaContentText(content)
	}
}

func emLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	pageTitle := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"验证码", "访问验证", "页面不存在", "404 not found", "system error", "系统错误", "access denied", "cloudflare", "just a moment", "请先登录", "请登录后"} {
		if strings.Contains(pageTitle, strings.ToLower(marker)) {
			return true
		}
	}
	bodyText := strings.ToLower(strings.Join(strings.Fields(doc.Find("body").Text()), " "))
	for _, marker := range []string{"页面不存在", "访问被拒绝", "安全验证", "请输入验证码", "请先登录", "请登录后", "请求已被阻断", "access denied", "cloudflare", "just a moment"} {
		if strings.Contains(bodyText, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func emArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if title := emBreadcrumbType(doc); title != "" {
		return title
	}
	for _, selector := range []string{".n_left .con h1", "#nav_slide .n_title h1", ".n_title h1", ".article .current h3", ".current h3"} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func emAppendAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	scope := doc.Find(".show").First()
	if scope.Length() == 0 {
		scope = doc.Find(".article").First()
	}
	if scope.Length() == 0 {
		scope = doc.Find("body").First()
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}

	scope.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !emIsAttachmentLink(href) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if resolved == "" || seen[resolved] {
			return
		}
		name := strings.TrimSpace(link.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(link.AttrOr("name", ""))
		}
		if name == "" {
			name = strings.Join(strings.Fields(link.Text()), " ")
		}
		if name == "" {
			if parsed, err := url.Parse(resolved); err == nil {
				name = strings.TrimPrefix(parsed.Path, "/")
			}
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	})
}

func emIsAttachmentLink(href string) bool {
	if href == "" {
		return false
	}
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, extension := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(lower, extension) {
			return true
		}
	}
	return false
}

func emAppendPrevNext(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	doc.Find(".show03 p a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		text := strings.Join(strings.Fields(link.Text()), " ")
		parentText := strings.Join(strings.Fields(link.Parent().Text()), " ")
		resolved := ""
		// The live em template emits bare sibling filenames (for example
		// 20636.htm), but this parser has no current article directory. Keep
		// the title and only resolve URLs whose base is unambiguous.
		if strings.HasPrefix(href, "/") || strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
			resolved = resolveURL(baseURL, href)
		}
		if strings.Contains(parentText, "上一篇") {
			article.PrevURL, article.PrevTitle = resolved, text
		} else if strings.Contains(parentText, "下一篇") {
			article.NextURL, article.NextTitle = resolved, text
		}
	})
}
