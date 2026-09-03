package scraper

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"swjtu-cli/internal/model"
)

var ottSubPages = []string{
	"xwdt1/gzdt", "xwdt1/rdxw", "xwdt1/xydt", "y_j_y/yjydt", "tzgong/tzgg",
	"kjhz/hzdt", "kjhz/xqfb", "jszyzx/zxdt", "g_j_d_x_k_j_y/kjydt",
	"zcfg/dfzc", "zcfg/gjzc", "zcfg/xxzc", "cgzh/gsgg", "bslc__/xmsb",
	"jszyzx/jsjlr",
}

var ottMonthRE = regexp.MustCompile(`(20\d{2})\s*[./-]\s*(\d{1,2})`)

// ottAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("ott", SiteAdapter{
		SubPages:     ottSubPages,
		ParseList:    parseAdapter_ottList,
		ParseArticle: parseAdapter_ottArticle,
		PageURL:      ottPageURL,
	})
}

func ottPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 || (page > 1 && totalPages < 1) || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	physical := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

func parseAdapter_ottList(html []byte, baseURL string) ([]model.NewsItem, error) {
	if items := parseOTTListRows(html, baseURL); items != nil {
		return items, nil
	}
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}
	filtered := items[:0]
	for _, item := range items {
		// The homepage mixes the institute's own articles with links copied
		// from the university news site. Only same-site entries belong to this
		// adapter; otherwise stale university links become false articles for
		// the technology-transfer feed.
		if !isExternalURL(item.URL, baseURL) {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("ott: homepage contains no same-site news entries")
	}
	return filtered, nil
}

func parseAdapter_ottArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if article.Type == "" {
		if doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html))); parseErr == nil {
			article.Type = ottSectionType(doc)
		}
	}
	return article, nil
}

// parseOTTListRows handles the list template used by the article-bearing OTT
// sections. Returning nil means the response was another template and lets
// the conservative shared parser try its own scoped patterns.
func parseOTTListRows(raw []byte, baseURL string) []model.NewsItem {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil
	}
	rows := doc.Find(".lb-list > li")
	if rows.Length() == 0 {
		return nil
	}
	typ := ottSectionType(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool)
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL := resolveURL(baseURL, anchor.AttrOr("href", ""))
		if fullURL == "" || isExternalURL(fullURL, baseURL) || !isValidNewsURL(fullURL) || seen[fullURL] {
			return
		}
		title := cleanTitle(row.Find(".lb-list-tt").First().Text())
		if title == "" {
			title = cleanTitle(anchor.Text())
		}
		if !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := ottListDate(row.Find(".lb-list-date"))
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	return items
}

func ottSectionType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-left-tt", ".list-banner-tt"} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func ottListDate(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	day := strings.TrimSpace(sel.Clone().Children().Remove().End().Text())
	if day == "" {
		day = strings.TrimSpace(sel.Text())
	}
	monthText := strings.Join(strings.Fields(sel.Find("span").First().Text()), " ")
	match := ottMonthRE.FindStringSubmatch(monthText)
	if match == nil {
		return "", ""
	}
	dayMatch := regexp.MustCompile(`\d{1,2}`).FindString(day)
	if dayMatch == "" {
		return "", ""
	}
	date := normalizeDateParts(match[1], match[2], dayMatch)
	return date, normalizePublishedAt(date)
}
