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
	ygbCanonicalBaseURL = "https://ygb.swjtu.edu.cn"
	ygbMaxResourceBytes = 32 << 20
)

// These are the ten public, non-empty article leaves confirmed from the
// ygb.swjtu.edu.cn navigation.  Empty announcement shells, static pages and
// VPN/external links are intentionally not feeds.
var ygbSubPages = []string{
	"xwzx/xwdt",
	"xwzx/tzgg",
	"xwzx/dcxy",
	"xwzx/hdyg",
	"xwzx/ssfc",
	"djyl1/zbfc",
	"djyl1/xzzg",
	"djyl1/_hl_xj",
	"djyl1/gzdl",
	"bszn/gzzd",
}

var ygbFeedTypes = map[string]string{
	"xwzx/xwdt":    "新闻动态",
	"xwzx/tzgg":    "通知公告",
	"xwzx/dcxy":    "多彩学院",
	"xwzx/hdyg":    "活动预告",
	"xwzx/ssfc":    "师生风采",
	"djyl1/zbfc":   "支部风采",
	"djyl1/xzzg":   "行知中国",
	"djyl1/_hl_xj": "“红路”学讲",
	"djyl1/gzdl":   "挂职锻炼",
	"bszn/gzzd":    "规章制度",
}

var (
	ygbArticlePathRE   = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	ygbPageCounterRE   = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)
	ygbSplitDateRE     = regexp.MustCompile(`(20\d{2})\s+(\d{1,2})\s*[/.-]\s*(\d{1,2})`)
	ygbPageNumberRE    = regexp.MustCompile(`^\d+$`)
	ygbResourceShellRE = regexp.MustCompile(`(?is)(?:<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|captcha|验证码|人机验证|安全验证|cloudflare|just a moment|waf|访问过于频繁|请求已被阻断|页面不存在|404\s+not\s+found)`)
)

func init() {
	registerAdapter("ygb", SiteAdapter{
		SubPages:     ygbSubPages,
		ParseList:    parseAdapter_ygbList,
		ParseArticle: parseAdapter_ygbArticle,
		PageURL:      ygbPageURL,
		TotalPages:   ygbTotalPages,
		FetchArticle: ygbFetchArticle,
	})
}

func ygbPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	return reverseBodaPageURL(baseURL, slug, page, totalPages, extension)
}

func parseAdapter_ygbList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ygb: parse list HTML: %w", err)
	}
	if ygbBlockedPage(raw, doc) {
		return nil, fmt.Errorf("ygb: list response is an error or verification page")
	}
	if ygbIsHomepage(doc) {
		items := ygbHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("ygb: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := doc.Find(".newsList").First()
	if container.Length() == 0 {
		return nil, fmt.Errorf("ygb: response has no newsList container")
	}
	return ygbParseRows(container, baseURL, ygbDocumentType(doc)), nil
}

func ygbIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", "")) == "1" ||
		(doc.Find(".newsBox, .newsBox2, .newsBox3").Length() > 0 && doc.Find(".newsList").Length() == 0)
}

func ygbHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	// Each homepage column owns its heading and more-link.  Keep that scope so
	// a heading from a neighbouring column cannot become an item's type.
	doc.Find(".newsBox, .newsBox2, .newsBox3").Each(func(_ int, box *goquery.Selection) {
		box.Find(".newsNotice, .newsClassify").Each(func(_ int, section *goquery.Selection) {
			typ := ygbHomepageColumnType(section)
			if typ == "" {
				typ = ygbDocumentType(doc)
			}
			section.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
				if !strings.Contains(strings.ToLower(anchor.AttrOr("href", "")), "/info/") &&
					!strings.Contains(strings.ToLower(anchor.AttrOr("href", "")), "info/") {
					return
				}
				row := anchor.Closest("li, .newsImg, .newsInfo, .schoolNews")
				ygbAppendItem(&items, seen, anchor, row, typ, baseURL)
			})
		})
	})
	return items
}

func ygbHomepageColumnType(section *goquery.Selection) string {
	if section == nil || section.Length() == 0 {
		return ""
	}
	href := section.Find("a[href]").FilterFunction(func(_ int, selection *goquery.Selection) bool {
		value := strings.ToLower(selection.AttrOr("href", ""))
		return strings.Contains(value, "xwzx/") || strings.Contains(value, "djyl1/") || strings.Contains(value, "bszn/")
	}).First().AttrOr("href", "")
	route := strings.Trim(strings.SplitN(strings.TrimSpace(href), "?", 2)[0], "/")
	route = strings.TrimSuffix(strings.TrimSuffix(route, ".html"), ".htm")
	if typ := ygbFeedTypes[route]; typ != "" {
		return typ
	}
	for _, selector := range []string{".noticeTitle .title", ".teachingDynamics .title", ".title"} {
		value := strings.Join(strings.Fields(section.Find(selector).First().Text()), " ")
		if typ := ygbTypeCandidate(value, ""); typ != "" {
			return typ
		}
	}
	return ""
}

func ygbDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{
		`meta[name="pageTitle"], meta[name="pagetitle"]`,
		".boxRight > .title .w700",
		".boxRight .top_left",
		".position h1",
		".list-title",
		".newsTitle",
		".title h2",
		".title h3",
	} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			candidate := selection.Eq(i).Text()
			if strings.HasPrefix(selector, "meta[") {
				candidate = selection.Eq(i).AttrOr("content", "")
			}
			if typ := ygbTypeCandidate(candidate, ""); typ != "" {
				return typ
			}
		}
	}
	return ""
}

func ygbTypeCandidate(value, articleTitle string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '>' || r == '›' || r == '»' || r == '|' || r == '/' || r == '＞'
	})
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		candidate = strings.Trim(candidate, " >|›»")
		if candidate == "" || candidate == strings.Join(strings.Fields(articleTitle), " ") {
			continue
		}
		switch candidate {
		case "首页", "正文", "文章详情", "新闻中心", "党建引领", "办事指南", "部门概况", "网站首页":
			continue
		}
		if typ := ygbFeedTypeByName(candidate); typ != "" {
			return typ
		}
		if len([]rune(candidate)) <= 40 {
			return candidate
		}
	}
	return ""
}

func ygbFeedTypeByName(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	for _, typ := range ygbFeedTypes {
		if value == typ {
			return typ
		}
	}
	return ""
}

func ygbParseRows(container *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	rows := container.Find("li")
	if rows.Length() == 0 {
		rows = container.Find("a[href]").Parent()
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").FilterFunction(func(_ int, selection *goquery.Selection) bool {
			return ygbArticleURL(cleanURL(resolveURL(baseURL, selection.AttrOr("href", ""))), baseURL)
		}).First()
		ygbAppendItem(&items, seen, anchor, row, typ, baseURL)
	})
	return items
}

func ygbAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := html.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if href == "" || !ygbArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".newsItem, .infoText, .text, .word, .noticeWord, .title, h3, h4").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := ygbDateValues(row)
	if typ == "" && row != nil && row.Length() > 0 {
		typ = strings.Trim(strings.TrimSpace(row.Find(".tag, .category, .type").First().Text()), "[]【】")
	}
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

func ygbDateValues(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	for _, selector := range []string{".newsDate", ".infoDate", ".noticeDate", ".date", ".time", "time"} {
		if value := strings.Join(strings.Fields(row.Find(selector).First().Text()), " "); value != "" {
			if date, publishedAt := ygbDateText(value); date != "" {
				return date, publishedAt
			}
		}
	}
	return ygbDateText(strings.Join(strings.Fields(row.Text()), " "))
}

func ygbDateText(value string) (string, string) {
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
	if match := ygbSplitDateRE.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func ygbTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if ygbFeedTypes[slug] == "" {
		return 0, fmt.Errorf("ygb: %q is not an article-list route", slug)
	}
	baseURL := ygbSiteBaseURL()
	body, err := fetch(ygbPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("ygb: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("ygb: parse first page for %s: %w", slug, err)
	}
	if ygbBlockedPage(body, doc) || doc.Find(".newsList").Length() == 0 {
		return 0, fmt.Errorf("ygb: %s is not an article list page", slug)
	}
	if total := ygbPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func ygbPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	pageArea := doc.Find(".p_pages, .fanye, .fyt, .paging, .page, [id^='fanye']")
	text := strings.Join(strings.Fields(pageArea.Text()), " ")
	if match := ygbPageCounterRE.FindStringSubmatch(text); match != nil {
		if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
			return total
		}
	}
	maxPage := 0
	pageArea.Find(".p_no, .p_no_d, a").Each(func(_ int, node *goquery.Selection) {
		for _, value := range []string{strings.TrimSpace(node.Text()), strings.TrimSpace(node.AttrOr("href", ""))} {
			if ygbPageNumberRE.MatchString(value) {
				if page, err := strconv.Atoi(value); err == nil && page > maxPage {
					maxPage = page
				}
				continue
			}
			match := regexp.MustCompile(`(?:^|/)(\d+)\.html?$`).FindStringSubmatch(value)
			if len(match) == 2 {
				if page, err := strconv.Atoi(match[1]); err == nil && page > maxPage {
					maxPage = page
				}
			}
		}
	})
	return maxPage
}

