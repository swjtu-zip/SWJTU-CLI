package scraper

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const clxyCanonicalBaseURL = "https://clxy.swjtu.edu.cn"

// clxySubPages contains the article-list entry points rendered by the live
// home page. The parent routes are real merged archives, while the nested
// routes are the visible/hidden tabs behind their "更多" links.
//
// The top navigation's articleinfo.jsp/articlelist.jsp links currently return
// a permission-error page, and the mobile-only y*.htm aliases return 404s;
// neither is advertised as a feed here.
var clxySubPages = []string{
	"sylm_xwgg/xyxw",
	"sylm_xwgg/xygg",
	"jwgg",
	"jwgg/bks",
	"jwgg/yjs",
	"sylm_djgz",
	"sylm_djgz/tzgg",
	"sylm_djgz/djxw",
	"sylm_xsgz",
	"sylm_xsgz/gsgg",
	"sylm_xsgz/txkx",
	"sylm_xsgz/jyzp",
	"sylm_xshd",
	"sylm_xshd/xshb",
	"sylm_xshd/xsxw",
	"sylm_xshd/kjdt",
}

var (
	clxyArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	clxyDateRe        = regexp.MustCompile(`(20\d{2})\s*(?:年|[-/.])\s*(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*(?:日|号)?`)
	clxyDateTimeRe    = regexp.MustCompile(`(20\d{2})\s*(?:年|[-/.])\s*(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*(?:日|号)?\s+(\d{1,2}):(\d{2})(?::(\d{2}))?`)
)

func init() {
	if site := AllSites["clxy"]; site != nil {
		site.BaseURL = clxyCanonicalBaseURL
	}
	registerAdapter("clxy", SiteAdapter{
		SubPages:     clxySubPages,
		ParseList:    parseAdapter_clxyList,
		ParseArticle: parseAdapter_clxyArticle,
		PageURL:      clxyPageURL,
		FetchArticle: clxyFetchArticle,
	})
}

// clxyPageURL maps the logical page number (1 = newest) to the generated
// filename used by clxy. The unsuffixed page is page 1; the site's link for
// logical page 2 is /<slug>/<total-1>.htm and its last page is /<slug>/1.htm.
func clxyPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
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

func parseAdapter_clxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse clxy list: %w", err)
	}
	if clxyErrorDocument(doc) {
		return nil, fmt.Errorf("clxy: response is an error or permission page")
	}

	// All live archive pages expose their actual rows in this container. It is
	// deliberately checked before any home-page fallback so the mobile copy,
	// sidebars and pagination links cannot become articles.
	if rows := clxyListRows(doc); rows.Length() > 0 {
		items := clxyParseRows(rows, doc, baseURL)
		if external := clxyExternalListRowCount(rows, baseURL); external > 0 {
			return items, fmt.Errorf("clxy: %d list row(s) point outside the site; refusing a partial feed", external)
		}
		return items, nil
	}
	if doc.Find(".conlistComm").Length() > 0 {
		// A real but currently empty list is a valid empty result.
		return []model.NewsItem{}, nil
	}

	// index.htm is an aggregation page. Its desktop content contains all
	// server-rendered tabs, including tabs hidden by CSS until clicked.
	if home := clxyHomeContent(doc); home.Length() > 0 {
		items := clxyParseHome(home, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("clxy: home page contains no article entries")
		}
		return items, nil
	}

	return nil, fmt.Errorf("clxy: response is not a supported article list")
}

func clxyListRows(doc *goquery.Document) *goquery.Selection {
	if rows := doc.Find(".conlistComm > dl"); rows.Length() > 0 {
		return rows
	}
	return doc.Find(`dl[id^="line_"]`)
}

