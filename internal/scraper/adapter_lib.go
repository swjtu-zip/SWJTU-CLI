package scraper

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const libCanonicalBaseURL = "https://www.lib.swjtu.edu.cn"

// libSubPages contains every article-bearing entry point found in the
// library portal's navigation, hidden tabs, and homepage modules.  Resource
// catalogs, service pages, authentication pages, and external databases are
// deliberately not feeds.
var libSubPages = []string{
	"home/service/1005/0", // 新闻动态, all categories
	"home/service/0/2564", // 资源发布
	"home/service/0/1165", // 通知公告
	"home/service/0/2561", // 最新动态
	"home/service/0/2563", // 公示
	"home/service/0/1006", // 图片新闻
	"lecture/1007",        // 讲座, full paginated list
	"news/newslist/1007",  // 讲座 tab (legacy entry point)
	"news/newslist/1020",  // 展览 tab
	"news/newslist/1021",  // 活动 tab
}

var (
	libArticlePathRE = regexp.MustCompile(`(?i)^/(?:home/servicedetail/[0-9]+|home/service/[0-9]+/[0-9]+|news/newsdetail/[0-9]+|lecture/detail/[0-9]+)$`)
	libPageCountRE   = regexp.MustCompile(`共\s*([0-9]+)\s*页`)
	libItemCountRE   = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
	libYearMonthRE   = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`)
	libDayRE         = regexp.MustCompile(`^[0-9]{1,2}$`)
	libDataJSONRE    = regexp.MustCompile(`(?s)var\s+datajson\s*=\s*(\[.*?\])\s*;`)
	libAttachmentRE  = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
)

// The portal is a Smart Library/VIPS application rather than one of the
// university's ordinary 博达 pages.  Keep all selectors and URL rules here;
// sharing the generic parser would turn service/resource links into news.
func init() {
	if site := AllSites["lib"]; site != nil {
		site.BaseURL = libCanonicalBaseURL
	}
	registerAdapter("lib", SiteAdapter{
		SubPages:     libSubPages,
		ParseList:    parseAdapter_libList,
		ParseArticle: parseAdapter_libArticle,
		PageURL:      libPageURL,
		TotalPages:   libTotalPages,
	})
}

// libPageURL uses the portal's query pagination.  The source numbers pages in
// the normal direction: page 1 is newest and the末页 link has the largest
// page number.  The extension argument is intentionally ignored because these
// routes are MVC endpoints, not generated .htm files.
func libPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	_ = baseURL
	_ = extension

	route := strings.Trim(strings.TrimSpace(slug), "/")
	if route == "" || route == "index" {
		if page != 1 {
			return ""
		}
		return libCanonicalBaseURL + "/"
	}
	if page < 1 || (page > 1 && totalPages < 1) || (totalPages > 0 && page > totalPages) {
		return ""
	}

	u, err := url.Parse("/" + route)
	if err != nil {
		return fmt.Sprintf("%s/%s?page=%d", libCanonicalBaseURL, route, page)
	}
	query := u.Query()
	query.Set("page", strconv.Itoa(page))
	u.RawQuery = query.Encode()
	return libCanonicalBaseURL + u.String()
}

// libTotalPages reads the first page and derives a page count from the
// portal's explicit 共N页 counter.  The hidden article count is only a
// fallback; it is divided by the observed row count so 132/27 page counts
// remain correct even when the visible page links are truncated.
func libTotalPages(slug string) (int, error) {
	listURL := libPageURL(libCanonicalBaseURL, slug, 1, 0, "")
	body, err := fetch(listURL)
	if err != nil {
		return 0, fmt.Errorf("fetch lib first page %q: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("parse lib first page %q: %w", slug, err)
	}
	if libBlockedResponse(body) {
		return 0, fmt.Errorf("lib page %q is a CAPTCHA/WAF or error response", slug)
	}
	if libIsSPAShell(doc) {
		return 0, fmt.Errorf("lib page %q is an unrendered SPA shell; article API response is unavailable", slug)
	}
	if slug == "" || slug == "index" {
		if !libIsHomepage(doc) {
			return 0, fmt.Errorf("lib homepage is not a supported portal page")
		}
		return 1, nil
	}
	list := libListContainer(doc)
	if list.Length() == 0 {
		return 0, fmt.Errorf("lib page %q is not an article list", slug)
	}
	if pages, known := libPageCount(doc); known {
		return pages, nil
	}
	if libExplicitEmptyList(doc) {
		return 1, nil
	}
	return 0, fmt.Errorf("lib page %q has article rows but no reliable page count", slug)
}

func parseAdapter_libList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if libBlockedResponse(raw) {
		return nil, fmt.Errorf("lib list is a CAPTCHA/WAF or error response")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse lib list: %w", err)
	}

	if libIsHomepage(doc) {
		items := parseLibHomepage(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("lib homepage contains no local article entries")
		}
		return items, nil
	}
	if libIsSPAShell(doc) {
		return nil, fmt.Errorf("lib page is an unrendered SPA shell; article API response is unavailable")
	}

	if libIsResourcePage(doc) {
		return nil, fmt.Errorf("lib page is a resource/service catalog, not an article list")
	}
	list := libListContainer(doc)
	if list.Length() == 0 {
		return nil, fmt.Errorf("lib page is not an article list (static/resource/login page)")
	}

	rows := list.Find("dl")
	if rows.Length() == 0 {
		if libExplicitEmptyList(doc) {
			return []model.NewsItem{}, nil
		}
		return nil, fmt.Errorf("lib recognized article list has no rows")
	}

	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	typ := libDocumentType(doc)
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("dt a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := libArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := libAnchorTitle(anchor)
		if !libIsArticleTitle(title) {
			return
		}
		date, publishedAt := libDateValues(row.Find(".time, .date, time, em").First())
		rowType := strings.TrimSpace(row.Find(".type").First().Text())
		if rowType == "" {
			rowType = typ
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        rowType,
			ParsedAt:    time.Now(),
		})
	})

	if len(items) == 0 {
		if libExplicitEmptyList(doc) {
			return []model.NewsItem{}, nil
		}
		return nil, fmt.Errorf("lib article list contains no local article rows")
	}
	return items, nil
}

func parseLibHomepage(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	add := func(anchor *goquery.Selection, title, rawDate, typ string) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		fullURL, ok := libArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		if title == "" {
			title = libAnchorTitle(anchor)
		} else {
			title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
		}
		if !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := libRawDateValues(rawDate)
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        strings.TrimSpace(typ),
			ParsedAt:    time.Now(),
		})
	}

	// 新闻动态 is a separate m-box.  Restrict extraction to that box so
	// database/resource/service cards cannot become fake articles.
	doc.Find("#part2 .m-box").Each(func(_ int, box *goquery.Selection) {
		heading := strings.Join(strings.Fields(box.Find(".m-box-title h2").First().Text()), " ")
		if strings.Contains(heading, "新闻动态") {
			box.Find(".news-list li").Each(func(_ int, row *goquery.Selection) {
				anchor := row.Find("a[href]").First()
				add(anchor, libAnchorTitle(anchor), row.Find(".time, .date, time").First().Text(), row.Find(".type").First().Text())
			})
			// The module also contains a carousel. Resource cards in the same
			// carousel are rejected by libArticleURL; local article cards are
			// real 图片新闻 entries and must not be lost.
			box.Find(".pic-show .pic-move li").Each(func(_ int, row *goquery.Selection) {
				anchor := row.Find("a[href]").First()
				add(anchor, libAnchorTitle(anchor), "", "图片新闻")
			})
			return
		}
		if !strings.Contains(heading, "讲座活动") {
			return
		}

		// The lecture tab is populated from a complete JSON array embedded in
		// the page, not just the first few visible rows.
		box.Find("script").Each(func(_ int, script *goquery.Selection) {
			match := libDataJSONRE.FindStringSubmatch(script.Text())
			if len(match) != 2 {
				return
			}
			var records []struct {
				Title    string `json:"title"`
				Datetime string `json:"datetime"`
				Href     string `json:"href"`
			}
			if json.Unmarshal([]byte(match[1]), &records) != nil {
				return
			}
			for _, record := range records {
				fullURL, ok := libArticleURL(baseURL, record.Href)
				if !ok || seen[fullURL] || !libIsArticleTitle(record.Title) {
					continue
				}
				date, publishedAt := libRawDateValues(record.Datetime)
				seen[fullURL] = true
				items = append(items, model.NewsItem{
					Title:       strings.Join(strings.Fields(record.Title), " "),
					URL:         fullURL,
					Date:        date,
					PublishedAt: publishedAt,
					Type:        "讲座",
					ParsedAt:    time.Now(),
				})
			}
		})

		box.Find(".hd-list2 .lecture-list dl, .hd-list2 .news-list dl").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("dt a[href]").First()
			add(anchor, libAnchorTitle(anchor), row.Find(".time, .date, time").First().Text(), "展览")
		})
		box.Find(".hd-list3 .lecture-list dl, .hd-list3 .news-list dl").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("dt a[href]").First()
			add(anchor, libAnchorTitle(anchor), row.Find(".time, .date, time").First().Text(), "活动")
		})
		box.Find(".hd-list3 .news-list li").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			add(anchor, libAnchorTitle(anchor), row.Find(".time, .date, time").First().Text(), "活动")
		})
	})

	return items
}

func parseAdapter_libArticle(raw []byte, baseURL string) (*model.Article, error) {
	if libBlockedResponse(raw) {
		return nil, fmt.Errorf("lib article is a CAPTCHA/WAF or error response")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse lib article: %w", err)
	}
	detail := doc.Find(".news-detail").First()
	if detail.Length() == 0 {
		return nil, fmt.Errorf("lib page is not an article detail")
	}
	contentSel := detail.Find(".news-content").First()
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("lib article has no news-content body")
	}
	title := strings.Join(strings.Fields(detail.Find("h1").First().Text()), " ")
	if !libIsArticleTitle(title) {
		return nil, fmt.Errorf("lib article has no valid title")
	}

	meta := detail.Find("em").First()
	date, publishedAt := libDateValues(meta)
	article := &model.Article{
		Title:       title,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        libDocumentType(doc),
		Source:      "图书馆",
	}
	meta.Find("span").Each(func(_ int, field *goquery.Selection) {
		text := strings.TrimSpace(field.Text())
		for _, prefix := range []string{"发布人：", "发布人:", "来源：", "来源:", "作者：", "作者:"} {
			if strings.HasPrefix(text, prefix) {
				value := strings.TrimSpace(strings.TrimPrefix(text, prefix))
				if value != "" && !strings.HasPrefix(prefix, "作者") {
					article.Source = value
				}
				if strings.HasPrefix(prefix, "作者") {
					article.Author = value
				}
				break
			}
		}
	})

	article.Content = strings.Join(strings.Fields(contentSel.Text()), " ")
	if html, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = html
	}

	seenImages := make(map[string]bool)
	contentSel.Find("img").Each(func(_ int, image *goquery.Selection) {
		var source string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				source = value
				break
			}
		}
		resolved := libImageURL(baseURL, source)
		if resolved == "" || seenImages[resolved] {
			return
		}
		seenImages[resolved] = true
		article.Images = append(article.Images, resolved)
		image.SetAttr("src", resolved)
	})

	seenAttachments := make(map[string]bool)
	appendAttachment := func(link *goquery.Selection) {
		if link == nil || link.Length() == 0 {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		resolved := libResolveURL(baseURL, href)
		if !libIsLocalURL(resolved) || seenAttachments[resolved] || !libLooksLikeAttachment(link, resolved) {
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
			name = libURLFilename(resolved)
		}
		if name == "" {
			return
		}
		seenAttachments[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	}
	contentSel.Find("a[href]").Each(func(_ int, link *goquery.Selection) { appendAttachment(link) })
	// Some versions render an attachment list beside the editor body.  Only
	// labeled/local links are eligible; the empty .media placeholder is not.
	detail.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if link.Closest(".news-content").Length() == 0 {
			appendAttachment(link)
		}
	})
	if html, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = html
	}

	normalizeArticlePublishedAt(article)
	return article, nil
}

func libListContainer(doc *goquery.Document) *goquery.Selection {
	return doc.Find(".main-content .news-list").First()
}

func libIsHomepage(doc *goquery.Document) bool {
	return doc.Find("#part2 .m-box").Length() > 0 && doc.Find("#header, #top-nav").Length() > 0
}

func libIsSPAShell(doc *goquery.Document) bool {
	if doc == nil || doc.Find("#home_page").Length() == 0 {
		return false
	}
	return doc.Find(".m-box, .main-content .news-list, .news-detail").Length() == 0
}

func libImageURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	for _, prefix := range []string{"data:", "javascript:", "file:", "blob:"} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	resolved := libResolveURL(baseURL, raw)
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	return resolved
}

func libIsResourcePage(doc *goquery.Document) bool {
	column := strings.TrimSpace(doc.Find("#pageColumn").First().Text())
	if strings.Contains(column, "快速链接") || strings.Contains(column, "古籍特藏") {
		return true
	}
	return doc.Find(".news-detail").Length() == 0 &&
		doc.Find("a[href*='/pagelinklog/'], a[href*='/link/']").Length() > 0 &&
		doc.Find(".main-content .news-list").Length() > 0
}

func libExplicitEmptyList(doc *goquery.Document) bool {
	text := strings.Join(strings.Fields(doc.Find(".pages, .pagination, .page, .news-list").Text()), " ")
	if strings.Contains(text, "暂无") || strings.Contains(text, "没有相关") {
		return true
	}
	return regexp.MustCompile(`共\s*0\s*(?:页|条)`).MatchString(text)
}

func libPageCount(doc *goquery.Document) (int, bool) {
	pageText := strings.Join(strings.Fields(doc.Find(".pages, .pagination, .page").Text()), " ")
	if match := libPageCountRE.FindStringSubmatch(pageText); len(match) == 2 {
		pages, _ := strconv.Atoi(match[1])
		return pages, true
	}

	// A few portal revisions omit the text counter but retain the hidden total
	// article count and 10 rows per page.
	countText := doc.Find("#hidPageCount").AttrOr("value", "")
	if countText == "" {
		countText = doc.Find("input[name='hidPageCount']").AttrOr("value", "")
	}
	if countText == "" {
		if match := libItemCountRE.FindStringSubmatch(pageText); len(match) == 2 {
			countText = match[1]
		}
	}
	count, err := strconv.Atoi(strings.TrimSpace(countText))
	rows := doc.Find(".main-content .news-list dl").Length()
	if err == nil && count >= 0 && rows > 0 {
		return (count + rows - 1) / rows, true
	}
	return 0, false
}

func libDocumentType(doc *goquery.Document) string {
	for _, selector := range []string{".main-site a", ".news-guid li.current a"} {
		if text := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); text != "" {
			return text
		}
	}
	text := strings.Join(strings.Fields(doc.Find("#pageColumn").First().Text()), " ")
	if text == "讲座展览活动" {
		return "讲座"
	}
	return text
}

func libAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	return strings.Join(strings.Fields(title), " ")
}

func libIsArticleTitle(title string) bool {
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	if len([]rune(title)) < 4 || strings.HasSuffix(title, ">>") {
		return false
	}
	switch title {
	case "首页", "更多", "查看更多", "登录", "注册", "搜索", "资源", "服务", "概况", "我的图书馆":
		return false
	}
	return true
}

func libDateValues(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	raw := strings.TrimSpace(sel.Text())
	date, publishedAt := libRawDateValues(raw)
	if date != "" {
		return date, publishedAt
	}
	// The list template concatenates <span>day</span> and YYYY-MM, so Text()
	// may be "132024-12" without a separator.
	month := libYearMonthRE.FindStringSubmatch(raw)
	day := strings.TrimSpace(sel.Find("span").First().Text())
	if len(month) == 3 && libDayRE.MatchString(day) {
		date = fmt.Sprintf("%s-%02s-%02s", month[1], month[2], day)
		return date, normalizePublishedAt(date)
	}
	return "", ""
}

func libRawDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	if match := dateTimeRe.FindString(raw); match != "" {
		if publishedAt := normalizePublishedAt(match); publishedAt != "" {
			return publishedAt[:10], publishedAt
		}
	}
	if match := bareDateRe.FindString(raw); match != "" {
		if publishedAt := normalizePublishedAt(match); publishedAt != "" {
			return publishedAt[:10], publishedAt
		}
	}
	return "", ""
}

func libArticleURL(baseURL, href string) (string, bool) {
	resolved := libResolveURL(baseURL, href)
	parsed, err := url.Parse(resolved)
	if err != nil || !libIsLocalURL(resolved) || !libArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	return resolved, true
}

func libResolveURL(baseURL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = libCanonicalBaseURL
	}
	// Do not use the shared cleanURL/resolveURL path here: its historical
	// TrimRight cleanup treats a trailing article ID such as /5887 as an
	// artifact and silently drops the final digit.
	href = strings.TrimSuffix(strings.TrimSuffix(href, "%7D"), "}")
	base, baseErr := url.Parse(baseURL)
	ref, refErr := url.Parse(href)
	if baseErr != nil || refErr != nil {
		return href
	}
	resolved := base.ResolveReference(ref).String()
	parsed, err := url.Parse(resolved)
	if err != nil {
		return resolved
	}
	if strings.EqualFold(parsed.Hostname(), "www.lib.swjtu.edu.cn") {
		parsed.Scheme = "https"
		parsed.Host = "www.lib.swjtu.edu.cn"
		return parsed.String()
	}
	return resolved
}

func libIsLocalURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && strings.EqualFold(parsed.Hostname(), "www.lib.swjtu.edu.cn")
}

func libLooksLikeAttachment(link *goquery.Selection, resolved string) bool {
	href := strings.TrimSpace(link.AttrOr("href", ""))
	text := strings.TrimSpace(link.Text())
	class := strings.ToLower(link.AttrOr("class", ""))
	if link.AttrOr("download", "") != "" || libAttachmentRE.MatchString(resolved) || libAttachmentRE.MatchString(href) {
		return true
	}
	if strings.Contains(text, "附件") || strings.Contains(class, "attach") || strings.Contains(class, "download") {
		return strings.Contains(strings.ToLower(href), "download") || strings.Contains(strings.ToLower(href), "file") || strings.Contains(strings.ToLower(href), "attach")
	}
	return false
}

func libURLFilename(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func libBlockedResponse(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	for _, marker := range []string{
		"cf-chl-", "challenge-platform", "turnstile", "captcha", "验证码",
		"安全验证", "access denied", "just a moment",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
