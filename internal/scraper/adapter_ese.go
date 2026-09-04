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

// ese.swjtu.edu.cn numbers generated list pages from oldest to newest while
// the site presents page 1 as the newest page. Keep the site's feeds and URL
// mapping in this adapter instead of changing the shared Boda implementation.
func init() {
	registerAdapter("ese", SiteAdapter{
		SubPages:     eseSubPages,
		ParseList:    parseAdapter_eseList,
		ParseArticle: parseAdapter_eseArticle,
		PageURL:      esePageURL,
		TotalPages:   eseTotalPages,
		FetchArticle: eseFetchArticle,
	})
}

// These are the navigation leaves that currently expose real internal
// article rows. Profile/static pages, external links, and placeholder (#)
// pages are deliberately not presented as news feeds. Platform/download
// pages remain here when they expose a real CMS article list.
var eseSubPages = []string{
	"index/xwgg/zyxw",
	"index/xwgg/hjyw",
	"index/xwgg/tzgg1/qb",
	"index/xwgg/tzgg1/dzbg",
	"index/xwgg/tzgg1/bks",
	"index/xwgg/tzgg1/yjs",
	"index/xwgg/tzgg1/kygz",
	"index/xwgg/tzgg1/xsgz",
	"index/xwgg/tzgg1/qt",
	"djgz/djdt",
	"djgz/jwgz",
	"djgz/ghgz",
	"kxyj/xsdt",
	"xsgz/tzjgg",
	"xsgz/txdt",
	"xsgz/xsfc",
	"xywh/hjsg",
	"xyyd/xyqy",
	"index/cyxz",
	"jyjx/jxcg/jxcgj",
	"jyjx/rcpy/yjspy",
	"jyjx/jypt/syjx/bksyjxxm",
	"jyjx/jypt/syjx/yjssjjd",
	"jyjx/jypt/syjx/syjxcyxz",
	"jyjx/jypt/syjx/cxsyxm",
	"jyjx/jypt/sysaq/sysaqgd",
	"jyjx/jypt/sysaq/aqjsal",
	"jyjx/rcpy/bkspy",
	"kxyj/kypt",
	"kxyj/kytd",
	"szdw/wjxz",
	"xsgz/wjxz",
	"xywh/whhd",
	"xywh/whly",
	"xyyd/xyfc",
	"xyyd/xyhbqysy",
	"cpyc/szzp",
	"cpyc/zzbshzp",
}

var eseAttachmentExtensions = []string{
	".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf",
	".zip", ".rar", ".7z", ".txt", ".csv",
}

