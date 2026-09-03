package scraper

import (
	"fmt"
	stdhtml "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const jjghCanonicalBaseURL = "https://jjgh.swjtu.edu.cn"

// jjghSubPages is the complete set of article-bearing list pages found by
// walking jjgh's top navigation, side tabs and homepage modules. The three
// other policy tabs and 学习园地 are real empty list shells, but are not
// registered as feeds because they contain no articles.
var jjghSubPages = []string{
	"index/xwdt",
	"zbjs",
	"gggs",
	"zcfg/gjxgfgzd",
	"zcfg/xxxggzzd",
	"gzcx/gcxm",
	"tszs",
}

var (
	jjghArticlePathRE  = regexp.MustCompile(`(?i)^/info/\d+/\d+\.(?:html?|shtml)$`)
	jjghPageNumberRE   = regexp.MustCompile(`^\s*(\d+)\s*$`)
	jjghDateTimeRE     = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*(?:日|号)?\s*(?:[Tt]|\s)+(\d{1,2})\s*[:：](\d{2})(?:\s*[:：](\d{2}))?`)
	jjghDateRE         = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*(?:日|号)?`)
	jjghYearMonthRE    = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})`)
	jjghAttachmentRE   = regexp.MustCompile(`(?i)\.(?:doc|docx|xls|xlsx|ppt|pptx|pdf|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#&])`)
	jjghAttachmentName = regexp.MustCompile(`(?i)(?:^|[/\\])([^/\\?#]+\.(?:doc|docx|xls|xlsx|ppt|pptx|pdf|zip|rar|7z|txt|csv|wps|rtf))(?:$|[?#&])`)
)

func init() {
	if site := AllSites["jjgh"]; site != nil {
		site.BaseURL = jjghCanonicalBaseURL
	}
	registerAdapter("jjgh", SiteAdapter{
		SubPages:     jjghSubPages,
		ParseList:    parseAdapter_jjghList,
		ParseArticle: parseAdapter_jjghArticle,
		PageURL:      jjghPageURL,
		TotalPages:   jjghTotalPages,
	})
}

// jjghPageURL maps logical pages to the generated files used by the site.
// The unsuffixed page is logical page 1; for a seven-page list, logical page 2
// is /6.htm and the last page is /1.htm.
func jjghPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		return ""
	}
	if slug == "" || slug == "index" || slug == "home" {
		if page != 1 {
			return ""
		}
		return baseURL + "/index" + extension
	}
	if page == 1 {
		return baseURL + "/" + slug + extension
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	filePage := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func jjghTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" || slug == "home" {
		return 1, nil
	}
	if !jjghKnownFeed(slug) {
		return 0, fmt.Errorf("jjgh: %q is not an article-list route", slug)
	}
	site := AllSites["jjgh"]
	if site == nil {
		return 0, fmt.Errorf("jjgh: site is not registered")
	}
	body, err := fetch(jjghPageURL(site.BaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, err
	}
	if err := jjghResponseError(body); err != nil {
		return 0, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("jjgh: parse page count: %w", err)
	}
	if !jjghListShell(doc) {
		return 0, fmt.Errorf("jjgh: %s is not an article list", slug)
	}
	if total := jjghPageCount(doc); total > 0 {
		return total, nil
	}
	return 0, fmt.Errorf("jjgh: %s has no recognized pagination", slug)
}

func jjghKnownFeed(slug string) bool {
	for _, known := range jjghSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func jjghPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		value := strings.TrimSpace(sel.Text())
		match := jjghPageNumberRE.FindStringSubmatch(value)
		if match == nil {
			return
		}
		page, err := strconv.Atoi(match[1])
		if err == nil && page > maxPage {
			maxPage = page
		}
	})
	if maxPage > 0 {
		return maxPage
	}
	if doc.Find(".p_pages").Length() > 0 {
		return 1
	}
	return 0
}