func clxyParseRows(rows *goquery.Selection, doc *goquery.Document, baseURL string) []model.NewsItem {
	defaultType := clxyBreadcrumbType(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())

	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := clxyArticleAnchor(row, baseURL)
		if anchor.Length() == 0 {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL, ok := clxyArticleURL(baseURL, href)
		if !ok || seen[fullURL] {
			return
		}

		title := clxyRowTitle(row, anchor)
		if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := clxyDateValues(row.Find("dd").Text())
		if date == "" {
			date, publishedAt = clxyDateValues(row.Text())
		}
		typ := clxyRowType(row)
		if typ == "" {
			typ = defaultType
		}

		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     typ,
			ParsedAt: time.Now(),
		}
		if publishedAt != "" && publishedAtHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}
		seen[fullURL] = true
		items = append(items, item)
	})
	return items
}

func clxyArticleAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	var found *goquery.Selection
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := clxyArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			found = anchor
			return false
		}
		return true
	})
	if found == nil {
		return row.Find("a[href]").First()
	}
	return found
}

// clxyExternalListRowCount detects real content rows that point to another
// site. Such rows are common in the live aggregates (教务 and employment).
// Silently dropping them would make a partially collected page look complete.
func clxyExternalListRowCount(rows *goquery.Selection, baseURL string) int {
	count := 0
	rows.Each(func(_ int, row *goquery.Selection) {
		local, external := false, false
		row.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			href := strings.TrimSpace(anchor.AttrOr("href", ""))
			if _, ok := clxyArticleURL(baseURL, href); ok {
				local = true
				return
			}
			if clxyExternalListAnchor(anchor, baseURL, href) {
				external = true
			}
		})
		if external && !local {
			count++
		}
	})
	return count
}

func clxyExternalListAnchor(anchor *goquery.Selection, baseURL, href string) bool {
	if anchor == nil || anchor.Length() == 0 || href == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	// The category marker is an ordinary link inside the row, not the row's
	// article target. Ignore it when it happens to be hosted elsewhere.
	if anchor.Closest("dt > span").Length() > 0 {
		return false
	}
	resolved := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || strings.EqualFold(parsed.Host, base.Host) {
		return false
	}
	title := clxyRowTitle(anchor, anchor)
	return title != "" && isValidNewsTitle(title)
}

func clxyRowTitle(row, anchor *goquery.Selection) string {
	if value := strings.TrimSpace(anchor.AttrOr("title", "")); value != "" {
		return cleanTitle(value)
	}
	return cleanTitle(strings.TrimSpace(anchor.Text()))
}

func clxyRowType(row *goquery.Selection) string {
	label := strings.TrimSpace(row.Find("dt > span a").First().Text())
	if label == "" {
		label = strings.TrimSpace(row.Find("dt span a").First().Text())
	}
	return strings.TrimSpace(strings.Trim(label, "[]【】"))
}

func clxyHomeContent(doc *goquery.Document) *goquery.Selection {
	return doc.Find("#pc > .content").First()
}

func clxyParseHome(home *goquery.Selection, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	home.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if clxyHomeDecoration(anchor) {
			return
		}
		fullURL, ok := clxyArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		title := clxyHomeTitle(anchor)
		if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := clxyDateValues(clxyHomeDateText(anchor))
		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     clxyHomeType(anchor),
			ParsedAt: time.Now(),
		}
		if publishedAt != "" && publishedAtHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}

		if index, exists := seen[fullURL]; exists {
			// The same article is often present in the "材料要闻" image block
			// and the ordinary news list. Keep the first occurrence/order, but
			// use later metadata when the first presentation lacks it.
			if items[index].Date == "" && item.Date != "" {
				items[index].Date = item.Date
			}
			if items[index].PublishedAt == "" && item.PublishedAt != "" {
				items[index].PublishedAt = item.PublishedAt
			}
			if items[index].Type == "" && item.Type != "" {
				items[index].Type = item.Type
			}
			if clxyTypeIsBetter(item.Type, items[index].Type) {
				items[index].Type = item.Type
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, item)
	})
	return items
}

func clxyTypeIsBetter(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	current = strings.TrimSpace(current)
	if candidate == "" || candidate == current {
		return false
	}
	if current == "" {
		return true
	}
	return current == "材料要闻"
}

