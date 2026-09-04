package scraper

import (
	"bytes"
	"fmt"
	"io"
	"mime"
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

const bksyCanonicalBaseURL = "https://bksy.swjtu.edu.cn"

// bksySubPages contains only entry points that returned at least one real
// /info/<site>/<id>.htm article during the 2026-09-04 audit. The notice
// archive under /tzgg and the subject-specific navigation archive are both
// exposed because they are distinct, usable feeds.
var bksySubPages = []string{
	"tzgg/qb",
	"tzgg/jxyx",
	"tzgg/jxjs",
	"tzgg/jsfz",
	"tzgg/sjjx",
	"tzgg/syjx",
	"tzgg/cxcy",
	"tzgg/zljs",

	"jxyx/xjxg",
	"jxyx/cjxg",
	"jxyx/pkxg",
	"jxyx/xkxg",
	"jxyx/ksxg",
	"jxjs/jxgg",
	"jxjs/zyjs",
	"jxjs/kcsj",
	"jxjs/jcjs",
	"jsfz/jsjxzz",
	"syjx/syjx",
	"syjx/sysjs",
	"syjx/sfzx",
	"syjx/syjs",
	"syjx/gxhsy",
	"sjjx/sxsj",
	"sjjx/bysj_lw_",
	"sjjx/xkjs",
	"sjjx/kcxl",
	"cxcy/cxcyhd",
	"zljj/zlbz",
	"zljj/kcpg",
	"zljj/zyrz",
	"zljj/shpg",
}

var bksyArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)

var bksyAttachmentExts = []string{
	".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
	".pdf", ".zip", ".rar", ".7z", ".txt", ".csv",
}

func init() {
	registerAdapter("bksy", SiteAdapter{
		SubPages:     bksySubPages,
		ParseList:    parseAdapter_bksyList,
		ParseArticle: parseAdapter_bksyArticle,
		PageURL:      bksyAdapterPageURL,
		FetchArticle: bksyFetchArticle,
	})
}

func bksySiteBaseURL() string {
	if site := AllSites["bksy"]; site != nil {
		if baseURL := strings.TrimSpace(site.BaseURL); baseURL != "" {
			return strings.TrimRight(baseURL, "/")
		}
	}
	return bksyCanonicalBaseURL
}

// bksyAdapterPageURL maps the logical page number (1 = newest) to the
// reverse-numbered static file used by bksy.swjtu.edu.cn. Page 1 is the
// unsuffixed page; for a 243-page list, logical page 2 is /242.htm and the
// last page is /1.htm. A page after the first cannot be addressed safely
// until the source-reported total is known.
func bksyAdapterPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if baseURL == "" || slug == "" || page < 1 || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}

	if totalPages < 1 {
		return ""
	}
	fileNumber := totalPages - page + 1
	if fileNumber < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, fileNumber, extension)
}

// parseAdapter_bksyList only accepts the two real list shapes on the site:
// #content-1 on the homepage and ul.right-list in a list-container page. A
// document-wide info/ scan would pull in banners, sidebars, navigation and
// direct file links, so a missing list shape is reported as an error rather
// than being treated as a successful empty list.
func parseAdapter_bksyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	if reason := bksyResponseError(html); reason != "" {
		return nil, fmt.Errorf("bksy: rejected error/WAF response (%s)", reason)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse bksy list: %w", err)
	}

	var listItems *goquery.Selection
	var section string
	if homepage := doc.Find("#content-1").First(); homepage.Length() > 0 {
		list := homepage.Find("ul").First()
		if list.Length() == 0 {
			return nil, fmt.Errorf("bksy: homepage has no notice list")
		}
		listItems = list.ChildrenFiltered("li")
		section = bksyHomepageSection(doc)
	} else {
		list := doc.Find("main.list-container ul.right-list, .list-container ul.right-list, ul.right-list").First()
		if list.Length() == 0 {
			return nil, fmt.Errorf("bksy: response is not a homepage or list page")
		}
		listItems = list.ChildrenFiltered("li")
		section = bksyListSection(doc)
	}

	items := make([]model.NewsItem, 0, listItems.Length())
	seen := make(map[string]bool, listItems.Length())
	listItems.Each(func(_ int, item *goquery.Selection) {
		addBksyAdapterItem(&items, seen, item, baseURL, section)
	})
	return items, nil
}

