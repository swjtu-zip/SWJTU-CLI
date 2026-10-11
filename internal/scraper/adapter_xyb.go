package scraper

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const (
	xybCanonicalBaseURL = "https://xyb.swjtu.edu.cn"
	xybMaxResourceBytes = 32 << 20
)

// These are the article-bearing lists confirmed from the xyb navigation and
// live list pages. qnxyh is a real page but its list is empty, while the other
// xyzh pages are static/service pages or external applications.
var xybSubPages = []string{
	"xwdt1",
	"xwdt1/djgz",
	"xwdt1/tzgg",
	"xwdt1/xwzx",
	"xwdt1/xwzx/jj",
	"xwdt1/xwzx/xy",
	"xyzh/xyfc",
}

var xybFeedTypes = map[string]string{
	"xwdt1":         "新闻动态",
	"xwdt1/djgz":    "党建工作",
	"xwdt1/tzgg":    "通知公告",
	"xwdt1/xwzx":    "新闻资讯",
	"xwdt1/xwzx/jj": "基金",
	"xwdt1/xwzx/xy": "校友",
	"xyzh/xyfc":     "校友风采",
}

var (
	xybArticlePathRE   = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	xybPageNumberRE    = regexp.MustCompile(`(?:^|/)(\d+)\.html?$`)
	xybPageCounterRE   = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)
	xybResourceShellRE = regexp.MustCompile(`(?is)(?:<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|captcha|验证码|人机验证|安全验证|cloudflare|just a moment|access denied|访问过于频繁|请求已被阻断|页面不存在|404\s+not\s+found)`)
)

func init() {
	registerAdapter("xyb", SiteAdapter{
		SubPages:     xybSubPages,
		ParseList:    parseAdapter_xybList,
		ParseArticle: parseAdapter_xybArticle,
		PageURL:      xybPageURL,
		TotalPages:   xybTotalPages,
		FetchArticle: xybFetchArticle,
	})
}

func xybPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	return reverseBodaPageURL(baseURL, slug, page, totalPages, extension)
}

func parseAdapter_xybList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xyb: parse list HTML: %w", err)
	}
	if xybBlockedPage(raw, doc) {
		return nil, fmt.Errorf("xyb: list response is an error or verification page")
	}
	if xybIsHomepage(doc) {
		items := xybHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("xyb: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := xybListContainer(doc)
	if container.Length() == 0 {
		return nil, fmt.Errorf("xyb: response has no list_con container")
	}
	items := xybParseRows(container, baseURL, xybDocumentType(doc))
	if len(items) == 0 {
		return nil, fmt.Errorf("xyb: article list is empty or contains no local article rows")
	}
	return items, nil
}

func xybBlockedPage(raw []byte, doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	if doc.Find(".prompt_down, .error-page, .error-container, #captcha, .challenge-form").Length() > 0 {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	if xybResourceShellRE.MatchString(title) {
		return true
	}
	visible := doc.Find("body").Clone()
	visible.Find("script, style, noscript").Remove()
	text := strings.ToLower(strings.Join(strings.Fields(visible.Text()), " "))
	for _, marker := range []string{
		"cf-chl-", "challenge-platform", "turnstile", "captcha", "验证码", "人机验证", "安全验证",
		"cloudflare", "just a moment", "access denied", "访问过于频繁", "请求已被阻断",
		"404 not found", "页面不存在", "网页不存在", "error 404",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	if len(raw) == 0 {
		return true
	}
	return false
}

func xybIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", "")) == "1" ||
		(doc.Find(".main1 .content .con").Length() > 0 && doc.Find(".list_con").Length() == 0)
}

func xybHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".main1 .content .con").Each(func(index int, section *goquery.Selection) {
		typ := xybHomepageSectionType(section)
		if typ == "" {
			typ = []string{"新闻资讯", "党建工作", "通知公告"}[minInt(index, 2)]
		}
		section.Find("li.contentli").Each(func(_ int, row *goquery.Selection) {
			anchor := xybFirstArticleAnchor(row, baseURL)
			xybAppendItem(&items, seen, anchor, row, typ, baseURL)
		})
	})
	// The home page has separate “校友活动” and “校友风采” cards. They are
	// scoped by their own heading and are not allowed to pull in service,
	// donation, navigation, or external application links.
	doc.Find(".xyhd").Each(func(_ int, section *goquery.Selection) {
		typ := xybHomepageSectionType(section)
		if typ == "" {
			return
		}
		section.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			if !xybArticleURL(cleanURL(resolveURL(baseURL, anchor.AttrOr("href", ""))), baseURL) {
				return
			}
			row := anchor.Closest("div[style*='margin-bottom'], .xyhd")
			xybAppendItem(&items, seen, anchor, row, typ, baseURL)
		})
	})
	return items
}

