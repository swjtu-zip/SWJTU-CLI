package scraper

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const mathsCanonicalBaseURL = "https://maths.swjtu.edu.cn"

const mathsMaxResourceBytes = 32 << 20

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
		FetchArticle: mathsFetchArticle,
	})
}

// mathsPageURL maps the source's logical page numbers to its reverse-numbered
// generated files. For a 62-page list, logical page 2 is /61.htm and logical
// page 62 is /1.htm.
func mathsPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = mathsCanonicalBaseURL
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if page < 1 || totalPages < 0 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if slug == "" || slug == "index" || slug == "home" {
		if page != 1 || totalPages > 1 {
			return ""
		}
		return baseURL + "/index" + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	physical := totalPages - page + 1
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
	localRows := make(map[string]bool)
	invalidRows := 0
	itemType := mathsListType(doc)
	rows := region.Find("ul").First().ChildrenFiltered("li")
	if rows.Length() == 0 {
		rows = region.Find("li")
	}
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		fullURL, ok := mathsArticleURL(mathsCanonicalBaseURL, anchor.AttrOr("href", ""))
		if !ok || localRows[fullURL] {
			return
		}
		localRows[fullURL] = true
		title := mathsText(row.Find(".txts, .news-title, h3, h4").First().Text())
		if title == "" {
			title = mathsText(anchor.AttrOr("title", ""))
		}
		if title == "" {
			title = mathsText(anchor.Text())
		}
		if title == "" || !mathsValidNewsTitle(title) {
			invalidRows++
			return
		}
		if itemType == "" {
			invalidRows++
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
	if invalidRows > 0 {
		return nil, fmt.Errorf("maths: %d local article rows could not be parsed", invalidRows)
	}
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
	addWithDate := func(row *goquery.Selection, title, typ string, includeDate bool) {
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
		if title == "" || !mathsValidNewsTitle(title) {
			return
		}
		date, publishedAt := "", ""
		if includeDate {
			date, publishedAt = mathsHomeDate(row)
		}
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
	add := func(row *goquery.Selection, title, typ string) {
		addWithDate(row, title, typ, true)
	}
	addWithoutDate := func(row *goquery.Selection, title, typ string) {
		addWithDate(row, title, typ, false)
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
			// The activity card's .time is the event date, not the article's
			// fixed publication timestamp. Leave it empty until the detail page
			// supplies the authoritative publication node.
			addWithoutDate(row, mathsHomepageTitle(row, row.Find("a[href]").First()), typ)
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

func mathsValidNewsTitle(title string) bool {
	if isValidNewsTitle(title) {
		return true
	}
	// The shared title filter treats every title ending in the Chinese closing
	// quote as a navigation label. Maths has real articles such as
	// “数学学院研究生易曾瑞荣登《人民日报》” and archived forwarded notices
	// that legitimately end with “》”, so retain those non-trivial titles.
	return len(title) > 6 && strings.HasSuffix(title, "》")
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

func mathsArticleContent(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		".inform .v_news_content",
		".v_news_content",
		"[id^='vsb_content'] .v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return doc.Find("__maths_missing__")
}

func mathsFixedDateSource(titleSel *goquery.Selection) string {
	if titleSel == nil || titleSel.Length() == 0 {
		return ""
	}
	candidate := titleSel.Next()
	if candidate.Length() == 0 || candidate.HasClass("v_news_content") || candidate.Find(".v_news_content").Length() > 0 {
		return ""
	}
	id := strings.ToLower(candidate.AttrOr("id", ""))
	if strings.HasPrefix(id, "vsb_content") {
		return ""
	}
	return candidate.Text()
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
	titleSel := doc.Find("form[name='_newscontent_fromname'] > div > h1").First()
	if titleSel.Length() == 0 {
		titleSel = doc.Find(".dynamics .inform h1, .inform h1, form[name='_newscontent_fromname'] h1").First()
	}
	contentSel := mathsArticleContent(doc)
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
	// parseBodaArticle has generic date fallbacks for unregistered sites. They
	// are intentionally discarded here: maths' fixed line immediately after
	// the headline is the only publication metadata source accepted by this
	// adapter, so body dates such as event/deadline dates can never leak in.
	article.Date = ""
	article.PublishedAt = ""
	dateSource := mathsFixedDateSource(titleSel)
	date, publishedAt := mathsDateValues(dateSource)
	if date != "" && mathsHasFullDate(date) {
		article.Date = date
		if publishedAt != "" {
			article.PublishedAt = publishedAt
		} else {
			article.PublishedAt = date
		}
	}
	mathsNormalizeArticleContent(article, contentSel, mathsCanonicalBaseURL)
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
		if href == "" || bodaIsPseudoResourceHref(href) {
			return
		}
		name := mathsText(anchor.Text())
		lower := strings.ToLower(href)
		isDownload := strings.Contains(lower, "download.jsp") || anchor.AttrOr("download", "") != ""
		nearAttachment := strings.Contains(mathsText(anchor.Parent().Text()), "附件") || strings.Contains(name, "附件")
		if !isDownload && !nearAttachment {
			return
		}
		resolved := mathsResourceURL(baseURL, href)
		if resolved == "" {
			return
		}
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

// mathsFetchArticle is the live detail path. The HTML parser remains usable
// with saved source fixtures, while the live path verifies every advertised
// image and attachment before exposing it to callers. The maths site has
// returned HTTP 200 CAPTCHA pages from its download endpoint, so status alone
// is not evidence that an attachment is available.
func mathsFetchArticle(articleURL string) (*model.Article, []byte, error) {
	fullURL, ok := mathsArticleURL(mathsCanonicalBaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("maths: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_mathsArticle(body, mathsCanonicalBaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("maths article %s: %w", fullURL, err)
	}
	article.Images = mathsVerifyImages(article.Images, mathsCanonicalBaseURL)
	article.Attachments = mathsVerifyAttachments(article.Attachments, mathsCanonicalBaseURL)
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		validImages[image] = true
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		validAttachments[attachment.URL] = true
	}
	article.ContentHTML = mathsSanitizeContentHTML(article.ContentHTML, mathsCanonicalBaseURL, validImages, validAttachments)
	return article, body, nil
}

func mathsResourceURL(baseURL, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	return bodaResourceURL(baseURL, raw)
}

func mathsVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := mathsResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := mathsFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func mathsVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := mathsResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		resourceName := strings.TrimSpace(attachment.Name + " " + resolved)
		body, contentType, err := mathsFetchResource(resolved, baseURL, false)
		if err != nil || !mathsResourceBodyIsUsable(contentType, body, resourceName, false) {
			// mathsFetchResource already validates the response. Keep this
			// guard close to the candidate as well so a future resource helper
			// cannot accidentally turn an untyped response into an attachment.
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func mathsFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := mathsResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("maths: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("maths: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	if strings.TrimSpace(baseURL) != "" {
		req.Header.Set("Referer", strings.TrimRight(baseURL, "/")+"/")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("maths: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("maths: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, mathsMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("maths: read resource %s: %w", resolved, err)
	}
	if len(body) > mathsMaxResourceBytes || !mathsResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("maths: resource %s returned an invalid shell or file", resolved)
	}
	return body, contentType, nil
}

func mathsResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	textProbe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(probe))), "\ufeff")
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "附件下载", "请输入验证码", "验证码下载附件",
		"请先登录", "请登录后", "登录页面", "页面不存在", "404 not found", "access denied",
		"cloudflare", "just a moment", "安全验证", "请求已被阻断", "captcha", "waf",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	magic := mathsResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && mathsIsImageMagic(magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.Contains(lowerName, ".tsv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values"
	case strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func mathsIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func mathsResourceMagic(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x03, 0x04}):
		return "zip"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x05, 0x06}):
		return "zip"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x07, 0x08}):
		return "zip"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}):
		return "ole"
	case len(body) >= 3 && bytes.Equal(body[:3], []byte{0xff, 0xd8, 0xff}):
		return "jpeg"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "png"
	case len(body) >= 6 && (bytes.Equal(body[:6], []byte("GIF87a")) || bytes.Equal(body[:6], []byte("GIF89a"))):
		return "gif"
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		return "webp"
	case len(body) >= 2 && bytes.Equal(body[:2], []byte("BM")):
		return "bmp"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{'I', 'I', '*', 0}) || bytes.Equal(body[:4], []byte{'M', 'M', 0, '*'})):
		return "tiff"
	case len(body) >= 7 && bytes.Equal(body[:7], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00}):
		return "rar"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x01, 0x00}):
		return "rar"
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 {
		probe := trimmed
		if len(probe) > 4096 {
			probe = probe[:4096]
		}
		lower := strings.ToLower(string(probe))
		if strings.HasPrefix(lower, "<svg") || (strings.HasPrefix(lower, "<?xml") && strings.Contains(lower, "<svg")) {
			return "svg"
		}
	}
	return ""
}

