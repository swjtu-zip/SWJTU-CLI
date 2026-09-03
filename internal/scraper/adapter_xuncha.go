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

var xunchaSubPages = []string{
	"gzdt", "sjjs", "xxgongkai", "zcfg", "zcfg/tlztxx", "zcfg/dnfg", "zcfg/xxzd", "zyjh", "jyjl/tszs",
}

var xunchaFeedTypes = map[string]string{
	"gzdt":        "工作动态",
	"sjjs":        "审计介绍",
	"xxgongkai":   "信息公开",
	"zcfg":        "政策法规",
	"zcfg/tlztxx": "巡察专题信息",
	"zcfg/dnfg":   "党内法规",
	"zcfg/xxzd":   "学习制度",
	"zyjh":        "重要计划",
	"jyjl/tszs":   "经验交流",
}

var xunchaArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var xunchaPageCounterRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)

func init() {
	registerAdapter("xuncha", SiteAdapter{
		SubPages:     xunchaSubPages,
		ParseList:    parseAdapter_xunchaList,
		ParseArticle: parseAdapter_xunchaArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   xunchaTotalPages,
	})
}

func parseAdapter_xunchaList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xuncha: parse list HTML: %w", err)
	}
	container := doc.Find(".conlistComm").First()
	if container.Length() == 0 {
		return nil, fmt.Errorf("xuncha: response has no conlistComm list container")
	}
	typ := xunchaDocumentType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	container.Find("dl").Each(func(_ int, row *goquery.Selection) {
		xunchaAppendItem(&items, seen, row.Find("dt a[href*='info/']").First(), row, typ, baseURL)
	})
	return items, nil
}

func xunchaDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-title", ".position h1", ".local h2", ".title h2", ".title h3", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		for _, typ := range xunchaFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	for _, typ := range xunchaFeedTypes {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func xunchaAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if href == "" || !xunchaArticleURL(fullURL, baseURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".tit, .title, em, h3, h4").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := xunchaDateValues(row.Find("dd, .date, .time").First().Text())
	if date == "" {
		date, publishedAt = xunchaDateValues(row.Text())
	}
	rowType := strings.Trim(strings.TrimSpace(row.Find("dt .tag, dt .type, dt span").First().Text()), "[]【】")
	if rowType != "" {
		typ = rowType
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func xunchaDateValues(value string) (string, string) {
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

func xunchaTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if xunchaFeedTypes[slug] == "" {
		return 0, fmt.Errorf("xuncha: %q is not an article-list route", slug)
	}
	baseURL := "https://xuncha.swjtu.edu.cn"
	if site := AllSites["xuncha"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("xuncha: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("xuncha: parse first page for %s: %w", slug, err)
	}
	if doc.Find(".conlistComm").Length() == 0 {
		return 0, fmt.Errorf("xuncha: %s is not an article list page", slug)
	}
	if total := xunchaPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func xunchaPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .p_pages, .paging, .page").Text()), " ")
	if match := xunchaPageCounterRE.FindStringSubmatch(text); match != nil {
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

func parseAdapter_xunchaArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xuncha: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("xuncha: response has no article body")
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	for _, selector := range []string{".hd h3", ".news-title", ".article-title", ".detail-title", "h1"} {
		if title := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); title != "" {
			article.Title = title
			break
		}
	}
	if article.Type == "" {
		article.Type = xunchaDocumentType(doc)
	}
	xunchaAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func xunchaAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		if href == "" || xunchaArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
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

func xunchaArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && xunchaArticlePathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}