func bksyHomepageSection(doc *goquery.Document) string {
	if tab := doc.Find(`.tabNav li[data-content="content-1"]`).First(); tab.Length() > 0 {
		return bksyCompactText(tab.Text())
	}
	return "全部"
}

func bksyListSection(doc *goquery.Document) string {
	for _, selector := range []string{
		"main.list-container .content-nav",
		".list-container .content-nav",
		".content-nav",
	} {
		if heading := doc.Find(selector).First(); heading.Length() > 0 {
			if value := bksyCompactText(heading.Text()); value != "" {
				return value
			}
		}
	}
	return ""
}

func addBksyAdapterItem(items *[]model.NewsItem, seen map[string]bool, item *goquery.Selection, baseURL, section string) {
	if item == nil || item.Length() == 0 || isInNavArea(item) {
		return
	}

	anchor := item
	if _, ok := item.Attr("href"); !ok {
		anchor = item.Find("a[href]").First()
	}
	if anchor.Length() == 0 {
		return
	}

	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL, ok := bksyInternalArticleURL(baseURL, href)
	if !ok || seen[fullURL] {
		return
	}

	title := ""
	for _, selector := range []string{".content", ".line1.news", ".line1"} {
		if candidate := anchor.Find(selector).First(); candidate.Length() > 0 {
			title = bksyCompactText(candidate.Text())
			if title != "" {
				break
			}
		}
	}
	if title == "" {
		title = bksyCompactText(anchor.AttrOr("title", ""))
	}
	if title == "" {
		clone := anchor.Clone()
		clone.Find("time, .time, [class*='date'], [class*='Date']").Remove()
		title = bksyCompactText(clone.Text())
	}
	title = cleanTitle(title)
	if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) || isBksyNavigationTitle(title) {
		return
	}

	date := bksyListDate(anchor)
	seen[fullURL] = true
	newsItem := model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     date,
		Type:     section,
		ParsedAt: time.Now(),
	}
	if precise := normalizePublishedAt(date); precise != "" {
		newsItem.PublishedAt = precise
	}
	*items = append(*items, newsItem)
}

func bksyListDate(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	for _, selector := range []string{"time", ".time", "[class*='date']", "[class*='Date']"} {
		if date := bksyCompactText(anchor.Find(selector).First().Text()); date != "" {
			if len(date) <= 32 && strings.Count(date, " ") <= 4 {
				return date
			}
		}
	}
	return ""
}

// bksyInternalArticleURL deliberately rejects direct downloads and external
// SPA routes in a list. Attachments in an actual bksy article are handled
// separately by appendBksyAttachments and may legitimately be cross-host.
func bksyInternalArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !bksyArticlePathRE.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	return fullURL, true
}

func parseAdapter_bksyArticle(html []byte, baseURL string) (*model.Article, error) {
	if reason := bksyResponseError(html); reason != "" {
		return nil, fmt.Errorf("bksy: rejected error/WAF response (%s)", reason)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse bksy article: %w", err)
	}
	if pageType := bksyPageType(doc); pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("bksy: pageType %q is not an article", pageType)
	}

	details := doc.Find(".left-news-details").First()
	title := details.Find(".news-title").First()
	if title.Length() == 0 {
		title = doc.Find(".news-title").First()
	}
	content := bksyContentSelection(doc)
	if title.Length() == 0 || bksyCompactText(title.Text()) == "" || content.Length() == 0 {
		return nil, fmt.Errorf("bksy: response is not an article page")
	}

	article := &model.Article{
		Title:  bksyCompactText(title.Text()),
		Type:   bksyMetaValue(details.Find(".tips .middle1").First()),
		Source: bksyMetaValue(details.Find(".tips .middle2").First()),
	}
	if details.Length() == 0 {
		article.Type = bksyMetaValue(doc.Find(".tips .middle1").First())
		article.Source = bksyMetaValue(doc.Find(".tips .middle2").First())
	}
	dateSel := details.Find(".tips .left-details").First()
	if details.Length() == 0 {
		dateSel = doc.Find(".tips .left-details").First()
	}
	if rawDate := bksyCompactText(dateSel.Text()); rawDate != "" {
		if precise := bksyDetailPublishedAt(rawDate); precise != "" {
			article.Date = precise[:10] // Article.Date is deliberately date-only.
			article.PublishedAt = precise
		}
	}

	textContent := content.Clone()
	textContent.Find("script, style, noscript").Remove()
	article.Content = extractBodaContentText(textContent)
	article.ContentHTML = bksyRenderContentHTML(content, baseURL)
	article.Images = bksyContentImages(content, baseURL)
	appendBksyAttachments(article, doc, content, baseURL)
	return article, nil
}

