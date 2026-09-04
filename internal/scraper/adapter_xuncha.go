package scraper

import (
	"bytes"
	"fmt"
	htmlstd "html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// xunchaSubPages contains the article-list entry points confirmed from the
// current navigation and homepage modules. Static profile/contact pages,
// the empty jyjl parent shell, and the unavailable "交大实践" link are not
// article feeds.
var xunchaSubPages = []string{
	"gzdt", "sjjs", "xxgongkai", "zcfg", "zcfg/tlztxx", "zcfg/dnfg", "zcfg/xxzd", "zyjh", "jyjl/tszs",
}

var xunchaFeedTypes = map[string]string{
	"gzdt":        "工作动态",
	"sjjs":        "上级精神",
	"xxgongkai":   "信息公开",
	"zcfg":        "政策法规",
	"zcfg/tlztxx": "条例专题学习",
	"zcfg/dnfg":   "党内法规",
	"zcfg/xxzd":   "学校制度",
	"zyjh":        "重要讲话",
	"jyjl/tszs":   "他山之石",
}

var (
	xunchaArticlePathRE   = regexp.MustCompile("(?i)^/info/[0-9]+/[0-9]+[.]html?$")
	xunchaPageCounterRE   = regexp.MustCompile("(?:^|[^0-9])[0-9]+\\s*/\\s*([0-9]+)(?:[^0-9]|$)")
	xunchaResourceShellRE = regexp.MustCompile("(?is)<!doctype\\s+html|<html(?:\\s|>)|<head(?:\\s|>)|<body(?:\\s|>)|附件下载|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|404\\s+not\\s+found|access\\s+denied|cloudflare|just\\s+a\\s+moment|安全验证|请求已被阻断")
)

const (
	xunchaCanonicalBaseURL = "https://xuncha.swjtu.edu.cn"
	xunchaMaxResourceBytes = 32 << 20
)

func init() {
	registerAdapter("xuncha", SiteAdapter{
		SubPages:     xunchaSubPages,
		ParseList:    parseAdapter_xunchaList,
		ParseArticle: parseAdapter_xunchaArticle,
		PageURL:      xunchaPageURL,
		TotalPages:   xunchaTotalPages,
		FetchArticle: xunchaFetchArticle,
	})
}

func xunchaBaseURL() string {
	if site := AllSites["xuncha"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return xunchaCanonicalBaseURL
}

func xunchaIsKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	for _, candidate := range xunchaSubPages {
		if slug == candidate {
			return true
		}
	}
	return false
}

func xunchaPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	if !xunchaIsKnownFeed(slug) {
		return ""
	}
	return reverseBodaPageURL(baseURL, slug, page, totalPages, extension)
}

func parseAdapter_xunchaList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xuncha: parse list HTML: %w", err)
	}
	if xunchaBlockedDocument(doc) {
		return nil, fmt.Errorf("xuncha: response is an error, WAF, or verification page")
	}
	if xunchaIsHomepage(doc) {
		items := xunchaParseHomepage(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("xuncha: homepage has no recognisable article modules")
		}
		return items, nil
	}
	pageType := strings.TrimSpace(doc.Find("meta[name=\"pageType\"], meta[name=\"pagetype\"]").First().AttrOr("content", ""))
	if pageType != "" && pageType != "2" {
		return nil, fmt.Errorf("xuncha: response is not an article list page (pageType=%s)", pageType)
	}

	container := doc.Find(".main_right .conlistComm").First()
	if container.Length() == 0 {
		container = doc.Find(".conlistComm").First()
	}
	if container.Length() == 0 {
		return nil, fmt.Errorf("xuncha: response has no conlistComm list container")
	}

	typ := xunchaDocumentType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	container.Children().Each(func(_ int, row *goquery.Selection) {
		if strings.EqualFold(goquery.NodeName(row), "dl") {
			xunchaAppendItem(&items, seen, row.Find("dt > a[href]").First(), row, typ, baseURL)
		}
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("xuncha: list container has no real article rows")
	}
	return items, nil
}

func xunchaIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find("meta[name=\"pageType\"], meta[name=\"pagetype\"]").First().AttrOr("content", ""))
	if pageType == "1" {
		return true
	}
	return doc.Find(".news-bd-slider .bd, .news-bd-list, .column-list-item").Length() > 0 &&
		doc.Find(".conlistComm dl").Length() == 0
}

func xunchaParseHomepage(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	appendHomepage := func(anchor *goquery.Selection, typ, dateText string) {
		xunchaAppendHomepageItem(&items, seen, anchor, typ, dateText, baseURL)
	}

	// The carousel and the adjacent work-dynamics list intentionally overlap;
	// the URL set removes that presentation duplicate.
	doc.Find(".news-bd-slider .bd ul > li > a[href]").Each(func(_ int, anchor *goquery.Selection) {
		appendHomepage(anchor, "工作动态", "")
	})
	doc.Find(".news-bd-list").Each(func(_ int, module *goquery.Selection) {
		typ := xunchaKnownType(module.Find(".tab > .tit").First().Text())
		if typ == "" {
			return
		}
		module.Find(".tab-bd > h3 > a[href]").Each(func(_ int, anchor *goquery.Selection) {
			appendHomepage(anchor, typ, "")
		})
		module.Find(".tab-bd > ul > li > a[href]").Each(func(_ int, anchor *goquery.Selection) {
			appendHomepage(anchor, typ, anchor.Find("span").Last().Text())
		})
	})
	doc.Find(".column-list-item > dl").Each(func(_ int, column *goquery.Selection) {
		typ := xunchaKnownType(column.Find("dt > .tit").First().Text())
		if typ == "" {
			return
		}
		column.Find("dd > a[href]").Each(func(_ int, anchor *goquery.Selection) {
			appendHomepage(anchor, typ, anchor.Find("span").Last().Text())
		})
	})
	return items
}

func xunchaAppendHomepageItem(items *[]model.NewsItem, seen map[string]bool, anchor *goquery.Selection, typ, dateText, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	fullURL := xunchaHTTPURL(baseURL, anchor.AttrOr("href", ""))
	if !xunchaArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find("p").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := xunchaDateValues(dateText)
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

func xunchaKnownType(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	switch value {
	case "工作动态", "上级精神", "信息公开", "政策法规", "条例专题学习", "党内法规", "学校制度", "重要讲话", "他山之石":
		return value
	default:
		return ""
	}
}

func xunchaDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	// This is the fixed section label on both list and detail pages.
	for _, selector := range []string{".main_right .current > h3", ".current > h3", ".list-title", ".position h1", ".local h2"} {
		if typ := xunchaKnownType(doc.Find(selector).First().Text()); typ != "" {
			return typ
		}
	}
	// Keep compatibility with synthetic Boda fixtures while only accepting
	// exact known labels; never infer a type from broad h1/h2 text.
	return xunchaKnownType(doc.Find("meta[name=\"pageTitle\"], meta[name=\"pagetitle\"]").First().AttrOr("content", ""))
}

func xunchaAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	fullURL := xunchaHTTPURL(baseURL, anchor.AttrOr("href", ""))
	if !xunchaArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := xunchaDateValues(row.Find("dd").First().Text())
	rowType := strings.Trim(strings.TrimSpace(row.Find("dt > span").First().Text()), "[]【】")
	if known := xunchaKnownType(rowType); known != "" {
		typ = known
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

func xunchaDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(htmlstd.UnescapeString(value)), " ")
	if match := dateTimeRe.FindString(value); match != "" {
		published := normalizePublishedAt(match)
		if len(published) >= 10 {
			if publishedAtHasClock(published) {
				return published[:10], published
			}
			return published[:10], ""
		}
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func xunchaTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if !xunchaIsKnownFeed(slug) {
		return 0, fmt.Errorf("xuncha: %q is not an article-list route", slug)
	}
	body, err := fetch(xunchaPageURL(xunchaBaseURL(), slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("xuncha: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xuncha: parse first page for %s: %w", slug, err)
	}
	if xunchaBlockedDocument(doc) || strings.TrimSpace(doc.Find("meta[name=\"pageType\"], meta[name=\"pagetype\"]").First().AttrOr("content", "")) == "3" {
		return 0, fmt.Errorf("xuncha: %s first response is not a list page", slug)
	}
	if doc.Find(".main_right .conlistComm dl, .conlistComm dl").Length() == 0 {
		return 0, fmt.Errorf("xuncha: %s first response has no article rows", slug)
	}
	if total := xunchaPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func xunchaPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find("[id^=\"fanye\"], .fanye, .p_pages, .paging, .page").Text()), " ")
	if match := xunchaPageCounterRE.FindStringSubmatch(text); match != nil {
		total, _ := strconv.Atoi(match[1])
		if total > 0 {
			return total
		}
	}
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d, .p_no, .p_no_d").Each(func(_ int, node *goquery.Selection) {
		value, _ := strconv.Atoi(strings.TrimSpace(node.Text()))
		if value > maxPage {
			maxPage = value
		}
	})
	return maxPage
}

