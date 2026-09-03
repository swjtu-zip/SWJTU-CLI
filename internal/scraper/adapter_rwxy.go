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

var rwxySubPages = []string{
	"xyxw", "dzbg", "xshd", "xsgz/tzgg", "xsgz/xgdt", "xsgz/xzfc", "xsgz/jyxx", "jwgz/jwdt",
	"szll1/cbx", "szll1/ylx", "szll1/zwx", "djgz/djdt", "djgz/xygh", "rcpy/yjsjy", "rcpy/bshldz",
}

// rwxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("rwxy", SiteAdapter{
		SubPages:     rwxySubPages,
		ParseList:    parseAdapter_rwxyList,
		ParseArticle: parseAdapter_rwxyArticle,
		PageURL:      rwxyPageURL,
		TotalPages:   rwxyTotalPages,
	})
}

func parseAdapter_rwxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("rwxy parse list: %w", err)
	}
	if rows := doc.Find(".list-news > ul > li"); rows.Length() > 0 {
		items := make([]model.NewsItem, 0, rows.Length())
		seen := make(map[string]bool, rows.Length())
		typ := rwxyListType(doc)
		rows.Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			href := strings.TrimSpace(anchor.AttrOr("href", ""))
			fullURL := cleanURL(resolveURL(baseURL, href))
			if href == "" || !isValidNewsURL(fullURL) || seen[fullURL] {
				return
			}
			// The outer anchor contains an invalid nested More anchor. The
			// headline is the visible text in .time; its date is in the
			// HTML comment immediately before that text.
			timeBlock := row.Find(".time").First()
			title := cleanTitle(strings.TrimSpace(timeBlock.Text()))
			if title == "" || title == "More →" || !isValidNewsTitle(title) {
				return
			}
			dateText, _ := timeBlock.Html()
			date, publishedAt := rwxyDateValues(dateText)
			seen[fullURL] = true
			items = append(items, model.NewsItem{
				Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt,
				Type: typ, ParsedAt: time.Now(),
			})
		})
		return items, nil
	}
	if items := parseRwxyHomeList(doc, baseURL); len(items) > 0 {
		return items, nil
	}
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_rwxyArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil || article == nil {
		return article, err
	}
	doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if parseErr != nil {
		return article, nil
	}
	if meta := doc.Find("form[name='_newscontent_fromname'] .title .info, form[name='_newscontent_fromname'] .info").First(); meta.Length() > 0 {
		if date, publishedAt := rwxyDateValues(meta.Text()); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		} else if rwxyHasLegacyPlaceholder(meta.Text()) {
			article.Date = ""
			article.PublishedAt = ""
		}
	}
	if rwxyHasLegacyPlaceholder(article.Date) || rwxyHasLegacyPlaceholder(article.PublishedAt) {
		article.Date = ""
		article.PublishedAt = ""
	}
	return article, nil
}

var rwxyDateRe = regexp.MustCompile(`20\d{2}\s*(?:[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}日?)(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?`)
var rwxyPageIndicatorRe = regexp.MustCompile(`\b1\s*/\s*(\d+)\b`)

const rwxyLegacyPlaceholderDate = "2013-01-01"

func rwxyDateValues(text string) (string, string) {
	match := rwxyDateRe.FindString(strings.Join(strings.Fields(text), " "))
	if match == "" {
		return "", ""
	}
	precise := normalizePublishedAt(match)
	if precise == "" {
		return "", ""
	}
	if rwxyHasLegacyPlaceholder(precise) {
		return "", ""
	}
	return precise[:10], precise
}

func rwxyHasLegacyPlaceholder(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), rwxyLegacyPlaceholderDate)
}

func rwxyListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".loaction a", ".sidebar .title span", ".sidebar .title"} {
		values := doc.Find(selector)
		for i := values.Length() - 1; i >= 0; i-- {
			value := strings.Join(strings.Fields(values.Eq(i).Text()), " ")
			if value != "" && value != "首页" {
				return value
			}
		}
	}
	return ""
}

func parseRwxyHomeList(doc *goquery.Document, baseURL string) []model.NewsItem {
	if doc == nil {
		return nil
	}
	rows := doc.Find(".news .conts .item, .activity .conts .swiper-slide, .dynamic .intrs .list li")
	if rows.Length() == 0 {
		return nil
	}
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL := cleanURL(resolveURL(baseURL, href))
		if href == "" || !isValidSiteNewsURL(fullURL, baseURL) || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(row.Find(".tits a[href*='info/']").First().Text())
		if title == "" {
			title = strings.TrimSpace(anchor.AttrOr("title", ""))
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if !isValidNewsTitle(title) {
			return
		}
		seen[fullURL] = true
		date, publishedAt := rwxyHomeDate(row)
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        rwxyHomeType(row),
			ParsedAt:    time.Now(),
		})
	})
	return items
}

func rwxyHomeDate(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	if labs := row.Find(".labs").First(); labs.Length() > 0 {
		day := strings.TrimSpace(labs.Find("em").First().Text())
		year := strings.TrimSpace(labs.Find("b").First().Text())
		if day != "" && year != "" {
			if date, publishedAt := rwxyDateValues(year + "-" + day); date != "" {
				return date, publishedAt
			}
		}
	}
	for _, selector := range []string{".inform .time", ".time", ".date"} {
		if value := strings.TrimSpace(row.Find(selector).First().Text()); value != "" {
			if date, publishedAt := rwxyDateValues(value); date != "" {
				return date, publishedAt
			}
		}
	}
	return "", ""
}

func rwxyHomeType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	if parent := row.Closest(".intrs"); parent.Length() > 0 {
		if value := strings.TrimSpace(parent.Find(".title b").First().Text()); value != "" {
			return value
		}
	}
	if parent := row.Closest(".conts"); parent.Length() > 0 {
		if value := strings.TrimSpace(parent.Find(".title b").First().Text()); value != "" {
			return value
		}
	}
	if parent := row.Closest(".activity"); parent.Length() > 0 {
		if value := strings.TrimSpace(parent.Find(".title b").First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func rwxyPageURL(baseURL, slug string, page, totalPages int, extension string) string {
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

func rwxyTotalPages(slug string) (int, error) {
	site := AllSites["rwxy"]
	if site == nil {
		return 0, fmt.Errorf("rwxy: site registration is missing")
	}
	body, err := fetch(rwxyPageURL(site.BaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		body, err = fetch(rwxyPageURL(site.BaseURL, slug, 1, 0, ".html"))
	}
	if err != nil {
		return 0, err
	}
	if total := rwxyTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	if rwxyHasArticleRows(body) {
		return 1, nil
	}
	return 0, fmt.Errorf("rwxy: list page %q has no total-page marker or article rows", slug)
}

func rwxyTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err == nil {
		maxPage := 0
		doc.Find(".p_pages a, .p_pages span, .pagination a, .pagination span").Each(func(_ int, sel *goquery.Selection) {
			value, parseErr := strconv.Atoi(strings.TrimSpace(sel.Text()))
			if parseErr == nil && value > maxPage {
				maxPage = value
			}
		})
		if maxPage > 0 {
			return maxPage
		}
	}
	if match := rwxyPageIndicatorRe.FindStringSubmatch(string(raw)); match != nil {
		value, _ := strconv.Atoi(match[1])
		return value
	}
	return 0
}

func rwxyHasArticleRows(raw []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return false
	}
	return doc.Find(".list-news > ul > li a[href*='info/'], .news .conts .item a[href*='info/'], .activity .conts .swiper-slide a[href*='info/'], .dynamic .intrs .list li a[href*='info/']").Length() > 0
}