func parseAdapter_ygbArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ygb: parse article HTML: %w", err)
	}
	if ygbBlockedPage(raw, doc) {
		return nil, fmt.Errorf("ygb: article is an error or verification page")
	}
	content := ygbArticleContent(doc)
	if content.Length() == 0 {
		return nil, fmt.Errorf("ygb: response has no article body")
	}
	title := ygbArticleTitle(doc)
	if title == "" {
		return nil, fmt.Errorf("ygb: response has no article title")
	}
	typ := ygbArticleType(doc, title)
	if typ == "" {
		return nil, fmt.Errorf("ygb: response has no article type")
	}
	article := &model.Article{
		Title:       title,
		Type:        typ,
		Date:        "",
		PublishedAt: "",
	}
	article.Date, article.PublishedAt = ygbArticleDate(doc)
	article.Source = ygbArticleSource(doc)
	ygbApplyContent(article, content, baseURL)
	ygbAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func ygbArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	for _, selector := range []string{
		".infoContent .v_news_content",
		".article .v_news_content",
		"#vsb_content_501 .v_news_content",
		".v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return nil
}

func ygbArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".infoContent > .title", ".article h1", ".article-title", ".detail-title"} {
		if title := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); title != "" {
			return title
		}
	}
	return ""
}

func ygbArticleType(doc *goquery.Document, title string) string {
	if doc == nil {
		return ""
	}
	// New pages expose the section as the penultimate meaningful breadcrumb
	// item: 首页 > 新闻中心 > 新闻动态 > 正文.
	if typ := ygbTypeCandidate(doc.Find(".boxRight .top_left").First().Text(), title); typ != "" {
		return typ
	}
	for _, selector := range []string{".infoContent", ".article"} {
		container := doc.Find(selector).First()
		if container.Length() == 0 {
			continue
		}
		for _, breadcrumb := range []string{".top_left", ".breadcrumb", ".current"} {
			if typ := ygbTypeCandidate(container.Find(breadcrumb).First().Text(), title); typ != "" {
				return typ
			}
		}
	}
	if current := doc.Find(".current h3").First(); current.Length() > 0 {
		if typ := ygbTypeCandidate(current.Text(), title); typ != "" {
			return typ
		}
	}
	for _, selector := range []string{".list-title", ".boxRight > .title .w700", ".newsTitle"} {
		if typ := ygbTypeCandidate(doc.Find(selector).First().Text(), title); typ != "" {
			return typ
		}
	}
	return ""
}

func ygbArticleDate(doc *goquery.Document) (string, string) {
	if doc == nil {
		return "", ""
	}
	for _, selector := range []string{".infoContent .newsAttribute .date", ".article .source", ".news_source"} {
		field := doc.Find(selector).First()
		if field.Length() == 0 {
			continue
		}
		if day := strings.Join(strings.Fields(field.Find(".day").First().Text()), " "); day != "" {
			clock := strings.Join(strings.Fields(field.Find(".time").First().Text()), " ")
			if date, publishedAt := ygbDateText(strings.TrimSpace(day + " " + clock)); date != "" {
				return date, publishedAt
			}
		}
		if value := strings.Join(strings.Fields(field.Text()), " "); value != "" {
			if date, publishedAt := ygbDateText(value); date != "" {
				return date, publishedAt
			}
		}
	}
	return "", ""
}

func ygbArticleSource(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".infoContent .newsAttribute .source", ".article .source", ".news_source"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if value == "" {
			continue
		}
		for _, prefix := range []string{"来源:", "来源：", "信息来源:", "信息来源："} {
			value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
		return value
	}
	return ""
}

func ygbApplyContent(article *model.Article, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}
	article.Content = extractBodaContentText(content)
	article.Images = nil
	clone := content.Clone()
	seenImages := make(map[string]bool)
	clone.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ygbImageURL(image, baseURL)
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
		resolved := ygbResourceURL(baseURL, href)
		if resolved == "" {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		link.SetAttr("href", resolved)
		name := ygbAttachmentName(link, resolved)
		if name == "" {
			name = ygbURLFilename(resolved)
		}
		if name != "" {
			article.Attachments = ygbAppendAttachment(article.Attachments, model.Attachment{Name: name, URL: resolved})
		}
	})
	if htmlValue, err := clone.Html(); err == nil {
		article.ContentHTML = htmlValue
	}
}

func ygbAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		if href == "" || !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := ygbResourceURL(baseURL, href)
		if resolved == "" || ygbArticleURL(resolved, baseURL) || seen[resolved] {
			return
		}
		name := ygbAttachmentName(link, resolved)
		if name == "" {
			name = ygbURLFilename(resolved)
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func ygbAppendAttachment(attachments []model.Attachment, candidate model.Attachment) []model.Attachment {
	for _, attachment := range attachments {
		if attachment.URL == candidate.URL {
			return attachments
		}
	}
	return append(attachments, candidate)
}

func ygbAttachmentName(link *goquery.Selection, resolved string) string {
	if link == nil || link.Length() == 0 {
		return ""
	}
	for _, value := range []string{link.AttrOr("download", ""), link.AttrOr("name", ""), link.AttrOr("title", ""), link.Text()} {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" || value == "下载" || value == "点击下载" || value == "附件" {
			continue
		}
		return value
	}
	return ygbURLFilename(resolved)
}

func ygbURLFilename(raw string) string {
	parsed, err := url.Parse(raw)
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

func ygbImageURL(image *goquery.Selection, baseURL string) string {
	if image == nil || image.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		if value := ygbResourceURL(baseURL, image.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func ygbResourceURL(baseURL, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	path := strings.ToLower(parsed.Path)
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif", "/loading.gif"} {
		if strings.HasSuffix(path, placeholder) {
			return ""
		}
	}
	return resolved
}

func ygbArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && ygbArticlePathRE.MatchString(parsed.Path) &&
		strings.EqualFold(parsed.Scheme, base.Scheme) && strings.EqualFold(parsed.Host, base.Host)
}

func ygbSiteBaseURL() string {
	if site := AllSites["ygb"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return ygbCanonicalBaseURL
}

func ygbFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := ygbSiteBaseURL()
	fullURL := cleanURL(resolveURL(baseURL, html.UnescapeString(strings.TrimSpace(articleURL))))
	if !ygbArticleURL(fullURL, baseURL) {
		return nil, nil, fmt.Errorf("ygb: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("ygb: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_ygbArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("ygb: parse article %s: %w", fullURL, err)
	}
	article.Images = ygbVerifyImages(article.Images, baseURL)
	article.Attachments = ygbVerifyAttachments(article.Attachments, baseURL)
	ygbSanitizeContentHTML(article, baseURL)
	return article, body, nil
}

func ygbVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := ygbResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := ygbFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func ygbVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := ygbResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := ygbFetchResource(resolved, baseURL, false)
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

func ygbFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := ygbResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("ygb: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("ygb: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if strings.TrimSpace(baseURL) != "" {
		req.Header.Set("Referer", strings.TrimRight(baseURL, "/")+"/")
	}
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Host, via[0].URL.Host) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("ygb: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("ygb: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, ygbMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("ygb: read resource %s: %w", resolved, err)
	}
	if len(body) > ygbMaxResourceBytes || !ygbResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("ygb: resource %s returned an invalid type, shell, signature, or size", resolved)
	}
	return body, contentType, nil
}

func ygbResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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
	if ygbResourceShellRE.Match(probe) {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	magic := ygbResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && ygbIsImageMagic(magic)
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

func ygbIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func ygbResourceMagic(body []byte) string {
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

func ygbSanitizeContentHTML(article *model.Article, baseURL string) {
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
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"ygb-content-root\">" + article.ContentHTML + "</div>"))
	if err != nil {
		article.ContentHTML = ""
		return
	}
	root := doc.Find("#ygb-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ygbImageURL(image, baseURL)
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
		resolved := ygbResourceURL(baseURL, href)
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

func ygbBlockedPage(raw []byte, doc *goquery.Document) bool {
	if doc != nil {
		title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
		if ygbResourceShellRE.MatchString(title) {
			return true
		}
		visible := doc.Find("body").Clone()
		visible.Find("script, style, noscript").Remove()
		visible.Find(".v_news_content").Remove()
		lower := strings.ToLower(strings.Join(strings.Fields(visible.Text()), " "))
		for _, marker := range []string{
			"cf-chl-", "challenge-platform", "turnstile", "captcha", "验证码", "人机验证", "安全验证",
			"cloudflare", "just a moment", "access denied", "访问过于频繁", "请求已被阻断", "404 not found",
			"页面不存在", "网页不存在", "error 404",
		} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
		return false
	}
	probe := raw
	if len(probe) > 16384 {
		probe = probe[:16384]
	}
	lower := strings.ToLower(string(probe))
	for _, marker := range []string{
		"cf-chl-", "challenge-platform", "turnstile", "captcha", "验证码", "人机验证", "安全验证",
		"cloudflare", "just a moment", "access denied", "访问过于频繁", "请求已被阻断", "404 not found",
		"页面不存在", "网页不存在", "error 404",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
