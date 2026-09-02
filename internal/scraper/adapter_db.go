package scraper

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const dbBaseURL = "https://db.swjtu.edu.cn"

// dbSubPages contains the article-bearing pages exposed by the site's visible
// navigation.  The profile pages under bmgk are static pages; the three
// download pages under bmgz are article lists whose content.jsp details carry
// the actual files, so they must remain separate feeds.
var dbSubPages = []string{
	"xwdt",
	"tzgg",
	"bmgz",
	"bmgz/bmdt",
	"bmgz/jzzm",
	"bmgz/bmgzbslc",
	"bmgz/1796",
	"bmgz/1806",
	"bmgz/1816",
	"fwzn",
	"gzzd",
}

type dbFeedRoute struct {
	path    string
	treeID  string
	reverse bool
}

var dbRoutes = map[string]dbFeedRoute{
	"index":         {path: "index.htm"},
	"xwdt":          {path: "xwdt.htm", reverse: true},
	"tzgg":          {path: "tzgg.htm"},
	"bmgz":          {path: "bmgz.htm"},
	"bmgz/bmdt":     {path: "bmgz/bmdt.htm"},
	"bmgz/jzzm":     {path: "bmgz/jzzm.htm"},
	"bmgz/bmgzbslc": {path: "bmgz/bmgzbslc.htm"},
	"bmgz/1796":     {treeID: "1796"},
	"bmgz/1806":     {treeID: "1806"},
	"bmgz/1816":     {treeID: "1816"},
	"fwzn":          {path: "fwzn.htm"},
	"gzzd":          {path: "gzzd.htm"},
}

var (
	dbArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	dbDateTimeRE    = regexp.MustCompile(`日期\s*[:：]\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})(?:日|号)?(?:\s+|[Tt]+)(\d{1,2}):(\d{2})(?::(\d{2}))?`)
	dbDateRE        = regexp.MustCompile(`日期\s*[:：]\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
	dbListDateRE    = regexp.MustCompile(`时间\s*[:：]\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
	dbBareDateRE    = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
	dbPageNumberRE  = regexp.MustCompile(`^\s*(\d+)\s*$`)
)

func init() {
	if site := AllSites["db"]; site != nil {
		site.BaseURL = dbBaseURL
	}
	registerAdapter("db", SiteAdapter{
		SubPages:  dbSubPages,
		ParseList: parseAdapter_dbList,
		// ListPage handles both the reverse-numbered static archive and the
		// list.jsp query pagination used by the download tabs.
		ListPage:     dbListPage,
		ParseArticle: parseAdapter_dbArticle,
	})
}

func parseAdapter_dbList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse db list: %w", err)
	}
	if dbLooksLikeBlockedPage(doc) {
		return nil, fmt.Errorf("db list is an error or verification page")
	}

	if dbIsHomepage(doc) {
		items := parseDBHomepage(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("db homepage contains no article entries")
		}
		return items, nil
	}

	rows := doc.Find(".conlistComm dl")
	if rows.Length() == 0 {
		return nil, fmt.Errorf("db page is not a supported article list")
	}
	items := parseDBRows(rows, baseURL, dbListType(doc))
	return items, nil
}

func dbIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	return pageType == "1" || (doc.Find(".news-list").Length() > 0 && doc.Find(".notice-list").Length() > 0)
}

func parseDBHomepage(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	addSection := func(section *goquery.Selection) {
		typ := dbCompactText(section.Find(".title h3").First().Text())
		typ = strings.TrimSpace(strings.TrimSuffix(typ, "More+"))
		section.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			fullURL, ok := dbArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok || seen[fullURL] {
				return
			}
			title := strings.TrimSpace(anchor.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(anchor.Text())
			}
			title = cleanTitle(title)
			if !dbValidTitle(title) {
				return
			}
			dateText := anchor.Closest("li").Find(".time, time, .date").First().Text()
			if dateText == "" {
				dateText = anchor.Closest("li").Text()
			}
			date, publishedAt := dbListDate(dateText)
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
	}

	// The home page has two independent article modules.  Keep them scoped so
	// footer links, carousel slides and the search widget cannot become news.
	if section := doc.Find(".fl-box.news").First(); section.Length() > 0 {
		addSection(section)
	}
	if section := doc.Find(".fr-box.notice").First(); section.Length() > 0 {
		addSection(section)
	}
	return items
}

func parseDBRows(rows *goquery.Selection, baseURL, pageType string) []model.NewsItem {
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		var anchor *goquery.Selection
		row.Find("dt a[href]").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
			if _, ok := dbArticleURL(baseURL, candidate.AttrOr("href", "")); ok {
				anchor = candidate
				return false
			}
			return true
		})
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		fullURL, ok := dbArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if !dbValidTitle(title) {
			return
		}
		date, publishedAt := dbListDate(row.Text())
		typ := pageType
		// The parent bmgz aggregation contains both 保密动态 and 警钟长鸣;
		// retain the row's own category instead of flattening it to 保密工作.
		if category := dbCategoryText(row); category != "" {
			if typ == "" || typ == "保密工作" {
				typ = category
			}
		}
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

