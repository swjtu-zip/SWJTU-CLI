package scraper

import (
	"bytes"
	"fmt"
	"html"
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

const xghCanonicalBaseURL = "https://xgh.swjtu.edu.cn"

const xghMaxResourceBytes = 32 << 20

// xghSubPages keeps the ten top-level aggregate feeds used by the existing
// site catalogue. xghFeedPages below additionally records every article-list
// leaf found while walking the top navigation and its sidebars. The aggregate
// pages are retained here because several leaf pages intentionally duplicate
// their parent archive.
var xghSubPages = []string{
	"xwzx1", "tzgg", "jcdt", "jgfc", "szyl", "ghdj", "gzzd", "hdyx1", "bszn", "xzzx",
}

// xghFeedPages is the complete set of real article-list routes confirmed from
// the live homepage, top navigation, sidebars, and generated pagination. The
// static/empty pages under gh2, jdh1, jyxc, ghdj/zbgk, jgfc/jsjy,
// jgfc/xjmf, and the empty bszn leaves are deliberately absent.
var xghFeedPages = []string{
	"xwzx1",
	"tzgg",
	"jcdt",
	"jgfc",
	"jgfc/gwjg",
	"szyl",
	"ghdj",
	"ghdj/djdt",
	"ghdj/xxyd",
	"gzzd",
	"gzzd/gjfg",
	"gzzd/dffg",
	"gzzd/xxgz",
	"gzzd/xghzd",
	"hdyx1",
	"bszn",
	"xzzx",
}

var xghFeedTypes = map[string]string{
	"xwzx1":      "新闻中心",
	"tzgg":       "通知公告",
	"jcdt":       "基层动态",
	"jgfc":       "教工风采",
	"jgfc/gwjg":  "岗位建功",
	"szyl":       "思政引领",
	"ghdj":       "工会党建",
	"ghdj/djdt":  "党建动态",
	"ghdj/xxyd":  "学习园地",
	"gzzd":       "规章制度",
	"gzzd/gjfg":  "国家法规",
	"gzzd/dffg":  "地方法规",
	"gzzd/xxgz":  "学校规章",
	"gzzd/xghzd": "校工会制度",
	"hdyx1":      "活动影像",
	"bszn":       "办事指南",
	"xzzx":       "下载中心",
}

var (
	xghArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	// Kept as a fallback for older Boda pagination markup. The live xgh
	// pages expose numbered .p_no/.p_no_d nodes, which xghPageCount reads
	// first.
	xghPageCounterRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)
)

var xghShellMarkers = []string{
	"页面不存在",
	"您访问的页面不存在",
	"404错误",
	"404 not found",
	"403 forbidden",
	"访问被拒绝",
	"请求被阻断",
	"请求被拦截",
	"安全验证",
	"人机验证",
	"请输入验证码",
	"验证码下载附件",
	"请完成验证",
	"请先登录",
	"请登录后",
	"access denied",
	"request blocked",
	"just a moment",
	"cloudflare",
	"please verify",
	"waf",
}

func init() {
	registerAdapter("xgh", SiteAdapter{
		SubPages:     xghSubPages,
		ParseList:    parseAdapter_xghList,
		ParseArticle: parseAdapter_xghArticle,
		PageURL:      xghPageURL,
		TotalPages:   xghTotalPages,
		FetchArticle: xghFetchArticle,
	})
}

func parseAdapter_xghList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xgh: parse list HTML: %w", err)
	}
	if err := xghRejectShell(doc, "list"); err != nil {
		return nil, err
	}
	if xghIsHomepage(doc) {
		items := xghHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("xgh: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := xghListContainer(doc)
	if container.Length() == 0 {
		return nil, fmt.Errorf("xgh: response has no article list container")
	}
	items := xghListItems(container, xghDocumentType(doc), baseURL)
	if len(items) == 0 {
		return nil, fmt.Errorf("xgh: article list contains no article entries")
	}
	return items, nil
}

func xghIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", "")) == "1"
}