func clxyHomeDecoration(anchor *goquery.Selection) bool {
	if isInNavArea(anchor) {
		return true
	}
	return anchor.Closest(".iosSlider, .slider, .swiper, .carousel, .focus, .hero, .banner, .four-nav, .sidebar, .side, .widget").Length() > 0
}

func clxyHomeTitle(anchor *goquery.Selection) string {
	if value := strings.TrimSpace(anchor.AttrOr("title", "")); value != "" {
		return cleanTitle(value)
	}
	clone := anchor.Clone()
	clone.Find(".date, .time, time, .hot").Remove()
	if value := strings.TrimSpace(clone.Text()); value != "" {
		return cleanTitle(value)
	}
	for _, selector := range []string{"img[title]", "img[alt]"} {
		if value := strings.TrimSpace(anchor.Find(selector).First().AttrOr("title", "")); value != "" {
			return cleanTitle(value)
		}
		if value := strings.TrimSpace(anchor.Find(selector).First().AttrOr("alt", "")); value != "" {
			return cleanTitle(value)
		}
	}
	return ""
}

func clxyHomeDateText(anchor *goquery.Selection) string {
	if value := strings.TrimSpace(anchor.Find(".date, .time, time").First().Text()); value != "" {
		return value
	}
	if row := anchor.Closest("li"); row.Length() > 0 {
		return strings.TrimSpace(row.Find(".date, .time, time").First().Text())
	}
	return ""
}

func clxyHomeType(anchor *goquery.Selection) string {
	if tabs := anchor.Closest(".tabs-content"); tabs.Length() > 0 {
		panel := tabs.Closest(".item-s, .item-long")
		if panel.Length() > 0 {
			index := anchor.Closest("ul[data-index]").AttrOr("data-index", "")
			if n, err := strconv.Atoi(strings.TrimSpace(index)); err == nil && n > 0 {
				if label := strings.TrimSpace(panel.Find(".tabs-choose a").Eq(n - 1).Text()); label != "" {
					return label
				}
			}
		}
	}
	panel := anchor.Closest(".item-s, .item-long")
	if panel.Length() == 0 {
		return ""
	}
	heading := strings.TrimSpace(panel.Find("h3.title").First().Text())
	heading = strings.TrimSpace(strings.TrimSuffix(heading, "更多"))
	return heading
}

func clxyArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !clxyArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	return fullURL, true
}

func clxyDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "", ""
	}
	if match := clxyDateTimeRe.FindString(value); match != "" {
		if precise := normalizePublishedAt(match); precise != "" {
			return precise[:10], precise
		}
	}
	if match := clxyDateRe.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, ""
	}
	return "", ""
}

func clxyBreadcrumbType(doc *goquery.Document) string {
	for _, selector := range []string{".current h3", ".main_left .submenu_cur a", ".current a[href*='.htm']"} {
		if value := strings.TrimSpace(doc.Find(selector).First().Text()); value != "" && value != "正文" {
			return value
		}
	}
	return ""
}

func parseAdapter_clxyArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse clxy article: %w", err)
	}
	if clxyErrorDocument(doc) {
		return nil, fmt.Errorf("clxy: response is an error or permission page")
	}
	if doc.Find(".article h1").First().Length() == 0 || doc.Find(".article .v_news_content").First().Length() == 0 {
		return nil, fmt.Errorf("clxy: response is not an article detail page")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if title := strings.TrimSpace(doc.Find(".article h1").First().Text()); title != "" {
		article.Title = title
	}
	clxyApplyArticleMetadata(article, doc)
	// Clear the generic parser's broad date fallback before reading the two
	// fixed metadata blocks used by this site. Dates in article prose are not
	// publication dates.
	article.Date = ""
	article.PublishedAt = ""
	if date, publishedAt := clxyDateValues(doc.Find(".article .source").First().Text()); date != "" {
		article.Date = date
		if publishedAt != "" {
			article.PublishedAt = publishedAt
		}
	} else if date, publishedAt := clxyDateValues(doc.Find(".meta-info").First().Text()); date != "" {
		article.Date = date
		if publishedAt != "" {
			article.PublishedAt = publishedAt
		}
	}
	if typ := clxyBreadcrumbType(doc); typ != "" {
		article.Type = typ
	}
	appendClxyAttachments(article, doc, baseURL)
	clxyFilterArticleResources(article, baseURL, nil)
	normalizeArticlePublishedAt(article)
	return article, nil
}

