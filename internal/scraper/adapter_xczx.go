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

var xczxSubPages = []string{
	"tzxw/tzgg", "tzxw/xwdt", "tzxw/xydt", "xsyt/xsfx", "xsyt/xsjl", "xsyt/zjtd",
	"ktyj/kfkt", "ktyj/yjcg", "ktyj/yjxm", "cyzx/xmch", "cyzx/zxfw",
}

var xczxFeedTypes = map[string]string{
	"tzxw/tzgg": "通知公告",
	"tzxw/xwdt": "新闻动态",
	"tzxw/xydt": "行业动态",
	"xsyt/xsfx": "学术方向",
	"xsyt/xsjl": "学术交流",
	"xsyt/zjtd": "专家团队",
	"ktyj/kfkt": "开放课题",
	"ktyj/yjcg": "研究成果",
	"ktyj/yjxm": "研究项目",
	"cyzx/xmch": "项目策划",
	"cyzx/zxfw": "资讯服务",
}

var xczxArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var xczxPageCounterRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)

func init() {
	registerAdapter("xczx", SiteAdapter{
		SubPages:     xczxSubPages,
		ParseList:    parseAdapter_xczxList,
		ParseArticle: parseAdapter_xczxArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   xczxTotalPages,
	})
}

func parseAdapter_xczxList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xczx: parse list HTML: %w", err)
	}
	if xczxIsHomepage(doc) {
		items := xczxHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("xczx: homepage contains no scoped article entries")
		}
		return items, nil
	}
	container := doc.Find(".main_conRCb").First()
	if container.Length() == 0 {
		return nil, fmt.Errorf("xczx: response has no main_conRCb list container")
	}
	return xczxParseRows(container, baseURL, xczxDocumentType(doc)), nil
}

func xczxIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" ||
		doc.Find(".new_inforBox, .informBox").Length() > 0
}

func xczxHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".new_list ul.dynamic_list li, .infor_list ul.dynamic_list li").Each(func(_ int, row *goquery.Selection) {
		typ := strings.Join(strings.Fields(row.Closest(".vsb-space, .new_inforBox, .informBox").Find("h2, h3").First().Text()), " ")
		xczxAppendItem(&items, seen, row.Find("a[href]").First(), row, typ, baseURL)
	})
	return items
}

func xczxDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".main_titT", ".position h1", ".list-title", ".dynamic h2", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		for _, typ := range xczxFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	for _, typ := range xczxFeedTypes {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func xczxParseRows(container *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	container.Find("li").Each(func(_ int, row *goquery.Selection) {
		xczxAppendItem(&items, seen, row.Find("a[href*='info/']").First(), row, typ, baseURL)
	})
	return items
}

func xczxAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if href == "" || !xczxArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find("em, .title, .news-title, h3, h4").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	dateText := strings.TrimSpace(row.Find("span, .date, .time").First().Text())
	if dateText == "" {
		dateText = row.Text()
	}
	date, publishedAt := xczxDateValues(dateText)
	if typ == "" {
		typ = strings.Trim(strings.TrimSpace(row.Find(".tag, .category, .type").First().Text()), "[]【】")
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func xczxDateValues(value string) (string, string) {
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

func xczxTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if xczxFeedTypes[slug] == "" {
		return 0, fmt.Errorf("xczx: %q is not an article-list route", slug)
	}
	baseURL := "https://xczx.swjtu.edu.cn"
	if site := AllSites["xczx"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("xczx: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xczx: parse first page for %s: %w", slug, err)
	}
	if doc.Find(".main_conRCb").Length() == 0 {
		return 0, fmt.Errorf("xczx: %s is not an article list page", slug)
	}
	if total := xczxPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func xczxPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .p_pages, .paging, .page").Text()), " ")
	if match := xczxPageCounterRE.FindStringSubmatch(text); match != nil {
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

func parseAdapter_xczxArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xczx: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("xczx: response has no article body")
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	if title := strings.Join(strings.Fields(doc.Find(".news-title, .article-title, .detail-title, h1").First().Text()), " "); title != "" {
		article.Title = title
	}
	if article.Type == "" {
		article.Type = xczxDocumentType(doc)
	}
	xczxAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func xczxAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		if href == "" || xczxArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
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

func xczxArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && xczxArticlePathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}
