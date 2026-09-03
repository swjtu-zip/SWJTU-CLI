package scraper

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const hospitalCanonicalBaseURL = "https://hospital.swjtu.edu.cn"

// hospitalSubPages contains every article-bearing list surface found in the
// hospital's navigation. Several old side-bar entries are generated copies of
// their parent list, but they remain explicit feeds: they are real list URLs
// and the archive de-duplicates their canonical article URLs.
//
// The following discovered pages are deliberately not feeds:
// yygk/*, jkgl.htm, ylbx/bxxz.htm and the other profile/appointment pages are
// static or service pages; the home-page department cards are also static
// information pages rather than news.
var hospitalSubPages = []string{
	"ddjs",
	"yydt",
	"tzgg",
	"jkjy",
	"jkjy/azbjkjyzl",
	"jkjy/jhbjkjyzl",
	"jkjy/jkshfszl",
	"jkjy/rccrbfk",
	"ylbx",
	"ylbx/xsyb",
	"ylbx/jgyb",
	"jkgl/jzgjkgl",
	"jkgl/xsjkgl",
	"hszh",
	"hszh/xhszhfc",
	"hszh/hszyd",
}

var (
	hospitalArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	hospitalDateRE        = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*(?:日|号)?`)
	hospitalMonthDayRE    = regexp.MustCompile(`(?:^|\s|[^\d])(\d{1,2})\s*[-/.月]\s*(\d{1,2})(?:日)?(?:$|\s|[^\d])`)
	hospitalPageNumberRE  = regexp.MustCompile(`^\s*(\d+)\s*$`)
)

var hospitalAttachmentExtensions = []string{
	".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf",
	".zip", ".rar", ".7z", ".txt", ".csv",
}

func init() {
	if site := AllSites["hospital"]; site != nil {
		site.BaseURL = hospitalCanonicalBaseURL
	}
	registerAdapter("hospital", SiteAdapter{
		SubPages:     hospitalSubPages,
		ParseList:    parseAdapter_hospitalList,
		ParseArticle: parseAdapter_hospitalArticle,
		PageURL:      hospitalPageURL,
		ListPage:     hospitalListPage,
		TotalPages:   hospitalTotalPages,
	})
}

// hospitalListPage is the public list path for this site.  The home page and
// some list pages render long headlines with a literal trailing ellipsis and
// no title attribute.  Repair those rows from the authoritative detail page
// before returning them to the archive; publishing the display-layer prefix
// would permanently lose part of the headline.
func hospitalListPage(slug string, page int) ([]model.NewsItem, int, error) {
	site := AllSites["hospital"]
	if site == nil {
		return nil, 0, fmt.Errorf("hospital: site is not registered")
	}
	baseURL := strings.TrimRight(site.BaseURL, "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if page < 1 {
		page = 1
	}

	firstBody, err := hospitalFetchPage(baseURL, slug, 1, 0)
	if err != nil {
		return nil, 0, err
	}
	totalPages := 1
	if slug != "" && slug != "index" && slug != "home" {
		totalPages = hospitalPageCountFromHTML(firstBody)
		if totalPages == 0 {
			return nil, 0, fmt.Errorf("hospital: %s has no recognized pagination", slug)
		}
	}
	if page > totalPages {
		return nil, 0, fmt.Errorf("hospital: page %d exceeds %d pages for %s", page, totalPages, slug)
	}

	body := firstBody
	if page > 1 {
		body, err = hospitalFetchPage(baseURL, slug, page, totalPages)
		if err != nil {
			return nil, 0, err
		}
	}
	items, err := parseAdapter_hospitalList(body, baseURL)
	if err != nil {
		return nil, 0, err
	}
	if err := hospitalRepairTruncatedTitles(items, baseURL); err != nil {
		return nil, 0, err
	}
	return items, totalPages, nil
}

func hospitalFetchPage(baseURL, slug string, page, totalPages int) ([]byte, error) {
	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		pageURL := hospitalPageURL(baseURL, slug, page, totalPages, extension)
		body, err := fetch(pageURL)
		if err != nil {
			lastErr = err
			continue
		}
		if err := hospitalResponseError(body); err != nil {
			return nil, err
		}
		return body, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("hospital: unable to fetch %s page %d", slug, page)
	}
	return nil, lastErr
}

func hospitalRepairTruncatedTitles(items []model.NewsItem, baseURL string) error {
	for i := range items {
		if !hospitalTitleIsTruncated(items[i].Title) {
			continue
		}
		body, err := fetch(items[i].URL)
		if err != nil {
			return fmt.Errorf("hospital: fetch detail for truncated title %q: %w", items[i].URL, err)
		}
		article, err := parseAdapter_hospitalArticle(body, baseURL)
		if err != nil {
			return fmt.Errorf("hospital: parse detail for truncated title %q: %w", items[i].URL, err)
		}
		if article == nil || article.Title == "" || hospitalTitleIsTruncated(article.Title) {
			return fmt.Errorf("hospital: detail still has truncated title for %q", items[i].URL)
		}
		items[i].Title = article.Title
		if items[i].Date == "" {
			items[i].Date = article.Date
		}
		if items[i].PublishedAt == "" {
			items[i].PublishedAt = article.PublishedAt
		}
		if items[i].Type == "" {
			items[i].Type = article.Type
		}
	}
	return nil
}

func hospitalTitleIsTruncated(title string) bool {
	title = strings.TrimSpace(title)
	return strings.HasSuffix(title, "...") || strings.HasSuffix(title, "…")
}

// hospitalPageURL maps the site's logical page number to its generated file.
// The unsuffixed URL is logical page 1; the displayed page 2 is /24.htm on a
// 25-page list, and the oldest page is /1.htm.
func hospitalPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if slug == "" || slug == "index" || slug == "home" {
		if page < 1 || page > 1 {
			return ""
		}
		return baseURL + "/index" + extension
	}
	if page < 1 {
		return ""
	}
	if page == 1 {
		return baseURL + "/" + slug + extension
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

func hospitalKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	for _, known := range hospitalSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

// hospitalTotalPages reads the first list page and uses displayed logical
// page labels. The hrefs themselves are reverse-numbered generated files.
func hospitalTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" || slug == "home" {
		return 1, nil
	}
	if !hospitalKnownFeed(slug) {
		return 0, fmt.Errorf("hospital: %q is not an article-list route", slug)
	}

	site := AllSites["hospital"]
	if site == nil {
		return 0, fmt.Errorf("hospital: site is not registered")
	}
	baseURL := strings.TrimRight(site.BaseURL, "/")
	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(hospitalPageURL(baseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if err := hospitalResponseError(body); err != nil {
			lastErr = err
			continue
		}
		doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		if doc.Find(".conlistComm").Length() == 0 {
			lastErr = fmt.Errorf("hospital: %s is not an article list", slug)
			continue
		}
		if total := hospitalPageCountFromHTML(body); total > 0 {
			return total, nil
		}
		lastErr = fmt.Errorf("hospital: %s has no recognized pagination", slug)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("hospital: unable to read page count for %s", slug)
	}
	return 0, lastErr
}

func hospitalPageCountFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0
	}
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		value := strings.TrimSpace(sel.Text())
		if match := hospitalPageNumberRE.FindStringSubmatch(value); match != nil {
			if page, convErr := strconv.Atoi(match[1]); convErr == nil && page > maxPage {
				maxPage = page
			}
		}
	})
	if maxPage > 0 {
		return maxPage
	}
	if doc.Find(".p_pages").Length() > 0 {
		return 1
	}
	return 0
}

func parseAdapter_hospitalList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if err := hospitalResponseError(raw); err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("hospital: parse list HTML: %w", err)
	}

	if hospitalHomeLayout(doc) {
		items := parseHospitalHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("hospital: home page contains no recognized article modules")
		}
		return items, nil
	}

	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
	if pageType != "" && pageType != "2" {
		return nil, fmt.Errorf("hospital: page is not an article list (pageType %q)", pageType)
	}
	list := doc.Find(".conlistComm").First()
	if list.Length() == 0 {
		return nil, fmt.Errorf("hospital: page is not an article list (static/resource/appointment/external page)")
	}

	items := make([]model.NewsItem, 0, list.Find("dl").Length())
	seen := make(map[string]bool)
	list.Find("dl").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("dt > a[href]").First()
		fullURL, ok := hospitalArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := hospitalAnchorTitle(anchor)
		if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 160 {
			return
		}
		typ := hospitalCleanType(row.Find("dt span a[href]").First().Text())
		date, publishedAt := hospitalDateValues(row.Find("dd").First().Text())
		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     typ,
			ParsedAt: time.Now(),
		}
		if publishedAtHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}
		seen[fullURL] = true
		items = append(items, item)
	})

	// A real .conlistComm shell with no local rows is a legal empty list. It
	// can also contain external WeChat rows; those are intentionally omitted.
	return items, nil
}

func hospitalHomeLayout(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
	return pageType == "1" || doc.Find(".visual-slider1, .third .list-3, .first .list.half").Length() > 0
}

func parseHospitalHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	appendRows := func(rows *goquery.Selection, typ string) {
		rows.Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			fullURL, ok := hospitalArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok {
				return
			}
			title := hospitalHomeTitle(row, anchor)
			// Image-only headline carousel cards have no text or title in the
			// source. They are a visual/static module, not a safely identifiable
			// article row; do not invent metadata for them.
			if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 160 {
				return
			}
			date, publishedAt := hospitalDateValues(hospitalHomeDateText(row))
			item := model.NewsItem{Title: title, URL: fullURL, Date: date, Type: hospitalCleanType(typ), ParsedAt: time.Now()}
			if publishedAtHasClock(publishedAt) {
				item.PublishedAt = publishedAt
			}
			if index, exists := seen[fullURL]; exists {
				if items[index].Date == "" && item.Date != "" {
					items[index].Date = item.Date
				}
				if items[index].PublishedAt == "" && item.PublishedAt != "" {
					items[index].PublishedAt = item.PublishedAt
				}
				if items[index].Type == "" && item.Type != "" {
					items[index].Type = item.Type
				}
				return
			}
			seen[fullURL] = len(items)
			items = append(items, item)
		})
	}

	// The first visual slider is image-only and has no recoverable titles. The
	// second slider and its adjacent list are the textual医院动态 module.
	appendRows(doc.Find(".first .main-image2 > ul > li"), "医院动态")
	appendRows(doc.Find(".first .list.half ul.list-1 > li"), "医院动态")

	// The three visible/hidden home modules are independent article lists.
	doc.Find(".third .list-3 > li").Each(func(_ int, section *goquery.Selection) {
		typ := hospitalCleanType(section.Find(".title .h3").First().Text())
		appendRows(section.Find(".common-list > li"), typ)
	})

	return items
}

func hospitalHomeTitle(row, anchor *goquery.Selection) string {
	if value := strings.TrimSpace(anchor.AttrOr("title", "")); value != "" {
		return cleanTitle(value)
	}
	if value := strings.TrimSpace(row.Find(".pic-title").First().Text()); value != "" {
		return cleanTitle(value)
	}
	clone := anchor.Clone()
	clone.Find(".date, .time, time").Remove()
	return cleanTitle(strings.TrimSpace(clone.Text()))
}

func hospitalHomeDateText(row *goquery.Selection) string {
	for _, selector := range []string{".date", ".time", "time"} {
		if value := strings.TrimSpace(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return row.Text()
}

func hospitalAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if value := strings.TrimSpace(anchor.AttrOr("title", "")); value != "" {
		return cleanTitle(value)
	}
	clone := anchor.Clone()
	clone.Find(".date, .time, time").Remove()
	return cleanTitle(strings.TrimSpace(clone.Text()))
}

func hospitalCleanType(value string) string {
	value = strings.Join(strings.Fields(htmlstd.UnescapeString(strings.TrimSpace(value))), " ")
	return strings.TrimSpace(strings.Trim(value, "[]【】"))
}

func hospitalDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(htmlstd.UnescapeString(strings.TrimSpace(raw))), " ")
	if raw == "" {
		return "", ""
	}
	if precise := normalizePublishedAt(raw); precise != "" {
		if publishedAtHasClock(precise) {
			return precise[:10], precise
		}
		return precise[:10], ""
	}
	if match := hospitalDateRE.FindStringSubmatch(raw); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, ""
	}
	if match := hospitalMonthDayRE.FindStringSubmatch(raw); match != nil {
		month, monthErr := strconv.Atoi(match[1])
		day, dayErr := strconv.Atoi(match[2])
		if monthErr == nil && dayErr == nil && month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			return fmt.Sprintf("%02d-%02d", month, day), ""
		}
	}
	return "", ""
}

func hospitalArticleURL(baseURL, href string) (string, bool) {
	href = htmlstd.UnescapeString(strings.TrimSpace(href))
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !hospitalArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func hospitalResponseError(raw []byte) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("hospital: parse response HTML: %w", err)
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
	title := strings.ToLower(strings.TrimSpace(doc.Find("title").First().Text()))
	for _, marker := range []string{
		"请输入验证码", "验证码下载", "安全验证", "access denied", "forbidden",
		"just a moment", "cloudflare", "security verification", "waf",
	} {
		if strings.Contains(visible, marker) || strings.Contains(title, marker) {
			return fmt.Errorf("hospital: captcha/WAF/error response (%s)", marker)
		}
	}
	return nil
}

func parseAdapter_hospitalArticle(raw []byte, baseURL string) (*model.Article, error) {
	if err := hospitalResponseError(raw); err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("hospital: parse article HTML: %w", err)
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
	if pageType != "3" {
		return nil, fmt.Errorf("hospital: page is not an article (pageType %q)", pageType)
	}
	root := doc.Find("form[name='_newscontent_fromname'] .article").First()
	if root.Length() == 0 {
		root = doc.Find(".article").First()
	}
	if root.Length() == 0 || root.Find("h1").Length() == 0 || root.Find(".v_news_content").Length() == 0 {
		return nil, fmt.Errorf("hospital: article shell or body is missing")
	}
	content := root.Find(".v_news_content").First()
	if strings.TrimSpace(content.Text()) == "" && content.Find("img").Length() == 0 {
		return nil, fmt.Errorf("hospital: article body is empty")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = cleanTitle(strings.TrimSpace(root.Find("h1").First().Text()))
	article.Type = hospitalCleanType(doc.Find(".current h3").First().Text())
	if article.Type == "正文" {
		article.Type = ""
	}
	if date, publishedAt := hospitalDateValues(root.Find(".source").First().Text()); date != "" {
		if article.Date == "" {
			article.Date = date
		}
		if article.PublishedAt == "" && publishedAt != "" {
			article.PublishedAt = publishedAt
		}
	}
	hospitalAppendOutsideAttachments(article, doc, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func hospitalAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("form[name='_newscontent_fromname'] a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if !hospitalIsAttachmentLink(anchor, href) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if !hospitalSameHost(resolved, baseURL) || seen[resolved] {
			return
		}
		name := strings.TrimSpace(anchor.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(anchor.AttrOr("name", ""))
		}
		if name == "" {
			name = strings.TrimSpace(anchor.Text())
		}
		if name == "" {
			if parsed, err := url.Parse(resolved); err == nil {
				name = path.Base(parsed.Path)
			}
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func hospitalIsAttachmentLink(anchor *goquery.Selection, href string) bool {
	if anchor == nil || anchor.Length() == 0 || href == "" {
		return false
	}
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, value := range []string{
		anchor.AttrOr("download", ""), anchor.AttrOr("name", ""), href,
	} {
		lowerValue := strings.ToLower(value)
		for _, extension := range hospitalAttachmentExtensions {
			if strings.Contains(lowerValue, extension) {
				return true
			}
		}
	}
	return strings.Contains(strings.ToLower(anchor.Parent().Text()), "附件")
}

func hospitalSameHost(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	base, err := url.Parse(baseURL)
	return err == nil && strings.EqualFold(parsed.Hostname(), base.Hostname())
}
