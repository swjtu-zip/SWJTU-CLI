package scraper

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// sic.swjtu.edu.cn is a 博达 site with a template of its own. Its homepage
// and category pages expose article links as info/<column>/<id>.htm, while the
// homepage repeats some of those links in several feature blocks.
func init() {
	registerAdapter("sic", SiteAdapter{
		SubPages: []string{
			"xydt",
			"dqgz/dqdt",
			"dqgz/ghzj",
			"dqgz/xytw",
			"dqgz/jwgz",
			"tzgg/qb",
			"tzgg/dzb",
			"tzgg/yjsjx",
			"tzgg/bkjx",
			"tzgg/ky",
			"tzgg/xsgz",
			"tzgg/sy",
			"tzgg/xshd",
			"tzgg/zsjy",
			"xsgz/xsfc",
			"xsjl",
			"jxhd",
		},
		ParseList:    parseSicNews,
		ParseArticle: parseSicArticle,
		PageURL:      sicPageURL,
		ListPage:     sicListPage,
		FetchArticle: sicFetchArticle,
	})
}

var (
	sicArticlePathRe  = regexp.MustCompile(`(?i)(?:^|/)info/[0-9]+/[0-9]+\.html?$`)
	sicDateRe         = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})`)
	sicYearMonthRe    = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})(?:月)?`)
	sicDayBeforeYMRe  = regexp.MustCompile(`^\s*([0-9]{1,2})\D+(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`)
	sicCompactDayYMRe = regexp.MustCompile(`^\s*([0-9]{1,2})(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`)
	sicYMBeforeDayRe  = regexp.MustCompile(`^\s*(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\D+([0-9]{1,2})\s*(?:日)?\s*$`)
	sicDayOnlyRe      = regexp.MustCompile(`^\s*([0-9]{1,2})\s*(?:日)?\s*$`)
	sicTotalCountRe   = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
)

const sicCanonicalBaseURL = "https://sic.swjtu.edu.cn"

// sicListPage owns the HTTP request for this site because the origin returns
// an access-denied shell when a category page is requested without a browser
// Referer. It also returns the source-reported page count so the shared range
// crawler cannot mistake the largest reverse-numbered href for the page
// count (xydt has 17 pages but its largest href is 16.htm).
func sicListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		page = 1
	}
	firstURL := sicPageURL(sicCanonicalBaseURL, slug, 1, 0, ".htm")
	firstBody, err := sicFetchHTML(firstURL)
	if err != nil {
		return nil, 0, err
	}
	totalPages, err := sicTotalPagesFromHTML(firstBody, slug)
	if err != nil {
		return nil, 0, err
	}
	if page == 1 {
		items, parseErr := parseSicNews(firstBody, sicCanonicalBaseURL)
		return items, totalPages, parseErr
	}
	listURL := sicPageURL(sicCanonicalBaseURL, slug, page, totalPages, ".htm")
	body, err := sicFetchHTML(listURL)
	if err != nil {
		return nil, 0, err
	}
	items, err := parseSicNews(body, sicCanonicalBaseURL)
	return items, totalPages, err
}

// sicTotalPagesFromHTML calculates pages from the displayed article total and
// the number of entries on the newest page. Static/empty shells are not
// silently treated as article lists: the homepage is the sole intentional
// exception and has exactly one page.
func sicTotalPagesFromHTML(body []byte, slug string) (int, error) {
	if strings.Trim(slug, "/") == "index" {
		return 1, nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("sic: parse first page for %s: %w", slug, err)
	}
	countText := strings.TrimSpace(doc.Find(".p_t").First().Text())
	match := sicTotalCountRe.FindStringSubmatch(countText)
	if match == nil {
		return 0, fmt.Errorf("sic: %s has no source article total", slug)
	}
	total, err := strconv.Atoi(match[1])
	if err != nil || total < 0 {
		return 0, fmt.Errorf("sic: invalid article total %q for %s", match[1], slug)
	}
	if total == 0 {
		return 1, nil
	}
	items, err := parseSicNews(body, sicCanonicalBaseURL)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, fmt.Errorf("sic: %s reports %d articles but first page has none", slug, total)
	}
	return (total + len(items) - 1) / len(items), nil
}

func sicFetchHTML(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("sic: create request %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", sicCanonicalBaseURL+"/")
	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sic: fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sic: fetch %s: status %d", rawURL, resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml+xml") {
		return nil, fmt.Errorf("sic: %s returned non-HTML content type %q", rawURL, contentType)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sic: read %s: %w", rawURL, err)
	}
	return body, nil
}

