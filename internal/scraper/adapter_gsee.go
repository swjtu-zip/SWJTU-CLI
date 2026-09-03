package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const gseeCanonicalBaseURL = "https://gsee.swjtu.edu.cn"

// gseeSubPages contains the reachable article-list entry points found in the
// navigation, news-center sidebar, and homepage cards. The two 文件下载 indexes
// are kept separate below: they are resource indexes, not ordinary article
// feeds, and some of their rows are direct file downloads.
var gseeSubPages = []string{
	"xwzx/xwdt",
	"xwzx/tzgg/dzbg1",
	"xwzx/tzgg",
	"xwzx/xsxx",
	"xwzx/tzgg/sysgl",

	"bksjy",
	"bksjy/tzgg",
	"yjsjy/tzgg",
	"xsky/xsdt",
	"xkjs/xsjl",

	"djgz/djdt",
	"djgz/zxtz",
	"djgz/dwgg",
	"djgz/dxgz",
	"djgz/llyd",
	"djgz/hszd",

	"xtgz",
	"xtgz/xtdt",
	"xtgz/tzgg",
	"xtgz/jygz/jyxx",
	"xygz/tzgg",
}

var gseeResourcePages = []string{
	"djgz/wjxz",
	"xtgz/wjxz",
	"info/1298/10593",
}

// gsee.swjtu.edu.cn uses a Visual SiteBuilder template whose list rows are
// <dl>s under .conlist. The first link in <dt> is a category label and the
// second link is the article; dates are in the matching <dd>.
func init() {
	if site := AllSites["gsee"]; site != nil {
		site.BaseURL = gseeCanonicalBaseURL
	}
	registerAdapter("gsee", SiteAdapter{
		SubPages:     gseeSubPages,
		ParseList:    parseGseeNews,
		ParseArticle: parseGseeArticle,
		PageURL:      gseePageURL,
	})
}

var (
	gseeArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	gseeFullDateRe    = regexp.MustCompile(`(20[0-9]{2})\s*(?:年|[-/.])\s*([0-9]{1,2})\s*(?:月|[-/.])\s*([0-9]{1,2})\s*日?`)
	gseeShortDateRe   = regexp.MustCompile(`(?:^|[^0-9])([0-9]{1,2})\s*[-/.]\s*([0-9]{1,2})(?:\s*日)?(?:$|[^0-9])`)
	gseeDateTimeRe    = regexp.MustCompile(`20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?\s*(?:[Tt]|\s)+\s*[0-9]{1,2}\s*[:：]\s*[0-9]{2}(?:\s*[:：]\s*[0-9]{2})?`)
	gseeISODateRe     = regexp.MustCompile(`^20[0-9]{2}-[0-9]{2}-[0-9]{2}$`)
	gseeShortISORe    = regexp.MustCompile(`^[0-9]{2}-[0-9]{2}$`)
	gseeAttachmentExt = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
)