func xybHomepageSectionType(section *goquery.Selection) string {
	if section == nil || section.Length() == 0 {
		return ""
	}
	heading := strings.Join(strings.Fields(section.Find(".sjt24, .title .item.active, .title, h2, h3").First().Text()), " ")
	switch heading {
	case "校友活动":
		return "校友"
	case "校友风采":
		return "校友风采"
	default:
		return xybFeedTypeByName(heading)
	}
}

func xybListContainer(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	// 校友风采 now renders a card grid (div.tpfylist) instead of .list_con;
	// the adjacent ul.list_consss is only the 推荐资讯 sidebar.
	for _, selector := range []string{".n_right .tpfylist", ".tpfylist", ".list_con"} {
		if container := doc.Find(selector).First(); container.Length() > 0 {
			return container
		}
	}
	return &goquery.Selection{}
}

func xybDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{
		`meta[name="pageTitle"], meta[name="pagetitle"]`,
		".n_nav a.cur", ".n_tit a:last-child", ".list-title", ".position h1", ".title h2", ".title h3",
	} {
		selection := doc.Find(selector).First()
		value := selection.Text()
		if strings.HasPrefix(selector, "meta[") {
			value = selection.AttrOr("content", "")
		}
		if typ := xybFeedTypeByName(value); typ != "" {
			return typ
		}
	}
	return ""
}

func xybFeedTypeByName(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	for _, typ := range xybFeedTypes {
		if value == typ {
			return typ
		}
	}
	return ""
}

func xybParseRows(container *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	if container.HasClass("tpfylist") {
		// 校友风采 cards are bare divs; each card links its picture and its
		// headline to the same article, and the shared seen set dedups them.
		container.Children().Each(func(_ int, row *goquery.Selection) {
			xybAppendItem(&items, seen, xybFirstArticleAnchor(row, baseURL), row, typ, baseURL)
		})
		return items
	}
	container.Find("li").Each(func(_ int, row *goquery.Selection) {
		xybAppendItem(&items, seen, xybFirstArticleAnchor(row, baseURL), row, typ, baseURL)
	})
	return items
}

func xybFirstArticleAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	if row == nil || row.Length() == 0 {
		return nil
	}
	return row.Find("a[href]").FilterFunction(func(_ int, selection *goquery.Selection) bool {
		return xybArticleURL(cleanURL(resolveURL(baseURL, html.UnescapeString(selection.AttrOr("href", "")))), baseURL)
	}).First()
}

func xybAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := html.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if href == "" || !xybArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".location1, .infoText, .text, .title, em, h3, h4").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(html.UnescapeString(title))
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := xybDateValuesFromRow(row, title)
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        typ,
		ParsedAt:    time.Now(),
	})
}

func xybDateValuesFromRow(row *goquery.Selection, title string) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	for _, selector := range []string{".date", ".time", "time", ".locationsj"} {
		if value := strings.Join(strings.Fields(row.Find(selector).First().Text()), " "); value != "" {
			if date, publishedAt := xybDateText(value); date != "" {
				return date, publishedAt
			}
		}
	}
	// xyb's actual list rows put the date as a text node immediately after
	// the headline. Remove the headline before looking for a date so a year in
	// the title cannot be mistaken for the publication date.
	value := strings.Join(strings.Fields(row.Text()), " ")
	if title != "" {
		value = strings.TrimSpace(strings.Replace(value, title, " ", 1))
	}
	return xybDateText(value)
}

func xybDateValues(value string) (string, string) {
	return xybDateText(value)
}

func xybDateText(value string) (string, string) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if match := dateTimeRe.FindString(value); match != "" {
		if published := normalizePublishedAt(match); len(published) >= 10 {
			return published[:10], published
		}
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func xybTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if xybFeedTypes[slug] == "" {
		return 0, fmt.Errorf("xyb: %q is not an article-list route", slug)
	}
	baseURL := xybSiteBaseURL()
	body, err := fetch(xybPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("xyb: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xyb: parse first page for %s: %w", slug, err)
	}
	if xybBlockedPage(body, doc) || xybListContainer(doc).Length() == 0 {
		return 0, fmt.Errorf("xyb: %s is not an article list page", slug)
	}
	if total := xybPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func xybPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	pageArea := doc.Find(".p_pages, .fanye, .paging, .page, [id^='fanye']")
	text := strings.Join(strings.Fields(pageArea.Text()), " ")
	if match := xybPageCounterRE.FindStringSubmatch(text); len(match) == 2 {
		if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
			return total
		}
	}
	maxPage := 0
	pageArea.Find(".p_no, .p_no_d, a").Each(func(_ int, node *goquery.Selection) {
		for _, value := range []string{strings.TrimSpace(node.Text()), strings.TrimSpace(node.AttrOr("href", ""))} {
			if page, err := strconv.Atoi(value); err == nil && page > maxPage {
				maxPage = page
				continue
			}
			if match := xybPageNumberRE.FindStringSubmatch(value); len(match) == 2 {
				if page, err := strconv.Atoi(match[1]); err == nil && page > maxPage {
					maxPage = page
				}
			}
		}
	})
	return maxPage
}

