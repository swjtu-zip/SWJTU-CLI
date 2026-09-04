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

// Confirmed article-bearing entry points. Empty/static pages (wkyx, gywm,
// gzzd/sskl and xzzx/sskl) are intentionally excluded.
var wkjscSubPages = []string{
	"tzgg", "wkxw", "xsdt", "wkpt", "gzzd", "gzzd/gj", "gzzd/jyb", "gzzd/qt",
	"xzzx", "xzzx/gj", "xzzx/jyb", "xzzx/qt", "bszn",
}

var wkjscFeedTypes = map[string]string{
	"tzgg":     "通知公告",
	"wkxw":     "文科新闻",
	"xsdt":     "学术动态",
	"wkpt":     "文科平台",
	"gzzd":     "规章制度",
	"xzzx":     "下载中心",
	"bszn":     "办事指南",
	"gzzd/gj":  "国家",
	"gzzd/jyb": "教育部",
	"gzzd/qt":  "其他",
	"xzzx/gj":  "国家",
	"xzzx/jyb": "教育部",
	"xzzx/qt":  "其他",
}

// Article pages expose the source column through the generated _jsq_ call.
// IDs are site-specific even when several sites use the same CMS.
var wkjscColumnTypes = map[string]string{
	"1002": "通知公告",
	"1003": "文科新闻",
	"1004": "学术动态",
	"1006": "文科平台",
	"1007": "规章制度",
	"1008": "下载中心",
	"1009": "办事指南",
	"1013": "国家",
	"1014": "教育部",
	"1015": "省社科联",
	"1016": "其他",
	"1021": "国家",
	"1022": "教育部",
	"1023": "省社科联",
	"1024": "其他",
}

// wkjsc.swjtu.edu.cn has four explicit news lists on its home page and uses
// the same news_ul1 list shell for every archive page. The generic parser only
// knows news_ul, and its fallback cannot see the legacy fanye counter.
func init() {
	registerAdapter("wkjsc", SiteAdapter{
		SubPages:     wkjscSubPages,
		ParseList:    parseAdapter_wkjscList,
		ParseArticle: parseAdapter_wkjscArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   wkjscTotalPages,
		FetchArticle: wkjscFetchArticle,
	})
}

var wkjscArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var (
	wkjscDateRE     = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?`)
	wkjscSourceRE   = regexp.MustCompile(`来源\s*[:：]?\s*(.*?)(?:\s+(?:编辑|浏览)\b|\])`)
	wkjscColumnIDRE = regexp.MustCompile(`(?i)_jsq_\(\s*(\d+)`)
	wkjscPageTypeRE = regexp.MustCompile(`(?i)^\s*3\s*$`)
)

const wkjscSiteName = "西南交通大学文科建设处"

const wkjscMaxResourceBytes int64 = 32 << 20

func parseAdapter_wkjscList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse wkjsc list: %w", err)
	}
	if wkjscErrorShell(doc) {
		return nil, fmt.Errorf("wkjsc: refusing error, WAF, or CAPTCHA response")
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	listShell := doc.Find("ul.news_ul, ul.news_ul1")
	if listShell.Length() == 0 {
		return nil, fmt.Errorf("wkjsc: response has no news_ul/news_ul1 list shell")
	}
	add := func(anchor *goquery.Selection, scope *goquery.Selection, typ string) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL := cleanURL(resolveURL(baseURL, href))
		if !wkjscArticleURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}

		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 100 {
			return
		}
		date := wkjscDate(scope)
		if index, ok := seen[fullURL]; ok {
			if items[index].Date == "" && date != "" {
				items[index].Date = date
				items[index].PublishedAt = normalizePublishedAt(date)
			}
			if items[index].Type == "" {
				items[index].Type = typ
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{
			Title: title, URL: fullURL, Date: date,
			PublishedAt: normalizePublishedAt(date), Type: typ, ParsedAt: time.Now(),
		})
	}

	// These lists are the source of truth for the page's four visible feeds.
	doc.Find("ul.news_ul li, ul.news_ul1 li").Each(func(_ int, row *goquery.Selection) {
		typ := wkjscFeedTypeForRow(row)
		if typ == "" {
			typ = wkjscListType(doc)
		}
		add(row.Find("a[href]").First(), row, typ)
	})
	return items, nil
}

func wkjscFeedTypeForRow(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	// The homepage nests the news_ul lists below several anonymous wrappers;
	// walk every ancestor with an id instead of stopping at a class that may be
	// absent from a particular responsive rendering.
	for current := row; current.Length() > 0; current = current.Parent() {
		id := strings.TrimSpace(current.AttrOr("id", ""))
		if typ := wkjscFeedTypes[id]; typ != "" {
			return typ
		}
	}
	return ""
}

func wkjscListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{"#div_title1", ".news_title", ".list-title", ".list_tit", ".title h2", ".title h3", ".ny-title", ".position h1", "h1", "h2"} {
		candidate := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if typ := wkjscTypeLabel(candidate); typ != "" {
			return typ
		}
	}
	return ""
}

func wkjscTypeLabel(candidate string) string {
	candidate = strings.TrimSpace(strings.Join(strings.Fields(candidate), " "))
	if candidate == "" || candidate == wkjscSiteName || candidate == "文科建设处" {
		return ""
	}
	for _, typ := range wkjscColumnTypes {
		if candidate == typ {
			return typ
		}
	}
	for _, typ := range wkjscFeedTypes {
		if candidate == typ {
			return typ
		}
	}
	return ""
}

func wkjscTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if wkjscFeedTypes[slug] == "" {
		return 0, fmt.Errorf("wkjsc: %q is not a supported article-list route", slug)
	}
	baseURL := "https://wkjsc.swjtu.edu.cn"
	if site := AllSites["wkjsc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("wkjsc: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("wkjsc: parse first page for %s: %w", slug, err)
	}
	if doc.Find("ul.news_ul, ul.news_ul1").Length() == 0 {
		return 0, fmt.Errorf("wkjsc: %s is not a news list page", slug)
	}
	if total := wkjscPageCount(doc); total > 0 {
		return total, nil
	}
	// A recognized list with p_no_d and no numbered link is a legitimate
	// one-page feed, including the small bszn/gzzd/xzzx archives.
	return 1, nil
}

var wkjscPageCountRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)

func wkjscPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	for _, value := range []string{
		strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .p_pages, .paging, .page").Text()), " "),
		doc.Text(),
	} {
		if match := wkjscPageCountRE.FindStringSubmatch(value); match != nil {
			var total int
			fmt.Sscanf(match[1], "%d", &total)
			if total > 0 {
				return total
			}
		}
	}
	return 0
}

func wkjscArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || !wkjscArticlePathRE.MatchString(parsed.Path) {
		return false
	}
	return !isExternalURL(rawURL, baseURL)
}

func wkjscErrorShell(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	text := strings.ToLower(strings.Join(strings.Fields(doc.Find("body").First().Text()), " "))
	if len(text) > 4096 {
		text = text[:4096]
	}
	for _, marker := range []string{
		"captcha", "验证码", "安全验证", "cloudflare", "access denied", "just a moment",
		"请求已被阻断", "页面不存在", "系统错误", "404 not found", "403 forbidden",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func wkjscDate(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(scope.Find(".date").First().Text()), " ")
	if text == "" {
		text = strings.Join(strings.Fields(scope.Text()), " ")
	}
	date, _ := wkjscDateValues(text)
	return date
}

func normalizeWkjscDate(text string) string {
	date, _ := wkjscDateValues(text)
	return date
}

func wkjscDateValues(text string) (string, string) {
	text = strings.Join(strings.Fields(text), " ")
	if match := dateTimeRe.FindString(text); match != "" {
		if published := normalizePublishedAt(match); len(published) >= 10 {
			return published[:10], published
		}
	}
	match := wkjscDateRE.FindStringSubmatch(text)
	if match == nil {
		return "", ""
	}
	date := normalizeDateParts(match[1], match[2], match[3])
	return date, date
}

func wkjscArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}

	// The detail template renders the real headline as #div_title #lb_title.
	// The document title and the first pageTitle meta instead identify the site.
	for _, selector := range []string{"#div_title #lb_title", "#lb_title"} {
		if title := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); title != "" {
			return title
		}
	}
	return ""
}

func wkjscArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	var typ string
	doc.Find("script").EachWithBreak(func(_ int, script *goquery.Selection) bool {
		match := wkjscColumnIDRE.FindStringSubmatch(script.Text())
		if len(match) == 2 {
			typ = wkjscColumnTypes[match[1]]
		}
		return typ == ""
	})
	if typ != "" {
		return typ
	}
	return wkjscListType(doc)
}

func wkjscFallbackTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}

	var title string
	doc.Find(`meta[name="pageTitle"]`).EachWithBreak(func(_ int, meta *goquery.Selection) bool {
		candidate := strings.Join(strings.Fields(meta.AttrOr("content", "")), " ")
		if candidate != "" && candidate != wkjscSiteName {
			title = candidate
			return false
		}
		return true
	})
	if title != "" {
		return title
	}

	title = stripSiteSuffix(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	if title == wkjscSiteName {
		return ""
	}
	return title
}

func parseAdapter_wkjscArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("wkjsc: parse article HTML: %w", err)
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", "")); pageType != "" && !wkjscPageTypeRE.MatchString(pageType) {
		return nil, fmt.Errorf("wkjsc: refusing non-article response (pageType=%q)", pageType)
	}
	title := wkjscArticleTitle(doc)
	if title == "" {
		return nil, fmt.Errorf("wkjsc: article shell has no fixed #lb_title headline")
	}
	content := doc.Find("#div_wenzi #vsb_content .v_news_content, #div_wenzi #vsb_content_501 .v_news_content").First()
	if content.Length() == 0 {
		content = doc.Find("#vsb_content .v_news_content, #vsb_content_501 .v_news_content").First()
	}
	if content.Length() == 0 {
		return nil, fmt.Errorf("wkjsc: response has no article body")
	}
	if wkjscErrorShell(doc) {
		return nil, fmt.Errorf("wkjsc: refusing error, WAF, or CAPTCHA article response")
	}
	article := &model.Article{Title: cleanTitle(title), Type: wkjscArticleType(doc)}
	if meta := strings.Join(strings.Fields(doc.Find("#lb_hits").First().Text()), " "); meta != "" {
		if date, publishedAt := wkjscDateValues(meta); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		}
		if article.Source == "" {
			if match := wkjscSourceRE.FindStringSubmatch(meta); match != nil {
				if source := strings.TrimSpace(cleanLabelValue(match[1])); source != "" {
					article.Source = source
				}
			}
		}
	}
	article.Content = extractBodaContentText(content)
	article.Images = wkjscContentImages(content, baseURL)
	article.Attachments = wkjscContentAttachments(content, baseURL)
	wkjscAppendOutsideAttachments(article, doc, baseURL)
	if contentHTML, htmlErr := content.Html(); htmlErr == nil {
		article.ContentHTML = contentHTML
	}
	return article, nil
}

func wkjscAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" || !wkjscAttachmentLink(link, href) {
			return
		}
		resolved, ok := wkjscResourceURL(baseURL, href)
		if !ok {
			return
		}
		if seen[resolved] {
			return
		}
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
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	})
}

func wkjscContentImages(content *goquery.Selection, baseURL string) []string {
	images := make([]string, 0)
	seen := make(map[string]bool)
	if content == nil || content.Length() == 0 {
		return images
	}
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		var raw string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := htmlstd.UnescapeString(strings.TrimSpace(image.AttrOr(attr, ""))); value != "" {
				raw = value
				break
			}
		}
		resolved, ok := wkjscResourceURL(baseURL, raw)
		if !ok || seen[resolved] {
			image.Remove()
			return
		}
		seen[resolved] = true
		images = append(images, resolved)
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vsbhref", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	return images
}

func wkjscContentAttachments(content *goquery.Selection, baseURL string) []model.Attachment {
	attachments := make([]model.Attachment, 0)
	seen := make(map[string]bool)
	if content == nil || content.Length() == 0 {
		return attachments
	}
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		wkjscAppendAttachment(&attachments, seen, link, baseURL)
	})
	return attachments
}

func wkjscAppendAttachment(attachments *[]model.Attachment, seen map[string]bool, link *goquery.Selection, baseURL string) {
	if attachments == nil || link == nil || link.Length() == 0 {
		return
	}
	href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
	if !wkjscAttachmentLink(link, href) {
		return
	}
	resolved, ok := wkjscResourceURL(baseURL, href)
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

func wkjscAttachmentLink(link *goquery.Selection, href string) bool {
	if link == nil || link.Length() == 0 || href == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	if strings.TrimSpace(link.AttrOr("download", "")) != "" {
		return true
	}
	lower := strings.ToLower(href + " " + link.AttrOr("name", "") + " " + link.Text())
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".wps", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	for _, selector := range []string{"li", ".attachment", ".attachments", ".news-attachment", ".download"} {
		if ancestor := link.Closest(selector); ancestor.Length() > 0 && strings.Contains(ancestor.Text(), "附件") {
			return true
		}
	}
	return false
}

func wkjscResourceURL(baseURL, raw string) (string, bool) {
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
	pathQuery := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{"captcha", "verifycode", "verification", "createimage", "getcode", "/login", "accessdenied"} {
		if strings.Contains(pathQuery, marker) {
			return "", false
		}
	}
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif", "/pixel.gif", "/loading.gif"} {
		if strings.HasSuffix(strings.ToLower(parsed.Path), placeholder) {
			return "", false
		}
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func wkjscFetchArticle(articleURL string) (*model.Article, []byte, error) {
	return wkjscFetchArticleAt(articleURL, "https://wkjsc.swjtu.edu.cn")
}

func wkjscFetchArticleAt(articleURL, baseURL string) (*model.Article, []byte, error) {
	canonical := cleanURL(resolveURL(baseURL, articleURL))
	if !wkjscArticleURL(canonical, baseURL) {
		return nil, nil, fmt.Errorf("wkjsc: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonical)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_wkjscArticle(body, baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("wkjsc article %s: %w", canonical, err)
	}
	candidates := append([]model.Attachment(nil), article.Attachments...)
	article.Images = wkjscVerifyImages(article.Images, canonical)
	article.Attachments = wkjscVerifyAttachments(article.Attachments, canonical)
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
	article.ContentHTML = wkjscSanitizeContentHTML(article.ContentHTML, baseURL, validImages, candidateAttachments, validAttachments)
	return article, body, nil
}

func wkjscVerifyImages(images []string, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	baseURL := wkjscVerificationBaseURL(referer)
	for _, raw := range images {
		resolved, ok := wkjscResourceURL(baseURL, raw)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := wkjscFetchResource(resolved, referer, true)
		if err != nil || !wkjscResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func wkjscVerifyAttachments(attachments []model.Attachment, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	baseURL := wkjscVerificationBaseURL(referer)
	for _, attachment := range attachments {
		resolved, ok := wkjscResourceURL(baseURL, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := wkjscFetchResource(resolved, referer, false)
		if err != nil || !wkjscResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func wkjscFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	baseURL := wkjscVerificationBaseURL(referer)
	resolved, ok := wkjscResourceURL(baseURL, rawURL)
	if !ok {
		return nil, "", fmt.Errorf("wkjsc: refusing invalid resource %q", rawURL)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return nil, "", fmt.Errorf("wkjsc: refusing non-HTTP resource %q", resolved)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("wkjsc: create resource request: %w", err)
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
	client := &http.Client{Timeout: 15 * time.Second}
	if defaultClient != nil {
		copyClient := *defaultClient
		client = &copyClient
	}
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if scheme := strings.ToLower(next.URL.Scheme); scheme != "http" && scheme != "https" {
			return fmt.Errorf("wkjsc: refusing non-HTTP resource redirect")
		}
		if !strings.EqualFold(next.URL.Hostname(), parsed.Hostname()) {
			return fmt.Errorf("wkjsc: refusing cross-site resource redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("wkjsc: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("wkjsc: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	if response.ContentLength > wkjscMaxResourceBytes {
		return nil, contentType, fmt.Errorf("wkjsc: resource %s exceeds size limit", resolved)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, wkjscMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("wkjsc: read resource %s: %w", resolved, err)
	}
	if int64(len(body)) > wkjscMaxResourceBytes || !wkjscResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("wkjsc: resource %s returned invalid Content-Type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func wkjscVerificationBaseURL(referer string) string {
	parsed, err := url.Parse(strings.TrimSpace(referer))
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return "https://wkjsc.swjtu.edu.cn"
}

func wkjscResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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
		"<!doctype html", "<html", "<head", "<body", "请输入验证码", "验证码下载附件", "captcha",
		"请先登录", "请登录后", "登录页面", "页面不存在", "access denied", "just a moment",
		"cloudflare", "请求已被阻断", "安全威胁", "系统错误",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	magic := wkjscResourceMagic(body)
	if image {
		return wkjscImageTypeMatchesMagic(mediaType, magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(lowerName, ".wps") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip" || magic == "ole"
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

func wkjscImageTypeMatchesMagic(mediaType, magic string) bool {
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

func wkjscResourceMagic(body []byte) string {
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

func wkjscSanitizeContentHTML(rawHTML, baseURL string, validImages, candidateAttachments, validAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="wkjsc-content-root">` + rawHTML + `</div>`))
	if err != nil {
		return rawHTML
	}
	root := doc.Find("#wkjsc-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var resolved string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if raw := htmlstd.UnescapeString(strings.TrimSpace(image.AttrOr(attr, ""))); raw != "" {
				if candidate, ok := wkjscResourceURL(baseURL, raw); ok {
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
		resolved, ok := wkjscResourceURL(baseURL, href)
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
