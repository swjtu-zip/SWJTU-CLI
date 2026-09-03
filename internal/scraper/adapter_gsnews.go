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

// gsnews.swjtu.edu.cn uses the 博达 CMS, but its navigation leaves are not
// interchangeable with the other sites.  In particular, generated list
// files are numbered from oldest to newest while the public page presents
// the newest page as page 1.  Keep the route inventory, pagination, list
// filtering, and article enrichment in this adapter.
func init() {
	registerAdapter("gsnews", SiteAdapter{
		SubPages:     gsnewsSubPages,
		ParseList:    parseAdapter_gsnewsList,
		ParseArticle: parseAdapter_gsnewsArticle,
		PageURL:      gsnewsPageURL,
		TotalPages:   gsnewsTotalPages,
	})
}

// The first feeds are homepage/news modules.  The remaining entries are the
// actual navigation leaves that expose an article list, including the
// secondary links reachable from article sidebars and hidden notice tabs.
// Static profile pages, external招生/研工 links, service-card pages, and the
// two download-only pages are deliberately not feeds.  Empty article-list
// shells remain registered when they are real navigation leaves, so an empty
// result is distinguishable from a fetch/parser failure.
var gsnewsSubPages = []string{
	"xwdtnew/rdxw",
	"xwdtnew/xsdt",
	"xwdtnew/mtbd",
	"tzggnew/qb",
	"tzggnew/pygz",
	"tzggnew/xwgz",
	"tzggnew/sxsj",
	"tzggnew/zhxx",
	"gzlc/pygz",
	"gzzdnew/yzgz",
	"gzzdnew/pygz",
	"gzzdnew/xwgz",
	"gzzdnew/sxsj",
	"gzzdnew/gjjl",
	"gzzdnew/zhsw",
	"pygznew/pyfa",
	"pygznew/xjxl",
	"pygznew/jxgl",
	"pygznew/kcsz",
	"pygznew/sxsj",
	"pygznew/gjjl",
	"pygznew/jxgg",
	"pygznew/jcjs",
	"pygznew/zldd",
	"pygznew/jsfz",
	"pygznew/qt",
	"xwgznew/xwsy",
	"xwgznew/xwsqd",
	"xwgznew/dsdw",
	"xwgznew/pycy",
	"xwgznew/tdxl",
	"xzzx/pygz",
	"xzzx/xwgz",
	"xzzx/sxsj",
	"xzzx/gjjl",
	"xzzx/lxsxg",
	"zhxxnew/xkjs",
	"zhxxnew/cydjt",
	"zsgz/sszs",
	"zsgz/bszs",
	"zsgz/zsxc",
}

// Audited live totals.  The HTML pagination is authoritative at runtime;
// these values only keep a reverse-page URL usable when a page-count request
// is temporarily unavailable (and make an out-of-range request a miss rather
// than silently repeating page 1).
var gsnewsPageTotals = map[string]int{
	"xwdtnew/rdxw":  9,
	"xwdtnew/xsdt":  2,
	"xwdtnew/mtbd":  1,
	"tzggnew/qb":    60,
	"tzggnew/pygz":  33,
	"tzggnew/xwgz":  5,
	"tzggnew/sxsj":  3,
	"tzggnew/zhxx":  19,
	"xzzx/pygz":     2,
	"xzzx/xwgz":     1,
	"xzzx/sxsj":     1,
	"xzzx/gjjl":     1,
	"xzzx/lxsxg":    1,
	"zsgz/sszs":     12,
	"zsgz/bszs":     5,
	"gzlc/pygz":     1,
	"gzzdnew/yzgz":  1,
	"gzzdnew/pygz":  1,
	"gzzdnew/xwgz":  1,
	"gzzdnew/sxsj":  1,
	"gzzdnew/gjjl":  1,
	"gzzdnew/zhsw":  1,
	"pygznew/pyfa":  1,
	"pygznew/xjxl":  1,
	"pygznew/jxgl":  1,
	"pygznew/kcsz":  1,
	"pygznew/sxsj":  1,
	"pygznew/gjjl":  1,
	"pygznew/jxgg":  1,
	"pygznew/jcjs":  1,
	"pygznew/zldd":  1,
	"pygznew/jsfz":  1,
	"pygznew/qt":    1,
	"xwgznew/xwsy":  1,
	"xwgznew/xwsqd": 1,
	"xwgznew/dsdw":  1,
	"xwgznew/pycy":  1,
	"xwgznew/tdxl":  1,
	"zhxxnew/xkjs":  1,
	"zhxxnew/cydjt": 1,
	"zsgz/zsxc":     1,
}

var (
	gsnewsArticlePathRE  = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	gsnewsDateRE         = regexp.MustCompile(`20[0-9]{2}\s*(?:[-/.年]\s*[0-9]{1,2}\s*[-/.月]\s*[0-9]{1,2}\s*日?|年\s*[0-9]{1,2}\s*月\s*[0-9]{1,2}\s*日?)(?:\s*[Tt ]\s*[0-9]{1,2}:[0-9]{2}(?::[0-9]{2})?)?`)
	gsnewsDayYearMonthRE = regexp.MustCompile(`([0-9]{1,2})\s*(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`)
	gsnewsPageNumberRE   = regexp.MustCompile(`^[0-9]+$`)
	gsnewsAttachmentRE   = regexp.MustCompile(`(?i)(?:download\.jsp|\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv))(?:$|[?#])`)
)

