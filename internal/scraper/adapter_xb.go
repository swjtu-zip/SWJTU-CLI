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

var xbSubPages = []string{"xxfb/tzgg", "xxfb/xwdt", "cyxz", "fwzn/gflc", "fwzn/glwj", "zdgf/bgswj", "zdgf/xjwj"}

var xbFeedTypes = map[string]string{
	"xxfb/tzgg":  "通知公告",
	"xxfb/xwdt":  "新闻动态",
	"cyxz":       "常用下载",
	"fwzn/gflc":  "工作流程",
	"fwzn/glwj":  "管理文件",
	"zdgf/bgswj": "办公室文件",
	"zdgf/xjwj":  "校级文件",
}

var xbArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)

func init() {
	registerAdapter("xb", SiteAdapter{
		SubPages:     xbSubPages,
		ParseList:    parseAdapter_xbList,
		ParseArticle: parseAdapter_xbArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   xbTotalPages,
	})
}

func parseAdapter_xbList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xb: parse list HTML: %w", err)
	}
	if xbIsHomepage(doc) {
		items := xbHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("xb: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := doc.Find(".news-list-box").First()
	if container.Length() == 0 {
		return nil, fmt.Errorf("xb: response has no news-list-box container")
	}
	typ := xbDocumentType(doc)
	return xbParseRows(container, baseURL, typ), nil
}

func xbIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" ||
		doc.Find(".list-box-div .scien-list").Length() > 0
}

func xbHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".list-box-div").Each(func(_ int, box *goquery.Selection) {
		typ := strings.Join(strings.Fields(box.Find(".plane-title, .plane-title .tz").First().Text()), " ")
		if typ == "" {
			typ = strings.Join(strings.Fields(box.Find(".title h2, .title h3").First().Text()), " ")
		}
		box.Find("ul.scien-list li").Each(func(_ int, row *goquery.Selection) {
			xbAppendItem(&items, seen, row.Find("a[href]").First(), row, typ, baseURL)
		})
	})
	return items
}

func xbDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-title", ".list-head", ".position h1", ".ny-title", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if value == "" {
			continue
		}
		for _, typ := range xbFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	for _, typ := range xbFeedTypes {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func xbParseRows(container *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	rows := container.Find("li")
	if rows.Length() == 0 {
		rows = container.Find("a[href*='info/']").Parent()
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			anchor = row.Find("a[href]").First()
		}
		xbAppendItem(&items, seen, anchor, row, typ, baseURL)
	})
	return items
}

func xbAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	base, baseErr := url.Parse(baseURL)
	if href == "" || err != nil || baseErr != nil || !xbArticlePathRE.MatchString(parsed.Path) || !strings.EqualFold(parsed.Host, base.Host) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".new-l_b_title, .line1, .title, .news-title, h3, h4, em").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := xbDateValues(row.Text())
	if typ == "" {
		typ = strings.Trim(strings.TrimSpace(row.Find(".tag, .category, .type").First().Text()), "[]【】")
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func xbDateValues(value string) (string, string) {
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

func xbTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if xbFeedTypes[slug] == "" {
		return 0, fmt.Errorf("xb: %q is not an article-list route", slug)
	}
	baseURL := "https://xb.swjtu.edu.cn"
	if site := AllSites["xb"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("xb: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xb: parse first page for %s: %w", slug, err)
	}
	if doc.Find(".news-list-box").Length() == 0 {
		return 0, fmt.Errorf("xb: %s is not a news list page", slug)
	}
	if total := xbPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func xbPageCount(doc *goquery.Document) int {
	maxPage := 0
	if doc == nil {
		return 0
	}
	doc.Find(".p_pages .p_no, .p_pages .p_no_d, .p_no, .p_no_d").Each(func(_ int, node *goquery.Selection) {
		var value int
		fmt.Sscanf(strings.TrimSpace(node.Text()), "%d", &value)
		if value > maxPage {
			maxPage = value
		}
	})
	if maxPage > 0 {
		return maxPage
	}
	return 0
}

func parseAdapter_xbArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xb: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("xb: response has no article body")
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	if title := strings.Join(strings.Fields(doc.Find(".news-title, .article-title, .detail-title, h1").First().Text()), " "); title != "" {
		article.Title = title
	}
	if article.Type == "" {
		article.Type = xbDocumentType(doc)
	}
	xbAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func xbAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		if href == "" || xbArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
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

func xbArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && xbArticlePathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}
