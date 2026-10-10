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
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// ai.swjtu.edu.cn is an older Visual SiteBuilder site, but it does not use
// the list markup handled safely by the generic 博达 parser.  In particular,
// its archive rows are <dl> elements, its home page has four independent
// article blocks, and generated page numbers run backwards after page one.
func init() {
	registerAdapter("ai", SiteAdapter{
		SubPages:     aiSubPages,
		ParseList:    parseAdapter_aiList,
		ParseArticle: parseAdapter_aiArticle,
		PageURL:      aiPageURL,
		TotalPages:   aiTotalPages,
		FetchArticle: aiFetchArticle,
	})
}

var (
	aiArticlePathRe    = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	aiErrorPageRe      = regexp.MustCompile(`(?i)404\s*(?:not\s+found|错误)?|403\s*(?:forbidden|错误)?|页面不存在|您访问的页面不存在|访问被拒绝|请求被拦截|访问频繁|安全验证|人机验证|请输入验证码|验证码下载附件|access\s+denied|request\s+blocked|just\s+a\s+moment|cloudflare|please\s+verify|verify\s+you\s+are\s+human|forbidden|too\s+many\s+requests`)
	aiResourceMarkerRe = regexp.MustCompile(`(?i)/(?:spacer|blank|transparent|placeholder|noimage|no-image|loading|captcha|verify|waf|login|access[-_]denied|createimage)(?:[./?]|$)|/pixel\.gif(?:[?]|$)`)
	aiResourceShellRe  = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|您访问的页面不存在|无权限访问|access\s+denied|request\s+blocked|just\s+a\s+moment|cloudflare|please\s+verify|verify\s+you\s+are\s+human|安全验证|安全威胁|请求已被阻断`)
	aiPageTotals       = map[string]int{
		"tzgg": 2,
		"xwdt": 4,
		"xsbg": 2,
		"kxyj": 2,
		"jcsj": 1,
	}
	aiSubPages = []string{"tzgg", "xwdt", "xsbg", "kxyj", "jcsj"}
)

var aiAttachmentExtensions = []string{
	".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf",
	".zip", ".rar", ".7z", ".txt", ".csv",
}

// aiPageURL maps the logical page number to the site's generated file.  The
// live site exposes the actual totals in the page-number labels. Keeping the
// audited totals here supplies a safe fallback when a caller has no usable
// total-page hint; aiTotalPages reads the live .page-box when crawling.
func aiPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}

	if totalPages <= 1 {
		if knownTotal, ok := aiPageTotals[slug]; ok {
			totalPages = knownTotal
		}
	}
	if totalPages <= 1 {
		return ""
	}
	if page > totalPages {
		return ""
	}

	filePage := totalPages - page + 1
	if filePage < 1 {
		// A request beyond the audited archive must not silently repeat the last
		// page and create duplicate news.
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func parseAdapter_aiList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ai: parse list HTML: %w", err)
	}
	if aiLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("ai: received an error or verification page instead of a news list")
	}
	if pageType := aiMetaPageType(doc); pageType != "" && pageType != "1" && pageType != "2" {
		return nil, fmt.Errorf("ai: response is not a list page (pageType=%q)", pageType)
	}

	// Category pages use .conlistComm > dl.  This is deliberately tried first
	// so a future page shell containing a stray home-style block cannot widen
	// extraction to arbitrary document links.
	items := parseAIArchiveRows(doc, baseURL)
	if len(items) == 0 {
		// index.htm has four visible article modules. The dedicated bottom
		// marquee is added by parseAIHomeRows; the top promotional carousel and
		// navigation are not feed entries.
		items = parseAIHomeRows(doc, baseURL)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("ai: page contains no recognized article list")
	}
	return items, nil
}

func parseAIArchiveRows(doc *goquery.Document, baseURL string) []model.NewsItem {
	rows := doc.Find(".conlistComm dl")
	if rows.Length() == 0 {
		return nil
	}

	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	pageType := aiPageType(doc)
	rows.Each(func(_ int, row *goquery.Selection) {
		var anchor *goquery.Selection
		row.Find("dt a[href]").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
			if href := candidate.AttrOr("href", ""); href != "" {
				if _, ok := aiArticleURL(baseURL, href); ok {
					anchor = candidate
					return false
				}
			}
			return true
		})
		if anchor == nil || anchor.Length() == 0 {
			return
		}

		fullURL, ok := aiArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := aiAnchorTitle(anchor)
		if !aiIsArticleTitle(title) {
			return
		}
		date, publishedAt := aiDateFields(row.Find("dd").First().Text())
		if date == "" {
			date, publishedAt = aiDateFields(row.Find("time, .time, .date").First().Text())
		}
		typ := aiTypeLabel(row.Find("dt span a[href]").First().Text())
		if typ == "" {
			typ = pageType
		}
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

func parseAIHomeRows(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("div.fl-box, div.fr-box").Each(func(_ int, section *goquery.Selection) {
		title := section.Find(".title h3").First().Clone()
		if title.Length() == 0 {
			return
		}
		title.Find("a").Remove()
		typ := aiTypeLabel(title.Text())
		if typ == "" {
			return
		}

		section.Find(".list li").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			fullURL, ok := aiArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok || seen[fullURL] {
				return
			}
			articleTitle := aiAnchorTitle(anchor)
			if !aiIsArticleTitle(articleTitle) {
				return
			}
			date, publishedAt := aiDateFields(row.Find(".time, time, .date").First().Text())
			seen[fullURL] = true
			items = append(items, model.NewsItem{
				Title:       articleTitle,
				URL:         fullURL,
				Date:        date,
				PublishedAt: publishedAt,
				Type:        typ,
				ParsedAt:    time.Now(),
			})
		})
	})

	// The bottom marquee is the homepage's "精彩瞬间" module.  Its cards are
	// real internal articles (info/1111/...), while the top .flexslider only
	// contains external promotional links and must not enter the feed.
	doc.Find(".jd-background #u_u7_demo1 td").Each(func(_ int, card *goquery.Selection) {
		anchor := card.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := aiArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := aiAnchorTitle(anchor)
		if title == "" {
			title = aiCleanText(card.Find(".titlestyle244104").First().Text())
		}
		if !aiIsArticleTitle(title) {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Type:     "精彩瞬间",
			ParsedAt: time.Now(),
		})
	})
	return items
}

func parseAdapter_aiArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ai: parse article HTML: %w", err)
	}
	if aiLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("ai: received an error or verification page instead of an article")
	}
	if pageType := aiMetaPageType(doc); pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("ai: response is not an article page (pageType=%q)", pageType)
	}

	root := doc.Find(".content .article").First()
	if root.Length() == 0 {
		root = doc.Find(".article").First()
	}
	titleSel := root.Find("h1").First()
	contentSel := aiArticleContentSelection(root)
	if root.Length() == 0 || titleSel.Length() == 0 || contentSel.Length() == 0 {
		return nil, fmt.Errorf("ai: article shell or body is missing")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = aiCleanText(titleSel.Text())
	if article.Title == "" {
		return nil, fmt.Errorf("ai: article headline is empty")
	}

	// AI uses .source rather than .news_source.  Use the scoped source block
	// first, so body text such as “来源：...” cannot become article metadata.
	article.Source = ""
	article.Date = ""
	article.PublishedAt = ""
	if sourceSel := root.Find(".source").First(); sourceSel.Length() > 0 {
		sourceText := flattenedText(sourceSel)
		if source := aiMetaValue(sourceText, "来源"); source != "" {
			article.Source = source
		}
		if rawDate := aiMetaValue(sourceText, "日期"); rawDate != "" {
			if date, publishedAt := aiDateFields(rawDate); date != "" {
				article.Date = date
				article.PublishedAt = publishedAt
			}
		}
	}
	if typ := aiArticleType(doc); typ != "" {
		article.Type = typ
	}

	aiFinalizeArticleResources(article, contentSel, baseURL)
	if bodyText := extractBodaContentText(contentSel); bodyText != "" {
		article.Content = bodyText
	} else if bodyText := aiBodyText(contentSel); bodyText != "" {
		article.Content = bodyText
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("ai: article body is empty")
	}
	return article, nil
}

func aiArticleContentSelection(scope *goquery.Selection) *goquery.Selection {
	if scope == nil || scope.Length() == 0 {
		return nil
	}
	for _, selector := range []string{
		".v_news_content",
		"#vsb_content",
		"[id^='vsb_content']",
	} {
		if found := scope.Find(selector).First(); found.Length() > 0 {
			if selector != ".v_news_content" {
				if nested := found.Find(".v_news_content").First(); nested.Length() > 0 {
					return nested
				}
			}
			return found
		}
	}
	return nil
}

func aiFinalizeArticleResources(article *model.Article, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}
	content.Find("script, style, noscript").Remove()

	images := make([]string, 0)
	seenImages := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		var src string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				src = value
				break
			}
		}
		resolved := aiResourceURL(baseURL, src)
		if resolved == "" {
			image.Remove()
			return
		}
		if seenImages[resolved] {
			image.SetAttr("src", resolved)
			return
		}
		seenImages[resolved] = true
		images = append(images, resolved)
		image.SetAttr("src", resolved)
	})
	article.Images = images

	seenAttachments := make(map[string]bool, len(article.Attachments))
	filteredAttachments := make([]model.Attachment, 0, len(article.Attachments))
	for _, attachment := range article.Attachments {
		resolved := aiResourceURL(baseURL, attachment.URL)
		if resolved == "" || seenAttachments[resolved] {
			continue
		}
		attachment.URL = resolved
		seenAttachments[resolved] = true
		filteredAttachments = append(filteredAttachments, attachment)
	}
	article.Attachments = filteredAttachments
	content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" {
			return
		}
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(strings.ToLower(href), "#") {
				anchor.Remove()
			}
			return
		}
		resolved := aiLinkURL(baseURL, href)
		if resolved == "" {
			anchor.Remove()
			return
		}
		anchor.SetAttr("href", resolved)
		if !aiIsAttachmentLink(anchor, href) {
			return
		}
		resourceURL := aiResourceURL(baseURL, resolved)
		if resourceURL == "" {
			// A direct file link on another site is still a normal external
			// link. A same-origin placeholder/challenge is not an attachment
			// and must not survive as one.
			if aiSameOriginHTTPURL(baseURL, resolved) {
				anchor.Remove()
			}
			return
		}
		resolved = resourceURL
		anchor.SetAttr("href", resolved)
		if seenAttachments[resolved] {
			return
		}
		seenAttachments[resolved] = true
		attachment := model.Attachment{
			Name: aiAttachmentName(anchor, href),
			URL:  resolved,
			Size: strings.TrimSpace(anchor.AttrOr("data-size", "")),
		}
		if attachment.Name == "" {
			return
		}
		article.Attachments = append(article.Attachments, attachment)
	})
	if htmlText, err := content.Html(); err == nil {
		article.ContentHTML = htmlText
	}
}

func aiIsAttachmentLink(anchor *goquery.Selection, href string) bool {
	if anchor == nil || anchor.Length() == 0 || href == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	lowerHref := strings.ToLower(href)
	if strings.Contains(lowerHref, "downloadattachurl") ||
		strings.Contains(lowerHref, "download.jsp") ||
		strings.Contains(lowerHref, "/download/") {
		return true
	}
	for _, attr := range []string{"download", "data-download", "data-filename", "data-name"} {
		if strings.TrimSpace(anchor.AttrOr(attr, "")) != "" {
			return true
		}
	}
	for _, value := range []string{
		href,
		anchor.AttrOr("name", ""),
		anchor.AttrOr("title", ""),
		anchor.Text(),
	} {
		if aiHasAttachmentExtension(value) {
			return true
		}
	}
	return false
}

func aiHasAttachmentExtension(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, extension := range aiAttachmentExtensions {
		if strings.Contains(lower, extension) {
			return true
		}
	}
	return false
}

func aiAttachmentName(anchor *goquery.Selection, href string) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"download", "data-filename", "data-name", "name", "title"} {
		if value := aiCleanText(anchor.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	if value := aiCleanText(anchor.Text()); value != "" {
		return value
	}
	if parsed, err := url.Parse(href); err == nil {
		value := path.Base(parsed.Path)
		if decoded, decodeErr := url.PathUnescape(value); decodeErr == nil {
			value = decoded
		}
		if value := aiCleanText(value); value != "" && value != "." && value != "/" {
			return value
		}
	}
	return ""
}

func aiArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(htmlstd.UnescapeString(href))
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
	if !aiArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	return fullURL, true
}

func aiLinkURL(baseURL, raw string) string {
	raw = strings.TrimSpace(htmlstd.UnescapeString(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		if strings.HasPrefix(strings.ToLower(raw), "#") {
			return raw
		}
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "mailto" || scheme == "tel" {
		return resolved
	}
	if parsed.Host == "" || (scheme != "http" && scheme != "https") {
		return ""
	}
	return resolved
}

// aiResourceURL only admits same-origin HTTP(S) assets. Ordinary external
// hyperlinks are normalized by aiLinkURL and retained in ContentHTML, but
// they are never silently promoted to downloadable image/attachment fields.
func aiResourceURL(baseURL, raw string) string {
	resolved := aiLinkURL(baseURL, raw)
	if resolved == "" || !aiSameOriginHTTPURL(baseURL, resolved) {
		return ""
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return ""
	}
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	if aiResourceMarkerRe.MatchString(resourceText) {
		return ""
	}
	return resolved
}

func aiSameOriginHTTPURL(baseURL, candidate string) bool {
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), base.Hostname())
}

func aiAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if title := aiCleanText(anchor.AttrOr("title", "")); title != "" {
		return title
	}
	if alt := aiCleanText(anchor.Find("img[alt]").First().AttrOr("alt", "")); alt != "" {
		return alt
	}
	return aiCleanText(anchor.Text())
}

func aiIsArticleTitle(title string) bool {
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return false
	}
	if isValidNewsTitle(title) {
		return true
	}
	// The shared title filter treats every title ending in 》 as a section
	// heading. AI's report list has real headlines such as
	// “人工智能大讲堂第29讲《面向低质量数据的知识发现关键技术》”, so only
	// retry validation without the closing book-title mark.
	if strings.HasSuffix(title, "》") {
		return isValidNewsTitle(strings.TrimSuffix(title, "》"))
	}
	return false
}

func aiDateFields(raw string) (string, string) {
	if normalized := normalizePublishedAt(raw); len(normalized) >= 10 {
		if len(normalized) > 10 {
			return normalized[:10], normalized
		}
		return normalized, ""
	}
	return "", ""
}

func aiMetaPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
}

func aiPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".main_left .menu h3", ".main_right .current h3", ".current h3"} {
		if typ := aiTypeLabel(doc.Find(selector).First().Text()); typ != "" {
			return typ
		}
	}
	return aiTypeLabel(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
}

func aiArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	// The current breadcrumb is the strongest signal on detail pages because
	// the same article may be linked from more than one home-page module.
	typ := ""
	doc.Find(".current a[href]").Each(func(_ int, anchor *goquery.Selection) {
		value := aiTypeLabel(anchor.Text())
		if value != "" && value != "首页" {
			typ = value
		}
	})
	if typ != "" {
		return typ
	}
	return aiPageType(doc)
}

func aiTypeLabel(value string) string {
	value = aiCleanText(value)
	value = strings.TrimSpace(strings.Trim(value, "[]【】"))
	if value == "" || value == "首页" || value == "正文" ||
		value == "西南交通大学人工智能研究院" || strings.EqualFold(value, "more +") {
		return ""
	}
	return value
}

func aiMetaValue(text, label string) string {
	text = htmlstd.UnescapeString(text)
	text = strings.Join(strings.Fields(text), " ")
	start := strings.Index(text, label+"：")
	separatorLength := len(label) + len("：")
	if start < 0 {
		start = strings.Index(text, label+":")
		separatorLength = len(label) + 1
	}
	if start < 0 {
		return ""
	}
	rest := text[start+separatorLength:]
	end := len(rest)
	for _, nextLabel := range []string{"来源：", "来源:", "发布者：", "发布者:", "日期：", "日期:", "点击数：", "点击数:"} {
		if index := strings.Index(rest, nextLabel); index >= 0 && index < end {
			end = index
		}
	}
	return strings.TrimSpace(rest[:end])
}

func aiBodyText(content *goquery.Selection) string {
	if content == nil || content.Length() == 0 {
		return ""
	}
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	return strings.Join(strings.Fields(htmlstd.UnescapeString(clone.Text())), " ")
}

func aiCleanText(value string) string {
	value = htmlstd.UnescapeString(value)
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func aiLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(aiCleanText(doc.Find("title").First().Text()))
	for _, marker := range []string{"404", "403", "error", "错误", "不存在", "验证码", "安全验证", "附件下载"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	body := doc.Find("body").First()
	if body.Length() > 0 && doc.Find(".article, .conlistComm").Length() == 0 {
		visible := strings.ToLower(aiCleanText(body.Text()))
		if aiErrorPageRe.MatchString(visible) {
			return true
		}
	}
	return false
}

func aiSiteBaseURL() string {
	if site := AllSites["ai"]; site != nil {
		if baseURL := strings.TrimSpace(site.BaseURL); baseURL != "" {
			return strings.TrimRight(baseURL, "/")
		}
	}
	return "https://ai.swjtu.edu.cn"
}

func aiTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "index" {
		return aiHomepageTotalPages()
	}
	if !aiKnownSubPage(slug) {
		return 0, fmt.Errorf("ai: %q is not an audited article-list route", slug)
	}
	site := AllSites["ai"]
	if site == nil {
		return 0, fmt.Errorf("ai: site registration is missing")
	}
	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(site.listPageURL(slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if total := aiTotalPagesFromHTML(body); total > 0 {
			return total, nil
		}
		lastErr = fmt.Errorf("ai: %s has no recognized article pagination", slug)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("ai: unable to read page count for %s", slug)
	}
	return 0, lastErr
}

// aiHomepageTotalPages audits the implicit "index" feed. The homepage renders
// the four article modules parsed by parseAIHomeRows plus the 精彩瞬间
// marquee and carries no pagination control, so a verified homepage is a
// one-page feed. Anything else (archive, static page, error/WAF response)
// must be rejected instead of silently counted.
func aiHomepageTotalPages() (int, error) {
	site := AllSites["ai"]
	if site == nil {
		return 0, fmt.Errorf("ai: site registration is missing")
	}
	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(site.listPageURL("index", 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if aiHomepageTotalPagesFromHTML(body) {
			return 1, nil
		}
		lastErr = fmt.Errorf("ai: index response is not the audited homepage")
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("ai: unable to read the homepage")
	}
	return 0, lastErr
}

func aiHomepageTotalPagesFromHTML(raw []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil || aiLooksLikeErrorPage(doc) {
		return false
	}
	if pageType := aiMetaPageType(doc); pageType != "" && pageType != "1" {
		return false
	}
	return len(parseAIHomeRows(doc, aiSiteBaseURL())) > 0
}

func aiKnownSubPage(slug string) bool {
	for _, known := range aiSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func aiTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil || aiLooksLikeErrorPage(doc) || doc.Find(".conlistComm").Length() == 0 {
		return 0
	}

	maxPage := 0
	doc.Find(".page-box .p_pages").Each(func(_ int, pages *goquery.Selection) {
		pages.Find(".p_no, .p_no_d").Each(func(_ int, page *goquery.Selection) {
			value := strings.TrimSpace(page.Text())
			if number, convErr := strconv.Atoi(value); convErr == nil && number > maxPage {
				maxPage = number
			}
		})
	})
	if maxPage > 0 {
		return maxPage
	}
	if doc.Find(".page-box").Length() > 0 {
		return 1
	}
	// A recognized archive without a page control is a valid one-page feed.
	return 1
}

func aiFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := aiSiteBaseURL()
	fullURL, ok := aiArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("ai: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("ai: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_aiArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("ai: parse article %s: %w", fullURL, err)
	}
	article.Images = aiVerifyImages(article.Images, baseURL)
	article.Attachments = aiVerifyAttachments(article.Attachments, baseURL)
	aiSanitizeContentHTML(article, baseURL)
	return article, body, nil
}

func aiVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := aiResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := aiFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func aiVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := aiResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := aiFetchResource(resolved, baseURL, false)
		if err != nil || !aiResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			// aiFetchResource already validates the response. Keep this branch
			// defensive if its implementation changes to return a body on error.
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

const aiMaxResourceBytes = 32 << 20

func aiFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := aiResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("ai: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("ai: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Referer", baseURL)
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || aiSameOriginHTTPURL(baseURL, next.URL.String()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("ai: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("ai: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, aiMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("ai: read resource %s: %w", resolved, err)
	}
	if len(body) > aiMaxResourceBytes || !aiResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("ai: resource %s returned an invalid shell, type, or file signature", resolved)
	}
	return body, contentType, nil
}

func aiResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if aiResourceShellRe.Match(probe) {
		return false
	}

	magic := aiResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && aiIsImageMagic(magic)
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
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values"
	case strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func aiIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func aiResourceMagic(body []byte) string {
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
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	case bytes.Contains(bytes.ToLower(body[:aiMinInt(len(body), 4096)]), []byte("<svg")):
		return "svg"
	default:
		return ""
	}
}

func aiMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func aiSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		if resolved := aiResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved := aiResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"ai-content-root\">" + article.ContentHTML + "</div>"))
	if err != nil {
		article.ContentHTML = ""
		return
	}
	root := doc.Find("#ai-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var raw string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				raw = value
				break
			}
		}
		resolved := aiResourceURL(baseURL, raw)
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(strings.ToLower(href), "#") {
				link.Remove()
			}
			return
		}
		resolved := aiLinkURL(baseURL, href)
		if resolved == "" {
			link.Remove()
			return
		}
		if aiIsAttachmentLink(link, href) && aiSameOriginHTTPURL(baseURL, resolved) {
			resourceURL := aiResourceURL(baseURL, resolved)
			if resourceURL == "" || !validAttachments[resourceURL] {
				link.Remove()
				return
			}
			resolved = resourceURL
		}
		link.SetAttr("href", resolved)
	})
	rendered, err := root.Html()
	if err != nil {
		article.ContentHTML = ""
		return
	}
	article.ContentHTML = rendered
}