// gsnewsPageURL maps logical pages (1 = newest) to generated files.  The
// unsuffixed URL is always logical page 1; logical page N is stored at
// totalPages-N+1.htm.
func gsnewsPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages <= 1 {
		totalPages = gsnewsPageTotals[slug]
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	physical := totalPages - page + 1
	if physical < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

func gsnewsTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" || slug == "home" {
		return 1, nil
	}
	if !gsnewsKnownFeed(slug) {
		return 0, fmt.Errorf("gsnews: %q is not an audited article-list route", slug)
	}

	baseURL := AllSites["gsnews"].BaseURL
	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(gsnewsPageURL(baseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if err := gsnewsResponseError(body); err != nil {
			lastErr = err
			continue
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			lastErr = err
			continue
		}
		if doc.Find("main.news-list-page-main ul.news-list").Length() == 0 {
			lastErr = fmt.Errorf("gsnews: %s has no recognized news list", slug)
			continue
		}
		if total := gsnewsPaginationTotal(doc); total > 0 {
			return total, nil
		}
		return 1, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("gsnews: unable to read page count for %s", slug)
	}
	return 0, lastErr
}

func gsnewsKnownFeed(slug string) bool {
	for _, known := range gsnewsSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func gsnewsPaginationTotal(doc *goquery.Document) int {
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		value := strings.TrimSpace(sel.Text())
		if !gsnewsPageNumberRE.MatchString(value) {
			return
		}
		page, err := strconv.Atoi(value)
		if err == nil && page > maxPage {
			maxPage = page
		}
	})
	return maxPage
}

func parseAdapter_gsnewsList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("gsnews: parse list HTML: %w", err)
	}
	if err := gsnewsResponseError(raw); err != nil {
		return nil, err
	}

	if gsnewsIsHome(doc) {
		items := parseGsnewsHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("gsnews: homepage has no recognized article modules")
		}
		return items, nil
	}

	list := doc.Find("main.news-list-page-main ul.news-list").First()
	if list.Length() == 0 {
		return nil, fmt.Errorf("gsnews: page has no recognized article list")
	}
	typ := gsnewsPageType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	list.Find("li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		gsnewsAppendItem(&items, seen, anchor, row, baseURL, typ)
	})
	// An empty <ul class="news-list"> with a valid page shell is a legitimate
	// empty tab, not a parser failure. Direct resource links in 下载中心 are
	// intentionally ignored because only /info/... URLs are article details.
	return items, nil
}

func gsnewsIsHome(doc *goquery.Document) bool {
	return doc.Find("main.index-main").Length() > 0 ||
		(doc.Find("#content-1").Length() > 0 && doc.Find(".msg-tabNav").Length() > 0)
}

func parseGsnewsHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	// The news module contains both a slider and a three-row list.  The list
	// is the stable source of dates; the slider is deliberately not parsed.
	doc.Find("main.index-main .news-list li").Each(func(_ int, row *goquery.Selection) {
		gsnewsAppendItem(&items, seen, row.Find("a[href]").First(), row, baseURL, "热点新闻")
	})

	noticeTypes := []string{"通知公告", "培养工作", "学位工作", "实习实践", "招生工作", "综合信息"}
	doc.Find("main.index-main .tabcons").Each(func(index int, panel *goquery.Selection) {
		typ := "通知公告"
		if index < len(noticeTypes) {
			typ = noticeTypes[index]
		}
		panel.Find("li").Each(func(_ int, row *goquery.Selection) {
			gsnewsAppendItem(&items, seen, row.Find("a[href]").First(), row, baseURL, typ)
		})
	})
	return items
}

func gsnewsAppendItem(items *[]model.NewsItem, seen map[string]int, anchor, scope *goquery.Selection, baseURL, typ string) {
	if anchor == nil || anchor.Length() == 0 || scope == nil || scope.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if !gsnewsArticleURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		for _, selector := range []string{".content", ".right-content", ".title", "p", "h1", "h2", "h3"} {
			if value := strings.TrimSpace(anchor.Find(selector).First().Text()); value != "" {
				title = value
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	// The URL is already constrained to the site's /info/ article namespace
	// and the anchor is inside a recognized news-list.  Do not apply the
	// shared navigation-title heuristic here: valid notices on this site can
	// end in a Chinese quote mark or exceed the generic title-length limit.
	if title == "" {
		return
	}
	date, publishedAt := gsnewsDateFromScope(scope)
	item := model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: strings.TrimSpace(typ), ParsedAt: time.Now()}
	if index, ok := seen[fullURL]; ok {
		if (*items)[index].Date == "" && item.Date != "" {
			(*items)[index].Date = item.Date
			(*items)[index].PublishedAt = item.PublishedAt
		}
		// The homepage's 全部 panel is a duplicate of the more precise tab;
		// retain the latter as the type when it is available.
		if ((*items)[index].Type == "通知公告" || (*items)[index].Type == "") && item.Type != "" && item.Type != "通知公告" {
			(*items)[index].Type = item.Type
		}
		return
	}
	seen[fullURL] = len(*items)
	*items = append(*items, item)
}

func gsnewsArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !gsnewsArticlePathRE.MatchString(parsed.Path) {
		return false
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Host == "" || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, base.Host)
}

