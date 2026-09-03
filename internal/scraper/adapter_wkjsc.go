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

var wkjscSubPages = []string{"tzgg", "wkxw", "xsdt", "gzzd", "xzzx", "bszn"}

var wkjscFeedTypes = map[string]string{
	"tzgg": "通知公告",
	"wkxw": "文科新闻",
	"xsdt": "学术动态",
	"gzzd": "规章制度",
	"xzzx": "下载中心",
	"bszn": "办事指南",
}

// wkjsc.swjtu.edu.cn has four explicit news lists on its home page and uses
// the same news_ul1 list shell for every archive page. The generic parser only
// knows news_ul, and its fallback cannot see the legacy fanye counter.
func init() {
	registerAdapter("wkjsc", SiteAdapter{
		SubPages:     wkjscSubPages,
		ParseList:    parseAdapter_wkjscList,
		ParseArticle: parseAdapter_wkjscArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   wkjscTotalPages,
	})
}

var wkjscArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var (
	wkjscDateRE   = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?`)
	wkjscSourceRE = regexp.MustCompile(`来源\s*[:：]?\s*([^\s\[\]]+)`)
)

const wkjscSiteName = "西南交通大学文科建设处"

func parseAdapter_wkjscList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse wkjsc list: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	listShell := doc.Find("ul.news_ul, ul.news_ul1")
	if listShell.Length() == 0 {
		return nil, fmt.Errorf("wkjsc: response has no news_ul/news_ul1 list shell")
	}
	add := func(anchor *goquery.Selection, scope *goquery.Selection, typ string) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL := cleanURL(resolveURL(baseURL, href))
		if !wkjscArticleURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}

		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 100 {
			return
		}
		date := wkjscDate(scope)
		if index, ok := seen[fullURL]; ok {
			if items[index].Date == "" && date != "" {
				items[index].Date = date
				items[index].PublishedAt = normalizePublishedAt(date)
			}
			if items[index].Type == "" {
				items[index].Type = typ
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{
			Title: title, URL: fullURL, Date: date,
			PublishedAt: normalizePublishedAt(date), Type: typ, ParsedAt: time.Now(),
		})
	}

	// These lists are the source of truth for the page's four visible feeds.
	doc.Find("ul.news_ul li, ul.news_ul1 li").Each(func(_ int, row *goquery.Selection) {
		typ := wkjscFeedTypeForRow(row)
		if typ == "" {
			typ = wkjscListType(doc)
		}
		add(row.Find("a[href]").First(), row, typ)
	})
	return items, nil
}

func wkjscFeedTypeForRow(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	// The homepage nests the news_ul lists below several anonymous wrappers;
	// walk every ancestor with an id instead of stopping at a class that may be
	// absent from a particular responsive rendering.
	for current := row; current.Length() > 0; current = current.Parent() {
		id := strings.TrimSpace(current.AttrOr("id", ""))
		if typ := wkjscFeedTypes[id]; typ != "" {
			return typ
		}
	}
	return ""
}

func wkjscListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-title", ".list_tit", ".title h2", ".title h3", ".ny-title", ".position h1", "h1", "h2"} {
		candidate := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if candidate == "" || candidate == wkjscSiteName || candidate == "文科建设处" {
			continue
		}
		for _, typ := range wkjscFeedTypes {
			if strings.Contains(candidate, typ) {
				return typ
			}
		}
	}
	return ""
}

func wkjscTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if wkjscFeedTypes[slug] == "" {
		return 0, fmt.Errorf("wkjsc: %q is not a supported article-list route", slug)
	}
	baseURL := "https://wkjsc.swjtu.edu.cn"
	if site := AllSites["wkjsc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("wkjsc: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("wkjsc: parse first page for %s: %w", slug, err)
	}
	if doc.Find("ul.news_ul, ul.news_ul1").Length() == 0 {
		return 0, fmt.Errorf("wkjsc: %s is not a news list page", slug)
	}
	if total := wkjscPageCount(doc); total > 0 {
		return total, nil
	}
	// A recognized list with p_no_d and no numbered link is a legitimate
	// one-page feed, including the small bszn/gzzd/xzzx archives.
	return 1, nil
}

var wkjscPageCountRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)

func wkjscPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	for _, value := range []string{
		strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .p_pages, .paging, .page").Text()), " "),
		doc.Text(),
	} {
		if match := wkjscPageCountRE.FindStringSubmatch(value); match != nil {
			var total int
			fmt.Sscanf(match[1], "%d", &total)
			if total > 0 {
				return total
			}
		}
	}
	return 0
}

func wkjscArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || !wkjscArticlePathRE.MatchString(parsed.Path) {
		return false
	}
	return !isExternalURL(rawURL, baseURL)
}

func wkjscDate(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(scope.Find(".date").First().Text()), " ")
	if text == "" {
		text = strings.Join(strings.Fields(scope.Text()), " ")
	}
	date, _ := wkjscDateValues(text)
	return date
}

func normalizeWkjscDate(text string) string {
	date, _ := wkjscDateValues(text)
	return date
}

func wkjscDateValues(text string) (string, string) {
	text = strings.Join(strings.Fields(text), " ")
	if match := dateTimeRe.FindString(text); match != "" {
		if published := normalizePublishedAt(match); len(published) >= 10 {
			return published[:10], published
		}
	}
	match := wkjscDateRE.FindStringSubmatch(text)
	if match == nil {
		return "", ""
	}
	date := normalizeDateParts(match[1], match[2], match[3])
	return date, date
}

func wkjscArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}

	// The detail template renders the real headline as #div_title #lb_title.
	// The document title and the first pageTitle meta instead identify the site.
	for _, selector := range []string{"#div_title #lb_title", "#lb_title"} {
		if title := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); title != "" {
			return title
		}
	}
	return ""
}

func wkjscFallbackTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}

	var title string
	doc.Find(`meta[name="pageTitle"]`).EachWithBreak(func(_ int, meta *goquery.Selection) bool {
		candidate := strings.Join(strings.Fields(meta.AttrOr("content", "")), " ")
		if candidate != "" && candidate != wkjscSiteName {
			title = candidate
			return false
		}
		return true
	})
	if title != "" {
		return title
	}

	title = stripSiteSuffix(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	if title == wkjscSiteName {
		return ""
	}
	return title
}

func parseAdapter_wkjscArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("wkjsc: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("wkjsc: response has no article body")
	}
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if title := wkjscArticleTitle(doc); title != "" {
		article.Title = title
	} else if article.Title == wkjscSiteName {
		// Keep the site name from becoming an article title if an older detail
		// page omits the legacy span but still exposes a distinct pageTitle.
		article.Title = wkjscFallbackTitle(doc)
	}
	if meta := strings.Join(strings.Fields(doc.Find("#lb_hits").First().Text()), " "); meta != "" {
		if date, publishedAt := wkjscDateValues(meta); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		}
		if article.Source == "" {
			if match := wkjscSourceRE.FindStringSubmatch(meta); match != nil {
				if source := cleanLabelValue(match[1]); source != "" {
					article.Source = source
				}
			}
		}
	}
	if article.Type == "" {
		article.Type = wkjscListType(doc)
	}
	wkjscAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func wkjscAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		if href == "" || wkjscArticleURL(cleanURL(resolveURL(baseURL, href)), baseURL) {
			return
		}
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if seen[resolved] {
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