func parseAdapter_jjghList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if err := jjghResponseError(raw); err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("jjgh: parse list HTML: %w", err)
	}

	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
	if pageType == "1" || jjghHomepage(doc) {
		items := jjghHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("jjgh: homepage contains no local article entries")
		}
		return items, nil
	}
	if pageType != "" && pageType != "2" {
		return nil, fmt.Errorf("jjgh: page is not an article list (pageType %q)", pageType)
	}
	if !jjghListShell(doc) {
		return nil, fmt.Errorf("jjgh: page is not a supported article list")
	}

	typ := jjghListType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".list-grid > .list-cell, .list-grid > .list-box > .list-col").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		fullURL, ok := jjghArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := jjghAnchorTitle(anchor)
		if title == "" || !jjghValidTitle(title) || len([]rune(title)) > 200 {
			return
		}
		date, publishedAt := jjghDateValues(row.Find(".list-time, .list-col-time, time, .date, .time").First().Text())
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
	// An identified list shell with no rows is a legal empty list. It is not
	// registered in jjghSubPages, but returning an empty result here preserves
	// the distinction between an empty source list and a static/error page.
	return items, nil
}

func jjghHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".index-tabs, .index_ban, .index-tell").Length() > 0
}

func jjghHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	add := func(anchor *goquery.Selection, title, date, typ string) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		fullURL, ok := jjghArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		title = cleanTitle(strings.Join(strings.Fields(strings.TrimSpace(title)), " "))
		if title == "" {
			title = jjghAnchorTitle(anchor)
		}
		if title == "" || !jjghValidTitle(title) || len([]rune(title)) > 200 {
			return
		}
		typ = strings.TrimSpace(typ)
		if typ == "工程公告" {
			typ = "公告公示"
		}
		if index, exists := seen[fullURL]; exists {
			current := &items[index]
			if current.Date == "" && date != "" {
				current.Date = date
				current.PublishedAt = normalizePublishedAt(date)
			}
			if current.Type == "" && typ != "" {
				current.Type = typ
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: normalizePublishedAt(date),
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	}

	// The two tabs are hidden until clicked in the browser. Their dates are
	// split into day and year-month nodes, so parse them before the duplicate
	// carousel and block modules can obscure the precise date.
	tabTypes := make(map[string]string)
	doc.Find(".index-tabs .nav-tabs a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimPrefix(strings.TrimSpace(anchor.AttrOr("href", "")), "#")
		if href != "" {
			tabTypes[href] = strings.Join(strings.Fields(anchor.Text()), " ")
		}
	})
	doc.Find(".index-tabs .tab-pane").Each(func(_ int, pane *goquery.Selection) {
		typ := tabTypes[pane.AttrOr("id", "")]
		pane.Find(".index-tabs-cell").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			add(anchor, jjghAnchorTitle(anchor), jjghHomeDate(row), typ)
		})
	})

	// Each .index-tell is a separate homepage list module. Scope rows to that
	// module so repeated links from the other blocks do not leak their type.
	doc.Find(".index-tell").Each(func(_ int, block *goquery.Selection) {
		typ := strings.Join(strings.Fields(block.Find(".list-head p").First().Text()), " ")
		block.Find(".list-cell, .list-box > .list-col").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			date, _ := jjghDateValues(row.Find(".list-time, .list-col-time, time").First().Text())
			add(anchor, jjghAnchorTitle(anchor), date, typ)
		})
	})

	// The carousel has no date in its markup. Keep its local article links so
	// no homepage-only item is silently lost; the subsequent detail fetch will
	// supply the authoritative date when the archive processes it.
	doc.Find(".swiper-slide .index-ban-tt a[href], .index_ban .index-ban-tt a[href]").Each(func(_ int, anchor *goquery.Selection) {
		add(anchor, jjghAnchorTitle(anchor), "", "基建动态")
	})
	return items
}

