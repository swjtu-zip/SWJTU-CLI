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

// These are the four article-list tabs exposed in the talent site's news
// section. jrjd/jrjdsy is a recruitment landing page (cards and profile
// links), not a homogeneous article archive, so it is intentionally not
// advertised as a news feed.
var talentSubPages = []string{"xwzx/rczc", "xwzx/tzgg", "xwzx/xwdt", "xwzx/xzzx"}

var talentFeedTypes = map[string]string{
	"xwzx/rczc": "人才政策",
	"xwzx/tzgg": "通知公告",
	"xwzx/xwdt": "新闻动态",
	"xwzx/xzzx": "下载中心",
}

var talentArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)

func init() {
	registerAdapter("talent", SiteAdapter{
		SubPages:     talentSubPages,
		ParseList:    parseAdapter_talentList,
		ParseArticle: parseAdapter_talentArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   talentTotalPages,
	})
}

func parseAdapter_talentList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("talent: parse list HTML: %w", err)
	}
	if talentIsHomepage(doc) {
		items := talentHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("talent: homepage contains no scoped article entries")
		}
		return items, nil
	}

	list := doc.Find("ul.news-list-container, ul.news-else-container, ul.news-list").First()
	if list.Length() == 0 {
		return nil, fmt.Errorf("talent: response has no news list container")
	}
	typ := talentDocumentType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	rows := list.Find("li.list-item")
	if rows.Length() == 0 {
		rows = list.ChildrenFiltered("li")
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			anchor = row.Find("a[href]").First()
		}
		talentAppendItem(&items, seen, anchor, row, typ, baseURL)
	})
	return items, nil
}

func talentIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" ||
		doc.Find(".news-information .list-gonggao").Length() > 0
}

func talentHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".news-information ul.list-gonggao").Each(func(_ int, list *goquery.Selection) {
		typ := talentHomeListType(list)
		list.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			talentAppendItem(&items, seen, anchor, anchor.Closest("li"), typ, baseURL)
		})
	})
	return items
}

func talentHomeListType(list *goquery.Selection) string {
	if list == nil || list.Length() == 0 {
		return ""
	}
	if box := list.Closest(".col-1"); box.Length() > 0 {
		if value := strings.Join(strings.Fields(box.Find(".news-title").First().Text()), " "); value != "" {
			return value
		}
	}
	if panel := list.Closest(".tabcons"); panel.Length() > 0 {
		switch strings.TrimSpace(panel.AttrOr("id", "")) {
		case "content-1":
			return "通知公告"
		case "content-2":
			return "人才政策"
		case "content-3":
			return "下载中心"
		}
	}
	return ""
}

func talentDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-title", ".list-head", ".article_category_bg h2", ".position h1", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		for _, typ := range talentFeedTypes {
			if strings.Contains(value, typ) {
				return typ
			}
		}
	}
	value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	for _, typ := range talentFeedTypes {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func talentAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := parseURL(fullURL)
	if href == "" || err != nil || !talentArticlePathRE.MatchString(parsed.Path) || !isValidNewsURL(fullURL) || seen[fullURL] {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Find(".title, .line1, .news-title, h3, h4").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 200 {
		return
	}
	date, publishedAt := talentDateValues(row.Text())
	if typ == "" {
		typ = strings.TrimSpace(row.Find(".tag, .category, .type").First().Text())
		typ = strings.Trim(typ, "[]【】")
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

// parseURL is kept local to this adapter so a malformed card URL cannot be
// mistaken for a relative article URL by the list parser.
func parseURL(raw string) (*url.URL, error) {
	return url.Parse(strings.TrimSpace(raw))
}

var talentDateRE = regexp.MustCompile(`(20\d{2})\s*(?:[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})日?|年\s*(\d{1,2})月\s*(\d{1,2})日?)`)

func talentDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(value), " ")
	if match := dateTimeRe.FindString(value); match != "" {
		if published := normalizePublishedAt(match); len(published) >= 10 {
			return published[:10], published
		}
	}
	if match := talentDateRE.FindStringSubmatch(value); match != nil {
		month, day := match[2], match[3]
		if month == "" {
			month, day = match[4], match[5]
		}
		date := normalizeDateParts(match[1], month, day)
		return date, date
	}
	return "", ""
}

func talentTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if talentFeedTypes[slug] == "" {
		return 0, fmt.Errorf("talent: %q is not an article-list route", slug)
	}
	baseURL := "https://talent.swjtu.edu.cn"
	if site := AllSites["talent"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("talent: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("talent: parse first page for %s: %w", slug, err)
	}
	if doc.Find("ul.news-list-container, ul.news-else-container, ul.news-list").Length() == 0 {
		return 0, fmt.Errorf("talent: %s is not a news list page", slug)
	}
	if total := talentPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func talentPageCount(doc *goquery.Document) int {
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
	return parseTotalPages([]byte(doc.Text()))
}

func parseAdapter_talentArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("talent: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("talent: response has no article body")
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	for _, selector := range []string{".news-title", ".article-title", ".detail-title", "h1"} {
		if title := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); title != "" && title != strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")) {
			article.Title = title
			break
		}
	}
	if article.Type == "" {
		article.Type = talentDocumentType(doc)
	}
	talentAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

func talentAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
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
		parsed, err := parseURL(resolved)
		if href == "" || err != nil || talentArticlePathRE.MatchString(parsed.Path) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
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
