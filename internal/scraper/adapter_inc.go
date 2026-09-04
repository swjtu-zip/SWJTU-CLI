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

const incBaseURL = "https://inc.swjtu.edu.cn"

// INC exposes ordinary article lists together with CMS-backed service and
// download guides.  The latter are retained because their rows lead to real
// /info detail pages (and frequently contain the actual downloadable file),
// while profile/intro pages, external service links, and the English service
// guide pages are deliberately not feeds.
var incSubPages = []string{
	"xwdt",
	"tzgg",
	"wlaq",
	"zbrj",
	"djgz",
	"zcfg",
	"xsfw",
	"jsfw",
	"gdwfw",
	"wxwl",
	"english/About_Inc",
	"english/Campus_Network",
	"english/Service_Guide",
	"english/News_Event",
}

var (
	incArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	incCountRE       = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
	incDateTimeRE    = regexp.MustCompile(`20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?\s*(?:[Tt]|\s)+\s*[0-9]{1,2}\s*[:：]\s*[0-9]{2}(?:\s*[:：]\s*[0-9]{2})?`)
	incDateRE        = regexp.MustCompile(`20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?`)
	incFileExtRE     = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
)

func init() {
	if site := AllSites["inc"]; site != nil {
		site.BaseURL = incBaseURL
	}
	registerAdapter("inc", SiteAdapter{
		SubPages:     incSubPages,
		ParseList:    parseAdapter_incList,
		ParseArticle: parseAdapter_incArticle,
		PageURL:      incPageURL,
		ListPage:     incListPage,
		TotalPages:   incTotalPages,
		FetchArticle: incFetchArticle,
	})
}

// incPageURL maps the logical page order (newest first) to INC's reversed
// generated files: xwdt.htm, xwdt/11.htm, ..., xwdt/1.htm.
func incPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	physicalPage := totalPages - page + 1
	if physicalPage < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physicalPage, extension)
}

// incListPage owns requests so the site sees the same browser-style headers
// as a normal visit.  The source uses a reversed static pagination scheme,
// so the first page is read before the requested logical page is resolved.
func incListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("inc: page must be positive: %d", page)
	}
	firstURL := incPageURL(incBaseURL, slug, 1, 0, ".htm")
	firstBody, err := incFetchHTML(firstURL)
	if err != nil {
		return nil, 0, err
	}
	totalPages, err := incTotalPagesFromHTML(firstBody, slug)
	if err != nil {
		return nil, 0, err
	}
	if page > totalPages {
		return nil, 0, fmt.Errorf("inc: requested page %d beyond %d for %s", page, totalPages, slug)
	}
	if page == 1 {
		items, parseErr := parseAdapter_incList(firstBody, incBaseURL)
		return items, totalPages, parseErr
	}
	body, err := incFetchHTML(incPageURL(incBaseURL, slug, page, totalPages, ".htm"))
	if err != nil {
		return nil, 0, err
	}
	items, err := parseAdapter_incList(body, incBaseURL)
	return items, totalPages, err
}

