package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// clxySubPages contains the article-list entry points rendered by the live
// home page. The parent routes are real merged archives, while the nested
// routes are the visible/hidden tabs behind their "更多" links.
//
// The top navigation's articleinfo.jsp/articlelist.jsp links currently return
// a permission-error page, and the mobile-only y*.htm aliases return 404s;
// neither is advertised as a feed here.
var clxySubPages = []string{
	"sylm_xwgg/xyxw",
	"sylm_xwgg/xygg",
	"jwgg",
	"jwgg/bks",
	"jwgg/yjs",
	"sylm_djgz",
	"sylm_djgz/tzgg",
	"sylm_djgz/djxw",
	"sylm_xsgz",
	"sylm_xsgz/gsgg",
	"sylm_xsgz/txkx",
	"sylm_xsgz/jyzp",
	"sylm_xshd",
	"sylm_xshd/xshb",
	"sylm_xshd/xsxw",
	"sylm_xshd/kjdt",
}

var (
	clxyArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	clxyDateRe        = regexp.MustCompile(`(20\d{2})\s*(?:年|[-/.])\s*(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*(?:日|号)?`)
	clxyDateTimeRe    = regexp.MustCompile(`(20\d{2})\s*(?:年|[-/.])\s*(\d{1,2})\s*(?:月|[-/.])\s*(\d{1,2})\s*(?:日|号)?\s+(\d{1,2}):(\d{2})(?::(\d{2}))?`)
)

func init() {
	registerAdapter("clxy", SiteAdapter{
		SubPages:     clxySubPages,
		ParseList:    parseAdapter_clxyList,
		ParseArticle: parseAdapter_clxyArticle,
		PageURL:      clxyPageURL,
	})
}

// clxyPageURL maps the logical page number (1 = newest) to the generated
// filename used by clxy. The unsuffixed page is page 1; the site's link for
// logical page 2 is /<slug>/<total-1>.htm and its last page is /<slug>/1.htm.
func clxyPageURL(baseURL, slug string, page, totalPages int, extension string) string {
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

func parseAdapter_clxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse clxy list: %w", err)
	}
	if clxyErrorDocument(doc) {
		return nil, fmt.Errorf("clxy: response is an error or permission page")
	}

	// All live archive pages expose their actual rows in this container. It is
	// deliberately checked before any home-page fallback so the mobile copy,
	// sidebars and pagination links cannot become articles.
	if rows := clxyListRows(doc); rows.Length() > 0 {
		return clxyParseRows(rows, doc, baseURL), nil
	}
	if doc.Find(".conlistComm").Length() > 0 {
		// A real but currently empty list is a valid empty result.
		return []model.NewsItem{}, nil
	}

	// index.htm is an aggregation page. Its desktop content contains all
	// server-rendered tabs, including tabs hidden by CSS until clicked.
	if home := clxyHomeContent(doc); home.Length() > 0 {
		items := clxyParseHome(home, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("clxy: home page contains no article entries")
		}
		return items, nil
	}

	return nil, fmt.Errorf("clxy: response is not a supported article list")
}

func clxyListRows(doc *goquery.Document) *goquery.Selection {
	if rows := doc.Find(".conlistComm > dl"); rows.Length() > 0 {
		return rows
	}
	return doc.Find(`dl[id^="line_"]`)
}

func clxyParseRows(rows *goquery.Selection, doc *goquery.Document, baseURL string) []model.NewsItem {
	defaultType := clxyBreadcrumbType(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())

	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := clxyArticleAnchor(row, baseURL)
		if anchor.Length() == 0 {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL, ok := clxyArticleURL(baseURL, href)
		if !ok || seen[fullURL] {
			return
		}

		title := clxyRowTitle(row, anchor)
		if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := clxyDateValues(row.Find("dd").Text())
		if date == "" {
			date, publishedAt = clxyDateValues(row.Text())
		}
		typ := clxyRowType(row)
		if typ == "" {
			typ = defaultType
		}

		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     typ,
			ParsedAt: time.Now(),
		}
		if publishedAt != "" && publishedAtHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}
		seen[fullURL] = true
		items = append(items, item)
	})
	return items
}

func clxyArticleAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	var found *goquery.Selection
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := clxyArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			found = anchor
			return false
		}
		return true
	})
	if found == nil {
		return row.Find("a[href]").First()
	}
	return found
}

func clxyRowTitle(row, anchor *goquery.Selection) string {
	if value := strings.TrimSpace(anchor.AttrOr("title", "")); value != "" {
		return cleanTitle(value)
	}
	return cleanTitle(strings.TrimSpace(anchor.Text()))
}

func clxyRowType(row *goquery.Selection) string {
	label := strings.TrimSpace(row.Find("dt > span a").First().Text())
	if label == "" {
		label = strings.TrimSpace(row.Find("dt span a").First().Text())
	}
	return strings.TrimSpace(strings.Trim(label, "[]【】"))
}

func clxyHomeContent(doc *goquery.Document) *goquery.Selection {
	return doc.Find("#pc > .content").First()
}

func clxyParseHome(home *goquery.Selection, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	home.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if clxyHomeDecoration(anchor) {
			return
		}
		fullURL, ok := clxyArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		title := clxyHomeTitle(anchor)
		if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := clxyDateValues(clxyHomeDateText(anchor))
		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     clxyHomeType(anchor),
			ParsedAt: time.Now(),
		}
		if publishedAt != "" && publishedAtHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}

		if index, exists := seen[fullURL]; exists {
			// The same article is often present in the "材料要闻" image block
			// and the ordinary news list. Keep the first occurrence/order, but
			// use later metadata when the first presentation lacks it.
			if items[index].Date == "" && item.Date != "" {
				items[index].Date = item.Date
			}
			if items[index].PublishedAt == "" && item.PublishedAt != "" {
				items[index].PublishedAt = item.PublishedAt
			}
			if items[index].Type == "" && item.Type != "" {
				items[index].Type = item.Type
			}
			if clxyTypeIsBetter(item.Type, items[index].Type) {
				items[index].Type = item.Type
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, item)
	})
	return items
}

func clxyTypeIsBetter(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	current = strings.TrimSpace(current)
	if candidate == "" || candidate == current {
		return false
	}
	if current == "" {
		return true
	}
	return current == "材料要闻"
}

func clxyHomeDecoration(anchor *goquery.Selection) bool {
	if isInNavArea(anchor) {
		return true
	}
	return anchor.Closest(".iosSlider, .slider, .swiper, .carousel, .focus, .hero, .banner, .four-nav, .sidebar, .side, .widget").Length() > 0
}

func clxyHomeTitle(anchor *goquery.Selection) string {
	if value := strings.TrimSpace(anchor.AttrOr("title", "")); value != "" {
		return cleanTitle(value)
	}
	clone := anchor.Clone()
	clone.Find(".date, .time, time, .hot").Remove()
	if value := strings.TrimSpace(clone.Text()); value != "" {
		return cleanTitle(value)
	}
	for _, selector := range []string{"img[title]", "img[alt]"} {
		if value := strings.TrimSpace(anchor.Find(selector).First().AttrOr("title", "")); value != "" {
			return cleanTitle(value)
		}
		if value := strings.TrimSpace(anchor.Find(selector).First().AttrOr("alt", "")); value != "" {
			return cleanTitle(value)
		}
	}
	return ""
}

func clxyHomeDateText(anchor *goquery.Selection) string {
	if value := strings.TrimSpace(anchor.Find(".date, .time, time").First().Text()); value != "" {
		return value
	}
	if row := anchor.Closest("li"); row.Length() > 0 {
		return strings.TrimSpace(row.Find(".date, .time, time").First().Text())
	}
	return ""
}

