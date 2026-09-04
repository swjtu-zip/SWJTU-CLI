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

// eesSubPages contains the real article-list entry points exposed by the
// current navigation. Profile/contact pages and the two hidden "more" links
// that currently render the homepage shell are intentionally omitted.
// Resource-like pages are retained when they are CMS article lists: their
// rows have real /info/... detail pages (for example 资料下载 and 常用表格).
var eesSubPages = []string{
	"xwdt1/xwdt",
	"xwdt1/mtbd",
	"djgz/djtz",
	"djgz/djdt",
	"tzgg/qb",
	"tzgg/zs",
	"tzgg/py",
	"tzgg/sj",
	"tzgg/xg",
	"tzgg/zh",
	"zsxx/zsdt",
	"zsxx/zlxz",
	"rcpy/kcjx",
	"rcpy/gcal",
	"rcpy/rqsj",
	"rcpy/xwgz",
	"xmpt/xmjs",
	"xmpt/xqpt",
	"xmpt/gjjlpt",
	"szdw/xnds",
	"szdw/qyds",
	"xsgz/xgtz",
	"xsgz/xshd",
	"gfwj/zcwj",
	"gfwj/gzzd",
	"gfwj/cybg",
}

var (
	eesArticlePathRe = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	eesDateRe        = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
	eesSplitDateRe   = regexp.MustCompile(`(\d{1,2})\s*[-/.]\s*(\d{1,2})\s*(20\d{2})`)
	eesDottedYearRe  = regexp.MustCompile(`(\d{1,2})\s*[./-]\s*(\d{1,2})\s*/\s*(20\d{2})`)
	eesAttachmentRe  = regexp.MustCompile(`(?i)\.(?:docx?|xlsx?|pptx?|pdf|zip|rar|7z|txt|csv)(?:$|[?#])`)
	eesEmbeddedPDFRe = regexp.MustCompile(`(?i)showVsbpdfIframe\(\s*["']([^"']+\.pdf)["']`)
)

const (
	eesCanonicalBaseURL    = "https://ees.swjtu.edu.cn"
	eesMaxResourceBytes    = 32 << 20
	eesHomepagePageType    = "1"
	eesArticleListPageType = "2"
	eesArticlePageType     = "3"
)

