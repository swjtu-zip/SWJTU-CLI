package scraper

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var rscSubPages = []string{
	"xwtz/xwNews", "xwtz/tzNotice", "rszp/gljqtzjryzphgs", "rszp/shpyryzphgs",
	"rszp/jsjbshzphgs", "rszp/zpxx", "rszp/gsxx", "rszp/ggxx",
	"xgxz", "xgxz/zcps", "bszn", "bszn/rszc", "bszn/shbx", "bszn/szrc", "cjwt",
}

// rscAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rsc", SiteAdapter{
		SubPages:     rscSubPages,
		ParseList:    parseAdapter_rscList,
		ParseArticle: parseAdapter_rscArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_rscList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("rsc: parse list HTML: %w", err)
	}
	if rscIsHomepage(doc) {
		items := rscHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("rsc: homepage contains no local article entries")
		}
		return items, nil
	}

	items := rscInfoRows(doc, baseURL, rscPageType(doc))
	if len(items) > 0 {
		return items, nil
	}
	if rscListShell(doc) {
		// A real tab can be empty (公告信息 currently is one such page).  Keep
		// the distinction from an unrelated/static response explicit.
		return []model.NewsItem{}, nil
	}
	return nil, fmt.Errorf("rsc: response contains no recognised article list")
}

func parseAdapter_rscArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil {
		return nil, err
	}
	if article.Type == "" {
		if doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html))); parseErr == nil {
			article.Type = rscPageType(doc)
		}
	}
	return article, nil
}

var rscShortDateRE = regexp.MustCompile(`(?:^|\D)(\d{1,2})\s*[-/.]\s*(\d{1,2})(?:\D|$)`)

func rscIsHomepage(doc *goquery.Document) bool {
	return doc != nil && doc.Find(".news-left, .news-right").Length() > 0 && doc.Find(".information-bd").Length() > 0
}

func rscHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	appendRows := func(section *goquery.Selection, typ string, selector string) {
		section.Find(selector).Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			rscAppendItem(&items, seen, anchor, row, typ, baseURL)
		})
	}

	doc.Find(".news-left").Each(func(_ int, section *goquery.Selection) {
		typ := rscCleanType(section.Find(".g-list-hd h3").First().Text())
		appendRows(section, typ, ".bd .tit, .ft li")
	})
	doc.Find(".news-right").Each(func(_ int, section *goquery.Selection) {
		typ := rscCleanType(section.Find(".g-list-hd h3").First().Text())
		appendRows(section, typ, ".news-right-bd li")
	})
	for _, selector := range []string{".information-bd > dl", ".guide-bd > dl", ".system-bd > dl"} {
		doc.Find(selector).Each(func(_ int, section *goquery.Selection) {
			typ := rscCleanType(section.Find("dt h3").First().Text())
			appendRows(section, typ, "dd")
		})
	}
	return items
}

func rscInfoRows(doc *goquery.Document, baseURL, fallbackType string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) || !strings.Contains(strings.ToLower(anchor.AttrOr("href", "")), "info/") {
			return
		}
		row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
		if row.Length() == 0 {
			row = anchor.Parent()
		}
		typ := rscRowType(row)
		if typ == "" {
			typ = fallbackType
		}
		rscAppendItem(&items, seen, anchor, row, typ, baseURL)
	})
	return items
}

func rscAppendItem(items *[]model.NewsItem, seen map[string]bool, anchor, row *goquery.Selection, typ, baseURL string) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
	if fullURL == "" || seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) {
		return
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		clone := anchor.Clone()
		clone.Find("i, span, .date, .time, .state").Remove()
		title = strings.TrimSpace(clone.Text())
	}
	title = cleanTitle(title)
	if !isValidNewsTitle(title) {
		return
	}
	date, publishedAt := rscDateValues(row)
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        rscCleanType(typ),
		ParsedAt:    time.Now(),
	})
}

func rscPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if typ := rscCleanType(bodaArticleSectionType(doc, "")); typ != "" {
		return typ
	}
	for _, selector := range []string{".list-title", ".list-head", ".main_titT", ".loaction", ".location", "h1", "h2"} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			if typ := rscCleanType(selection.Eq(i).Text()); typ != "" && typ != "首页" && typ != "新闻通知" && typ != "人事招聘" {
				return typ
			}
		}
	}
	return ""
}

func rscRowType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	rowType := ""
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		href := strings.ToLower(anchor.AttrOr("href", ""))
		if strings.Contains(href, "info/") {
			return true
		}
		if value := rscCleanType(anchor.Text()); value != "" {
			rowType = value
			return false
		}
		return true
	})
	return rowType
}

func rscCleanType(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "[]【】〖〗")
	if strings.Contains(value, ">") {
		parts := strings.Split(value, ">")
		value = strings.TrimSpace(parts[len(parts)-1])
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "栏目："), "栏目:"))
}

func rscDateValues(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	raw := strings.TrimSpace(row.Find(".date, .time, time, em, .left").First().Text())
	if raw == "" {
		raw = strings.TrimSpace(row.Text())
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	if match := rscShortDateRE.FindStringSubmatch(raw); len(match) == 3 {
		if year := regexp.MustCompile(`20\d{2}`).FindString(raw); year != "" {
			date := normalizeDateParts(year, match[1], match[2])
			return date, date
		}
		return fmt.Sprintf("%02s-%02s", match[1], match[2]), ""
	}
	return raw, ""
}

func rscListShell(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".loaction, .location, .pagination, .p_pages, .pagenum").Length() > 0
}