func clxyHomeType(anchor *goquery.Selection) string {
	if tabs := anchor.Closest(".tabs-content"); tabs.Length() > 0 {
		panel := tabs.Closest(".item-s, .item-long")
		if panel.Length() > 0 {
			index := anchor.Closest("ul[data-index]").AttrOr("data-index", "")
			if n, err := strconv.Atoi(strings.TrimSpace(index)); err == nil && n > 0 {
				if label := strings.TrimSpace(panel.Find(".tabs-choose a").Eq(n - 1).Text()); label != "" {
					return label
				}
			}
		}
	}
	panel := anchor.Closest(".item-s, .item-long")
	if panel.Length() == 0 {
		return ""
	}
	heading := strings.TrimSpace(panel.Find("h3.title").First().Text())
	heading = strings.TrimSpace(strings.TrimSuffix(heading, "更多"))
	return heading
}

func clxyArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !clxyArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	return fullURL, true
}

func clxyDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "", ""
	}
	if match := clxyDateTimeRe.FindString(value); match != "" {
		if precise := normalizePublishedAt(match); precise != "" {
			return precise[:10], precise
		}
	}
	if match := clxyDateRe.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, ""
	}
	return "", ""
}

func clxyBreadcrumbType(doc *goquery.Document) string {
	for _, selector := range []string{".current h3", ".main_left .submenu_cur a", ".current a[href*='.htm']"} {
		if value := strings.TrimSpace(doc.Find(selector).First().Text()); value != "" && value != "正文" {
			return value
		}
	}
	return ""
}

func parseAdapter_clxyArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse clxy article: %w", err)
	}
	if clxyErrorDocument(doc) {
		return nil, fmt.Errorf("clxy: response is an error or permission page")
	}
	if doc.Find(".article h1").First().Length() == 0 || doc.Find(".article .v_news_content").First().Length() == 0 {
		return nil, fmt.Errorf("clxy: response is not an article detail page")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if title := strings.TrimSpace(doc.Find(".article h1").First().Text()); title != "" {
		article.Title = title
	}
	if date, publishedAt := clxyDateValues(doc.Find(".article .source").First().Text()); date != "" {
		article.Date = date
		if publishedAt != "" {
			article.PublishedAt = publishedAt
		}
	} else if date, publishedAt := clxyDateValues(doc.Find(".meta-info").First().Text()); date != "" {
		article.Date = date
		if publishedAt != "" {
			article.PublishedAt = publishedAt
		}
	}
	if typ := clxyBreadcrumbType(doc); typ != "" {
		article.Type = typ
	}
	appendClxyAttachments(article, doc, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func appendClxyAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}

	doc.Find(".content a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" || !clxyAttachmentLink(anchor, href) {
			return
		}
		resolved := resolveURL(baseURL, href)
		if seen[resolved] {
			return
		}
		name := strings.TrimSpace(anchor.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(anchor.AttrOr("name", ""))
		}
		if name == "" {
			name = strings.TrimSpace(anchor.Text())
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func clxyAttachmentLink(anchor *goquery.Selection, href string) bool {
	hrefLower := strings.ToLower(href)
	if strings.Contains(hrefLower, "download.jsp") || strings.Contains(hrefLower, "download") {
		return true
	}
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(hrefLower, ext) {
			return true
		}
	}
	for _, ancestor := range []*goquery.Selection{anchor, anchor.Parent(), anchor.Parent().Parent()} {
		if ancestor != nil && strings.Contains(strings.TrimSpace(ancestor.Text()), "附件") {
			return true
		}
	}
	return false
}

func clxyErrorDocument(doc *goquery.Document) bool {
	title := strings.ToLower(strings.TrimSpace(doc.Find("title").First().Text()))
	if strings.Contains(title, "系统提示") || strings.Contains(title, "404错误") {
		return true
	}
	body := strings.Join(strings.Fields(doc.Find("body").Text()), " ")
	return strings.Contains(body, "您没有访问当前栏目的权限") || strings.Contains(body, "404错误提示")
}
