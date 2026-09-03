package scraper

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

var rvlSubPages = []string{
	"djgz/xwdt", "rsgz/tzgg", "kxyj/kygg", "kxyj/kylw", "kxyj/kyxm/gjjxm",
	"kxyj/kyzl", "kxyj/ryjl", "xsgz/tzgg", "tzgg", "sysaq", "cpyc",
	"djgz/dwgzgg", "yjsjy/yjspygg",
}

// rvlAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rvl", SiteAdapter{
		SubPages:     rvlSubPages,
		ParseList:    parseAdapter_rvlList,
		ParseArticle: parseAdapter_rvlArticle,
		PageURL:      rvlPageURL,
		TotalPages:   rvlTotalPages,
	})
}

var rvlDateRe = regexp.MustCompile(`20\d{2}\s*(?:[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}日?)(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?`)
var rvlPageIndicatorRe = regexp.MustCompile(`\b1\s*/\s*(\d+)\b`)

func parseAdapter_rvlList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("rvl parse list: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	listRows := doc.Find(".conlistComm dl")
	if listRows.Length() == 0 {
		// The home page has one list for each visible tab and separate news /
		// academic-activity blocks. Keep these selectors scoped to content;
		// the global navigation also contains /info/ links on this site.
		listRows = doc.Find(".news li, .content-tab ul.list li, .activity li")
	}
	listRows.Each(func(_ int, row *goquery.Selection) {
		item, ok := rvlNewsItem(row, baseURL, rvlRowType(row, rvlDocumentType(doc)))
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("rvl: no article rows found")
	}
	return items, nil
}

func parseAdapter_rvlArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBodaArticleWithSectionType(html, baseURL)
}

func rvlNewsItem(row *goquery.Selection, baseURL, typ string) (model.NewsItem, bool) {
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
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	// Do not impose the shared byte-length limit here. Chinese headlines can
	// exceed 100 UTF-8 bytes while still being a single valid title.
	if !isValidNewsTitle(title) {
		return model.NewsItem{}, false
	}

	date, publishedAt := rvlDateValues(row.Text())
	return model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        strings.TrimSpace(typ),
		ParsedAt:    time.Now(),
	}, true
}

func rvlDateValues(text string) (string, string) {
	match := rvlDateRe.FindString(strings.Join(strings.Fields(text), " "))
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

func rvlRowType(row *goquery.Selection, fallback string) string {
	if row != nil && row.Length() > 0 {
		for _, selector := range []string{"dt span", ".type", ".category"} {
			if value := strings.TrimSpace(row.Find(selector).First().Text()); value != "" {
				value = strings.Trim(value, "[]【】 ")
				if value != "" {
					return value
				}
			}
		}
		if id := row.Parent().AttrOr("id", ""); id != "" {
			if value := map[string]string{
				"zxtz": "行政公告", "djgz": "党建工作", "kygg": "科研公告",
				"yjs": "研究生教育", "xs": "学生工作", "rczp": "人事工作",
			}[id]; value != "" {
				return value
			}
		}
		for _, selector := range []string{".news h3", ".dtbox .title", ".activity .title b"} {
			if value := strings.TrimSpace(row.Closest(selector).Text()); value != "" {
				return strings.TrimSpace(strings.TrimSuffix(value, "News"))
			}
		}
	}
	return strings.TrimSpace(fallback)
}

func rvlDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".main_titT a", ".con_tit h3", ".main_right .current h3", `meta[name="pageTitle"]`} {
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

func rvlPageURL(baseURL, slug string, page, totalPages int, extension string) string {
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

func rvlTotalPages(slug string) (int, error) {
	site := AllSites["rvl"]
	if site == nil {
		return 0, fmt.Errorf("rvl: site registration is missing")
	}
	url := rvlPageURL(site.BaseURL, slug, 1, 0, ".htm")
	body, err := fetch(url)
	if err != nil {
		body, err = fetch(rvlPageURL(site.BaseURL, slug, 1, 0, ".html"))
	}
	if err != nil {
		return 0, err
	}
	if total := rvlTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	if rvlHasArticleRows(body) {
		return 1, nil
	}
	return 0, fmt.Errorf("rvl: list page %q has no total-page marker or article rows", slug)
}

func rvlTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err == nil {
		for _, selector := range []string{".pb_sys_common", "[id^='fanye']", ".p_pages"} {
			if pagination := doc.Find(selector).First(); pagination.Length() > 0 {
				if match := rvlPageIndicatorRe.FindStringSubmatch(strings.Join(strings.Fields(pagination.Text()), " ")); match != nil {
					var total int
					fmt.Sscanf(match[1], "%d", &total)
					if total > 0 {
						return total
					}
				}
			}
		}
	}
	if match := rvlPageIndicatorRe.FindStringSubmatch(string(raw)); match != nil {
		var total int
		fmt.Sscanf(match[1], "%d", &total)
		return total
	}
	return 0
}

func rvlHasArticleRows(raw []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return false
	}
	return doc.Find(".conlistComm dl a[href*='info/'], .news li a[href*='info/'], .content-tab ul.list li a[href*='info/'], .activity li a[href*='info/']").Length() > 0
}
