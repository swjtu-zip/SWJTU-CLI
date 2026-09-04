package scraper

import (
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

const rwxyCanonicalBaseURL = "https://rwxy.swjtu.edu.cn"

var rwxySubPages = []string{
	"xyxw", "dzbg", "xshd", "jxdt", "jxdt/bkjx", "jxdt/yjsjx",
	"xsgz/tzgg", "xsgz/xgdt", "xsgz/xzfc", "xsgz/jyxx", "jwgz/jwdt",
	"djgz/djdt", "djgz/xygh", "rcpy/yjsjy", "rcpy/bshldz",
}

// rwxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	if site := AllSites["rwxy"]; site != nil {
		site.BaseURL = rwxyCanonicalBaseURL
	}
	registerAdapter("rwxy", SiteAdapter{
		SubPages:     rwxySubPages,
		ParseList:    parseAdapter_rwxyList,
		ParseArticle: parseAdapter_rwxyArticle,
		PageURL:      rwxyPageURL,
		TotalPages:   rwxyTotalPages,
		FetchArticle: rwxyFetchArticle,
	})
}

func parseAdapter_rwxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("rwxy parse list: %w", err)
	}
	if rwxyListContainerExists(doc) {
		if rwxyBlockedListResponse(doc) {
			return nil, fmt.Errorf("rwxy: list response is a login, WAF, or error page")
		}
		return rwxyParseListRows(doc, baseURL), nil
	}
	if rwxyBlockedListResponse(doc) {
		return nil, fmt.Errorf("rwxy: list response is a login, WAF, or error page")
	}
	if items := parseRwxyHomeList(doc, baseURL); len(items) > 0 {
		return items, nil
	}
	if rwxyHomeSectionsExist(doc) {
		return []model.NewsItem{}, nil
	}
	return nil, fmt.Errorf("rwxy: response contains no recognized list or homepage feed")
}

func parseAdapter_rwxyArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil || article == nil {
		return article, err
	}
	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if parseErr != nil {
		return article, nil
	}
	if rwxyBlockedListResponse(doc) {
		return nil, fmt.Errorf("rwxy: article response is a login, WAF, or error page")
	}
	if doc.Find("form[name='_newscontent_fromname']").Length() == 0 {
		return nil, fmt.Errorf("rwxy: response contains no recognized article shell")
	}
	if title := rwxyArticleTitle(doc); title != "" {
		article.Title = title
	}
	if typ := rwxyArticleType(doc, article.Title); typ != "" {
		article.Type = typ
	}
	meta := doc.Find("form[name='_newscontent_fromname'] .bd .title .info, form[name='_newscontent_fromname'] .title .info, form[name='_newscontent_fromname'] .info").First()
	if meta.Length() > 0 {
		if date, publishedAt := rwxyDateValues(meta.Text()); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		} else if rwxyHasLegacyPlaceholder(meta.Text()) {
			article.Date = ""
			article.PublishedAt = ""
		} else {
			article.Date = ""
			article.PublishedAt = ""
		}
	} else {
		// The shared parser has broader fallbacks for other sites. RWXY's
		// article date is only trustworthy in the fixed .info metadata line;
		// never promote an event/deadline date from the body.
		article.Date = ""
		article.PublishedAt = ""
	}
	if rwxyHasLegacyPlaceholder(article.Date) || rwxyHasLegacyPlaceholder(article.PublishedAt) {
		article.Date = ""
		article.PublishedAt = ""
	}
	rwxyAppendAttachments(doc, article, baseURL)
	rwxyFilterArticleResources(article, baseURL)
	return article, nil
}

// rwxyResourceURL accepts only fetchable HTTP(S) resources.  The shared
// parser deliberately permits CDN images, but RWXY's detail pages also carry
// spacer, login, and WAF links in the same DOM, so the adapter applies a
// second site-specific filter to both images and attachments.
func rwxyResourceURL(baseURL, raw string, sameSite bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) {
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
	if sameSite {
		if !isValidSiteNewsURL(resolved, baseURL) {
			return ""
		}
	} else if !isValidNewsURL(resolved) {
		return ""
	}
	path := strings.ToLower(parsed.EscapedPath())
	if path == "" || path == "/" {
		return ""
	}
	resourceText := path + "?" + strings.ToLower(parsed.RawQuery)
	for _, marker := range []string{
		"/spacer.gif", "/blank.gif", "/transparent.gif", "/placeholder.",
		"/placeholder/", "/loading.", "/captcha", "/verify", "/waf",
		"/login", "/access-denied", "/access_denied", "/filedownload/", "createimage.jsp",
	} {
		if strings.Contains(resourceText, marker) {
			return ""
		}
	}
	return resolved
}

