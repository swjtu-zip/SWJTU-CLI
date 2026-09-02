package scraper

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"path"
	"regexp"
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
	})
}

// These are the navigation leaves that currently expose real internal
// article rows. Profile, static, download, platform/resource, external-link,
// and placeholder (#) pages are deliberately not presented as news feeds.
var eseSubPages = []string{
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
}

// Audited totals are only a fallback for callers asking for page 2 before a
// total-page request has succeeded. A total parsed from the live pagination
// always takes precedence.
var esePageTotals = map[string]int{
	"index/xwgg/hjyw":       38,
	"index/xwgg/tzgg1/qb":   1,
	"index/xwgg/tzgg1/dzbg": 1,
	"index/xwgg/tzgg1/bks":  1,
	"index/xwgg/tzgg1/yjs":  1,
	"index/xwgg/tzgg1/kygz": 1,
	"index/xwgg/tzgg1/xsgz": 1,
	"index/xwgg/tzgg1/qt":   1,
	"djgz/djdt":             23,
	"djgz/jwgz":             2,
	"djgz/ghgz":             1,
	"kxyj/xsdt":             2,
	"xsgz/tzjgg":            24,
	"xsgz/txdt":             10,
	"xsgz/xsfc":             1,
	"xywh/hjsg":             1,
	"xyyd/xyqy":             1,
}

var eseAttachmentExtensions = []string{
	".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf",
	".zip", ".rar", ".7z", ".txt", ".csv",
}

var (
	eseAdapterPDFImagePathRe = regexp.MustCompile(`(?i)["']((?:https?://|/|\.\.?/)[^"']+\.(?:jpg|jpeg|png|gif|webp)(?:\?[^"']*)?)["']`)
	eseAdapterPDFFileRe      = regexp.MustCompile(`(?i)showVsbpdfIframe\s*\(\s*["']([^"']+\.pdf(?:\?[^"']*)?)["']`)
)

// esePageURL maps logical pages (1 = newest) to ESE's reverse-numbered files.
// The first page is always the unsuffixed list page, as on the live site.
func esePageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page < 1 {
		page = 1
	}
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages <= 1 {
		if knownTotal, ok := esePageTotals[slug]; ok {
			totalPages = knownTotal
		}
	}
	if totalPages <= 1 {
		// Do not silently repeat page 1 when a page count is unavailable.
		return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, page, extension)
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		// An out-of-range request should become a miss/404, not a duplicate of
		// the oldest page.
		filePage = 0
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
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
	".ny-right .img-lists > ul > li",
	".ny-right .img-lists li",
	"#main-cont .tw-list li",
	"#main-cont .list li",
	"#main-cont .img-lists li",
}

var eseAdapterListRegionSelectors = []string{
	".ny-right .tw-list",
	".ny-right .list",
	".ny-right .img-lists",
	"#main-cont .tw-list",
	"#main-cont .list",
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
	if typ := eseAdapterPageType(doc); typ != "" {
		article.Type = typ
	}

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
		if publishedAt := normalizePublishedAt(metaText); publishedAt != "" {
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
		resolved := cleanURL(resolveURL(baseURL, rawURL))
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
		addImage(src)
		if src != "" {
			image.SetAttr("src", cleanURL(resolveURL(baseURL, src)))
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
	for index := range article.Attachments {
		article.Attachments[index].URL = cleanURL(article.Attachments[index].URL)
		seenAttachments[article.Attachments[index].URL] = true
	}
	addAttachment := func(rawURL, name string) {
		rawURL = strings.TrimSpace(htmlstd.UnescapeString(rawURL))
		resolved := cleanURL(resolveURL(baseURL, rawURL))
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
		anchor.SetAttr("href", cleanURL(resolveURL(baseURL, href)))
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
	clone.Find("p, h1, h2, h3, h4, h5, h6").Each(func(_ int, sel *goquery.Selection) {
		if text := eseAdapterCleanText(sel.Text()); text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	return eseAdapterCleanText(clone.Text())
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
	for _, marker := range []string{"404", "403", "500", "error", "错误", "不存在", "验证码", "安全验证", "附件下载", "登录", "login", "forbidden", "not found"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	body := strings.ToLower(eseAdapterCleanText(doc.Find("body").First().Text()))
	for _, marker := range []string{"请输入验证码", "请先登录", "页面不存在", "您访问的页面不存在", "无权限访问", "系统错误", "安全验证"} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}