func parseAdapter_xybArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xyb: parse article HTML: %w", err)
	}
	if xybBlockedPage(raw, doc) {
		return nil, fmt.Errorf("xyb: article is an error or verification page")
	}
	content := xybArticleContent(doc)
	if content.Length() == 0 {
		return nil, fmt.Errorf("xyb: response has no article body")
	}
	title := xybArticleTitle(doc)
	if title == "" {
		return nil, fmt.Errorf("xyb: response has no article title")
	}
	article := &model.Article{
		Title:  title,
		Type:   xybArticleType(doc, title),
		Source: xybArticleSource(doc),
	}
	article.Date, article.PublishedAt = xybArticleDate(doc)
	xybApplyContent(article, content, baseURL)
	xybAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func xybArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{
		".n_right .news-title", ".n_right .article-title", ".n_right .detail-title",
		".article .news-title", ".article h1", ".artical h4", ".artical h1",
		".article-title", ".detail-title", ".news-title", ".infoContent > .title",
	} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return cleanTitle(value)
		}
	}
	if strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", "")) == "3" {
		return cleanTitle(strings.TrimSpace(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).First().AttrOr("content", "")))
	}
	return ""
}

func xybArticleType(doc *goquery.Document, title string) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".n_tit a", ".breadcrumb a", ".current a", ".n_nav a.cur", ".list-title", ".position h1"} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(selection.Eq(i).Text())
			if typ := xybFeedTypeByName(candidate); typ != "" && typ != title {
				return typ
			}
		}
	}
	return ""
}

func xybArticleDate(doc *goquery.Document) (string, string) {
	if doc == nil {
		return "", ""
	}
	for _, selector := range []string{".n_right .news_source", ".news_source", ".article .source", ".artical .pubinfo", ".pubinfo", ".newsAttribute"} {
		field := doc.Find(selector).First()
		if field.Length() == 0 {
			continue
		}
		day := strings.Join(strings.Fields(field.Find(".day").First().Text()), " ")
		clock := strings.Join(strings.Fields(field.Find(".time").First().Text()), " ")
		if date, publishedAt := xybDateText(strings.TrimSpace(day + " " + clock)); date != "" {
			return date, publishedAt
		}
		if date, publishedAt := xybDateText(field.Text()); date != "" {
			return date, publishedAt
		}
	}
	return "", ""
}

func xybArticleSource(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".n_right .news_source", ".news_source", ".article .source", ".artical .pubinfo", ".pubinfo", ".newsAttribute"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if value == "" {
			continue
		}
		for _, prefix := range []string{"来源：", "来源:", "信息来源：", "信息来源:"} {
			value = strings.Replace(value, prefix, " ", 1)
		}
		value = regexp.MustCompile(`(?:日期|发布时间|发布日期)\s*[:：]?\s*20\d{2}.*$`).ReplaceAllString(value, "")
		return strings.TrimSpace(value)
	}
	return ""
}

func xybArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	for _, selector := range []string{
		"#vsb_content_501 .v_news_content", "#vsb_content .v_news_content",
		".n_right .v_news_content", ".article .v_news_content", ".artical .v_news_content",
		".v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return nil
}

func xybApplyContent(article *model.Article, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}
	article.Content = extractBodaContentText(content)
	clone := content.Clone()
	seenImages := make(map[string]bool)
	clone.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := xybImageURL(image, baseURL)
		if resolved == "" {
			image.Remove()
			return
		}
		if !seenImages[resolved] {
			seenImages[resolved] = true
			article.Images = append(article.Images, resolved)
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	seenAttachments := make(map[string]bool)
	clone.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(href, "#") {
				link.RemoveAttr("href")
				link.RemoveAttr("download")
			}
			return
		}
		if href == "" || !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := xybResourceURL(baseURL, href)
		if resolved == "" {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		link.SetAttr("href", resolved)
		name := xybAttachmentName(link, resolved)
		if name == "" || seenAttachments[resolved] {
			return
		}
		seenAttachments[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
	if htmlValue, err := clone.Html(); err == nil {
		article.ContentHTML = htmlValue
	}
}

func xybAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) {
			return
		}
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		resolved := xybResourceURL(baseURL, href)
		if href == "" || resolved == "" || xybArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
			return
		}
		name := xybAttachmentName(link, resolved)
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func xybAttachmentName(link *goquery.Selection, resolved string) string {
	if link != nil && link.Length() > 0 {
		for _, value := range []string{link.AttrOr("download", ""), link.AttrOr("name", ""), link.AttrOr("title", ""), link.Text()} {
			value = strings.Join(strings.Fields(value), " ")
			if value != "" && value != "下载" && value != "点击下载" && value != "附件" {
				return value
			}
		}
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return ""
	}
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return html.UnescapeString(parts[len(parts)-1])
}

