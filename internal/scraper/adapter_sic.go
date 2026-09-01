package scraper

import (
	"fmt"
	"net/url"
	"regexp"
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
			"tzgg/qb",
			"tzgg/yjsjx",
			"tzgg/bkjx",
			"tzgg/ky",
			"tzgg/xsgz",
			"tzgg/sy",
			"tzgg/xshd",
			"xsjl",
			"jxhd",
		},
		ParseList:    parseSicNews,
		ParseArticle: parseSicArticle,
		PageURL:      sicPageURL,
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
)

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
			ParsedAt: time.Now(),
		})
	})

	return items, nil
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
	if doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html))); docErr == nil {
		// SIC detail pages put the real headline in .show01 h5; the shared
		// parser can otherwise see the breadcrumb/sidebar heading "学院动态"
		// first and return that as the article title.
		if title := cleanTitle(strings.TrimSpace(doc.Find(".show01 h5").First().Text())); title != "" {
			article.Title = title
		}
	}
	if normalized := normalizeSicDate(article.Date); normalized != "" {
		article.Date = normalized
	}
	if article.Date == "" {
		doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
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
	return article, nil
}
