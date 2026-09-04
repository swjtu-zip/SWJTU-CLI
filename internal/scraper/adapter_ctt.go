package scraper

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const cttBaseURL = "https://ctt.swjtu.edu.cn"

// cttSubPages contains the reachable article archives exposed by the live
// navigation and by the "查看更多" links in its hidden home/landing modules.
// xzfc and mtgz are deliberately retained: they are real list endpoints even
// when the source currently has no entries. Static profile/overview pages and
// landing pages whose content is only a six-item preview are not feeds.
var cttSubPages = []string{
	"xyywlist/jyyw",
	"xyywlist/dcjy",
	"xyywlist/xzfc",
	"xyywlist/kycx",
	"xyywlist/tsdj",
	"xyywlist/jwgz",
	"xyywlist/mtgz",
	"xyywlist/wjxz",
	"xyywlist/gzzd",
	"tzgg/xytz",
	"tzgg/rsxg",
	"tzgg/tzzf",
	"tzgg/wjxz",
	"bkjxnew/jxgg",
	"yjsjxnew/tzgg",
	"yjsjxnew/wjxz",
	"kxyjNew/kytz",
	"sysglnew/tzgg",
	"shpxnew/jxjyxx",
	"xsgzNew1/txhd",
	"xsgzNew1/bksgl",
	"xsgzNew1/yjsgl",
	"xsgzNew1/xszz",
	"xsgzNew1/bjjs",
	"dqgzNew/djtz",
	"dqgzNew/djxw",
	"dqgzNew/gszq",
	"dqgzNew/ghhd",
	"ltgz_ggw_",
}

var (
	cttInfoArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	cttRootArticlePathRe = regexp.MustCompile(`(?i)^/[0-9]{4,}\.html?$`)
	cttFullDateRe        = regexp.MustCompile(`(?i)(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?`)
	cttYearMonthRe       = regexp.MustCompile(`(?i)^(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*月?$`)
	cttMonthDayRe        = regexp.MustCompile(`^([0-9]{1,2})\s*[-/.]\s*([0-9]{1,2})$`)
	cttAttachmentExtRe   = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
	cttDownloadURLRe     = regexp.MustCompile(`(?i)(?:downloadattachurl|/system/_content/download\.jsp(?:[?#]|$)|(?:^|[/_-])attachment(?:[/_.?#]|$))`)
)

// ctt.swjtu.edu.cn uses a different generated-page order from the shared
// default: the unsuffixed file is logical page 1, while logical page 2 is
// the file named total-1.htm and the oldest page is /1.htm.
func init() {
	if site := AllSites["ctt"]; site != nil {
		site.BaseURL = cttBaseURL
	}
	registerAdapter("ctt", SiteAdapter{
		SubPages:     cttSubPages,
		ParseList:    parseCttNews,
		ParseArticle: parseCttArticle,
		PageURL:      cttPageURL,
		TotalPages:   cttTotalPages,
	})
}

// cttPageURL maps the logical page number (1=newest) to CTT's generated
// files. For example, the first xytz page advertises 20 pages and links
// logical page 2 to xytz/19.htm.
func cttPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages <= 1 {
		return ""
	}

	filePage := page
	if totalPages > 1 {
		filePage = totalPages - page + 1
	}
	if filePage < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

// cttTotalPages reads the logical page labels from the first page. Looking at
// the labels, rather than the generated hrefs, is essential because the href
// for the last page is /1.htm even when the logical total is much larger.
func cttTotalPages(slug string) (int, error) {
	baseURL := cttBaseURL
	if site := AllSites["ctt"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}

	listURL := cttPageURL(baseURL, slug, 1, 0, ".htm")
	body, err := fetch(listURL)
	if err != nil {
		listURL = cttPageURL(baseURL, slug, 1, 0, ".html")
		body, err = fetch(listURL)
	}
	if err != nil {
		return 0, fmt.Errorf("ctt: fetch first page for %s: %w", slug, err)
	}
	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if parseErr != nil {
		return 0, fmt.Errorf("ctt: parse first page for %s: %w", slug, parseErr)
	}
	if reason := cttResponseReason(doc); reason != "" {
		return 0, fmt.Errorf("ctt: %s (%s)", reason, slug)
	}

	if total := cttTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	if cttListShellFromHTML(body) {
		if !cttHasListEntries(doc) {
			return 0, fmt.Errorf("ctt: recognized list is empty (%s)", slug)
		}
		return 1, nil
	}
	return 0, fmt.Errorf("ctt: %s does not expose a recognized list page", slug)
}

func cttTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0
	}

	maxPage := 0
	doc.Find(".p_pages .p_no a, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		value, err := strconv.Atoi(strings.TrimSpace(sel.Text()))
		if err == nil && value > maxPage {
			maxPage = value
		}
	})
	return maxPage
}

func cttListShellFromHTML(raw []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return false
	}
	return cttListShell(doc)
}

func cttListShell(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".main_right #curType, .main_right .position, .main_right .p_pages").Length() > 0
}

func cttHasListEntries(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".news-list, .news-ul > li, .innews > li, .rowb .list li, .notice-title-box, .notice-list-box > li").Length() > 0
}

// cttResponseReason distinguishes an unavailable response from an ordinary
// empty archive. CTT is server-rendered, so a login/WAF/SPA shell must never be
// treated as a successful zero-item page.
func cttResponseReason(doc *goquery.Document) string {
	if doc == nil {
		return "empty HTML response"
	}
	if cttHasListEntries(doc) || cttArticleContent(doc) != nil {
		return ""
	}

	title := cleanCttText(doc.Find("title").First().Text())
	if reason := cttBlockedTextReason(title); reason != "" {
		return reason
	}

	if doc.Find("#app, #root, [id='__next'], [data-reactroot]").Length() > 0 && doc.Find("script").Length() > 0 {
		return "SPA shell"
	}
	if doc.Find(`form[action*="login" i], input[type="password"], [class*="login" i], [id*="login" i]`).Length() > 0 {
		return "login response"
	}

	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	if reason := cttBlockedTextReason(cleanCttText(clone.Find("body").Text())); reason != "" {
		return reason
	}
	return ""
}

func cttBlockedTextReason(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	for _, marker := range []string{
		"验证码", "人机验证", "安全验证", "访问验证", "访问被拒绝", "拒绝访问",
		"captcha", "cloudflare", "access denied", "just a moment", "cf-chl", "challenge-platform", "waf", "403 forbidden",
	} {
		if strings.Contains(text, marker) {
			return "WAF/captcha response"
		}
	}
	if strings.Contains(text, "附件下载") {
		return "HTML attachment/captcha response"
	}
	if strings.Contains(text, "请登录") || strings.Contains(text, "用户登录") || strings.Contains(text, "统一身份认证") || strings.Contains(text, "cas/login") {
		return "login response"
	}
	return ""
}

func parseCttNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse CTT list: %w", err)
	}
	if reason := cttResponseReason(doc); reason != "" {
		return nil, fmt.Errorf("ctt: %s", reason)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	listType := cttListType(doc)

	// Category archives have one .news-list card per article. Restricting the
	// extraction to this container excludes the banner cards and page shell,
	// both of which also contain article-looking links.
	doc.Find(".news-list").Each(func(_ int, card *goquery.Selection) {
		anchor := card.Find(".news-box > a[href]").First()
		if anchor.Length() == 0 {
			anchor = card.Find("a[href]").First()
		}
		addCttListItem(&items, seen, card, anchor, baseURL, listType, false)
	})

	// Keep compatibility with the older home-page template, whose real rows
	// use root-level numeric article URLs.
	doc.Find(".news-ul > li").Each(func(_ int, row *goquery.Selection) {
		addCttListItem(&items, seen, row, row.Find(".newstxtli a[href]").First(), baseURL, "交运要闻", true)
	})

	// The current home page has a four-item latest-news block plus four hidden
	// tab panels. Parse every panel instead of stopping after the first block;
	// all four panels are present in the HTML even though only one is visible.
	doc.Find(".innews > li").Each(func(_ int, row *goquery.Selection) {
		typ := cttClosestHeading(row, ".rowa", "交运要闻")
		anchor := row.Find(".info a[href]").First()
		if anchor.Length() == 0 {
			anchor = row.Find("a.line2[href]").First()
		}
		addCttListItem(&items, seen, row, anchor, baseURL, typ, false)
	})
	doc.Find(".rowb").Each(func(_ int, panel *goquery.Selection) {
		tabs := panel.Find(".tabNav li")
		foundTabs := false
		panel.Find(".tabcons").Each(func(index int, tab *goquery.Selection) {
			foundTabs = true
			typ := ""
			if index < tabs.Length() {
				typ = cleanCttText(tabs.Eq(index).Text())
			}
			tab.Find(".list li").Each(func(_ int, row *goquery.Selection) {
				addCttListItem(&items, seen, row, row.Find("a.texto[href]").First(), baseURL, typ, false)
			})
		})
		if !foundTabs {
			typ := cleanCttText(tabs.First().Text())
			panel.Find(".list li").Each(func(_ int, row *goquery.Selection) {
				addCttListItem(&items, seen, row, row.Find("a.texto[href]").First(), baseURL, typ, false)
			})
		}
	})

	// Some top-level navigation pages are landing pages with one or more
	// hidden notice panels. They are not registered as feeds, but keeping this
	// scoped fallback makes their real preview articles parseable when a caller
	// explicitly supplies the landing URL. Banner-image links remain excluded.
	doc.Find(".notice-title-box").Each(func(_ int, block *goquery.Selection) {
		addCttListItem(&items, seen, block, block.Find("a[href]").First(), baseURL, listType, false)
	})
	doc.Find(".notice-list-box > li").Each(func(_ int, row *goquery.Selection) {
		addCttListItem(&items, seen, row, row.Find("a.notice-txt[href]").First(), baseURL, listType, false)
	})

	if len(items) == 0 {
		if cttListShell(doc) {
			return nil, fmt.Errorf("ctt: recognized list is empty")
		}
		return nil, fmt.Errorf("ctt: list contains no recognized article entries")
	}
	return items, nil
}

