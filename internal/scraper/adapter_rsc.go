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

const rscCanonicalBaseURL = "https://rsc.swjtu.edu.cn"

// These are the non-empty, local article lists confirmed from the RSC home
// page, navigation leaf pages, and their generated list pages.  Parent pages
// such as xwtz.htm and static/empty recruitment or policy pages are not feeds.
var rscSubPages = []string{
	"xwtz/xwNews", "xwtz/tzNotice", "rszp/shpyryzphgs",
	"xgxz", "xgxz/szrc", "xgxz/rszc", "xgxz/zcps", "xgxz/bsh", "xgxz/ldgz", "xgxz/shbx", "xgxz/zzkyjkyzl",
	"bszn", "bszn/szrc", "bszn/rszc", "bszn/bsh", "bszn/shbx", "cjwt",
}

var rscFeedTypes = map[string]string{
	"xwtz/xwNews":      "新闻News",
	"xwtz/tzNotice":    "通知Notice",
	"rszp/shpyryzphgs": "社会聘用人员招聘和公示",
	"xgxz":             "相关下载",
	"xgxz/szrc":        "师资人才",
	"xgxz/rszc":        "人事政策",
	"xgxz/zcps":        "职称评审",
	"xgxz/bsh":         "博士后",
	"xgxz/ldgz":        "劳动工资",
	"xgxz/shbx":        "社会保险",
	"xgxz/zzkyjkyzl":   "专职科研及科研助理",
	"bszn":             "办事指南",
	"bszn/szrc":        "师资人才",
	"bszn/rszc":        "人事政策",
	"bszn/bsh":         "博士后",
	"bszn/shbx":        "社会保险",
	"cjwt":             "常见问题",
}

const rscMaxResourceBytes int64 = 32 << 20

