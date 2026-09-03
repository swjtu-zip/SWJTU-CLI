package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// xyll is a static contact-information page with a recommendation sidebar,
// not an article feed. Keeping it out prevents those six sidebar links from
// becoming fake news items.
var xybSubPages = []string{
	"xwdt1", "xwdt1/djgz", "xwdt1/tzgg", "xwdt1/xwzx", "xwdt1/xwzx/jj", "xwdt1/xwzx/xy",
	"xyzh/qnxyh", "xyzh/xyfc",
}

var xybFeedTypes = map[string]string{
	"xwdt1":         "新闻动态",
	"xwdt1/djgz":    "党建工作",
	"xwdt1/tzgg":    "通知公告",
	"xwdt1/xwzx":    "新闻资讯",
	"xwdt1/xwzx/jj": "基金",
	"xwdt1/xwzx/xy": "校友",
	"xyzh/qnxyh":    "青年校友会",
	"xyzh/xyfc":     "校友风采",
}

var xybArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var xybPageCounterRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)

func init() {
	registerAdapter("xyb", SiteAdapter{
		SubPages:     xybSubPages,
		ParseList:    parseAdapter_xybList,
		ParseArticle: parseAdapter_xybArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   xybTotalPages,
	})
}

func parseAdapter_xybList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xyb: parse list HTML: %w", err)
	}
	if xybIsHomepage(doc) {
		items := xybHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("xyb: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := xybListContainer(doc)
	if container.Length() == 0 {
		return nil, fmt.Errorf("xyb: response has no list_con/article-list container")
	}
	return xybParseRows(container, baseURL, xybDocumentType(doc)), nil
}

func xybIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" ||
		doc.Find(".main1 .content .con, .xyfc").Length() > 0 && doc.Find(".list_con").Length() == 0
}

func xybHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".main1 .content .con").Each(func(index int, section *goquery.Selection) {
		typ := "新闻资讯"
		if index == 1 {
			typ = "党建工作"
		} else if index == 2 {
			typ = "通知公告"
		}
		section.Find("li.contentli").Each(func(_ int, row *goquery.Selection) {
			xybAppendItem(&items, seen, row.Find("a[href*='info/']").First(), row, typ, baseURL)
		})
	})
	doc.Find(".xyhd").Each(func(_ int, section *goquery.Selection) {
		typ := strings.Join(strings.Fields(section.Find(".sjt24").First().Text()), " ")
		section.Find("a[href*='info/']").Each(func(_ int, anchor *goquery.Selection) {
			xybAppendItem(&items, seen, anchor, anchor.Closest("div[style*='margin-bottom'], .xyhd"), typ, baseURL)
		})
	})
	return items
}

func xybListContainer(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	for _, selector := range []string{".list_con", ".xyfc", ".news-list"} {
		if list := doc.Find(selector).First(); list.Length() > 0 {
			return list
		}
	}
	return &goquery.Selection{}
}

func xybDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-title", ".position h1", ".title h2", ".title h3", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		for _, typ := range xybFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	for _, typ := range xybFeedTypes {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func xybParseRows(container *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	rows := container.Find("li")
	if rows.Length() == 0 {
		rows = container.Find("a[href*='info/']").Parent()
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		xybAppendItem(&items, seen, row.Find("a[href*='info/']").First(), row, typ, baseURL)
	})
	return items
}

func xybAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if href == "" || !xybArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".infoText, .text, .title, .locationsj, .list-title, em, h3, h4").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := xybDateValues(row.Text())
	if typ == "" {
		typ = strings.Trim(strings.TrimSpace(row.Find(".tag, .category, .type").First().Text()), "[]【】")
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func xybDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(value), " ")
	if match := dateTimeRe.FindString(value); match != "" {
		if published := normalizePublishedAt(match); len(published) >= 10 {
			return published[:10], published
		}
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func xybTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if xybFeedTypes[slug] == "" {
		return 0, fmt.Errorf("xyb: %q is not an article-list route", slug)
	}
	baseURL := "https://xyb.swjtu.edu.cn"
	if site := AllSites["xyb"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("xyb: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xyb: parse first page for %s: %w", slug, err)
	}
	if xybListContainer(doc).Length() == 0 {
		return 0, fmt.Errorf("xyb: %s is not an article list page", slug)
	}
	if total := xybPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func xybPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .p_pages, .paging, .page").Text()), " ")
	if match := xybPageCounterRE.FindStringSubmatch(text); match != nil {
		var total int
		fmt.Sscanf(match[1], "%d", &total)
		return total
	}
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d, .p_no, .p_no_d").Each(func(_ int, node *goquery.Selection) {
		var value int
		fmt.Sscanf(strings.TrimSpace(node.Text()), "%d", &value)
		if value > maxPage {
			maxPage = value
		}
	})
	return maxPage
}

func parseAdapter_xybArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xyb: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("xyb: response has no article body")
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	if title := strings.Join(strings.Fields(doc.Find(".news-title, .article-title, .detail-title, h1").First().Text()), " "); title != "" {
		article.Title = title
	}
	if article.Type == "" {
		article.Type = xybDocumentType(doc)
	}
	xybAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func xybAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		resolved := cleanURL(resolveURL(baseURL, href))
		if href == "" || xybArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
			return
		}
		name := strings.Join(strings.Fields(link.AttrOr("download", "")), " ")
		if name == "" {
			name = strings.Join(strings.Fields(link.AttrOr("name", "")), " ")
		}
		if name == "" {
			name = strings.Join(strings.Fields(link.Text()), " ")
		}
		if name == "" {
			name = href
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func xybArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && xybArticlePathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}