// xghHomepageItems explicitly walks the four article-bearing homepage
// modules. In particular, the activity carousel is a real article module,
// while the separate hero carousel contains only banner images and is not.
func xghHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".global-hd").Each(func(_ int, heading *goquery.Selection) {
		typ := xghTypeFromText(heading.Find("h3").First().Text())
		if !xghHomepageType(typ) {
			return
		}
		list := xghHomepageList(heading)
		if list.Length() == 0 {
			return
		}
		list.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
			xghAppendItem(&items, seen, row.Find("a[href]").First(), row, typ, baseURL)
		})
	})
	return items
}

func xghHomepageType(typ string) bool {
	switch typ {
	case "新闻中心", "通知公告", "基层动态", "活动影像":
		return true
	default:
		return false
	}
}

// xghHomepageList follows the fixed module structure instead of selecting
// every ul.list on the page. This excludes the header navigation, service
// links, footer links, and the banner-only swiper.
func xghHomepageList(heading *goquery.Selection) *goquery.Selection {
	if heading == nil || heading.Length() == 0 {
		return &goquery.Selection{}
	}
	for parent := heading.Parent(); parent.Length() > 0; parent = parent.Parent() {
		if parent.HasClass("activity-wrap") {
			if list := parent.Find(".swiper-container .swiper-wrapper").First(); list.Length() > 0 {
				return list
			}
		}
		if bd := parent.ChildrenFiltered(".bd").First(); bd.Length() > 0 {
			if list := bd.ChildrenFiltered("ul").First(); list.Length() > 0 && xghHasArticleHref(list) {
				return list
			}
			if list := bd.Find(".swiper-container .swiper-wrapper").First(); list.Length() > 0 && xghHasArticleHref(list) {
				return list
			}
		}
		if list := parent.ChildrenFiltered("ul").First(); list.Length() > 0 && xghHasArticleHref(list) {
			return list
		}
		if parent.HasClass("news-box") || parent.HasClass("main") || parent.Is("body") {
			break
		}
	}
	return &goquery.Selection{}
}

func xghHasArticleHref(sel *goquery.Selection) bool {
	if sel == nil || sel.Length() == 0 {
		return false
	}
	found := false
	sel.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if strings.Contains(strings.ToLower(anchor.AttrOr("href", "")), "info/") {
			found = true
			return false
		}
		return true
	})
	return found
}

func xghListContainer(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	// The first ul.list under .news-list-wrap is the article list. The
	// .news-navbar sidebar also uses ul.list and is often empty.
	for _, selector := range []string{
		".news-list-wrap > .bd > ul.list",
		".news-list-wrap .bd ul.list",
		".news-list-wrap .bd > ul",
		".news-list-wrap ul.list",
	} {
		if list := doc.Find(selector).First(); list.Length() > 0 && xghHasArticleHref(list) {
			return list
		}
	}
	// This narrow fallback keeps compatibility with the older Boda-shaped
	// fixture used by the shared adapter smoke test. It is only considered
	// when it itself contains article URLs, never merely because it is a list.
	for _, selector := range []string{".right ul.list", ".left ul.list", "ul.list"} {
		if list := doc.Find(selector).First(); list.Length() > 0 && xghHasArticleHref(list) {
			return list
		}
	}
	return &goquery.Selection{}
}

func xghListItems(container *goquery.Selection, typ, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	container.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
		xghAppendItem(&items, seen, row.Find("a[href]").First(), row, typ, baseURL)
	})
	return items
}

func xghDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{
		".news-list-wrap > .hd > h3",
		".list-title",
		".right .title h3",
		".right .title",
		"h1",
		"h2",
	} {
		if typ := xghTypeFromText(doc.Find(selector).First().Text()); typ != "" {
			return typ
		}
	}
	return xghTypeFromText(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).First().AttrOr("content", ""))
}

