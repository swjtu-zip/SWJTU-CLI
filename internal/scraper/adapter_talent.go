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

// The home page exposes three tabbed lists and a separate news-dynamics list.
// The recruitment page and the service-platform link are a landing page and a
// CAS entry point, respectively, so neither is an article feed.
var talentSubPages = []string{"xwzx/rczc", "xwzx/tzgg", "xwzx/xwdt", "xwzx/xzzx"}

var talentFeedTypes = map[string]string{
	"xwzx/rczc": "人才政策",
	"xwzx/tzgg": "通知公告",
	"xwzx/xwdt": "新闻动态",
	"xwzx/xzzx": "下载中心",
}

// The column number is stable in the site's /info/<column>/<article>.htm
// URLs. It is only a fallback; the detail breadcrumb remains authoritative.
var talentColumnTypes = map[string]string{
	"1131": "新闻动态",
	"1151": "通知公告",
	"1161": "人才政策",
	"1171": "下载中心",
}

var talentArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var talentDateRE = regexp.MustCompile(`(20\d{2})\s*(?:[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})日?|年\s*(\d{1,2})月\s*(\d{1,2})日?)`)

const talentMaxResourceBytes = 32 << 20

func init() {
	registerAdapter("talent", SiteAdapter{
		SubPages:     talentSubPages,
		ParseList:    parseAdapter_talentList,
		ParseArticle: parseAdapter_talentArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   talentTotalPages,
		FetchArticle: talentFetchArticle,
	})
}

func parseAdapter_talentList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if talentResponseLooksLikeChallenge(raw) {
		return nil, fmt.Errorf("talent: response is a WAF or verification page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("talent: parse list HTML: %w", err)
	}

	if talentIsHomepage(doc) {
		items := talentHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("talent: homepage contains no scoped article entries")
		}
		return items, nil
	}

	list := talentListContainer(doc)
	if list.Length() == 0 {
		return nil, fmt.Errorf("talent: response has no article-list container")
	}
	typ := talentDocumentType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	rows := list.Find("li.list-item")
	if rows.Length() == 0 {
		rows = list.ChildrenFiltered("li")
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		talentAppendItem(&items, seen, talentListAnchor(row, baseURL), row, typ, baseURL)
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("talent: article-list container contains no valid local articles")
	}
	return items, nil
}

func talentListContainer(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	return doc.Find("ul.news-list.news-list-container, ul.news-list.news-else-container, ul.news-list").First()
}

func talentIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" ||
		doc.Find(".news-information ul.list-gonggao").Length() > 0
}

func talentHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".news-information ul.list-gonggao").Each(func(_ int, list *goquery.Selection) {
		typ := talentHomeListType(list)
		list.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			talentAppendItem(&items, seen, anchor, anchor.Closest("li"), typ, baseURL)
		})
	})
	return items
}

func talentHomeListType(list *goquery.Selection) string {
	if list == nil || list.Length() == 0 {
		return ""
	}
	if box := list.Closest(".col-1"); box.Length() > 0 {
		if value := talentText(box.Find(".news-title").First().Text()); value != "" {
			return talentKnownType(value)
		}
	}
	if panel := list.Closest(".tabcons"); panel.Length() > 0 {
		switch strings.TrimSpace(panel.AttrOr("id", "")) {
		case "content-1":
			return "通知公告"
		case "content-2":
			return "人才政策"
		case "content-3":
			return "下载中心"
		}
	}
	return ""
}

func talentKnownType(value string) string {
	value = talentText(value)
	for _, typ := range []string{"新闻动态", "通知公告", "人才政策", "下载中心"} {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func talentDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	values := []string{
		doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""),
		doc.Find(".el-breadcrumb .current-text").First().Text(),
		doc.Find(".list-title, .list-head").First().Text(),
	}
	for _, value := range values {
		if typ := talentKnownType(value); typ != "" {
			return typ
		}
	}
	return ""
}

func talentListAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	if row == nil || row.Length() == 0 {
		return nil
	}
	var result *goquery.Selection
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := talentArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			result = anchor
			return false
		}
		return true
	})
	return result
}

func talentAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	fullURL, ok := talentArticleURL(baseURL, anchor.AttrOr("href", ""))
	if !ok || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".title, .line1, .line2, .news-title, h3, h4").First().Text())
	}
	if title == "" && row != nil && row.Length() > 0 {
		// The current list template nests a second <a> inside the row's main
		// anchor (the MORE+ and date links), so the HTML parser closes the
		// outer anchor before the .title box and the title becomes the
		// anchor's sibling. Fall back to the row before using anchor text,
		// which would otherwise be just the split date ("01.14 2026").
		title = strings.TrimSpace(row.Find(".title, .line1, .line2, .news-title, h3, h4").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}

	date, publishedAt := talentListDate(row)
	if typ == "" && row != nil && row.Length() > 0 {
		typ = talentKnownType(row.Find(".tag, .category, .type").First().Text())
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

func talentListDate(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	for _, selector := range []string{
		".tips .f-s-20", ".tips time", ".tips .date", ".tips .publish-date",
		".f-s-20", ".date", ".publish-date", "time",
	} {
		if node := row.Find(selector).First(); node.Length() > 0 {
			if date, publishedAt := talentDateValues(node.Text()); date != "" {
				return date, publishedAt
			}
		}
	}
	return "", ""
}

func talentDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "", ""
	}
	if match := dateTimeRe.FindString(value); match != "" {
		if published := normalizePublishedAt(match); len(published) >= 10 {
			return published[:10], published
		}
	}
	if match := talentDateRE.FindStringSubmatch(value); match != nil {
		month, day := match[2], match[3]
		if month == "" {
			month, day = match[4], match[5]
		}
		date := normalizeDateParts(match[1], month, day)
		return date, date
	}
	return "", ""
}

func talentTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if talentFeedTypes[slug] == "" {
		return 0, fmt.Errorf("talent: %q is not an article-list route", slug)
	}
	baseURL := talentSiteBaseURL()
	body, err := talentFetchListFirstPage(baseURL, slug)
	if err != nil {
		return 0, fmt.Errorf("talent: fetch first page for %s: %w", slug, err)
	}
	if _, err := parseAdapter_talentList(body, baseURL); err != nil {
		return 0, fmt.Errorf("talent: first page for %s is not a valid article list: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("talent: parse first page for %s: %w", slug, err)
	}
	return talentPageCount(doc), nil
}

func talentFetchListFirstPage(baseURL, slug string) ([]byte, error) {
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err == nil {
		return body, nil
	}
	if fallback, fallbackErr := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".html")); fallbackErr == nil {
		return fallback, nil
	}
	return nil, err
}

func talentPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 1
	}
	pages := doc.Find(".p_pages").First()
	if pages.Length() == 0 {
		return 1
	}
	maxPage := 0
	pages.Find(".p_no, .p_no_d").Each(func(_ int, node *goquery.Selection) {
		if value, err := strconv.Atoi(strings.TrimSpace(node.Text())); err == nil && value > maxPage {
			maxPage = value
		}
	})
	if maxPage > 0 {
		return maxPage
	}
	// Keep this fallback scoped to the pagination node. It handles a template
	// which omits the p_no classes without mistaking article IDs for pages.
	pages.Find("a").Each(func(_ int, node *goquery.Selection) {
		if value, err := strconv.Atoi(strings.TrimSpace(node.Text())); err == nil && value > maxPage {
			maxPage = value
		}
	})
	if maxPage > 0 {
		return maxPage
	}
	return 1
}