func mathsSanitizeContentHTML(rawHTML, baseURL string, validImages, validAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="maths-content">` + rawHTML + `</div>`))
	if err != nil {
		return rawHTML
	}
	content := doc.Find("#maths-content").First()
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := mathsImageResourceURL(image, baseURL)
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := mathsResourceURL(baseURL, href)
		if resolved == "" || !validAttachments[resolved] {
			link.RemoveAttr("href")
			link.RemoveAttr("download")
			return
		}
		link.SetAttr("href", resolved)
	})
	if contentHTML, err := content.Html(); err == nil {
		return contentHTML
	}
	return rawHTML
}

func mathsImageResourceURL(image *goquery.Selection, baseURL string) string {
	if image == nil || image.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		if resolved := mathsResourceURL(baseURL, image.AttrOr(attr, "")); resolved != "" {
			return resolved
		}
	}
	return ""
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

func mathsNormalizeArticleContent(article *model.Article, contentSel *goquery.Selection, baseURL string) {
	if article == nil || contentSel == nil || contentSel.Length() == 0 {
		return
	}

	images := make([]string, 0)
	seenImages := make(map[string]bool)
	contentSel.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := mathsResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				resolved = candidate
				break
			}
		}
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
		if !seenImages[resolved] {
			seenImages[resolved] = true
			images = append(images, resolved)
		}
	})
	article.Images = images

	contentSel.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			anchor.RemoveAttr("href")
			return
		}
		if !bodaLooksLikeAttachmentLink(anchor, href) {
			return
		}
		if resolved := mathsResourceURL(baseURL, href); resolved != "" {
			anchor.SetAttr("href", resolved)
		} else {
			anchor.RemoveAttr("href")
		}
	})
	if contentHTML, err := contentSel.Html(); err == nil {
		article.ContentHTML = contentHTML
	}
}

func mathsRejectPage(_ []byte, doc *goquery.Document, kind string) error {
	if doc == nil {
		return fmt.Errorf("maths: nil %s document", kind)
	}
	pageTitle := strings.ToLower(mathsText(doc.Find("title").First().Text()))
	visible := doc.Find("body").First().Clone()
	visible.Find("script, style, noscript, template").Remove()
	if kind == "article" {
		// “验证码” may be legitimate prose in an article. It is only a page
		// marker when it appears in the shell/meta area, not in the body.
		visible.Find(".v_news_content").Remove()
	}
	pageText := strings.ToLower(mathsText(visible.Text()))
	for _, marker := range []string{
		"验证码", "安全验证", "访问过于频繁", "captcha", "verifycode", "人机验证",
		"请完成验证", "请先登录", "请登录后", "附件下载", "页面不存在",
		"404错误", "404 not found", "access denied", "request blocked", "请求已被阻断",
		"cloudflare", "just a moment", "waf",
	} {
		if strings.Contains(pageTitle, marker) || strings.Contains(pageText, marker) {
			return fmt.Errorf("maths: received %s verification/error page", kind)
		}
	}
	if doc.Find(`#captcha, .captcha, [id*="captcha"], [class*="captcha"], input[name="codeValue"], img[src*="createimage.jsp"]`).Length() > 0 {
		return fmt.Errorf("maths: received %s verification page", kind)
	}
	return nil
}

func mathsText(value string) string {
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u200b", "")
	value = strings.ReplaceAll(value, "\ufeff", "")
	return strings.Join(strings.Fields(value), " ")
}