func xghTypeFromText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	for _, typ := range xghFeedTypes {
		if value == typ {
			return typ
		}
	}
	// A title can include a small site suffix, but prefer the longest known
	// label so “工会党建/党建动态” cannot be mistaken for its parent.
	known := make([]string, 0, len(xghFeedTypes))
	seen := make(map[string]bool)
	for _, typ := range xghFeedTypes {
		if !seen[typ] {
			known = append(known, typ)
			seen[typ] = true
		}
	}
	for i := 0; i < len(known); i++ {
		for j := i + 1; j < len(known); j++ {
			if len([]rune(known[j])) > len([]rune(known[i])) {
				known[i], known[j] = known[j], known[i]
			}
		}
	}
	for _, typ := range known {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func xghAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := html.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
	fullURL, ok := xghArticleURLValue(baseURL, href)
	if href == "" || !ok || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find("p.toe, p.toe2, .tit, .title, .line1, .news-title, h3, h4, em").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := xghRowDate(row)
	rowType := xghRowType(row)
	if rowType != "" && rowType != "首页" && rowType != "网站首页" {
		typ = rowType
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        typ,
		ParsedAt:    time.Now(),
	})
}

func xghRowType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	var typ string
	row.Find("a > span, a span").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		rawValue := strings.TrimSpace(node.Text())
		if node.HasClass("time") || node.HasClass("date") {
			return true
		}
		if _, published := xghDateValues(rawValue); published != "" || bareDateRe.MatchString(rawValue) {
			return true
		}
		value := rawValue
		value = strings.Trim(value, "[]【】()（）")
		if value != "" && value != "时间" && value != "日期" {
			typ = value
			return false
		}
		return true
	})
	return typ
}

// xghRowDate only reads dedicated list metadata nodes. It intentionally does
// not inspect the complete row text: homepage summaries and article titles
// contain activity/deadline dates that are not publication dates.
func xghRowDate(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	for _, selector := range []string{".info", ".time", "time", ".date", "[datetime]", "[data-date]"} {
		var date, published string
		row.Find(selector).EachWithBreak(func(_ int, node *goquery.Selection) bool {
			value := node.Text()
			if node.Is("[datetime]") {
				value = node.AttrOr("datetime", value)
			}
			date, published = xghDateValues(value)
			return date == ""
		})
		if date != "" {
			return date, published
		}
	}
	// Homepage 基层动态 places its date in an unclassed span alongside the
	// title. Only direct anchor spans are considered after the named nodes.
	var date, published string
	row.Find("a > span").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		date, published = xghDateValues(node.Text())
		return date == ""
	})
	return date, published
}

func xghDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(value), " ")
	if match := dateTimeRe.FindString(value); match != "" {
		if published := normalizePublishedAt(match); len(published) >= 10 {
			return published[:10], published
		}
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func xghKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	for _, known := range xghFeedPages {
		if slug == known {
			return true
		}
	}
	return false
}

func xghPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "index" {
		// The homepage is a real article feed (xghHomepageItems) and single
		// page; expose it under /index.htm so the shared list fetch works.
		if page == 1 {
			return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/index.htm"
		}
		return ""
	}
	if !xghKnownFeed(slug) {
		return ""
	}
	return reverseBodaPageURL(baseURL, slug, page, totalPages, extension)
}

func xghTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if !xghKnownFeed(slug) {
		return 0, fmt.Errorf("xgh: %q is not an article-list route", slug)
	}
	baseURL := xghBaseURL()
	firstURL := xghPageURL(baseURL, slug, 1, 0, ".htm")
	body, err := fetch(firstURL)
	if err != nil {
		return 0, fmt.Errorf("xgh: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xgh: parse first page for %s: %w", slug, err)
	}
	if err := xghRejectShell(doc, "list"); err != nil {
		return 0, err
	}
	container := xghListContainer(doc)
	if container.Length() == 0 || !xghHasArticleHref(container) {
		return 0, fmt.Errorf("xgh: %s is not an article list page", slug)
	}
	if total := xghPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func xghPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	maxPage := 0
	for _, selector := range []string{".news-list-wrap .p_pages", ".news-list-wrap .paging", ".p_pages", ".paging"} {
		doc.Find(selector).First().Find(".p_no, .p_no_d").Each(func(_ int, node *goquery.Selection) {
			value, parseErr := strconv.Atoi(strings.TrimSpace(node.Text()))
			if parseErr == nil && value > maxPage {
				maxPage = value
			}
		})
		if maxPage > 0 {
			return maxPage
		}
	}
	text := strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .page").Text()), " ")
	if match := xghPageCounterRE.FindStringSubmatch(text); match != nil {
		if total, err := strconv.Atoi(match[1]); err == nil {
			return total
		}
	}
	return 0
}

