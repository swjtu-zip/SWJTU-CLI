package scraper

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const mathsCanonicalBaseURL = "https://maths.swjtu.edu.cn"

// mathsSubPages is the complete set of article-bearing pages found by
// walking the live navigation, homepage modules, and their visible/hidden
// tabs. Static profile/service pages and external links are deliberately not
// included.
var mathsSubPages = []string{
	"jyjx1",
	"jyjx1/bkjx1",
	"jyjx1/yjsjx1",
	"jyjx1/zyjsjjyq",
	"jyjx1/sxsyzx",
	"jyjx1/sxjd",
	"jyjx1/hzq",
	"xsky/kytz",
	"xsky/xsjl",
	"xsky/sjycx",
	"xsky/kycg",
	"xsky/scsxctjxh",
	"dwjw",
	"dwjw/djdt",
	"dwjw/djtz",
	"dwjw/llxx",
	"dwjw/rdzn",
	"xsgz",
	"xsgz/xgtz",
	"xsgz/xshd",
	"xsgz/jyxx",
	"ghgz",
	"ghgz/ghhd",
	"xygz",
	"xygz/xyhd",
	"xyxw",
	"xygg",
}

var (
	mathsArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	mathsDateRE        = regexp.MustCompile(`(?i)(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*(?:日|号)?`)
	mathsDateTimeRE    = regexp.MustCompile(`(?i)(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*(?:日|号)?\s*(?:[Tt]|\s+)\s*([0-9]{1,2})\s*[:：]\s*([0-9]{2})(?:\s*[:：]\s*([0-9]{2}))?`)
)

func init() {
	if site := AllSites["maths"]; site != nil {
		site.BaseURL = mathsCanonicalBaseURL
	}
	registerAdapter("maths", SiteAdapter{
		SubPages:     mathsSubPages,
		ParseList:    parseAdapter_mathsList,
		ParseArticle: parseAdapter_mathsArticle,
		PageURL:      mathsPageURL,
		TotalPages:   mathsTotalPages,
	})
}

// mathsPageURL maps the source's logical page numbers to its reverse-numbered
// generated files. For a 62-page list, logical page 2 is /61.htm and logical
// page 62 is /1.htm.
func mathsPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = mathsCanonicalBaseURL
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if slug == "" || slug == "index" || slug == "home" {
		return baseURL + "/index" + extension
	}
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	physical := totalPages - page + 1
	if physical < 1 {
		// Do not silently repeat the oldest page for an out-of-range request.
		physical = 0
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

func mathsKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	for _, known := range mathsSubPages {
		if slug == known {
			return true
		}
	}
	return false
}

func mathsTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" || slug == "home" {
		return 1, nil
	}
	if !mathsKnownFeed(slug) {
		return 0, fmt.Errorf("maths: %q is not an audited article-list route", slug)
	}
	body, err := fetch(mathsPageURL(mathsCanonicalBaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("maths: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("maths: parse first page for %s: %w", slug, err)
	}
	if err := mathsRejectPage(body, doc, "list"); err != nil {
		return 0, err
	}
	if mathsListRegion(doc).Length() == 0 {
		return 0, fmt.Errorf("maths: %s is not an article list", slug)
	}
	total := mathsPageCount(doc)
	if total > 1 {
		// xsjl currently publishes a stale page-2 link: the first page says
		// that the total is 47 and links to /46.htm, but that URL is a real
		// 404. The adjacent /45.htm is 200 and labels itself logical page 2
		// with a 46-page tail. Probe before adjusting; never guess a page
		// count merely because a site resembles another Boda site.
		secondURL := mathsPageURL(mathsCanonicalBaseURL, slug, 2, total, ".htm")
		if _, secondErr := fetch(secondURL); secondErr != nil && strings.Contains(secondErr.Error(), "status 404") {
			usableTotal := total - 1
			candidateURL := mathsPageURL(mathsCanonicalBaseURL, slug, 2, usableTotal, ".htm")
			candidateBody, candidateErr := fetch(candidateURL)
			if candidateErr == nil {
				candidateDoc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(candidateBody)))
				if parseErr == nil && mathsRejectPage(candidateBody, candidateDoc, "list") == nil && mathsListRegion(candidateDoc).Length() > 0 && mathsPageCount(candidateDoc) == usableTotal {
					return usableTotal, nil
				}
			}
		}
	}
	return total, nil
}

func mathsPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	pageBox := doc.Find(".p_pages").First()
	if pageBox.Length() == 0 {
		return 1
	}
	maxPage := 0
	pageBox.Find(".p_no, .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		value := strings.TrimSpace(sel.Text())
		if page, err := strconv.Atoi(value); err == nil && page > maxPage {
			maxPage = page
		}
	})
	if maxPage == 0 {
		return 1
	}
	return maxPage
}