func gsnewsPageType(doc *goquery.Document) string {
	for _, selector := range []string{
		"main.news-list-page-main section .current-title",
		"main.news-list-page-main .current-title",
		"main.news-list-page-main aside .lm-title",
	} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func gsnewsDateFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}
	raw := strings.Join(strings.Fields(scope.Find("time, .time, .date").First().Text()), " ")
	if raw == "" {
		raw = strings.Join(strings.Fields(scope.Text()), " ")
	}
	if match := gsnewsDayYearMonthRE.FindStringSubmatch(raw); match != nil {
		date := normalizeDateParts(match[2], match[3], match[1])
		return date, date
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" && len(publishedAt) >= 10 {
		return publishedAt[:10], publishedAt
	}
	if match := gsnewsDateRE.FindString(raw); match != "" {
		publishedAt := normalizePublishedAt(match)
		if publishedAt != "" && len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	return "", ""
}

func parseAdapter_gsnewsArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("gsnews: parse article HTML: %w", err)
	}
	if err := gsnewsResponseError(raw); err != nil {
		return nil, err
	}
	if doc.Find("main.news-detail-page-main .intro-part .title, main.news-detail-page-main .v_news_content").Length() == 0 {
		return nil, fmt.Errorf("gsnews: page has no recognized article content")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	if title := strings.Join(strings.Fields(doc.Find("main.news-detail-page-main .intro-part .title").First().Text()), " "); title != "" {
		article.Title = title
	}
	if typ := gsnewsPageTypeFromArticle(doc); typ != "" {
		article.Type = typ
	}
	if rawDate := strings.Join(strings.Fields(doc.Find("main.news-detail-page-main .little-tips .fbsj").First().Text()), " "); rawDate != "" {
		if date, publishedAt := gsnewsDateFromText(rawDate); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		}
	}
	appendGsnewsAttachments(article, doc, baseURL)
	resolveGsnewsContentHTML(article, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func gsnewsPageTypeFromArticle(doc *goquery.Document) string {
	for _, selector := range []string{
		"main.news-detail-page-main section .current-title",
		"main.news-detail-page-main .current-title",
		"main.news-detail-page-main aside li.active a",
	} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func gsnewsDateFromText(raw string) (string, string) {
	raw = strings.Join(strings.Fields(raw), " ")
	for _, prefix := range []string{"发布时间：", "发布时间:", "日期：", "日期:"} {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, prefix))
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" && len(publishedAt) >= 10 {
		return publishedAt[:10], publishedAt
	}
	if match := gsnewsDateRE.FindString(raw); match != "" {
		publishedAt := normalizePublishedAt(match)
		if publishedAt != "" && len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	return "", ""
}

func appendGsnewsAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	// Some pages leave .provenance empty and render the actual attachment UL
	// directly beside it inside the article section.  Scan that whole section,
	// then keep only download/file URLs so page-turn and external links do not
	// become attachments.
	doc.Find("main.news-detail-page-main section a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" || !gsnewsAttachmentRE.MatchString(strings.ToLower(href)) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if seen[resolved] {
			return
		}
		name := strings.TrimSpace(anchor.Text())
		if name == "" {
			name = strings.TrimSpace(anchor.AttrOr("download", ""))
		}
		if name == "" {
			name = resolved
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func resolveGsnewsContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(article.ContentHTML))
	if err != nil {
		return
	}
	doc.Find("img[src]").Each(func(_ int, sel *goquery.Selection) {
		sel.SetAttr("src", resolveURL(baseURL, strings.TrimSpace(sel.AttrOr("src", ""))))
	})
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		sel.SetAttr("href", resolveURL(baseURL, strings.TrimSpace(sel.AttrOr("href", ""))))
	})
	if html, err := doc.Find("body").Html(); err == nil && strings.TrimSpace(html) != "" {
		article.ContentHTML = html
		return
	}
	if html, err := doc.Html(); err == nil {
		article.ContentHTML = html
	}
}

func gsnewsResponseError(raw []byte) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("gsnews: invalid HTML response: %w", err)
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"404", "not found", "页面不存在", "访问错误", "access denied", "waf"} {
		if strings.Contains(title, marker) {
			return fmt.Errorf("gsnews: received error page (%s)", marker)
		}
	}
	mainText := strings.ToLower(strings.Join(strings.Fields(doc.Find("main").First().Text()), " "))
	for _, marker := range []string{"验证码", "安全验证", "access denied", "waf challenge"} {
		if strings.Contains(mainText, marker) {
			return fmt.Errorf("gsnews: received verification/error page (%s)", marker)
		}
	}
	return nil
}