// gseePageURL maps logical pages to the reverse-numbered static files used
// by the site. For example, xwzx/xwdt.htm is page 1 and xwzx/xwdt/130.htm
// is page 2 when the pagination reports 131 pages.
func gseePageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}

	// Older snapshots and callers used the short names before the site moved
	// the news center below xwzx/. Keep those aliases pointed at the live path.
	switch slug {
	case "xwdt", "tzgg", "xsxx":
		slug = "xwzx/" + slug
	}

	if baseURL == "" || slug == "" || page < 1 {
		return ""
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

func parseGseeNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse gsee html: %w", err)
	}
	if err := gseeResponseError(doc); err != nil {
		return nil, err
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	// Category/list pages have a stable, narrow container. Do not fall back to
	// a document-wide info/ scan: the same pages contain navigation and service
	// links that are not articles.
	list := doc.Find(".main-right .conlist").First()
	if list.Length() == 0 {
		list = doc.Find(".conlist").First()
	}
	if list.Length() > 0 {
		rows := list.ChildrenFiltered("dl")
		defaultType := gseeListPageType(doc)
		if defaultType == "文件下载" {
			return nil, fmt.Errorf("gsee: resource page is not an article list")
		}
		rows.Each(func(_ int, row *goquery.Selection) {
			addGseeListRow(&items, seen, row, defaultType, baseURL)
		})
		return items, nil
	}

	if gseeHomeLayout(doc) {
		// index.htm is a dashboard rather than a .conlist page. Its featured
		// links carry complete titles, while the adjacent news/notice lists carry
		// dates (often only MM-DD). Add both sources and merge by canonical URL.
		doc.Find(".main_visual .main_image > ul > li").Each(func(_ int, row *goquery.Selection) {
			anchor := gseeArticleAnchor(row, baseURL)
			if anchor == nil || anchor.Length() == 0 {
				return
			}
			title := strings.TrimSpace(row.Find("p").First().Text())
			if title == "" {
				title = gseeAnchorTitle(anchor)
			}
			addGseeItem(&items, seen, baseURL, anchor.AttrOr("href", ""), title, "", "新闻动态")
		})

		doc.Find(".news > ul > li").Each(func(_ int, row *goquery.Selection) {
			anchor := gseeArticleAnchor(row, baseURL)
			if anchor == nil || anchor.Length() == 0 {
				return
			}
			addGseeItem(&items, seen, baseURL, anchor.AttrOr("href", ""), gseeAnchorTitle(anchor), row.Find("em").First().Text(), "新闻动态")
		})

		// The notice tabs are all rendered in the HTML, including hidden tabs. They
		// are useful content sections, so retain their unique article links while
		// merging entries repeated in the “全部” tab.
		notice := doc.Find(".notice").First()
		notice.Find("ul[id^='con_one_'] > li").Each(func(_ int, row *goquery.Selection) {
			anchor := gseeArticleAnchor(row, baseURL)
			if anchor == nil || anchor.Length() == 0 {
				return
			}
			addGseeItem(&items, seen, baseURL, anchor.AttrOr("href", ""), gseeAnchorTitle(anchor), row.Find("em").First().Text(), gseeHomeNoticeType(notice, row))
		})

		// The academic homepage cards have an external poster link in the image
		// cell and a local article link in dd/h4. Select the latter explicitly.
		doc.Find(".acad > dl").Each(func(_ int, row *goquery.Selection) {
			anchor := gseeArticleAnchor(row, baseURL)
			if anchor == nil || anchor.Length() == 0 {
				return
			}
			title := gseeNormalizeTitle(row.Find("dd h4 a").First().Text())
			if title == "" {
				title = gseeAnchorTitle(anchor)
			}
			addGseeItem(&items, seen, baseURL, anchor.AttrOr("href", ""), title, row.Find("dd p").Last().Text(), "学术信息")
		})
	}

	// Academic entries pair a date-title immediately before each schedule.
	schedules := doc.Find(".acad .schedule")
	if schedules.Length() == 0 {
		// The academic-information page currently renders the same schedule
		// rows directly under .main-right .content, without the .acad wrapper
		// used by the homepage template.
		schedules = doc.Find(".main-right .content .schedule")
	}
	schedules.Each(func(_ int, schedule *goquery.Selection) {
		anchor := schedule.Find(".todo h5 a[href], .todo a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		date := ""
		previous := schedule.Prev()
		if previous.Length() > 0 && previous.HasClass("date-title") {
			date = previous.Find("h4").First().Text()
		}
		addGseeItem(&items, seen, baseURL, anchor.AttrOr("href", ""), gseeAnchorTitle(anchor), date, gseeListPageType(doc))
	})

	if schedules.Length() > 0 {
		return items, nil
	}
	if !gseeHomeLayout(doc) {
		return nil, fmt.Errorf("gsee: page is not an article list (static/resource/redirect page)")
	}
	return items, nil
}

func addGseeListRow(items *[]model.NewsItem, seen map[string]int, row *goquery.Selection, defaultType, baseURL string) {
	anchor := gseeListAnchor(row, baseURL)
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	itemType := gseeRowType(row)
	if itemType == "" {
		itemType = defaultType
	}
	addGseeItem(items, seen, baseURL, anchor.AttrOr("href", ""), gseeAnchorTitle(anchor), row.Find("dd").First().Text(), itemType)
}

// gseeListAnchor deliberately prefers a direct dt child. The category link
// is nested inside <span>, while the article link is the direct child of dt.
func gseeListAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	var fallback *goquery.Selection
	anchors := row.Find("dt a[href]")
	if anchors.Length() == 0 {
		anchors = row.Find("a[href]")
	}
	anchors.EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := gseeArticleURL(baseURL, anchor.AttrOr("href", "")); !ok {
			return true
		}
		title := gseeAnchorTitle(anchor)
		if title == "" || isGseeCategoryTitle(title) {
			return true
		}
		if anchor.Parent().Length() > 0 && anchor.Parent().Get(0).Data != "span" {
			fallback = anchor
			return false
		}
		if fallback == nil {
			fallback = anchor
		}
		return true
	})
	return fallback
}