func parseAdapter_mathsList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("maths: parse list HTML: %w", err)
	}
	if err := mathsRejectPage(raw, doc, "list"); err != nil {
		return nil, err
	}
	pageType := mathsPageType(doc)
	if pageType == "1" {
		items := mathsHomepageItems(doc, mathsCanonicalBaseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("maths: homepage contains no local article module")
		}
		return items, nil
	}
	if pageType != "" && pageType != "2" {
		return nil, fmt.Errorf("maths: page is not an article list (pageType %q)", pageType)
	}
	region := mathsListRegion(doc)
	if region.Length() == 0 {
		return nil, fmt.Errorf("maths: page is not an article list")
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	itemType := mathsListType(doc)
	rows := region.Find("ul").First().ChildrenFiltered("li")
	if rows.Length() == 0 {
		rows = region.Find("li")
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		fullURL, ok := mathsArticleURL(mathsCanonicalBaseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := mathsText(row.Find(".txts, .news-title, h3, h4").First().Text())
		if title == "" {
			title = mathsText(anchor.AttrOr("title", ""))
		}
		if title == "" {
			title = mathsText(anchor.Text())
		}
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := mathsDateValues(row.Find(".time, .date, time").First().Text())
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        itemType,
			ParsedAt:    time.Now(),
		})
	})
	// A recognized .lists region with no local rows is a legal empty list.
	// Static pages never reach this branch because they have no list region.
	return items, nil
}

func mathsListRegion(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{".dynamics .inform .lists", ".inform .lists", ".lists"} {
		if region := doc.Find(selector).First(); region.Length() > 0 {
			return region
		}
	}
	return doc.Find("__maths_missing__")
}

