package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// sfl.swjtu.edu.cn has a legacy 博达 template whose home page is an
// aggregation of several sections. The home-page links do not carry dates;
// the canonical学院新闻 list (xyxw.htm) does. Keep the two shapes separate
// so image cards and navigation links cannot become news items.
func init() {
	registerAdapter("sfl", SiteAdapter{
		ParseList:    parseSflNews,
		ParseArticle: parseSflArticle,
		PageURL:      sflPageURL,
		TotalPages:   sflTotalPages,
	})
}

var (
	sflArticlePathRe = regexp.MustCompile(`(?i)^/info/(\d+)/(\d+)\.html?$`)
	sflDateRe        = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?`)
	sflNoticeDateRe  = regexp.MustCompile(`^\s*(\d{1,2})\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})`)
	sflArticleIDRe   = regexp.MustCompile(`(?i)_jsq_\(\s*(\d+)\s*,\s*['"][^'"]*['"]\s*,\s*(\d+)`)
	sflPageCountRe   = regexp.MustCompile(`\b\d+\s*/\s*(\d+)\b`)
	sflLabeledDateRe = regexp.MustCompile(`(?:发布时间|发布日期|发表日期|发表时间|日期)\s*[:：]\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
)

// The current xyxw snapshot advertises 58 generated pages. The generic
// paginator does not recognize this site's old table-based pagination, so
// this is only a fallback; parseSflNews records the advertised count when it
// sees a canonical list page.
const sflFallbackPageCount = 58

var sflState = struct {
	sync.RWMutex
	dates      map[string]string
	pageCounts map[string]int
}{
	dates:      make(map[string]string),
	pageCounts: make(map[string]int),
}

func sflTargetSlug(slug string) string {
	slug = strings.Trim(slug, "/")
	if slug == "" || slug == "index" {
		return "xyxw"
	}
	return slug
}

// sflPageURL points the synthetic index feed at the canonical学院新闻 list.
// That list is the only home-level source carrying authoritative dates.
func sflPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = sflTargetSlug(slug)
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}

	pageCount := totalPages
	sflState.RLock()
	if known := sflState.pageCounts[slug]; known > 1 {
		pageCount = known
	}
	sflState.RUnlock()
	if pageCount <= 1 {
		pageCount = sflFallbackPageCount
	}
	pageNumber := pageCount - page + 1
	if pageNumber < 1 {
		pageNumber = 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, pageNumber, extension)
}

func parseSflNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse sfl list: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	// Category/list pages use this exact list. Return immediately when the
	// container exists, even if it is empty: do not fall through to scanning
	// unrelated home-page links on a broken list response.
	list := doc.Find("ul.list-group.rightnew")
	if list.Length() > 0 {
		list.Find("li > a[href]").Each(func(_ int, anchor *goquery.Selection) {
			item := anchor.Closest("li")
			addSflItem(&items, seen, anchor, sflDateFromListItem(item), baseURL)
		})
		sflRememberPageCount(list.Parent())
		return items, nil
	}

	// The home page has seven .product blocks. Their .news-text lists are
	// curated article lists; the adjacent .news-img cards are duplicates or
	// historical highlights and do not have reliable dates. Restricting the
	// selector to news-text fixes the old parser's document-wide info/ scan.
	doc.Find(".product-wrap .product .news-text ul li > a[href]").Each(func(_ int, anchor *goquery.Selection) {
		addSflItem(&items, seen, anchor, "", baseURL)
	})

	// The notice half of the home page uses a split date: <dt>24<em>2026.06
	// </em></dt>. It is still a real article list, so retain it with a
	// normalized date.
	doc.Find(".product-wrap2 .product .notice .con dl").Each(func(_ int, entry *goquery.Selection) {
		anchor := entry.Find("dd > a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		addSflItem(&items, seen, anchor, sflDateFromNotice(entry), baseURL)
	})

	return items, nil
}

func addSflItem(items *[]model.NewsItem, seen map[string]int, anchor *goquery.Selection, date, baseURL string) {
	if anchor == nil || anchor.Length() == 0 || isInNavArea(anchor) {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL, ok := sflArticleURL(baseURL, href)
	if !ok {
		return
	}

	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(strings.Join(strings.Fields(title), " "))
	if title == "" || !isValidNewsTitle(title) {
		return
	}

	date = sflDateFromText(date)
	if date == "" {
		date = sflCachedDate(fullURL)
	}
	if index, exists := seen[fullURL]; exists {
		// On the home page a news-text row can precede the same notice row.
		// Keep the first title/order but upgrade its missing date.
		if (*items)[index].Date == "" && date != "" {
			(*items)[index].Date = date
			sflRememberDate(fullURL, date)
		}
		return
	}

	seen[fullURL] = len(*items)
	*items = append(*items, model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     date,
		ParsedAt: time.Now(),
	})
	if date != "" {
		sflRememberDate(fullURL, date)
	}
}

func sflArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !isValidNewsURL(fullURL) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !sflArticlePathRe.MatchString(parsed.Path) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func sflDateFromListItem(item *goquery.Selection) string {
	if item == nil || item.Length() == 0 {
		return ""
	}
	for _, selector := range []string{".tim", ".date", ".time", "time"} {
		if value := strings.TrimSpace(item.Find(selector).First().Text()); value != "" {
			if date := sflDateFromText(value); date != "" {
				return date
			}
		}
	}
	return ""
}

func sflDateFromNotice(entry *goquery.Selection) string {
	if entry == nil || entry.Length() == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(entry.Find("dt").First().Text()), "")
	match := sflNoticeDateRe.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	return normalizeDateParts(match[2], match[3], match[1])
}

func sflDateFromText(text string) string {
	match := sflDateRe.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return ""
	}
	return normalizeDateParts(match[1], match[2], match[3])
}

func sflRememberDate(articleURL, date string) {
	key := sflArticleKey(articleURL)
	date = sflDateFromText(date)
	if key == "" || date == "" {
		return
	}
	sflState.Lock()
	sflState.dates[key] = date
	sflState.Unlock()
}

func sflCachedDate(articleURL string) string {
	key := sflArticleKey(articleURL)
	if key == "" {
		return ""
	}
	sflState.RLock()
	date := sflState.dates[key]
	sflState.RUnlock()
	return date
}

func sflArticleKey(articleURL string) string {
	parsed, err := url.Parse(articleURL)
	if err != nil {
		return ""
	}
	match := sflArticlePathRe.FindStringSubmatch(parsed.Path)
	if match == nil {
		return ""
	}
	return match[1] + "/" + match[2]
}

func sflRememberPageCount(scope *goquery.Selection) {
	if scope == nil || scope.Length() == 0 {
		return
	}
	if pagination := scope.Find(".lit").First(); pagination.Length() > 0 {
		scope = pagination
	}
	text := strings.Join(strings.Fields(scope.Text()), " ")
	match := sflPageCountRe.FindStringSubmatch(text)
	if match == nil {
		return
	}
	// The only configured sfl feed is redirected to xyxw; keeping this keyed
	// prevents a future category from changing the index's page order.
	var count int
	_, _ = fmt.Sscanf(match[1], "%d", &count)
	if count <= 1 {
		return
	}
	sflState.Lock()
	sflState.pageCounts["xyxw"] = count
	sflState.Unlock()
}

func sflTotalPages(slug string) (int, error) {
	baseURL := "http://sfl.swjtu.edu.cn"
	if site := AllSites["sfl"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = strings.TrimRight(site.BaseURL, "/")
	}
	body, err := fetch(fmt.Sprintf("%s/%s.htm", baseURL, sflTargetSlug(slug)))
	if err != nil {
		return 0, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("sfl parse page count: %w", err)
	}
	sflRememberPageCount(doc.Find("body").First())
	sflState.RLock()
	count := sflState.pageCounts[sflTargetSlug(slug)]
	sflState.RUnlock()
	if count < 1 {
		return 0, fmt.Errorf("sfl page count is missing")
	}
	return count, nil
}

func parseSflArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if date := sflDateFromText(article.Date); date != "" {
		article.Date = date
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return article, nil
	}
	if title := strings.TrimSpace(doc.Find("h1.newtitle2").First().Text()); title != "" {
		article.Title = cleanTitle(title)
	}

	// Some older sfl pages may contain an explicit metadata line. Only inspect
	// metadata containers; a bare date in .v_news_content is an event date,
	// not necessarily the publication date.
	if article.Date == "" {
		for _, selector := range []string{".news_source", ".article-meta", ".article-info", ".detail-info", ".news-info", ".top-info", ".tim"} {
			if date := sflLabeledOrExplicitDate(doc.Find(selector).First().Text()); date != "" {
				article.Date = date
				break
			}
		}
	}

	// The audited article template contains no publication field. Its _jsq_
	// marker gives us the same channel/article key used by the list parser, so
	// a preceding list fetch supplies the authoritative .tim date.
	if article.Date == "" {
		if match := sflArticleIDRe.FindStringSubmatch(string(html)); match != nil {
			article.Date = sflCachedDate("https://sfl.swjtu.edu.cn/info/" + match[1] + "/" + match[2] + ".htm")
		}
	}
	return article, nil
}

func sflLabeledOrExplicitDate(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if match := sflLabeledDateRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	return sflDateFromText(text)
}