func incTotalPages(slug string) (int, error) {
	body, err := incFetchHTML(incPageURL(incBaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, err
	}
	return incTotalPagesFromHTML(body, slug)
}

func incTotalPagesFromHTML(rawHTML []byte, slug string) (int, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(rawHTML)))
	if err != nil {
		return 0, fmt.Errorf("inc: parse list %q: %w", slug, err)
	}
	if err := incResponseError(doc); err != nil {
		return 0, err
	}
	if incIsHomepage(doc) {
		return 1, nil
	}
	if strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "3" {
		return 0, fmt.Errorf("inc: %s is an article page, not a list", slug)
	}
	rows := incListRows(doc)
	if rows.Length() == 0 && !incIsExplicitEmptyList(doc) {
		return 0, fmt.Errorf("inc: %s has no article rows or explicit empty marker", slug)
	}

	// INC exposes the logical page number as the anchor text.  The hrefs are
	// physical reverse-numbered files, so deriving the count from hrefs alone
	// would undercount by one (xwdt/1.htm is logical page 12).
	maxLogicalPage := 0
	doc.Find(".p_pages .p_no_d, .p_pages .p_no a, #div_list .p_no_d, #div_list .p_no a").Each(func(_ int, sel *goquery.Selection) {
		if page, convErr := strconv.Atoi(strings.TrimSpace(sel.Text())); convErr == nil && page > maxLogicalPage {
			maxLogicalPage = page
		}
	})
	if maxLogicalPage > 0 {
		return maxLogicalPage, nil
	}

	// A list with a counter but no numbered anchors is still unambiguous when
	// its first page exposes the fixed page size.
	if match := incCountRE.FindStringSubmatch(strings.Join(strings.Fields(doc.Text()), "")); match != nil {
		count, convErr := strconv.Atoi(match[1])
		if convErr == nil {
			if count == 0 || rows.Length() == 0 {
				return 1, nil
			}
			return (count + rows.Length() - 1) / rows.Length(), nil
		}
	}
	if rows.Length() > 0 || incIsExplicitEmptyList(doc) {
		return 1, nil
	}
	return 0, fmt.Errorf("inc: %s has no source pagination", slug)
}

func incFetchHTML(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("inc: create request %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", incBaseURL+"/")
	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inc: fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inc: fetch %s: status %d", rawURL, resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml+xml") {
		return nil, fmt.Errorf("inc: %s returned non-HTML content type %q", rawURL, contentType)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inc: read %s: %w", rawURL, err)
	}
	return body, nil
}

func incFetchArticle(articleURL string) (*model.Article, []byte, error) {
	body, err := incFetchHTML(articleURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_incArticle(body, incBaseURL)
	if err != nil {
		return nil, nil, err
	}
	return article, body, nil
}

func parseAdapter_incList(rawHTML []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(rawHTML)))
	if err != nil {
		return nil, fmt.Errorf("inc: parse list: %w", err)
	}
	if err := incResponseError(doc); err != nil {
		return nil, err
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")); pageType == "3" {
		return nil, fmt.Errorf("inc: article page was passed to list parser")
	}
	if incIsHomepage(doc) {
		items := incHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("inc: homepage has no local article modules")
		}
		return items, nil
	}
	rows := incListRows(doc)
	if rows.Length() == 0 {
		if incIsExplicitEmptyList(doc) {
			return []model.NewsItem{}, nil
		}
		return nil, fmt.Errorf("inc: page is not a supported article list")
	}

	typ := incListType(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").FilterFunction(func(_ int, selection *goquery.Selection) bool {
			return strings.TrimSpace(selection.AttrOr("title", "")) != "" || strings.TrimSpace(selection.Text()) != ""
		}).First()
		if anchor.Length() == 0 {
			anchor = row.Find("a[href]").First()
		}
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := incArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !incValidTitle(title) {
			return
		}
		date, publishedAt := incDateValues(row.Find("em, .list_time, .time, time, .date").First().Text())
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
	return items, nil
}

func incHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	add := func(row *goquery.Selection, typ string) {
		if row == nil || row.Length() == 0 {
			return
		}
		anchor := row.Find("a[href]").FilterFunction(func(_ int, selection *goquery.Selection) bool {
			return strings.TrimSpace(selection.AttrOr("title", "")) != "" || strings.TrimSpace(selection.Text()) != ""
		}).First()
		if anchor.Length() == 0 {
			anchor = row.Find("a[href]").First()
		}
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := incArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !incValidTitle(title) {
			return
		}
		date, publishedAt := incDateValues(row.Find(".new_text_min span, em, time, .date").Last().Text())
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	}
	for _, section := range []struct {
		selector string
		typ      string
	}{
		{selector: ".con_1 .ys1", typ: "新闻动态"},
		{selector: ".con_1 .ys2", typ: "通知公告"},
	} {
		sectionSel := doc.Find(section.selector).First()
		if sectionSel.Length() == 0 {
			continue
		}
		sectionSel.Find(".new_text").Each(func(_ int, row *goquery.Selection) { add(row, section.typ) })
		sectionSel.Find("ul.list_1 > li").Each(func(_ int, row *goquery.Selection) { add(row, section.typ) })
	}
	return items
}