var clxyMetaFieldRe = regexp.MustCompile(`(?:来源|作者|审核|编辑|摄影|日期|时间|发布时间|点击数)\s*[：:]`)

func clxyApplyArticleMetadata(article *model.Article, doc *goquery.Document) {
	if article == nil || doc == nil {
		return
	}
	fields := clxyMetaFields(doc.Find(".article .source").First().Text())
	if value := fields["来源"]; value != "" {
		article.Source = value
	}
	if value := fields["作者"]; value != "" {
		article.Author = value
	}
	if value := fields["摄影"]; value != "" {
		article.Photographer = value
	}
	if value := fields["编辑"]; value != "" {
		article.Editor = value
	}
}

func clxyMetaFields(raw string) map[string]string {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	fields := make(map[string]string)
	matches := clxyMetaFieldRe.FindAllStringIndex(raw, -1)
	for i, match := range matches {
		label := strings.TrimRight(strings.TrimSpace(raw[match[0]:match[1]]), "：:")
		end := len(raw)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		value := strings.TrimSpace(raw[match[1]:end])
		value = strings.Trim(value, " |")
		if value != "" {
			fields[label] = value
		}
	}
	return fields
}

func appendClxyAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}

	doc.Find(".content a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" || !clxyAttachmentLink(anchor, href) {
			return
		}
		resolved := resolveURL(baseURL, href)
		if seen[resolved] {
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
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func clxyAttachmentLink(anchor *goquery.Selection, href string) bool {
	if anchor == nil || anchor.Length() == 0 || bodaIsPseudoResourceHref(href) {
		return false
	}
	hrefLower := strings.ToLower(href)
	if strings.Contains(hrefLower, "download.jsp") || strings.Contains(hrefLower, "downloadattachurl") || strings.TrimSpace(anchor.AttrOr("download", "")) != "" {
		return true
	}
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(hrefLower, ext) {
			return true
		}
	}
	for _, selector := range []string{".attachment", ".attachments", ".news-attachment", ".download"} {
		if ancestor := anchor.Closest(selector); ancestor.Length() > 0 && strings.Contains(strings.TrimSpace(ancestor.Text()), "附件") {
			return true
		}
	}
	return false
}

// clxyFilterArticleResources removes pseudo URLs from both normalized fields
// and ContentHTML. The shared parser already filters Images, but its HTML
// serialization intentionally retains an unresolvable src attribute.
func clxyFilterArticleResources(article *model.Article, baseURL string, allowedImages map[string]bool) {
	if article == nil {
		return
	}
	seen := make(map[string]bool, len(article.Images))
	images := make([]string, 0, len(article.Images))
	for _, raw := range article.Images {
		resolved := bodaResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] || (allowedImages != nil && !allowedImages[resolved]) {
			continue
		}
		seen[resolved] = true
		images = append(images, resolved)
	}
	article.Images = images
	article.ContentHTML = clxySanitizeContentHTML(article.ContentHTML, baseURL, allowedImages)
}

