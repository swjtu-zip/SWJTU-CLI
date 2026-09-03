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

// gsxx is a real, currently empty public category and is kept in the feed
// list so an empty source is distinguishable from an omitted tab.
var ygbSubPages = []string{"xwzx/xwdt", "xwzx/tzgg", "xwzx/dcxy", "xwzx/hdyg", "xwzx/ssfc", "xwzx/gsxx", "djyl1/xzzg"}

var ygbFeedTypes = map[string]string{
	"xwzx/xwdt":  "新闻动态",
	"xwzx/tzgg":  "通知公告",
	"xwzx/dcxy":  "多彩学院",
	"xwzx/hdyg":  "活动预告",
	"xwzx/ssfc":  "师生风采",
	"xwzx/gsxx":  "公示信息",
	"djyl1/xzzg": "党建引领",
}

var ygbArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var ygbPageCounterRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)
var ygbSplitDateRE = regexp.MustCompile(`(20\d{2})\s+(\d{1,2})\s*[/.-]\s*(\d{1,2})`)

func init() {
	registerAdapter("ygb", SiteAdapter{
		SubPages:     ygbSubPages,
		ParseList:    parseAdapter_ygbList,
		ParseArticle: parseAdapter_ygbArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   ygbTotalPages,
	})
}

func parseAdapter_ygbList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ygb: parse list HTML: %w", err)
	}
	if ygbIsHomepage(doc) {
		items := ygbHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("ygb: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := doc.Find(".newsList").First()
	if container.Length() == 0 {
		return nil, fmt.Errorf("ygb: response has no newsList container")
	}
	return ygbParseRows(container, baseURL, ygbDocumentType(doc)), nil
}

func ygbIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" ||
		doc.Find(".newsBox2, .schoolNews, .newsNotice").Length() > 0 && doc.Find(".newsList").Length() == 0
}

func ygbHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	// The home page has three independently rendered blocks: newsBox contains
	// notice/news, newsBox3 contains school news/faculty style, and newsBox2
	// contains public announcements/events. Scope each column separately so a
	// heading from the neighbouring column cannot become its type.
	doc.Find(".newsBox, .newsBox2, .newsBox3").Each(func(_ int, box *goquery.Selection) {
		box.Find(".newsNotice, .newsClassify").Each(func(_ int, section *goquery.Selection) {
			typ := ygbHomepageColumnType(section)
			if typ == "" {
				typ = ygbDocumentType(doc)
			}
			section.Find("a[href*='info/']").Each(func(_ int, anchor *goquery.Selection) {
				ygbAppendItem(&items, seen, anchor, anchor.Closest("li, .newsImg, .newsInfo, .schoolNews"), typ, baseURL)
			})
		})
	})
	return items
}

func ygbHomepageColumnType(section *goquery.Selection) string {
	if section == nil || section.Length() == 0 {
		return ""
	}
	for _, href := range []string{
		section.Find("a[href*='xwzx/'], a[href*='djyl1/']").First().AttrOr("href", ""),
	} {
		route := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(strings.SplitN(href, "?", 2)[0]), ".html"), ".htm")
		if typ := ygbFeedTypes[route]; typ != "" {
			return typ
		}
	}
	for _, selector := range []string{".noticeTitle .title", ".teachingDynamics .title", ".title"} {
		value := strings.Join(strings.Fields(section.Find(selector).First().Text()), " ")
		for _, typ := range ygbFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	return ""
}

func ygbDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-title", ".position h1", ".newsTitle", ".title h2", ".title h3", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		for _, typ := range ygbFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	for _, typ := range ygbFeedTypes {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func ygbParseRows(container *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	rows := container.Find("li")
	if rows.Length() == 0 {
		rows = container.Find("a[href*='info/']").Parent()
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		ygbAppendItem(&items, seen, row.Find("a[href*='info/']").First(), row, typ, baseURL)
	})
	return items
}

func ygbAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if href == "" || !ygbArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".newsItem, .infoText, .text, .word, .noticeWord, .title, h3, h4").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := ygbDateValues(row)
	if typ == "" {
		typ = strings.Trim(strings.TrimSpace(row.Find(".tag, .category, .type").First().Text()), "[]【】")
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func ygbDateValues(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	for _, selector := range []string{".newsDate", ".infoDate", ".noticeDate", ".date", ".time", "time"} {
		if value := strings.Join(strings.Fields(row.Find(selector).First().Text()), " "); value != "" {
			if date, publishedAt := ygbDateText(value); date != "" {
				return date, publishedAt
			}
		}
	}
	return ygbDateText(strings.Join(strings.Fields(row.Text()), " "))
}

func ygbDateText(value string) (string, string) {
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
	if match := ygbSplitDateRE.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func ygbTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if ygbFeedTypes[slug] == "" {
		return 0, fmt.Errorf("ygb: %q is not an article-list route", slug)
	}
	baseURL := "https://ygb.swjtu.edu.cn"
	if site := AllSites["ygb"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("ygb: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("ygb: parse first page for %s: %w", slug, err)
	}
	if doc.Find(".newsList").Length() == 0 {
		return 0, fmt.Errorf("ygb: %s is not an article list page", slug)
	}
	if total := ygbPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func ygbPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .p_pages, .paging, .page").Text()), " ")
	if match := ygbPageCounterRE.FindStringSubmatch(text); match != nil {
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

func parseAdapter_ygbArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ygb: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("ygb: response has no article body")
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	if title := strings.Join(strings.Fields(doc.Find(".news-title, .article-title, .detail-title, h1").First().Text()), " "); title != "" {
		article.Title = title
	}
	if article.Type == "" {
		article.Type = ygbDocumentType(doc)
	}
	ygbAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func ygbAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		if href == "" || ygbArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
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

func ygbArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && ygbArticlePathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}