func incListRows(doc *goquery.Document) *goquery.Selection {
	rows := doc.Find(".list_main_content ul.new_list_1 > li")
	if rows.Length() == 0 {
		rows = doc.Find("#div_list.newslist > ul > li")
	}
	return rows
}

func incListType(doc *goquery.Document) string {
	typ := strings.Join(strings.Fields(doc.Find(".on_page h3").First().Text()), " ")
	if typ == "" {
		typ = strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	}
	return typ
}

func incIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" ||
		(doc.Find(".con_1 .ys1").Length() > 0 && doc.Find(".con_1 .ys2").Length() > 0)
}

func incIsExplicitEmptyList(doc *goquery.Document) bool {
	if doc == nil || doc.Find(".list_main_content ul.new_list_1, #div_list.newslist > ul").Length() == 0 {
		return false
	}
	return incCountRE.MatchString(strings.Join(strings.Fields(doc.Text()), "")) &&
		strings.Contains(strings.Join(strings.Fields(doc.Text()), ""), "共0条")
}

func incResponseError(doc *goquery.Document) error {
	if doc == nil {
		return fmt.Errorf("inc: empty response")
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"access denied", "403 forbidden", "404 not found", "page not found", "页面不存在", "系统维护中", "请完成安全验证"} {
		if strings.Contains(title, marker) {
			return fmt.Errorf("inc: response is an error or verification page")
		}
	}
	text := strings.Join(strings.Fields(doc.Text()), " ")
	if strings.Contains(text, "请输入验证码下载附件") || strings.Contains(text, "请完成安全验证") {
		return fmt.Errorf("inc: response is an error or verification page")
	}
	return nil
}

func incValidTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) < 2 {
		return false
	}
	for _, skipped := range []string{"首页", "更多...", "more>>", "English", "正文", "登录", "注册"} {
		if strings.EqualFold(title, skipped) {
			return false
		}
	}
	return true
}

func incArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || !incArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	return fullURL, true
}

func incDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	candidate := incDateTimeRE.FindString(raw)
	if candidate == "" {
		candidate = incDateRE.FindString(raw)
	}
	if candidate == "" {
		return "", ""
	}
	normalized := normalizePublishedAt(candidate)
	if len(normalized) < len("2006-01-02") {
		return "", ""
	}
	return normalized[:10], normalized
}