func clxySanitizeContentHTML(rawHTML, baseURL string, allowedImages map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="clxy-content-root">` + rawHTML + `</div>`))
	if err != nil {
		return rawHTML
	}
	root := doc.Find("#clxy-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var resolved string
		for _, attr := range []string{"src", "data-src", "data-original", "orisrc", "vurl"} {
			if value := image.AttrOr(attr, ""); value != "" {
				if candidate := bodaResourceURL(baseURL, value); candidate != "" {
					resolved = candidate
					break
				}
			}
		}
		if resolved == "" || (allowedImages != nil && !allowedImages[resolved]) {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "orisrc", "vurl", "vsbhref"} {
			image.RemoveAttr(attr)
		}
	})
	result, err := root.Html()
	if err != nil {
		return rawHTML
	}
	return result
}

func clxyFetchArticle(articleURL string) (*model.Article, []byte, error) {
	canonicalURL, ok := clxyArticleURL(clxyCanonicalBaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("clxy: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonicalURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_clxyArticle(body, clxyCanonicalBaseURL)
	if err != nil {
		return nil, body, err
	}
	article.Images = clxyVerifyImages(article.Images)
	article.Attachments = clxyVerifyAttachments(article.Attachments)
	allowedImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		allowedImages[image] = true
	}
	clxyFilterArticleResources(article, clxyCanonicalBaseURL, allowedImages)
	return article, body, nil
}

func clxyVerifyImages(images []string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := bodaResourceURL(clxyCanonicalBaseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := clxyFetchResource(resolved)
		if err != nil || !clxyResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func clxyVerifyAttachments(attachments []model.Attachment) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := bodaResourceURL(clxyCanonicalBaseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := clxyFetchResource(resolved)
		if err != nil || !clxyResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func clxyFetchResource(rawURL string) ([]byte, string, error) {
	resolved := bodaResourceURL(clxyCanonicalBaseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("clxy: refusing non-http resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("clxy: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/*,application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("clxy: fetch resource %s: %w", resolved, err)
	}
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK {
		return nil, contentType, fmt.Errorf("clxy: resource %s: HTTP %d", resolved, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, contentType, fmt.Errorf("clxy: read resource %s: %w", resolved, err)
	}
	return body, contentType, nil
}

func clxyResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	probe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(body[:minInt(len(body), 4096)]))), "\ufeff")
	for _, marker := range []string{"<!doctype html", "<html", "<head", "<body", "请输入验证码", "验证码下载附件", "captcha", "请先登录", "请登录后", "登录页面", "页面不存在", "access denied", "just a moment", "cloudflare", "请求已被阻断", "安全威胁"} {
		if strings.Contains(probe, marker) {
			return false
		}
	}
	if image {
		return clxyImageBodyIsUsable(mediaType, body)
	}
	return clxyAttachmentBodyIsUsable(mediaType, body, nameOrURL)
}

func clxyImageBodyIsUsable(mediaType string, body []byte) bool {
	switch mediaType {
	case "image/png":
		return len(body) >= 8 && string(body[:8]) == "\x89PNG\r\n\x1a\n"
	case "image/jpeg", "image/jpg":
		return len(body) >= 3 && string(body[:3]) == "\xff\xd8\xff"
	case "image/gif":
		return len(body) >= 6 && (string(body[:6]) == "GIF87a" || string(body[:6]) == "GIF89a")
	case "image/webp":
		return len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP"
	case "image/bmp":
		return len(body) >= 2 && string(body[:2]) == "BM"
	case "image/svg+xml":
		text := strings.TrimSpace(strings.ToLower(string(body[:minInt(len(body), 4096)])))
		return strings.HasPrefix(text, "<svg") || strings.Contains(text, "<svg ")
	default:
		return false
	}
}

func clxyAttachmentBodyIsUsable(mediaType string, body []byte, nameOrURL string) bool {
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf":
		return len(body) >= 5 && string(body[:5]) == "%PDF-"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(lowerName, ".zip") || mediaType == "application/zip":
		return len(body) >= 4 && string(body[:4]) == "PK\x03\x04"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt"):
		return len(body) >= 8 && string(body[:8]) == "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "application/octet-stream"
	default:
		return mediaType != "" && mediaType != "text/html" && mediaType != "application/xhtml+xml" && mediaType != "application/json" && mediaType != "text/xml" && mediaType != "application/xml"
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clxyErrorDocument(doc *goquery.Document) bool {
	title := strings.ToLower(strings.TrimSpace(doc.Find("title").First().Text()))
	if strings.Contains(title, "系统提示") || strings.Contains(title, "404错误") {
		return true
	}
	body := strings.Join(strings.Fields(doc.Find("body").Text()), " ")
	return strings.Contains(body, "您没有访问当前栏目的权限") || strings.Contains(body, "404错误提示") || strings.Contains(body, "栏目不存在")
}
