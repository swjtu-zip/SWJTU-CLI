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

const fadCanonicalBaseURL = "https://fad.swjtu.edu.cn"

// fadSubPages contains the actual article-list entry points. The parent pages
// in the navigation are landing/static pages; the two zero-item pages are
// retained because they are valid list shells and may become populated later.
var fadSubPages = []string{
	"xwsd/tzgg",
	"xwsd/xwsd",
	"xwsd/xxgs",
	"hwxx/gjgpxm",
	"hwxx/sxwxm",
	"hwxx/dqjlxm",
	"gjxz/yzxmsb",
	"gjxz/wjjsjlb",
	"gjxz/swrytx",
	"djyd/hdxw",
}

var (
	fadArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	fadDateRE        = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?`)
	fadYearMonthRE   = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})`)
	fadYearRE        = regexp.MustCompile(`^20\d{2}$`)
	fadMonthDayRE    = regexp.MustCompile(`(\d{1,2})\s*[-/.月](\d{1,2})`)
	fadTotalCountRE  = regexp.MustCompile(`共\s*(\d+)\s*条`)
	fadMetaLabelRE   = regexp.MustCompile(`(信息来源|来源|作者|发布日期|发布时间|日期|编辑|摄影)\s*[:：]?`)
	fadAttachmentRE  = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv)(?:$|[?#])`)
)

var fadListTypes = map[string]bool{
	"新闻动态":    true,
	"新闻速递":    true,
	"通知公告":    true,
	"信息公示":    true,
	"国家公派项目":  true,
	"双学位项目":   true,
	"短期交流项目":  true,
	"引智项目申报":  true,
	"外籍教师俱乐部": true,
	"涉外荣誉头衔":  true,
	"活动新闻":    true,
}

// fad.swjtu.edu.cn is currently canonical over HTTPS. Its article-bearing
// pages use reverse-numbered generated files, while sy.htm itself is only a
// service-card shell; xwsd.htm is the article-bearing landing page linked by
// that shell.
func init() {
	if site := AllSites["fad"]; site != nil {
		site.BaseURL = fadCanonicalBaseURL
	}
	registerAdapter("fad", SiteAdapter{
		SubPages:     fadSubPages,
		ParseList:    parseAdapter_fadList,
		ParseArticle: parseAdapter_fadArticle,
		PageURL:      fadPageURL,
		TotalPages:   fadTotalPages,
	})
}

// fadPageURL maps the logical page number (1 = newest) to the generated file
// name used by the site. For example, a 78-page xwsd/xwsd feed exposes logical
// page 2 at xwsd/xwsd/77.htm and the oldest page at xwsd/xwsd/1.htm.
func fadPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}

	if slug == "index" || slug == "home" || slug == "xwsd" {
		return baseURL + "/xwsd.htm"
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

func fadTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "index" || slug == "home" || slug == "xwsd" {
		return 1, nil
	}
	if !fadKnownFeed(slug) {
		return 0, fmt.Errorf("fad: %q is not an article-list route", slug)
	}

	site := AllSites["fad"]
	if site == nil {
		return 0, fmt.Errorf("fad: site registration is missing")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(fadPageURL(baseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if err := fadResponseError(body); err != nil {
			lastErr = err
			continue
		}
		if total := fadTotalPagesFromHTML(body); total > 0 {
			return total, nil
		}
		lastErr = fmt.Errorf("fad: %s has no recognized list pagination", slug)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("fad: unable to read page count for %s", slug)
	}
	return 0, lastErr
}

// fadTotalPagesFromHTML reads the displayed logical page labels, never the
// reverse-numbered hrefs. The last href is 1.htm on every multi-page feed.
func fadTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0
	}
	if fadHomeLayout(doc) {
		return 1
	}
	if !fadListTypeAllowed(fadListType(doc)) {
		return 0
	}

	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		page, err := strconv.Atoi(strings.TrimSpace(sel.Text()))
		if err == nil && page > maxPage {
			maxPage = page
		}
	})
	if maxPage > 0 {
		return maxPage
	}
	if doc.Find(".p_t, .p_pages").Length() > 0 {
		return 1
	}
	return 0
}

