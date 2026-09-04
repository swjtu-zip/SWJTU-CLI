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

	"swjtu-cli/internal/model"
)

const hospitalCanonicalBaseURL = "https://hospital.swjtu.edu.cn"

const hospitalMaxResourceBytes = 32 << 20

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
	hospitalArticlePathRE   = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	hospitalDateRE          = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*(?:日|号)?`)
	hospitalMonthDayRE      = regexp.MustCompile(`(?:^|\s|[^\d])(\d{1,2})\s*[-/.月]\s*(\d{1,2})(?:日)?(?:$|\s|[^\d])`)
	hospitalPageNumberRE    = regexp.MustCompile(`^\s*(\d+)\s*$`)
	hospitalResourceShellRE = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|请输入验证码|验证码下载|安全验证|人机验证|请先登录|登录页面|页面不存在|access\s+denied|forbidden|cloudflare|just\s+a\s+moment|waf|请求已被阻断`)
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
		FetchArticle: hospitalFetchArticle,
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
	if precise := normalizePublishedAt(raw); precise != "" && len(precise) >= 10 {
		if publishedAtHasClock(precise) {
			return precise[:10], precise
		}
		return precise[:10], ""
	}
	// The live hospital template prefixes the authoritative value with
	// “日期：”. Parse the date token first, then let the shared normalizer read
	// the optional clock from the remainder. This keeps body dates out while
	// preserving minute/second precision from .source.
	if indexes := hospitalDateRE.FindStringSubmatchIndex(raw); len(indexes) >= 8 {
		date := normalizeDateParts(raw[indexes[2]:indexes[3]], raw[indexes[4]:indexes[5]], raw[indexes[6]:indexes[7]])
		if precise := normalizePublishedAt(raw[indexes[0]:]); publishedAtHasClock(precise) {
			return date, precise
		}
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

func hospitalBaseURL() string {
	if site := AllSites["hospital"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return hospitalCanonicalBaseURL
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
		"页面不存在", "您访问的页面不存在", "请求已被阻断",
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
	// The source block is the only publication-metadata authority for this
	// site. Never retain a date inferred by parseBodaArticle from the body.
	article.Date, article.PublishedAt = hospitalDateValues(root.Find(".source").First().Text())
	article.Content, article.ContentHTML, article.Images = hospitalRenderContent(content, baseURL)
	article.Attachments = nil
	hospitalAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func hospitalRenderContent(content *goquery.Selection, baseURL string) (string, string, []string) {
	if content == nil || content.Length() == 0 {
		return "", "", nil
	}
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	images := make([]string, 0)
	seenImages := make(map[string]bool)
	clone.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := hospitalResourceURL(baseURL, hospitalImageSource(image))
		if resolved == "" || seenImages[resolved] {
			image.Remove()
			return
		}
		seenImages[resolved] = true
		images = append(images, resolved)
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	clone.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		raw := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if raw == "" || bodaIsPseudoResourceHref(raw) {
			link.RemoveAttr("href")
			return
		}
		resolved := cleanURL(resolveURL(baseURL, raw))
		if resolved == "" {
			link.RemoveAttr("href")
			return
		}
		if hospitalIsAttachmentLink(link, raw) && hospitalResourceURL(baseURL, raw) == "" && hospitalSameHost(resolved, baseURL) {
			// Same-site download/CAPTCHA/placeholder links are not ordinary
			// links. Keep their label, but do not expose the bad URL.
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	htmlValue, _ := clone.Html()
	return extractBodaContentText(clone), htmlValue, images
}

func hospitalImageSource(image *goquery.Selection) string {
	if image == nil || image.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
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
		resolved := hospitalResourceURL(baseURL, href)
		if resolved == "" || seen[resolved] {
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

func hospitalResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	if !hospitalSameHost(resolved, baseURL) {
		return ""
	}
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"/captcha", "captcha", "verifycode", "verification", "createimage", "getcode",
		"placeholder", "transparent", "spacer", "blank", "loading", "pixel.gif",
		"access-denied", "access_denied", "waf", "login",
	} {
		if strings.Contains(resourceText, marker) {
			return ""
		}
	}
	parsed.Fragment = ""
	return parsed.String()
}

func hospitalFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := hospitalBaseURL()
	fullURL, ok := hospitalArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("hospital: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("hospital: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_hospitalArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("hospital: parse article %s: %w", fullURL, err)
	}
	article.Images = hospitalVerifyImages(article.Images, baseURL, fullURL)
	article.Attachments = hospitalVerifyAttachments(article.Attachments, baseURL, fullURL)
	article.ContentHTML = hospitalSanitizeContentHTML(article.ContentHTML, baseURL, article.Images, article.Attachments)
	article.Content = hospitalContentTextFromHTML(article.ContentHTML)
	return article, body, nil
}

func hospitalVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved := hospitalResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := hospitalFetchResource(resolved, baseURL, referer, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func hospitalVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := hospitalResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := hospitalFetchResource(resolved, baseURL, referer, false)
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

func hospitalFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	resolved := hospitalResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("hospital: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("hospital: create resource request: %w", err)
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
		if len(via) == 0 || hospitalSameHost(next.URL.String(), baseURL) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("hospital: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("hospital: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, hospitalMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("hospital: read resource %s: %w", resolved, err)
	}
	if len(body) > hospitalMaxResourceBytes || !hospitalResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("hospital: resource %s returned invalid Content-Type, shell, magic, or size", resolved)
	}
	return body, contentType, nil
}

func hospitalResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	probe := body
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if hospitalResourceShellRE.Match(probe) {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	magic := hospitalResourceMagic(body)
	if image {
		expected := map[string]string{
			"image/jpeg": "jpeg", "image/jpg": "jpeg", "image/png": "png",
			"image/gif": "gif", "image/webp": "webp", "image/bmp": "bmp",
			"image/tiff": "tiff", "image/svg+xml": "svg",
		}[mediaType]
		return strings.HasPrefix(mediaType, "image/") && magic != "" && (expected == "" || magic == expected)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return hospitalBinaryContentTypeOK(mediaType, "pdf") && magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return hospitalBinaryContentTypeOK(mediaType, "office-zip") && magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return hospitalBinaryContentTypeOK(mediaType, "office-ole") && magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return hospitalBinaryContentTypeOK(mediaType, "archive") && (magic == "zip" || magic == "rar" || magic == "7z")
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func hospitalBinaryContentTypeOK(mediaType, kind string) bool {
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
	default:
		return false
	}
}

func hospitalResourceMagic(body []byte) string {
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
	trimmed := bytes.TrimSpace(bytes.ToLower(body))
	if bytes.HasPrefix(trimmed, []byte("<svg")) || bytes.HasPrefix(trimmed, []byte("<?xml")) && bytes.Contains(trimmed, []byte("<svg")) {
		return "svg"
	}
	return ""
}

func hospitalSanitizeContentHTML(raw, baseURL string, images []string, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	validImages := make(map[string]bool, len(images))
	for _, image := range images {
		if resolved := hospitalResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		if resolved := hospitalResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"hospital-content\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	root := doc.Find("#hospital-content").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := hospitalResourceURL(baseURL, hospitalImageSource(image))
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		rawHref := strings.TrimSpace(link.AttrOr("href", ""))
		if rawHref == "" || bodaIsPseudoResourceHref(rawHref) {
			link.RemoveAttr("href")
			return
		}
		resolved := cleanURL(resolveURL(baseURL, rawHref))
		if hospitalIsAttachmentLink(link, rawHref) && hospitalSameHost(resolved, baseURL) && !validAttachments[resolved] {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	htmlValue, _ := root.Html()
	return htmlValue
}

func hospitalContentTextFromHTML(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"hospital-content\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	return extractBodaContentText(doc.Find("#hospital-content").First())
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
