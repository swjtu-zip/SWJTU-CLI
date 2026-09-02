package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const hzcCanonicalBaseURL = "https://hzc.swjtu.edu.cn"

// hzcSubPages contains every article-bearing list discovered from the site's
// navigation, its hidden news tabs, and the homepage's "more" links.  The
// parent pages for 部门概况、对外合作、继续教育、对口支援 and 定点帮扶 are
// landing/static pages and are intentionally not feeds.
var hzcSubPages = []string{
	"xwdt/zx",
	"xwdt/dw",
	"xwdt/zh",
	"tzgs",
	"xzzq/xzzq",
	"dwhz/hzqk",
	"dwhz/zdwj",
	"jxjy/xxgs",
	"jxjy/zdwj",
	"ddbf/rwfc",
	"zbjs/djyd",
}

var (
	hzcArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.(?:html?|shtml)$`)
	hzcDateTimeRE    = regexp.MustCompile(`20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?\s*(?:[Tt]|\s)+\s*[0-9]{1,2}\s*[:：]\s*[0-9]{2}(?:\s*[:：]\s*[0-9]{2})?`)
	hzcDateRE        = regexp.MustCompile(`(20[0-9]{2})\s*(?:年|[-/.])\s*([0-9]{1,2})\s*(?:月|[-/.])\s*([0-9]{1,2})\s*(?:日|号)?`)
	hzcAttachmentRE  = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
)

func init() {
	if site := AllSites["hzc"]; site != nil {
		site.BaseURL = hzcCanonicalBaseURL
	}
	registerAdapter("hzc", SiteAdapter{
		SubPages:     hzcSubPages,
		ParseList:    parseAdapter_hzcList,
		ParseArticle: parseAdapter_hzcArticle,
		PageURL:      hzcPageURL,
		TotalPages:   hzcTotalPages,
	})
}

// hzcPageURL maps logical pages (1 = newest) to hzc's reverse-numbered
// generated files.  For a 38-page list, logical page 2 is /37.htm and the
// oldest logical page is /1.htm.
func hzcPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		page = 1
	}
	if slug == "" || slug == "index" || slug == "home" {
		return baseURL + "/index" + extension
	}
	if page <= 1 || totalPages <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		filePage = 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func hzcKnownFeed(slug string) bool {
	for _, known := range hzcSubPages {
		if known == strings.Trim(strings.TrimSpace(slug), "/") {
			return true
		}
	}
	return false
}

func hzcTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" || slug == "home" {
		return 1, nil
	}
	if !hzcKnownFeed(slug) {
		return 0, fmt.Errorf("hzc: %q is not an article-list route", slug)
	}
	site := AllSites["hzc"]
	if site == nil {
		return 0, fmt.Errorf("hzc: site registration is missing")
	}
	body, err := fetch(hzcPageURL(site.BaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("hzc: read first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("hzc: parse first page for %s: %w", slug, err)
	}
	if err := hzcRejectErrorPage(doc, "list"); err != nil {
		return 0, err
	}
	if hzcPageType(doc) != "2" || doc.Find(".conlistComm").Length() == 0 {
		return 0, fmt.Errorf("hzc: %s is not a list page", slug)
	}
	return hzcPaginationTotal(doc), nil
}

func parseAdapter_hzcList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("hzc: parse list HTML: %w", err)
	}
	if err := hzcRejectErrorPage(doc, "list"); err != nil {
		return nil, err
	}
	if hzcIsHomepage(doc) {
		items := hzcHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("hzc: homepage article modules contained no local articles")
		}
		return items, nil
	}
	if hzcPageType(doc) != "2" {
		return nil, fmt.Errorf("hzc: response is not a list page")
	}
	list := doc.Find(".conlistComm").First()
	if list.Length() == 0 {
		return nil, fmt.Errorf("hzc: response is a static page, not an article list")
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	defaultType := hzcCurrentType(doc)
	list.Find("dl").Each(func(_ int, row *goquery.Selection) {
		hzcAppendRow(&items, seen, row, defaultType, baseURL)
	})
	// A real .conlistComm without rows is a legal empty list.  Do not turn it
	// into an error or accidentally scan unrelated navigation links.
	return items, nil
}

func hzcHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	foundModule := false
	doc.Find(".box").Each(func(_ int, box *goquery.Selection) {
		title := strings.Join(strings.Fields(box.Find(".box-header .box-title, .box-header h2, .box-header h3").First().Text()), " ")
		typ := ""
		switch {
		case strings.Contains(title, "新闻动态"):
			typ = "新闻动态"
		case strings.Contains(title, "通知公告"):
			typ = "通知公告"
		case strings.Contains(title, "下载专区"):
			typ = "下载专区"
		}
		if typ == "" {
			return
		}
		foundModule = true
		box.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			href := anchor.AttrOr("href", "")
			row := anchor.Closest("li")
			if row.Length() == 0 {
				row = anchor.Parent()
			}
			dateText := row.Text()
			if _, publishedAt := hzcDateValues(dateText); publishedAt == "" {
				dateText = anchor.Parent().Text()
			}
			titleText := strings.TrimSpace(anchor.AttrOr("title", ""))
			if titleText == "" {
				titleText = strings.TrimSpace(anchor.Text())
			}
			hzcAppendItem(&items, seen, href, titleText, dateText, typ, baseURL)
		})
	})
	if !foundModule {
		return nil
	}
	return items
}

func hzcAppendRow(items *[]model.NewsItem, seen map[string]bool, row *goquery.Selection, defaultType, baseURL string) {
	anchor := row.Find("dt > a[href*='info/'], dt a[href*='info/']").First()
	if anchor.Length() == 0 {
		return
	}
	typ := hzcRowType(row)
	if typ == "" {
		typ = defaultType
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	hzcAppendItem(items, seen, anchor.AttrOr("href", ""), title, row.Find("dd").Text(), typ, baseURL)
}

func hzcAppendItem(items *[]model.NewsItem, seen map[string]bool, href, title, dateText, typ, baseURL string) {
	fullURL, ok := hzcArticleURL(baseURL, href)
	if !ok || seen[fullURL] {
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		// The caller can only reach this branch for a row whose title attribute
		// is absent; it is filled by the caller's anchor text when applicable.
		return
	}
	title = cleanTitle(title)
	if !hzcValidTitle(title) {
		return
	}
	date, publishedAt := hzcDateValues(dateText)
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        strings.TrimSpace(typ),
		ParsedAt:    time.Now(),
	})
}

// hzcValidTitle is deliberately narrower than the shared navigation-title
// heuristic.  The URL is already constrained to an /info/ article and this
// site has legitimate notices whose titles end in a Chinese closing quote
// (for example a title ending in “》”), which the generic heuristic treats as
// a section heading.
func hzcValidTitle(title string) bool {
	if len([]rune(strings.TrimSpace(title))) < 2 {
		return false
	}
	switch strings.TrimSpace(title) {
	case "首页", "更多", "更多>>", "通知公告", "新闻动态", "下载专区":
		return false
	default:
		return true
	}
}

func hzcRowType(row *goquery.Selection) string {
	text := strings.TrimSpace(row.Find("dt > span a, dt span a").First().Text())
	text = strings.TrimSpace(strings.Trim(text, "[]"))
	return text
}

func hzcCurrentType(doc *goquery.Document) string {
	if current := strings.Join(strings.Fields(doc.Find(".current h3").First().Text()), " "); current != "" {
		return current
	}
	if current := strings.TrimSpace(doc.Find(".menu .submenu_cur a").First().AttrOr("title", "")); current != "" {
		return current
	}
	return strings.Join(strings.Fields(doc.Find(".menu h3").First().Text()), " ")
}

func hzcDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "", ""
	}
	if match := hzcDateTimeRE.FindString(value); match != "" {
		publishedAt := normalizePublishedAt(match)
		if len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	if match := hzcDateRE.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func hzcPaginationTotal(doc *goquery.Document) int {
	total := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		page, err := strconv.Atoi(strings.TrimSpace(sel.Text()))
		if err == nil && page > total {
			total = page
		}
	})
	if total < 1 {
		return 1
	}
	return total
}

func hzcArticleURL(baseURL, href string) (string, bool) {
	if strings.TrimSpace(href) == "" {
		return "", false
	}
	resolved := resolveURL(baseURL, href)
	u, err := url.Parse(resolved)
	if err != nil || u.Host == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Host, base.Host) {
		return "", false
	}
	if !hzcArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	return u.String(), true
}

func hzcIsHomepage(doc *goquery.Document) bool {
	return hzcPageType(doc) == "1" && doc.Find(".info-panel, #carousel-example-generic").Length() > 0
}

func hzcPageType(doc *goquery.Document) string {
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).First().AttrOr("content", ""))
}

func hzcRejectErrorPage(doc *goquery.Document, kind string) error {
	if doc == nil {
		return fmt.Errorf("hzc: nil %s response", kind)
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	text := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
	title := strings.ToLower(strings.TrimSpace(doc.Find("title").First().Text()))
	for _, marker := range []string{"access denied", "cloudflare", "just a moment", "请输入验证码下载附件", "应用维护中", "安全验证"} {
		if strings.Contains(text, marker) || strings.Contains(title, marker) {
			return fmt.Errorf("hzc: %s response is an error/captcha page", kind)
		}
	}
	return nil
}

func parseAdapter_hzcArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("hzc: parse article HTML: %w", err)
	}
	if err := hzcRejectErrorPage(doc, "article"); err != nil {
		return nil, err
	}
	if hzcPageType(doc) != "3" {
		return nil, fmt.Errorf("hzc: response is not an article page")
	}
	articleShell := doc.Find(".article").First()
	if articleShell.Length() == 0 {
		return nil, fmt.Errorf("hzc: article shell is missing")
	}
	titleSel := articleShell.ChildrenFiltered("h3").First()
	if titleSel.Length() == 0 {
		titleSel = articleShell.Find("h3").First()
	}
	if titleSel.Length() == 0 || strings.TrimSpace(titleSel.Text()) == "" {
		return nil, fmt.Errorf("hzc: article title is missing")
	}
	if articleShell.Find(".v_news_content").First().Length() == 0 {
		return nil, fmt.Errorf("hzc: article content shell is missing")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, fmt.Errorf("hzc: parse article: %w", err)
	}
	article.Title = strings.TrimSpace(titleSel.Text())
	if date, publishedAt := hzcDateValues(articleShell.Find(".wz_sx").First().Text()); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	article.Type = hzcCurrentType(doc)
	hzcAppendArticleAttachments(article, articleShell, baseURL)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("hzc: article has no body or resource")
	}
	article.RawHTML = string(raw)
	return article, nil
}

func hzcAppendArticleAttachments(article *model.Article, shell *goquery.Selection, baseURL string) {
	if article == nil || shell == nil || shell.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	shell.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" {
			return
		}
		lowerHref := strings.ToLower(href)
		isDownloadEndpoint := strings.Contains(lowerHref, "download.jsp")
		isFile := hzcAttachmentRE.MatchString(lowerHref)
		if !isDownloadEndpoint && !isFile {
			return
		}
		linkText := strings.Join(strings.Fields(anchor.Text()), " ")
		parentText := strings.Join(strings.Fields(anchor.Parent().Text()), " ")
		if isDownloadEndpoint && !strings.Contains(linkText, "附件") && !strings.Contains(parentText, "附件") {
			return
		}
		resolved := resolveURL(baseURL, href)
		if seen[resolved] {
			return
		}
		name := strings.TrimSpace(anchor.AttrOr("download", ""))
		if name == "" {
			name = linkText
		}
		name = strings.TrimSpace(strings.Trim(name, "【】[]"))
		if name == "" {
			name = resolved
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}