func mathsListType(doc *goquery.Document) string {
	for _, selector := range []string{".dynamics .inform > .title .txts", ".inform > .title .txts", ".page-bann h5"} {
		if value := mathsText(doc.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func mathsHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	add := func(row *goquery.Selection, title, typ string) {
		if row == nil || row.Length() == 0 {
			return
		}
		anchor := row.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := mathsArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		if title == "" {
			title = mathsHomepageTitle(row, anchor)
		}
		title = mathsText(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := mathsHomeDate(row)
		typ = mathsText(typ)
		if index, exists := seen[fullURL]; exists {
			current := &items[index]
			if (current.Date == "" || !mathsHasFullDate(current.Date)) && date != "" {
				current.Date = date
				current.PublishedAt = publishedAt
			}
			if current.Type == "" || current.Type == "首页焦点" {
				if typ != "" {
					current.Type = typ
				}
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	}

	// Carousel links are local article links even though they do not carry a
	// publication date. Keep them as a separate homepage module and let the
	// detail parser supply the date later; never invent a year from MM-DD.
	doc.Find(".news .bann .swiper-wrapper > li, .bann .swiper-wrapper > li").Each(func(_ int, row *goquery.Selection) {
		add(row, mathsHomepageTitle(row, row.Find("a[href]").First()), "首页焦点")
	})

	if block := doc.Find(".news .dynamic").First(); block.Length() > 0 {
		typ := mathsHomepageType(block)
		block.Find(".list > ul > li").Each(func(_ int, row *goquery.Selection) {
			add(row, mathsHomepageTitle(row, row.Find("a[href]").First()), typ)
		})
	}

	if block := doc.Find(".news .notice").First(); block.Length() > 0 {
		typ := mathsHomepageType(block)
		tabs := make([]string, 0)
		block.Find(".tabs li").Each(func(_ int, sel *goquery.Selection) {
			tabs = append(tabs, mathsText(sel.Text()))
		})
		block.Find(".list > ul").Each(func(index int, list *goquery.Selection) {
			panelType := typ
			if index < len(tabs) && tabs[index] != "" && tabs[index] != "全部" && tabs[index] != "全 部" {
				panelType = tabs[index]
			}
			list.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
				add(row, mathsHomepageTitle(row, row.Find("a[href]").First()), panelType)
			})
		})
	}

	if block := doc.Find(".news .activity").First(); block.Length() > 0 {
		typ := mathsHomepageType(block)
		block.Find(".list > ul > li").Each(func(_ int, row *goquery.Selection) {
			add(row, mathsHomepageTitle(row, row.Find("a[href]").First()), typ)
		})
	}

	// 党建工作、教学工作、学生工作 are separate homepage modules. They
	// are not all visible at once on small screens, but all are in the HTML.
	doc.Find(".type .item").Each(func(_ int, block *goquery.Selection) {
		typ := mathsText(block.Find(".title b").First().Text())
		block.Find(".list > ul > li").Each(func(_ int, row *goquery.Selection) {
			add(row, mathsHomepageTitle(row, row.Find("a[href]").First()), typ)
		})
	})

	return items
}

func mathsHomepageType(block *goquery.Selection) string {
	if block == nil || block.Length() == 0 {
		return ""
	}
	if typ := mathsText(block.Find(".titles .txts span").First().Text()); typ != "" {
		return typ
	}
	return mathsText(block.Find(".titles .txts").First().Text())
}

func mathsHomepageTitle(row, anchor *goquery.Selection) string {
	if anchor != nil && anchor.Length() > 0 {
		if title := mathsText(anchor.AttrOr("title", "")); title != "" {
			return title
		}
	}
	for _, selector := range []string{".txts", ".tits .txts", ".text h5", ".text .title", ".text", ".title"} {
		if title := mathsText(row.Find(selector).First().Text()); title != "" {
			return title
		}
	}
	if anchor != nil && anchor.Length() > 0 {
		return mathsText(anchor.Text())
	}
	return ""
}

func mathsHomeDate(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	date, publishedAt := mathsDateValues(row.Find(".time, .date, time").First().Text())
	if mathsHasFullDate(date) {
		return date, publishedAt
	}
	day := mathsText(row.Find(".time h5").First().Text())
	yearMonth := mathsText(row.Find(".time p").First().Text())
	if day != "" && yearMonth != "" {
		if match := regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`).FindStringSubmatch(yearMonth); match != nil {
			date = fmt.Sprintf("%s-%02s-%02s", match[1], match[2], day)
			return date, ""
		}
	}
	return date, publishedAt
}

func mathsDateValues(value string) (string, string) {
	value = mathsText(value)
	if value == "" {
		return "", ""
	}
	if match := mathsDateTimeRE.FindStringSubmatch(value); match != nil {
		second := match[6]
		if second == "" {
			second = "00"
		}
		publishedAt := fmt.Sprintf("%s-%02s-%02s %02s:%s:%s", match[1], match[2], match[3], match[4], match[5], second)
		return publishedAt[:10], publishedAt
	}
	if match := mathsDateRE.FindStringSubmatch(value); match != nil {
		return fmt.Sprintf("%s-%02s-%02s", match[1], match[2], match[3]), ""
	}
	// MM-DD on the homepage is source text, not a complete date. Preserve it
	// without fabricating a year; the detail page is authoritative.
	return value, ""
}

func mathsHasFullDate(value string) bool {
	return mathsDateRE.MatchString(value)
}

func parseAdapter_mathsArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("maths: parse article HTML: %w", err)
	}
	if err := mathsRejectPage(raw, doc, "article"); err != nil {
		return nil, err
	}
	if mathsPageType(doc) != "3" {
		return nil, fmt.Errorf("maths: page is not an article (pageType %q)", mathsPageType(doc))
	}
	titleSel := doc.Find(".dynamics .inform h1, .inform h1, form[name='_newscontent_fromname'] h1").First()
	contentSel := doc.Find(".inform .v_news_content, .v_news_content").First()
	if titleSel.Length() == 0 {
		return nil, fmt.Errorf("maths: article has no detail headline")
	}
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("maths: article has no v_news_content body")
	}

	article, err := parseBodaArticle(raw, mathsCanonicalBaseURL)
	if err != nil {
		return nil, err
	}
	article.Title = mathsText(titleSel.Text())
	if article.Title == "" {
		return nil, fmt.Errorf("maths: article headline is empty")
	}
	article.Type = mathsListType(doc)
	if article.Type == "" {
		return nil, fmt.Errorf("maths: article has no breadcrumb type")
	}
	dateSource := titleSel.Next().Text()
	date, publishedAt := mathsDateValues(dateSource)
	if date != "" && mathsHasFullDate(date) {
		article.Date = date
		if publishedAt != "" {
			article.PublishedAt = publishedAt
		}
	}
	mathsAppendDocumentAttachments(article, doc, mathsCanonicalBaseURL)
	if article.Content == "" && contentSel.Find("img").Length() == 0 {
		return nil, fmt.Errorf("maths: article body is empty")
	}
	return article, nil
}

func mathsAppendDocumentAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find(".inform a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
		if href == "" {
			return
		}
		name := mathsText(anchor.Text())
		lower := strings.ToLower(href)
		isDownload := strings.Contains(lower, "download.jsp") || anchor.AttrOr("download", "") != ""
		nearAttachment := strings.Contains(mathsText(anchor.Parent().Text()), "附件") || strings.Contains(name, "附件")
		if !isDownload && !nearAttachment {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if seen[resolved] {
			return
		}
		if name == "" {
			name = href
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func mathsArticleURL(baseURL, href string) (string, bool) {
	href = html.UnescapeString(strings.TrimSpace(href))
	fullURL := cleanURL(resolveURL(mathsCanonicalBaseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), "maths.swjtu.edu.cn") || !mathsArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	parsed.Scheme = "https"
	parsed.Host = "maths.swjtu.edu.cn"
	parsed.Fragment = ""
	return parsed.String(), true
}

func mathsPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
}

func mathsRejectPage(raw []byte, doc *goquery.Document, kind string) error {
	text := strings.ToLower(string(raw))
	for _, marker := range []string{"验证码", "安全验证", "访问过于频繁", "captcha", "verifycode"} {
		if strings.Contains(text, marker) {
			return fmt.Errorf("maths: received %s verification page", kind)
		}
	}
	if doc == nil {
		return fmt.Errorf("maths: nil %s document", kind)
	}
	return nil
}

func mathsText(value string) string {
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u200b", "")
	value = strings.ReplaceAll(value, "\ufeff", "")
	return strings.Join(strings.Fields(value), " ")
}