func sicFetchArticle(articleURL string) (*model.Article, []byte, error) {
	body, err := sicFetchHTML(articleURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseSicArticle(body, sicCanonicalBaseURL)
	if err != nil {
		return nil, nil, err
	}
	return article, body, nil
}

// sicPageURL maps the logical page number to the reversed filenames emitted
// by the static site: page 1 is xydt.htm, page 2 is xydt/<total-1>.htm, and
// the last page is xydt/1.htm.
func sicPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page <= 1 || totalPages <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, totalPages-page+1, extension)
}

// parseSicNews parses both the sic homepage and its static category pages.
// The stable signal is the local info/<column>/<id>.htm article path; all
// other links on these pages are navigation, banner, or utility links.
func parseSicNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse sic html: %w", err)
	}

	itemType := sicSectionType(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		if sicIsNonContentAnchor(sel) {
			return
		}

		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if !sicIsArticleHref(href) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, href))
		if isExternalURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}

		title := sicTitle(sel)
		if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
			return
		}
		date := sicDateForAnchor(sel)

		if index, ok := seen[fullURL]; ok {
			// A feature image can be the first occurrence of an article and
			// carry no date; a later text link often has the date. Retain one
			// item while filling fields from the better duplicate.
			if items[index].Title == "" {
				items[index].Title = title
			}
			if items[index].Date == "" && date != "" {
				items[index].Date = date
			}
			return
		}

		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     itemType,
			ParsedAt: time.Now(),
		})
	})

	return items, nil
}

// sicSectionType reads the category heading from both list and detail pages.
// The detail template has a generic h1 (学院动态) before the real headline;
// retaining this value as Type keeps the feed/category metadata correct while
// parseSicArticle separately selects the headline from .show01 h5.
func sicSectionType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	var value string
	for _, selector := range []string{
		"#nav_slide .n_title h1",
		".n_title h1",
		".margin_auto_1",
	} {
		value = cleanTitle(strings.TrimSpace(doc.Find(selector).First().Text()))
		if value != "" {
			break
		}
	}
	if value == "" {
		value = stripSiteSuffix(cleanTitle(strings.TrimSpace(doc.Find("title").First().Text())))
	}
	if value == "" || strings.Contains(value, "集成电路科学与工程学院") {
		return ""
	}
	if value == "全部" {
		return "通知公告"
	}
	return value
}

func sicIsArticleHref(href string) bool {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return false
	}
	return sicArticlePathRe.MatchString(ref.Path)
}

// The generic nav helper treats every .swiper as non-content, but sic uses
// swipers for its real academic and notice sections. Exclude only the actual
// header/banner containers here.
func sicIsNonContentAnchor(sel *goquery.Selection) bool {
	for _, selector := range []string{
		"header",
		"nav",
		"footer",
		".bannerBox",
		"#swiper",
		".navBox",
		".m_nav_list",
		".foot-nav",
	} {
		if sel.Closest(selector).Length() > 0 {
			return true
		}
	}
	return false
}

func sicTitle(sel *goquery.Selection) string {
	title := strings.TrimSpace(sel.AttrOr("title", ""))
	if title == "" {
		for _, selector := range []string{
			".nr h5",
			".text h5",
			".text-title",
			".title",
			"h1",
			"h2",
			"h3",
			"h4",
			"h5",
			"p",
		} {
			candidate := strings.TrimSpace(sel.Find(selector).First().Text())
			if candidate != "" {
				title = candidate
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSpace(sel.Text())
	}
	return cleanTitle(title)
}

var sicDateSelectors = []string{
	".time",
	".date",
	"time",
	".news-time",
	".time_box",
	".time-container",
	".list-time",
	".list-col-time",
	".date-box",
	".publish-time",
	"i",
}

func sicDateForAnchor(anchor *goquery.Selection) string {
	if date := sicDateFromSelection(anchor); date != "" {
		return date
	}

	// On the academic-exchange cards the date is a sibling of the headline
	// anchor inside .con; on category pages it is normally in the same <li>.
	parent := anchor.Parent()
	for depth := 0; parent.Length() > 0 && depth < 4; depth++ {
		if sicIsDateContext(parent) {
			if date := sicDateFromSelection(parent); date != "" {
				return date
			}
		}
		parent = parent.Parent()
	}
	return ""
}

func sicIsDateContext(sel *goquery.Selection) bool {
	if sel.Is("li") {
		return true
	}
	for _, class := range []string{
		"con",
		"part1L",
		"part1RT",
		"list-cell",
		"list-col",
		"list-item",
		"news-item",
		"news-desc",
		"text",
		"info",
	} {
		if sel.HasClass(class) {
			return true
		}
	}
	return false
}

func sicDateFromSelection(sel *goquery.Selection) string {
	for _, selector := range sicDateSelectors {
		var date string
		sel.Find(selector).EachWithBreak(func(_ int, dateSel *goquery.Selection) bool {
			date = sicDateFromDateBlock(dateSel)
			return date == ""
		})
		if date != "" {
			return date
		}
	}
	return ""
}

func sicDateFromDateBlock(sel *goquery.Selection) string {
	text := strings.Join(strings.Fields(sel.Text()), " ")
	if date := normalizeSicDate(text); date != "" {
		return date
	}

	day := strings.TrimSpace(sel.Find(".day, .date-day").First().Text())
	yearMonth := strings.TrimSpace(sel.Find(".year, .date-year").First().Text())
	if date := normalizeSicYearMonthDay(yearMonth, day); date != "" {
		return date
	}

	if match := sicDayBeforeYMRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[2], match[3], match[1])
	}
	if match := sicCompactDayYMRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[2], match[3], match[1])
	}
	if match := sicYMBeforeDayRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}

	var childDay, childYearMonth string
	sel.Children().Each(func(_ int, child *goquery.Selection) {
		childText := strings.TrimSpace(child.Text())
		if childYearMonth == "" && sicYearMonthRe.MatchString(childText) {
			childYearMonth = childText
		}
		if childDay == "" && sicDayOnlyRe.MatchString(childText) {
			childDay = childText
		}
	})
	if date := normalizeSicYearMonthDay(childYearMonth, childDay); date != "" {
		return date
	}
	return ""
}

