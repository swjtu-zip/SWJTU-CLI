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
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const fdcBaseURL = "https://fdc.swjtu.edu.cn"

// These are the article-list routes exposed by the live navigation.  The
// bmjj pages and sdjd/jbxz are static information pages, while the homepage,
// carousel, 专题专栏 and 相关链接 are not list feeds.
var fdcSubPages = []string{
	"tzgg",
	"xwdt",
	"llxx",
	"sdby",
	"sdby/rybz",
	"sdby/yrgs",
	"gzzd",
	"gzzd/sjwj",
	"gzzd/xxwj",
	"sdjd",
	"sdjd/dxal",
}

var (
	fdcArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	fdcPageCounterRE = regexp.MustCompile(`共\s*([0-9]+)\s*条\s*([0-9]+)\s*/\s*([0-9]+)`)
	fdcMetaLabelRE   = regexp.MustCompile(`(信息来源|来源|作者|编辑|责编|摄影|日期|发布时间|文章类型|信息类型|类型|类别|分类|点击数)\s*[:：]\s*`)
	fdcAttachmentRE  = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf|jpe?g|png|gif)(?:$|[?#])`)
)

// fdcAdapter keeps this site's homepage modules, list shell, reverse static
// pagination and article shell validation independent from other 博达 sites.
func init() {
	if site := AllSites["fdc"]; site != nil {
		site.BaseURL = fdcBaseURL
	}
	registerAdapter("fdc", SiteAdapter{
		SubPages:     fdcSubPages,
		ParseList:    parseAdapter_fdcList,
		ParseArticle: parseAdapter_fdcArticle,
		PageURL:      fdcPageURL,
		TotalPages:   fdcTotalPages,
	})
}

// fdcPageURL maps logical pages (1 = newest) to FDC's generated files:
// slug.htm, slug/{total-1}.htm, ..., slug/1.htm.
func fdcPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		page = 1
	}
	if slug == "" {
		slug = "index"
	}
	if page <= 1 || totalPages <= 1 {
		return baseURL + "/" + slug + extension
	}
	generatedPage := totalPages - page + 1
	if generatedPage < 1 {
		generatedPage = 1
	}
	return baseURL + "/" + slug + "/" + strconv.Itoa(generatedPage) + extension
}

func parseAdapter_fdcList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse fdc list: %w", err)
	}
	if fdcLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("fdc list is an error or verification page")
	}

	if fdcIsHomepage(doc) {
		items := parseFdcHomepage(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("fdc homepage contains no article entries")
		}
		return items, nil
	}

	content := fdcListContent(doc)
	rows := content.Find("dl")
	_, _, _, hasPagination := fdcPaginationFromDocument(doc)
	if content.Length() == 0 || (rows.Length() == 0 && !hasPagination) {
		return nil, fmt.Errorf("fdc page is not a supported article list")
	}
	if rows.Length() == 0 {
		// sdby.htm and sdby/yrgs.htm currently expose a real 0/0 list shell.
		// Preserve that as a legal empty feed, not as a static-page parse.
		return []model.NewsItem{}, nil
	}

	items := parseFdcRows(rows, baseURL, fdcListType(doc))
	if len(items) == 0 {
		// A recognized list can contain only external entries (sdby/rybz does
		// so today).  External links are intentionally not site feed items.
		return []model.NewsItem{}, nil
	}
	return items, nil
}

func parseFdcRows(rows *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("h4 a[href]").First()
		if anchor.Length() == 0 {
			anchor = row.Find("a[href]").First()
		}
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := fdcArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}

		title := anchor.AttrOr("title", "")
		if strings.TrimSpace(title) == "" {
			title = anchor.Text()
		}
		title = cleanTitle(strings.TrimSpace(title))
		if !fdcIsArticleTitle(title) {
			return
		}

		date, publishedAt := fdcDateValues(row.Find("em, time, .date, .time").First().Text())
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	})
	return items
}

// parseFdcHomepage scopes extraction to the three .inform/news-list panels.
// It deliberately does not inspect the carousel, mobile navigation, empty
// 专题专栏 panel or external 相关链接 panel.
func parseFdcHomepage(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	sections := doc.Find("main.index-main .inform")
	if sections.Length() == 0 {
		sections = doc.Find(".index-main .inform")
	}
	sections.Each(func(_ int, section *goquery.Selection) {
		typ := strings.Join(strings.Fields(section.Find(".caption-title").First().Text()), " ")
		if typ == "" {
			return
		}
		section.Find("ul.news-list > li").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			fullURL, ok := fdcArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok || seen[fullURL] {
				return
			}
			title := anchor.AttrOr("title", "")
			if strings.TrimSpace(title) == "" {
				title = row.Find(".content").First().Text()
			}
			if strings.TrimSpace(title) == "" {
				title = anchor.Text()
			}
			title = cleanTitle(strings.TrimSpace(title))
			if !fdcIsArticleTitle(title) {
				return
			}
			date, publishedAt := fdcDateValues(row.Find("time, em, .date").First().Text())
			seen[fullURL] = true
			items = append(items, model.NewsItem{
				Title:       title,
				URL:         fullURL,
				Date:        date,
				PublishedAt: publishedAt,
				Type:        typ,
				ParsedAt:    time.Now(),
			})
		})
	})
	return items
}

