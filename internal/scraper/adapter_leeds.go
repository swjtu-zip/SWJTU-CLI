package scraper

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// leedsSubPages contains the article-bearing tabs confirmed in the current
// Leeds navigation and list pages.  Profile/contact/complaint pages are
// intentionally not feeds.
var leedsSubPages = []string{
	"index/xyxw",
	"xytz/dzbg",
	"xytz/jwjx",
	"xytz/xszc",
	"xxgs/dzbg",
	"xxgs/jwjx",
	"xxgs/xszc",
	"index/hdyg",
	"index/zsxx",
	"index/zyxz",
	"xszc/xshd",
	"xszc/xsfc",
	"xszc/gjjl",
	"ztjy/xjpxsdzgtsshzysxztjy/xxdt",
}

var leedsEventJSONRE = regexp.MustCompile(`(?s)newsListDataArray\[[^\]]+\]\s*=\s*JSON\.parse\('(\[\{.*?\}\])'\)`)

// Leeds has its own article template.  In particular, the publish date is
// rendered in .m-textq1 .info .date instead of one of the metadata blocks
// understood by the generic 博达 article parser.  Keep both hooks attached to
// the site so future Leeds list changes do not have to be folded into the
// shared parser.
func init() {
	registerAdapter("leeds", SiteAdapter{
		SubPages:     leedsSubPages,
		ParseList:    parseLeedsNews,
		ParseArticle: parseLeedsArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   leedsTotalPages,
	})
}

func parseLeedsNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse leeds list html: %w", err)
	}
	if leedsIsHomepage(doc) {
		items := leedsHomepageItems(doc, html, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("leeds: homepage contains no local article modules")
		}
		return items, nil
	}

	// Current Leeds archives use two deliberately different list templates.
	// Parse them by container so the dashboard/sidebars and media links cannot
	// be mistaken for article rows.  The old list_zx fixture remains supported
	// below for older generated pages.
	if root := doc.Find(".ny_news > ul").First(); root.Length() > 0 {
		items := leedsNyNewsItems(root, leedsPageType(doc), baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("leeds: ny_news contains no local article rows")
		}
		return items, nil
	}
	if root := doc.Find(".nywp > ul.ul-list-d3").First(); root.Length() > 0 {
		items := leedsNywpItems(root, leedsPageType(doc), baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("leeds: nywp contains no local article rows")
		}
		return items, nil
	}

	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}

	// The Leeds list template exposes the section name in the breadcrumb, but
	// the shared Boda list parser deliberately leaves Type empty.  It also
	// preserves source date text verbatim.  Enrich both fields here, including
	// homepage rows whose section is determined from their own module.
	pageType := leedsPageType(doc)
	for i := range items {
		if date, publishedAt := leedsItemDate(doc, items[i].URL, items[i].Date); date != "" {
			items[i].Date = date
			items[i].PublishedAt = publishedAt
		}
		if items[i].Type == "" {
			items[i].Type = leedsItemType(doc, items[i].URL)
		}
		if items[i].Type == "" {
			items[i].Type = pageType
		}
	}
	return items, nil
}

func leedsIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	return pageType == "1" && doc.Find(".news_bg, .noitce_bg, .Events").Length() > 0
}

func leedsHomepageItems(doc *goquery.Document, raw []byte, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	add := func(row *goquery.Selection, typ string, dateOverride string) {
		if row == nil || row.Length() == 0 {
			return
		}
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			for _, selector := range []string{"h5", "h3", ".EventsBoxLTopText"} {
				if value := strings.Join(strings.Fields(row.Find(selector).First().Text()), " "); value != "" {
					title = value
					break
				}
			}
		}
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Text()), " ")
		}
		date := dateOverride
		if date == "" {
			date = strings.TrimSpace(row.Find("i, time, .date, .time, span").Last().Text())
		}
		date, publishedAt := leedsNormalizeDate(date)
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if !isValidSiteNewsURL(fullURL, baseURL) || !isValidNewsTitle(title) {
			return
		}
		if index, exists := seen[fullURL]; exists {
			if items[index].Date == "" && date != "" {
				items[index].Date, items[index].PublishedAt = date, publishedAt
			}
			if items[index].Type == "" {
				items[index].Type = typ
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	}

	doc.Find(".news_bg .news_box1 li").Each(func(_ int, row *goquery.Selection) { add(row, "学院新闻", "") })
	doc.Find("#NoitceCenterTobox .swiper-slide").Each(func(index int, slide *goquery.Selection) {
		typ := "信息公示"
		if index == 0 {
			typ = "学院通知"
		}
		slide.Find("dl").Each(func(_ int, row *goquery.Selection) { add(row, typ, "") })
	})
	// Event dates are absent from the visible HTML but are present in the
	// source's JSON model.  Use only that model, never the event body text.
	eventDates := leedsHomepageEventDates(raw, baseURL)
	doc.Find(".EventsBoxLTop").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		date := ""
		if anchor.Length() > 0 {
			date = eventDates[cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))]
		}
		add(row, "活动预告", date)
	})
	return items
}

func leedsHomepageEventDates(raw []byte, baseURL string) map[string]string {
	result := make(map[string]string)
	for _, match := range leedsEventJSONRE.FindAllSubmatch(raw, -1) {
		var records []struct {
			URL       string `json:"url"`
			ShowDate  string `json:"showDate"`
			YearMonth string `json:"showDate1"`
			Day       string `json:"showDate2"`
		}
		if err := json.Unmarshal(match[1], &records); err != nil {
			continue
		}
		for _, record := range records {
			date := record.ShowDate
			if date == "" && record.YearMonth != "" && record.Day != "" {
				date = record.YearMonth + "." + record.Day
			}
			if normalized, _ := leedsNormalizeDate(date); normalized != "" {
				result[cleanURL(resolveURL(baseURL, record.URL))] = normalized
			}
		}
	}
	return result
}