func addGseeItem(items *[]model.NewsItem, seen map[string]int, baseURL, href, title, dateText, itemType string) {
	fullURL, ok := gseeArticleURL(baseURL, href)
	if !ok {
		return
	}

	title = gseeNormalizeTitle(title)
	if title == "" || isGseeCategoryTitle(title) || !isValidNewsTitle(title) || utf8.RuneCountInString(title) > 200 {
		return
	}
	date, publishedAt := gseeDateValues(dateText)
	itemType = gseeCleanType(itemType)

	if index, exists := seen[fullURL]; exists {
		if gseePreferTitle((*items)[index].Title, title) {
			(*items)[index].Title = title
		}
		// Keep the metadata attached to the first homepage module. In
		// particular, a carousel item has no date; a duplicate dated row in
		// the news module must not make that carousel item appear dated.
		if (*items)[index].Date != "" && gseeDateRank(date) > gseeDateRank((*items)[index].Date) {
			(*items)[index].Date = date
			if publishedAt != "" && !publishedAtHasClock((*items)[index].PublishedAt) {
				(*items)[index].PublishedAt = publishedAt
			}
		}
		if gseePreferType((*items)[index].Type, itemType) {
			(*items)[index].Type = itemType
		}
		return
	}

	seen[fullURL] = len(*items)
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        itemType,
		ParsedAt:    time.Now(),
	})
}

func gseeArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !gseeArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	if base.Scheme == "https" {
		parsed.Scheme = "https"
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func gseeAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	if title == "" {
		image := anchor.Find("img").First()
		title = strings.TrimSpace(image.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(image.AttrOr("alt", ""))
		}
	}
	return gseeNormalizeTitle(title)
}

func gseeNormalizeTitle(title string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
}

func isGseeCategoryTitle(title string) bool {
	title = gseeNormalizeTitle(title)
	return (len(title) >= 2 && strings.HasPrefix(title, "[") && strings.HasSuffix(title, "]")) ||
		(len(title) >= 2 && strings.HasPrefix(title, "【") && strings.HasSuffix(title, "】"))
}

func gseePreferTitle(current, candidate string) bool {
	current = gseeNormalizeTitle(current)
	candidate = gseeNormalizeTitle(candidate)
	if current == "" {
		return candidate != ""
	}
	currentTruncated := strings.HasSuffix(current, "...") || strings.HasSuffix(current, "…")
	candidateTruncated := strings.HasSuffix(candidate, "...") || strings.HasSuffix(candidate, "…")
	if currentTruncated != candidateTruncated {
		return currentTruncated
	}
	return utf8.RuneCountInString(candidate) > utf8.RuneCountInString(current)
}

func gseeDateRank(date string) int {
	if gseeISODateRe.MatchString(date) {
		return 2
	}
	if gseeShortISORe.MatchString(date) {
		return 1
	}
	return 0
}

// gseeDateFromText returns a full ISO date whenever the source provides a
// year. A homepage value such as 09-01 remains 09-01: no year is inferred.
func gseeDateFromText(text string, allowShort bool) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.Join(strings.Fields(text), " ")
	if match := gseeFullDateRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	if !allowShort {
		return ""
	}
	if match := gseeShortDateRe.FindStringSubmatch(text); match != nil {
		month, monthErr := strconv.Atoi(match[1])
		day, dayErr := strconv.Atoi(match[2])
		if monthErr == nil && dayErr == nil && month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			return fmt.Sprintf("%02d-%02d", month, day)
		}
	}
	return ""
}

func gseeDateValues(text string) (string, string) {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.Join(strings.Fields(text), " ")
	if match := dateTimeRe.FindString(text); match != "" {
		if published := normalizePublishedAt(match); publishedAtHasClock(published) {
			return published[:10], published
		}
	}
	if match := gseeDateTimeRe.FindString(text); match != "" {
		normalized := strings.NewReplacer("年", "-", "月", "-", "日", " ", "号", " ", "时", ":", "分", "", "：", ":").Replace(match)
		if published := normalizePublishedAt(strings.Join(strings.Fields(normalized), " ")); publishedAtHasClock(published) {
			return published[:10], published
		}
	}
	return gseeDateFromText(text, true), ""
}