func parseAdapter_talentArticle(raw []byte, baseURL string) (*model.Article, error) {
	if talentResponseLooksLikeChallenge(raw) {
		return nil, fmt.Errorf("talent: response is a WAF or verification page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("talent: parse article HTML: %w", err)
	}
	title := talentArticleTitle(doc)
	contentSel := talentArticleContent(doc)
	if title == "" {
		return nil, fmt.Errorf("talent: response has no fixed article title")
	}
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("talent: response has no fixed article body")
	}

	date, publishedAt := talentFixedPublication(doc)
	article := &model.Article{
		Title:       title,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        talentArticleType(doc, baseURL),
		Source:      talentFixedSource(doc),
		Content:     talentContentText(contentSel, baseURL),
	}
	article.ContentHTML, article.Images, article.Attachments = talentRenderContent(contentSel, baseURL)
	talentAppendOutsideAttachments(article, doc, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func talentArticleTitle(doc *goquery.Document) string {
	for _, selector := range []string{
		".news-detail .top-part h5.title",
		".news-detail .top-part .title",
		".top-part h5.title",
		".article-title",
		".detail-title",
		".news-title",
	} {
		if title := talentText(doc.Find(selector).First().Text()); title != "" {
			return title
		}
	}
	return ""
}

func talentArticleContent(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		".news-detail .content #vsb_content .v_news_content",
		".news-detail .content #vsb_content_501 .v_news_content",
		".news-detail .content .v_news_content",
		"#vsb_content_501 .v_news_content",
		"#vsb_content .v_news_content",
		".v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return &goquery.Selection{}
}

func talentArticleMeta(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		".news-detail .tips",
		".news_source",
		".article-meta",
		".detail-meta",
	} {
		if meta := doc.Find(selector).First(); meta.Length() > 0 {
			return meta
		}
	}
	return &goquery.Selection{}
}

func talentFixedPublication(doc *goquery.Document) (string, string) {
	meta := talentArticleMeta(doc)
	if meta.Length() == 0 {
		return "", ""
	}
	nodes := meta.Find("span, time, .date, .publish-date, .publish-time")
	if nodes.Length() == 0 {
		nodes = meta
	}
	var date, publishedAt string
	nodes.EachWithBreak(func(_ int, node *goquery.Selection) bool {
		value := talentText(node.Text())
		if value == "" {
			return true
		}
		labeled := strings.Contains(value, "发布时间") || strings.Contains(value, "发布日期") || strings.Contains(value, "日期")

		if !labeled && node.Get(0) != nil && !node.Is("time") && !node.HasClass("date") && !node.HasClass("publish-date") && !node.HasClass("publish-time") {
			return true
		}
		if parsedDate, parsedPublishedAt := talentDateValues(value); parsedDate != "" {
			date, publishedAt = parsedDate, parsedPublishedAt
			return false
		}
		return true
	})
	return date, publishedAt
}

func talentFixedSource(doc *goquery.Document) string {
	meta := talentArticleMeta(doc)
	if meta.Length() == 0 {
		return ""
	}
	var source string
	meta.Find("span, .source").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		value := talentText(node.Text())
		if strings.HasPrefix(value, "来源：") {
			source = strings.TrimSpace(strings.TrimPrefix(value, "来源："))
			return false
		}
		if strings.HasPrefix(value, "来源:") {
			source = strings.TrimSpace(strings.TrimPrefix(value, "来源:"))
			return false
		}
		return true
	})
	return source
}

func talentArticleType(doc *goquery.Document, articleURL string) string {
	if doc != nil {
		if breadcrumb := talentKnownType(doc.Find(".news-detail .el-breadcrumb .container, .el-breadcrumb .container").First().Text()); breadcrumb != "" {
			return breadcrumb
		}
		for _, selector := range []string{".list-title", ".article-category", ".article_category_bg h2", ".list-head"} {
			if typ := talentKnownType(doc.Find(selector).First().Text()); typ != "" {
				return typ
			}
		}
	}
	parsed, err := url.Parse(strings.TrimSpace(articleURL))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) >= 2 {
		return talentColumnTypes[parts[len(parts)-2]]
	}
	return ""
}

func talentContentText(contentSel *goquery.Selection, baseURL string) string {
	if contentSel == nil || contentSel.Length() == 0 {
		return ""
	}
	textSel := contentSel.Clone()
	textSel.Find("script, style, noscript").Remove()
	textSel.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !talentIsAttachmentCandidate(link, href, baseURL) {
			return
		}
		if row := link.Closest("li"); row.Length() > 0 && strings.Contains(row.Text(), "附件") {
			row.Remove()
			return
		}
		link.Remove()
	})

	parts := make([]string, 0)
	appendText := func(selection *goquery.Selection) {
		if value := talentText(selection.Text()); value != "" {
			parts = append(parts, value)
		}
	}
	textSel.Find("p, h1, h2, h3, h4, li, td, th, div").Each(func(_ int, node *goquery.Selection) {
		data := node.Get(0)
		if data == nil {
			return
		}
		switch strings.ToLower(data.Data) {
		case "p", "h1", "h2", "h3", "h4":
			appendText(node)
		case "li":
			if node.Find("p, h1, h2, h3, h4, li, td, th").Length() == 0 {
				appendText(node)
			}
		case "td", "th":
			if node.Find("p, h1, h2, h3, h4, li, td, th").Length() == 0 {
				appendText(node)
			}
		case "div":
			if node.Closest("p, h1, h2, h3, h4, li, td, th").Length() > 0 || node.Find("p, h1, h2, h3, h4, li, td, th, div").Length() > 0 || node.Find("img").Length() > 0 {
				return
			}
			appendText(node)
		}
	})
	if len(parts) == 0 {
		return talentText(textSel.Text())
	}
	return strings.Join(parts, "\n\n")
}