func jjghListShell(doc *goquery.Document) bool {
	if doc == nil || doc.Find(".list-grid").Length() == 0 {
		return false
	}
	return doc.Find(".p_pages, .pb_sys_common").Length() > 0
}

func jjghListType(doc *goquery.Document) string {
	for _, selector := range []string{".list-head p", ".navleft-hd p", `meta[name="pageTitle"]`} {
		if selector == `meta[name="pageTitle"]` {
			if value := strings.TrimSpace(doc.Find(selector).AttrOr("content", "")); value != "" {
				return value
			}
			continue
		}
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func jjghArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(stdhtml.UnescapeString(href))
	if href == "" {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, href))
	u, err := url.Parse(fullURL)
	if err != nil || u.Host == "" || !jjghArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(u.Host, base.Host) {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func jjghAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if title := strings.TrimSpace(anchor.AttrOr("title", "")); title != "" {
		return cleanTitle(title)
	}
	return cleanTitle(strings.Join(strings.Fields(anchor.Text()), " "))
}

// jjghValidTitle keeps the shared navigation filters but allows a legitimate
// Chinese headline to end with a closing book-title mark. The generic helper
// treats every trailing "》" as navigation noise; jjgh's live zbjs feed has
// real article titles such as ...《志愿军：存亡之战》.
func jjghValidTitle(title string) bool {
	if strings.HasSuffix(title, "》") {
		return isValidNewsTitle(strings.TrimSpace(strings.TrimSuffix(title, "》")))
	}
	return isValidNewsTitle(title)
}

func jjghHomeDate(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	day := strings.TrimSpace(scope.Find(".index-tabs-day").First().Text())
	yearMonth := strings.TrimSpace(scope.Find(".index-tabs-year").First().Text())
	if day != "" && yearMonth != "" {
		if match := jjghYearMonthRE.FindStringSubmatch(yearMonth); match != nil {
			return normalizeDateParts(match[1], match[2], day)
		}
	}
	date, _ := jjghDateValues(scope.Text())
	return date
}

func jjghDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(stdhtml.UnescapeString(strings.TrimSpace(raw))), " ")
	if raw == "" {
		return "", ""
	}
	if match := jjghDateTimeRE.FindStringSubmatch(raw); match != nil {
		second := match[6]
		if second == "" {
			second = "00"
		}
		published := fmt.Sprintf("%s-%02s-%02s %02s:%s:%s", match[1], match[2], match[3], match[4], match[5], second)
		return published[:10], published
	}
	if match := jjghDateRE.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func parseAdapter_jjghArticle(raw []byte, baseURL string) (*model.Article, error) {
	if err := jjghResponseError(raw); err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("jjgh: parse article HTML: %w", err)
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
	if pageType != "3" {
		return nil, fmt.Errorf("jjgh: page is not an article (pageType %q)", pageType)
	}
	title := cleanTitle(strings.Join(strings.Fields(doc.Find(".deta-tt").First().Text()), " "))
	if title == "" {
		return nil, fmt.Errorf("jjgh: article has no deta-tt title")
	}
	content := doc.Find(".content14 .v_news_content").First()
	if content.Length() == 0 {
		return nil, fmt.Errorf("jjgh: article has no content14/v_news_content container")
	}
	if strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a").Length() == 0 {
		return nil, fmt.Errorf("jjgh: article has empty content")
	}

	date, publishedAt := jjghDateValues(doc.Find(".deta-fun .deta-time").First().Text())
	typ := jjghArticleType(doc)
	article := &model.Article{Title: title, Date: date, PublishedAt: publishedAt, Type: typ}

	content.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		resolved := cleanURL(resolveURL(baseURL, image.AttrOr("src", "")))
		if resolved == "" {
			return
		}
		if !jjghContainsString(article.Images, resolved) {
			article.Images = append(article.Images, resolved)
		}
		image.SetAttr("src", resolved)
	})

	// This template places download.jsp links beside the v_news_content div,
	// under the article's .list-grid. Scan that bounded article zone so the
	// breadcrumb/header links cannot become attachments.
	attachmentScope := doc.Find(".list-grid").First()
	if attachmentScope.Length() == 0 {
		attachmentScope = content
	}
	seenAttachments := make(map[string]bool)
	for _, attachment := range article.Attachments {
		seenAttachments[attachment.URL] = true
	}
	attachmentScope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(stdhtml.UnescapeString(anchor.AttrOr("href", "")))
		if !jjghAttachmentLink(anchor, href) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		u, parseErr := url.Parse(resolved)
		if parseErr != nil || u.Host == "" {
			return
		}
		base, baseErr := url.Parse(baseURL)
		if baseErr != nil || !strings.EqualFold(u.Host, base.Host) {
			return
		}
		anchor.SetAttr("href", resolved)
		if seenAttachments[resolved] {
			return
		}
		name := jjghAttachmentDisplayName(anchor, resolved)
		if name == "" {
			return
		}
		seenAttachments[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})

	var parts []string
	content.Find("p, h1, h2, h3, h4").Each(func(_ int, node *goquery.Selection) {
		if value := strings.Join(strings.Fields(node.Text()), " "); value != "" {
			parts = append(parts, value)
		}
	})
	if len(parts) == 0 {
		if value := strings.Join(strings.Fields(content.Text()), " "); value != "" {
			parts = append(parts, value)
		}
	}
	article.Content = strings.Join(parts, "\n\n")
	if html, htmlErr := content.Html(); htmlErr == nil {
		article.ContentHTML = html
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func jjghArticleType(doc *goquery.Document) string {
	typ := ""
	doc.Find(".post .container a[href]").Each(func(_ int, anchor *goquery.Selection) {
		value := strings.Join(strings.Fields(anchor.Text()), " ")
		if value != "" && value != "首页" {
			typ = value
		}
	})
	if typ != "" {
		return typ
	}
	return jjghListType(doc)
}

func jjghAttachmentLink(anchor *goquery.Selection, href string) bool {
	if anchor == nil || anchor.Length() == 0 || href == "" {
		return false
	}
	lower := strings.ToLower(href)
	if strings.Contains(lower, "downloadattachurl") {
		return true
	}
	if strings.Contains(lower, "download.jsp") {
		text := strings.Join(strings.Fields(anchor.Text()), " ")
		return strings.Contains(text, "附件") || anchor.AttrOr("download", "") != "" || anchor.AttrOr("name", "") != ""
	}
	if anchor.AttrOr("download", "") != "" || anchor.AttrOr("name", "") != "" {
		return true
	}
	return jjghAttachmentRE.MatchString(lower)
}

func jjghAttachmentDisplayName(anchor *goquery.Selection, resolved string) string {
	for _, value := range []string{anchor.AttrOr("download", ""), anchor.AttrOr("name", ""), anchor.Text(), anchor.AttrOr("title", "")} {
		if value = strings.Join(strings.Fields(stdhtml.UnescapeString(strings.TrimSpace(value))), " "); value != "" {
			return value
		}
	}
	if match := jjghAttachmentName.FindStringSubmatch(resolved); match != nil {
		return match[1]
	}
	return ""
}

func jjghResponseError(raw []byte) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("jjgh: parse response HTML: %w", err)
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"captcha", "验证码", "登录", "login", "403", "404", "500", "error", "错误", "禁止访问"} {
		if strings.Contains(title, marker) {
			return fmt.Errorf("jjgh: error/login/verification page (%s)", title)
		}
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
	for _, marker := range []string{"请输入验证码下载附件", "请求已被阻断", "安全威胁", "没有访问当前栏目的权限", "您没有访问", "请先登录", "请登录后", "禁止访问"} {
		if strings.Contains(visible, marker) {
			return fmt.Errorf("jjgh: error/login/verification page")
		}
	}
	return nil
}

func jjghContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
