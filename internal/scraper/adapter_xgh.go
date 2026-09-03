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

var xghSubPages = []string{"xwzx1", "tzgg", "jcdt", "jgfc", "szyl", "ghdj", "gzzd", "hdyx1", "bszn", "xzzx"}

var xghFeedTypes = map[string]string{
	"xwzx1": "新闻资讯",
	"tzgg":  "通知公告",
	"jcdt":  "基层动态",
	"jgfc":  "机关风采",
	"szyl":  "师资园地",
	"ghdj":  "工会党建",
	"gzzd":  "规章制度",
	"hdyx1": "活动掠影",
	"bszn":  "办事指南",
	"xzzx":  "下载中心",
}

var xghArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var xghPageCounterRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)

func init() {
	registerAdapter("xgh", SiteAdapter{
		SubPages:     xghSubPages,
		ParseList:    parseAdapter_xghList,
		ParseArticle: parseAdapter_xghArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   xghTotalPages,
	})
}

func parseAdapter_xghList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xgh: parse list HTML: %w", err)
	}
	if xghIsHomepage(doc) {
		items := xghHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("xgh: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := xghListContainer(doc)
	if container.Length() == 0 {
		return nil, fmt.Errorf("xgh: response has no article list container")
	}
	typ := xghDocumentType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	container.Find("li").Each(func(_ int, row *goquery.Selection) {
		xghAppendItem(&items, seen, row.Find("a[href*='info/']").First(), row, typ, baseURL)
	})
	return items, nil
}

func xghIsHomepage(doc *goquery.Document) bool {
	return doc != nil && strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1"
}

func xghHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".right ul.list, .left ul.list, .news-list ul.list, ul.list").Each(func(_ int, list *goquery.Selection) {
		typ := xghHomepageListType(list)
		if typ == "" {
			typ = xghDocumentType(doc)
		}
		list.Find("li").Each(func(_ int, row *goquery.Selection) {
			xghAppendItem(&items, seen, row.Find("a[href*='info/']").First(), row, typ, baseURL)
		})
	})
	return items
}

func xghHomepageListType(list *goquery.Selection) string {
	if list == nil || list.Length() == 0 {
		return ""
	}
	for _, scopeSelector := range []string{".right", ".left", ".news-list", ".section-list", ".section-box"} {
		scope := list.Closest(scopeSelector)
		if scope.Length() == 0 {
			continue
		}
		value := strings.Join(strings.Fields(scope.Find(".global-title h3, .title h3, .title").First().Text()), " ")
		for _, typ := range xghFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	return ""
}

func xghListContainer(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	for _, selector := range []string{".right ul.list", ".right .list ul", "ul.list"} {
		if list := doc.Find(selector).First(); list.Length() > 0 {
			return list
		}
	}
	return &goquery.Selection{}
}

func xghDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-title", ".position h1", ".right .title h3", ".right .title", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		for _, typ := range xghFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	for _, typ := range xghFeedTypes {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func xghAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if href == "" || !xghArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".tit, .title, .line1, .news-title, h3, h4, em").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := xghDateValues(row.Text())
	if typ == "" {
		typ = strings.Trim(strings.TrimSpace(row.Find(".tag, .category, .type").First().Text()), "[]【】")
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func xghDateValues(value string) (string, string) {
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

func xghTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if xghFeedTypes[slug] == "" {
		return 0, fmt.Errorf("xgh: %q is not an article-list route", slug)
	}
	baseURL := "https://xgh.swjtu.edu.cn"
	if site := AllSites["xgh"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("xgh: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xgh: parse first page for %s: %w", slug, err)
	}
	if xghListContainer(doc).Length() == 0 {
		return 0, fmt.Errorf("xgh: %s is not an article list page", slug)
	}
	if total := xghPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func xghPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .p_pages, .paging, .page").Text()), " ")
	if match := xghPageCounterRE.FindStringSubmatch(text); match != nil {
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

func parseAdapter_xghArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xgh: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("xgh: response has no article body")
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	for _, selector := range []string{".hd h3", ".article-title", ".detail-title", "h1", "h3"} {
		if title := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); title != "" {
			article.Title = title
			break
		}
	}
	if article.Type == "" {
		article.Type = xghDocumentType(doc)
	}
	xghAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func xghAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		if href == "" || xghArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
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

func xghArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && xghArticlePathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}
