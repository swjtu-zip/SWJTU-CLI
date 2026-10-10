package scraper

import (
	"bytes"
	"fmt"
	"io"
	"mime"
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

// rvlSubPages contains only the article-bearing feeds confirmed by walking
// the home page, the global navigation, and each linked sidebar. Aggregate
// pages which merely introduce a section, profile/download pages, and the two
// empty project tabs are deliberately not registered as feeds.
var rvlSubPages = []string{
	"xwdt", "tzgg", "xshd",
	"djgz/dwgzgg", "djgz/xwdt", "djgz/jwgz",
	"kxyj/kygg", "kxyj/kyxm/gjjxm", "kxyj/kylw", "kxyj/kyzl", "kxyj/ryjl",
	"yjsjy/yjspygg", "yjsjy/zlxz",
	"xsgz/tzgg", "xsgz/gszl", "xsgz/xszx", "xsgz/xgwjxz", "xsgz/xsjy",
	"rsgz/tzgg", "rsgz/g_s",
	"sysaq", "cpyc",
}

var (
	rvlDateRe          = regexp.MustCompile(`20\d{2}\s*(?:[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}日?)(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?`)
	rvlPageIndicatorRe = regexp.MustCompile(`\b(\d+)\s*/\s*(\d+)\b`)
	rvlArticlePathRe   = regexp.MustCompile(`(?i)^/info/[^/]+/\d+\.(?:htm|html)$`)
	rvlArticleDateRe   = regexp.MustCompile(`日期\s*[:：]\s*`)
	rvlListDateRe      = regexp.MustCompile(`(?:^|[\s\x{a0}])时间\s*[:：]\s*`)
)

// rvlAdapter gives this website its own extension point. The detail fetcher
// is intentionally site-owned because the site's source/date node and its
// download endpoint are not safe to infer with the broad Boda fallback.
func init() {
	registerAdapter("rvl", SiteAdapter{
		SubPages:     rvlSubPages,
		ParseList:    parseAdapter_rvlList,
		ParseArticle: parseAdapter_rvlArticle,
		PageURL:      rvlPageURL,
		TotalPages:   rvlTotalPages,
		FetchArticle: rvlFetchArticle,
	})
}

func parseAdapter_rvlList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("rvl parse list: %w", err)
	}
	if reason := rvlBlockedReason(doc); reason != "" {
		return nil, fmt.Errorf("rvl: response is blocked or is an error page (%s)", reason)
	}

	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	if pageType == "3" {
		return nil, fmt.Errorf("rvl: detail page supplied where a list was expected")
	}

	// The home page contains both visible and hidden notice tabs. Parse every
	// tab from its own list, not only the selected tab, and keep academic-event
	// dates out of the publication fields.
	if rvlIsHomepage(doc) {
		items := rvlHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("rvl: homepage contains no local article modules")
		}
		return items, nil
	}

	if roots := doc.Find(".conlistComm"); roots.Length() > 0 {
		items := rvlDirectListItems(roots, doc, baseURL)
		if len(items) > 0 {
			return items, nil
		}
		if rvlExplicitEmptyList(roots) {
			return []model.NewsItem{}, nil
		}
		return nil, fmt.Errorf("rvl: article list container has no local article rows")
	}

	return nil, fmt.Errorf("rvl: response contains no recognisable article list")
}

func rvlIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	if doc.Find(".conlistComm").Length() > 0 {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	modules := doc.Find(".news ul li, .content-tab ul.list, .hdlist > ul > dl")
	if pageType == "1" {
		return modules.Length() > 0 || doc.Find(".activity li").Length() > 0
	}
	return pageType == "" && modules.Length() > 0
}

func rvlHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	appendItem := func(row *goquery.Selection, typ string, dateMode string) {
		item, ok := rvlNewsItemWithDateMode(row, baseURL, typ, dateMode)
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	}

	doc.Find(".news ul li").Each(func(_ int, row *goquery.Selection) {
		appendItem(row, "新闻动态", "home")
	})
	doc.Find(".content-tab ul.list").Each(func(_ int, list *goquery.Selection) {
		typ := rvlHomeTabType(list.AttrOr("id", ""))
		if typ == "" {
			typ = rvlDocumentType(doc)
		}
		list.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
			appendItem(row, typ, "home")
		})
	})
	doc.Find(".hdlist > ul > dl, .activity li").Each(func(_ int, row *goquery.Selection) {
		appendItem(row, "学术活动", "none")
	})
	return items
}

func rvlDirectListItems(roots *goquery.Selection, doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	typ := rvlDocumentType(doc)
	if typ == "" {
		typ = "通知公告"
	}
	roots.Find("dl").Each(func(_ int, row *goquery.Selection) {
		item, ok := rvlNewsItem(row, baseURL, rvlRowType(row, typ))
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	})
	return items
}

func rvlNewsItem(row *goquery.Selection, baseURL, typ string) (model.NewsItem, bool) {
	return rvlNewsItemWithDateMode(row, baseURL, typ, "list")
}

func rvlNewsItemWithDateMode(row *goquery.Selection, baseURL, typ, dateMode string) (model.NewsItem, bool) {
	if row == nil || row.Length() == 0 {
		return model.NewsItem{}, false
	}
	var anchor *goquery.Selection
	row.Find("a[href]").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
		if href := strings.TrimSpace(candidate.AttrOr("href", "")); href != "" {
			if _, ok := rvlArticleURL(baseURL, href); ok {
				anchor = candidate
				return false
			}
		}
		return true
	})
	if anchor == nil || anchor.Length() == 0 {
		return model.NewsItem{}, false
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL, ok := rvlArticleURL(baseURL, href)
	if !ok {
		return model.NewsItem{}, false
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	// Do not impose the shared byte-length limit here. Chinese headlines can
	// exceed 100 UTF-8 bytes while remaining one valid title.
	if !isValidNewsTitle(title) {
		return model.NewsItem{}, false
	}

	date, publishedAt := rvlListDate(row, dateMode)
	return model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        strings.TrimSpace(typ),
		ParsedAt:    time.Now(),
	}, true
}

func rvlListDate(row *goquery.Selection, mode string) (string, string) {
	if row == nil || row.Length() == 0 || mode == "none" {
		return "", ""
	}
	if mode == "home" {
		for _, selector := range []string{"em", "time", ".date", ".time"} {
			if value := strings.TrimSpace(row.Find(selector).First().Text()); value != "" {
				if date, publishedAt := rvlDateValues(value); date != "" {
					return date, publishedAt
				}
				if partial := rvlPartialDate(value); partial != "" {
					return partial, ""
				}
			}
		}
		return "", ""
	}

	// On the archive template only the label “时间：” is a publication field.
	// “报告时间：”, “活动时间：”, and prose dates are intentionally ignored.
	zone := row.Find("dd").First()
	if zone.Length() == 0 {
		zone = row
	}
	text := strings.Join(strings.Fields(zone.Text()), " ")
	loc := rvlListDateRe.FindStringIndex(text)
	if loc == nil {
		return "", ""
	}
	rest := strings.TrimSpace(text[loc[1]:])
	match := rvlDateRe.FindString(rest)
	if match == "" {
		return "", ""
	}
	return rvlDateValues(match)
}

func rvlPartialDate(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	match := regexp.MustCompile(`^(\d{1,2})\s*[-/.]\s*(\d{1,2})$`).FindStringSubmatch(value)
	if match == nil {
		return ""
	}
	return fmt.Sprintf("%02s-%02s", match[1], match[2])
}

func rvlDateValues(text string) (string, string) {
	match := rvlDateRe.FindString(strings.Join(strings.Fields(text), " "))
	if match == "" {
		return "", ""
	}
	precise := normalizePublishedAt(match)
	if precise == "" {
		return "", ""
	}
	if len(precise) > len("2006-01-02") {
		return precise[:10], precise
	}
	return precise, precise
}