func parseAdapter_incArticle(rawHTML []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(rawHTML)))
	if err != nil {
		return nil, fmt.Errorf("inc: parse article: %w", err)
	}
	if err := incResponseError(doc); err != nil {
		return nil, err
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")); pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("inc: page is not an article (pageType %q)", pageType)
	}
	scope := doc.Find(".art_con").First()
	content := scope.Find(".v_news_content").First()
	if scope.Length() == 0 || content.Length() == 0 {
		// English detail pages use a different outer shell, but retain the same
		// v_news_content body and pageType=3 marker.
		content = doc.Find(".v_news_content").First()
	}
	if content.Length() == 0 || (strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a").Length() == 0) {
		return nil, fmt.Errorf("inc: article has no usable body")
	}

	article, err := parseBodaArticle(rawHTML, baseURL)
	if err != nil {
		return nil, err
	}
	if title := strings.TrimSpace(scope.Find(".zwtitle").First().Text()); title != "" {
		article.Title = title
	} else if title := strings.TrimSpace(doc.Find(".newstitle").First().Text()); title != "" {
		article.Title = title
	}
	if article.Title == "" {
		return nil, fmt.Errorf("inc: article has no title")
	}
	article.Type = incArticleType(doc, "")

	// The common parser deliberately searches only Chinese labels. INC's
	// English detail uses [2018-11-07 ...], so extract the date from the
	// article metadata zone after removing the body (which may mention dates).
	metaScope := doc.Clone()
	metaScope.Find(".v_news_content, script, style, noscript").Remove()
	if date, publishedAt := incDateValues(flattenedText(metaScope.Find("body"))); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}

	// INC places the download list after .v_news_content rather than inside
	// it. Collect both locations, while accepting only explicit CMS download
	// links or known file extensions.
	seenAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seenAttachments[attachment.URL] = true
	}
	scopeForAttachments := doc.Find(".art_con").First()
	if scopeForAttachments.Length() == 0 {
		scopeForAttachments = doc.Find("#wenzhang_content").First()
	}
	scopeForAttachments.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		attachment, ok := incAttachment(anchor, baseURL)
		if !ok || seenAttachments[attachment.URL] {
			return
		}
		seenAttachments[attachment.URL] = true
		article.Attachments = append(article.Attachments, attachment)
	})

	if article.ContentHTML == "" {
		if html, htmlErr := content.Html(); htmlErr == nil {
			article.ContentHTML = html
		}
	}
	incNormalizeContentHTML(article, baseURL)
	if strings.TrimSpace(article.Content) == "" {
		article.Content = strings.TrimSpace(content.Text())
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func incArticleType(doc *goquery.Document, articleURL string) string {
	typ := strings.Join(strings.Fields(doc.Find(".on_page h3").First().Text()), " ")
	if typ != "" {
		return typ
	}
	keywords := strings.ToLower(doc.Find(`meta[name="keywords"]`).AttrOr("content", ""))
	if strings.Contains(keywords, "news event") {
		return "News Event"
	}
	if parsed, err := url.Parse(articleURL); err == nil {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 3 {
			if value := incColumnTypes[parts[1]]; value != "" {
				return value
			}
		}
	}
	return ""
}

var incColumnTypes = map[string]string{
	"1012": "网络安全",
	"1021": "新闻动态",
	"1022": "通知公告",
	"1023": "学生服务",
	"1031": "无线网络",
	"1046": "News Event",
	"1052": "正版软件",
	"1124": "政策法规",
	"1125": "党建工作",
}

func incAttachment(anchor *goquery.Selection, baseURL string) (model.Attachment, bool) {
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	name := strings.TrimSpace(anchor.AttrOr("download", ""))
	if name == "" {
		name = strings.TrimSpace(anchor.Text())
	}
	if name == "" {
		name = strings.TrimSpace(anchor.AttrOr("title", ""))
	}
	isDownload := strings.Contains(strings.ToLower(href), "download.jsp") || strings.Contains(strings.ToLower(href), "downloadattachurl")
	if !isDownload && !incFileExtRE.MatchString(href) && anchor.AttrOr("download", "") == "" && anchor.AttrOr("name", "") == "" {
		return model.Attachment{}, false
	}
	if href == "" || name == "" {
		return model.Attachment{}, false
	}
	return model.Attachment{Name: name, URL: cleanURL(resolveURL(baseURL, href))}, true
}

func incNormalizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="inc-content-root">` + article.ContentHTML + `</div>`))
	if err != nil {
		return
	}
	root := doc.Find("#inc-content-root").First()
	seenImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		seenImages[image] = true
	}
	root.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		resolved := cleanURL(resolveURL(baseURL, image.AttrOr("src", "")))
		if resolved == "" {
			return
		}
		image.SetAttr("src", resolved)
		if !seenImages[resolved] {
			seenImages[resolved] = true
			article.Images = append(article.Images, resolved)
		}
	})
	root.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(strings.ToLower(href), "javascript:") || strings.HasPrefix(strings.ToLower(href), "mailto:") {
			return
		}
		anchor.SetAttr("href", cleanURL(resolveURL(baseURL, href)))
	})
	if html, htmlErr := root.Html(); htmlErr == nil {
		article.ContentHTML = html
	}
}