func gseeRowType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	var itemType string
	row.Find("dt a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		title := gseeAnchorTitle(anchor)
		if isGseeCategoryTitle(title) {
			itemType = gseeCleanType(title)
			return false
		}
		return true
	})
	return itemType
}

func gseeCleanType(value string) string {
	value = gseeNormalizeTitle(value)
	return strings.Trim(strings.TrimSpace(value), "[]【】")
}

func gseePreferType(current, candidate string) bool {
	current = gseeCleanType(current)
	candidate = gseeCleanType(candidate)
	if candidate == "" || candidate == current {
		return false
	}
	return current == "" || current == "全部" || current == "通知公告"
}

func gseeListPageType(doc *goquery.Document) string {
	if typ := gseeBreadcrumbType(doc); typ != "" {
		return typ
	}
	return gseeCleanType(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).First().AttrOr("content", ""))
}

func gseeBreadcrumbType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	var itemType string
	doc.Find(".current a").Each(func(_ int, anchor *goquery.Selection) {
		value := gseeNormalizeTitle(anchor.Text())
		if value == "" || value == "正文" || value == "首页" || strings.Contains(value, "地学学院") {
			return
		}
		itemType = value
	})
	return gseeCleanType(itemType)
}

func gseeHomeLayout(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".main_visual .main_image, .news, .notice, .acad").Length() > 0
}

func gseeHomeNoticeType(notice, row *goquery.Selection) string {
	if notice == nil || row == nil || row.Length() == 0 {
		return "通知公告"
	}
	id := row.Parent().AttrOr("id", "")
	if !strings.HasPrefix(id, "con_one_") {
		return "通知公告"
	}
	number := strings.TrimPrefix(id, "con_one_")
	typ := gseeCleanType(notice.Find("#one" + number).First().Text())
	if typ == "" || typ == "全部" {
		return "通知公告"
	}
	return typ
}

func gseeArticleAnchor(scope *goquery.Selection, baseURL string) *goquery.Selection {
	if scope == nil || scope.Length() == 0 {
		return nil
	}
	var result *goquery.Selection
	scope.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := gseeArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			result = anchor
			return false
		}
		return true
	})
	return result
}

func gseeResponseError(doc *goquery.Document) error {
	if doc == nil {
		return fmt.Errorf("gsee: empty response")
	}
	title := strings.ToLower(gseeNormalizeTitle(doc.Find("title").First().Text()))
	visible := strings.ToLower(gseeNormalizeTitle(doc.Find("body").First().Text()))
	if strings.Contains(title, "验证码") || strings.Contains(title, "安全验证") ||
		strings.Contains(title, "404") || strings.Contains(title, "错误") ||
		strings.Contains(title, "not found") || strings.Contains(title, "附件下载") {
		return fmt.Errorf("gsee: received error or verification page")
	}
	if doc.Find("#captcha, .captcha, [id*='captcha'], [class*='captcha']").Length() > 0 ||
		(strings.Contains(visible, "验证码") && strings.Contains(visible, "下载")) {
		return fmt.Errorf("gsee: received captcha/verification page")
	}
	if strings.Contains(title, "登录") || strings.Contains(title, "login") {
		return fmt.Errorf("gsee: received login page")
	}
	return nil
}

