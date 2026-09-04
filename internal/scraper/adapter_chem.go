package scraper

import (
	"bytes"
	"fmt"
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

const chemCanonicalBaseURL = "https://chem.swjtu.edu.cn"

const chemMaxResourceBytes = 32 << 20

// chemSubPages contains the article feeds exposed by the site's visible
// sections and by the secondary navigation on those section pages. The
// profile/static pages are deliberately not included: they are real pages,
// but they are not news feeds. A few of the linked feeds currently render
// another feed's contents; keeping their URLs here preserves their actual
// reachability without pretending that the source is correctly partitioned.
var chemSubPages = []string{
	"index/xyxw",
	"index/xytz",
	"index/jwgg",
	"index/jwgg/yjsgg",
	"index/ttxw",
	"index/kxyj",
	"rcpy",
	"rcpy/yjsds",
	"rcpy/yjspygg",
	"rcpy/wjzlxz",
	"kxyj",
	"kxyj/kytz",
	"kxyj/kytd",
	"kxyj/kydt",
	"kxyj/xshd",
	"kxyj/sypt",
	"dqgz",
	"dqgz/djc",
	"dqgz/djdt",
	"dqgz/zzjsydyfz",
	"dqgz/jwgz",
	"dqgz/ghgz",
	"xsgz",
	"xsgz/xgtz",
	"xsgz/xgdt",
	"xsgz/gsgg",
	"aqgz",
	"aqgz/aqxz",
	"aqgz/gzzd",
	"aqgz/xgzy",
	"rczp",
	"xygz",
	"xygz/xyfc",
	"xygz/bysfc",
	"hzjl",
	"hzjl/gjhhz",
	"hzjl/xqhz",
	"zsjy",
	"zsjy/zsgz",
	"zsjy/jygz",
}

var (
	chemInfoArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	chemDateRe            = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})`)
	chemSplitDateRe       = regexp.MustCompile(`^\s*([0-9]{1,2})\s*(20[0-9]{2})\s*[-/.]\s*([0-9]{1,2})\s*$`)
	chemResourceShellRe   = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|附件下载|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|404\s+not\s+found|access\s+denied|cloudflare|just\s+a\s+moment|安全验证|请求被阻断|请求报文存在语法错误`)
)

func init() {
	if site := AllSites["chem"]; site != nil {
		site.BaseURL = chemCanonicalBaseURL
	}
	registerAdapter("chem", SiteAdapter{
		SubPages:     chemSubPages,
		ParseList:    parseAdapter_chemList,
		ParseArticle: parseAdapter_chemArticle,
		PageURL:      chemPageURL,
		FetchArticle: chemFetchArticle,
	})
}

// chemPageURL maps the logical page number (1=newest) to the reverse-numbered
// generated files used by chem.swjtu.edu.cn. For example, a 31-page feed
// advertises page 2 as /feed/30.htm and page 31 as /feed/1.htm.
func chemPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
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

func parseAdapter_chemList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse chem list: %w", err)
	}
	if chemLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("chem list is an error or verification page")
	}

	// Every real chem list page uses this exact container. It is important to
	// return an empty result when the container is present but has no rows (for
	// example the currently empty index/kxyj feed), rather than scanning the
	// page shell and producing navigation links.
	list := doc.Find("main .news-list-wrap ul.list")
	if list.Length() > 0 {
		items := make([]model.NewsItem, 0)
		seen := make(map[string]bool)
		defaultType := chemBreadcrumbType(doc)
		list.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
			addChemListItem(&items, seen, row, defaultType, baseURL)
		})
		return items, nil
	}

	// The home page is an aggregation of several real lists. Restricting the
	// pass to article links below <main> excludes the header, banner, footer,
	// external links, and the page's non-article navigation.
	if chemHasHomeLayout(doc) {
		items := parseChemHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("chem home page contains no article entries")
		}
		return items, nil
	}

	return nil, fmt.Errorf("chem page is not a supported article list")
}