func rwxyFilterArticleResources(article *model.Article, baseURL string) {
	if article == nil {
		return
	}
	images := make([]string, 0, len(article.Images))
	seenImages := make(map[string]bool, len(article.Images))
	for _, raw := range article.Images {
		resolved := rwxyResourceURL(baseURL, raw, false)
		if resolved == "" || seenImages[resolved] {
			continue
		}
		seenImages[resolved] = true
		images = append(images, resolved)
	}
	article.Images = images

	attachments := make([]model.Attachment, 0, len(article.Attachments))
	seenAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		resolved := rwxyResourceURL(baseURL, attachment.URL, true)
		if resolved == "" || seenAttachments[resolved] {
			continue
		}
		attachment.URL = resolved
		seenAttachments[resolved] = true
		attachments = append(attachments, attachment)
	}
	article.Attachments = attachments
	rwxySanitizeContentHTML(article, baseURL)
}

func rwxySanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || article.ContentHTML == "" {
		return
	}
	fragment := `<div id="rwxy-content-root">` + article.ContentHTML + `</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return
	}
	root := doc.Find("#rwxy-content-root").First()
	root.Find("img").Each(func(_ int, sel *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if raw := strings.TrimSpace(sel.AttrOr(attr, "")); raw != "" {
				resolved = rwxyResourceURL(baseURL, raw, false)
				if resolved != "" {
					break
				}
			}
		}
		if resolved == "" {
			sel.Remove()
			return
		}
		sel.SetAttr("src", resolved)
	})
	if sanitized, err := root.Html(); err == nil {
		article.ContentHTML = sanitized
	}
}

// RWXY places its download list outside .v_news_content.  The shared parser
// intentionally scans the prose wrapper only, so this adapter scans the
// article form for explicit download/file links and then applies the same
// host and pseudo-resource checks as the content parser.
func rwxyAppendAttachments(doc *goquery.Document, article *model.Article, baseURL string) {
	if doc == nil || article == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved := rwxyResourceURL(baseURL, attachment.URL, true); resolved != "" {
			seen[resolved] = true
		}
	}
	doc.Find("form[name='_newscontent_fromname'] a[href], .left-news-details li a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if !bodaLooksLikeAttachmentLink(sel, href) {
			return
		}
		resolved := rwxyResourceURL(baseURL, href, true)
		if resolved == "" || seen[resolved] {
			return
		}
		name := strings.TrimSpace(sel.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(sel.AttrOr("name", ""))
		}
		if name == "" {
			name = strings.TrimSpace(sel.Text())
		}
		if name == "" {
			name = resolved
		}
		if decoded, err := url.QueryUnescape(name); err == nil {
			name = decoded
		}
		article.Attachments = append(article.Attachments, model.Attachment{
			Name: strings.Join(strings.Fields(name), " "),
			URL:  resolved,
		})
		seen[resolved] = true
	})
}

// rwxyVerifyAttachments is called by rwxyFetchArticle after HTML parsing.
// HTML parsing remains deterministic and cheap for callers that only need
// fields; a live article fetch additionally removes links whose response is a
// captcha/login/error shell instead of a file.
func rwxyVerifyAttachments(attachments []model.Attachment) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		body, contentType, err := rwxyFetchAttachment(attachment.URL)
		if err != nil {
			continue
		}
		if !rwxyAttachmentBodyIsUsable(contentType, body, attachment.Name+" "+attachment.URL) {
			continue
		}
		attachment.Size = strconv.Itoa(len(body))
		verified = append(verified, attachment)
	}
	return verified
}

func rwxyFetchAttachment(rawURL string) ([]byte, string, error) {
	resolved := rwxyResourceURL(rwxyCanonicalBaseURL, rawURL, true)
	if resolved == "" {
		return nil, "", fmt.Errorf("rwxy: refusing non-local attachment %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("rwxy: create attachment request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("rwxy: fetch attachment %s: %w", resolved, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.Header.Get("Content-Type"), fmt.Errorf("rwxy: attachment %s: HTTP %d", resolved, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header.Get("Content-Type"), fmt.Errorf("rwxy: read attachment %s: %w", resolved, err)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

func rwxyAttachmentBodyIsUsable(contentType string, body []byte, nameOrURL string) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
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
		"just a moment", "cloudflare", "请求已被阻断", "安全威胁",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf":
		return len(body) >= 5 && strings.HasPrefix(string(body), "%PDF-")
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || mediaType == "application/zip":
		return len(body) >= 4 && string(body[:4]) == "PK\x03\x04"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt"):
		return len(body) >= 8 && string(body[:8]) == "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"
	default:
		return true
	}
}

func rwxyFetchArticle(articleURL string) (*model.Article, []byte, error) {
	if !isValidSiteNewsURL(articleURL, rwxyCanonicalBaseURL) {
		return nil, nil, fmt.Errorf("rwxy: refusing article URL outside the site: %s", articleURL)
	}
	body, err := fetch(articleURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_rwxyArticle(body, rwxyCanonicalBaseURL)
	if err != nil {
		return nil, nil, err
	}
	article.Attachments = rwxyVerifyAttachments(article.Attachments)
	return article, body, nil
}

var rwxyDateRe = regexp.MustCompile(`20\d{2}\s*(?:[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}日?)(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?`)
var rwxyPageIndicatorRe = regexp.MustCompile(`\b1\s*/\s*(\d+)\b`)

const rwxyLegacyPlaceholderDate = "2013-01-01"

func rwxyDateValues(text string) (string, string) {
	text = strings.NewReplacer("：", ":", "／", "/", "．", ".").Replace(text)
	match := rwxyDateRe.FindString(strings.Join(strings.Fields(text), " "))
	if match == "" {
		return "", ""
	}
	precise := normalizePublishedAt(match)
	if precise == "" {
		return "", ""
	}
	if rwxyHasLegacyPlaceholder(precise) {
		return "", ""
	}
	return precise[:10], precise
}

func rwxyHasLegacyPlaceholder(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), rwxyLegacyPlaceholderDate)
}

func rwxyListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".loaction a", ".sidebar .title span", ".sidebar .title"} {
		values := doc.Find(selector)
		for i := values.Length() - 1; i >= 0; i-- {
			value := strings.Join(strings.Fields(values.Eq(i).Text()), " ")
			if value != "" && value != "首页" {
				return value
			}
		}
	}
	if value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " "); value != "" && value != "首页" {
		return value
	}
	return ""
}

func rwxyListContainerExists(doc *goquery.Document) bool {
	return doc != nil && doc.Find(".list-news, .list-notice").Length() > 0
}

func rwxyParseListRows(doc *goquery.Document, baseURL string) []model.NewsItem {
	if doc == nil {
		return []model.NewsItem{}
	}
	rows := doc.Find(".list-news > ul > li")
	listNews := rows.Length() > 0 || doc.Find(".list-news").Length() > 0
	if !listNews {
		rows = doc.Find(".list-notice > ul > li")
	}
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	typ := rwxyListType(doc)
	rows.Each(func(_ int, row *goquery.Selection) {
		var anchor *goquery.Selection
		if listNews {
			anchor = row.Find("a[href*='info/']").First()
		} else {
			anchor = row.Find(".tits a[href], a[href*='info/']").First()
		}
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL := cleanURL(resolveURL(baseURL, href))
		if href == "" || !isValidSiteNewsURL(fullURL, baseURL) || seen[fullURL] {
			return
		}

		title := ""
		if listNews {
			// The outer anchor contains an invalid nested More anchor. The
			// headline is the visible text in .time; its date is in the
			// HTML comment immediately before that text.
			timeBlock := row.Find(".time").First()
			title = strings.TrimSpace(timeBlock.Text())
		} else {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || title == "More →" || !isValidNewsTitle(title) {
			return
		}

		date, publishedAt := rwxyNoticeDate(row)
		if listNews {
			if timeBlock := row.Find(".time").First(); timeBlock.Length() > 0 {
				dateText, _ := timeBlock.Html()
				date, publishedAt = rwxyDateValues(dateText)
			}
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt,
			Type: typ, ParsedAt: time.Now(),
		})
	})
	return items
}

func rwxyNoticeDate(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	timeBlock := row.Find(".time").First()
	if timeBlock.Length() == 0 {
		return "", ""
	}
	day := strings.Join(strings.Fields(timeBlock.Find("b").First().Text()), "")
	yearMonth := strings.Join(strings.Fields(timeBlock.Find("em").First().Text()), "")
	if day != "" && yearMonth != "" {
		if date, publishedAt := rwxyDateValues(yearMonth + "-" + day); date != "" {
			return date, publishedAt
		}
	}
	return rwxyDateValues(timeBlock.Text())
}

func rwxyArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{
		"form[name='_newscontent_fromname'] .bd .title h3",
		"form[name='_newscontent_fromname'] .title h3",
		".bd .title h3",
	} {
		if title := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); title != "" {
			return title
		}
	}
	return ""
}

func rwxyArticleType(doc *goquery.Document, articleTitle string) string {
	if doc == nil {
		return ""
	}
	values := doc.Find(".loaction a")
	for i := values.Length() - 1; i >= 0; i-- {
		candidate := strings.Join(strings.Fields(values.Eq(i).Text()), " ")
		for _, part := range strings.Split(candidate, ">") {
			value := strings.TrimSpace(part)
			if value == "" || value == "首页" || value == "正文" || value == articleTitle {
				continue
			}
			return value
		}
	}
	return ""
}

func rwxyHomeSectionsExist(doc *goquery.Document) bool {
	return doc != nil && doc.Find(".news .conts, .activity .conts, .dynamic .intrs").Length() > 0
}

func rwxyBlockedListResponse(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"404", "错误", "登录", "captcha", "验证码", "waf", "access denied"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	if doc.Find("form[action*='login'], form[action*='Login'], input[type='password'], .login-page, #login").Length() > 0 {
		return true
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
	for _, marker := range []string{
		"请输入验证码", "验证码下载附件", "请先登录", "请登录后", "页面不存在",
		"access denied", "just a moment", "cloudflare", "cf-chl-", "请求已被阻断",
		"安全威胁", "没有访问当前栏目的权限", "您没有访问",
	} {
		if strings.Contains(visible, marker) {
			return true
		}
	}
	return false
}

func parseRwxyHomeList(doc *goquery.Document, baseURL string) []model.NewsItem {
	if doc == nil {
		return nil
	}
	rows := doc.Find(".news .conts .item, .activity .conts .swiper-slide, .dynamic .intrs .list li")
	if rows.Length() == 0 {
		return nil
	}
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL := cleanURL(resolveURL(baseURL, href))
		if href == "" || !isValidSiteNewsURL(fullURL, baseURL) || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(row.Find(".tits a[href*='info/']").First().Text())
		if title == "" {
			title = strings.TrimSpace(anchor.AttrOr("title", ""))
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if !isValidNewsTitle(title) {
			return
		}
		seen[fullURL] = true
		date, publishedAt := rwxyHomeDate(row)
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        rwxyHomeType(row),
			ParsedAt:    time.Now(),
		})
	})
	return items
}

func rwxyHomeDate(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	if labs := row.Find(".labs").First(); labs.Length() > 0 {
		day := strings.TrimSpace(labs.Find("em").First().Text())
		year := strings.TrimSpace(labs.Find("b").First().Text())
		if day != "" && year != "" {
			if date, publishedAt := rwxyDateValues(year + "-" + day); date != "" {
				return date, publishedAt
			}
		}
	}
	for _, selector := range []string{".inform .time", ".time", ".date"} {
		if value := strings.TrimSpace(row.Find(selector).First().Text()); value != "" {
			if date, publishedAt := rwxyDateValues(value); date != "" {
				return date, publishedAt
			}
		}
	}
	return "", ""
}

func rwxyHomeType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	if parent := row.Closest(".intrs"); parent.Length() > 0 {
		if value := strings.TrimSpace(parent.Find(".title b").First().Text()); value != "" {
			return value
		}
	}
	if parent := row.Closest(".conts"); parent.Length() > 0 {
		if value := strings.TrimSpace(parent.Find(".title b").First().Text()); value != "" {
			return value
		}
	}
	if parent := row.Closest(".activity"); parent.Length() > 0 {
		if value := strings.TrimSpace(parent.Find(".title b").First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func rwxyPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, totalPages-page+1, extension)
}

func rwxyTotalPages(slug string) (int, error) {
	site := AllSites["rwxy"]
	if site == nil {
		return 0, fmt.Errorf("rwxy: site registration is missing")
	}
	body, err := fetch(rwxyPageURL(site.BaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		body, err = fetch(rwxyPageURL(site.BaseURL, slug, 1, 0, ".html"))
	}
	if err != nil {
		return 0, err
	}
	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if parseErr != nil {
		return 0, fmt.Errorf("rwxy: parse list page %q: %w", slug, parseErr)
	}
	if rwxyBlockedListResponse(doc) {
		return 0, fmt.Errorf("rwxy: list page %q is a login, WAF, or error page", slug)
	}
	if total := rwxyTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	if rwxyListContainerExists(doc) || rwxyHomeSectionsExist(doc) || rwxyHasArticleRows(body) {
		return 1, nil
	}
	return 0, fmt.Errorf("rwxy: list page %q has no total-page marker or article rows", slug)
}

func rwxyTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err == nil {
		maxPage := 0
		doc.Find(".p_pages a, .p_pages span, .pagination a, .pagination span").Each(func(_ int, sel *goquery.Selection) {
			value, parseErr := strconv.Atoi(strings.TrimSpace(sel.Text()))
			if parseErr == nil && value > maxPage {
				maxPage = value
			}
		})
		if maxPage > 0 {
			return maxPage
		}
	}
	if match := rwxyPageIndicatorRe.FindStringSubmatch(string(raw)); match != nil {
		value, _ := strconv.Atoi(match[1])
		return value
	}
	return 0
}

func rwxyHasArticleRows(raw []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return false
	}
	return doc.Find(".list-news > ul > li a[href*='info/'], .list-notice > ul > li a[href*='info/'], .news .conts .item a[href*='info/'], .activity .conts .swiper-slide a[href*='info/'], .dynamic .intrs .list li a[href*='info/']").Length() > 0
}
