package scraper

import (
	"bytes"
	"fmt"
	htmlstd "html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	htmlnode "golang.org/x/net/html"

	"swjtu-cli/internal/model"
)

const bwcBaseURL = "https://bwc.swjtu.edu.cn"

const bwcMaxResourceBytes = 32 << 20

// These are the reachable article-list entry points found in the site's
// navigation, homepage modules, and secondary navigation. Static profile/
// process pages, the empty English party branch, and the old safety-case URL
// (which currently returns a non-list shell) are deliberately not feeds.
var bwcSubPages = []string{
	"tzgg",
	"gzdt",
	"fwzn",
	"fwzn/bslc",
	"aqjs",
	"aqjs/aqjy",
	"wzgz/zbgz",
	"zcfg/flfg",
	"zcfg/gzzd",
	"English/News",
	"English/Laws",
	"English/Rules",
	"English/Announcement",
	"English/Service_Guide",
	"English/Service_Guide/Work_Processes",
}

var (
	bwcArticlePathRE  = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	bwcPageCounterRE  = regexp.MustCompile(`(?:^|[^0-9])[0-9]+\s*/\s*([0-9]+)(?:[^0-9]|$)`)
	bwcAttachmentRE   = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
	bwcHTMLShellRE    = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|您访问的页面不存在|访问被拒绝|请求已被阻断|安全验证|人机验证|access\s+denied|request\s+blocked|just\s+a\s+moment|cloudflare`)
	bwcResourceMarkRE = regexp.MustCompile(`(?i)(?:/|^)(?:captcha|verify(?:code|image)?|verification|createimage|getcode|codevalue|waf|access[-_]denied|login)(?:[/?.]|$)|(?:验证码|人机验证|安全验证)`)
)

// bwcAdapter keeps this site's non-standard list and article markup out of
// the shared 博达 parser. In particular, a successful HTML shell is not a
// successful feed: every parser below requires the site's real content nodes.
func init() {
	registerAdapter("bwc", SiteAdapter{
		SubPages:     bwcSubPages,
		ParseList:    parseAdapter_bwcList,
		ParseArticle: parseAdapter_bwcArticle,
		PageURL:      bwcPageURL,
		ListPage:     bwcListPage,
		FetchArticle: bwcFetchArticle,
	})
}

func parseAdapter_bwcList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBwcNewsList(html, baseURL)
}

func parseAdapter_bwcArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBwcArticle(html, baseURL)
}

// bwcPageURL maps a user-facing page number to the generated file name. BWC
// publishes the newest page as slug.htm, then writes logical page 2 to
// slug/{total-1}.htm, with the oldest page at slug/1.htm.
func bwcPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	slug = bwcCanonicalSlug(slug)
	if page < 1 || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if slug == "" || strings.EqualFold(slug, "index") {
		if page > 1 {
			return ""
		}
		return baseURL + "/index" + extension
	}
	if page == 1 {
		return baseURL + "/" + slug + extension
	}
	if totalPages <= 1 {
		return ""
	}
	generatedPage := totalPages - page + 1
	if generatedPage < 1 {
		return ""
	}
	return baseURL + "/" + slug + "/" + strconv.Itoa(generatedPage) + extension
}

// bwcListPage is used by the shared BodaSite pagination API, but keeps all
// BWC-specific fetching and reverse numbering inside this adapter.
func bwcListPage(slug string, page int) ([]model.NewsItem, int, error) {
	baseURL := bwcBaseURL
	if site := AllSites["bwc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	slug = bwcCanonicalSlug(slug)
	if !bwcIsKnownListRoute(slug) {
		return nil, 0, fmt.Errorf("bwc: %s is not an audited article-list route", slug)
	}
	if page < 1 {
		return nil, 0, fmt.Errorf("bwc: list page must be positive: %d", page)
	}

	totalPages := 0
	if page > 1 {
		firstBody, err := bwcFetchListPage(bwcPageURL(baseURL, slug, 1, 0, ".htm"))
		if err != nil {
			return nil, 0, fmt.Errorf("bwc: read first page for %s: %w", slug, err)
		}
		totalPages = bwcTotalPages(firstBody)
		if totalPages < 1 {
			return nil, 0, fmt.Errorf("bwc: %s does not expose a reachable page count", slug)
		}
	}

	body, err := bwcFetchListPage(bwcPageURL(baseURL, slug, page, totalPages, ".htm"))
	if err != nil {
		return nil, 0, err
	}
	items, err := parseBwcNewsList(body, baseURL)
	if err != nil {
		return nil, 0, err
	}
	if totalPages == 0 {
		totalPages = bwcTotalPages(body)
	}
	if totalPages == 0 {
		totalPages = 1
	}
	return items, totalPages, nil
}

func bwcFetchListPage(pageURL string) ([]byte, error) {
	if strings.TrimSpace(pageURL) == "" {
		return nil, fmt.Errorf("bwc: empty list URL")
	}
	body, err := fetch(pageURL)
	if err == nil && bwcRecognizedListHTML(body) {
		return body, nil
	}
	if strings.HasSuffix(strings.ToLower(pageURL), ".htm") {
		if fallback, fallbackErr := fetch(strings.TrimSuffix(pageURL, ".htm") + ".html"); fallbackErr == nil {
			if bwcRecognizedListHTML(fallback) {
				return fallback, nil
			}
		}
	}
	if err == nil {
		return nil, fmt.Errorf("bwc: list %s returned an error/WAF/static shell", pageURL)
	}
	return nil, fmt.Errorf("bwc: fetch list %s: %w", pageURL, err)
}

func bwcTotalPages(html []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}
	if bwcHTMLShellReason(doc) != "" {
		return 0
	}

	// English pages report e.g. "共65条 1/4" in an element named fanye.
	for _, text := range []string{
		doc.Find("[id^='fanye']").Text(),
		doc.Find(".p_t").Text(),
	} {
		if match := bwcPageCounterRE.FindStringSubmatch(strings.Join(strings.Fields(text), " ")); match != nil {
			if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
				return total
			}
		}
	}
	if total := parseTotalPages(html); total > 0 {
		return total
	}

	if doc.Find(".text-wrapper_4, .advice, .aq, .zc, .xz").Length() > 0 {
		return 1
	}
	if doc.Find("div.group_7, #MainContent_GridView1").Length() > 0 {
		return 1
	}
	return 0
}

// bwcRecognizedListHTML is intentionally narrower than a generic HTTP
// success check. BWC has returned 200 for WAF/404 pages and for client-side
// shells; neither may be used to infer a one-page archive.
func bwcRecognizedListHTML(raw []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil || bwcHTMLShellReason(doc) != "" {
		return false
	}
	pageType := bwcPageType(doc)
	if pageType == "3" {
		return false
	}
	return doc.Find(".text-wrapper_4, .advice, .aq, .zc, .xz, div.group_7, #MainContent_GridView1").Length() > 0
}

func bwcPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{`meta[name="pageType"]`, `meta[name="pagetype"]`} {
		if value := strings.TrimSpace(doc.Find(selector).First().AttrOr("content", "")); value != "" {
			return value
		}
	}
	return ""
}

func bwcHTMLShellReason(doc *goquery.Document) string {
	if doc == nil {
		return "nil document"
	}
	title := strings.Join(strings.Fields(doc.Find("title").First().Text()), " ")
	if bwcHTMLShellRE.MatchString(title) {
		return title
	}
	body := doc.Find("body").First().Clone()
	if body.Length() == 0 {
		return "missing body"
	}
	body.Find("script, style, noscript").Remove()
	visible := strings.Join(strings.Fields(body.Text()), " ")
	// A normal article may discuss security verification or a CAPTCHA. Only
	// classify visible shell text as an error when no BWC content container is
	// present, so body text cannot replace the fixed metadata/content scope.
	if bwcHTMLShellRE.MatchString(visible) && doc.Find("#vsb_content .v_news_content, #vsb_content_2 .v_news_content, div.group_7, #MainContent_GridView1, .text-wrapper_4, .advice, .aq, .zc, .xz").Length() == 0 {
		return visible
	}
	if doc.Find("#app, #root, #__next").Length() > 0 && doc.Find("#vsb_content .v_news_content, #vsb_content_2 .v_news_content, div.group_7, #MainContent_GridView1, .text-wrapper_4, .advice, .aq, .zc, .xz").Length() == 0 {
		return "SPA shell without server-rendered content"
	}
	return ""
}

func bwcCanonicalSlug(slug string) string {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	// The current homepage still links this legacy label. Its reachable
	// server-rendered archive is the canonical zbgz list, whose pagination was
	// audited separately; normalize the alias before constructing URLs.
	if strings.EqualFold(slug, "wzgz/dxszbgz") {
		return "wzgz/zbgz"
	}
	return slug
}

func bwcIsKnownListRoute(slug string) bool {
	slug = bwcCanonicalSlug(slug)
	for _, route := range bwcSubPages {
		if strings.EqualFold(route, slug) {
			return true
		}
	}
	return false
}

func parseBwcNewsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("bwc: parse list HTML: %w", err)
	}
	if reason := bwcHTMLShellReason(doc); reason != "" {
		return nil, fmt.Errorf("bwc: list response is an error/WAF/SPA shell: %s", reason)
	}
	if pageType := bwcPageType(doc); pageType == "3" {
		return nil, fmt.Errorf("bwc: page is an article detail (pageType %q)", pageType)
	}

	var items []model.NewsItem
	seen := make(map[string]bool)
	add := func(href, title, date, typ string) {
		fullURL, ok := bwcArticleURL(baseURL, href)
		if !ok {
			return
		}
		title = cleanTitle(strings.Join(strings.Fields(title), " "))
		if !isValidNewsTitle(title) || seen[fullURL] {
			return
		}
		date, publishedAt := bwcListDate(date)
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

	// The homepage contains article-looking carousel/recommendation URLs.
	// Only these five named modules are article feeds; no document-wide scan is
	// allowed here.
	homeSections := []struct {
		selector string
		typ      string
	}{
		{selector: ".text-wrapper_4", typ: "工作动态"},
		{selector: ".advice", typ: "通知公告"},
		{selector: ".aq", typ: "安全警示"},
		{selector: ".zc", typ: "政策法规"},
		{selector: ".xz", typ: "下载专区"},
	}
	homeFound := false
	for _, section := range homeSections {
		found := doc.Find(section.selector)
		if found.Length() == 0 {
			continue
		}
		homeFound = true
		bwcEachArticleCandidate(found, func(sel *goquery.Selection) {
			title := bwcCandidateTitle(sel)
			date := strings.TrimSpace(sel.Find(".date, .time, .text-group_3").First().Text())
			add(sel.AttrOr("href", ""), title, date, section.typ)
		})
	}
	if homeFound {
		if len(items) == 0 {
			return nil, fmt.Errorf("bwc: homepage modules contained no real article items")
		}
		return items, nil
	}

	// Chinese BWC list pages use a div with the article URL as its href.
	groups := doc.Find(".right div.group_7[href]")
	if groups.Length() == 0 {
		groups = doc.Find("div.group_7[href]")
	}
	if groups.Length() > 0 {
		typ := bwcBreadcrumbType(doc)
		groups.Each(func(_ int, sel *goquery.Selection) {
			title := bwcCandidateTitle(sel)
			date := strings.TrimSpace(sel.Find(".text-group_3").First().Text())
			add(sel.AttrOr("href", ""), title, date, typ)
		})
		if len(items) == 0 {
			return nil, fmt.Errorf("bwc: group_7 list contained no valid article items")
		}
		return items, nil
	}

	// The old English pages are server-rendered tables. Restrict extraction to
	// this table so header/menu/related links cannot become articles.
	englishTable := doc.Find("#MainContent_GridView1")
	if englishTable.Length() > 0 {
		typ := bwcBreadcrumbType(doc)
		englishTable.Find("tr").Each(func(_ int, row *goquery.Selection) {
			var articleLink *goquery.Selection
			row.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
				if _, ok := bwcArticleURL(baseURL, link.AttrOr("href", "")); ok {
					articleLink = link
					return false
				}
				return true
			})
			if articleLink == nil {
				return
			}
			title := strings.TrimSpace(articleLink.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(articleLink.Text())
			}
			date := strings.TrimSpace(row.Find("span[id*='Label7'], .date, .time").First().Text())
			add(articleLink.AttrOr("href", ""), title, date, typ)
		})
		if len(items) == 0 {
			return nil, fmt.Errorf("bwc: English article table contained no real rows")
		}
		return items, nil
	}

	return nil, fmt.Errorf("bwc: no recognized article-list container")
}

func bwcEachArticleCandidate(scope *goquery.Selection, visit func(*goquery.Selection)) {
	if scope == nil || scope.Length() == 0 || visit == nil {
		return
	}
	// Some home modules put href on the row itself; others use an anchor. A
	// strict article URL check in add() remains the final gate, so More/menu
	// links in the same module cannot leak into the feed.
	if _, ok := scope.Attr("href"); ok {
		visit(scope)
	}
	scope.Find(".item[href], a[href]").Each(func(_ int, candidate *goquery.Selection) {
		visit(candidate)
	})
}

func bwcCandidateTitle(candidate *goquery.Selection) string {
	if candidate == nil || candidate.Length() == 0 {
		return ""
	}
	if value := strings.TrimSpace(candidate.Find(".title").First().Text()); value != "" {
		return value
	}
	if value := strings.TrimSpace(candidate.AttrOr("title", "")); value != "" {
		return value
	}
	if node := candidate.Get(0); node != nil && strings.EqualFold(node.Data, "a") {
		return strings.TrimSpace(candidate.Text())
	}
	return strings.TrimSpace(candidate.Find("a").First().Text())
}

func bwcArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !bwcArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !isValidNewsURL(fullURL) {
		return "", false
	}
	return fullURL, true
}

func bwcListDate(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if normalized := normalizePublishedAt(raw); normalized != "" {
		if publishedAtHasClock(normalized) {
			return normalized[:10], normalized
		}
		return normalized[:10], ""
	}
	return "", ""
}

func bwcBreadcrumbType(doc *goquery.Document) string {
	var values []string
	doc.Find(".nav .text").Each(func(_ int, sel *goquery.Selection) {
		if value := strings.Join(strings.Fields(sel.Text()), " "); value != "" {
			values = append(values, value)
		}
	})
	if len(values) == 0 {
		doc.Find("#MainContent_LinkHome").Each(func(_ int, sel *goquery.Selection) {
			if value := strings.Join(strings.Fields(sel.Text()), " "); value != "" {
				values = append(values, value)
			}
		})
	}
	for i := len(values) - 1; i >= 0; i-- {
		switch strings.ToLower(strings.TrimSpace(values[i])) {
		case "", "首页", "主页", "正文", "home", "中文":
			continue
		default:
			return values[i]
		}
	}
	return ""
}

func parseBwcArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("bwc: parse article HTML: %w", err)
	}
	if reason := bwcHTMLShellReason(doc); reason != "" {
		return nil, fmt.Errorf("bwc: article response is an error/WAF/SPA shell: %s", reason)
	}
	if pageType := bwcPageType(doc); pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("bwc: page is not an article detail (pageType %q)", pageType)
	}

	var title string
	// BWC has two fixed detail templates: the Chinese c1088738 shell and the
	// old ASP.NET English shell. Do not fall back to h1/meta/pageTitle: those
	// fields also occur on home/list shells and are not article metadata.
	for _, selector := range []string{".c1088738_title", "#MainContent_LabelTitle"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			if value := strings.Join(strings.Fields(found.Text()), " "); value != "" {
				title = cleanTitle(value)
				break
			}
		}
	}

	var contentSel *goquery.Selection
	for _, selector := range []string{"#vsb_content .v_news_content", "#vsb_content_2 .v_news_content"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			contentSel = found
			break
		}
	}
	if title == "" || contentSel == nil || contentSel.Length() == 0 || strings.TrimSpace(contentSel.Text()) == "" && contentSel.Find("img, table, a").Length() == 0 {
		return nil, fmt.Errorf("bwc: page is not a rendered article detail")
	}

	date, publishedAt := bwcArticleDate(doc)
	article := &model.Article{
		Title:       title,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        bwcBreadcrumbType(doc),
		Source:      bwcSource(doc),
		Author:      bwcAuthor(doc),
		Content:     bwcArticleText(contentSel),
		Images:      bwcArticleImages(contentSel, baseURL),
	}

	attachments := bwcArticleAttachments(doc, contentSel, baseURL)
	article.Attachments = attachments

	contentSel.Find("img").Each(func(_ int, sel *goquery.Selection) {
		raw := bwcImageSource(sel)
		if resolved := bwcResourceURL(baseURL, raw); resolved != "" {
			sel.SetAttr("src", resolved)
		}
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			sel.RemoveAttr(attr)
		}
	})
	contentSel.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		if resolved := bwcLinkURL(baseURL, sel.AttrOr("href", "")); resolved != "" {
			sel.SetAttr("href", resolved)
		}
	})
	if contentHTML, err := contentSel.Html(); err == nil {
		article.ContentHTML = contentHTML
	}

	return article, nil
}

func bwcArticleDate(doc *goquery.Document) (string, string) {
	for _, selector := range []string{".c1088738_date", "#MainContent_LabelDate"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			if normalized := normalizePublishedAt(found.Text()); normalized != "" {
				if publishedAtHasClock(normalized) {
					return normalized[:10], normalized
				}
				return normalized[:10], ""
			}
		}
	}
	return "", ""
}

func bwcSource(doc *goquery.Document) string {
	for _, selector := range []string{".c1088738_source", "#MainContent_LabelSource"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			value := strings.Join(strings.Fields(found.Text()), " ")
			value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "来源："), "来源:"))
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func bwcAuthor(doc *goquery.Document) string {
	if found := doc.Find(".c1088738_author, #MainContent_LabelAuthor").First(); found.Length() > 0 {
		if value := strings.Join(strings.Fields(found.Text()), " "); value != "" {
			return value
		}
	}
	dateSel := doc.Find("#MainContent_LabelDate").First()
	if dateSel.Length() > 0 {
		authorRE := regexp.MustCompile(`录入\s*[:：]?\s*([^\s]+)`)
		if match := authorRE.FindStringSubmatch(strings.Join(strings.Fields(dateSel.Parent().Text()), " ")); len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func bwcArticleText(contentSel *goquery.Selection) string {
	clone := contentSel.Clone()
	clone.Find("script, style, noscript").Remove()
	const nestedBlocks = "p,h1,h2,h3,h4,h5,h6,li,td,th,caption,blockquote"
	var parts []string
	clone.Find(nestedBlocks).Each(func(_ int, sel *goquery.Selection) {
		node := sel.Get(0)
		if node == nil {
			return
		}
		tag := strings.ToLower(node.Data)
		if (tag == "li" || tag == "td" || tag == "th" || tag == "caption" || tag == "blockquote") && sel.Find(nestedBlocks).Length() > 0 {
			return
		}
		if value := strings.Join(strings.Fields(sel.Text()), " "); value != "" {
			parts = append(parts, value)
		}
	})
	if len(parts) == 0 {
		return strings.Join(strings.Fields(clone.Text()), " ")
	}
	return strings.Join(parts, "\n")
}

func bwcArticleImages(contentSel *goquery.Selection, baseURL string) []string {
	seen := make(map[string]bool)
	var images []string
	contentSel.Find("img").Each(func(_ int, sel *goquery.Selection) {
		fullURL := bwcResourceURL(baseURL, bwcImageSource(sel))
		if fullURL != "" && !seen[fullURL] {
			seen[fullURL] = true
			images = append(images, fullURL)
		}
	})
	return images
}

func bwcArticleAttachments(doc *goquery.Document, contentSel *goquery.Selection, baseURL string) []model.Attachment {
	root := contentSel.Closest(".text-group_1, form").First()
	if root.Length() == 0 {
		root = contentSel.Parent()
	}

	seen := make(map[string]bool)
	var attachments []model.Attachment
	collect := func(scope *goquery.Selection) {
		if scope == nil || scope.Length() == 0 {
			return
		}
		scope.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
			href := strings.TrimSpace(link.AttrOr("href", ""))
			fullURL := bwcResourceURL(baseURL, href)
			if href == "" || fullURL == "" || seen[fullURL] || !bwcIsAttachmentLinkAt(link, href, baseURL) {
				return
			}
			seen[fullURL] = true
			attachments = append(attachments, model.Attachment{
				Name: bwcAttachmentName(link, fullURL),
				URL:  fullURL,
			})
		})
	}
	collect(contentSel)
	collect(root) // catches BWC's attachment blocks outside .v_news_content
	// Some templates place the accessory block beside the article wrapper.
	for _, selector := range []string{".attachments", ".attachment", ".accessories", ".accessory", ".annex", "[id*='Attach']"} {
		collect(doc.Find(selector))
	}
	return attachments
}

func bwcIsAttachmentLink(link *goquery.Selection, href string) bool {
	return bwcIsAttachmentLinkAt(link, href, "")
}

func bwcIsAttachmentLinkAt(link *goquery.Selection, href, baseURL string) bool {
	if bwcAttachmentRE.MatchString(strings.ToLower(href)) {
		if baseURL == "" {
			return true
		}
		resolved := bwcResourceURL(baseURL, href)
		return resolved != "" && bwcSameHost(baseURL, resolved)
	}
	for _, attr := range []string{"download", "data-download", "data-file", "data-attachment", "data-filename", "data-name"} {
		if value, ok := link.Attr(attr); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	text := strings.ToLower(strings.Join(strings.Fields(link.Text()), " "))
	if strings.Contains(text, "下载") || strings.Contains(text, "download") {
		return true
	}
	for _, node := range append(link.Nodes, link.Parents().Nodes...) {
		marker := strings.ToLower(node.Data + " " + nodeAttr(node, "class") + " " + nodeAttr(node, "id"))
		if strings.Contains(marker, "attachment") || strings.Contains(marker, "accessor") ||
			strings.Contains(marker, "annex") || strings.Contains(marker, "appendix") ||
			strings.Contains(marker, "fujian") || strings.Contains(marker, "附件") ||
			strings.Contains(marker, "download") || strings.Contains(marker, "file") {
			return true
		}
	}
	return false
}

// Keep the old two-argument helper available to package-local callers while
// requiring the parser itself to distinguish a same-site file from an
// ordinary external PDF link.
func bwcIsAttachmentLinkForBase(link *goquery.Selection, href, baseURL string) bool {
	return bwcIsAttachmentLinkAt(link, href, baseURL)
}

func nodeAttr(node *htmlnode.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

func bwcAttachmentName(link *goquery.Selection, fullURL string) string {
	for _, attr := range []string{"download", "data-filename", "data-name", "name"} {
		if value := bwcCleanAttachmentName(link.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	if value := bwcCleanAttachmentName(link.Text()); value != "" {
		return value
	}
	if row := link.Closest("tr"); row.Length() > 0 {
		var value string
		row.Find("td, th").EachWithBreak(func(_ int, cell *goquery.Selection) bool {
			candidate := bwcCleanAttachmentName(cell.Text())
			if candidate == "" || candidate == bwcCleanAttachmentName(link.Text()) {
				return true
			}
			value = candidate
			return false
		})
		if value != "" {
			return value
		}
	}
	parsed, err := url.Parse(fullURL)
	if err == nil {
		if value, err := url.PathUnescape(path.Base(parsed.Path)); err == nil && value != "." && value != "/" {
			return value
		}
	}
	return "附件"
}

func bwcCleanAttachmentName(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	for _, generic := range []string{"下载", "点击下载", "下载附件", "download", "click to download", "附件"} {
		if lower == generic {
			return ""
		}
	}
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "附件："), "附件:"))
	return value
}

func bwcImageSource(image *goquery.Selection) string {
	if image == nil || image.Length() == 0 {
		return ""
	}
	var fallback string
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		value := strings.TrimSpace(image.AttrOr(attr, ""))
		if value == "" {
			continue
		}
		if fallback == "" {
			fallback = value
		}
		if !bwcRawResourceMarked(value) {
			return value
		}
	}
	return fallback
}

func bwcRawResourceMarked(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(htmlstd.UnescapeString(raw)))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return true
	}
	for _, marker := range []string{
		"captcha", "verifycode", "verification", "createimage", "getcode", "codevalue",
		"access-denied", "access_denied", "waf", "login", "验证码", "人机验证", "安全验证",
		"spacer.gif", "blank.gif", "transparent.gif", "placeholder", "loading.gif", "pixel.gif",
	} {
		if strings.Contains(raw, marker) {
			return true
		}
	}
	return bwcResourceMarkRE.MatchString(raw)
}

func bwcResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bwcRawResourceMarked(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	parsed.Fragment = ""
	return cleanURL(parsed.String())
}

func bwcLinkURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "#") {
		return raw
	}
	return bwcResourceURL(baseURL, raw)
}

func bwcSameHost(baseURL, candidate string) bool {
	base, baseErr := url.Parse(baseURL)
	target, targetErr := url.Parse(candidate)
	return baseErr == nil && targetErr == nil && base.Hostname() != "" && target.Hostname() != "" && strings.EqualFold(base.Hostname(), target.Hostname())
}

func bwcAttachmentCandidate(link *goquery.Selection, href, baseURL string) bool {
	return bwcIsAttachmentLinkAt(link, href, baseURL)
}

func bwcFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := bwcBaseURL
	if site := AllSites["bwc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	fullURL, ok := bwcArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("bwc: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("bwc: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_bwcArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("bwc: parse article %s: %w", fullURL, err)
	}
	article.Images = bwcVerifyImages(article.Images, baseURL, fullURL)
	article.Attachments = bwcVerifyAttachments(article.Attachments, baseURL, fullURL)
	article.ContentHTML = bwcSanitizeContentHTML(article.ContentHTML, baseURL, article.Images, article.Attachments)
	article.Content = bwcContentTextFromHTML(article.ContentHTML)
	return article, body, nil
}

func bwcVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := bwcResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := bwcFetchResource(resolved, baseURL, referer, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func bwcVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := bwcResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := bwcFetchResource(resolved, baseURL, referer, false)
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

func bwcFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	resolved := bwcResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("bwc: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("bwc: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := http.Client{}
	if defaultClient != nil {
		client = *defaultClient
	}
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[0].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("bwc: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("bwc: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, bwcMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("bwc: read resource %s: %w", resolved, err)
	}
	if len(body) > bwcMaxResourceBytes || !bwcResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("bwc: resource %s returned an invalid type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func bwcResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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
	if bwcHTMLShellRE.Match(probe) {
		return false
	}

	magic := bwcResourceMagic(body)
	if image {
		if !strings.HasPrefix(mediaType, "image/") {
			return false
		}
		expected := map[string]string{
			"image/jpeg": "jpeg", "image/jpg": "jpeg", "image/png": "png",
			"image/gif": "gif", "image/webp": "webp", "image/bmp": "bmp",
			"image/tiff": "tiff", "image/svg+xml": "svg",
		}[mediaType]
		return magic != "" && (expected == "" || magic == expected)
	}

	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return bwcBinaryContentTypeOK(mediaType, "pdf") && magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return bwcBinaryContentTypeOK(mediaType, "office-zip") && magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return bwcBinaryContentTypeOK(mediaType, "office-ole") && magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return bwcBinaryContentTypeOK(mediaType, "archive") && (magic == "zip" || magic == "rar" || magic == "7z")
	case strings.Contains(lowerName, ".rtf") || mediaType == "application/rtf" || mediaType == "text/rtf":
		return bwcBinaryContentTypeOK(mediaType, "rtf") && magic == "rtf"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func bwcBinaryContentTypeOK(mediaType, kind string) bool {
	if mediaType == "application/octet-stream" || mediaType == "application/force-download" || mediaType == "application/download" {
		return true
	}
	switch kind {
	case "pdf":
		return mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf")
	case "office-zip":
		return mediaType == "application/zip" || strings.Contains(mediaType, "officedocument") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml")
	case "office-ole":
		return mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint"
	case "archive":
		return mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed"
	case "rtf":
		return mediaType == "application/rtf" || mediaType == "text/rtf"
	default:
		return false
	}
}

func bwcResourceMagic(body []byte) string {
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
	if bytes.HasPrefix(trimmed, []byte("{\\rtf")) {
		return "rtf"
	}
	probe := trimmed
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if bytes.Contains(bytes.ToLower(probe), []byte("<svg")) {
		return "svg"
	}
	return ""
}

func bwcSanitizeContentHTML(raw, baseURL string, images []string, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	validImages := make(map[string]bool, len(images))
	for _, image := range images {
		if resolved := bwcResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		if resolved := bwcResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"bwc-content-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	root := doc.Find("#bwc-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := bwcResourceURL(baseURL, bwcImageSource(image))
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if href == "" {
			link.Remove()
			return
		}
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(strings.ToLower(href), "#") {
				link.Remove()
			}
			return
		}
		resolved := bwcLinkURL(baseURL, href)
		if resolved == "" {
			link.Remove()
			return
		}
		if bwcAttachmentCandidate(link, href, baseURL) {
			resourceURL := bwcResourceURL(baseURL, resolved)
			if resourceURL == "" || !validAttachments[resourceURL] {
				link.Remove()
				return
			}
			resolved = resourceURL
		}
		link.SetAttr("href", resolved)
	})
	rendered, err := root.Html()
	if err != nil {
		return ""
	}
	return rendered
}

func bwcContentTextFromHTML(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"bwc-text-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	return bwcArticleText(doc.Find("#bwc-text-root").First())
}