func parseAdapter_xghArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xgh: parse article HTML: %w", err)
	}
	if err := xghRejectShell(doc, "article"); err != nil {
		return nil, err
	}
	contentSel := xghArticleContent(doc)
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("xgh: response has no fixed article body")
	}
	title, typ, date, publishedAt, source, err := xghFixedArticleFields(doc)
	if err != nil {
		return nil, err
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, fmt.Errorf("xgh: parse article body: %w", err)
	}
	article.Title = title
	article.Type = typ
	article.Date = date
	article.PublishedAt = publishedAt
	if source != "" {
		article.Source = source
	}
	xghNormalizeArticleContent(article, contentSel, baseURL)
	xghAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

// xghNormalizeArticleContent keeps only actual HTTP(S) resource candidates
// and tries all CMS lazy-load attributes. The shared parser deliberately
// remains generic; xgh's live pages sometimes leave a javascript placeholder
// in src while the real URL is in orisrc/data-src.
func xghNormalizeArticleContent(article *model.Article, contentSel *goquery.Selection, baseURL string) {
	if article == nil || contentSel == nil || contentSel.Length() == 0 {
		return
	}
	images := make([]string, 0)
	seenImages := make(map[string]bool)
	contentSel.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := xghResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				resolved = candidate
				break
			}
		}
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
		if !seenImages[resolved] {
			seenImages[resolved] = true
			images = append(images, resolved)
		}
	})
	article.Images = images

	contentSel.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		if resolved := xghResourceURL(baseURL, href); resolved != "" {
			link.SetAttr("href", resolved)
		} else {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
		}
	})
	if contentHTML, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = contentHTML
	}
}

func xghArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	if wrapper := doc.Find(".news-list-wrap > .bd").First(); wrapper.Length() > 0 {
		for _, selector := range []string{
			"#vsb_content > .v_news_content",
			"#vsb_content_501 > .v_news_content",
		} {
			if content := wrapper.Find(selector).First(); content.Length() > 0 {
				return content
			}
		}
	}
	// Compatibility with the older, synthetic Boda detail fixture. The live
	// xgh template always takes the scoped branch above.
	for _, selector := range []string{"#vsb_content_501 > .v_news_content", "#vsb_content > .v_news_content"} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return &goquery.Selection{}
}

