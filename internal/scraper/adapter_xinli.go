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

// These are the article-list surfaces exposed by the psychology centre's
// homepage tabs.  The homepage itself only shows a small preview; every
// linked list must be crawled as a feed so that older notices are not lost.
var xinliSubPages = []string{
	"zxyw",
	"tzgg/qb",
	"tzgg/xljkjy",
	"tzgg/dzbg",
	"tzgg/bks",
	"tzgg/yjs",
	"tzgg/kygz",
	"tzgg/xsgz",
	"tzgg/sygz",
	"tzgg/gsgg",
}

var (
	xinliArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	xinliColumnPathRE  = regexp.MustCompile(`(?i)^/info/([0-9]+)/[0-9]+\.html?$`)
	xinliShortDateRE   = regexp.MustCompile(`^(0?[1-9]|1[0-2])[-/.](0?[1-9]|[12][0-9]|3[01])$`)
)

var xinliColumnTypes = map[string]string{
	"1022": "中心要闻",
	"1502": "心理健康教育",
	"1512": "党政办公",
	"1522": "本科生",
	"1532": "研究生",
	"1542": "科研工作",
	"1552": "学生工作",
	"1562": "实验工作",
	"1572": "公示公告",
}

// xinliAdapter gives this website its own extension point.  Although it uses
// the same CMS as several other units, its homepage and list templates have
// different DOM shapes and its generated pages are reverse numbered.
func init() {
	registerAdapter("xinli", SiteAdapter{
		SubPages:     xinliSubPages,
		ParseList:    parseAdapter_xinliList,
		ParseArticle: parseAdapter_xinliArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_xinliList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("xinli: parse list: %w", err)
	}

	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType == "2" || doc.Find(".ej_list").Length() > 0 {
		list := doc.Find(".ej_list").First()
		if list.Length() == 0 {
			return nil, fmt.Errorf("xinli: list page is missing the audited .ej_list shell")
		}
		rows := list.Find("li")
		if rows.Length() == 0 {
			return []model.NewsItem{}, nil
		}
		return xinliCollectRows(rows, baseURL, xinliListType(doc)), nil
	}

	if pageType == "1" || xinliHomepage(doc) {
		items := make([]model.NewsItem, 0)
		seen := make(map[string]int)
		xinliCollectRowsInto(&items, seen, doc.Find(".p2_left ul.list_p1 > li"), baseURL, "中心要闻")
		doc.Find(".p3_box").Each(func(_ int, section *goquery.Selection) {
			typ := strings.Join(strings.Fields(section.Find(".xtit h2").First().Text()), " ")
			if typ == "" {
				return
			}
			xinliCollectRowsInto(&items, seen, section.Find(".p3_ul > li"), baseURL, typ)
		})
		if len(items) == 0 {
			return nil, fmt.Errorf("xinli: homepage contains no local article entries")
		}
		return items, nil
	}

	return nil, fmt.Errorf("xinli: response is not a homepage or article list (pageType=%q)", pageType)
}

func parseAdapter_xinliArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	// The xinli detail template renders the section heading in an earlier
	// <h1> (for example, "中心要闻"). The article headline is the .box1 .bt
	// paragraph in the detail area and must take precedence over that heading.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}
	if title := strings.TrimSpace(doc.Find(".box1 .bt").First().Text()); title != "" {
		article.Title = strings.Join(strings.Fields(title), " ")
	}
	if typ := xinliArticleType(doc, article.Title); typ != "" {
		article.Type = typ
	}
	if meta := strings.TrimSpace(doc.Find(".box1 .xx").First().Text()); meta != "" {
		if date, publishedAt := xinliDateValues(meta); date != "" {
			article.Date = date
			if publishedAt != "" {
				article.PublishedAt = publishedAt
			}
		}
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func xinliHomepage(doc *goquery.Document) bool {
	return doc.Find(".p2_left ul.list_p1, .p3_box .p3_ul").Length() > 0
}

func xinliListType(doc *goquery.Document) string {
	for _, selector := range []string{".ej_main_left .lmmc_yj", "#nav_slide h1", ".lmmc_yj"} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
}

func xinliCollectRows(rows *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0, rows.Length())
	xinliCollectRowsInto(&items, make(map[string]int), rows, baseURL, typ)
	return items
}

func xinliCollectRowsInto(items *[]model.NewsItem, seen map[string]int, rows *goquery.Selection, baseURL, typ string) {
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		fullURL, ok := xinliArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		index, exists := seen[fullURL]
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(row.Find(".bt").First().Text())
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}

		date, publishedAt := xinliDateValues(row.Find(".sj, .date, .time, time, em").First().Text())
		rowType := xinliTypeForURL(fullURL)
		if rowType == "" {
			rowType = strings.TrimSpace(typ)
		}
		item := model.NewsItem{
			Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt,
			Type: rowType, ParsedAt: time.Now(),
		}
		if !exists {
			seen[fullURL] = len(*items)
			*items = append(*items, item)
			return
		}
		// A homepage preview can repeat an article in the centre-news block and
		// an announcement tab. Keep the more specific column type and any date
		// precision discovered in the second occurrence.
		current := &(*items)[index]
		if current.Date == "" && item.Date != "" {
			current.Date = item.Date
		}
		if current.PublishedAt == "" && item.PublishedAt != "" {
			current.PublishedAt = item.PublishedAt
		}
		if (current.Type == "" || current.Type == "全部" || current.Type == "中心要闻") && item.Type != "" && item.Type != "全部" {
			current.Type = item.Type
		}
	})
}

func xinliArticleURL(baseURL, href string) (string, bool) {
	resolved := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	u, err := url.Parse(resolved)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return "", false
	}
	if !xinliArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), true
}

func xinliTypeForURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	match := xinliColumnPathRE.FindStringSubmatch(u.Path)
	if match == nil {
		return ""
	}
	return xinliColumnTypes[match[1]]
}

func xinliDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	if match := xinliShortDateRE.FindStringSubmatch(raw); match != nil {
		return fmt.Sprintf("%02s-%02s", match[1], match[2]), ""
	}
	return "", ""
}

func xinliArticleType(doc *goquery.Document, articleTitle string) string {
	for _, selector := range []string{".lmmc_yj", "#nav_slide h1", ".ej_main_left > h2"} {
		if value := bodaTypeCandidate(doc.Find(selector).First().Text(), articleTitle); value != "" {
			return value
		}
	}
	for _, selector := range []string{".dqwz a", ".loaction a", ".main_titT a"} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			if value := bodaTypeCandidate(selection.Eq(i).Text(), articleTitle); value != "" {
				return value
			}
		}
	}
	return ""
}