func addCttListItem(items *[]model.NewsItem, seen map[string]bool, block, anchor *goquery.Selection, baseURL, typ string, allowRootArticle bool) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href, ok := anchor.Attr("href")
	if !ok {
		return
	}
	fullURL, ok := cttArticleURL(baseURL, href, allowRootArticle)
	if !ok || seen[fullURL] {
		return
	}

	title := strings.TrimSpace(block.Find(".news-title").First().Text())
	if title == "" {
		title = strings.TrimSpace(anchor.AttrOr("title", ""))
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 100 {
		return
	}

	date := cttDateFromBlock(block)
	publishedAt := cttPublishedAtFromBlock(block)
	if cardType := cttTypeFromBlock(block); cardType != "" {
		typ = cardType
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        cleanCttText(typ),
		ParsedAt:    time.Now(),
	})
}

func cttArticleURL(baseURL, href string, allowRootArticle bool) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || isExternalURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
		return "", false
	}
	if cttInfoArticlePathRe.MatchString(parsed.Path) {
		return fullURL, true
	}
	if allowRootArticle && cttRootArticlePathRe.MatchString(parsed.Path) {
		return fullURL, true
	}
	return "", false
}

func cttListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".main_right #curType", ".main_left .submenu_cur", ".main_left .menu h3", ".selectbtn.active"} {
		if value := cleanCttText(doc.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	if title := cleanCttText(doc.Find("title").First().Text()); title != "" {
		return cleanCttText(stripSiteSuffix(title))
	}
	return ""
}

func cttClosestHeading(sel *goquery.Selection, ancestor, fallback string) string {
	if sel != nil && sel.Length() > 0 {
		if value := cleanCttText(sel.Closest(ancestor).Find("h3").First().Text()); value != "" {
			return value
		}
	}
	return fallback
}

func cttTypeFromBlock(block *goquery.Selection) string {
	if block == nil || block.Length() == 0 {
		return ""
	}
	content := block.Find(".news-content").First()
	if content.Length() == 0 {
		return ""
	}
	text := cleanCttText(flattenedText(content))
	marker := strings.Index(text, "分类")
	if marker < 0 {
		return ""
	}
	value := text[marker+len("分类"):]
	if !strings.ContainsAny(value, ":：") {
		return ""
	}
	value = strings.TrimSpace(strings.TrimLeft(value, ":："))
	for _, stop := range []string{"查看", "浏览", "点击"} {
		if index := strings.Index(value, stop); index >= 0 {
			value = value[:index]
		}
	}
	return cleanCttText(value)
}

func cleanCttText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func cttDateFromBlock(block *goquery.Selection) string {
	if block == nil || block.Length() == 0 {
		return ""
	}

	for _, selector := range []string{".news-time", ".timecon", ".notice-title-time", ".notice-time", "time", ".date", ".time", "[class*='date']", "[class*='time']"} {
		if date := cttDateFromScope(block.Find(selector).First()); date != "" {
			return date
		}
	}
	// Home tab rows use an unclassed direct span for their source date. It is
	// still a fixed row field; do not scan the title/body for an arbitrary date.
	var date string
	block.ChildrenFiltered("span").EachWithBreak(func(_ int, field *goquery.Selection) bool {
		date = cttDateFromScope(field)
		return date == ""
	})
	return date
}

func cttPublishedAtFromBlock(block *goquery.Selection) string {
	if block == nil || block.Length() == 0 {
		return ""
	}
	for _, selector := range []string{".news-time", ".timecon", ".notice-title-time", ".notice-time", "time", ".date", ".time", "[class*='date']", "[class*='time']"} {
		if published := cttPublishedAtFromScope(block.Find(selector).First()); published != "" {
			return published
		}
	}
	var published string
	block.ChildrenFiltered("span").EachWithBreak(func(_ int, field *goquery.Selection) bool {
		published = cttPublishedAtFromScope(field)
		return published == ""
	})
	return published
}

func cttDateFromScope(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}

	day := strings.TrimSpace(scope.Find(".day, .notice-day").First().Text())
	month := strings.TrimSpace(scope.Find(".month, .notice-year").First().Text())
	if day != "" && month != "" {
		if match := cttYearMonthRe.FindStringSubmatch(month); match != nil {
			return normalizeDateParts(match[1], match[2], day)
		}
		if match := cttFullDateRe.FindStringSubmatch(month); match != nil {
			return normalizeDateParts(match[1], match[2], match[3])
		}
	}

	text := cleanCttText(flattenedText(scope))
	if precise := cttPublishedAtFromText(text); precise != "" {
		return precise[:10]
	}
	if match := cttFullDateRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	if match := cttYearMonthRe.FindStringSubmatch(text); match != nil && day != "" {
		return normalizeDateParts(match[1], match[2], day)
	}
	if match := cttMonthDayRe.FindStringSubmatch(text); match != nil {
		month, monthErr := strconv.Atoi(match[1])
		day, dayErr := strconv.Atoi(match[2])
		if monthErr == nil && dayErr == nil && month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			return fmt.Sprintf("%02d-%02d", month, day)
		}
	}
	return ""
}