func talentRenderContent(contentSel *goquery.Selection, baseURL string) (string, []string, []model.Attachment) {
	root := contentSel.Clone()
	root.Find("script, style, noscript").Remove()
	images := make([]string, 0)
	attachments := make([]model.Attachment, 0)
	seenImages := make(map[string]bool)
	seenAttachments := make(map[string]bool)

	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		raw := talentFirstAttr(image, "src", "data-src", "data-original", "data-lazy-src", "orisrc")
		resolved := talentResourceURL(baseURL, raw)
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		if !seenImages[resolved] {
			seenImages[resolved] = true
			images = append(images, resolved)
		}
	})

	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		lower := strings.ToLower(href)
		isAttachment := talentIsAttachmentCandidate(link, href, baseURL)
		if strings.HasPrefix(lower, "#") {
			if isAttachment {
				link.Remove()
			}
			return
		}
		if strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") {
			return
		}
		if bodaIsPseudoResourceHref(href) {
			link.Remove()
			return
		}
		resolved := talentResourceURL(baseURL, href)
		if resolved == "" {
			link.Remove()
			return
		}
		link.SetAttr("href", resolved)
		if isAttachment && !seenAttachments[resolved] {
			seenAttachments[resolved] = true
			attachments = append(attachments, model.Attachment{Name: talentAttachmentName(link, resolved), URL: resolved})
		}
	})

	rendered, err := root.Html()
	if err != nil {
		return "", images, attachments
	}
	return rendered, images, attachments
}

func talentAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved := talentResourceURL(baseURL, attachment.URL); resolved != "" {
			seen[resolved] = true
		}
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if _, ok := talentArticleURL(baseURL, href); ok || !talentIsAttachmentCandidate(link, href, baseURL) {
			return
		}
		resolved := talentResourceURL(baseURL, href)
		if resolved == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{
			Name: talentAttachmentName(link, resolved),
			URL:  resolved,
		})
	})
}

func talentAttachmentName(link *goquery.Selection, resolved string) string {
	for _, value := range []string{
		link.AttrOr("download", ""),
		link.AttrOr("data-filename", ""),
		link.AttrOr("name", ""),
		link.AttrOr("title", ""),
		link.Text(),
	} {
		value = talentText(value)
		if value != "" && value != "下载" && value != "点击下载" && value != "附件" {
			return value
		}
	}
	if parsed, err := url.Parse(resolved); err == nil {
		if name := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]; name != "" {
			if decoded, err := url.PathUnescape(name); err == nil {
				return decoded
			}
			return name
		}
	}
	return resolved
}

func talentIsAttachmentCandidate(link *goquery.Selection, href, baseURL string) bool {
	if link == nil || link.Length() == 0 || href == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	lowerHref := strings.ToLower(href)
	if strings.TrimSpace(link.AttrOr("download", "")) != "" || strings.Contains(lowerHref, "download.jsp") || strings.Contains(lowerHref, "downloadattachurl") {
		return true
	}
	for _, selector := range []string{".attachment", ".attachments", ".news-attachment", ".download"} {
		if ancestor := link.Closest(selector); ancestor.Length() > 0 && strings.Contains(ancestor.Text(), "附件") {
			return true
		}
	}
	if row := link.Closest("li"); row.Length() > 0 && strings.Contains(row.Text(), "附件") {
		return true
	}

	resolved := talentResourceURL(baseURL, href)
	if resolved == "" {
		return false
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return false
	}
	if !talentSameOriginHTTPURL(baseURL, resolved) {
		// An external .pdf/.doc link is commonly an ordinary reference. Keep it
		// as a body link unless the markup explicitly calls it an attachment.
		return false
	}
	lowerPath := strings.ToLower(parsed.Path)
	for _, ext := range bodaAttachmentExts {
		if strings.HasSuffix(lowerPath, ext) || strings.HasSuffix(strings.ToLower(link.AttrOr("name", "")), ext) {
			return true
		}
	}
	return false
}