func rvlRowType(row *goquery.Selection, fallback string) string {
	if row != nil && row.Length() > 0 {
		for _, selector := range []string{"dt span a", "dt span", ".type", ".category"} {
			if value := rvlCleanType(row.Find(selector).First().Text()); value != "" {
				return value
			}
		}
		if list := row.Closest("ul"); list.Length() > 0 {
			if value := rvlHomeTabType(list.AttrOr("id", "")); value != "" {
				return value
			}
		}
	}
	return rvlCleanType(fallback)
}

func rvlHomeTabType(id string) string {
	return map[string]string{
		"zxtz": "行政公告",
		"djgz": "党建工作",
		"kygg": "科研公告",
		"yjs":  "研究生教育",
		"xs":   "学生工作",
		"rczp": "人事工作",
	}[strings.TrimSpace(id)]
}

func rvlCleanType(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "[]【】()（） ")
	if value == "首页" || value == "正文" || value == "" {
		return ""
	}
	return value
}

func rvlDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	// The final breadcrumb anchor is the actual leaf column. This matters for
	// a graduate detail/list page where the parent is “研究生教育”.
	if current := doc.Find(".main_right .current").First(); current.Length() > 0 {
		var value string
		current.Find("a").Each(func(_ int, sel *goquery.Selection) {
			if candidate := rvlCleanType(sel.Text()); candidate != "" {
				value = candidate
			}
		})
		if value != "" {
			return value
		}
		if value := rvlCleanType(current.Find("h3").First().Text()); value != "" {
			return value
		}
	}
	for _, selector := range []string{".main_titT a", ".con_tit h3", ".main_right .current h3", ".list-title", ".list-head h1", ".list-head h2", ".list-head h3"} {
		values := doc.Find(selector)
		for i := values.Length() - 1; i >= 0; i-- {
			value := strings.TrimSpace(values.Eq(i).AttrOr("content", ""))
			if value == "" {
				value = values.Eq(i).Text()
			}
			if value = rvlCleanType(value); value != "" {
				return value
			}
		}
	}
	return ""
}

func rvlExplicitEmptyList(roots *goquery.Selection) bool {
	if roots == nil || roots.Length() == 0 {
		return false
	}
	zone := roots.Parent()
	if zone.Length() == 0 {
		zone = roots
	}
	text := strings.Join(strings.Fields(zone.Text()), " ")
	return strings.Contains(text, "共0条") || strings.Contains(text, "0/0")
}

func rvlBlockedReason(doc *goquery.Document) string {
	if doc == nil {
		return "empty document"
	}
	for _, selector := range []string{"title", `meta[name="pageTitle"]`, `meta[name="pagetitle"]`} {
		values := doc.Find(selector)
		for i := values.Length() - 1; i >= 0; i-- {
			value := strings.TrimSpace(values.Eq(i).AttrOr("content", ""))
			if value == "" {
				value = values.Eq(i).Text()
			}
			if reason := rvlBlockedMarker(value); reason != "" {
				return reason
			}
		}
	}

	// Only use visible-body markers when no known content root is present. A
	// legitimate notice may discuss a failed request or a verification code in
	// its prose; a response with no article/list structure cannot be legitimate.
	if doc.Find(".conlistComm, form[name='_newscontent_fromname'] .v_news_content, .news ul, .content-tab ul.list, .hdlist").Length() == 0 {
		visible := doc.Clone()
		visible.Find("script, style, noscript").Remove()
		if reason := rvlBlockedMarker(visible.Text()); reason != "" {
			return reason
		}
	}
	return ""
}

func rvlBlockedMarker(value string) string {
	lower := strings.ToLower(strings.Join(strings.Fields(value), " "))
	for _, marker := range []string{
		"安全验证", "请输入验证码", "验证码下载附件", "页面不存在", "访问被拒绝",
		"请求已被阻断", "403 forbidden", "403错误", "404 not found", "404错误",
		"access denied", "just a moment", "cloudflare", "系统提示",
	} {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return marker
		}
	}
	return ""
}