func cttPublishedAtFromScope(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	return cttPublishedAtFromText(cleanCttText(flattenedText(scope)))
}

func cttPublishedAtFromText(text string) string {
	match := dateTimeRe.FindString(text)
	if match == "" {
		return ""
	}
	value := normalizePublishedAt(match)
	if publishedAtHasClock(value) {
		return value
	}
	return ""
}

func parseCttArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse CTT article: %w", err)
	}
	if reason := cttArticleShellReason(doc); reason != "" {
		return nil, fmt.Errorf("ctt: %s", reason)
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	// CTT's article headline is a fixed field in .left-top. Keep the source
	// title verbatim rather than accepting a banner/sidebar heading selected by
	// the generic parser.
	article.Title = cttArticleTitle(doc)
	cttApplyArticleMeta(doc, article)
	cttAppendAttachments(article, doc, baseURL)
	cttFinalizeContent(article, doc, baseURL)

	if strings.TrimSpace(article.Title) == "" {
		return nil, fmt.Errorf("ctt: article has no title")
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("ctt: article body is empty")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func cttArticleIsShell(doc *goquery.Document) bool {
	return cttArticleShellReason(doc) != ""
}

func cttArticleShellReason(doc *goquery.Document) string {
	if doc == nil {
		return "empty HTML response"
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return fmt.Sprintf("non-article pageType %q", pageType)
	}
	title := cleanCttText(doc.Find("title").First().Text())
	titleLower := strings.ToLower(title)
	if strings.Contains(title, "404") || strings.Contains(title, "错误提示") || strings.Contains(title, "验证码") || strings.Contains(title, "附件下载") || strings.Contains(titleLower, "not found") {
		if reason := cttBlockedTextReason(title); reason != "" {
			return reason
		}
		return "article error response"
	}
	if reason := cttResponseReason(doc); reason != "" {
		return reason
	}
	if cttArticleTitleSelection(doc).Length() == 0 {
		return "article detail has no fixed title"
	}
	content := cttArticleContent(doc)
	if content == nil || content.Length() == 0 {
		return "article detail has no fixed content"
	}
	return ""
}

func cttArticleTitleSelection(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	return doc.Find(".left-top > .news-title, .left-top > h1.news_tit").First()
}

func cttArticleTitle(doc *goquery.Document) string {
	selection := cttArticleTitleSelection(doc)
	if selection == nil || selection.Length() == 0 {
		return ""
	}
	return strings.TrimSpace(selection.Text())
}

func cttArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	for _, selector := range []string{".left-main .v_news_content", ".v_news_content", "#vsb_content_501", ".content-main"} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return nil
}

func cttApplyArticleMeta(doc *goquery.Document, article *model.Article) {
	if doc == nil || article == nil {
		return
	}
	// Reset generic-parser guesses first. CTT publishes its authoritative
	// fields only in this fixed metadata row; dates in article prose (event,
	// deadline, meeting time) are never publication dates.
	article.Source = ""
	article.Date = ""
	article.PublishedAt = ""
	article.Type = ""
	meta := doc.Find(".left-top > .top-info, .left-top > .news_source").First()
	if meta.Length() == 0 {
		return
	}
	meta.Find("span").Each(func(_ int, field *goquery.Selection) {
		value := cleanCttText(flattenedText(field))
		switch {
		case strings.HasPrefix(value, "来源：") || strings.HasPrefix(value, "来源:"):
			article.Source = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "来源："), "来源:"))
		case strings.HasPrefix(value, "发布日期：") || strings.HasPrefix(value, "发布日期:") || strings.HasPrefix(value, "日期：") || strings.HasPrefix(value, "日期:"):
			value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(value, "发布日期："), "发布日期:"), "日期："), "日期:"))
			if date, publishedAt := cttDateValue(value); date != "" {
				article.Date = date
				if publishedAt != "" {
					article.PublishedAt = publishedAt
				}
			}
		case strings.HasPrefix(value, "类型：") || strings.HasPrefix(value, "类型:") || strings.HasPrefix(value, "分类：") || strings.HasPrefix(value, "分类:"):
			article.Type = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(value, "类型："), "类型:"), "分类："), "分类:"))
		}
	})
}