var (
	rscArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.(?:htm|html)$`)
	rscDateRE        = regexp.MustCompile(`20\d{2}\s*(?:[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}(?:日|号)?)(?:(?:[Tt]\s*|\s+)\d{1,2}:\d{2}(?::\d{2})?)?`)
	rscSourceRE      = regexp.MustCompile(`(?:信息来源|来源)\s*[:：]\s*(.*?)(?:\s*(?:日期|发布时间|发布日期|浏览次数)\s*[:：]?|$)`)
)

// rscAdapter keeps this site's HTML and resource rules independent from the
// other Boda sites even though the generated list URLs look similar.
func init() {
	if site := AllSites["rsc"]; site != nil {
		site.BaseURL = rscCanonicalBaseURL
	}
	registerAdapter("rsc", SiteAdapter{
		SubPages:     rscSubPages,
		ParseList:    parseAdapter_rscList,
		ParseArticle: parseAdapter_rscArticle,
		PageURL:      reverseBodaPageURL,
		FetchArticle: rscFetchArticle,
	})
}

func parseAdapter_rscList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("rsc: parse list HTML: %w", err)
	}
	if reason := rscBlockedReason(doc); reason != "" {
		return nil, fmt.Errorf("rsc: list is blocked or an error page (%s)", reason)
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", "")); pageType == "3" {
		return nil, fmt.Errorf("rsc: detail page supplied where a list was expected")
	}

	if rscIsHomepage(doc) {
		items := rscHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("rsc: homepage contains no local article entries")
		}
		return items, nil
	}

	root := doc.Find(".conlistComm").First()
	if root.Length() == 0 {
		return nil, fmt.Errorf("rsc: response contains no recognised article list")
	}
	rows := root.ChildrenFiltered("dl")
	if rows.Length() == 0 {
		rows = root.Find("dl")
	}
	if rows.Length() == 0 {
		// A generated page can be a legitimate empty list.  Keep it distinct
		// from a static page, which has no .conlistComm at all.
		return []model.NewsItem{}, nil
	}

	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		item, ok := rscListItem(row, baseURL)
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	})
	// A real list may currently contain only links to another site.  Returning
	// a non-nil empty slice prevents those external cards from becoming local
	// articles while allowing the feed to recover if the source changes.
	return items, nil
}

func parseAdapter_rscArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("rsc: parse article HTML: %w", err)
	}
	if reason := rscBlockedReason(doc); reason != "" {
		return nil, fmt.Errorf("rsc: article is blocked or an error page (%s)", reason)
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", "")); pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("rsc: response is not an article page (pageType=%q)", pageType)
	}

	body := rscArticleBody(doc)
	if body == nil || body.Length() == 0 {
		return nil, fmt.Errorf("rsc: article has no fixed .v_news_content body")
	}
	title := rscArticleTitle(doc)
	if title == "" || !isValidNewsTitle(title) {
		return nil, fmt.Errorf("rsc: article has no fixed title")
	}

	source, date, publishedAt := rscArticleMeta(doc)
	article := &model.Article{
		Title:       title,
		Source:      source,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        rscArticleType(doc, title),
		Content:     extractBodaContentText(body),
		Images:      rscContentImages(body, baseURL),
		Attachments: rscContentAttachments(doc, body, baseURL),
	}
	if html, htmlErr := body.Html(); htmlErr == nil {
		article.ContentHTML = html
	}
	return article, nil
}

func rscListItem(row *goquery.Selection, baseURL string) (model.NewsItem, bool) {
	if row == nil || row.Length() == 0 {
		return model.NewsItem{}, false
	}
	var articleAnchor *goquery.Selection
	row.Find("dt a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if resolved, ok := rscArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			clone := anchor.Clone()
			clone.Find("i, span, .date, .time, .state").Remove()
			if title := cleanTitle(strings.TrimSpace(firstNonEmpty(anchor.AttrOr("title", ""), clone.Text()))); isValidNewsTitle(title) {
				articleAnchor = anchor
				_ = resolved
				return false
			}
		}
		return true
	})
	if articleAnchor == nil || articleAnchor.Length() == 0 {
		return model.NewsItem{}, false
	}
	articleURL, ok := rscArticleURL(baseURL, articleAnchor.AttrOr("href", ""))
	if !ok {
		return model.NewsItem{}, false
	}
	title := strings.TrimSpace(articleAnchor.AttrOr("title", ""))
	if title == "" {
		clone := articleAnchor.Clone()
		clone.Find("i, span, .date, .time, .state").Remove()
		title = clone.Text()
	}
	title = cleanTitle(title)
	if !isValidNewsTitle(title) {
		return model.NewsItem{}, false
	}
	typ := rscRowType(row)
	date, publishedAt := rscDateValues(row.Find("dd").First())
	return model.NewsItem{
		Title:       title,
		URL:         articleURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        typ,
		ParsedAt:    time.Now(),
	}, true
}

func rscIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	return pageType == "1" || (doc.Find(".news-left, .news-right").Length() > 0 && doc.Find(".information-bd").Length() > 0)
}

func rscHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	appendRows := func(section *goquery.Selection, typ string, selector string) {
		section.Find(selector).Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			if item, ok := rscHomepageItem(anchor, row, typ, baseURL); ok && !seen[item.URL] {
				seen[item.URL] = true
				items = append(items, item)
			}
		})
	}
	doc.Find(".news-left").Each(func(_ int, section *goquery.Selection) {
		typ := rscCleanType(section.Find(".g-list-hd h3").First().Text())
		appendRows(section, typ, ".bd .tit, .ft li")
	})
	doc.Find(".news-right").Each(func(_ int, section *goquery.Selection) {
		typ := rscCleanType(section.Find(".g-list-hd h3").First().Text())
		appendRows(section, typ, ".news-right-bd li")
	})
	for _, selector := range []string{".information-bd > dl", ".guide-bd > dl", ".system-bd > dl"} {
		doc.Find(selector).Each(func(_ int, section *goquery.Selection) {
			typ := rscCleanType(section.Find("dt h3").First().Text())
			appendRows(section, typ, "dd")
		})
	}
	return items
}

func rscHomepageItem(anchor, row *goquery.Selection, typ, baseURL string) (model.NewsItem, bool) {
	articleURL, ok := rscArticleURL(baseURL, anchor.AttrOr("href", ""))
	if !ok {
		return model.NewsItem{}, false
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		clone := anchor.Clone()
		clone.Find("i, span, .date, .time, .state").Remove()
		title = clone.Text()
	}
	title = cleanTitle(title)
	if !isValidNewsTitle(title) {
		return model.NewsItem{}, false
	}
	date, publishedAt := rscDateValues(row)
	return model.NewsItem{Title: title, URL: articleURL, Date: date, PublishedAt: publishedAt, Type: rscCleanType(typ), ParsedAt: time.Now()}, true
}

func rscRowType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	var typ string
	row.Find("dt a[href], a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, article := rscArticleURL(rscCanonicalBaseURL, anchor.AttrOr("href", "")); article {
			return true
		}
		candidate := rscCleanType(anchor.Text())
		if candidate != "" && candidate != "MORE＋" && candidate != "MORE" {
			typ = candidate
			return false
		}
		return true
	})
	return typ
}

func rscDateValues(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	raw := strings.Join(strings.Fields(sel.Text()), " ")
	match := rscDateRE.FindString(raw)
	if match == "" {
		return "", ""
	}
	normalized := normalizePublishedAt(strings.TrimSpace(match))
	if len(normalized) < len("2006-01-02") {
		return "", ""
	}
	if publishedAtHasClock(normalized) {
		return normalized[:10], normalized
	}
	return normalized[:10], ""
}

func rscArticleTitle(doc *goquery.Document) string {
	for _, selector := range []string{".article > h1", ".article h1", ".article-tt", ".art-tit h1", ".art-tit h2", "h1.news_tit", ".news-title", ".article-title", "h1"} {
		if sel := doc.Find(selector).First(); sel.Length() > 0 {
			if title := cleanTitle(strings.TrimSpace(sel.Text())); title != "" {
				return title
			}
		}
	}
	return ""
}

func rscArticleBody(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{".article #vsb_content .v_news_content", "#vsb_content .v_news_content", ".article .v_news_content", ".v_news_content"} {
		if sel := doc.Find(selector).First(); sel.Length() > 0 {
			return sel
		}
	}
	return nil
}

func rscArticleType(doc *goquery.Document, title string) string {
	for _, selector := range []string{".main_titT h3", ".article-category", ".category", ".position a", ".loaction a"} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			candidate := rscCleanType(selection.Eq(i).Text())
			if candidate == "" || candidate == cleanTitle(title) || candidate == "首页" || candidate == "新闻通知" || candidate == "人事招聘" {
				continue
			}
			return candidate
		}
	}
	return ""
}

func rscArticleMeta(doc *goquery.Document) (source, date, publishedAt string) {
	var meta *goquery.Selection
	for _, selector := range []string{".news_source", ".article-sm", ".article-source", ".article .source"} {
		if sel := doc.Find(selector).First(); sel.Length() > 0 {
			meta = sel
			break
		}
	}
	if meta == nil || meta.Length() == 0 {
		return "", "", ""
	}
	metaText := strings.Join(strings.Fields(meta.Text()), " ")
	if match := rscSourceRE.FindStringSubmatch(metaText); len(match) > 1 {
		source = strings.TrimSpace(match[1])
	}
	if value := strings.TrimSpace(meta.Find(".date, time").First().Text()); value != "" {
		date, publishedAt = rscDateValues(goquerySingleText(value))
	}
	if date == "" {
		date, publishedAt = rscDateValues(goquerySingleText(metaText))
	}
	return source, date, publishedAt
}

// goquerySingleText makes a small selection for the shared date helper while
// keeping the article metadata search confined to the fixed meta block.
func goquerySingleText(text string) *goquery.Selection {
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader("<span>" + htmlstd.EscapeString(text) + "</span>"))
	if doc == nil {
		return nil
	}
	return doc.Find("span").First()
}

func rscContentImages(content *goquery.Selection, baseURL string) []string {
	images := make([]string, 0)
	seen := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		raw := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			if value := strings.TrimSpace(htmlstd.UnescapeString(image.AttrOr(attr, ""))); value != "" {
				raw = value
				break
			}
		}
		resolved := rscResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			if resolved == "" {
				image.Remove()
			}
			return
		}
		seen[resolved] = true
		images = append(images, resolved)
		image.SetAttr("src", resolved)
	})
	return images
}

func rscContentAttachments(doc *goquery.Document, content *goquery.Selection, baseURL string) []model.Attachment {
	attachments := make([]model.Attachment, 0)
	seen := make(map[string]bool)
	appendLink := func(link *goquery.Selection) {
		if link == nil || link.Length() == 0 {
			return
		}
		href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := rscResourceURL(baseURL, href)
		if resolved == "" || seen[resolved] {
			return
		}
		name := strings.TrimSpace(link.AttrOr("download", ""))
		if name == "" {
			name = strings.Join(strings.Fields(link.Text()), " ")
		}
		if name == "" {
			name = resolved
		}
		seen[resolved] = true
		attachments = append(attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	}
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) { appendLink(link) })
	for _, selector := range []string{".attachment a[href]", ".attachments a[href]", ".news-attachment a[href]", ".download a[href]", `form[name="_newscontent_fromname"] a[href]`} {
		doc.Find(selector).Each(func(_ int, link *goquery.Selection) { appendLink(link) })
	}
	return attachments
}

func rscFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := rscCanonicalBaseURL
	if site := AllSites["rsc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	canonical, ok := rscArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("rsc: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonical)
	if err != nil {
		return nil, nil, fmt.Errorf("rsc: fetch article %s: %w", canonical, err)
	}
	article, err := parseAdapter_rscArticle(body, baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("rsc: parse article %s: %w", canonical, err)
	}
	candidateImages := append([]string(nil), article.Images...)
	candidateAttachments := append([]model.Attachment(nil), article.Attachments...)
	article.Images = rscVerifyImages(candidateImages, baseURL, canonical)
	article.Attachments = rscVerifyAttachments(candidateAttachments, baseURL, canonical)
	allowedImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		allowedImages[image] = true
	}
	candidateAttachmentSet := make(map[string]bool, len(candidateAttachments))
	for _, attachment := range candidateAttachments {
		candidateAttachmentSet[attachment.URL] = true
	}
	allowedAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		allowedAttachments[attachment.URL] = true
	}
	article.ContentHTML = rscSanitizeContentHTML(article.ContentHTML, baseURL, allowedImages, candidateAttachmentSet, allowedAttachments)
	return article, body, nil
}

func rscVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved := rscResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := rscFetchResource(resolved, referer, true)
		if err != nil || !rscResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func rscVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := rscResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := rscFetchResource(resolved, referer, false)
		if err != nil || !rscResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func rscFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	baseURL := rscCanonicalBaseURL
	if parsed, err := url.Parse(strings.TrimSpace(referer)); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		baseURL = parsed.Scheme + "://" + parsed.Host
	}
	resolved := rscResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("rsc: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("rsc: create resource request: %w", err)
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
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if scheme := strings.ToLower(next.URL.Scheme); scheme != "http" && scheme != "https" {
			return fmt.Errorf("rsc: refusing non-HTTP resource redirect")
		}
		if !strings.EqualFold(next.URL.Hostname(), req.URL.Hostname()) {
			return fmt.Errorf("rsc: refusing cross-site resource redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("rsc: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("rsc: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	if response.ContentLength > rscMaxResourceBytes {
		return nil, contentType, fmt.Errorf("rsc: resource %s exceeds size limit", resolved)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, rscMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("rsc: read resource %s: %w", resolved, err)
	}
	if int64(len(body)) > rscMaxResourceBytes || !rscResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("rsc: resource %s returned invalid Content-Type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func rscResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" || (strings.ToLower(parsed.Scheme) != "http" && strings.ToLower(parsed.Scheme) != "https") {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(base.Hostname(), parsed.Hostname()) {
		return ""
	}
	pathQuery := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{"captcha", "verifycode", "verification", "createimage", "getcode", "login", "access-denied", "waf"} {
		if strings.Contains(pathQuery, marker) {
			return ""
		}
	}
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif", "/loading.gif"} {
		if strings.HasSuffix(strings.ToLower(parsed.Path), placeholder) {
			return ""
		}
	}
	parsed.Fragment = ""
	return parsed.String()
}

func rscArticleURL(baseURL, raw string) (string, bool) {
	resolved := rscResourceURL(baseURL, raw)
	if resolved == "" {
		return "", false
	}
	parsed, err := url.Parse(resolved)
	if err != nil || !rscArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), true
}

func rscResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 || int64(len(body)) > rscMaxResourceBytes {
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
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	lowerProbe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(probe))), "\ufeff")
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "请输入验证码", "验证码下载附件", "请先登录", "请登录后", "登录页面", "页面不存在", "404 not found", "access denied", "cloudflare", "just a moment", "安全验证", "请求已被阻断", "网站防火墙", "系统错误",
	} {
		if strings.Contains(lowerProbe, marker) {
			return false
		}
	}

	magic := rscResourceMagic(body)
	if image {
		return rscImageTypeMatchesMagic(mediaType, magic)
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
	case strings.Contains(lowerName, ".rtf") || mediaType == "application/rtf" || mediaType == "text/rtf":
		return bytes.HasPrefix(bytes.TrimSpace(body), []byte(`{\rtf`))
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return !bytes.Contains(body, []byte{0})
	default:
		return magic != ""
	}
}

func rscImageTypeMatchesMagic(mediaType, magic string) bool {
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

func rscResourceMagic(body []byte) string {
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
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	case bytes.Contains(bytes.ToLower(body[:rscMinInt(len(body), 4096)]), []byte("<svg")):
		return "svg"
	}
	return ""
}

func rscMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func rscSanitizeContentHTML(rawHTML, baseURL string, allowedImages, candidateAttachments, allowedAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="rsc-content-root">` + rawHTML + `</div>`))
	if err != nil {
		return ""
	}
	root := doc.Find("#rsc-content-root").First()
	root.Find("script, style, noscript").Remove()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			if value := strings.TrimSpace(htmlstd.UnescapeString(image.AttrOr(attr, ""))); value != "" {
				if candidate := rscResourceURL(baseURL, value); candidate != "" && allowedImages[candidate] {
					resolved = candidate
					break
				}
			}
		}
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(strings.ToLower(href), "#") && !strings.HasPrefix(strings.ToLower(href), "mailto:") && !strings.HasPrefix(strings.ToLower(href), "tel:") {
				link.RemoveAttr("href")
			}
			return
		}
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := rscResourceURL(baseURL, href)
		if resolved == "" {
			// A foreign file-like link is still an external link, not an RSC
			// attachment; retain it. Same-origin invalid candidates are removed.
			candidate := cleanURL(resolveURL(baseURL, href))
			if rscSameHost(baseURL, candidate) {
				link.Remove()
			}
			return
		}
		if candidateAttachments[resolved] && allowedAttachments[resolved] {
			link.SetAttr("href", resolved)
			return
		}
		link.Remove()
	})
	result, err := root.Html()
	if err != nil {
		return ""
	}
	return result
}

