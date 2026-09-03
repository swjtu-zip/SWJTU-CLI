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

// emSubPages contains the article-list routes exposed by the site's menus and
// by the additional lists linked from the home page. Profile, contact, map,
// restricted-IP and other static pages are deliberately not feeds.
var emSubPages = []string{
	"xwdt",
	"dqgz",
	"dqgz/djgz",
	"dqgz/jwgz",
	"dqgz/ghgz",
	"dqgz/ltgz",
	"rcpy",
	"rcpy/xwdt",
	"rcpy/zszl",
	"rcpy/tzgg",
	"rcpy/glgd",
	"rcpy/wdxz",
	"rcpy/zyjj",
	"jypx3/pxdt",
	"jypx3/tzgg",
	"tzgg",
	"hqbz",
	"qt",
	"gzdt",
	"xqfg",
	"xyyd",
	"xyyd/xyhd",
	"xyyd/xyjy",
}

var (
	emArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	emDateRe        = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?`)
	emClockRe       = regexp.MustCompile(`[0-9]{1,2}\s*(?::|时)[0-9]{1,2}`)
	emPageCountRe   = regexp.MustCompile(`(?:/|共|第)\s*([0-9]+)\s*页`)
)

var emHomeTabTypes = []string{
	"校区公告",
	"党政办公",
	"人才培养",
	"教育培训",
	"后勤保障",
	"其他",
}

func init() {
	registerAdapter("em", SiteAdapter{
		SubPages:     emSubPages,
		ParseList:    parseAdapter_emList,
		ParseArticle: parseAdapter_emArticle,
		PageURL:      emPageURL,
		TotalPages:   emTotalPages,
	})
}

// emPageURL maps the logical page number (1 = newest) to em's reverse-numbered
// generated files. For example, xwdt's logical page 2 is xwdt/70.htm and its
// last page is xwdt/1.htm.
func emPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}

	filePage := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

// emTotalPages reads the first page and uses the displayed logical page
// numbers. The shared parser cannot be used because em's hrefs contain the
// reverse generated filename rather than the logical page number.
func emTotalPages(slug string) (int, error) {
	site, ok := AllSites["em"]
	if !ok || site == nil {
		return 0, fmt.Errorf("em site is not registered")
	}

	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(emPageURL(site.BaseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if total := emPageCountFromHTML(body); total > 0 {
			return total, nil
		}
		lastErr = fmt.Errorf("em list %q has no page count", slug)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("em list %q has no page count", slug)
	}
	return 0, lastErr
}

func emPageCountFromHTML(html []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}

	for _, text := range []string{doc.Find(".p_t").Text(), doc.Find(".p_pages").Text()} {
		if match := emPageCountRe.FindStringSubmatch(strings.Join(strings.Fields(text), "")); match != nil {
			if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
				return total
			}
		}
	}

	// Use the text of the page controls, never the href. In the live HTML the
	// href for displayed page 2 is 70.htm on a 71-page feed.
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

	// A legitimate one-page or empty list still exposes the list shell and
	// disabled page controls. It has one logical page.
	if doc.Find(".p_t, .p_pages").Length() > 0 {
		return 1
	}
	return 0
}

func parseAdapter_emList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse em list: %w", err)
	}

	if emHasHomeLayout(doc) {
		items := parseEmHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("em home page contains no article entries")
		}
		return items, nil
	}

	list := doc.Find("section.n_titu.bgss, section.TextList.bgss").First()
	if list.Length() == 0 {
		return nil, fmt.Errorf("em page is not a supported article list")
	}

	items := parseEmListRows(list, emPageType(doc), baseURL)
	if len(items) > 0 {
		return items, nil
	}
	if emIsExplicitEmptyList(doc) {
		return []model.NewsItem{}, nil
	}
	return nil, fmt.Errorf("em list contains no valid article rows")
}

func emHasHomeLayout(doc *goquery.Document) bool {
	return doc.Find(".newsBox, .NoticeBox, .gzdtBox, .campusBox").Length() > 0 &&
		doc.Find("#tabs-container1").Length() > 0
}

func parseEmHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)

	appendRows := func(rows *goquery.Selection, typ string) {
		rows.Each(func(_ int, row *goquery.Selection) {
			anchor := emRowArticleAnchor(row, baseURL)
			if anchor == nil || anchor.Length() == 0 {
				return
			}
			item, ok := emNewsItemFromRow(row, anchor, typ, baseURL)
			if !ok || seen[item.URL] {
				return
			}
			seen[item.URL] = true
			items = append(items, item)
		})
	}

	// Parse the visible home lists only. The news carousel repeats the same
	// four rows in a swiper, so the newsText list is the canonical copy.
	appendRows(doc.Find(".newsBox .newsText > li"), "校区要闻")

	doc.Find("#tabs-container1 .swiper-slide").Each(func(i int, slide *goquery.Selection) {
		if i >= len(emHomeTabTypes) {
			return
		}
		appendRows(slide.ChildrenFiltered("ul").First().ChildrenFiltered("li"), emHomeTabTypes[i])
	})

	appendRows(doc.Find(".gzdtBox .gzdtCon > ul > li"), "工作动态")
	appendRows(doc.Find(".campusBox .campusCon > ul > li"), "校区风光")

	return items
}

func parseEmListRows(list *goquery.Selection, pageType, baseURL string) []model.NewsItem {
	rows := list.Find("ul > li")
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := emRowArticleAnchor(row, baseURL)
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		item, ok := emNewsItemFromRow(row, anchor, pageType, baseURL)
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	})
	return items
}

func emRowArticleAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	var result *goquery.Selection
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := emArticleURL(baseURL, anchor.AttrOr("href", "")); !ok {
			return true
		}
		result = anchor
		return false
	})
	return result
}

func emNewsItemFromRow(row, anchor *goquery.Selection, typ, baseURL string) (model.NewsItem, bool) {
	fullURL, ok := emArticleURL(baseURL, anchor.AttrOr("href", ""))
	if !ok {
		return model.NewsItem{}, false
	}

	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		for _, selector := range []string{"h5", "h6", ".line1", ".textline", ".title", "p"} {
			if value := strings.TrimSpace(anchor.Find(selector).First().Text()); value != "" {
				title = value
				break
			}
		}
	}
	title = cleanTitle(title)
	if !emIsValidNewsTitle(title) {
		return model.NewsItem{}, false
	}

	date, publishedAt := emDateFromRow(row)
	item := model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     date,
		Type:     strings.TrimSpace(typ),
		ParsedAt: time.Now(),
	}
	if publishedAt != "" {
		item.PublishedAt = publishedAt
	}
	return item, true
}

func emIsValidNewsTitle(title string) bool {
	if title == "" || len([]rune(title)) > 160 {
		return false
	}
	if isValidNewsTitle(title) {
		return true
	}
	// The shared guard treats every title ending in the Chinese book
	// delimiter as navigation noise. em has real list entries such as
	// “【党章党规】《中国共产党支部工作条例（试行）》”, so allow a
	// sufficiently substantive row title while retaining the normal filters.
	return strings.HasSuffix(title, "》") && len([]rune(title)) > 8
}

func emDateFromRow(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	if dateBox := row.Find(".date, .data").First(); dateBox.Length() > 0 {
		day := strings.TrimSpace(dateBox.Find("i").First().Text())
		year := strings.TrimSpace(dateBox.Find("b").First().Text())
		if day != "" && year != "" {
			if date, published := emDateValues(year + "-" + day); date != "" {
				return date, published
			}
		}
		if date, published := emDateValues(dateBox.Text()); date != "" {
			return date, published
		}
	}

	for _, selector := range []string{"time", ".text > i", ".date", ".data", "i"} {
		if value := strings.TrimSpace(row.Find(selector).First().Text()); value != "" {
			if date, published := emDateValues(value); date != "" {
				return date, published
			}
		}
	}
	return "", ""
}

func emDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(raw); normalized != "" {
		date := normalized[:10]
		if emClockRe.MatchString(raw) {
			return date, normalized
		}
		return date, ""
	}
	if match := emDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func emPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if title := strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")); title != "" {
		return title
	}
	for _, selector := range []string{"#nav_slide .n_title h1", ".n_title h1", ".n_left .con h1"} {
		if title := strings.TrimSpace(doc.Find(selector).First().Text()); title != "" {
			return strings.Join(strings.Fields(title), " ")
		}
	}
	return ""
}

func emIsExplicitEmptyList(doc *goquery.Document) bool {
	if doc == nil || doc.Find(".p_pages").Length() == 0 {
		return false
	}
	return strings.Contains(strings.Join(strings.Fields(doc.Find(".p_t").Text()), ""), "共0条")
}

func emArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !isValidNewsURL(fullURL) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}

	if emArticlePathRe.MatchString(parsed.Path) {
		parsed.Fragment = ""
		return parsed.String(), true
	}
	// The 管理规定 list uses the same site's legacy Visual SiteBuilder
	// content endpoint instead of /info/<channel>/<id>.htm.
	query := parsed.Query()
	if strings.EqualFold(parsed.Path, "/zzz_content.jsp") &&
		strings.Contains(strings.ToLower(query.Get("urltype")), "newscontenturl") &&
		query.Get("wbnewsid") != "" {
		parsed.Fragment = ""
		return parsed.String(), true
	}
	return "", false
}

func parseAdapter_emArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse em article html: %w", err)
	}
	if emLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("em article is an error or verification page")
	}
	if doc.Find(".show01 h5").First().Length() == 0 || doc.Find(".v_news_content").First().Length() == 0 {
		return nil, fmt.Errorf("em page is not a supported article")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse em article: %w", err)
	}

	if title := cleanTitle(doc.Find(".show01 h5").First().Text()); title != "" {
		article.Title = title
	}
	if date, publishedAt := emDateValues(doc.Find(".show01 p").First().Text()); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	if typ := emArticleType(doc); typ != "" {
		article.Type = typ
	}
	emAppendAttachments(article, doc, baseURL)
	emAppendPrevNext(article, doc, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func emLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")); pageType != "" && pageType != "3" {
		return true
	}
	pageTitle := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"验证码", "访问验证", "页面不存在", "404 not found", "system error", "系统错误"} {
		if strings.Contains(pageTitle, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func emArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	var breadcrumb []string
	doc.Find(".mianbao a").Each(func(_ int, anchor *goquery.Selection) {
		if value := strings.Join(strings.Fields(anchor.Text()), " "); value != "" && value != "首页" {
			breadcrumb = append(breadcrumb, value)
		}
	})
	if len(breadcrumb) > 0 {
		return breadcrumb[len(breadcrumb)-1]
	}
	for _, selector := range []string{".n_left .con h1", "#nav_slide .n_title h1", ".n_title h1"} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func emAppendAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}

	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !emIsAttachmentLink(href) {
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

func emIsAttachmentLink(href string) bool {
	if href == "" {
		return false
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

func emAppendPrevNext(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	doc.Find(".show03 p a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		text := strings.Join(strings.Fields(link.Text()), " ")
		parentText := strings.Join(strings.Fields(link.Parent().Text()), " ")
		resolved := ""
		// The live em template emits bare sibling filenames (for example
		// 20636.htm), but this parser has no current article directory. Keep
		// the title and only resolve URLs whose base is unambiguous.
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