func parseAdapter_rvlArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("rvl parse article: %w", err)
	}
	if reason := rvlBlockedReason(doc); reason != "" {
		return nil, fmt.Errorf("rvl: response is blocked or is an error page (%s)", reason)
	}

	form := doc.Find(`form[name="_newscontent_fromname"]`).First()
	if form.Length() == 0 {
		return nil, fmt.Errorf("rvl: article form is missing")
	}
	articleRoot := form.Find(".article").First()
	if articleRoot.Length() == 0 {
		articleRoot = form
	}
	titleSel := articleRoot.Find("h1").First()
	if titleSel.Length() == 0 {
		return nil, fmt.Errorf("rvl: fixed article title node is missing")
	}
	contentSel := articleRoot.Find(".art_con .v_news_content").First()
	if contentSel.Length() == 0 {
		contentSel = articleRoot.Find(".v_news_content").First()
	}
	// Pre-2019 articles follow an older template that keeps the body
	// paragraphs in .content14 and ships an empty .v_news_content.  Fall
	// back to the wrapper only when the fixed node carries no text and no
	// resources; an empty legacy body must not mask a missing article.
	if contentSel.Length() == 0 || rvlSelectionHasNoContent(contentSel) {
		if legacy := articleRoot.Find(".content14").First(); legacy.Length() > 0 && !rvlSelectionHasNoContent(legacy) {
			contentSel = legacy
		}
	}
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("rvl: fixed article content node is missing")
	}
	contentSel = contentSel.Clone()
	contentSel.Find("script, style, noscript").Remove()
	contentSel.Find("#vsb_content, #div_vote_id").Remove()

	title := cleanTitle(titleSel.Text())
	if !isValidNewsTitle(title) {
		return nil, fmt.Errorf("rvl: invalid article title %q", title)
	}
	article := &model.Article{
		Title:   title,
		Type:    rvlArticleType(doc),
		Content: extractBodaContentText(contentSel),
	}

	meta := articleRoot.Find(".source").First()
	if meta.Length() > 0 {
		metaText := strings.Join(strings.Fields(meta.Text()), " ")
		article.Source = rvlMetaField(metaText, "来源")
		if dateText := rvlArticleDateText(metaText); dateText != "" {
			article.Date, article.PublishedAt = rvlDateValues(dateText)
		}
	}

	imageSeen := make(map[string]bool)
	contentSel.Find("img").Each(func(_ int, image *goquery.Selection) {
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				if resolved := rvlResourceURL(baseURL, value); resolved != "" && !imageSeen[resolved] {
					imageSeen[resolved] = true
					article.Images = append(article.Images, resolved)
					image.SetAttr("src", resolved)
				}
				break
			}
		}
	})

	attachmentSeen := make(map[string]bool)
	articleRoot.Find(".art_con a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved, ok := rvlAttachmentURL(baseURL, href)
		if !ok || attachmentSeen[resolved] {
			return
		}
		name := strings.TrimSpace(link.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(link.AttrOr("name", ""))
		}
		if name == "" {
			name = strings.Join(strings.Fields(link.Text()), " ")
		}
		if name == "" {
			name = rvlPathBaseFromURL(resolved)
		}
		attachmentSeen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	})

	if rendered, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = rendered
	}
	return article, nil
}

// rvlSelectionHasNoContent reports whether a candidate content node carries
// neither visible prose nor resources (images, links, tables) after
// scripts/styles are removed.
func rvlSelectionHasNoContent(sel *goquery.Selection) bool {
	if sel == nil || sel.Length() == 0 {
		return true
	}
	clone := sel.Clone()
	clone.Find("script, style, noscript").Remove()
	return strings.TrimSpace(clone.Text()) == "" && clone.Find("img, a, table").Length() == 0
}

func rvlArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if current := doc.Find(".main_right .current").First(); current.Length() > 0 {
		var value string
		current.Find("a").Each(func(_ int, link *goquery.Selection) {
			if candidate := rvlCleanType(link.Text()); candidate != "" {
				value = candidate
			}
		})
		if value != "" {
			return value
		}
		return rvlCleanType(current.Find("h3").First().Text())
	}
	return rvlDocumentType(doc)
}