var (
	eseAdapterPDFImagePathRe        = regexp.MustCompile(`(?i)["']((?:https?://|/|\.\.?/)[^"']+\.(?:jpg|jpeg|png|gif|webp)(?:\?[^"']*)?)["']`)
	eseAdapterPDFFileRe             = regexp.MustCompile(`(?i)showVsbpdfIframe\s*\(\s*["']([^"']+\.pdf(?:\?[^"']*)?)["']`)
	eseAdapterPublicationLabelRe    = regexp.MustCompile(`(?i)(?:^|[\s\x{a0}：:，。；;、|])(?:发布时间|发布日期|发表时间|发表日期|日期|时间)\s*[:：]\s*`)
	eseAdapterPublicationDateRe     = regexp.MustCompile(`20\d{2}\s*[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}(?:\s*(?:日|号))?(?:[ Tt]+\s*\d{1,2}\s*[:：]\s*\d{2}(?:\s*[:：]\s*\d{2})?)?`)
	eseAdapterResourceMarkerRe      = regexp.MustCompile(`(?i)/(?:spacer|blank|transparent|placeholder|noimage|no-image|loading|captcha|verify|waf|login|access[-_]denied)(?:[./?]|$)|/pixel\.gif(?:[?]|$)|createimage\.jsp`)
	eseAdapterResourceShellMarkerRe = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|您访问的页面不存在|无权限访问|access\s+denied|request\s+blocked|just\s+a\s+moment|cloudflare|安全验证|安全威胁|请求已被阻断`)
)

const eseAdapterMaxResourceBytes = 32 << 20

// esePageURL maps logical pages (1 = newest) to ESE's reverse-numbered files.
// The first page is always the unsuffixed list page, as on the live site. A
// later page requires a known in-range total; returning an empty URL prevents
// an unknown or out-of-range request from becoming a duplicate old page.
func esePageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page < 1 {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages <= 1 || page > totalPages {
		return ""
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func eseTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if !eseKnownSubPage(slug) {
		return 0, fmt.Errorf("ese: %q is not an audited article-list route", slug)
	}
	site := AllSites["ese"]
	if site == nil {
		return 0, fmt.Errorf("ese: site registration is missing")
	}

	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(esePageURL(site.BaseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if total := eseTotalPagesFromHTML(body); total > 0 {
			return total, nil
		}
		lastErr = fmt.Errorf("ese: %s has no recognized pagination", slug)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("ese: unable to read page count for %s", slug)
	}
	return 0, lastErr
}

func eseKnownSubPage(slug string) bool {
	for _, known := range eseSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func eseTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil || eseAdapterLooksLikeErrorPage(doc) {
		return 0
	}
	if _, recognized := eseAdapterListRows(doc); !recognized {
		return 0
	}
	if total := parseTotalPages(raw); total > 0 {
		return total
	}
	if doc.Find(".p_pages").Length() > 0 {
		// A recognized list with only disabled page-1 controls is a legal
		// empty/single-page list, not an unknown multi-page source.
		return 1
	}
	return 0
}

func parseAdapter_eseList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ese: parse list HTML: %w", err)
	}
	if eseAdapterLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("ese: received an error or verification page instead of a news list")
	}

	if eseAdapterHasHomeSections(doc) {
		items := parseEseAdapterHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("ese: home page has no recognized article rows")
		}
		return items, nil
	}

	rows, recognized := eseAdapterListRows(doc)
	if !recognized {
		return nil, fmt.Errorf("ese: page has no recognized article list")
	}
	// A recognized list with no valid internal article links is a legitimate
	// empty list (for example, a currently empty/placeholder culture page).
	return parseEseAdapterRows(rows, doc, baseURL), nil
}

func parseEseAdapterHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	add := func(anchor *goquery.Selection, typ string) {
		eseAdapterAppendItem(&items, seen, anchor, baseURL, typ, "", "")
	}

	for _, section := range []struct {
		selectors []string
		typ       string
	}{
		{[]string{"#s1 .s1-ban a[href]", ".s1 .s1-ban a[href]", "#s1 .s1-lt a[href]", ".s1 .s1-lt a[href]"}, "环境要闻"},
		{[]string{"#s3 .s3-lt a[href]", ".s3 .s3-lt a[href]", "#s3 .s3-ban a[href]", ".s3 .s3-ban a[href]"}, "学术动态"},
		{[]string{"#s6 .s6-lt a[href]", ".s6 .s6-lt a[href]"}, "团学动态"},
		{[]string{"#s7 .s7-lt a[href]", ".s7 .s7-lt a[href]"}, "学院文化"},
	} {
		for _, selector := range section.selectors {
			doc.Find(selector).Each(func(_ int, anchor *goquery.Selection) {
				add(anchor, section.typ)
			})
		}
	}

	// The notice module renders all seven tab panels in the initial HTML,
	// although only one is visible until JavaScript switches tabs.
	noticeTypes := []string{"全部", "党政办公", "本科生", "研究生", "科研工作", "学生工作", "其他"}
	panels := doc.Find("#s2 .s2-qhc .s2-lt")
	if panels.Length() == 0 {
		panels = doc.Find(".s2 .s2-qhc .s2-lt")
	}
	panels.Each(func(index int, panel *goquery.Selection) {
		typ := "通知公告"
		if index < len(noticeTypes) {
			typ = noticeTypes[index]
		}
		panel.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			add(anchor, typ)
		})
	})

	return items
}

var eseAdapterListRowSelectors = []string{
	".ny-right .tw-list > ul > li",
	".ny-right .tw-list li",
	".ny-right .list > ul > li",
	".ny-right .list li",
	".ny-right .xyhb > ul > li",
	".ny-right .xyhb li",
	".ny-right .img-lists > ul > li",
	".ny-right .img-lists li",
	"#main-cont .tw-list li",
	"#main-cont .list li",
	"#main-cont .xyhb li",
	"#main-cont .img-lists li",
}

var eseAdapterListRegionSelectors = []string{
	".ny-right .tw-list",
	".ny-right .list",
	".ny-right .xyhb",
	".ny-right .img-lists",
	"#main-cont .tw-list",
	"#main-cont .list",
	"#main-cont .xyhb",
	"#main-cont .img-lists",
}

func eseAdapterListRows(doc *goquery.Document) (*goquery.Selection, bool) {
	for _, selector := range eseAdapterListRowSelectors {
		if rows := doc.Find(selector); rows.Length() > 0 {
			return rows, true
		}
	}
	for _, selector := range eseAdapterListRegionSelectors {
		if region := doc.Find(selector); region.Length() > 0 {
			return region.Find("li"), true
		}
	}
	return nil, false
}

func parseEseAdapterRows(rows *goquery.Selection, doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	typ := eseAdapterPageType(doc)
	if rows == nil {
		return items
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := eseAdapterFirstArticleAnchor(row, baseURL)
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		date, publishedAt := eseAdapterRowDateFields(row, anchor)
		eseAdapterAppendItem(&items, seen, anchor, baseURL, typ, date, publishedAt)
	})
	return items
}

func eseAdapterFirstArticleAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	if row == nil || row.Length() == 0 {
		return nil
	}
	var result *goquery.Selection
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := eseAdapterArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			result = anchor
			return false
		}
		return true
	})
	return result
}

func eseAdapterAppendItem(items *[]model.NewsItem, seen map[string]int, anchor *goquery.Selection, baseURL, typ, date, publishedAt string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	fullURL, ok := eseAdapterArticleURL(baseURL, anchor.AttrOr("href", ""))
	if !ok {
		return
	}
	title := eseAdapterAnchorTitle(anchor)
	if !eseAdapterUsableTitle(title) {
		return
	}
	if date == "" && publishedAt == "" {
		date, publishedAt = eseAdapterAnchorDateFields(anchor)
	}

	if index, ok := seen[fullURL]; ok {
		if (*items)[index].Date == "" && date != "" {
			(*items)[index].Date = date
		}
		if (*items)[index].PublishedAt == "" && publishedAt != "" {
			(*items)[index].PublishedAt = publishedAt
		}
		if (*items)[index].Type == "" && typ != "" {
			(*items)[index].Type = typ
		}
		return
	}

	seen[fullURL] = len(*items)
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        typ,
		ParsedAt:    time.Now(),
	})
}

func eseAdapterArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(htmlstd.UnescapeString(href))
	if href == "" {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	if !eseArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return cleanURL(parsed.String()), true
}

func eseAdapterAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"title", "data-title", "aria-label"} {
		if title := eseAdapterCleanText(anchor.AttrOr(attr, "")); eseAdapterUsableTitle(title) {
			return title
		}
	}
	for _, selector := range []string{
		".s1-rtx h3", ".s1-txt h3", ".s2-lt p", ".s3-tx p", ".s3-ban p",
		".s6-tx h3", ".tw-tx h3", ".news-title", ".article-title", ".title",
		"[class*='title']", "h1", "h2", "h3", "h4", "h5", "p",
	} {
		if title := eseAdapterCleanText(anchor.Find(selector).First().Text()); eseAdapterUsableTitle(title) {
			return title
		}
	}
	return eseAdapterCleanText(anchor.Text())
}

func eseAdapterUsableTitle(title string) bool {
	title = eseAdapterCleanText(title)
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return false
	}
	// The generic title filter treats the university-wide masthead as noise;
	// on this site it is an actual 环境要闻 article.
	if !isValidNewsTitle(title) && !strings.HasPrefix(title, "习近平给四所交通大学全体师生") {
		return false
	}
	switch title {
	case "环境要闻", "通知公告", "学术动态", "教学平台", "科研平台", "团学动态", "学院文化", "快速链接":
		return false
	}
	return true
}

var eseAdapterDateSelectors = []string{
	"time[datetime]", "time", ".s1-date", ".s1-txt > span", ".s2-date", ".s3-date", ".s6-tx p",
	".tw-date", ".publish-date", ".publish-time", ".news-date", ".list-date", ".date", ".time",
	"[class*='date']", "[class*='time']", "span",
}

func eseAdapterAnchorDateFields(anchor *goquery.Selection) (string, string) {
	if anchor == nil || anchor.Length() == 0 {
		return "", ""
	}
	for _, selector := range eseAdapterDateSelectors {
		if date, publishedAt := eseAdapterDateFields(anchor.Find(selector).First()); date != "" {
			return date, publishedAt
		}
	}
	card := anchor.Closest("li, article, .item, .news-item, .list-item")
	if card.Length() > 0 {
		return eseAdapterRowDateFields(card, anchor)
	}
	return "", ""
}

func eseAdapterRowDateFields(row, anchor *goquery.Selection) (string, string) {
	if row != nil && row.Length() > 0 {
		for _, selector := range eseAdapterDateSelectors {
			if date, publishedAt := eseAdapterDateFields(row.Find(selector).First()); date != "" {
				return date, publishedAt
			}
		}
	}
	if anchor != nil && anchor.Length() > 0 {
		return eseAdapterAnchorDateFieldsDirect(anchor)
	}
	return "", ""
}

func eseAdapterAnchorDateFieldsDirect(anchor *goquery.Selection) (string, string) {
	for _, selector := range eseAdapterDateSelectors {
		if date, publishedAt := eseAdapterDateFields(anchor.Find(selector).First()); date != "" {
			return date, publishedAt
		}
	}
	return "", ""
}

func eseAdapterDateFields(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	if !eseAdapterDateSelectionAllowed(sel) {
		return "", ""
	}
	if publishedAt := eseAdapterLabeledPublishedAt(flattenedText(sel)); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	if value := strings.TrimSpace(sel.AttrOr("datetime", "")); value != "" {
		if normalized := normalizePublishedAt(value); normalized != "" {
			return eseAdapterDateValues(normalized)
		}
	}
	// ESE's home page splits dates into separate day/month/year nodes.
	if sel.HasClass("s1-date") || sel.HasClass("s2-date") || sel.HasClass("s3-date") {
		if date := eseDateFromSelection(sel); date != "" {
			return date, ""
		}
	}
	if normalized := normalizePublishedAt(sel.Text()); normalized != "" {
		return eseAdapterDateValues(normalized)
	}
	if date := parseEseDateText(sel.Text()); date != "" {
		return date, ""
	}
	return "", ""
}

func eseAdapterDateSelectionAllowed(sel *goquery.Selection) bool {
	if sel == nil || sel.Length() == 0 {
		return false
	}
	text := strings.ToLower(eseAdapterCleanText(flattenedText(sel)))
	class := strings.ToLower(strings.TrimSpace(sel.AttrOr("class", "")))
	for _, marker := range []string{
		"活动时间", "截止时间", "报名时间", "会议时间", "举办时间", "开展时间",
		"提交时间", "有效期", "更新时间", "修改时间", "转载时间", "系统时间", "生成时间",
	} {
		if strings.Contains(text, marker) || strings.Contains(class, strings.TrimSuffix(marker, "时间")) {
			return false
		}
	}
	for _, marker := range []string{"deadline", "activity", "event", "meeting", "update", "modified", "system", "share", "转载"} {
		if strings.Contains(class, marker) {
			return false
		}
	}
	return true
}

func eseAdapterLabeledPublishedAt(text string) string {
	text = eseAdapterCleanText(text)
	if text == "" {
		return ""
	}
	for _, match := range eseAdapterPublicationLabelRe.FindAllStringIndex(text, -1) {
		if len(match) < 2 || match[1] >= len(text) {
			continue
		}
		dateText := eseAdapterPublicationDateRe.FindString(text[match[1]:])
		if dateText == "" {
			continue
		}
		if normalized := normalizePublishedAt(dateText); normalized != "" {
			return normalized
		}
	}
	return ""
}

func eseAdapterDateValues(normalized string) (string, string) {
	if len(normalized) < 10 {
		return "", ""
	}
	if len(normalized) > 10 {
		return normalized[:10], normalized
	}
	return normalized, ""
}

func eseAdapterPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	typ := eseAdapterCleanText(doc.Find(".ny-right .position h3").First().Text())
	if typ == "" {
		typ = eseAdapterCleanText(doc.Find(".position h3").First().Text())
	}
	if typ == "首页" || typ == "正文" {
		return ""
	}
	if typ == "" {
		typ = bodaArticleSectionType(doc, "")
	}
	if typ == "首页" || typ == "正文" {
		return ""
	}
	return typ
}

func eseAdapterHasHomeSections(doc *goquery.Document) bool {
	return doc != nil && doc.Find("#s1, #s2, #s3, #s6, #s7, .s1, .s2, .s3, .s6, .s7").Length() > 0
}

func parseAdapter_eseArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ese: parse article HTML: %w", err)
	}
	if eseAdapterLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("ese: received an error or verification page instead of an article")
	}

	titleSel := eseAdapterArticleTitleSelection(doc)
	contentSel := eseAdapterArticleContentSelection(doc)
	if titleSel.Length() == 0 || contentSel.Length() == 0 {
		return nil, fmt.Errorf("ese: article shell or body is missing")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = eseAdapterCleanText(titleSel.Text())
	if article.Title == "" {
		return nil, fmt.Errorf("ese: article title is empty")
	}
	if typ := eseAdapterPageType(doc); typ != "" {
		article.Type = typ
	}
	// parseBodaArticle has compatibility fallbacks for many Boda templates.
	// ESE's fixed art-tit metadata is authoritative, so discard any date that
	// the shared fallback may have inferred from a body or unrelated wrapper.
	article.Date = ""
	article.PublishedAt = ""

	meta := doc.Find(".art-main .art-tit p").First()
	if meta.Length() == 0 {
		meta = doc.Find(".art-tit p").First()
	}
	if meta.Length() > 0 {
		metaText := flattenedText(meta)
		if source := eseAdapterMetaValue(metaText, "来源", "信息来源"); source != "" {
			article.Source = source
		}
		if author := eseAdapterMetaValue(metaText, "作者"); author != "" {
			article.Author = author
		}
		if publishedAt := eseAdapterLabeledPublishedAt(metaText); publishedAt != "" {
			article.PublishedAt = publishedAt
			article.Date = publishedAt[:10]
		}
	}

	eseAdapterFinalizeArticleResources(article, doc, contentSel, baseURL)
	if content := eseAdapterBodyText(contentSel); content != "" {
		article.Content = content
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("ese: article body is empty")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func eseAdapterArticleTitleSelection(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		".art-main .art-tit h1", ".art-main .art-tit h2", ".art-main .art-tit h3",
		".art-main .cont-tit h1", ".art-main .cont-tit h2", ".art-main .cont-tit h3",
		".art-tit h1", ".art-tit h2", ".art-tit h3",
	} {
		if title := doc.Find(selector).First(); title.Length() > 0 {
			return title
		}
	}
	return doc.Find(".missing-ese-title")
}

func eseAdapterArticleContentSelection(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		".art-main .art-body .v_news_content",
		".art-main .v_news_content",
		".art-body .v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return doc.Find(".missing-ese-content")
}

func eseAdapterFinalizeArticleResources(article *model.Article, doc *goquery.Document, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}

	images := make([]string, 0)
	seenImages := make(map[string]bool)
	addImage := func(rawURL string) {
		rawURL = strings.TrimSpace(htmlstd.UnescapeString(rawURL))
		if rawURL == "" {
			return
		}
		resolved := eseAdapterResourceURL(baseURL, rawURL)
		if resolved == "" || seenImages[resolved] {
			return
		}
		seenImages[resolved] = true
		images = append(images, resolved)
	}
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		src := strings.TrimSpace(image.AttrOr("src", ""))
		if src == "" {
			src = strings.TrimSpace(image.AttrOr("data-src", ""))
		}
		if src == "" {
			src = strings.TrimSpace(image.AttrOr("orisrc", ""))
		}
		if eseAdapterResourceURL(baseURL, src) == "" {
			image.Remove()
			return
		}
		addImage(src)
		if src != "" {
			image.SetAttr("src", eseAdapterResourceURL(baseURL, src))
		}
	})
	content.Find("script").Each(func(_ int, script *goquery.Selection) {
		text := script.Text()
		for _, match := range eseAdapterPDFImagePathRe.FindAllStringSubmatch(text, -1) {
			if len(match) > 1 {
				addImage(match[1])
			}
		}
	})
	article.Images = images

	seenAttachments := make(map[string]bool, len(article.Attachments))
	filteredAttachments := make([]model.Attachment, 0, len(article.Attachments))
	for _, attachment := range article.Attachments {
		resolved := eseAdapterResourceURL(baseURL, attachment.URL)
		if resolved == "" || seenAttachments[resolved] {
			continue
		}
		attachment.URL = resolved
		seenAttachments[resolved] = true
		filteredAttachments = append(filteredAttachments, attachment)
	}
	article.Attachments = filteredAttachments
	addAttachment := func(rawURL, name string) {
		rawURL = strings.TrimSpace(htmlstd.UnescapeString(rawURL))
		resolved := eseAdapterResourceURL(baseURL, rawURL)
		if rawURL == "" || resolved == "" || seenAttachments[resolved] {
			return
		}
		name = eseAdapterAttachmentName(name, rawURL)
		if name == "" {
			return
		}
		seenAttachments[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	}

	doc.Find(".art-main a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" || !eseAdapterIsAttachmentLink(anchor, href) {
			return
		}
		addAttachment(href, anchor.Text())
		if resolved := eseAdapterResourceURL(baseURL, href); resolved != "" {
			anchor.SetAttr("href", resolved)
		}
	})
	content.Find("script").Each(func(_ int, script *goquery.Selection) {
		match := eseAdapterPDFFileRe.FindStringSubmatch(script.Text())
		if len(match) > 1 {
			addAttachment(match[1], "")
		}
	})
	if htmlText, htmlErr := content.Html(); htmlErr == nil {
		article.ContentHTML = htmlText
	}
}

const eseCanonicalBaseURL = "https://ese.swjtu.edu.cn"

// eseAdapterResourceURL only admits same-origin HTTP(S) resources. The
// article body may still contain ordinary external hyperlinks, but an image
// or attachment field must not turn a javascript/data/placeholder URL into a
// downloadable asset.
func eseAdapterResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(htmlstd.UnescapeString(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return ""
	}
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	if eseAdapterResourceMarkerRe.MatchString(resourceText) || strings.Contains(resourceText, "/pixel.gif") {
		return ""
	}
	return cleanURL(parsed.String())
}

// eseFetchArticle is the live path used by BodaSite.GetArticleSnapshot. HTML
// parsing remains deterministic; only this network path verifies that every
// advertised image/file returned a successful, correctly typed response.
func eseFetchArticle(articleURL string) (*model.Article, []byte, error) {
	fullURL, ok := eseAdapterArticleURL(eseCanonicalBaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("ese: refusing article URL outside the site: %s", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_eseArticle(body, eseCanonicalBaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("ese article %s: %w", fullURL, err)
	}
	article.Images = eseVerifyImages(article.Images, eseCanonicalBaseURL)
	article.Attachments = eseVerifyAttachments(article.Attachments, eseCanonicalBaseURL)
	return article, body, nil
}

func eseVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := eseAdapterResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := eseFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func eseVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := eseAdapterResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := eseFetchResource(resolved, baseURL, false)
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

func eseFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := eseAdapterResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("ese: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("ese: create resource request: %w", err)
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
		return nil, "", fmt.Errorf("ese: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("ese: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, eseAdapterMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("ese: read resource %s: %w", resolved, err)
	}
	if len(body) > eseAdapterMaxResourceBytes || !eseAdapterResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("ese: resource %s returned an invalid shell or file", resolved)
	}
	return body, contentType, nil
}

func eseAdapterResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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
	if eseAdapterResourceShellMarkerRe.Match(probe) {
		return false
	}
	magic := eseAdapterResourceMagic(body)
	if image {
		if !strings.HasPrefix(mediaType, "image/") {
			return false
		}
		return strings.HasPrefix(magic, "image-")
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

func eseAdapterResourceMagic(body []byte) string {
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
		return "image-jpeg"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image-png"
	case len(body) >= 6 && (bytes.Equal(body[:6], []byte("GIF87a")) || bytes.Equal(body[:6], []byte("GIF89a"))):
		return "image-gif"
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		return "image-webp"
	case len(body) >= 2 && bytes.Equal(body[:2], []byte("BM")):
		return "image-bmp"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{'I', 'I', '*', 0}) || bytes.Equal(body[:4], []byte{'M', 'M', 0, '*'})):
		return "image-tiff"
	case len(body) >= 7 && bytes.Equal(body[:7], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00}):
		return "rar"
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	}
	return ""
}

func eseAdapterIsAttachmentLink(anchor *goquery.Selection, href string) bool {
	lowerHref := strings.ToLower(href)
	if strings.Contains(lowerHref, "downloadattachurl") || strings.Contains(lowerHref, "download.jsp") {
		return true
	}
	for _, attr := range []string{"download", "data-download", "data-filename", "data-name"} {
		if strings.TrimSpace(anchor.AttrOr(attr, "")) != "" {
			return true
		}
	}
	for _, value := range []string{
		href, anchor.AttrOr("download", ""), anchor.AttrOr("name", ""), anchor.AttrOr("title", ""), anchor.Text(),
	} {
		lower := strings.ToLower(value)
		for _, extension := range eseAttachmentExtensions {
			if strings.Contains(lower, extension) {
				return true
			}
		}
	}
	return false
}

func eseAdapterAttachmentName(value, href string) string {
	if name := eseAdapterCleanText(value); name != "" {
		name = strings.Trim(name, "[]【】")
		if name != "" {
			return name
		}
	}
	if parsed, err := url.Parse(href); err == nil {
		if name := eseAdapterCleanText(path.Base(parsed.Path)); name != "." && name != "/" {
			if decoded, decodeErr := url.PathUnescape(name); decodeErr == nil {
				return decoded
			}
			return name
		}
	}
	return ""
}

func eseAdapterBodyText(content *goquery.Selection) string {
	if content == nil || content.Length() == 0 {
		return ""
	}
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	parts := make([]string, 0)
	appendText := func(sel *goquery.Selection) {
		if text := eseAdapterCleanText(sel.Text()); text != "" {
			parts = append(parts, text)
		}
	}
	clone.Find("p, h1, h2, h3, h4, h5, h6").Each(func(_ int, sel *goquery.Selection) {
		appendText(sel)
	})
	// ESE uses paragraphs for most prose, but notices and platform pages also
	// put required text in lists, tables, and leaf divs. Add only leaf blocks
	// so nested paragraphs are not duplicated.
	clone.Find("li, td, th, div").Each(func(_ int, sel *goquery.Selection) {
		if sel.Find("p, h1, h2, h3, h4, h5, h6, li, td, th, div").Length() > 0 {
			return
		}
		if sel.Find("img").Length() > 0 && eseAdapterCleanText(sel.Text()) == "" {
			return
		}
		appendText(sel)
	})
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	return eseAdapterCleanText(flattenedText(clone))
}

func eseAdapterMetaValue(text string, labels ...string) string {
	text = eseAdapterCleanText(text)
	if text == "" {
		return ""
	}
	allLabels := []string{"时间", "日期", "发布时间", "发布日期", "来源", "信息来源", "作者", "点击", "点击量"}
	for _, label := range labels {
		for _, prefix := range []string{label + "：", label + ":"} {
			start := strings.Index(text, prefix)
			if start < 0 {
				continue
			}
			rest := text[start+len(prefix):]
			end := len(rest)
			for _, nextLabel := range allLabels {
				for _, nextPrefix := range []string{nextLabel + "：", nextLabel + ":"} {
					if index := strings.Index(rest, nextPrefix); index >= 0 && index < end {
						end = index
					}
				}
			}
			if value := strings.TrimSpace(rest[:end]); value != "" {
				return value
			}
		}
	}
	return ""
}

func eseAdapterCleanText(value string) string {
	value = htmlstd.UnescapeString(value)
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func eseAdapterLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(eseAdapterCleanText(doc.Find("title").First().Text()))
	for _, marker := range []string{"404", "403", "500", "error", "错误", "不存在", "验证码", "安全验证", "附件下载", "登录", "login", "forbidden", "not found", "access denied", "request blocked", "just a moment", "cloudflare"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	body := strings.ToLower(eseAdapterCleanText(doc.Find("body").First().Text()))
	for _, marker := range []string{"请输入验证码", "请先登录", "页面不存在", "您访问的页面不存在", "无权限访问", "系统错误", "安全验证", "请求已被阻断", "安全威胁", "access denied", "request blocked", "just a moment", "cloudflare"} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}