func dbListType(doc *goquery.Document) string {
	typ := strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	if typ != "" {
		return typ
	}
	return dbCompactText(doc.Find(".current h3").First().Text())
}

func dbCategoryText(row *goquery.Selection) string {
	category := dbCompactText(row.Find("dt span a").First().Text())
	category = strings.TrimSpace(strings.Trim(category, "[]【】"))
	return category
}

func dbListDate(text string) (string, string) {
	match := dbListDateRE.FindStringSubmatch(strings.Join(strings.Fields(text), " "))
	if match == nil {
		match = dbBareDateRE.FindStringSubmatch(strings.Join(strings.Fields(text), " "))
	}
	if match == nil {
		return "", ""
	}
	date := normalizeDateParts(match[1], match[2], match[3])
	return date, date
}

func dbCompactText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func dbValidTitle(title string) bool {
	return title != "" && len([]rune(title)) <= 200 && isValidNewsTitle(title)
}

func dbArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") || strings.HasPrefix(href, "#") {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || extractDomain(parsed.String()) != extractDomain(baseURL) {
		return "", false
	}
	if dbArticlePathRE.MatchString(parsed.Path) {
		return fullURL, true
	}
	if strings.EqualFold(parsed.Path, "/content.jsp") &&
		parsed.Query().Get("urltype") == "news.NewsContentUrl" &&
		parsed.Query().Get("wbtreeid") != "" && parsed.Query().Get("wbnewsid") != "" {
		return fullURL, true
	}
	return "", false
}

// dbListPage fetches one logical page.  xwdt is reverse-numbered (the source
// advertises page 2 as xwdt/1.htm), while the three list.jsp feeds use
// PAGENUM/totalpage query parameters.
func dbListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("db: page must be positive")
	}
	route, ok := dbRoutes[slug]
	if !ok {
		return nil, 0, fmt.Errorf("db: unknown feed %q", slug)
	}
	baseURL := dbSiteBaseURL()
	firstURL := dbRouteURL(baseURL, route, 1, 0)
	firstBody, err := fetch(firstURL)
	if err != nil {
		return nil, 0, err
	}
	totalPages := dbTotalPagesFromHTML(firstBody)
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		return nil, totalPages, fmt.Errorf("db: page %d is out of range (total %d)", page, totalPages)
	}
	body := firstBody
	if page > 1 {
		pageURL := dbRouteURL(baseURL, route, page, totalPages)
		body, err = fetch(pageURL)
		if err != nil {
			return nil, totalPages, err
		}
	}
	items, err := parseAdapter_dbList(body, baseURL)
	if err != nil {
		return nil, totalPages, err
	}
	if len(items) == 0 && page > 1 {
		return nil, totalPages, fmt.Errorf("db: feed %q page %d/%d returned no article rows", slug, page, totalPages)
	}
	return items, totalPages, nil
}

func dbSiteBaseURL() string {
	if site := AllSites["db"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return dbBaseURL
}

func dbRouteURL(baseURL string, route dbFeedRoute, page, totalPages int) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if route.treeID != "" {
		u, _ := url.Parse(baseURL + "/list.jsp")
		query := u.Query()
		query.Set("urltype", "tree.TreeTempUrl")
		query.Set("wbtreeid", route.treeID)
		if page > 1 {
			query.Set("totalpage", strconv.Itoa(totalPages))
			query.Set("PAGENUM", strconv.Itoa(page))
		}
		u.RawQuery = query.Encode()
		return u.String()
	}
	if page <= 1 || !route.reverse || totalPages <= 1 {
		return baseURL + "/" + strings.TrimLeft(route.path, "/")
	}
	filePage := totalPages - page + 1
	path := strings.TrimSuffix(route.path, ".htm")
	return fmt.Sprintf("%s/%s/%d.htm", baseURL, path, filePage)
}

func dbTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0
	}
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		match := dbPageNumberRE.FindStringSubmatch(sel.Text())
		if match == nil {
			return
		}
		page, _ := strconv.Atoi(match[1])
		if page > maxPage {
			maxPage = page
		}
	})
	if maxPage > 0 {
		return maxPage
	}
	// Some generated pages leave only the last-page URL.  This is a fallback;
	// normal db pages expose the logical number as visible text.
	last := doc.Find(".p_pages .p_last a").AttrOr("href", "")
	if match := regexp.MustCompile(`(?:totalpage=|/)(\d+)(?:\.htm)?(?:&|$)`).FindStringSubmatch(last); match != nil {
		page, _ := strconv.Atoi(match[1])
		return page
	}
	return 1
}