func talentFirstAttr(selection *goquery.Selection, attrs ...string) string {
	for _, attr := range attrs {
		if value := strings.TrimSpace(selection.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func talentText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func talentArticleURL(baseURL, raw string) (string, bool) {
	resolved := talentResourceURL(baseURL, raw)
	if resolved == "" || !talentSameOriginHTTPURL(baseURL, resolved) || !talentArticlePathRE.MatchString(mustParseURLPath(resolved)) || !isValidNewsURL(resolved) {
		return "", false
	}
	return resolved, true
}

func mustParseURLPath(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Path
}

func talentResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Hostname() == "" {
		return ""
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(ref)
	scheme := strings.ToLower(resolved.Scheme)
	if resolved.Hostname() == "" || (scheme != "http" && scheme != "https") {
		return ""
	}
	resolved.Fragment = ""
	return resolved.String()
}

func talentSameOriginHTTPURL(baseURL, candidate string) bool {
	base, baseErr := url.Parse(strings.TrimSpace(baseURL))
	parsed, candidateErr := url.Parse(strings.TrimSpace(candidate))
	if baseErr != nil || candidateErr != nil || base.Hostname() == "" || parsed.Hostname() == "" {
		return false
	}
	baseScheme, candidateScheme := strings.ToLower(base.Scheme), strings.ToLower(parsed.Scheme)
	if (baseScheme != "http" && baseScheme != "https") || (candidateScheme != "http" && candidateScheme != "https") {
		return false
	}
	if !strings.EqualFold(base.Hostname(), parsed.Hostname()) {
		return false
	}
	return base.Port() == parsed.Port()
}

func talentSiteBaseURL() string {
	if site := AllSites["talent"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return "https://talent.swjtu.edu.cn"
}

func talentFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := talentSiteBaseURL()
	fullURL, ok := talentArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("talent: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("talent: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_talentArticle(body, fullURL)
	if err != nil {
		return nil, body, fmt.Errorf("talent: parse article %s: %w", fullURL, err)
	}
	article.Images = talentVerifyImages(article.Images, fullURL)
	article.Attachments = talentVerifyAttachments(article.Attachments, fullURL, fullURL)
	talentSanitizeContentHTML(article, fullURL)
	return article, body, nil
}

func talentVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved := talentResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := talentFetchResource(resolved, baseURL, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func talentVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := talentResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := talentFetchResource(resolved, baseURL, referer, false)
		if err != nil || !talentResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func talentFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	resolved := talentResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("talent: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("talent: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Host, via[len(via)-1].URL.Host) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("talent: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("talent: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, talentMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("talent: read resource %s: %w", resolved, err)
	}
	if len(body) > talentMaxResourceBytes || !talentResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("talent: resource %s returned an invalid type, shell, or file signature", resolved)
	}
	return body, contentType, nil
}

func talentResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	lowerProbe := strings.ToLower(strings.TrimSpace(string(probe)))
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "captcha", "请输入验证码", "验证码下载附件",
		"请先登录", "请登录后", "cloudflare", "just a moment", "安全验证", "请求已被阻断",
	} {
		if strings.Contains(lowerProbe, marker) {
			return false
		}
	}

	magic := talentResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && talentImageMediaMatchesMagic(mediaType, magic)
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
	case mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values":
		return true
	default:
		return magic != ""
	}
}

func talentImageMediaMatchesMagic(mediaType, magic string) bool {
	switch {
	case mediaType == "image/jpeg":
		return magic == "jpeg"
	case mediaType == "image/png":
		return magic == "png"
	case mediaType == "image/gif":
		return magic == "gif"
	case mediaType == "image/webp":
		return magic == "webp"
	case mediaType == "image/bmp":
		return magic == "bmp"
	case mediaType == "image/tiff":
		return magic == "tiff"
	case mediaType == "image/svg+xml":
		return magic == "svg"
	default:
		return false
	}
}

func talentResourceMagic(body []byte) string {
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
	case bytes.Contains(bytes.ToLower(body[:minTalentInt(len(body), 4096)]), []byte("<svg")):
		return "svg"
	default:
		return ""
	}
}

func minTalentInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func talentSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		if resolved := talentResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved := talentResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"talent-content-root\">" + article.ContentHTML + "</div>"))
	if err != nil {
		article.ContentHTML = ""
		return
	}
	root := doc.Find("#talent-content-root").First()
	root.Find("script, style, noscript").Remove()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := talentResourceURL(baseURL, talentFirstAttr(image, "src", "data-src", "data-original", "data-lazy-src", "orisrc"))
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		lower := strings.ToLower(href)
		if strings.HasPrefix(lower, "#") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") {
			if strings.HasPrefix(lower, "#") && talentIsAttachmentCandidate(link, href, baseURL) {
				link.Remove()
			}
			return
		}
		if bodaIsPseudoResourceHref(href) {
			link.Remove()
			return
		}
		resolved := talentResourceURL(baseURL, href)
		if resolved == "" {
			link.Remove()
			return
		}
		if talentIsAttachmentCandidate(link, href, baseURL) && !validAttachments[resolved] {
			link.Remove()
			return
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

func talentResponseLooksLikeChallenge(raw []byte) bool {
	probe := raw
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	lower := strings.ToLower(string(probe))
	for _, marker := range []string{
		"just a moment", "cf-chl-", "challenge-platform", "access denied", "403 forbidden",
		"captcha", "请输入验证码", "安全验证", "请求已被阻断", "访问过于频繁",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