func xghFixedArticleFields(doc *goquery.Document) (title, typ, date, publishedAt, source string, err error) {
	if doc == nil {
		return "", "", "", "", "", fmt.Errorf("xgh: nil article document")
	}
	if wrapper := doc.Find(".news-list-wrap").First(); wrapper.Length() > 0 {
		header := wrapper.ChildrenFiltered(".hd").First()
		bd := wrapper.ChildrenFiltered(".bd").First()
		// The current template nests .title inside <form
		// name="_newscontent_fromname">, so a direct-child lookup misses it;
		// search descendants within .bd instead.
		titleBox := bd.Find(".title").First()
		title = strings.Join(strings.Fields(titleBox.ChildrenFiltered("h3").First().Text()), " ")
		typ = strings.Join(strings.Fields(header.ChildrenFiltered("h3").First().Text()), " ")
		date, publishedAt, source = xghDetailInfo(titleBox.ChildrenFiltered(".info").First())
		if title == "" || typ == "" || date == "" {
			return "", "", "", "", "", fmt.Errorf("xgh: fixed detail metadata is incomplete (title=%q type=%q date=%q)", title, typ, date)
		}
		return title, typ, date, publishedAt, source, nil
	}

	// Narrow compatibility branch for the pre-existing generic adapter smoke
	// fixture; do not use these broad selectors when a real xgh detail wrapper
	// is present.
	title = strings.Join(strings.Fields(doc.Find(".article-title").First().Text()), " ")
	typ = xghDocumentType(doc)
	if title == "" {
		title = strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	}
	if info := doc.Find(".news_source").First(); info.Length() > 0 {
		info.Find("span").EachWithBreak(func(_ int, node *goquery.Selection) bool {
			value := strings.TrimSpace(node.Text())
			if strings.HasPrefix(value, "日期：") || strings.HasPrefix(value, "日期:") {
				date, publishedAt = xghDateValues(strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(value, "日期："), "日期:"), " ")))
				return false
			}
			return true
		})
	}
	if title == "" || typ == "" || date == "" {
		return "", "", "", "", "", fmt.Errorf("xgh: detail metadata is incomplete (title=%q type=%q date=%q)", title, typ, date)
	}
	return title, typ, date, publishedAt, source, nil
}

func xghDetailInfo(info *goquery.Selection) (date, publishedAt, source string) {
	if info == nil || info.Length() == 0 {
		return "", "", ""
	}
	info.Find("span").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		value := strings.Join(strings.Fields(node.Text()), " ")
		if strings.HasPrefix(value, "日期：") || strings.HasPrefix(value, "日期:") {
			value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "日期："), "日期:"))
			date, publishedAt = xghDateValues(value)
			return false
		}
		if strings.HasPrefix(value, "来源：") || strings.HasPrefix(value, "来源:") {
			source = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "来源："), "来源:"))
		} else if source == "" && value != "" && !strings.Contains(value, "点击数") && !strings.Contains(value, "阅读次数") {
			source = value
		}
		return true
	})
	return date, publishedAt, source
}

func xghAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) || link.Closest(".breadcrumb, .news-list-wrap > .hd").Length() > 0 {
			return
		}
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if href == "" || bodaIsPseudoResourceHref(href) || !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := xghResourceURL(baseURL, href)
		if resolved == "" || xghArticleURL(resolved, baseURL) || seen[resolved] {
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
	})
}

func xghArticleURL(rawURL, baseURL string) bool {
	_, ok := xghArticleURLValue(baseURL, rawURL)
	return ok
}

func xghArticleURLValue(baseURL, rawURL string) (string, bool) {
	rawURL = html.UnescapeString(strings.TrimSpace(rawURL))
	if rawURL == "" || bodaIsPseudoResourceHref(rawURL) {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, rawURL))
	parsed, err := url.Parse(fullURL)
	base, baseErr := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || baseErr != nil || parsed.Hostname() == "" || base.Hostname() == "" {
		return "", false
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", false
	}
	if !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	if base.Port() != "" && parsed.Port() != base.Port() {
		return "", false
	}
	if !xghArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func xghBaseURL() string {
	if site := AllSites["xgh"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return xghCanonicalBaseURL
}

// xghFetchArticle is the live detail path. The parser can still be used with
// saved HTML fixtures, while this fetch hook verifies every advertised image
// and attachment before exposing the normalized article.
func xghFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := xghBaseURL()
	fullURL, ok := xghArticleURLValue(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("xgh: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_xghArticle(body, baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("xgh article %s: %w", fullURL, err)
	}
	article.Images = xghVerifyImages(article.Images, baseURL)
	article.Attachments = xghVerifyAttachments(article.Attachments, baseURL)
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		validImages[image] = true
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		validAttachments[attachment.URL] = true
	}
	article.ContentHTML = xghSanitizeContentHTML(article.ContentHTML, baseURL, validImages, validAttachments)
	return article, body, nil
}

