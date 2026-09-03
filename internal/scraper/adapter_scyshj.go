package scraper

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var scyshjSubPages = []string{
	"tzxw/tzgg", "tzxw/xwdt", "xsjl", "dxal", "yjygk/yjtd",
	"scsjczxwyh/hdly", "scsjczxwyh/wygd", "scsjczxwyh/zzgc",
}

// scyshjAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("scyshj", SiteAdapter{
		SubPages:     scyshjSubPages,
		ParseList:    parseAdapter_scyshjList,
		ParseArticle: parseAdapter_scyshjArticle,
		PageURL:      scyshjPageURL,
		TotalPages:   scyshjTotalPages,
	})
}

func parseAdapter_scyshjList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("scyshj parse list: %w", err)
	}

	rows := doc.Find(".main_conRCb li")
	if rows.Length() == 0 {
		rows = doc.Find(".dynamic_list li, .new_list h3")
	}
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	typ := scyshjDocumentType(doc)
	rows.Each(func(_ int, row *goquery.Selection) {
		item, ok := scyshjNewsItem(row, baseURL, typ)
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("scyshj: no article rows found")
	}
	return items, nil
}

func parseAdapter_scyshjArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}

var scyshjDateRe = regexp.MustCompile(`20\d{2}\s*(?:[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}日?)(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?`)
var scyshjPageIndicatorRe = regexp.MustCompile(`\b1\s*/\s*(\d+)\b`)

func scyshjNewsItem(row *goquery.Selection, baseURL, typ string) (model.NewsItem, bool) {
	if row == nil || row.Length() == 0 {
		return model.NewsItem{}, false
	}
	anchor := row.Find("a[href*='info/']").First()
	if anchor.Length() == 0 {
		return model.NewsItem{}, false
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	if href == "" || !isValidSiteNewsURL(fullURL, baseURL) {
		return model.NewsItem{}, false
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(row.Find("em").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if !isValidNewsTitle(title) {
		return model.NewsItem{}, false
	}
	date, publishedAt := scyshjDateValues(row.Text())
	return model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        strings.TrimSpace(typ),
		ParsedAt:    time.Now(),
	}, true
}

func scyshjDateValues(text string) (string, string) {
	match := scyshjDateRe.FindString(strings.Join(strings.Fields(text), " "))
	if match == "" {
		return "", ""
	}
	precise := normalizePublishedAt(match)
	if precise == "" {
		return "", ""
	}
	if len(precise) > len("2006-01-02") {
		return precise[:10], precise
	}
	return precise, precise
}

func scyshjDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".main_conLT a.cur", ".main_titT a", ".dynamic h2", `meta[name="pageTitle"]`} {
		values := doc.Find(selector)
		for i := values.Length() - 1; i >= 0; i-- {
			value := strings.TrimSpace(values.Eq(i).AttrOr("content", ""))
			if value == "" {
				value = strings.TrimSpace(values.Eq(i).Text())
			}
			value = strings.Trim(value, "[]【】 ")
			if value != "" && value != "首页" {
				return value
			}
		}
	}
	return ""
}

func scyshjPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, totalPages-page+1, extension)
}

func scyshjTotalPages(slug string) (int, error) {
	site := AllSites["scyshj"]
	if site == nil {
		return 0, fmt.Errorf("scyshj: site registration is missing")
	}
	body, err := fetch(scyshjPageURL(site.BaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		body, err = fetch(scyshjPageURL(site.BaseURL, slug, 1, 0, ".html"))
	}
	if err != nil {
		return 0, err
	}
	if total := scyshjTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	if scyshjHasArticleRows(body) {
		return 1, nil
	}
	return 0, fmt.Errorf("scyshj: list page %q has no total-page marker or article rows", slug)
}

func scyshjTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err == nil {
		for _, selector := range []string{"[id^='fanye']", ".p_pages", ".pb_sys_common"} {
			if pagination := doc.Find(selector).First(); pagination.Length() > 0 {
				if match := scyshjPageIndicatorRe.FindStringSubmatch(strings.Join(strings.Fields(pagination.Text()), " ")); match != nil {
					value, _ := strconv.Atoi(match[1])
					if value > 0 {
						return value
					}
				}
			}
		}
	}
	if match := scyshjPageIndicatorRe.FindStringSubmatch(string(raw)); match != nil {
		value, _ := strconv.Atoi(match[1])
		return value
	}
	return 0
}

func scyshjHasArticleRows(raw []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return false
	}
	return doc.Find(".main_conRCb li a[href*='info/'], .dynamic_list li a[href*='info/'], .new_list h3 a[href*='info/']").Length() > 0
}