func leedsNyNewsItems(root *goquery.Selection, typ, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	root.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		title := strings.Join(strings.Fields(row.Find(".txt h5").First().Text()), " ")
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Text()), " ")
		}
		date, publishedAt := leedsNormalizeDate(row.Find(".txt > span").First().Text())
		leedsAppendListItem(&items, seen, anchor, title, date, publishedAt, typ, baseURL)
	})
	return items
}

func leedsNywpItems(root *goquery.Selection, typ, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	root.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a.con[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		title := strings.Join(strings.Fields(row.Find(".g-tit-d").First().Text()), " ")
		date, publishedAt := leedsDateBox(row.Find(".g-info-d").First())
		leedsAppendListItem(&items, seen, anchor, title, date, publishedAt, typ, baseURL)
	})
	return items
}

func leedsDateBox(box *goquery.Selection) (string, string) {
	if box == nil || box.Length() == 0 {
		return "", ""
	}
	day := strings.TrimSpace(box.Find("em").First().Text())
	month := strings.TrimSpace(box.Clone().Find("em").Remove().End().Text())
	if day != "" && month != "" {
		if date, publishedAt := leedsNormalizeDate(month + "." + day); date != "" {
			return date, publishedAt
		}
	}
	return leedsNormalizeDate(box.Text())
}

func leedsAppendListItem(items *[]model.NewsItem, seen map[string]bool, anchor *goquery.Selection, title, date, publishedAt, typ, baseURL string) {
	fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
	if seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) || !isValidNewsTitle(title) {
		return
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: strings.TrimSpace(typ), ParsedAt: time.Now()})
}

func leedsNormalizeDate(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	return "", ""
}

func leedsTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if !leedsKnownFeed(slug) {
		return 0, fmt.Errorf("leeds: %q is not an article-list route", slug)
	}
	baseURL := "http://leeds.swjtu.edu.cn"
	if site := AllSites["leeds"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(fmt.Sprintf("%s/%s.htm", strings.TrimRight(baseURL, "/"), slug))
	if err != nil {
		return 0, fmt.Errorf("leeds: fetch first page for %s: %w", slug, err)
	}
	return leedsTotalPagesFromHTML(body, slug)
}

func leedsKnownFeed(slug string) bool {
	for _, known := range leedsSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func leedsTotalPagesFromHTML(raw []byte, slug string) (int, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("leeds: parse pagination for %s: %w", slug, err)
	}
	if leedsIsHomepage(doc) || doc.Find(".ny_news > ul, .nywp > ul.ul-list-d3").Length() == 0 {
		return 0, fmt.Errorf("leeds: %s response is not an article list", slug)
	}
	if total := parseTotalPages(raw); total > 0 {
		return total, nil
	}
	return 1, nil
}

func parseLeedsArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse leeds article html: %w", err)
	}

	// Scope the lookup to the article header.  The same page contains dates in
	// the “最新阅读” sidebar, which must never replace the publish date.
	if date, publishedAt := leedsArticleDate(doc); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	return article, nil
}

func leedsArticleDate(doc *goquery.Document) (string, string) {
	for _, selector := range []string{
		".m-textq1 > .info .date",
		".m-textq1 .info .date",
	} {
		date := strings.TrimSpace(doc.Find(selector).First().Text())
		if date == "" {
			continue
		}
		if publishedAt := normalizePublishedAt(date); publishedAt != "" {
			return publishedAt[:10], publishedAt
		}
		if match := bareDateRe.FindStringSubmatch(date); match != nil {
			if normalized := normalizeDateParts(match[1], match[2], match[3]); normalized != "" {
				return normalized, normalized
			}
		}
	}
	return "", ""
}

func leedsPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if typ := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).Last().AttrOr("content", "")), " "); typ != "" && typ != "首页" {
		return typ
	}
	if typ := bodaArticleSectionType(doc, ""); typ != "" {
		return typ
	}
	for _, selector := range []string{".list-title", ".list-head", ".m-textq1 > h1", ".m-textq1 > h2", ".m-textq1 > h3"} {
		candidate := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if candidate != "" && candidate != "首页" {
			return candidate
		}
	}
	return ""
}

func leedsItemType(doc *goquery.Document, itemURL string) string {
	anchor := leedsFindItemAnchor(doc, itemURL)
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
	if row.Length() == 0 {
		row = anchor.Parent()
	}
	for _, selector := range []string{".type", ".category", ".column", ".lanmu", ".source", ".tag"} {
		if value := leedsCleanType(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func leedsFindItemAnchor(doc *goquery.Document, itemURL string) *goquery.Selection {
	var found *goquery.Selection
	doc.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		for _, base := range []string{"http://leeds.swjtu.edu.cn", "https://leeds.swjtu.edu.cn"} {
			resolved := cleanURL(resolveURL(base, anchor.AttrOr("href", "")))
			if resolved == itemURL {
				found = anchor
				return false
			}
		}
		return true
	})
	return found
}

func leedsItemDate(doc *goquery.Document, itemURL, fallback string) (string, string) {
	raw := ""
	if anchor := leedsFindItemAnchor(doc, itemURL); anchor != nil && anchor.Length() > 0 {
		row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
		if row.Length() == 0 {
			row = anchor.Parent()
		}
		for _, selector := range []string{".date", ".time", "time", ".xw-ul-date", "em"} {
			raw = strings.TrimSpace(row.Find(selector).First().Text())
			if raw != "" {
				break
			}
		}
	}
	if raw == "" {
		raw = fallback
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	return strings.TrimSpace(raw), ""
}

func leedsCleanType(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "[]【】〖〗")
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "栏目："), "栏目:"))
}