func rvlMetaField(text, label string) string {
	text = strings.Join(strings.Fields(text), " ")
	index := strings.Index(text, label)
	if index < 0 {
		return ""
	}
	value := text[index+len(label):]
	value = strings.TrimLeft(value, " \t\r\n:：")
	for _, next := range []string{"来源", "发布者", "日期", "点击数", "点击量", "阅读次数"} {
		if next == label {
			continue
		}
		if nextIndex := strings.Index(value, next); nextIndex >= 0 {
			value = value[:nextIndex]
		}
	}
	return strings.TrimSpace(strings.Trim(value, ":："))
}

func rvlArticleDateText(metaText string) string {
	metaText = strings.Join(strings.Fields(metaText), " ")
	loc := rvlArticleDateRe.FindStringIndex(metaText)
	if loc == nil {
		return ""
	}
	rest := strings.TrimLeft(metaText[loc[1]:], " \t\r\n:：")
	return rvlDateRe.FindString(rest)
}

func rvlPathBaseFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if value := path.Base(parsed.Path); value != "." && value != "/" && value != "" {
		return value
	}
	return raw
}

func rvlArticleURL(baseURL, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return "", false
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" || !rvlArticlePathRe.MatchString(parsed.EscapedPath()) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func rvlAttachmentURL(baseURL, raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" || bodaIsPseudoResourceHref(raw) {
		return "", false
	}
	resolved := rvlResourceURL(baseURL, raw)
	if resolved == "" {
		return "", false
	}
	base, baseErr := url.Parse(baseURL)
	parsed, parsedErr := url.Parse(resolved)
	if baseErr != nil || parsedErr != nil || base.Hostname() == "" || parsed.Hostname() == "" || !strings.EqualFold(base.Hostname(), parsed.Hostname()) {
		return "", false
	}
	return resolved, true
}

func rvlResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
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
	markerPath := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"captcha", "verifycode", "verification", "createimage", "getcode",
		"placeholder", "transparent", "spacer", "blank.gif", "loading.gif",
		"pixel.gif", "access-denied", "waf",
	} {
		if strings.Contains(markerPath, marker) {
			return ""
		}
	}
	return resolved
}

func rvlFetchArticle(articleURL string) (*model.Article, []byte, error) {
	site := AllSites["rvl"]
	if site == nil {
		return nil, nil, fmt.Errorf("rvl: site registration is missing")
	}
	canonicalURL, ok := rvlArticleURL(site.BaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("rvl: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonicalURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_rvlArticle(body, site.BaseURL)
	if err != nil {
		return nil, nil, err
	}

	candidateImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		if resolved := rvlResourceURL(site.BaseURL, image); resolved != "" {
			candidateImages[resolved] = true
		}
	}
	candidateAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved, valid := rvlAttachmentURL(site.BaseURL, attachment.URL); valid {
			candidateAttachments[resolved] = true
		}
	}

	article.Images = rvlVerifyImages(article.Images, site.BaseURL, canonicalURL)
	article.Attachments = rvlVerifyAttachments(article.Attachments, site.BaseURL, canonicalURL)
	allowedImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		allowedImages[image] = true
	}
	allowedAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		allowedAttachments[attachment.URL] = true
	}
	article.ContentHTML = rvlSanitizeContentHTML(article.ContentHTML, site.BaseURL, allowedImages, candidateAttachments, allowedAttachments)
	return article, body, nil
}

func rvlVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := rvlResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := rvlFetchResource(resolved, referer, true)
		if err != nil || !rvlResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func rvlVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved, ok := rvlAttachmentURL(baseURL, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := rvlFetchResource(resolved, referer, false)
		if err != nil || !rvlResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func rvlFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	resolved := rvlResourceURL(referer, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("rvl: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("rvl: create resource request: %w", err)
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
		if scheme := strings.ToLower(next.URL.Scheme); scheme != "http" && scheme != "https" {
			return fmt.Errorf("rvl: refusing non-HTTP resource redirect")
		}
		if requestURL != nil && !strings.EqualFold(next.URL.Hostname(), requestURL.Hostname()) {
			return fmt.Errorf("rvl: refusing cross-site resource redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("rvl: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("rvl: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("rvl: read resource %s: %w", resolved, err)
	}
	if len(body) > 32<<20 || !rvlResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("rvl: resource %s returned invalid Content-Type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func rvlResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil && parsed != "" {
		mediaType = strings.ToLower(parsed)
	}
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
	textProbe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(probe))), "\ufeff")
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "附件下载", "请输入验证码",
		"验证码下载附件", "请先登录", "请登录后", "登录页面", "页面不存在",
		"404 not found", "access denied", "cloudflare", "just a moment",
		"安全验证", "请求已被阻断",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	magic := rvlResourceMagic(body)
	if image {
		return rvlImageTypeMatchesMagic(mediaType, magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "zip") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || strings.Contains(mediaType, "msword") || strings.Contains(mediaType, "ms-excel"):
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values":
		return true
	default:
		return magic != ""
	}
}

func rvlIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func rvlImageTypeMatchesMagic(mediaType, magic string) bool {
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

func rvlResourceMagic(body []byte) string {
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
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	case bytes.Contains(bytes.ToLower(body[:rvlMinInt(len(body), 4096)]), []byte("<svg")):
		return "svg"
	default:
		return ""
	}
}

func rvlMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func rvlSanitizeContentHTML(rawHTML, baseURL string, allowedImages, candidateAttachments, allowedAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="rvl-content-root">` + rawHTML + `</div>`))
	if err != nil {
		return ""
	}
	root := doc.Find("#rvl-content-root").First()
	root.Find("script, style, noscript").Remove()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var resolved string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				resolved = rvlResourceURL(baseURL, value)
				break
			}
		}
		if resolved == "" || !allowedImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vurl", "vsbhref"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" || strings.HasPrefix(strings.ToLower(href), "#") {
			return
		}
		if bodaIsPseudoResourceHref(href) {
			link.Remove()
			return
		}
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := rvlResourceURL(baseURL, href)
		// External file-like links are still ordinary external article links;
		// they are not local attachments and must remain in ContentHTML.
		if resolved == "" {
			if candidate := cleanURL(resolveURL(baseURL, href)); rvlSameHost(baseURL, candidate) && bodaLooksLikeAttachmentLink(link, href) {
				link.Remove()
			}
			return
		}
		if !rvlSameHost(baseURL, resolved) {
			return
		}
		if !candidateAttachments[resolved] || !allowedAttachments[resolved] {
			link.Remove()
			return
		}
		link.SetAttr("href", resolved)
	})
	result, err := root.Html()
	if err != nil {
		return ""
	}
	return result
}

func rvlSameHost(baseURL, candidate string) bool {
	base, baseErr := url.Parse(baseURL)
	parsed, parsedErr := url.Parse(candidate)
	return baseErr == nil && parsedErr == nil && base.Hostname() != "" && parsed.Hostname() != "" && strings.EqualFold(base.Hostname(), parsed.Hostname())
}

func rvlPageURL(baseURL, slug string, page, totalPages int, extension string) string {
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
	// This CMS labels the newest page as logical page 1 but stores numbered
	// pages in reverse: logical page 2 is /N-1.htm and the last is /1.htm.
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, totalPages-page+1, extension)
}

func rvlIsKnownFeed(slug string) bool {
	slug = strings.Trim(slug, "/")
	for _, known := range rvlSubPages {
		if slug == known {
			return true
		}
	}
	return false
}

func rvlTotalPages(slug string) (int, error) {
	site := AllSites["rvl"]
	if site == nil {
		return 0, fmt.Errorf("rvl: site registration is missing")
	}
	slug = strings.Trim(slug, "/")
	if slug == "index" {
		return rvlHomepageTotalPages(site.BaseURL)
	}
	if !rvlIsKnownFeed(slug) {
		return 0, fmt.Errorf("rvl: refusing unconfirmed/non-article feed %q", slug)
	}
	urlHTM := rvlPageURL(site.BaseURL, slug, 1, 0, ".htm")
	body, err := fetch(urlHTM)
	if err != nil {
		body, err = fetch(rvlPageURL(site.BaseURL, slug, 1, 0, ".html"))
	}
	if err != nil {
		return 0, err
	}
	if total := rvlTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	if rvlHasArticleRows(body) {
		return 1, nil
	}
	return 0, fmt.Errorf("rvl: list page %q has no total-page marker or article rows", slug)
}

// rvlHomepageTotalPages confirms the implicit homepage feed still exposes a
// public article list before reporting its single page.  The homepage is a
// preview page, not a reverse-numbered archive, so a confirmed portal always
// has exactly one page.
func rvlHomepageTotalPages(baseURL string) (int, error) {
	body, err := fetch(rvlPageURL(baseURL, "index", 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("rvl: fetch homepage: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("rvl: parse homepage: %w", err)
	}
	if reason := rvlBlockedReason(doc); reason != "" {
		return 0, fmt.Errorf("rvl: homepage is blocked or is an error page (%s)", reason)
	}
	if !rvlIsHomepage(doc) {
		return 0, fmt.Errorf("rvl: index route no longer serves the article portal")
	}
	if items := rvlHomepageItems(doc, baseURL); len(items) == 0 {
		return 0, fmt.Errorf("rvl: homepage contains no local article modules")
	}
	return 1, nil
}

func rvlTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err == nil {
		for _, selector := range []string{".pb_sys_common", "[id^='fanye']", ".p_pages"} {
			pagination := doc.Find(selector).First()
			if pagination.Length() == 0 {
				continue
			}
			if markup, htmlErr := pagination.Html(); htmlErr == nil {
				if total := rvlPageCountFromMarkup(markup); total > 0 {
					return total
				}
			}
			if total := rvlPageCountFromText(pagination.Text()); total > 0 {
				return total
			}
		}
	}
	return rvlPageCountFromMarkup(string(raw))
}

func rvlPageCountFromMarkup(markup string) int {
	// Matching a slash followed by a tag boundary avoids interpreting an
	// article URL such as /info/1022/20378.htm as a page count. Using markup
	// also survives minified pagination where adjacent span text becomes
	// “...161/16” after DOM text flattening.
	pageMarker := regexp.MustCompile(`(?i)(?:^|[>\s])(\d+)\s*/\s*(\d+)(?:\s*<|[^0-9]|$)`)
	for _, match := range pageMarker.FindAllStringSubmatch(markup, -1) {
		if len(match) != 3 {
			continue
		}
		current, currentErr := strconv.Atoi(match[1])
		total, totalErr := strconv.Atoi(match[2])
		if currentErr == nil && totalErr == nil && total > 0 && current > 0 && current <= total {
			return total
		}
	}
	return 0
}

func rvlPageCountFromText(text string) int {
	for _, match := range rvlPageIndicatorRe.FindAllStringSubmatch(strings.Join(strings.Fields(text), " "), -1) {
		if len(match) != 3 {
			continue
		}
		current, currentErr := strconv.Atoi(match[1])
		total, totalErr := strconv.Atoi(match[2])
		if currentErr == nil && totalErr == nil && total > 0 && current > 0 && current <= total {
			return total
		}
	}
	return 0
}

func rvlHasArticleRows(raw []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return false
	}
	found := false
	doc.Find(".conlistComm dl, .news ul li, .content-tab ul.list li, .hdlist > ul > dl, .activity li").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		row.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
			if _, ok := rvlArticleURL("http://rvl.swjtu.edu.cn", link.AttrOr("href", "")); ok {
				found = true
				return false
			}
			return true
		})
		return !found
	})
	return found
}