func addChemListItem(items *[]model.NewsItem, seen map[string]bool, row *goquery.Selection, defaultType, baseURL string) {
	anchor := row.Find("a[href]").First()
	if anchor.Length() == 0 || isInNavArea(anchor) {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL, ok := chemArticleURL(baseURL, href)
	if !ok || seen[fullURL] {
		return
	}

	title := chemListTitle(row, anchor)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 100 {
		return
	}

	date, publishedAt := chemDateFromScope(row.Find(".info").First())
	if date == "" {
		for _, selector := range []string{"time", ".date", ".time"} {
			date, publishedAt = chemDateFromScope(row.Find(selector).First())
			if date != "" {
				break
			}
		}
	}

	item := model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     date,
		Type:     chemListType(row, defaultType),
		ParsedAt: time.Now(),
	}
	if chemHasClock(publishedAt) {
		item.PublishedAt = publishedAt
	}
	seen[fullURL] = true
	*items = append(*items, item)
}

func chemListTitle(row, anchor *goquery.Selection) string {
	for _, selector := range []string{"p.toe", ".news-title", "h3", "h4"} {
		if title := strings.TrimSpace(row.Find(selector).First().Text()); title != "" {
			return cleanTitle(title)
		}
	}
	if title := strings.TrimSpace(anchor.AttrOr("title", "")); title != "" {
		return cleanTitle(title)
	}
	return cleanTitle(strings.TrimSpace(anchor.Text()))
}

func chemListType(row *goquery.Selection, defaultType string) string {
	label := strings.TrimSpace(row.Find("a > span").First().Text())
	if label == "" {
		label = strings.TrimSpace(row.Find("a span").First().Text())
	}
	label = strings.TrimSpace(strings.Trim(label, "[]【】"))
	if label != "" {
		return label
	}
	return defaultType
}

func chemHasHomeLayout(doc *goquery.Document) bool {
	return doc.Find("main section.news-list, main .news-section, main .news-box, main .notice-box").Length() > 0
}

func parseChemHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	doc.Find("main a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL, ok := chemArticleURL(baseURL, href)
		if !ok {
			return
		}
		title := chemHomeTitle(anchor)
		if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 100 {
			return
		}
		date, publishedAt := chemHomeDate(anchor)
		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     chemHomeType(anchor),
			ParsedAt: time.Now(),
		}
		if chemHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}
		addChemHomeItem(&items, seen, item)
	})
	return items
}

func chemHomeTitle(anchor *goquery.Selection) string {
	for _, selector := range []string{"h4.toe0", "p.toe", "p.toe0", "p.toe2", "h3", "h4"} {
		if title := strings.TrimSpace(anchor.Find(selector).First().Text()); title != "" {
			return cleanTitle(title)
		}
	}
	return cleanTitle(strings.TrimSpace(anchor.AttrOr("title", "")))
}

func chemHomeType(anchor *goquery.Selection) string {
	for _, selector := range []string{".news-box", ".notice-box", "section.news-list", "section"} {
		container := anchor.Closest(selector)
		if container.Length() == 0 {
			continue
		}
		for _, heading := range []string{".global-hd h3", ".hd h3"} {
			if value := strings.TrimSpace(container.Find(heading).First().Text()); value != "" {
				return value
			}
		}
	}
	return ""
}

func chemHomeDate(anchor *goquery.Selection) (string, string) {
	if scope := anchor.Find(".time").First(); scope.Length() > 0 {
		if date, published := chemDateFromScope(scope); date != "" {
			return date, published
		}
	}
	var date, published string
	anchor.Find("time, span, [class*='date']").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
		date, published = chemDateFromScope(candidate)
		return date == ""
	})
	return date, published
}

func addChemHomeItem(items *[]model.NewsItem, seen map[string]int, item model.NewsItem) {
	if index, ok := seen[item.URL]; ok {
		current := &(*items)[index]
		// The featured card precedes the dated list row on the live home page.
		// Upgrade that first occurrence with the authoritative date and list
		// category while retaining the page's display order.
		if current.Date == "" && item.Date != "" {
			current.Date = item.Date
			current.PublishedAt = item.PublishedAt
		}
		if (current.Type == "" || current.Type == "学院要闻") && item.Type != "" {
			current.Type = item.Type
		}
		return
	}
	seen[item.URL] = len(*items)
	*items = append(*items, item)
}

func chemDateFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}
	day := strings.TrimSpace(scope.Find(".t1").First().Text())
	yearMonth := strings.TrimSpace(scope.Find(".t2").First().Text())
	if day != "" && yearMonth != "" {
		if match := regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`).FindStringSubmatch(yearMonth); match != nil {
			if date := normalizeDateParts(match[1], match[2], day); date != "" {
				return date, ""
			}
		}
	}
	return chemDateValue(flattenedText(scope))
}

func chemDateValue(value string) (string, string) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(value); normalized != "" {
		if chemHasClock(normalized) {
			return normalized[:10], normalized
		}
		return normalized[:10], ""
	}
	if match := chemDateRe.FindStringSubmatch(value); match != nil {
		if date := normalizeDateParts(match[1], match[2], match[3]); date != "" {
			return date, ""
		}
	}
	if match := chemSplitDateRe.FindStringSubmatch(value); match != nil {
		if date := normalizeDateParts(match[2], match[3], match[1]); date != "" {
			return date, ""
		}
	}
	return "", ""
}

func chemHasClock(value string) bool {
	return strings.Contains(value, ":")
}

func chemArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !isValidNewsURL(fullURL) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}

	if chemInfoArticlePathRe.MatchString(parsed.Path) {
		parsed.Fragment = ""
		return parsed.String(), true
	}
	// The hidden 头条 feed uses a legacy content.jsp URL. It is accepted as a
	// source list entry, while parseAdapter_chemArticle still rejects the
	// current live response when that URL returns the site's error page.
	query := parsed.Query()
	if strings.EqualFold(parsed.Path, "/content.jsp") &&
		strings.Contains(strings.ToLower(query.Get("urltype")), "newscontenturl") && query.Get("wbnewsid") != "" {
		parsed.Fragment = ""
		return parsed.String(), true
	}
	return "", false
}

func parseAdapter_chemArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse chem article html: %w", err)
	}
	if chemLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("chem article is an error or verification page")
	}

	title := chemFixedArticleTitle(doc)
	if title == "" {
		return nil, fmt.Errorf("chem page is not an article: missing fixed detail title")
	}
	info := doc.Find(".news-list-wrap .bd .title .info").First()
	if info.Length() == 0 {
		return nil, fmt.Errorf("chem page is not an article: missing fixed detail metadata")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse chem article: %w", err)
	}
	article.Title = title
	article.Source = ""
	article.Author = ""
	article.Editor = ""
	article.Date = ""
	article.PublishedAt = ""
	article.Type = chemBreadcrumbType(doc)
	chemFillFixedArticleMetadata(article, info)

	appendChemOutsideImages(article, doc, baseURL)
	appendChemAttachments(article, doc, baseURL)
	chemFilterArticleImages(article, doc, baseURL)
	chemFilterArticleAttachments(article, baseURL)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("chem article has no content")
	}
	chemSanitizeContentHTML(article, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func chemFixedArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	title := doc.Find(".news-list-wrap .bd .title > h3, .news-list-wrap .bd .title > h1").First().Text()
	return strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
}

func chemFillFixedArticleMetadata(article *model.Article, info *goquery.Selection) {
	if article == nil || info == nil || info.Length() == 0 {
		return
	}
	info.Find("span, time, dd").Each(func(_ int, field *goquery.Selection) {
		value := strings.Join(strings.Fields(strings.TrimSpace(field.Text())), " ")
		if value == "" {
			return
		}
		for _, label := range []string{"作者", "发布者"} {
			if fieldValue, ok := chemFixedFieldValue(value, label); ok {
				article.Author = fieldValue
				return
			}
		}
		if fieldValue, ok := chemFixedFieldValue(value, "来源"); ok {
			article.Source = fieldValue
			return
		}
		if fieldValue, ok := chemFixedFieldValue(value, "编辑", "责编"); ok {
			article.Editor = fieldValue
			return
		}
		for _, label := range []string{"日期", "发布日期", "发布时间", "发表时间"} {
			if fieldValue, ok := chemFixedFieldValue(value, label); ok {
				article.Date, article.PublishedAt = chemDateValue(fieldValue)
				return
			}
		}
	})
}

func chemFixedFieldValue(value string, labels ...string) (string, bool) {
	for _, label := range labels {
		for _, separator := range []string{"：", ":"} {
			prefix := label + separator
			if strings.HasPrefix(value, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(value, prefix)), true
			}
		}
	}
	return "", false
}

func chemBreadcrumbType(doc *goquery.Document) string {
	crumb := doc.Find(".news-list-wrap .breadcrumb").First()
	if crumb.Length() > 0 {
		value := ""
		crumb.Find("a").Each(func(_ int, link *goquery.Selection) {
			text := strings.TrimSpace(link.Text())
			if text != "" && text != "首页" {
				value = text
			}
		})
		if value != "" {
			return value
		}
	}
	return strings.TrimSpace(doc.Find(".news-list-wrap > .hd h3").First().Text())
}

func appendChemOutsideImages(article *model.Article, doc *goquery.Document, baseURL string) {
	seen := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		seen[image] = true
	}
	doc.Find(".news-list-wrap .detail img").Each(func(_ int, image *goquery.Selection) {
		if image.Closest(".v_news_content").Length() > 0 {
			return
		}
		resolved := chemImageURLFromSelection(baseURL, image)
		if resolved == "" || chemImageIsPlaceholder(image, resolved) {
			return
		}
		if !seen[resolved] {
			seen[resolved] = true
			article.Images = append(article.Images, resolved)
		}
	})
}

func appendChemAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	// The CMS emits the attachment <UL> after the detail block and, on some
	// pages, malformed <p><UL> markup changes its apparent parent. Scan all
	// links, while retaining the strict download.jsp URL check so navigation
	// cannot leak in.
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if isInNavArea(link) || !chemIsAttachmentLink(link, href) {
			return
		}
		name := strings.TrimSpace(link.Text())
		if name == "" {
			return
		}
		resolved := chemResourceURL(baseURL, href)
		if resolved == "" {
			return
		}
		if seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func chemImageURLFromSelection(baseURL string, image *goquery.Selection) string {
	if image == nil || image.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
			if resolved := chemResourceURL(baseURL, value); resolved != "" {
				return resolved
			}
		}
	}
	return ""
}

func chemImageIsPlaceholder(image *goquery.Selection, resolved string) bool {
	lower := strings.ToLower(resolved)
	for _, marker := range []string{"/blank", "/spacer", "/transparent", "/placeholder", "/loading", "/pixel"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return chemImageDimension(image, "width") == 1 && chemImageDimension(image, "height") == 1
}

func chemImageDimension(image *goquery.Selection, attr string) int {
	if image == nil || image.Length() == 0 {
		return 0
	}
	value := strings.TrimSpace(image.AttrOr(attr, ""))
	if value == "" {
		style := strings.ToLower(image.AttrOr("style", ""))
		match := regexp.MustCompile(`(?:^|;)\s*` + attr + `\s*:\s*([0-9]+)px`).FindStringSubmatch(style)
		if match == nil {
			return 0
		}
		value = match[1]
	}
	number, _ := strconv.Atoi(strings.TrimSuffix(strings.ToLower(value), "px"))
	return number
}

func chemFilterArticleImages(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil {
		return
	}
	candidates := make(map[string]bool)
	if doc != nil {
		doc.Find(".news-list-wrap .detail img").Each(func(_ int, image *goquery.Selection) {
			resolved := chemImageURLFromSelection(baseURL, image)
			if resolved != "" && !chemImageIsPlaceholder(image, resolved) {
				candidates[resolved] = true
			}
		})
	}
	images := make([]string, 0, len(article.Images))
	seen := make(map[string]bool, len(article.Images))
	for _, raw := range article.Images {
		resolved := chemResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] || (len(candidates) > 0 && !candidates[resolved]) {
			continue
		}
		seen[resolved] = true
		images = append(images, resolved)
	}
	article.Images = images
}

func chemFilterArticleAttachments(article *model.Article, baseURL string) {
	if article == nil {
		return
	}
	attachments := make([]model.Attachment, 0, len(article.Attachments))
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		resolved := chemResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		attachment.URL = resolved
		seen[resolved] = true
		attachments = append(attachments, attachment)
	}
	article.Attachments = attachments
}

func chemIsAttachmentLink(link *goquery.Selection, href string) bool {
	if link == nil || link.Length() == 0 || href == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	if strings.TrimSpace(link.AttrOr("download", "")) != "" {
		return true
	}
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, extension := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(lower, extension) {
			return true
		}
	}
	for _, selector := range []string{".attachment", ".attachments", ".news-attachment", ".download"} {
		if ancestor := link.Closest(selector); ancestor.Length() > 0 && strings.Contains(ancestor.Text(), "附件") {
			return true
		}
	}
	return false
}

func chemLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.TrimSpace(doc.Find("title").First().Text())
	lowerTitle := strings.ToLower(title)
	for _, marker := range []string{"系统提示", "附件下载", "验证码", "安全验证", "访问被拒绝", "access denied", "cloudflare", "just a moment", "404"} {
		if strings.Contains(lowerTitle, strings.ToLower(marker)) {
			return true
		}
	}
	body := strings.ToLower(strings.Join(strings.Fields(doc.Find("body").Text()), " "))
	for _, marker := range []string{"页面未找到", "页面不存在", "您访问的页面未找到", "请输入验证码下载附件", "请先登录", "请登录后", "登录页面", "安全验证", "请求被阻断", "请求报文存在语法错误", "access denied", "cloudflare", "just a moment"} {
		if strings.Contains(body, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

// chemFetchArticle is the live detail path. The HTML parser intentionally
// stays deterministic; this hook is where chem's image and download URLs are
// checked against the source. The download endpoint currently returns an
// HTTP 200 CAPTCHA page, so status alone is not enough.
func chemFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := chemBaseURL()
	fullURL, ok := chemArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("chem: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_chemArticle(body, baseURL)
	if err != nil {
		return nil, body, err
	}
	article.Images = chemVerifyImages(article.Images, baseURL)
	article.Attachments = chemVerifyAttachments(article.Attachments, baseURL)
	chemSanitizeContentHTML(article, baseURL)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, body, fmt.Errorf("chem article has no usable content or resources")
	}
	return article, body, nil
}

func chemBaseURL() string {
	if site := AllSites["chem"]; site != nil {
		if baseURL := strings.TrimRight(strings.TrimSpace(site.BaseURL), "/"); baseURL != "" {
			return baseURL
		}
	}
	return chemCanonicalBaseURL
}

func chemResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := bodaResourceURL(baseURL, raw)
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return ""
	}
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{"/captcha", "/verify", "/login", "/signin", "/placeholder", "/transparent", "/spacer", "/blank", "/loading", "/pixel.gif"} {
		if strings.Contains(resourceText, marker) {
			return ""
		}
	}
	return cleanURL(parsed.String())
}

func chemVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := chemResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := chemFetchResource(resolved, baseURL, true)
		if err != nil || !chemResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func chemVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := chemResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := chemFetchResource(resolved, baseURL, false)
		if err != nil || !chemResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func chemFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := chemResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("chem: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("chem: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("chem: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode != http.StatusOK {
		return nil, contentType, fmt.Errorf("chem: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, chemMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("chem: read resource %s: %w", resolved, err)
	}
	if len(body) > chemMaxResourceBytes {
		return nil, contentType, fmt.Errorf("chem: resource %s exceeds %d bytes", resolved, chemMaxResourceBytes)
	}
	return body, contentType, nil
}

func chemResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if chemResourceShellRe.Match(probe) {
		return false
	}
	if image {
		return strings.HasPrefix(mediaType, "image/") && chemIsImageMagic(chemResourceMagic(body))
	}

	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return chemResourceMagic(body) == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return chemResourceMagic(body) == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return chemResourceMagic(body) == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		magic := chemResourceMagic(body)
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return (strings.HasPrefix(mediaType, "text/") && mediaType != "text/html" && mediaType != "text/xml") || mediaType == "application/octet-stream"
	default:
		return mediaType != "text/html" && mediaType != "application/xhtml+xml" && mediaType != "application/json" && mediaType != "text/xml" && mediaType != "application/xml" && chemResourceMagic(body) != ""
	}
}

func chemResourceMagic(body []byte) string {
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
	default:
		return ""
	}
}

func chemIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff":
		return true
	default:
		return false
	}
}

func chemSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		if resolved := chemResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved := chemResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"chem-content-root\">" + article.ContentHTML + "</div>"))
	if err != nil {
		return
	}
	content := doc.Find("#chem-content-root").First()
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := chemImageURLFromSelection(baseURL, image)
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vsbhref", "vurl"} {
			image.RemoveAttr(attr)
		}
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !chemIsAttachmentLink(link, href) {
			return
		}
		resolved := chemResourceURL(baseURL, href)
		if resolved == "" || !validAttachments[resolved] {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	if html, err := content.Html(); err == nil {
		article.ContentHTML = html
	}
}