func parseAdapter_xunchaArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xuncha: parse article HTML: %w", err)
	}
	if xunchaBlockedDocument(doc) {
		return nil, fmt.Errorf("xuncha: response is an error, WAF, or verification page")
	}
	pageType := strings.TrimSpace(doc.Find("meta[name=\"pageType\"], meta[name=\"pagetype\"]").First().AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("xuncha: response is not an article page (pageType=%s)", pageType)
	}

	titleSel := doc.Find(".main_right .article > h1").First()
	if titleSel.Length() == 0 {
		// These fallbacks are limited to the legacy synthetic Boda shape used
		// by the repository's cross-adapter regression test.
		for _, selector := range []string{".article-title", ".news-title", ".detail-title"} {
			if candidate := doc.Find(selector).First(); candidate.Length() > 0 {
				titleSel = candidate
				break
			}
		}
	}
	if titleSel.Length() == 0 {
		return nil, fmt.Errorf("xuncha: article has no fixed title node")
	}
	title := cleanTitle(strings.TrimSpace(titleSel.Text()))
	if title == "" {
		return nil, fmt.Errorf("xuncha: article title is empty")
	}

	sourceSel := doc.Find(".main_right .article > .source").First()
	if sourceSel.Length() == 0 {
		sourceSel = doc.Find(".news_source").First()
	}
	source, author, date, publishedAt := xunchaDetailMetadata(sourceSel)

	body := doc.Find(".main_right .article_content .article .art_con .content14 > [id^=\"vsb_content\"] > .v_news_content").First()
	if body.Length() == 0 {
		body = doc.Find("#vsb_content_501 > .v_news_content, #vsb_content > .v_news_content, .v_news_content").First()
	}
	if body.Length() == 0 {
		return nil, fmt.Errorf("xuncha: article has no fixed body node")
	}

	article := &model.Article{
		Title:       title,
		Source:      source,
		Author:      author,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        xunchaDocumentType(doc),
	}
	xunchaFinalizeContent(article, body, baseURL)
	xunchaAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func xunchaDetailMetadata(sourceSel *goquery.Selection) (source, author, date, publishedAt string) {
	if sourceSel == nil || sourceSel.Length() == 0 {
		return "", "", "", ""
	}
	text := strings.Join(strings.Fields(htmlstd.UnescapeString(sourceSel.Text())), " ")
	source = xunchaBetweenLabels(text, "来源", "发布者")
	author = xunchaBetweenLabels(text, "发布者", "日期")
	dateText := text
	if index := strings.Index(text, "日期"); index >= 0 {
		dateText = text[index+len("日期"):]
	}
	date, publishedAt = xunchaDateValues(dateText)
	return source, author, date, publishedAt
}

func xunchaBetweenLabels(value, start, end string) string {
	index := strings.Index(value, start)
	if index < 0 {
		return ""
	}
	value = value[index+len(start):]
	if colon := strings.IndexAny(value, "：:"); colon >= 0 {
		value = value[colon+len(string(value[colon])):]
	}
	if endIndex := strings.Index(value, end); endIndex >= 0 {
		value = value[:endIndex]
	}
	return strings.TrimSpace(value)
}