func parseAdapter_dbArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse db article: %w", err)
	}
	if dbLooksLikeBlockedPage(doc) {
		return nil, fmt.Errorf("db article is an error or verification page")
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("db page is not an article (pageType %q)", pageType)
	}
	scope := doc.Find(".article").First()
	if scope.Length() == 0 {
		return nil, fmt.Errorf("db article has no article container")
	}
	content := scope.Find(".v_news_content").First()
	if content.Length() == 0 {
		content = scope.Find("[id^='vsb_content'] .v_news_content").First()
	}
	if content.Length() == 0 {
		return nil, fmt.Errorf("db article has no content container")
	}
	if strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a").Length() == 0 && scope.Find("li a[href]").Length() == 0 {
		return nil, fmt.Errorf("db article has empty content")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = dbArticleTitle(scope, doc)
	if article.Title == "" {
		return nil, fmt.Errorf("db article has no title")
	}

	metaText := flattenedText(scope.Find(".source").First())
	if date, publishedAt := dbArticleDate(metaText); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	source := dbField(metaText, "信息来源", "作者", "日期", "点击数")
	if source == "" {
		source = dbField(metaText, "来源", "作者", "日期", "点击数")
	}
	if source != "" {
		article.Source = source
	}
	article.Type = dbArticleType(doc, baseURL)
	dbAppendAttachments(article, scope, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func dbArticleTitle(scope *goquery.Selection, doc *goquery.Document) string {
	title := dbCompactText(scope.Find("h1").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	}
	return cleanTitle(title)
}

func dbArticleDate(text string) (string, string) {
	if match := dbDateTimeRE.FindStringSubmatch(text); match != nil {
		second := match[6]
		if second == "" {
			second = "00"
		}
		publishedAt := fmt.Sprintf("%s-%02s-%02s %02s:%s:%s", match[1], match[2], match[3], match[4], match[5], second)
		return publishedAt[:10], publishedAt
	}
	if match := dbDateRE.FindStringSubmatch(text); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func dbField(text, label string, stopLabels ...string) string {
	text = strings.ReplaceAll(htmlstd.UnescapeString(text), "\u00a0", " ")
	marker := label + "："
	start := strings.Index(text, marker)
	if start < 0 {
		marker = label + ":"
		start = strings.Index(text, marker)
	}
	if start < 0 {
		return ""
	}
	value := text[start+len(marker):]
	for _, stop := range stopLabels {
		for _, marker := range []string{stop + "：", stop + ":"} {
			if index := strings.Index(value, marker); index >= 0 {
				value = value[:index]
			}
		}
	}
	return strings.TrimSpace(value)
}

func dbArticleType(doc *goquery.Document, baseURL string) string {
	var typ string
	doc.Find(".current a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		href := anchor.AttrOr("href", "")
		resolvedHref := resolveURL(baseURL, href)
		if _, ok := dbArticleURL(baseURL, resolvedHref); ok {
			// Article links can appear in the current trail on some templates;
			// only the known list route links represent a type.
			if candidate := dbRouteType(resolvedHref); candidate != "" {
				typ = candidate
				return false
			}
		}
		if candidate := dbRouteType(resolvedHref); candidate != "" {
			typ = candidate
			return false
		}
		return true
	})
	return typ
}

func dbRouteType(href string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if treeID := parsed.Query().Get("wbtreeid"); treeID != "" {
		switch treeID {
		case "1796":
			return "涉密人员相关下载"
		case "1806":
			return "涉密载体相关下载"
		case "1816":
			return "涉密部位相关下载"
		}
	}
	switch strings.Trim(strings.TrimSpace(parsed.Path), "/") {
	case "xwdt.htm":
		return "新闻动态"
	case "tzgg.htm":
		return "通知公告"
	case "bmgz.htm":
		return "保密工作"
	case "bmgz/bmdt.htm":
		return "保密动态"
	case "bmgz/jzzm.htm":
		return "警钟长鸣"
	case "bmgz/bmgzbslc.htm":
		return "保密工作办事流程"
	case "fwzn.htm":
		return "服务指南"
	case "gzzd.htm":
		return "规章制度"
	default:
		return ""
	}
}

func dbAppendAttachments(article *model.Article, scope *goquery.Selection, baseURL string) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" {
			return
		}
		parentText := dbCompactText(anchor.Closest("li").Text())
		lowerHref := strings.ToLower(href)
		if !strings.Contains(parentText, "附件") && !strings.Contains(lowerHref, "/download.jsp") {
			return
		}
		fullURL := resolveURL(baseURL, href)
		if seen[fullURL] {
			return
		}
		name := dbCompactText(anchor.Text())
		if name == "" {
			name = pathBaseFromURL(fullURL)
		}
		if name == "" {
			return
		}
		seen[fullURL] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: fullURL})
		anchor.SetAttr("href", fullURL)
	})
}

func pathBaseFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
}

func dbLooksLikeBlockedPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	text := strings.ToLower(dbCompactText(doc.Text()))
	for _, marker := range []string{
		"请输入验证码下载附件", "请输入验证码", "安全验证", "访问被拒绝",
		"404 not found", "404错误", "页面不存在", "登录后查看",
	} {
		if strings.Contains(text, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}