func fadKnownFeed(slug string) bool {
	for _, known := range fadSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func parseAdapter_fadList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if err := fadResponseError(raw); err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("fad: parse list HTML: %w", err)
	}

	if fadHomeLayout(doc) {
		items := fadParseHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("fad: article landing page contains no local article entries")
		}
		return items, nil
	}

	list := doc.Find(".ny-right .text-list.tz-list").First()
	if list.Length() == 0 {
		return nil, fmt.Errorf("fad: page is not an article list (static/resource/redirect page)")
	}
	listType := fadListType(doc)
	if !fadListTypeAllowed(listType) {
		return nil, fmt.Errorf("fad: unsupported list surface %q (static/resource page)", listType)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	rows := list.ChildrenFiltered("ul").First().ChildrenFiltered("li")
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		fadAppendListItem(&items, seen, row, anchor, listType, baseURL)
	})
	if len(items) > 0 {
		return items, nil
	}
	if fadExplicitEmptyList(doc) {
		return items, nil
	}
	return nil, fmt.Errorf("fad: list %q has no valid local article rows", listType)
}

func fadHomeLayout(doc *goquery.Document) bool {
	return doc != nil && doc.Find(".xwtz.xzxwtz .xzxwtz-t").Length() > 0 && doc.Find(".xwtz.xzxwtz .xzxwtz-d .qh-box").Length() > 0
}

func fadParseHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)

	news := doc.Find(".xzxwtz-t").First()
	newsType := fadCleanText(news.Find(".title h3 p").First().Text())
	if newsType == "" {
		newsType = fadCleanText(news.Find(".title h3").First().Text())
	}
	fadAppendHomeRows(&items, seen, news.Find("ul.flex > li"), newsType, baseURL)

	doc.Find(".xzxwtz-d .qh-box").Each(func(_ int, box *goquery.Selection) {
		typ := fadCleanText(box.Find(".qh-tit li").First().Text())
		fadAppendHomeRows(&items, seen, box.Find(".qh-bd > ul > li"), typ, baseURL)
	})
	return items
}

func fadAppendHomeRows(items *[]model.NewsItem, seen map[string]bool, rows *goquery.Selection, typ, baseURL string) {
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		fadAppendListItem(items, seen, row, anchor, typ, baseURL)
	})
}

func fadAppendListItem(items *[]model.NewsItem, seen map[string]bool, row, anchor *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	fullURL, ok := fadArticleURL(baseURL, anchor.AttrOr("href", ""))
	if !ok || seen[fullURL] {
		return
	}

	title := fadCleanText(anchor.AttrOr("title", ""))
	if title == "" {
		title = fadCleanText(row.Find("h3, h4").First().Text())
	}
	if title == "" {
		title = fadCleanText(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 160 {
		return
	}

	date, publishedAt := fadDateFromScope(row)
	item := model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        fadCleanText(typ),
		ParsedAt:    time.Now(),
	}
	seen[fullURL] = true
	*items = append(*items, item)
}

func fadArticleURL(baseURL, href string) (string, bool) {
	href = htmlstd.UnescapeString(strings.TrimSpace(href))
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !fadArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func fadListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if active := doc.Find(".leftNav li.active").First(); active.Length() > 0 {
		// Article pages nest the selected child under an active parent such as
		// 新闻速递; prefer that child over the parent label.
		if nested := active.ChildrenFiltered("ul").First().ChildrenFiltered("li").Filter(".active, .on").First(); nested.Length() > 0 {
			if value := fadCleanText(nested.ChildrenFiltered("a").First().Text()); value != "" {
				return value
			}
		}
		if value := fadCleanText(active.ChildrenFiltered("a").First().Text()); value != "" {
			return value
		}
	}
	if value := fadCleanText(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).First().AttrOr("content", "")); value != "" {
		return value
	}
	return fadCleanText(doc.Find(".ny-right .position h3").First().Text())
}

func fadListTypeAllowed(typ string) bool {
	return fadListTypes[fadCleanText(typ)]
}

func fadDateFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}
	if dateScope := scope.Find(".date").First(); dateScope.Length() > 0 {
		day := fadCleanText(dateScope.Find("p, .day").First().Text())
		yearMonth := fadCleanText(dateScope.Find("span, .month").First().Text())
		if day != "" && yearMonth != "" {
			if yearMatch := fadYearMonthRE.FindStringSubmatch(yearMonth); yearMatch != nil {
				if dayMatch := fadMonthDayRE.FindStringSubmatch(day); dayMatch != nil {
					if date := normalizeDateParts(yearMatch[1], yearMatch[2], dayMatch[2]); date != "" {
						return date, ""
					}
				}
				if date := normalizeDateParts(yearMatch[1], yearMatch[2], day); date != "" {
					return date, ""
				}
			}
			if fadYearRE.MatchString(yearMonth) {
				if dayMatch := fadMonthDayRE.FindStringSubmatch(day); dayMatch != nil {
					if date := normalizeDateParts(yearMonth, dayMatch[1], dayMatch[2]); date != "" {
						return date, ""
					}
				}
			}
		}
		if date, published := fadDateValues(fadCleanText(dateScope.Text())); date != "" {
			return date, published
		}
	}

	for _, selector := range []string{"time", ".jdt-tx > span", ".date", ".time", "[class*='date']"} {
		if candidate := scope.Find(selector).First(); candidate.Length() > 0 {
			if date, published := fadDateValues(fadCleanText(candidate.Text())); date != "" {
				return date, published
			}
		}
	}
	return fadDateValues(fadCleanText(scope.Text()))
}

func fadDateValues(raw string) (string, string) {
	raw = fadCleanText(raw)
	if raw == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(raw); normalized != "" {
		if publishedAtHasClock(normalized) {
			return normalized[:10], normalized
		}
		return normalized[:10], ""
	}
	if match := fadDateRE.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func fadExplicitEmptyList(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	countText := fadCleanText(doc.Find(".p_t").First().Text())
	match := fadTotalCountRE.FindStringSubmatch(countText)
	return len(match) > 1 && strings.TrimSpace(match[1]) == "0"
}

func parseAdapter_fadArticle(raw []byte, baseURL string) (*model.Article, error) {
	if err := fadResponseError(raw); err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("fad: parse article HTML: %w", err)
	}
	if pageType := fadCleanText(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", "")); pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("fad: page is not an article (pageType %q)", pageType)
	}

	title := fadArticleTitle(doc)
	content := fadArticleContent(doc)
	if title == "" {
		return nil, fmt.Errorf("fad: article has no title")
	}
	if content.Length() == 0 {
		return nil, fmt.Errorf("fad: page is not an article (no content container)")
	}
	if fadCleanText(content.Text()) == "" && content.Find("img, table, a").Length() == 0 {
		return nil, fmt.Errorf("fad: article has empty content")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = title
	fadApplyArticleMetadata(article, doc)
	if typ := fadArticleType(doc); typ != "" {
		article.Type = typ
	}
	fadAppendAttachments(article, doc, baseURL)
	content.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		image.SetAttr("src", cleanURL(resolveURL(baseURL, image.AttrOr("src", ""))))
	})
	if html, htmlErr := content.Html(); htmlErr == nil {
		article.ContentHTML = html
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func fadArticleTitle(doc *goquery.Document) string {
	for _, selector := range []string{
		".art-main .art-tit h1, .art-main .art-tit h2, .art-main .art-tit h3",
		".art-tit h1, .art-tit h2, .art-tit h3",
		".article-title, .detail-title, .news-title",
	} {
		if title := cleanTitle(fadCleanText(doc.Find(selector).First().Text())); title != "" {
			return title
		}
	}
	return cleanTitle(fadCleanText(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).First().AttrOr("content", "")))
}

func fadArticleContent(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		".art-main .v_news_content",
		"[id^='vsb_content'] .v_news_content",
		".v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return doc.Find("__fad_no_content__")
}