// parseGseeArticle keeps the shared article/resource extraction but owns the
// gsee-specific h3/source hooks. The live article template uses .article h3
// and .article .source instead of the generic .news_tit/.news_source names.
func parseGseeArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse gsee article: %w", err)
	}
	if err := gseeResponseError(doc); err != nil {
		return nil, err
	}
	pageType, hasArticlePageType := gseePageType(doc)
	if pageType != "" && !hasArticlePageType {
		return nil, fmt.Errorf("gsee: page is not an article (pageType %q)", pageType)
	}
	title := gseeArticleTitle(doc)
	if title == "" {
		return nil, fmt.Errorf("gsee: article has no title")
	}
	content := gseeArticleContent(doc)
	if content.Length() == 0 {
		return nil, fmt.Errorf("gsee: page is not an article (no content container)")
	}
	if strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a").Length() == 0 {
		return nil, fmt.Errorf("gsee: article body is empty")
	}
	articleType := gseeBreadcrumbType(doc)
	if articleType == "文件下载" {
		return nil, fmt.Errorf("gsee: resource page is not a news article")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	article.Title = title
	if articleType != "" {
		article.Type = articleType
	}
	source := doc.Find(".article .source").First()
	if source.Length() > 0 {
		if date, publishedAt := gseeDateValues(source.Text()); date != "" {
			article.Date = date
			if publishedAt != "" {
				article.PublishedAt = publishedAt
			}
		}
	} else {
		// .date-title is the scheduled event date on the academic poster
		// template, not the publication date of the detail page. Do not let
		// the generic parser promote it to Article.Date/PublishedAt.
		article.Date = ""
		article.PublishedAt = ""
	}
	gseeAppendAttachments(article, doc, baseURL)
	gseeFinalizeArticleContent(article, doc, baseURL)
	return article, nil
}

// gseePageType sees duplicate CMS metadata emitted by the live template.  A
// wrapper can carry pageType=1 before the actual article metadata carries
// pageType=3; the latter is authoritative whenever it is present.
func gseePageType(doc *goquery.Document) (first string, hasArticle bool) {
	if doc == nil {
		return "", false
	}
	doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).EachWithBreak(func(_ int, meta *goquery.Selection) bool {
		value := strings.TrimSpace(meta.AttrOr("content", ""))
		if value == "" {
			return true
		}
		if first == "" {
			first = value
		}
		if value == "3" {
			hasArticle = true
			return false
		}
		return true
	})
	return first, hasArticle
}

func gseeArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".article h3", ".article h1", `meta[name="pageTitle"], meta[name="pagetitle"]`} {
		if selector == `meta[name="pageTitle"], meta[name="pagetitle"]` {
			if title := gseeNormalizeTitle(doc.Find(selector).First().AttrOr("content", "")); title != "" {
				return title
			}
			continue
		}
		if title := gseeNormalizeTitle(doc.Find(selector).First().Text()); title != "" {
			return title
		}
	}
	return gseeNormalizeTitle(stripSiteSuffix(doc.Find("title").First().Text()))
}

func gseeArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	for _, selector := range []string{".article .v_news_content", ".article #vsb_content", ".article .content14", ".article .content"} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return &goquery.Selection{}
}

func gseeAppendAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	scope := doc.Find(".article").First()
	if scope.Length() == 0 {
		scope = doc.Find(".v_news_content").First()
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		name := gseeAttachmentName(anchor, href)
		if href == "" || name == "" || !gseeIsAttachmentLink(anchor, href, name) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if resolved == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		anchor.SetAttr("href", resolved)
	})
}

func gseeIsAttachmentLink(anchor *goquery.Selection, href, name string) bool {
	lowerHref := strings.ToLower(href)
	if strings.Contains(lowerHref, "download.jsp") || strings.Contains(lowerHref, "downloadattachurl") || strings.TrimSpace(anchor.AttrOr("download", "")) != "" {
		return true
	}
	if gseeAttachmentExt.MatchString(href) || gseeAttachmentExt.MatchString(name) {
		return true
	}
	return strings.Contains(gseeNormalizeTitle(anchor.Parent().Text()), "附件")
}

func gseeAttachmentName(anchor *goquery.Selection, href string) string {
	for _, attr := range []string{"download", "data-filename", "data-name", "name", "title"} {
		if value := gseeNormalizeTitle(anchor.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	if value := gseeNormalizeTitle(anchor.Text()); value != "" {
		return value
	}
	parsed, err := url.Parse(href)
	if err == nil {
		return gseeNormalizeTitle(parsed.Path[strings.LastIndex(parsed.Path, "/")+1:])
	}
	return ""
}

func gseeFinalizeArticleContent(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	content := gseeArticleContent(doc)
	if content.Length() == 0 {
		return
	}
	content.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		image.SetAttr("src", cleanURL(resolveURL(baseURL, image.AttrOr("src", ""))))
	})
	content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		name := gseeAttachmentName(anchor, href)
		if href != "" && name != "" && gseeIsAttachmentLink(anchor, href, name) {
			anchor.SetAttr("href", cleanURL(resolveURL(baseURL, href)))
		}
	})
	if html, err := content.Html(); err == nil {
		article.ContentHTML = html
	}
}
