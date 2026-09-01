package scraper

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// scai.swjtu.edu.cn uses its own static page template instead of the usual
// 博达 list markup. Its home page has several article sections and a separate
// five-slide banner.
func init() {
	registerAdapter("scai", SiteAdapter{
		ParseList:    parseScaiAdapterList,
		ParseArticle: parseScaiArticle,
	})
}

var (
	scaiJavaDateRe     = regexp.MustCompile("(?i)\\b(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)\\s+(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\\s+\\d{1,2}\\s+\\d{2}:\\d{2}:\\d{2}\\s+[A-Z]{2,5}\\s+\\d{4}\\b")
	scaiNumericDateRe  = regexp.MustCompile("(20\\d{2})[\\s\\x{a0}]*[-/.年][\\s\\x{a0}]*(\\d{1,2})[\\s\\x{a0}]*[-/.月][\\s\\x{a0}]*(\\d{1,2})")
	scaiPublishLabelRe = regexp.MustCompile("(?:发布日期|发布时间|发表日期|发表时间)\\s*[:：]?")
)

// parseScaiAdapterList extracts each article from its own link and its
// nearest item container. On scai, dates are siblings of ordinary links but
// descendants of featured links, so extracting them from the whole page
// would misalign dates and titles.
func parseScaiAdapterList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		if sel.HasClass("detail-btn") || isInNavArea(sel) || scaiIsBannerLink(sel) {
			return
		}

		href, _ := sel.Attr("href")
		fullURL, ok := scaiArticleURL(baseURL, href)
		if !ok || seen[fullURL] {
			return
		}

		title := scaiListTitle(sel)
		if !scaiIsArticleTitle(title) {
			return
		}

		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     scaiListDate(sel),
			ParsedAt: time.Now(),
		})
	})

	return items, nil
}

func scaiIsBannerLink(sel *goquery.Selection) bool {
	for _, selector := range []string{
		".banner",
		".swiper-container",
		".swiper-wrapper",
		".swiper-slide",
		".carousel",
		".slider",
		".focus",
		".hero",
	} {
		if sel.Closest(selector).Length() > 0 {
			return true
		}
	}
	return false
}

func scaiArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !strings.EqualFold(path.Base(parsed.Path), "page-newsdetail.html") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if strings.TrimSpace(parsed.Query().Get("nid")) == "" || !isValidNewsURL(fullURL) {
		return "", false
	}

	// A fragment does not identify another article and would otherwise defeat
	// de-duplication if a template adds one.
	parsed.Fragment = ""
	return parsed.String(), true
}

func scaiListTitle(sel *goquery.Selection) string {
	title := strings.TrimSpace(sel.AttrOr("title", ""))
	if title == "" {
		for _, selector := range []string{".title", "h1, h2, h3, h4, h5"} {
			if heading := sel.Find(selector).First(); heading.Length() > 0 {
				title = strings.TrimSpace(heading.Text())
				if title != "" {
					break
				}
			}
		}
	}
	if title == "" {
		title = strings.TrimSpace(sel.Text())
	}
	return strings.Join(strings.Fields(title), " ")
}

func scaiIsArticleTitle(title string) bool {
	if title == "" || !isValidNewsTitle(title) {
		return false
	}
	// The old parser used len(title), which counts UTF-8 bytes and discarded
	// legitimate long Chinese headlines. Bound by runes instead.
	if utf8.RuneCountInString(title) > 200 {
		return false
	}
	return !strings.HasPrefix(title, "了解详情")
}

func scaiListDate(anchor *goquery.Selection) string {
	// Featured cards keep their date inside the article link.
	if date := scaiDateInSelection(anchor); date != "" {
		return date
	}

	// Ordinary section rows put the date next to the link in .list-item.
	for _, selector := range []string{".list-item", ".news-top-wrapper", ".top-news-item"} {
		if item := anchor.Closest(selector); item.Length() > 0 {
			if date := scaiDateInSelection(item); date != "" {
				return date
			}
		}
	}
	return ""
}

func scaiDateInSelection(sel *goquery.Selection) string {
	for _, selector := range []string{
		".calendar .day",
		".calendar .date",
		".date-label",
		"time[datetime]",
		"time",
		"[data-date]",
	} {
		if dateSel := sel.Find(selector).First(); dateSel.Length() > 0 {
			if value := dateSel.AttrOr("datetime", ""); value != "" {
				if date := scaiDateFromText(value); date != "" {
					return date
				}
			}
			if value := dateSel.AttrOr("data-date", ""); value != "" {
				if date := scaiDateFromText(value); date != "" {
					return date
				}
			}
			if date := scaiDateFromText(dateSel.Text()); date != "" {
				return date
			}
		}
	}
	return ""
}

func scaiDateFromText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if match := scaiJavaDateRe.FindString(text); match != "" {
		if parsed, err := time.Parse("Mon Jan 02 15:04:05 MST 2006", match); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	if match := scaiNumericDateRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	return ""
}

// parseScaiArticle keeps the shared content/image/attachment extraction but
// owns scai's detail-page metadata hook. The detail template renders a Java
// Date after 发布日期, which is normalized before entering the model.
func parseScaiArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return article, nil
	}
	if title := scaiArticleTitle(doc); title != "" {
		article.Title = title
	}
	if date := scaiArticleDate(doc); date != "" {
		article.Date = date
	} else if date := scaiDateFromText(article.Date); date != "" {
		article.Date = date
	}
	return article, nil
}

// scaiArticleTitle is intentionally scoped to the article content wrapper.
// SCAI repeats the school name in the document title and header; when a
// detail-page variant does not expose the headline through a generic selector,
// the unscoped fallback can therefore return the school name as the article
// title. The real detail template puts the headline under .content inside
// .news-content-wrapper.
func scaiArticleTitle(doc *goquery.Document) string {
	for _, selector := range []string{
		".news-content-wrapper .content > h1.news-title",
		".news-content-wrapper .content > .news-title",
		".news-content-wrapper .content > h1",
		".news-content-wrapper .content > h2",
		".news-content-wrapper .content > .article-title",
		".news-content-wrapper .content > .detail-title",
		".news-content-wrapper .content > .title",
		".news-content-wrapper .content .news-title",
	} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			title := strings.Join(strings.Fields(found.Text()), " ")
			if scaiIsArticleTitle(title) {
				return title
			}
		}
	}
	return ""
}

func scaiArticleDate(doc *goquery.Document) string {
	for _, selector := range []string{
		"meta[property=\"article:published_time\"]",
		"meta[name=\"publishdate\"]",
		"meta[name=\"publish_date\"]",
		".news_source",
		".news-source",
		".news-meta",
		".article-meta",
		".article-info",
		".publish-info",
		".publish-date",
		".little-tips",
	} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			if value := found.AttrOr("content", ""); value != "" {
				if date := scaiDateFromText(value); date != "" {
					return date
				}
			}
			if date := scaiDateFromText(found.Text()); date != "" {
				return date
			}
		}
	}

	// If a copy of the template has no stable metadata class, only inspect
	// small elements with an explicit publish-date label. This prevents an
	// event date in the article body from becoming the publish date.
	var date string
	doc.Find("body *").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		text := strings.Join(strings.Fields(sel.Text()), " ")
		if utf8.RuneCountInString(text) > 256 || !scaiPublishLabelRe.MatchString(text) {
			return true
		}
		date = scaiDateFromText(text)
		return date == ""
	})
	return date
}