func normalizeSicYearMonthDay(yearMonth, day string) string {
	ym := sicYearMonthRe.FindStringSubmatch(strings.TrimSpace(yearMonth))
	d := sicDayOnlyRe.FindStringSubmatch(strings.TrimSpace(day))
	if ym == nil || d == nil {
		return ""
	}
	return normalizeDateParts(ym[1], ym[2], d[1])
}

func normalizeSicDate(text string) string {
	match := sicDateRe.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	return normalizeDateParts(match[1], match[2], match[3])
}

// parseSicArticle keeps the common article/body handling but owns the sic
// entry point and normalizes the site's timestamp-bearing metadata line.
func parseSicArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if docErr == nil {
		if pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")); pageType != "" && pageType != "3" {
			return nil, fmt.Errorf("sic: page type %q is not an article", pageType)
		}

		// SIC detail pages put the real headline in .show01 h5; the shared
		// parser can otherwise see the breadcrumb/sidebar heading "学院动态"
		// first and return that as the article title.
		for _, selector := range []string{".show01 h5", ".show01 h1", ".show01 .title"} {
			if title := cleanTitle(strings.TrimSpace(doc.Find(selector).First().Text())); title != "" {
				article.Title = title
				break
			}
		}
		if article.Title == "" {
			return nil, fmt.Errorf("sic: article headline is missing")
		}
		if typ := sicSectionType(doc); typ != "" {
			article.Type = typ
		}

		// The SIC source puts its precise timestamp in the show01 metadata
		// line using <em>, not the shared parser's older <span> shape.
		if meta := doc.Find(".show01").First(); meta.Length() > 0 {
			if precise := normalizePublishedAt(flattenedText(meta)); precise != "" {
				article.PublishedAt = precise
				article.Date = precise[:10]
			}
		}
	}
	if normalized := normalizeSicDate(article.Date); normalized != "" {
		article.Date = normalized
	}
	if article.Date == "" {
		if docErr == nil {
			for _, selector := range []string{
				".news_source",
				".article-meta",
				".article-info",
				".news-info",
				".tips",
				".top-info",
			} {
				text := strings.TrimSpace(doc.Find(selector).First().Text())
				if normalized := normalizeSicDate(text); normalized != "" {
					article.Date = normalized
					break
				}
			}
		}
	}
	sicDeduplicateResources(article)
	return article, nil
}

func sicDeduplicateResources(article *model.Article) {
	if article == nil {
		return
	}
	seenImages := make(map[string]bool, len(article.Images))
	images := article.Images[:0]
	for _, image := range article.Images {
		if image == "" || seenImages[image] {
			continue
		}
		seenImages[image] = true
		images = append(images, image)
	}
	article.Images = images

	seenAttachments := make(map[string]bool, len(article.Attachments))
	attachments := article.Attachments[:0]
	for _, attachment := range article.Attachments {
		if attachment.URL == "" || seenAttachments[attachment.URL] {
			continue
		}
		seenAttachments[attachment.URL] = true
		attachments = append(attachments, attachment)
	}
	article.Attachments = attachments
}