func bksyContentSelection(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	for _, selector := range []string{
		".left-news-details .article-content .v_news_content",
		".left-news-details .v_news_content",
		".article-content .v_news_content",
		".v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return doc.Find(".never-a-bksy-content").First()
}

func bksyMetaValue(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	value := bksyCompactText(sel.Text())
	for _, label := range []string{"分类：", "分类:", "来源：", "来源:"} {
		if strings.HasPrefix(value, label) {
			return strings.TrimSpace(strings.TrimPrefix(value, label))
		}
	}
	return value
}

func bksyPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
}

var bksyClockRe = regexp.MustCompile(`\d{1,2}(?::|时)\d{2}(?:(?::|分)\d{2}(?:秒)?)?`)

func bksyDetailPublishedAt(value string) string {
	normalized := normalizePublishedAt(value)
	if normalized == "" {
		return ""
	}
	clock := bksyClockRe.FindString(value)
	if clock == "" {
		return normalized[:10]
	}
	if strings.Contains(clock, "秒") || strings.Count(clock, ":") >= 2 {
		return normalized
	}
	if len(normalized) >= 16 {
		return normalized[:16]
	}
	return normalized
}

var bksyErrorTitleMarkers = []string{
	"404", "403", "500", "not found", "页面不存在", "错误提示", "系统错误",
	"验证码", "安全验证", "access denied", "just a moment", "cloudflare",
}

var bksyErrorBodyMarkers = []string{
	"404 not found", "403 forbidden", "500 internal server error", "页面不存在",
	"请求已被阻断", "安全威胁", "禁止访问", "请输入验证码", "验证码下载附件",
	"请完成安全验证", "正在进行安全验证", "access denied", "just a moment",
	"cloudflare", "系统错误",
}

// bksyResponseError identifies the error pages that can otherwise look like a
// successful HTML response (WAF/CAPTCHA pages, login gates and static error
// shells). It deliberately checks visible text and the title, not arbitrary
// source JavaScript, because ordinary bksy pages contain verification-related
// helper scripts.
func bksyResponseError(raw []byte) string {
	if len(raw) == 0 {
		return "empty response"
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return "invalid html"
	}

	title := strings.ToLower(bksyCompactText(doc.Find("title").First().Text()))
	for _, marker := range bksyErrorTitleMarkers {
		if strings.Contains(title, marker) {
			return "title contains " + marker
		}
	}

	visible := doc.Find("body").First()
	if visible.Length() == 0 {
		visible = doc.Find("html").First()
	}
	text := strings.ToLower(bksyCompactText(visible.Text()))
	for _, marker := range bksyErrorBodyMarkers {
		if strings.Contains(text, marker) {
			return "body contains " + marker
		}
	}
	return ""
}

// bksyResourceURL accepts only real HTTP(S) resources and removes the
// placeholder/verification URLs used by the CMS and its WAF. Ordinary links
// to another site are retained; only list/article routes are constrained to
// the bksy host.
func bksyResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	for _, prefix := range []string{"#", "data:", "javascript:", "file:", "blob:", "mailto:", "tel:"} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}

	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	resourcePath := strings.ToLower(parsed.EscapedPath() + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"captcha", "verifycode", "verification", "validcode", "createimage", "getcode",
		"placeholder", "transparent", "spacer", "blank.gif", "pixel.", "/pixel",
		"access-denied", "waf",
	} {
		if strings.Contains(resourcePath, marker) {
			return ""
		}
	}
	parsed.Fragment = ""
	return cleanURL(parsed.String())
}