func parseAdapter_fdcArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse fdc article: %w", err)
	}
	if fdcLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("fdc article is an error or verification page")
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("fdc page is not an article (pageType %q)", pageType)
	}

	scope := doc.Find(".main_right .content .article").First()
	if scope.Length() == 0 {
		scope = doc.Find(".main_right .article, .article").First()
	}
	if scope.Length() == 0 {
		return nil, fmt.Errorf("fdc article has no article container")
	}
	content := scope.Find(".v_news_content").First()
	if content.Length() == 0 {
		content = scope.Find("[id^='vsb_content']").Find(".v_news_content").First()
	}
	if content.Length() == 0 {
		return nil, fmt.Errorf("fdc article has no content container")
	}
	if strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a").Length() == 0 {
		return nil, fmt.Errorf("fdc article has empty content")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = strings.Join(strings.Fields(scope.Find("h1").First().Text()), " ")
	if article.Title == "" {
		return nil, fmt.Errorf("fdc article has no title")
	}
	if typ := fdcCurrentType(doc); typ != "" {
		article.Type = typ
	}

	if source := scope.Find(".source, .news_source").First(); source.Length() > 0 {
		fields := fdcMetaFields(flattenedText(source))
		article.Source = fields["来源"]
		if article.Source == "" {
			article.Source = fields["信息来源"]
		}
		article.Author = fields["作者"]
		article.Editor = fields["编辑"]
		if article.Editor == "" {
			article.Editor = fields["责编"]
		}
		article.Photographer = fields["摄影"]
		dateValue := fields["日期"]
		if dateValue == "" {
			dateValue = fields["发布时间"]
		}
		if date, publishedAt := fdcDateValues(dateValue); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		}
	}

	// FDC's CMS puts its download.jsp list after .v_news_content.  The
	// shared parser intentionally scopes body resources more narrowly, so
	// append only explicit file/download links inside this article shell.
	appendFdcAttachments(article, scope, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func fdcIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	return pageType == "1" || doc.Find("main.index-main, .index-main, .news-swiper").Length() > 0
}

func fdcListContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	return doc.Find(".main_right .content").First()
}

func fdcListType(doc *goquery.Document) string {
	return fdcCurrentType(doc)
}

func fdcCurrentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	typ := strings.Join(strings.Fields(doc.Find(".main_right .current h3").First().Text()), " ")
	if typ == "首页" || typ == "正文" {
		return ""
	}
	return typ
}

func fdcIsArticleTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return false
	}
	for _, label := range []string{"首页", "更多", "MORE +", "通知公告", "新闻动态", "理论学习", "师德榜样", "规章制度", "师德监督", "查看详情"} {
		if title == label {
			return false
		}
	}
	return true
}

func fdcArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !fdcArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	return fullURL, true
}

func fdcDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(raw), " ")
	if raw == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(raw); len(normalized) >= 10 {
		date := normalized[:10]
		if publishedAtHasClock(normalized) {
			return date, normalized
		}
		return date, ""
	}
	return "", ""
}

func fdcMetaFields(text string) map[string]string {
	text = strings.Join(strings.Fields(text), " ")
	fields := make(map[string]string)
	matches := fdcMetaLabelRE.FindAllStringSubmatchIndex(text, -1)
	for i, match := range matches {
		if len(match) < 4 {
			continue
		}
		label := text[match[2]:match[3]]
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		fields[label] = strings.TrimSpace(text[match[1]:end])
	}
	return fields
}

