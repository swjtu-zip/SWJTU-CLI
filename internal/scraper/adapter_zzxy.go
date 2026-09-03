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

var zzxySubPages = []string{"xyyw", "xwdt", "tzgg", "xsgz/xsdt", "djjgh/djgz/djfc", "szkc", "cgzs", "xsgg"}

var zzxyFeedTypes = map[string]string{
	"xyyw":            "学院要闻",
	"xwdt":            "新闻动态",
	"tzgg":            "通知公告",
	"xsgz/xsdt":       "学生工作",
	"djjgh/djgz/djfc": "党建风采",
	"szkc":            "思政课程",
	"cgzs":            "成果展示",
	"xsgg":            "学生公告",
}

var zzxyArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var zzxyPageCounterRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)

// zzxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("zzxy", SiteAdapter{
		SubPages:     zzxySubPages,
		ParseList:    parseAdapter_zzxyList,
		ParseArticle: parseAdapter_zzxyArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   zzxyTotalPages,
	})
}

func parseAdapter_zzxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}

	// The ordinary feeds use a plain <ul class="list">; szkc uses the
	// conlistComm/dl shell. Neither shape is covered by the generic parser.
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".right ul.list > li").Each(func(_ int, row *goquery.Selection) {
		a := row.Find(".tit a[href]").First()
		if a.Length() == 0 {
			return
		}
		title := cleanTitle(a.Text())
		href := strings.TrimSpace(a.AttrOr("href", ""))
		if title == "" || href == "" || !isValidNewsTitle(title) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, href))
		if seen[fullURL] || !isValidNewsURL(fullURL) {
			return
		}

		dateText := strings.TrimSpace(row.Find(".time").Text())
		date, publishedAt := zzxyDateValues(dateText)
		typ := strings.TrimSpace(row.Find(".tag").Text())
		typ = strings.TrimSpace(strings.Trim(typ, "[]【】"))
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt,
			Type: typ, ParsedAt: time.Now(),
		})
	})
	// Homepage columns use the same list class but put the title in <p> and
	// obtain the type from the adjacent global-title heading. The older .tit
	// parser above is retained for archive pages and legacy templates.
	doc.Find("ul.global-list.list > li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if !zzxyArticleURL(fullURL, baseURL) || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Find("p, .tit, .title, em, h3, h4").First().Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := zzxyDateValues(row.Find(".time, .date").First().Text())
		typ := zzxyDocumentTypeForRow(row)
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	doc.Find(".conlistComm dl").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("dt a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if !zzxyArticleURL(fullURL, baseURL) || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := zzxyDateValues(row.Find("dd, .time, .date").First().Text())
		typ := strings.Trim(strings.TrimSpace(row.Find("dt .tag, dt .type, dt span").First().Text()), "[]【】")
		if typ == "" {
			typ = zzxyDocumentType(doc)
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	if len(items) > 0 {
		return items, nil
	}
	if doc.Find(".right ul.list, ul.global-list.list, .conlistComm").Length() > 0 {
		return items, nil
	}
	return parseBodaNewsList(html, baseURL)
}

func zzxyDocumentTypeForRow(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	for _, scopeSelector := range []string{".right", ".left", ".news-list", ".section-list"} {
		scope := row.Closest(scopeSelector)
		if scope.Length() == 0 {
			continue
		}
		value := strings.Join(strings.Fields(scope.Find(".global-title h3, .title h3, .title").First().Text()), " ")
		for _, typ := range zzxyFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	return ""
}

func zzxyDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-title", ".position h1", ".right .title h3", ".right .title", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		for _, typ := range zzxyFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	for _, typ := range zzxyFeedTypes {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func zzxyTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if zzxyFeedTypes[slug] == "" {
		return 0, fmt.Errorf("zzxy: %q is not an article-list route", slug)
	}
	baseURL := "http://zzxy.swjtu.edu.cn"
	if site := AllSites["zzxy"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("zzxy: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("zzxy: parse first page for %s: %w", slug, err)
	}
	if doc.Find(".right ul.list, .conlistComm").Length() == 0 {
		return 0, fmt.Errorf("zzxy: %s is not an article list page", slug)
	}
	if total := zzxyPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func zzxyPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .p_pages, .paging, .page").Text()), " ")
	if match := zzxyPageCounterRE.FindStringSubmatch(text); match != nil {
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

func parseAdapter_zzxyArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("zzxy: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("zzxy: response has no article body")
	}
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil || article == nil {
		return article, err
	}

	if dateText := strings.TrimSpace(doc.Find(".author").First().Text()); dateText != "" {
		if date, publishedAt := zzxyDateValues(dateText); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		}
	}
	zzxyAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

var zzxyDateRe = regexp.MustCompile(`20\d{2}\s*(?:[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}日?)?(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?`)

func zzxyDateValues(text string) (string, string) {
	match := zzxyDateRe.FindString(strings.Join(strings.Fields(text), " "))
	if match == "" {
		return "", ""
	}
	precise := normalizePublishedAt(match)
	if precise == "" {
		return "", ""
	}
	return precise[:10], precise
}

func zzxyArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && zzxyArticlePathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}

func zzxyAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		if href == "" || zzxyArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
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