func bksyContentImages(content *goquery.Selection, baseURL string) []string {
	if content == nil || content.Length() == 0 {
		return nil
	}
	images := make([]string, 0)
	seen := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if resolved := bksyResourceURL(baseURL, image.AttrOr(attr, "")); resolved != "" {
				if !seen[resolved] {
					seen[resolved] = true
					images = append(images, resolved)
				}
				return
			}
		}
	})
	return images
}

func bksyRenderContentHTML(content *goquery.Selection, baseURL string) string {
	if content == nil || content.Length() == 0 {
		return ""
	}
	html, err := content.Html()
	if err != nil {
		return ""
	}
	return bksySanitizeContentHTML(html, baseURL, nil, nil, nil)
}

// bksySanitizeContentHTML keeps exactly the selected article fragment while
// normalising resource URLs. During the live fetch path, non-nil maps are
// allow-lists produced by HTTP status/content-type/magic verification. With
// nil maps the pure fixture parser still exposes valid-looking resources
// without issuing network requests.
func bksySanitizeContentHTML(raw, baseURL string, validImages, candidateAttachments, validAttachments map[string]bool) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	wrapper := `<div id="bksy-content-root">` + raw + `</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(wrapper))
	if err != nil {
		return ""
	}
	root := doc.Find("#bksy-content-root").First()
	if root.Length() == 0 {
		return ""
	}
	root.Find("script, style, noscript").Remove()

	checkImages := validImages != nil
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := bksyResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				resolved = candidate
				break
			}
		}
		if resolved == "" || (checkImages && !validImages[resolved]) {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "srcset"} {
			image.RemoveAttr(attr)
		}
	})

	checkAttachments := candidateAttachments != nil
	root.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		originalHref := anchor.AttrOr("href", "")
		resolved := bksyResourceURL(baseURL, originalHref)
		if resolved == "" {
			if bksyLooksLikeAttachment(anchor) {
				bksyRemoveContentResource(anchor)
			} else {
				anchor.RemoveAttr("href")
			}
			return
		}
		if bksyLooksLikeAttachment(anchor) && checkAttachments && candidateAttachments[resolved] {
			if validAttachments == nil || !validAttachments[resolved] {
				bksyRemoveContentResource(anchor)
				return
			}
		}
		anchor.SetAttr("href", resolved)
	})

	result, err := root.Html()
	if err != nil {
		return ""
	}
	return result
}

func bksyRemoveContentResource(anchor *goquery.Selection) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	if row := anchor.Closest("li"); row.Length() > 0 && strings.Contains(bksyCompactText(row.Text()), "附件") {
		row.Remove()
		return
	}
	anchor.Remove()
}

func appendBksyAttachments(article *model.Article, doc *goquery.Document, content *goquery.Selection, baseURL string) {
	if article == nil || doc == nil {
		return
	}

	seen := make(map[string]int, len(article.Attachments))
	for i, attachment := range article.Attachments {
		seen[attachment.URL] = i
	}

	add := func(anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" {
			return
		}
		resolved := bksyResourceURL(baseURL, href)
		if resolved == "" {
			return
		}
		name := bksyAttachmentName(anchor)
		if name == "" {
			return
		}
		if index, ok := seen[resolved]; ok {
			if !bksyLooksLikeFilename(article.Attachments[index].Name) && bksyLooksLikeFilename(name) {
				article.Attachments[index].Name = name
			}
			return
		}
		seen[resolved] = len(article.Attachments)
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	}

	if content != nil && content.Length() > 0 {
		content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			if bksyLooksLikeAttachment(anchor) {
				add(anchor)
			}
		})
	}

	// Current bksy pages render the attachment list after .v_news_content,
	// still inside .left-news-details. Scope by the containing li's label so
	// ordinary article/sidebar links are never promoted to attachments.
	doc.Find(".left-news-details li a[href]").Each(func(_ int, anchor *goquery.Selection) {
		li := anchor.Closest("li")
		if li.Length() == 0 || !strings.Contains(bksyCompactText(li.Text()), "附件") {
			return
		}
		add(anchor)
	})
}

func bksyLooksLikeAttachment(anchor *goquery.Selection) bool {
	if anchor == nil || anchor.Length() == 0 {
		return false
	}
	href := strings.ToLower(strings.TrimSpace(anchor.AttrOr("href", "")))
	for _, value := range []string{
		anchor.AttrOr("download", ""),
		anchor.AttrOr("name", ""),
		anchor.AttrOr("title", ""),
		anchor.Text(),
	} {
		if bksyLooksLikeFilename(value) {
			return true
		}
	}
	return strings.Contains(href, "download") || strings.Contains(href, "attachment")
}

func bksyAttachmentName(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"download", "name", "title"} {
		value := bksyCompactText(anchor.AttrOr(attr, ""))
		if bksyLooksLikeFilename(value) {
			return value
		}
	}
	if value := bksyCompactText(anchor.Text()); value != "" {
		return value
	}
	for _, attr := range []string{"download", "name", "title"} {
		if value := bksyCompactText(anchor.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func bksyLooksLikeFilename(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, ext := range bksyAttachmentExts {
		if strings.Contains(value, ext) {
			return true
		}
	}
	return false
}

func bksyCompactText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

// bksyFetchArticle is the live path used by BodaSite.GetArticleSnapshot. The
// parser remains deterministic for fixtures; this hook verifies every image
// and attachment before exposing it to callers or ContentHTML.
func bksyFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := bksySiteBaseURL()
	fullURL, ok := bksyInternalArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("bksy: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("bksy: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_bksyArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("bksy: parse article %s: %w", fullURL, err)
	}

	candidateAttachments := append([]model.Attachment(nil), article.Attachments...)
	article.Images = bksyVerifyImages(article.Images, baseURL)
	article.Attachments = bksyVerifyAttachmentsAt(candidateAttachments, baseURL)
	article.ContentHTML = bksySanitizeContentHTML(
		article.ContentHTML,
		baseURL,
		bksyURLSet(article.Images),
		bksyAttachmentURLSet(candidateAttachments),
		bksyAttachmentURLSet(article.Attachments),
	)
	return article, body, nil
}

const bksyMaxResourceBytes = 32 << 20

func bksyVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := bksyResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := bksyFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func bksyVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	return bksyVerifyAttachmentsAt(attachments, baseURL)
}

func bksyVerifyAttachmentsAt(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := bksyResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := bksyFetchResource(resolved, baseURL, false)
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

func bksyURLSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func bksyAttachmentURLSet(values []model.Attachment) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[bksyResourceURL(bksyCanonicalBaseURL, value.URL)] = true
		set[value.URL] = true
	}
	return set
}

func bksyFetchResource(resourceURL, referer string, image bool) ([]byte, string, error) {
	resolved := bksyResourceURL(referer, resourceURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("invalid resource URL %q", resourceURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/pdf,application/zip,application/octet-stream,*/*;q=0.5")
	}

	client := defaultClient
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	previousRedirect := clientCopy.CheckRedirect
	clientCopy.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if scheme := strings.ToLower(next.URL.Scheme); scheme != "http" && scheme != "https" {
			return fmt.Errorf("resource redirect uses unsupported scheme %q", next.URL.Scheme)
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	resp, err := clientCopy.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch resource %s: %w", resolved, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, resp.Header.Get("Content-Type"), fmt.Errorf("resource %s: status %d", resolved, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, bksyMaxResourceBytes+1))
	if err != nil {
		return nil, resp.Header.Get("Content-Type"), fmt.Errorf("read resource %s: %w", resolved, err)
	}
	if len(body) > bksyMaxResourceBytes {
		return nil, resp.Header.Get("Content-Type"), fmt.Errorf("resource %s exceeds size limit", resolved)
	}
	contentType := resp.Header.Get("Content-Type")
	if !bksyResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("resource %s has invalid content type or magic", resolved)
	}
	return body, contentType, nil
}

func bksyResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	mediaType := bksyMediaType(contentType)
	if mediaType == "" || len(body) == 0 || bksyBodyLooksLikeHTML(body) {
		return false
	}
	if image {
		return strings.HasPrefix(mediaType, "image/") && bksyImageBodyHasMagic(body)
	}
	return bksyAttachmentBodyIsUsable(contentType, body, nameOrURL)
}

func bksyAttachmentBodyIsUsable(contentType string, body []byte, nameOrURL string) bool {
	mediaType := bksyMediaType(contentType)
	if mediaType == "" || len(body) == 0 || bksyBodyLooksLikeHTML(body) {
		return false
	}
	switch mediaType {
	case "text/html", "application/xhtml+xml", "application/json", "application/xml", "text/xml":
		return false
	}

	kind := bksyAttachmentKind(nameOrURL)
	if kind == "" {
		switch mediaType {
		case "application/pdf":
			kind = "pdf"
		case "application/zip", "application/x-zip-compressed":
			kind = "zip"
		case "application/vnd.ms-office", "application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint":
			kind = "ole"
		}
	}

	switch kind {
	case "pdf":
		return bytes.HasPrefix(body, []byte("%PDF-"))
	case "zip":
		return bksyHasZIPMagic(body)
	case "ole":
		return bytes.HasPrefix(body, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1})
	case "rar":
		return bytes.HasPrefix(body, []byte("Rar!\x1a\x07"))
	case "7z":
		return bytes.HasPrefix(body, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c})
	case "text":
		return strings.HasPrefix(mediaType, "text/") || mediaType == "application/csv"
	}
	if strings.HasPrefix(mediaType, "text/") || mediaType == "application/csv" {
		return true
	}
	return bksyBinaryBodyKind(body) != ""
}

func bksyMediaType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func bksyBodyLooksLikeHTML(body []byte) bool {
	limit := len(body)
	if limit > 65536 {
		limit = 65536
	}
	probe := bytes.ToLower(body[:limit])
	for _, marker := range [][]byte{
		[]byte("<!doctype html"), []byte("<html"), []byte("<head"), []byte("<body"),
		[]byte("验证码下载附件"), []byte("请输入验证码"), []byte("access denied"),
		[]byte("cloudflare"), []byte("just a moment"),
	} {
		if bytes.Contains(probe, marker) {
			return true
		}
	}
	return false
}

func bksyImageBodyHasMagic(body []byte) bool {
	return strings.HasPrefix(bksyBinaryBodyKind(body), "image")
}

func bksyHasZIPMagic(body []byte) bool {
	return len(body) >= 4 && body[0] == 'P' && body[1] == 'K' &&
		((body[2] == 3 && body[3] == 4) || (body[2] == 5 && body[3] == 6) || (body[2] == 7 && body[3] == 8))
}

func bksyBinaryBodyKind(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case bksyHasZIPMagic(body):
		return "zip"
	case bytes.HasPrefix(body, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}):
		return "ole"
	case bytes.HasPrefix(body, []byte("Rar!\x1a\x07")):
		return "rar"
	case bytes.HasPrefix(body, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	case bytes.HasPrefix(body, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(body, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png"
	case bytes.HasPrefix(body, []byte("GIF87a")), bytes.HasPrefix(body, []byte("GIF89a")):
		return "image/gif"
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		return "image/webp"
	case bytes.HasPrefix(body, []byte("BM")):
		return "image/bmp"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{'I', 'I', '*', 0}) || bytes.Equal(body[:4], []byte{'M', 'M', 0, '*'})):
		return "image/tiff"
	}
	trimmed := bytes.ToLower(bytes.TrimSpace(body))
	if bytes.HasPrefix(trimmed, []byte("<svg")) || (bytes.HasPrefix(trimmed, []byte("<?xml")) && bytes.Contains(trimmed[:min(len(trimmed), 4096)], []byte("<svg"))) {
		return "image/svg"
	}
	return ""
}

func bksyAttachmentKind(value string) string {
	lower := strings.ToLower(value)
	for _, candidate := range []struct {
		exts []string
		kind string
	}{
		{[]string{".docx", ".xlsx", ".pptx", ".zip"}, "zip"},
		{[]string{".doc", ".xls", ".ppt"}, "ole"},
		{[]string{".pdf"}, "pdf"},
		{[]string{".rar"}, "rar"},
		{[]string{".7z"}, "7z"},
		{[]string{".txt", ".csv"}, "text"},
	} {
		for _, ext := range candidate.exts {
			if strings.Contains(lower, ext) {
				return candidate.kind
			}
		}
	}
	return ""
}