func appendFdcAttachments(article *model.Article, scope *goquery.Selection, baseURL string) {
	if article == nil || scope == nil || scope.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if !fdcLooksLikeAttachmentLink(anchor, href) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		parsed, err := url.Parse(resolved)
		base, baseErr := url.Parse(baseURL)
		if err != nil || baseErr != nil || parsed.Host == "" || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
			return
		}
		if seen[resolved] {
			return
		}
		name := fdcAttachmentName(anchor, resolved)
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func fdcLooksLikeAttachmentLink(anchor *goquery.Selection, href string) bool {
	if anchor == nil || anchor.Length() == 0 || href == "" {
		return false
	}
	lowerHref := strings.ToLower(href)
	if strings.Contains(lowerHref, "download.jsp") || strings.Contains(lowerHref, "downloadattachurl") {
		return true
	}
	if _, ok := anchor.Attr("download"); ok {
		return true
	}
	if !fdcAttachmentRE.MatchString(href) {
		return false
	}
	name := strings.Join(strings.Fields(htmlstd.UnescapeString(anchor.Text())), " ")
	if strings.HasSuffix(strings.ToLower(href), ".jpg") || strings.HasSuffix(strings.ToLower(href), ".jpeg") || strings.HasSuffix(strings.ToLower(href), ".png") || strings.HasSuffix(strings.ToLower(href), ".gif") {
		if name == "" && anchor.Find("img").Length() > 0 {
			return false
		}
		return strings.Contains(name, "附件") || fdcAttachmentRE.MatchString(name)
	}
	return true
}

func fdcAttachmentName(anchor *goquery.Selection, resolved string) string {
	for _, attribute := range []string{"download", "data-filename", "data-name", "name"} {
		if value := strings.TrimSpace(anchor.AttrOr(attribute, "")); value != "" {
			return strings.Join(strings.Fields(htmlstd.UnescapeString(value)), " ")
		}
	}
	if value := strings.Join(strings.Fields(htmlstd.UnescapeString(anchor.Text())), " "); value != "" {
		return value
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return ""
	}
	name := path.Base(parsed.Path)
	if name == "." || name == "/" || name == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	return name
}

func fdcPaginationFromDocument(doc *goquery.Document) (totalItems, currentPage, totalPages int, ok bool) {
	if doc == nil {
		return 0, 0, 0, false
	}
	values := []string{doc.Find("[id^='fanye']").Text()}
	if values[0] == "" {
		values = append(values, doc.Find(".main_right").Text())
	}
	for _, value := range values {
		if match := fdcPageCounterRE.FindStringSubmatch(strings.Join(strings.Fields(value), " ")); match != nil {
			totalItems, _ = strconv.Atoi(match[1])
			currentPage, _ = strconv.Atoi(match[2])
			totalPages, _ = strconv.Atoi(match[3])
			return totalItems, currentPage, totalPages, true
		}
	}
	return 0, 0, 0, false
}

func fdcTotalPagesFromHTML(html []byte) int {
	_, _, totalPages, ok := fdcPaginationFromHTML(html)
	if !ok {
		return 0
	}
	return totalPages
}

func fdcPaginationFromHTML(html []byte) (totalItems, currentPage, totalPages int, ok bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0, 0, 0, false
	}
	return fdcPaginationFromDocument(doc)
}

func fdcTotalPages(slug string) (int, error) {
	if strings.EqualFold(strings.Trim(slug, "/"), "index") {
		return 1, nil
	}
	baseURL := fdcBaseURL
	if site := AllSites["fdc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	firstURL := fdcPageURL(baseURL, slug, 1, 0, ".htm")
	body, err := fdcFetchListPage(firstURL)
	if err != nil {
		return 0, fmt.Errorf("fdc: read first page for %s: %w", slug, err)
	}
	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if parseErr != nil || fdcLooksLikeErrorPage(doc) {
		return 0, fmt.Errorf("fdc: %s is not a valid list page", slug)
	}
	content := fdcListContent(doc)
	rows := content.Find("dl")
	if content.Length() == 0 || (rows.Length() == 0 && !fdcPaginationIsPresent(doc)) {
		return 0, fmt.Errorf("fdc: %s is not a supported article list", slug)
	}
	if totalPages := fdcTotalPagesFromHTML(body); totalPages > 0 {
		return totalPages, nil
	}
	if fdcPaginationIsPresent(doc) {
		// 0/0 is the site's explicit legal empty-list marker.
		return 0, nil
	}
	return 1, nil
}

func fdcPaginationIsPresent(doc *goquery.Document) bool {
	_, _, _, ok := fdcPaginationFromDocument(doc)
	return ok
}

func fdcFetchListPage(pageURL string) ([]byte, error) {
	body, err := fetch(pageURL)
	if err == nil {
		return body, nil
	}
	if strings.HasSuffix(strings.ToLower(pageURL), ".htm") {
		if fallback, fallbackErr := fetch(strings.TrimSuffix(pageURL, ".htm") + ".html"); fallbackErr == nil {
			return fallback, nil
		}
	}
	return nil, err
}

func fdcLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"404", "403", "access denied", "not found", "页面不存在", "请求错误", "安全验证", "滑块验证"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	body := strings.ToLower(strings.Join(strings.Fields(doc.Find("body").First().Text()), " "))
	for _, marker := range []string{"请输入验证码下载附件", "请输入验证码", "页面不存在", "access denied", "404 not found"} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}