func fadArticleType(doc *goquery.Document) string {
	typ := fadListType(doc)
	if fadListTypeAllowed(typ) {
		return typ
	}
	return ""
}

func fadApplyArticleMetadata(article *model.Article, doc *goquery.Document) {
	meta := doc.Find(".art-main .art-tit p, .art-tit p").First()
	if meta.Length() == 0 {
		return
	}
	fields := fadMetaFields(fadCleanText(flattenedText(meta)))
	if _, ok := fields["信息来源"]; ok {
		article.Source = fields["信息来源"]
	} else if _, ok := fields["来源"]; ok {
		article.Source = fields["来源"]
	}
	if _, ok := fields["作者"]; ok {
		article.Author = fields["作者"]
	}
	dateValue := fields["发布日期"]
	if dateValue == "" {
		dateValue = fields["发布时间"]
	}
	if dateValue == "" {
		dateValue = fields["日期"]
	}
	if date, publishedAt := fadDateValues(dateValue); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
}

func fadMetaFields(text string) map[string]string {
	fields := make(map[string]string)
	matches := fadMetaLabelRE.FindAllStringIndex(text, -1)
	for i, match := range matches {
		if len(match) < 2 {
			continue
		}
		label := strings.TrimSpace(strings.TrimRight(text[match[0]:match[1]], "：: "))
		start, end := match[1], len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		fields[label] = strings.TrimSpace(strings.TrimLeft(text[start:end], "：: "))
	}
	return fields
}

func fadAppendAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	scope := doc.Find(".art-main").First()
	if scope.Length() == 0 {
		scope = fadArticleContent(doc)
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" || !fadAttachmentLink(anchor, href) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, htmlstd.UnescapeString(href)))
		name := fadCleanText(anchor.AttrOr("download", ""))
		if name == "" {
			name = fadCleanText(anchor.Text())
		}
		if name == "" {
			name = fadCleanText(anchor.AttrOr("title", ""))
		}
		anchor.SetAttr("href", resolved)
		if name == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func fadAttachmentLink(anchor *goquery.Selection, href string) bool {
	lowerHref := strings.ToLower(href)
	if strings.Contains(lowerHref, "downloadattachurl") {
		return true
	}
	if strings.Contains(lowerHref, "download.jsp") {
		item := anchor.Closest("li")
		return item.Length() > 0 && strings.Contains(fadCleanText(item.Text()), "附件")
	}
	if anchor.AttrOr("download", "") != "" || fadAttachmentRE.MatchString(lowerHref) {
		return true
	}
	return false
}

func fadCleanText(value string) string {
	return strings.Join(strings.Fields(htmlstd.UnescapeString(strings.TrimSpace(value))), " ")
}

func fadResponseError(raw []byte) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("fad: parse response HTML: %w", err)
	}
	title := strings.ToLower(fadCleanText(doc.Find("title").First().Text()))
	for _, marker := range []string{"captcha", "验证码", "登录页", "login", "系统提示", "没有访问", "403", "404", "500", "error", "错误"} {
		if strings.Contains(title, marker) {
			return fadClassifiedPageError(title, marker)
		}
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(fadCleanText(clone.Text()))
	switch {
	case strings.Contains(visible, "验证码") || strings.Contains(visible, "captcha") || strings.Contains(visible, "安全威胁") || strings.Contains(visible, "请求已被阻断"):
		return fmt.Errorf("fad: captcha/verification page")
	case strings.Contains(visible, "没有访问当前栏目的权限") || strings.Contains(visible, "您没有访问"):
		return fmt.Errorf("fad: permission/error page")
	case strings.Contains(visible, "请先登录") || strings.Contains(visible, "请登录后"):
		return fmt.Errorf("fad: login page")
	}
	return nil
}

func fadClassifiedPageError(title, marker string) error {
	switch marker {
	case "captcha", "验证码":
		return fmt.Errorf("fad: captcha/verification page (%s)", title)
	case "登录页", "login":
		return fmt.Errorf("fad: login page (%s)", title)
	default:
		return fmt.Errorf("fad: error page (%s)", title)
	}
}