var eesResourceShellMarkerRe = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|附件下载|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|404\s+not\s+found|access\s+denied|cloudflare|just\s+a\s+moment|安全验证|请求已被阻断`)

// EES is a reverse-numbered Visual SiteBuilder site, so use the existing
// site-specific URL mapper from ees.go in addition to the custom parsers.
func init() {
	registerAdapter("ees", SiteAdapter{
		SubPages:     eesSubPages,
		ParseList:    parseAdapter_eesList,
		ParseArticle: parseAdapter_eesArticle,
		PageURL:      eesPageURL,
		TotalPages:   eesTotalPages,
		FetchArticle: eesFetchArticle,
	})
}

// eesPageURL maps logical pages (1 = newest) to EES's reverse-numbered static
// files. A five-page list therefore uses the unsuffixed URL for page 1 and
// /4.htm, /3.htm, /2.htm, /1.htm for pages 2 through 5.
func eesPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func eesBaseURL() string {
	if site := AllSites["ees"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return eesCanonicalBaseURL
}

func eesIsKnownSubPage(slug string) bool {
	slug = strings.Trim(slug, "/")
	for _, candidate := range eesSubPages {
		if slug == candidate {
			return true
		}
	}
	return false
}

// eesTotalPages counts the unnumbered newest page plus the highest numbered
// generated file. The source's visible links are physical filenames (4, 3,
// 2, 1), not logical page numbers, so the shared generic counter undercounts
// every multi-page EES feed.
func eesTotalPages(slug string) (int, error) {
	slug = strings.Trim(slug, "/")
	if slug == "index" {
		return 1, nil
	}
	if !eesIsKnownSubPage(slug) {
		return 0, fmt.Errorf("ees: %q is not a registered article-list route", slug)
	}

	baseURL := eesBaseURL()
	pageURL := eesPageURL(baseURL, slug, 1, 0, ".htm")
	body, err := fetch(pageURL)
	if err != nil {
		pageURL = eesPageURL(baseURL, slug, 1, 0, ".html")
		body, err = fetch(pageURL)
	}
	if err != nil {
		return 0, fmt.Errorf("ees: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("ees: parse first page for %s: %w", slug, err)
	}
	if reason := eesBlockedReason(doc); reason != "" {
		return 0, fmt.Errorf("ees: first page for %s is blocked or unavailable (%s)", slug, reason)
	}
	if eesHasHomepageShell(doc) || eesPageType(doc) != eesArticleListPageType || eesListContainer(doc).Length() == 0 {
		return 0, fmt.Errorf("ees: first page for %s is not a complete article list", slug)
	}
	return eesPageCount(doc), nil
}

func eesPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	maxPhysicalPage := 0
	hasPagination := doc.Find(".p_pages").Length() > 0
	doc.Find(".p_pages a").Each(func(_ int, link *goquery.Selection) {
		if page := trailingPageNumber(link.AttrOr("href", "")); page > maxPhysicalPage {
			maxPhysicalPage = page
		}
	})
	if maxPhysicalPage > 0 {
		return maxPhysicalPage + 1
	}
	if hasPagination {
		return 1
	}
	return 1
}

func parseAdapter_eesList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse ees list: %w", err)
	}
	if reason := eesBlockedReason(doc); reason != "" {
		return nil, fmt.Errorf("ees: response is blocked or unavailable (%s)", reason)
	}

	if eesIsHomepage(doc) {
		items := eesHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("ees homepage has no article modules")
		}
		return items, nil
	}
	if eesHasHomepageShell(doc) {
		return nil, fmt.Errorf("ees response is a homepage shell, not an article list")
	}
	if pageType := eesPageType(doc); pageType != "" && pageType != eesArticleListPageType {
		return nil, fmt.Errorf("ees response has unsupported pageType %q", pageType)
	}

	list := eesListContainer(doc)
	if list == nil || list.Length() == 0 {
		return nil, fmt.Errorf("ees response is not a homepage or article list")
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	pageType := eesListType(doc)
	list.ChildrenFiltered("li.list-item").Each(func(_ int, row *goquery.Selection) {
		anchor, articleURL := eesRowArticle(row, baseURL)
		if anchor == nil || articleURL == "" {
			return
		}

		title := eesRowTitle(row, anchor)
		if !eesValidTitle(title) {
			return
		}
		date, publishedAt := eesDateValues(row.Find(".left-time, time, .date, .time").First().Text())
		item := model.NewsItem{
			Title:       title,
			URL:         articleURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        pageType,
			ParsedAt:    time.Now(),
		}
		eesAppendItem(&items, seen, item)
	})

	// A present news-list with no list-item rows is a valid empty result. It
	// must not be confused with a static page, which has no list container.
	return items, nil
}

func parseAdapter_eesArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse ees article: %w", err)
	}
	if !eesHasArticleBody(doc) {
		return nil, fmt.Errorf("ees response is not an article")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse ees article: %w", err)
	}

	title := eesText(doc.Find(".left-text > .text-heading, .left-text .text-heading").First())
	if !eesValidTitle(title) {
		return nil, fmt.Errorf("ees article has no fixed title")
	}
	article.Title = title
	source, rawDate := eesArticleMeta(doc)
	article.Source = source
	eesSetArticleDate(article, rawDate)
	article.Type = eesArticleType(doc, article.Source)

	// The generic parser is used only for non-EES conveniences such as adjacent
	// navigation. Its broad resource/date discovery is deliberately discarded;
	// EES owns the fixed metadata and content boundaries below.
	article.Images = nil
	article.Attachments = nil
	eesFinalizeContent(article, doc, baseURL)
	eesAppendAttachments(article, doc, baseURL)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("ees article has no body or file resource")
	}
	article.RawHTML = string(raw)
	return article, nil
}

func eesIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	if pageType := eesPageType(doc); pageType != "" && pageType != eesHomepagePageType {
		return false
	}
	return doc.Find("body.home-page, .home-school-news, .home-news-classification").Length() > 0
}

func eesPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
}

func eesHasHomepageShell(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find("body.home-page, .home-school-news, .home-news-classification, .home-fczy").Length() > 0
}

func eesBlockedReason(doc *goquery.Document) string {
	if doc == nil {
		return "invalid HTML"
	}
	title := strings.ToLower(eesText(doc.Find("title").First()))
	for _, marker := range []string{"404", "403", "500", "error", "错误", "不存在", "验证码", "安全验证", "附件下载", "forbidden", "not found", "access denied", "request blocked", "just a moment", "cloudflare"} {
		if strings.Contains(title, marker) {
			return marker
		}
	}
	body := strings.ToLower(eesText(doc.Find("body").First()))
	for _, marker := range []string{"请输入验证码", "请先登录", "页面不存在", "您访问的页面不存在", "无权限访问", "系统错误", "安全验证", "请求已被阻断", "安全威胁", "access denied", "request blocked", "just a moment", "cloudflare"} {
		if strings.Contains(body, marker) {
			return marker
		}
	}
	return ""
}

func eesHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	if scope := doc.Find(".home-school-news").First(); scope.Length() > 0 {
		eesCollectHomeRows(scope.Find(".mid-section > li, .bottom-section li"), "新闻动态", baseURL, &items, seen)
	}

	if scope := doc.Find(".home-news-classification").First(); scope.Length() > 0 {
		scope.Find("ul.content > li").Each(func(_ int, row *goquery.Selection) {
			anchor, articleURL := eesRowArticle(row, baseURL)
			if anchor == nil || articleURL == "" {
				return
			}
			title := eesText(row.Find(".news, .title, .desc-title, .msg-desc").First())
			if title == "" {
				title = eesAnchorTitle(anchor)
			}
			date, publishedAt := eesDateValues(row.Find("time, .time, .date").First().Text())
			typ := eesText(row.Find(".little-title").First())
			if typ == "" {
				typ = "通知公告"
			}
			if !eesValidTitle(title) {
				return
			}
			eesAppendItem(&items, seen, model.NewsItem{
				Title:       title,
				URL:         articleURL,
				Date:        date,
				PublishedAt: publishedAt,
				Type:        typ,
				ParsedAt:    time.Now(),
			})
		})
	}

	// This module is display:none in the source but contains three real CMS
	// article panels. Its more links render the homepage shell, so only the
	// concrete rows/slides are collected.
	if scope := doc.Find(".home-fczy").First(); scope.Length() > 0 {
		scope.Find(".left .swiper-wrapper > .swiper-slide").Each(func(_ int, slide *goquery.Selection) {
			anchor, articleURL := eesRowArticle(slide, baseURL)
			if anchor == nil || articleURL == "" {
				return
			}
			title := eesText(slide.Find(".swiper-text .text").First())
			if title == "" {
				title = eesAnchorTitle(anchor)
			}
			if !eesValidTitle(title) {
				return
			}
			date, publishedAt := eesDateValues(slide.Find(".swiper-text > a > div").First().Text())
			eesAppendItem(&items, seen, model.NewsItem{
				Title:       title,
				URL:         articleURL,
				Date:        date,
				PublishedAt: publishedAt,
				Type:        "技术中心",
				ParsedAt:    time.Now(),
			})
		})

		scope.Find("ul.main-box").Each(func(_ int, panel *goquery.Selection) {
			typ := eesHomePanelType(scope, panel.AttrOr("id", ""))
			panel.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
				anchor, articleURL := eesRowArticle(row, baseURL)
				if anchor == nil || articleURL == "" {
					return
				}
				title := eesText(row.Find(".right-text .title, .title, .desc-title, .msg-desc").First())
				if title == "" {
					title = eesAnchorTitle(anchor)
				}
				if !eesValidTitle(title) {
					return
				}
				eesAppendItem(&items, seen, model.NewsItem{
					Title:    title,
					URL:      articleURL,
					Type:     typ,
					ParsedAt: time.Now(),
				})
			})
		})
	}

	return items
}

func eesCollectHomeRows(rows *goquery.Selection, typ, baseURL string, items *[]model.NewsItem, seen map[string]int) {
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor, articleURL := eesRowArticle(row, baseURL)
		if anchor == nil || articleURL == "" {
			return
		}
		title := eesText(row.Find(".desc-title, .msg-desc, .news, .right-text .title, .title").First())
		if title == "" {
			title = eesAnchorTitle(anchor)
		}
		if !eesValidTitle(title) {
			return
		}
		date, publishedAt := eesDateValues(row.Find(".time, .time_box, time, .date").First().Text())
		eesAppendItem(items, seen, model.NewsItem{
			Title:       title,
			URL:         articleURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	})
}

func eesHomePanelType(scope *goquery.Selection, id string) string {
	if id == "fczj-content-1" {
		return "校企导师"
	}
	if id == "fczj-content-2" {
		return "卓工学生"
	}
	var typ string
	scope.Find(".fczj-tabNav li[data-content]").EachWithBreak(func(_ int, tab *goquery.Selection) bool {
		if tab.AttrOr("data-content", "") == id {
			typ = eesText(tab)
			return false
		}
		return true
	})
	return typ
}

func eesListContainer(doc *goquery.Document) *goquery.Selection {
	if list := doc.Find(".right-main-content > ul.news-list").First(); list.Length() > 0 {
		return list
	}
	return doc.Find(".right-main-content ul.news-list").First()
}

func eesRowArticle(row *goquery.Selection, baseURL string) (*goquery.Selection, string) {
	var found *goquery.Selection
	var articleURL string
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		resolved, ok := eesArticleURL(baseURL, anchor.AttrOr("href", ""))
		if ok {
			found = anchor
			articleURL = resolved
			return false
		}
		if resolved, ok = eesForwardedExternalURL(baseURL, anchor.AttrOr("href", "")); ok && eesForwardedRow(row, anchor) {
			found = anchor
			articleURL = resolved
			return false
		}
		return true
	})
	return found, articleURL
}

func eesForwardedRow(row, anchor *goquery.Selection) bool {
	title := eesRowTitle(row, anchor)
	return strings.Contains(title, "转发") || strings.Contains(title, "转载")
}

func eesForwardedExternalURL(baseURL, href string) (string, bool) {
	href = htmlstd.UnescapeString(strings.TrimSpace(href))
	if href == "" || bodaIsPseudoResourceHref(href) {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	path := strings.ToLower(parsed.Path)
	if (!strings.HasSuffix(path, ".htm") && !strings.HasSuffix(path, ".html")) || eesAttachmentRe.MatchString(path) || strings.Contains(path, "/login") {
		return "", false
	}
	if !isValidNewsURL(fullURL) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func eesRowTitle(row, anchor *goquery.Selection) string {
	title := eesText(row.Find(".right-text-desc.line1, .right-text-desc .line1, .title, .desc-title, .msg-desc, .news").First())
	if title == "" {
		title = eesAnchorTitle(anchor)
	}
	return cleanTitle(htmlstd.UnescapeString(title))
}

func eesAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if title := eesTextValue(anchor.AttrOr("title", "")); title != "" {
		return cleanTitle(htmlstd.UnescapeString(title))
	}
	return cleanTitle(htmlstd.UnescapeString(eesText(anchor)))
}

func eesListType(doc *goquery.Document) string {
	if typ := eesText(doc.Find(".selectionbar-right-title").First()); typ != "" {
		return typ
	}
	if typ := eesTextValue(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")); typ != "" {
		return typ
	}
	title := eesText(doc.Find("title").First())
	if index := strings.IndexAny(title, "-—"); index > 0 {
		title = title[:index]
	}
	return strings.TrimSpace(title)
}

func eesArticleURL(baseURL, href string) (string, bool) {
	href = htmlstd.UnescapeString(strings.TrimSpace(href))
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, ok := eesInternalURL(baseURL, fullURL)
	if !ok || !eesArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func eesInternalURL(baseURL, rawURL string) (*url.URL, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return nil, false
	}
	return parsed, true
}

func eesValidTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 200 {
		return false
	}
	switch title {
	case "首页", "更多", "查看更多", "正文", "登录", "注册", "联系我们",
		"学院概况", "党建工作", "招生信息", "人才培养", "项目平台", "师资队伍", "学生工作", "规范文件":
		return false
	}
	return true
}

func eesDateValues(raw string) (string, string) {
	raw = htmlstd.UnescapeString(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
	if raw == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(raw); normalized != "" {
		if len(normalized) > 10 && strings.Contains(normalized, ":") {
			return normalized[:10], normalized
		}
		return normalized[:10], ""
	}
	if match := eesDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	if match := eesSplitDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[3], match[1], match[2]), ""
	}
	if match := eesDottedYearRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[3], match[1], match[2]), ""
	}
	return "", ""
}

func eesArticleMeta(doc *goquery.Document) (string, string) {
	tips := doc.Find(".left-text > .tips, .left-text .tips").First()
	var source, rawDate string
	tips.Find("span").Each(func(_ int, field *goquery.Selection) {
		value := eesText(field)
		switch {
		case strings.HasPrefix(value, "来源："):
			source = strings.TrimSpace(strings.TrimPrefix(value, "来源："))
		case strings.HasPrefix(value, "来源:"):
			source = strings.TrimSpace(strings.TrimPrefix(value, "来源:"))
		case strings.HasPrefix(value, "日期："):
			rawDate = strings.TrimSpace(strings.TrimPrefix(value, "日期："))
		case strings.HasPrefix(value, "日期:"):
			rawDate = strings.TrimSpace(strings.TrimPrefix(value, "日期:"))
		}
	})
	return source, rawDate
}

func eesArticleType(doc *goquery.Document, source string) string {
	if typ := eesText(doc.Find(".second-title > span").First()); typ != "" && typ != "正文" && typ != "NEWS" {
		return typ
	}
	var typ string
	doc.Find(".right-link a").Each(func(_ int, link *goquery.Selection) {
		value := eesText(link)
		if value != "" && value != "首页" && value != "正文" {
			typ = value
		}
	})
	if typ != "" {
		return typ
	}
	if typ = eesText(doc.Find(".second-title span").First()); typ != "" {
		return typ
	}
	return strings.TrimSpace(source)
}

func eesHasArticleBody(doc *goquery.Document) bool {
	if eesPageType(doc) != eesArticlePageType {
		return false
	}
	if eesIsVerificationPage(doc) {
		return false
	}
	return doc.Find(".left-text > .text-heading, .left-text .text-heading").Length() > 0 && eesArticleContent(doc).Length() > 0
}

func eesIsVerificationPage(doc *goquery.Document) bool {
	title := eesText(doc.Find("title").First())
	body := eesText(doc.Find("body").First())
	return title == "附件下载" || strings.Contains(body, "请输入验证码下载附件") || strings.Contains(body, "验证码下载附件")
}

func eesArticleContent(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		".left-text > .text-body .v_news_content",
		".left-text .text-body .v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return doc.Find("__ees_missing_content__")
}

func eesFinalizeContent(article *model.Article, doc *goquery.Document, baseURL string) {
	content := eesArticleContent(doc)
	if content.Length() == 0 {
		return
	}
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()

	parts := make([]string, 0)
	clone.Find("p, h1, h2, h3, h4, h5, h6").Each(func(_ int, part *goquery.Selection) {
		if text := eesText(part); text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) == 0 {
		if text := eesText(clone); text != "" {
			parts = append(parts, text)
		}
	}
	article.Content = strings.Join(parts, "\n\n")

	images := make([]string, 0)
	seenImages := make(map[string]bool)
	clone.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		resolved := eesResourceURL(baseURL, image.AttrOr("src", ""))
		if resolved == "" {
			image.Remove()
			return
		}
		if seenImages[resolved] {
			return
		}
		seenImages[resolved] = true
		images = append(images, resolved)
		image.SetAttr("src", resolved)
	})
	article.Images = images
	clone.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(href, "#") {
				link.Remove()
			}
			return
		}
		if resolved := cleanURL(resolveURL(baseURL, href)); resolved != "" {
			link.SetAttr("href", resolved)
		}
	})
	if htmlText, err := clone.Html(); err == nil {
		article.ContentHTML = htmlText
	}
}

func eesAppendAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}

	doc.Find(".left-text a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !eesIsAttachmentLink(link, href) {
			return
		}
		resolved := eesResourceURL(baseURL, href)
		if resolved == "" {
			return
		}
		fullURL := resolved
		if seen[fullURL] {
			return
		}
		name := eesAttachmentName(link, href)
		if name == "" {
			return
		}
		seen[fullURL] = true
		article.Attachments = append(article.Attachments, model.Attachment{
			Name: name,
			URL:  fullURL,
			Size: strings.TrimSpace(link.AttrOr("data-size", "")),
		})
	})

	// Some EES resource details render a PDF through JavaScript instead of an
	// anchor. Keep that real file URL as an attachment so a caller can verify
	// it with an ordinary GET; the script itself is removed from ContentHTML.
	content := eesArticleContent(doc)
	content.Find("script").Each(func(_ int, script *goquery.Selection) {
		for _, match := range eesEmbeddedPDFRe.FindAllStringSubmatch(script.Text(), -1) {
			if len(match) < 2 {
				continue
			}
			resolved := eesResourceURL(baseURL, match[1])
			if resolved == "" || seen[resolved] {
				continue
			}
			seen[resolved] = true
			article.Attachments = append(article.Attachments, model.Attachment{
				Name: "正文内嵌PDF",
				URL:  resolved,
			})
		}
	})
}

func eesIsAttachmentLink(link *goquery.Selection, href string) bool {
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	name := strings.Join([]string{link.AttrOr("download", ""), link.AttrOr("name", ""), link.AttrOr("title", ""), link.Text(), href}, " ")
	if !eesAttachmentRe.MatchString(name) {
		return false
	}
	marker := link.Closest("li")
	return marker.Length() > 0 && strings.Contains(eesText(marker), "附件")
}

func eesAttachmentName(link *goquery.Selection, href string) string {
	for _, value := range []string{
		link.AttrOr("download", ""),
		link.AttrOr("data-filename", ""),
		link.AttrOr("name", ""),
		link.AttrOr("title", ""),
		link.Text(),
	} {
		if value = eesTextValue(value); value != "" {
			return strings.Trim(value, "【】[]")
		}
	}
	if parsed, err := url.Parse(href); err == nil {
		if name := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]; name != "" {
			return name
		}
	}
	return ""
}

func eesSetArticleDate(article *model.Article, rawDate string) {
	article.Date, article.PublishedAt = eesDateValues(rawDate)
}

// eesResourceURL accepts ordinary HTTP(S) resources, including externally
// hosted article images/files, but drops pseudo URLs and known placeholder or
// verification paths before they reach the downloader.
func eesResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"/captcha", "verifycode", "verification", "createimage", "getcode",
		"placeholder", "transparent", "spacer", "blank.gif", "loading.gif",
		"pixel.gif", "access-denied", "waf",
	} {
		if strings.Contains(resourceText, marker) {
			return ""
		}
	}
	parsed.Fragment = ""
	return parsed.String()
}

// eesFetchArticle is the live detail path. Parsing stays deterministic, while
// this network path verifies every image/file advertised by the fixed article
// container. EES's download.jsp currently responds with HTTP 200 HTML
// CAPTCHA pages, so status alone is intentionally insufficient.
func eesFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := eesBaseURL()
	fullURL, ok := eesArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("ees: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("ees: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_eesArticle(body, baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("ees: parse article %s: %w", fullURL, err)
	}
	article.Images = eesVerifyImages(article.Images, baseURL)
	article.Attachments = eesVerifyAttachments(article.Attachments, baseURL)
	article.ContentHTML = eesSanitizeContentHTML(article.ContentHTML, baseURL, article.Images, article.Attachments)
	return article, body, nil
}

func eesVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := eesResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := eesFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func eesVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := eesResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := eesFetchResource(resolved, baseURL, false)
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

func eesFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := eesResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("ees: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("ees: create resource request: %w", err)
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
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("ees: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("ees: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, eesMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("ees: read resource %s: %w", resolved, err)
	}
	if len(body) > eesMaxResourceBytes || !eesResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("ees: resource %s returned an invalid Content-Type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func eesResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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
	if eesResourceShellMarkerRe.Match(probe) {
		return false
	}
	magic := eesResourceMagic(body)
	if image {
		if !strings.HasPrefix(mediaType, "image/") || !eesIsImageMagic(magic) {
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
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func eesIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func eesResourceMagic(body []byte) string {
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
	if len(trimmed) >= 4 && strings.HasPrefix(strings.ToLower(string(trimmed)), "<svg") {
		return "svg"
	}
	return ""
}

func eesSanitizeContentHTML(raw, baseURL string, images []string, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	imageSet := make(map[string]bool, len(images))
	for _, image := range images {
		imageSet[eesResourceURL(baseURL, image)] = true
	}
	attachmentSet := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		attachmentSet[eesResourceURL(baseURL, attachment.URL)] = true
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"ees-content-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	root := doc.Find("#ees-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := eesResourceURL(baseURL, image.AttrOr("src", ""))
		if resolved == "" || !imageSet[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(href, "#") {
				link.Remove()
			}
			return
		}
		if eesIsAttachmentLink(link, href) {
			resolved := eesResourceURL(baseURL, href)
			if resolved == "" || !attachmentSet[resolved] {
				link.Remove()
				return
			}
			link.SetAttr("href", resolved)
			return
		}
		link.SetAttr("href", cleanURL(resolveURL(baseURL, href)))
	})
	rendered, err := root.Html()
	if err != nil {
		return ""
	}
	return rendered
}

func eesAppendItem(items *[]model.NewsItem, seen map[string]int, item model.NewsItem) {
	if index, ok := seen[item.URL]; ok {
		current := &(*items)[index]
		if current.Title == "" && item.Title != "" {
			current.Title = item.Title
		}
		if current.Date == "" && item.Date != "" {
			current.Date = item.Date
			current.PublishedAt = item.PublishedAt
		}
		if current.Type == "" && item.Type != "" {
			current.Type = item.Type
		}
		return
	}
	seen[item.URL] = len(*items)
	*items = append(*items, item)
}

func eesText(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	return eesTextValue(sel.Text())
}

func eesTextValue(value string) string {
	return strings.Join(strings.Fields(htmlstd.UnescapeString(strings.TrimSpace(value))), " ")
}