func xunchaFinalizeContent(article *model.Article, body *goquery.Selection, baseURL string) {
	if article == nil || body == nil || body.Length() == 0 {
		return
	}
	article.Content = extractBodaContentText(body)
	content := body.Clone()
	content.Find("script, style, noscript").Remove()
	seenImages := make(map[string]bool)
	seenAttachments := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := xunchaResourceURL(baseURL, xunchaImageSource(image))
		image.RemoveAttr("data-src")
		image.RemoveAttr("data-original")
		image.RemoveAttr("data-lazy-src")
		image.RemoveAttr("orisrc")
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		if !seenImages[resolved] {
			seenImages[resolved] = true
			article.Images = append(article.Images, resolved)
		}
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(htmlstd.UnescapeString(link.AttrOr("href", "")))
		resolved := xunchaResourceURL(baseURL, href)
		if bodaLooksLikeAttachmentLink(link, href) {
			if resolved == "" {
				link.Remove()
				return
			}
			if !seenAttachments[resolved] {
				seenAttachments[resolved] = true
				article.Attachments = append(article.Attachments, model.Attachment{
					Name: xunchaAttachmentName(link, resolved),
					URL:  resolved,
				})
			}
			link.SetAttr("href", resolved)
			return
		}
		if resolved == "" {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	htmlContent, err := content.Html()
	if err == nil {
		article.ContentHTML = htmlContent
	}
}

func xunchaImageSource(image *goquery.Selection) string {
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func xunchaAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	scope := doc.Find(".main_right .article_content").First()
	if scope.Length() == 0 {
		scope = doc.Selection
	}
	scope.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) {
			return
		}
		href := strings.TrimSpace(htmlstd.UnescapeString(link.AttrOr("href", "")))
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := xunchaResourceURL(baseURL, href)
		if resolved == "" || xunchaArticleURL(resolved, baseURL) || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{
			Name: xunchaAttachmentName(link, resolved),
			URL:  resolved,
		})
	})
}

func xunchaAttachmentName(link *goquery.Selection, href string) string {
	for _, value := range []string{
		link.AttrOr("download", ""),
		link.AttrOr("data-filename", ""),
		link.AttrOr("name", ""),
		link.AttrOr("title", ""),
		link.Text(),
	} {
		value = strings.TrimSpace(strings.Join(strings.Fields(htmlstd.UnescapeString(value)), " "))
		value = strings.Trim(value, "【】[]")
		if value != "" {
			return value
		}
	}
	if parsed, err := url.Parse(href); err == nil {
		query := parsed.Query()
		for _, key := range []string{"name", "filename", "fileName"} {
			if value := strings.TrimSpace(query.Get(key)); value != "" {
				return value
			}
		}
		if name := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]; name != "" {
			return htmlstd.UnescapeString(name)
		}
	}
	return href
}

func xunchaArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	if err != nil || baseErr != nil || parsed.Hostname() == "" || base.Hostname() == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return (scheme == "http" || scheme == "https") &&
		strings.EqualFold(parsed.Hostname(), base.Hostname()) &&
		xunchaArticlePathRE.MatchString(parsed.Path)
}

func xunchaHTTPURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	parsed.Fragment = ""
	return parsed.String()
}

func xunchaResourceURL(baseURL, raw string) string {
	resolved := xunchaHTTPURL(baseURL, raw)
	if resolved == "" {
		return ""
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return ""
	}
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"captcha", "verifycode", "verification", "createimage", "getcode",
		"placeholder", "transparent", "spacer", "blank.gif", "loading.gif",
		"pixel.gif", "access-denied", "waf",
	} {
		if strings.Contains(resourceText, marker) {
			return ""
		}
	}
	return resolved
}

func xunchaFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := xunchaBaseURL()
	fullURL := xunchaHTTPURL(baseURL, articleURL)
	if !xunchaArticleURL(fullURL, baseURL) {
		return nil, nil, fmt.Errorf("xuncha: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("xuncha: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_xunchaArticle(body, baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("xuncha: parse article %s: %w", fullURL, err)
	}
	article.Images = xunchaVerifyImages(article.Images, baseURL, fullURL)
	article.Attachments = xunchaVerifyAttachments(article.Attachments, baseURL, fullURL)
	article.ContentHTML = xunchaSanitizeContentHTML(article.ContentHTML, baseURL, article.Images, article.Attachments)
	return article, body, nil
}

func xunchaVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, image := range images {
		resolved := xunchaResourceURL(baseURL, image)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := xunchaFetchResource(resolved, referer, true)
		if err != nil || !xunchaResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func xunchaVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := xunchaResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := xunchaFetchResource(resolved, referer, false)
		if err != nil || !xunchaResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func xunchaFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	resolved := xunchaResourceURL(referer, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("xuncha: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("xuncha: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}

	requestURL, _ := url.Parse(resolved)
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 0 && requestURL != nil && !strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("xuncha: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("xuncha: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, xunchaMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("xuncha: read resource %s: %w", resolved, err)
	}
	if len(body) > xunchaMaxResourceBytes || !xunchaResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("xuncha: resource %s returned invalid Content-Type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func xunchaResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType == "" {
		return false
	}
	switch mediaType {
	case "text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml":
		return false
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if xunchaResourceShellRE.Match(probe) {
		return false
	}

	magic := xunchaResourceMagic(body)
	if image {
		return xunchaImageTypeMatchesMagic(mediaType, magic)
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
	case strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func xunchaImageTypeMatchesMagic(mediaType, magic string) bool {
	switch mediaType {
	case "image/jpeg", "image/jpg":
		return magic == "jpeg"
	case "image/png":
		return magic == "png"
	case "image/gif":
		return magic == "gif"
	case "image/webp":
		return magic == "webp"
	case "image/bmp":
		return magic == "bmp"
	case "image/tiff":
		return magic == "tiff"
	case "image/svg+xml":
		return magic == "svg"
	default:
		return false
	}
}

func xunchaResourceMagic(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x03, 0x04}) || bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x05, 0x06}) || bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x07, 0x08})):
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
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	if len(trimmed) > 0 {
		probe := trimmed
		if len(probe) > 4096 {
			probe = probe[:4096]
		}
		if bytes.Contains(bytes.ToLower(probe), []byte("<svg")) {
			return "svg"
		}
	}
	return ""
}

func xunchaSanitizeContentHTML(raw, baseURL string, images []string, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	imageSet := make(map[string]bool, len(images))
	for _, image := range images {
		if resolved := xunchaResourceURL(baseURL, image); resolved != "" {
			imageSet[resolved] = true
		}
	}
	attachmentSet := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		if resolved := xunchaResourceURL(baseURL, attachment.URL); resolved != "" {
			attachmentSet[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"xuncha-content-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	root := doc.Find("#xuncha-content-root").First()
	root.Find("script, style, noscript").Remove()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := xunchaResourceURL(baseURL, xunchaImageSource(image))
		if resolved == "" || !imageSet[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		image.RemoveAttr("data-src")
		image.RemoveAttr("data-original")
		image.RemoveAttr("data-lazy-src")
		image.RemoveAttr("orisrc")
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(htmlstd.UnescapeString(link.AttrOr("href", "")))
		resolved := xunchaHTTPURL(baseURL, href)
		if bodaLooksLikeAttachmentLink(link, href) {
			if resolved == "" || !attachmentSet[resolved] {
				link.Remove()
				return
			}
			link.SetAttr("href", resolved)
			return
		}
		if resolved == "" {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", resolved)
	})
	htmlContent, err := root.Html()
	if err != nil {
		return ""
	}
	return htmlContent
}

func xunchaBlockedDocument(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"404", "系统提示", "安全验证", "access denied", "just a moment", "页面不存在", "请求已被阻断"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	bodyText := strings.ToLower(strings.Join(strings.Fields(doc.Find("body").First().Text()), " "))
	if len(bodyText) > 4096 {
		bodyText = bodyText[:4096]
	}
	for _, marker := range []string{"请输入验证码下载附件", "请求已被阻断", "安全验证", "just a moment", "access denied"} {
		if strings.Contains(bodyText, marker) {
			return true
		}
	}
	return false
}
