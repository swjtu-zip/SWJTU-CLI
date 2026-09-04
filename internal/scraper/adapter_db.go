package scraper

import (
	"bytes"
	"fmt"
	htmlstd "html"
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

const dbBaseURL = "https://db.swjtu.edu.cn"

// dbSubPages contains only pages which were verified to contain article rows.
// The profile pages under bmgk and the bmgz landing page are static/category
// shells. fwzn/gzcx and llqy are legacy links not present in the current top
// navigation, but both still expose real article lists and are intentionally
// retained as audited feeds.
var dbSubPages = []string{
	"xwdt",
	"tzgg",
	"bmgz/bmdt",
	"bmgz/jzzm",
	"bmgz/bmgzbslc",
	"bmgz/1796",
	"bmgz/1806",
	"bmgz/1816",
	"fwzn",
	"fwzn/gzcx",
	"gzzd",
	"llqy",
}

type dbFeedRoute struct {
	path    string
	treeID  string
	reverse bool
	// numbered means the first generated file is slug.htm and the logical
	// second page is slug/1.htm. This is the convention used by db's static
	// list generator for forward-numbered archives.
	numbered bool
}

var dbRoutes = map[string]dbFeedRoute{
	"index":         {path: "index.htm"},
	"xwdt":          {path: "xwdt.htm", reverse: true, numbered: true},
	"tzgg":          {path: "tzgg.htm", numbered: true},
	"bmgz/bmdt":     {path: "bmgz/bmdt.htm", numbered: true},
	"bmgz/jzzm":     {path: "bmgz/jzzm.htm", numbered: true},
	"bmgz/bmgzbslc": {path: "bmgz/bmgzbslc.htm", numbered: true},
	"bmgz/1796":     {treeID: "1796"},
	"bmgz/1806":     {treeID: "1806"},
	"bmgz/1816":     {treeID: "1816"},
	"fwzn":          {path: "fwzn.htm", numbered: true},
	"fwzn/gzcx":     {path: "fwzn/gzcx.htm", numbered: true},
	"gzzd":          {path: "gzzd.htm", numbered: true},
	"llqy":          {path: "llqy.htm", reverse: true, numbered: true},
}

var (
	dbArticlePathRE   = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	dbDateTimeRE      = regexp.MustCompile(`(?:发布日期|发布时间|发表时间|日期)\s*[:：]\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})(?:日|号)?(?:\s+|[Tt]+)(\d{1,2}):(\d{2})(?::(\d{2}))?`)
	dbDateRE          = regexp.MustCompile(`(?:发布日期|发布时间|发表时间|日期)\s*[:：]\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
	dbListDateRE      = regexp.MustCompile(`时间\s*[:：]\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
	dbBareDateRE      = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
	dbPageNumberRE    = regexp.MustCompile(`^\s*(\d+)\s*$`)
	dbResourceShellRE = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|请输入验证码|验证码下载附件|安全验证|访问被拒绝|access\s+denied|cloudflare|just\s+a\s+moment|页面不存在|404\s+not\s+found|登录页面|请先登录`)
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
		FetchArticle: dbFetchArticle,
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
	if len(items) == 0 {
		return nil, fmt.Errorf("db article list contains no valid article rows")
	}
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

// dbListPage fetches one logical page. xwdt and llqy are reverse-numbered
// static archives; the other static archives use the source's zero-based
// generated filenames (logical page 2 is /1.htm). The three list.jsp feeds
// use PAGENUM/totalpage query parameters.
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
	if baseURL == "" || page < 1 {
		return ""
	}
	if page > 1 && (totalPages < 1 || page > totalPages) {
		return ""
	}
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
	if page == 1 {
		return baseURL + "/" + strings.TrimLeft(route.path, "/")
	}
	filePage := page
	if route.numbered {
		filePage = page - 1
	}
	if route.reverse {
		filePage = totalPages - page + 1
	}
	if filePage < 1 {
		return ""
	}
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

	// db's detail template has one authoritative metadata block and one
	// authoritative .v_news_content block. Do not call the generic article
	// parser here: its global fallback can select a sidebar content block and
	// its broad metadata search can see dates in the article body.
	title := dbArticleTitle(scope, doc)
	if title == "" {
		return nil, fmt.Errorf("db article has no title")
	}
	meta := scope.Find(".source").First()
	if meta.Length() == 0 {
		meta = scope.Find(".news_source").First()
	}
	metaText := flattenedText(meta)
	article := &model.Article{
		Title:  title,
		Source: dbFirstField(metaText, []string{"信息来源", "来源"}, "作者", "日期", "发布日期", "发布时间", "点击数"),
		Author: dbField(metaText, "作者", "日期", "发布日期", "发布时间", "点击数"),
		Type:   dbArticleType(doc, baseURL),
	}
	if date, publishedAt := dbArticleDate(metaText); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}

	article.Content, article.ContentHTML, article.Images = dbExtractContent(content, baseURL)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && content.Find("table, a").Length() == 0 && scope.Find("li a[href]").Length() == 0 {
		return nil, fmt.Errorf("db article has empty content")
	}
	article.Attachments = dbCollectAttachments(scope, baseURL)
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
		date := normalizeDateParts(match[1], match[2], match[3])
		hour, hourErr := strconv.Atoi(match[4])
		minute, minuteErr := strconv.Atoi(match[5])
		second := 0
		var secondErr error
		if match[6] != "" {
			second, secondErr = strconv.Atoi(match[6])
		}
		if date == "" || hourErr != nil || minuteErr != nil || secondErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59 {
			return "", ""
		}
		return date, fmt.Sprintf("%s %02d:%02d:%02d", date, hour, minute, second)
	}
	if match := dbDateRE.FindStringSubmatch(text); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func dbFirstField(text string, labels []string, stopLabels ...string) string {
	for _, label := range labels {
		if value := dbField(text, label, stopLabels...); value != "" {
			return value
		}
	}
	return ""
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
	if doc == nil {
		return ""
	}
	// Prefer the breadcrumb's route href. It remains unambiguous for content.jsp
	// pages, where the numeric tree ID is the only stable type identifier.
	var typ string
	typRank := 0
	doc.Find(".current a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if candidate := dbRouteType(resolveURL(baseURL, anchor.AttrOr("href", ""))); candidate != "" {
			rank := dbTypeRank(candidate)
			if rank >= typRank {
				typ, typRank = candidate, rank
			}
		}
	})
	if typ != "" {
		return typ
	}

	// Some db detail pages render the category as a heading while their
	// breadcrumb ends in only “正文”. Keep this lookup inside known header
	// zones; never search the body text for a category name.
	for _, selector := range []string{
		".current h3", ".current h2", ".current .title", ".current .current-title",
		".article-category", ".article-type", ".con_tit h3", ".con_tit h2",
	} {
		if candidate := dbKnownTypeFromText(doc.Find(selector).First().Text()); candidate != "" {
			return candidate
		}
	}
	return dbKnownTypeFromText(doc.Find(".current").First().Text())
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
	path := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	switch {
	case path == "xwdt.htm" || strings.HasPrefix(path, "xwdt/"):
		return "新闻动态"
	case path == "tzgg.htm" || strings.HasPrefix(path, "tzgg/"):
		return "通知公告"
	case path == "bmgz/bmdt.htm" || strings.HasPrefix(path, "bmgz/bmdt/"):
		return "保密动态"
	case path == "bmgz/jzzm.htm" || strings.HasPrefix(path, "bmgz/jzzm/"):
		return "警钟长鸣"
	case path == "bmgz/bmgzbslc.htm" || strings.HasPrefix(path, "bmgz/bmgzbslc/"):
		return "保密工作办事流程"
	case path == "bmgz.htm" || strings.HasPrefix(path, "bmgz/"):
		return "保密工作"
	case path == "fwzn/gzcx.htm" || strings.HasPrefix(path, "fwzn/gzcx/"):
		return "工作程序"
	case path == "fwzn.htm" || strings.HasPrefix(path, "fwzn/"):
		return "服务指南"
	case path == "gzzd.htm" || strings.HasPrefix(path, "gzzd/"):
		return "规章制度"
	case path == "llqy.htm" || strings.HasPrefix(path, "llqy/"):
		return "理论前沿"
	default:
		return ""
	}
}

var dbKnownArticleTypes = []string{
	"保密工作办事流程", "涉密人员相关下载", "涉密载体相关下载", "涉密部位相关下载",
	"新闻动态", "通知公告", "保密动态", "警钟长鸣", "服务指南", "规章制度", "理论前沿", "工作程序", "保密工作",
}

func dbKnownTypeFromText(text string) string {
	text = dbCompactText(text)
	if text == "" {
		return ""
	}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune(">»|｜", r)
	}) {
		part = strings.TrimSpace(strings.Trim(part, "[]【】"))
		for _, typ := range dbKnownArticleTypes {
			if part == typ || strings.Contains(part, typ) {
				return typ
			}
		}
	}
	return ""
}

func dbTypeRank(typ string) int {
	if typ == "保密工作" || typ == "服务指南" {
		return 1
	}
	return 2
}

func dbExtractContent(content *goquery.Selection, baseURL string) (string, string, []string) {
	if content == nil || content.Length() == 0 {
		return "", "", nil
	}
	text := extractBodaContentText(content)
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	images := make([]string, 0)
	seen := make(map[string]bool)
	clone.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := dbImageURLFromSelection(baseURL, image)
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vsbhref", "vurl"} {
			image.RemoveAttr(attr)
		}
		if !seen[resolved] {
			seen[resolved] = true
			images = append(images, resolved)
		}
	})
	clone.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if bodaIsPseudoResourceHref(href) {
			link.RemoveAttr("href")
			return
		}
		if !dbIsAttachmentLink(link, href) {
			return
		}
		resolved := dbResourceURL(baseURL, href)
		if resolved == "" {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	html, _ := clone.Html()
	return text, html, images
}

func dbCollectAttachments(scope *goquery.Selection, baseURL string) []model.Attachment {
	if scope == nil || scope.Length() == 0 {
		return nil
	}
	attachments := make([]model.Attachment, 0)
	seen := make(map[string]bool)
	scope.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !dbIsAttachmentLink(link, href) {
			return
		}
		resolved := dbResourceURL(baseURL, href)
		if resolved == "" || seen[resolved] {
			return
		}
		name := dbCompactText(link.AttrOr("download", ""))
		if name == "" {
			name = dbCompactText(link.Text())
		}
		if name == "" {
			name = pathBaseFromURL(resolved)
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		attachments = append(attachments, model.Attachment{Name: name, URL: resolved})
	})
	return attachments
}

// dbAppendAttachments is kept as a small compatibility wrapper for callers
// which used the former parser helper. The live parser uses the returned slice
// directly so it can verify every candidate before exposing it.
func dbAppendAttachments(article *model.Article, scope *goquery.Selection, baseURL string) {
	if article == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	for _, attachment := range dbCollectAttachments(scope, baseURL) {
		if seen[attachment.URL] {
			continue
		}
		seen[attachment.URL] = true
		article.Attachments = append(article.Attachments, attachment)
	}
}

func dbIsAttachmentLink(link *goquery.Selection, href string) bool {
	if link != nil && bodaIsPseudoResourceHref(href) {
		return strings.TrimSpace(link.AttrOr("download", "")) != "" || strings.Contains(dbCompactText(link.Closest("li").Text()), "附件")
	}
	return bodaLooksLikeAttachmentLink(link, href)
}

func dbImageURLFromSelection(baseURL string, image *goquery.Selection) string {
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		if raw := strings.TrimSpace(image.AttrOr(attr, "")); raw != "" {
			return dbResourceURL(baseURL, raw)
		}
	}
	return ""
}

func dbResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := bodaResourceURL(baseURL, raw)
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	lower := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{"/captcha", "/verify", "/validate", "/login", "/signin", "/placeholder", "/transparent", "/spacer", "/blank", "/loading", "/pixel.gif"} {
		if strings.Contains(lower, marker) {
			return ""
		}
	}
	return cleanURL(parsed.String())
}

const dbMaxResourceBytes = 32 << 20

func dbFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := dbSiteBaseURL()
	fullURL, ok := dbArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("db: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_dbArticle(body, baseURL)
	if err != nil {
		return nil, body, err
	}
	article.Images = dbVerifyImages(article.Images, baseURL)
	article.Attachments = dbVerifyAttachments(article.Attachments, baseURL)
	dbSanitizeContentHTML(article, baseURL)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, body, fmt.Errorf("db article has no usable content or resources")
	}
	return article, body, nil
}

func dbVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved := dbResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := dbFetchResource(resolved, baseURL, true)
		if err != nil || !dbResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func dbVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := dbResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := dbFetchResource(resolved, baseURL, false)
		if err != nil || !dbResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func dbFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := dbResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("db: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("db: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("db: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode != http.StatusOK {
		return nil, contentType, fmt.Errorf("db: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, dbMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("db: read resource %s: %w", resolved, err)
	}
	if len(body) > dbMaxResourceBytes {
		return nil, contentType, fmt.Errorf("db: resource %s exceeds %d bytes", resolved, dbMaxResourceBytes)
	}
	return body, contentType, nil
}

func dbResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if dbResourceShellRE.Match(probe) {
		return false
	}
	magic := dbResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && dbIsImageMagic(magic)
	}
	if strings.HasPrefix(mediaType, "text/") || mediaType == "application/xhtml+xml" || mediaType == "application/json" || mediaType == "application/xml" {
		lowerName := strings.ToLower(nameOrURL)
		if strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") {
			return mediaType == "text/plain" || mediaType == "text/csv"
		}
		return false
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
	default:
		return magic != ""
	}
}

func dbResourceMagic(body []byte) string {
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
	case bytes.HasPrefix(bytes.ToLower(bytes.TrimSpace(body)), []byte("<svg")):
		return "svg"
	default:
		return ""
	}
}

func dbIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func dbSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		if resolved := dbResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved := dbResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"db-content-root\">" + article.ContentHTML + "</div>"))
	if err != nil {
		return
	}
	content := doc.Find("#db-content-root").First()
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := dbImageURLFromSelection(baseURL, image)
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vsbhref", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if bodaIsPseudoResourceHref(href) {
			link.RemoveAttr("href")
			return
		}
		if !dbIsAttachmentLink(link, href) {
			return
		}
		resolved := dbResourceURL(baseURL, href)
		if resolved == "" || !validAttachments[resolved] {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	if html, err := content.Html(); err == nil {
		article.ContentHTML = html
	}
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