func xghResourceURL(baseURL, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	return bodaResourceURL(baseURL, raw)
}

func xghVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := xghResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := xghFetchResource(resolved, baseURL, "", true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func xghVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := xghResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		name := strings.TrimSpace(attachment.Name + " " + resolved)
		body, _, err := xghFetchResource(resolved, baseURL, name, false)
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

func xghFetchResource(rawURL, baseURL, name string, image bool) ([]byte, string, error) {
	resolved := xghResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("xgh: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("xgh: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	if strings.TrimSpace(baseURL) != "" {
		req.Header.Set("Referer", strings.TrimRight(baseURL, "/")+"/")
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
		return nil, "", fmt.Errorf("xgh: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("xgh: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, xghMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("xgh: read resource %s: %w", resolved, err)
	}
	if len(body) > xghMaxResourceBytes || !xghResourceBodyIsUsable(contentType, body, name+" "+resolved, image) {
		return nil, contentType, fmt.Errorf("xgh: resource %s returned invalid type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func xghResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
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
		"<!doctype html", "<html", "<head", "<body", "附件下载", "请输入验证码", "验证码下载附件",
		"请先登录", "请登录后", "登录页面", "页面不存在", "404 not found", "access denied",
		"cloudflare", "just a moment", "安全验证", "请求被阻断", "captcha", "waf",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}
	magic := xghResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && xghIsImageMagic(magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return xghMagicIsZIP(magic)
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return xghMagicIsZIP(magic) || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.Contains(lowerName, ".tsv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values"
	case strings.HasPrefix(mediaType, "text/"):
		return magic == "text" || !strings.Contains(textProbe, "<")
	default:
		return magic != ""
	}
}

func xghMagicIsZIP(magic string) bool {
	return magic == "zip"
}

func xghIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func xghResourceMagic(body []byte) string {
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
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 {
		probe := trimmed
		if len(probe) > 4096 {
			probe = probe[:4096]
		}
		lower := strings.ToLower(string(probe))
		if strings.HasPrefix(lower, "<svg") || (strings.HasPrefix(lower, "<?xml") && strings.Contains(lower, "<svg")) {
			return "svg"
		}
		if !strings.Contains(lower, "<") {
			return "text"
		}
	}
	return ""
}

func xghSanitizeContentHTML(rawHTML, baseURL string, validImages, validAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="xgh-content">` + rawHTML + `</div>`))
	if err != nil {
		return rawHTML
	}
	content := doc.Find("#xgh-content").First()
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := xghResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				resolved = candidate
				break
			}
		}
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := xghResourceURL(baseURL, href)
		if resolved == "" || !validAttachments[resolved] {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		link.SetAttr("href", resolved)
	})
	if contentHTML, htmlErr := content.Html(); htmlErr == nil {
		return contentHTML
	}
	return rawHTML
}

func xghRejectShell(doc *goquery.Document, kind string) error {
	if doc == nil {
		return fmt.Errorf("xgh: nil %s document", kind)
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	visible := doc.Find("body").First().Clone()
	if kind == "article" {
		// “验证码” can be legitimate prose. Inspect only the shell/meta zone
		// for articles, never the fixed prose body.
		visible.Find("#vsb_content, #vsb_content_501, .v_news_content, .detail").Remove()
	}
	visible.Find("script, style, noscript, template").Remove()
	pageText := strings.ToLower(strings.Join(strings.Fields(visible.Text()), " "))
	for _, marker := range xghShellMarkers {
		if strings.Contains(title, marker) || strings.Contains(pageText, marker) {
			return fmt.Errorf("xgh: received %s verification/error page", kind)
		}
	}
	if doc.Find(`#captcha, .captcha, [id*="captcha"], [class*="captcha"], input[name*="code"], input[id*="code"]`).Length() > 0 {
		return fmt.Errorf("xgh: received %s verification page", kind)
	}
	return nil
}
