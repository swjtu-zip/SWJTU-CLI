package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// ctt.swjtu.edu.cn has a site-specific second-page layout. Its category
// pages use .news-list cards, while the home page uses a .news-ul list. The
// generic parser does not know either container and can therefore return an
// empty list or pick unrelated links from the page shell.
func init() {
	registerAdapter("ctt", SiteAdapter{
		ParseList:    parseCttNews,
		ParseArticle: parseCttArticle,
		PageURL:      cttPageURL,
	})
}

var (
	cttInfoArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	cttRootArticlePathRe = regexp.MustCompile(`(?i)^/[0-9]{4,}\.html?$`)
	cttFullDateRe        = regexp.MustCompile(`(?i)(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?`)
	cttYearMonthRe       = regexp.MustCompile(`(?i)^(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*月?$`)
	cttMonthDayRe        = regexp.MustCompile(`^([0-9]{1,2})\s*[-/.]\s*([0-9]{1,2})$`)
)

// cttPageURL maps the logical page number (1=newest) to CTT's generated
// files. For example, the first xytz page advertises 20 pages and links page
// 2 to xytz/19.htm.
func cttPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}

	filePage := page
	if totalPages > 1 {
		filePage = totalPages - page + 1
	}
	if filePage < 1 {
		filePage = 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func parseCttNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse CTT list: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)

	// Category pages have one .news-list card per article. Restricting the
	// extraction to this container excludes the banner cards and left-side
	// navigation, both of which also contain article-looking links.
	listCards := doc.Find(".news-list")
	if listCards.Length() > 0 {
		listCards.Each(func(_ int, card *goquery.Selection) {
			anchor := card.Find(".news-box > a[href]").First()
			if anchor.Length() == 0 {
				anchor = card.Find("a[href]").First()
			}
			addCttListItem(&items, seen, card, anchor, baseURL, false)
		})
	}
	// The home page's "最近更新" block is intentionally handled separately.
	// Some generated home pages contain an empty .news-list shell for another
	// module, so fall back when the category pass did not yield an article.
	if len(items) == 0 {
		doc.Find(".news-ul > li").Each(func(_ int, row *goquery.Selection) {
			addCttListItem(&items, seen, row, row.Find(".newstxtli a[href]").First(), baseURL, true)
		})
	}
	if len(items) == 0 {
		// The current home page uses the newer .innews and .rowb modules
		// instead of .news-ul. Both contain real /info/<channel>/<id>.htm
		// links; keep the extraction scoped to those modules so image links,
		// navigation, and platform descriptions do not leak into the feed.
		doc.Find(".innews > li").Each(func(_ int, row *goquery.Selection) {
			addCttListItem(&items, seen, row, row.Find(".info a[href], a.line2[href]").First(), baseURL, false)
		})
		doc.Find(".rowb .list li").Each(func(_ int, row *goquery.Selection) {
			addCttListItem(&items, seen, row, row.Find("a.texto[href]").First(), baseURL, false)
		})
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("CTT list contains no article entries")
	}
	return items, nil
}

func addCttListItem(items *[]model.NewsItem, seen map[string]bool, block, anchor *goquery.Selection, baseURL string, allowRootArticle bool) {
	if anchor == nil || anchor.Length() == 0 {
		return
	}
	href, ok := anchor.Attr("href")
	if !ok {
		return
	}
	fullURL, ok := cttArticleURL(baseURL, href, allowRootArticle)
	if !ok || seen[fullURL] {
		return
	}

	title := strings.TrimSpace(block.Find(".news-title").First().Text())
	if title == "" {
		title = strings.TrimSpace(anchor.AttrOr("title", ""))
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 100 {
		return
	}

	seen[fullURL] = true
	*items = append(*items, model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     cttDateFromBlock(block),
		ParsedAt: time.Now(),
	})
}

func cttArticleURL(baseURL, href string, allowRootArticle bool) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || isExternalURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
		return "", false
	}
	if cttInfoArticlePathRe.MatchString(parsed.Path) {
		return fullURL, true
	}
	if allowRootArticle && cttRootArticlePathRe.MatchString(parsed.Path) {
		return fullURL, true
	}
	return "", false
}

func cttDateFromBlock(block *goquery.Selection) string {
	if block == nil || block.Length() == 0 {
		return ""
	}

	if date := cttDateFromScope(block.Find(".news-time").First()); date != "" {
		return date
	}
	if date := cttDateFromScope(block.Find(".timecon").First()); date != "" {
		return date
	}
	for _, selector := range []string{"time", ".date", ".time", "[class*='date']", "[class*='time']"} {
		if date := cttDateFromScope(block.Find(selector).First()); date != "" {
			return date
		}
	}
	if date := cttDateFromScope(block); date != "" {
		return date
	}
	return ""
}

func cttDateFromScope(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}

	day := strings.TrimSpace(scope.Find(".day").First().Text())
	month := strings.TrimSpace(scope.Find(".month").First().Text())
	if day != "" && month != "" {
		if match := cttYearMonthRe.FindStringSubmatch(month); match != nil {
			return normalizeDateParts(match[1], match[2], day)
		}
		if match := cttFullDateRe.FindStringSubmatch(month); match != nil {
			return normalizeDateParts(match[1], match[2], match[3])
		}
	}

	text := strings.Join(strings.Fields(scope.Text()), " ")
	if match := cttFullDateRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	if match := cttYearMonthRe.FindStringSubmatch(text); match != nil && day != "" {
		return normalizeDateParts(match[1], match[2], day)
	}
	if match := cttMonthDayRe.FindStringSubmatch(text); match != nil {
		month, monthErr := strconv.Atoi(match[1])
		day, dayErr := strconv.Atoi(match[2])
		if monthErr == nil && dayErr == nil && month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			return fmt.Sprintf("%02d-%02d", month, day)
		}
	}
	return ""
}

func parseCttArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if article.Date != "" {
		if match := cttFullDateRe.FindStringSubmatch(article.Date); match != nil {
			article.Date = normalizeDateParts(match[1], match[2], match[3])
		}
		return article, nil
	}

	doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if docErr != nil {
		return article, nil
	}
	for _, selector := range []string{".top-info", ".news_source", ".left-top"} {
		scope := doc.Find(selector).First()
		if scope.Length() == 0 {
			continue
		}
		if match := cttFullDateRe.FindStringSubmatch(strings.Join(strings.Fields(scope.Text()), " ")); match != nil {
			article.Date = normalizeDateParts(match[1], match[2], match[3])
			break
		}
	}
	return article, nil
}