func rscSameHost(baseURL, candidate string) bool {
	base, baseErr := url.Parse(baseURL)
	parsed, parsedErr := url.Parse(candidate)
	return baseErr == nil && parsedErr == nil && base.Hostname() != "" && parsed.Hostname() != "" && strings.EqualFold(base.Hostname(), parsed.Hostname())
}

func rscBlockedReason(doc *goquery.Document) string {
	if doc == nil {
		return "nil document"
	}
	text := strings.ToLower(strings.Join(strings.Fields(doc.Find("body").Text()), " "))
	for _, marker := range []string{"请输入验证码", "安全验证", "access denied", "forbidden", "request blocked", "网站防火墙", "just a moment", "cloudflare", "页面不存在", "404 not found", "系统错误", "请先登录", "用户登录"} {
		if strings.Contains(text, marker) {
			return marker
		}
	}
	return ""
}

func rscPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".main_titT h3", ".main_titT", ".position a", ".loaction a"} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			candidate := rscCleanType(selection.Eq(i).Text())
			if candidate != "" && candidate != "首页" && candidate != "新闻通知" && candidate != "人事招聘" {
				return candidate
			}
		}
	}
	return ""
}

func rscCleanType(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "[]【】〖〗")
	if strings.Contains(value, ">") {
		parts := strings.Split(value, ">")
		value = strings.TrimSpace(parts[len(parts)-1])
	}
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "栏目："), "栏目:"))
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