func xybImageURL(image *goquery.Selection, baseURL string) string {
	if image == nil || image.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
		if value := xybResourceURL(baseURL, image.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func xybResourceURL(baseURL, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" || (strings.ToLower(parsed.Scheme) != "http" && strings.ToLower(parsed.Scheme) != "https") {
		return ""
	}
	lowerPath := strings.ToLower(parsed.Path)
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif", "/loading.gif"} {
		if strings.HasSuffix(lowerPath, placeholder) {
			return ""
		}
	}
	if strings.Contains(lowerPath, "captcha") || strings.Contains(strings.ToLower(parsed.RawQuery), "captcha") {
		return ""
	}
	return resolved
}

func xybArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && xybArticlePathRE.MatchString(parsed.Path) &&
		strings.EqualFold(parsed.Scheme, base.Scheme) && strings.EqualFold(parsed.Host, base.Host)
}

func xybSiteBaseURL() string {
	if site := AllSites["xyb"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return xybCanonicalBaseURL
}

func xybFetchArticle(articleURL string) (*model.Article, []byte, error) {
	return xybFetchArticleAt(articleURL, xybSiteBaseURL())
}

func xybFetchArticleAt(articleURL, baseURL string) (*model.Article, []byte, error) {
	fullURL := cleanURL(resolveURL(baseURL, html.UnescapeString(strings.TrimSpace(articleURL))))
	if !xybArticleURL(fullURL, baseURL) {
		return nil, nil, fmt.Errorf("xyb: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("xyb: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_xybArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("xyb: parse article %s: %w", fullURL, err)
	}
	article.Images = xybVerifyImages(article.Images, fullURL, baseURL)
	article.Attachments = xybVerifyAttachments(article.Attachments, fullURL, baseURL)
	xybSanitizeContentHTML(article, baseURL)
	return article, body, nil
}

func xybVerifyImages(images []string, referer, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved := xybResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := xybFetchResource(resolved, baseURL, referer, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func xybVerifyAttachments(attachments []model.Attachment, referer, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := xybResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := xybFetchResource(resolved, baseURL, referer, false)
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

func xybFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	resolved := xybResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("xyb: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("xyb: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0 Safari/537.36")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := http.DefaultClient
	if defaultClient != nil {
		copyClient := *defaultClient
		copyClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			if len(via) == 0 || strings.EqualFold(next.URL.Host, via[0].URL.Host) {
				return nil
			}
			return http.ErrUseLastResponse
		}
		client = &copyClient
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("xyb: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("xyb: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, xybMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("xyb: read resource %s: %w", resolved, err)
	}
	if len(body) > xybMaxResourceBytes || !xybResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("xyb: resource %s returned invalid type, shell, signature, or size", resolved)
	}
	return body, contentType, nil
}

func xybResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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
	if xybResourceShellRE.Match(probe) {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	magic := xybResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && xybIsImageMagic(magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.Contains(lowerName, ".tsv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values"
	case strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func xybIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func xybResourceMagic(body []byte) string {
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
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 {
		probe := trimmed
		if len(probe) > 8192 {
			probe = probe[:8192]
		}
		lower := strings.ToLower(string(probe))
		if strings.HasPrefix(lower, "<svg") || (strings.HasPrefix(lower, "<?xml") && strings.Contains(lower, "<svg")) {
			return "svg"
		}
	}
	return ""
}

func xybSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		validImages[image] = true
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		validAttachments[attachment.URL] = true
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"xyb-content-root\">" + article.ContentHTML + "</div>"))
	if err != nil {
		article.ContentHTML = ""
		return
	}
	root := doc.Find("#xyb-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := xybImageURL(image, baseURL)
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
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(href, "#") {
				link.RemoveAttr("href")
				link.RemoveAttr("download")
			}
			return
		}
		if href == "" || !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := xybResourceURL(baseURL, href)
		if resolved == "" || !validAttachments[resolved] {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		link.SetAttr("href", resolved)
	})
	if value, err := root.Html(); err == nil {
		article.ContentHTML = value
	}
}
