package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// cmSubPages contains the article feeds exposed by CM's visible navigation
// and by the additional lists embedded in the home page.  The profile pages
// (学院介绍、现任领导、师资队伍 and similar) are deliberately excluded: they
// are real pages, but are not article feeds.  The two differently named
// 科研团队 pages are also intentional: /index/kytd is the home-page feed,
// while /kxyj/kytd is an empty/static shell.
var cmSubPages = []string{
	"index/xyyw",
	"index/tzgg",
	"index/kytd",
	"index/kydt",
	"index/zsjy",
	"index/xysh",
	"index/zt/rczp",
	"index/zt/djdt",
	"index/zt/zsjy",
	"index/zt/jjjd",
	"index/ssfc",
	"index/ssfc/xsfc",
	"index/ssfc/jsfc",
	"index/yxrl",
	"dqgz/djdt",
	"dqgz/ljyx",
	"rcpy/bkspy",
	"rcpy/yjspy",
	"kxyj/kypt",
	"xszc/xyjz",
	"cpyc/rczp",
	"cpyc/jszp",
	"cpyc/bsh",
	"cyxx",
}

var (
	cmArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	cmDateRe        = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?`)
	cmCompactDateRe = regexp.MustCompile(`(20[0-9]{2})\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?`)
	cmShortDateRe   = regexp.MustCompile(`^[0-9]{1,2}\s*[-/.]\s*[0-9]{1,2}$`)
	cmClockRe       = regexp.MustCompile(`[0-9]{1,2}\s*(?::|时)[0-9]{1,2}`)
	cmYearRe        = regexp.MustCompile(`^20[0-9]{2}$`)
	cmPageCountRe   = regexp.MustCompile(`(?:/|共|第)\s*([0-9]+)\s*页`)
	cmTotalItemsRe  = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
)

func init() {
	registerAdapter("cm", SiteAdapter{
		SubPages:     cmSubPages,
		ParseList:    parseAdapter_cmList,
		ParseArticle: parseAdapter_cmArticle,
		PageURL:      cmPageURL,
		TotalPages:   cmTotalPages,
	})
}

// cmPageURL maps the logical page number (1 = newest) to CM's reverse-numbered
// generated files. For example, a 36-page feed advertises logical page 2 as
// /feed/35.htm and its last page as /feed/1.htm.
func cmPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page < 1 || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages <= 1 {
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

// cmTotalPages reads the first page and extracts the displayed logical page
// numbers. CM's hrefs contain reverse generated filenames (35.htm is shown as
// page 2), so the shared href-based page counter cannot be used here.
func cmTotalPages(slug string) (int, error) {
	site, ok := AllSites["cm"]
	if !ok || site == nil {
		return 0, fmt.Errorf("cm site is not registered")
	}

	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(cmPageURL(site.BaseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if total := cmPageCountFromHTML(body); total > 0 {
			return total, nil
		}
		lastErr = fmt.Errorf("cm list %q has no page count", slug)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("cm list %q has no page count", slug)
	}
	return 0, lastErr
}

func cmPageCountFromHTML(html []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}

	// Prefer an explicit page counter if a particular CM template provides one.
	for _, text := range []string{doc.Find(".p_t").Text(), doc.Find(".p_pages").Text()} {
		if match := cmPageCountRe.FindStringSubmatch(strings.Join(strings.Fields(text), "")); match != nil {
			if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
				return total
			}
		}
	}

	// On the current site the displayed number is the logical page, while the
	// href is the reverse generated filename. Read the text from p_no instead
	// of interpreting the href, including the last visible number.
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, page *goquery.Selection) {
		value, err := strconv.Atoi(strings.TrimSpace(page.Text()))
		if err == nil && value > maxPage {
			maxPage = value
		}
	})
	if maxPage > 0 {
		return maxPage
	}

	// Keep a fallback for a template that only exposes total rows and current
	// page size; this still avoids the reverse-filename interpretation.
	match := cmTotalItemsRe.FindStringSubmatch(strings.Join(strings.Fields(doc.Find(".p_t").Text()), ""))
	if match != nil {
		totalItems, _ := strconv.Atoi(match[1])
		rows := doc.Find(".n_list > ul > li, .n_listTxt > ul > li").Length()
		if totalItems > 0 && rows > 0 {
			return (totalItems + rows - 1) / rows
		}
	}
	return 0
}

func parseAdapter_cmList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse cm list: %w", err)
	}

	// Ordinary list pages use n_list; the two 师生风采 child pages use the
	// older n_listTxt name. Scoping to these containers is what keeps the
	// navigation links (including the mobile 首页 h1) out of the feed.
	if list := doc.Find(".n_list > ul, .n_listTxt > ul").First(); list.Length() > 0 {
		return parseCmListRows(list.ChildrenFiltered("li"), cmPageType(doc), baseURL), nil
	}

	// index.htm is an aggregation page. Each home section has its own heading;
	// collect every article link from all sections instead of only the first
	// visible list. The calendar is populated from /bless/data1.json and its
	// complete article list is exposed separately as index/yxrl.
	if cmHasHomeLayout(doc) {
		items := parseCmHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("cm home page contains no article entries")
		}
		return items, nil
	}

	return nil, fmt.Errorf("cm page is not a supported article list")
}

func parseCmListRows(rows *goquery.Selection, pageType, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := cmArticleAnchor(row, baseURL)
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		fullURL, ok := cmArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}

		title := cmListTitle(row, anchor)
		if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 160 {
			return
		}
		date, publishedAt := cmDateFromScope(row.Find("time, .time, .date").First())
		if date == "" {
			date, publishedAt = cmDateFromScope(row)
		}

		seen[fullURL] = true
		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     pageType,
			ParsedAt: time.Now(),
		}
		if cmHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}
		items = append(items, item)
	})
	return items
}

func cmArticleAnchor(scope *goquery.Selection, baseURL string) *goquery.Selection {
	if scope == nil || scope.Length() == 0 {
		return nil
	}
	var result *goquery.Selection
	scope.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := cmArticleURL(baseURL, anchor.AttrOr("href", "")); !ok {
			return true
		}
		if result == nil {
			result = anchor
		}
		// A thumbnail wrapper can precede the headline in the same row.
		if strings.TrimSpace(anchor.AttrOr("title", "")) != "" || strings.TrimSpace(anchor.Text()) != "" {
			result = anchor
			return false
		}
		return true
	})
	return result
}

func cmListTitle(row, anchor *goquery.Selection) string {
	if title := strings.TrimSpace(anchor.AttrOr("title", "")); title != "" {
		return cleanTitle(title)
	}
	for _, selector := range []string{"h2", "h3", "h4", ".title", ".aline_r", ".eclip"} {
		if title := strings.TrimSpace(row.Find(selector).First().Text()); title != "" {
			return cleanTitle(title)
		}
	}
	return cleanTitle(strings.TrimSpace(anchor.Text()))
}

func cmPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if title := strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")); title != "" {
		return title
	}
	return strings.TrimSpace(doc.Find(".ny-tit h4").First().Text())
}

func cmHasHomeLayout(doc *goquery.Document) bool {
	return doc.Find("section.homea, section.homeb, section.homec, section.homed, section.homee").Length() > 0
}

func parseCmHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	// These selectors correspond to the site's current server-rendered home
	// sections. The article URL filter below also makes this safe if the site
	// adds ordinary links to one of the sections later.
	selectors := []string{
		"section.homea a[href]",
		"section.homeb a[href]",
		"section.homec a[href]",
		"section.homed a[href]",
	}
	for _, selector := range selectors {
		doc.Find(selector).Each(func(_ int, anchor *goquery.Selection) {
			if isInNavArea(anchor) {
				return
			}
			fullURL, ok := cmArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok {
				return
			}
			title := cmListTitle(anchor, anchor)
			if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 160 {
				return
			}
			date, publishedAt := cmDateFromScope(anchor.Find("time").First())
			item := model.NewsItem{
				Title:    title,
				URL:      fullURL,
				Date:     date,
				Type:     cmHomeType(anchor),
				ParsedAt: time.Now(),
			}
			if cmHasClock(publishedAt) {
				item.PublishedAt = publishedAt
			}
			cmAddHomeItem(&items, seen, item)
		})
	}
	return items
}

func cmHomeType(anchor *goquery.Selection) string {
	for _, selector := range []string{".homea .left", ".homea .right", ".homeb .wp", ".homec .item"} {
		container := anchor.Closest(selector)
		if container.Length() == 0 {
			continue
		}
		for _, heading := range []string{".title001 h2", ".tit002 h2"} {
			if value := strings.TrimSpace(container.Find(heading).First().Text()); value != "" {
				return value
			}
		}
	}
	if container := anchor.Closest(".homed .list2w1, .homed .list2w2"); container.Length() > 0 {
		if container.HasClass("list2w1") {
			return "学生风采"
		}
		return "教师风采"
	}
	return "师生风采"
}

func cmAddHomeItem(items *[]model.NewsItem, seen map[string]int, item model.NewsItem) {
	if index, ok := seen[item.URL]; ok {
		current := &(*items)[index]
		if current.Date == "" && item.Date != "" {
			current.Date = item.Date
			current.PublishedAt = item.PublishedAt
		}
		if current.Type == "" && item.Type != "" {
			current.Type = item.Type
		}
		return
	}
	seen[item.URL] = len(*items)
	*items = append(*items, item)
}

func cmDateFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}

	// The notice card on the home page separates the year and month/day into
	// <time><span>2026</span><em>07-08</em></time>.
	year := strings.TrimSpace(scope.Find("span").First().Text())
	monthDay := strings.TrimSpace(scope.Find("em").First().Text())
	if cmYearRe.MatchString(year) && cmShortDateRe.MatchString(monthDay) {
		return cmDateValues(year + "-" + monthDay)
	}
	return cmDateValues(strings.TrimSpace(scope.Text()))
}

func cmDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(raw), " ")
	if raw == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(raw); normalized != "" {
		if cmHasClock(raw) || strings.Contains(raw, "分") || strings.Contains(raw, "秒") {
			return normalized[:10], normalized
		}
		return normalized[:10], ""
	}
	if match := cmDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	if match := cmCompactDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	if cmShortDateRe.MatchString(raw) {
		return strings.ReplaceAll(strings.ReplaceAll(raw, "/", "-"), ".", "-"), ""
	}
	return "", ""
}

func cmHasClock(value string) bool {
	return cmClockRe.MatchString(value)
}

func cmArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !cmArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func parseAdapter_cmArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse cm article: %w", err)
	}

	titleSel := doc.Find(".dtl-tit h4").First()
	if titleSel.Length() == 0 || strings.TrimSpace(titleSel.Text()) == "" {
		return nil, fmt.Errorf("cm page is not an article: missing .dtl-tit h4")
	}
	contentSel := doc.Find(".dtl-txt .v_news_content").First()
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".dtl-txt #vsb_content, .dtl-txt").First()
	}
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("cm article has no content container")
	}

	article := &model.Article{
		Title: strings.TrimSpace(titleSel.Text()),
		Type:  cmArticleType(doc),
	}
	cmFillArticleMeta(article, doc, titleSel, contentSel)

	article.Content = cmContentText(contentSel)
	cmAppendImages(article, contentSel, baseURL)
	detail := doc.Find(".detail").First()
	if detail.Length() == 0 {
		detail = doc.Selection
	}
	cmAppendAttachments(article, detail, baseURL)
	cmAppendPrevNext(article, detail, baseURL)
	if html, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = html
	}

	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("cm article has empty content")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func cmArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if value := strings.TrimSpace(doc.Find(".ny-tit h4").First().Text()); value != "" {
		return value
	}
	var value string
	doc.Find(".position a").Each(func(_ int, link *goquery.Selection) {
		text := strings.TrimSpace(link.Text())
		if text != "" && text != "首页" && text != "正文" {
			value = text
		}
	})
	return value
}

func cmFillArticleMeta(article *model.Article, doc *goquery.Document, titleSel, contentSel *goquery.Selection) {
	var date, publishedAt string
	doc.Find(".dtl-tit .dtl-ar span").Each(func(_ int, field *goquery.Selection) {
		text := strings.TrimSpace(field.Text())
		switch {
		case strings.HasPrefix(text, "发布者：") || strings.HasPrefix(text, "发布者:"):
			article.Author = cmMetaValue(text, "发布者")
		case strings.HasPrefix(text, "作者：") || strings.HasPrefix(text, "作者:"):
			article.Author = cmMetaValue(text, "作者")
		case strings.HasPrefix(text, "摄影：") || strings.HasPrefix(text, "摄影:"):
			article.Photographer = cmMetaValue(text, "摄影")
		case strings.HasPrefix(text, "编辑：") || strings.HasPrefix(text, "编辑:"):
			article.Editor = cmMetaValue(text, "编辑")
		case strings.HasPrefix(text, "来源：") || strings.HasPrefix(text, "来源:"):
			article.Source = cmMetaValue(text, "来源")
		case strings.HasPrefix(text, "发布时间：") || strings.HasPrefix(text, "发布时间:"):
			date, publishedAt = cmDateValues(cmMetaValue(text, "发布时间"))
		case strings.HasPrefix(text, "日期：") || strings.HasPrefix(text, "日期:"):
			date, publishedAt = cmDateValues(cmMetaValue(text, "日期"))
		}
	})

	if date == "" {
		date = findDateInMetaZone(titleSel, contentSel)
	}
	if publishedAt == "" {
		if value := findDateTimeInMetaZone(titleSel, contentSel); value != "" {
			publishedAt = normalizePublishedAt(value)
		}
	}
	article.Date = date
	article.PublishedAt = publishedAt
}

func cmMetaValue(text, label string) string {
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, label+"："), label+":"))
	return value
}

func cmContentText(content *goquery.Selection) string {
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	parts := make([]string, 0)
	clone.Find("p, h1, h2, h3, h4, h5, h6, li, blockquote, pre").Each(func(_ int, block *goquery.Selection) {
		if block.Find("p, h1, h2, h3, h4, h5, h6, li, blockquote, pre").Length() > 0 {
			return
		}
		if text := strings.Join(strings.Fields(block.Text()), " "); text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}

	clone.Children().Each(func(_ int, child *goquery.Selection) {
		if text := strings.Join(strings.Fields(child.Text()), " "); text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	return strings.Join(strings.Fields(clone.Text()), " ")
}

func cmAppendImages(article *model.Article, content *goquery.Selection, baseURL string) {
	seen := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		src := ""
		for _, attr := range []string{"src", "orisrc", "data-src", "data-original", "data-lazy-src"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				src = value
				break
			}
		}
		if src == "" || strings.HasPrefix(strings.ToLower(src), "data:") || strings.Contains(src, "cm.swjtu.edu.cnnull") {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, src))
		if resolved == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Images = append(article.Images, resolved)
		image.SetAttr("src", resolved)
	})
}

func cmAppendAttachments(article *model.Article, detail *goquery.Selection, baseURL string) {
	if detail == nil || detail.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	detail.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !cmIsAttachmentLink(href, link) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if resolved == "" || seen[resolved] {
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
			if parsed, err := url.Parse(resolved); err == nil {
				name = strings.TrimPrefix(parsed.Path, "/")
			}
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	})
}

func cmIsAttachmentLink(href string, link *goquery.Selection) bool {
	if href == "" {
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
	return false
}

func cmAppendPrevNext(article *model.Article, detail *goquery.Selection, baseURL string) {
	detail.Find(".p-page a").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		text := strings.TrimSpace(link.Text())
		parentText := strings.TrimSpace(link.Parent().Text())
		// A detail parser does not receive the current article URL, so do not
		// manufacture a wrong root-relative URL for CM's 31301.htm links.
		resolved := ""
		if strings.HasPrefix(href, "/") || strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
			resolved = resolveURL(baseURL, href)
		}
		if strings.Contains(parentText, "上一篇") {
			article.PrevURL, article.PrevTitle = resolved, text
		} else if strings.Contains(parentText, "下一篇") {
			article.NextURL, article.NextTitle = resolved, text
		}
	})
}