func cttDateValue(value string) (string, string) {
	value = cleanCttText(value)
	if precise := cttPublishedAtFromText(value); precise != "" {
		return precise[:10], precise
	}
	if match := cttFullDateRe.FindStringSubmatch(value); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func cttAppendAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find(".news-left a[href], .left-main a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" {
			return
		}
		name := cttAttachmentName(anchor, href)
		if !cttIsAttachmentLink(href, name, anchor.AttrOr("download", "")) || name == "" {
			return
		}
		resolved := cttResourceURL(baseURL, href)
		if resolved == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		anchor.SetAttr("href", resolved)
	})
}

func cttIsAttachmentLink(href, name, download string) bool {
	return cttDownloadURLRe.MatchString(href) || cttAttachmentExtRe.MatchString(href) || cttAttachmentExtRe.MatchString(name) || strings.TrimSpace(download) != ""
}

func cttResourceURL(baseURL, raw string) string {
	return bodaResourceURL(baseURL, raw)
}

func cttAttachmentName(anchor *goquery.Selection, href string) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"download", "name"} {
		if value := cleanCttText(anchor.AttrOr(attr, "")); value != "" {
			if attr == "download" && strings.EqualFold(value, "true") {
				continue
			}
			return value
		}
	}
	if value := cleanCttText(anchor.Text()); value != "" {
		return value
	}
	if parsed, err := url.Parse(href); err == nil {
		return path.Base(parsed.Path)
	}
	return ""
}

func cttFinalizeContent(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	content := cttArticleContent(doc)
	if content == nil || content.Length() == 0 {
		return
	}
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		raw := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				raw = value
				break
			}
		}
		if resolved := cttResourceURL(baseURL, raw); resolved != "" {
			image.SetAttr("src", resolved)
		} else {
			image.RemoveAttr("src")
		}
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		name := cttAttachmentName(anchor, href)
		if cttIsAttachmentLink(href, name, anchor.AttrOr("download", "")) {
			if resolved := cttResourceURL(baseURL, href); resolved != "" {
				anchor.SetAttr("href", resolved)
			} else {
				anchor.RemoveAttr("href")
			}
		}
	})
	if html, err := content.Html(); err == nil {
		article.ContentHTML = html
	}
	if strings.TrimSpace(article.Content) == "" {
		article.Content = cleanCttText(flattenedText(content))
	}
}
