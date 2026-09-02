package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
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
		SubPages:     scaiSubPages(),
		ParseList:    parseScaiAdapterList,
		ParseArticle: parseScaiArticle,
		PageURL:      scaiPageURL,
		TotalPages:   scaiTotalPages,
	})
}

var (
	scaiJavaDateRe      = regexp.MustCompile("(?i)\\b(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)\\s+(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\\s+\\d{1,2}\\s+\\d{2}:\\d{2}:\\d{2}\\s+[A-Z]{2,5}\\s+\\d{4}\\b")
	scaiNumericDateRe   = regexp.MustCompile("(20\\d{2})[\\s\\x{a0}]*[-/.年][\\s\\x{a0}]*(\\d{1,2})[\\s\\x{a0}]*[-/.月][\\s\\x{a0}]*(\\d{1,2})")
	scaiPublishLabelRe  = regexp.MustCompile("(?:发布日期|发布时间|发表日期|发表时间)\\s*[:：]?")
	scaiBlockedPageRe   = regexp.MustCompile("(?:您的访问请求可能对网站造成安全威胁|您的请求可能存在威胁，已被拦截)")
	scaiAttachmentNumRe = regexp.MustCompile(`^\s*\d+\.\s*`)
	scaiTotalPageRe     = regexp.MustCompile(`(?s)共\s*[【\[]?\s*(\d+)\s*[】\]]?\s*页`)
	scaiGotoPageRe      = regexp.MustCompile(`(?i)gotoPage\s*\(\s*(\d+)\s*\)`)
)

var (
	errScaiBlockedPage = errors.New("scai: received the site's WAF block page")
	errScaiNoArticles  = errors.New("scai: no article list or homepage content surface found")
)

// scaiFeedRoute describes a public article-list route. The site uses opaque
// module IDs instead of the static slug.htm convention assumed by the
// shared Boda client. A few navigation entries are parents which silently
// resolve to a child module; only the canonical child route is registered so
// the feed list does not contain duplicate columns.
type scaiFeedRoute struct {
	slug string
	mid  string
	tid  string
}

var scaiFeedRoutes = []scaiFeedRoute{
	{slug: "focus", mid: "860AF115971C57D6"},
	{slug: "notice", mid: "F4DFBF47DF89091A"},
	{slug: "news", mid: "12D0D8DF28CC1D1D"},
	{slug: "notice/files", mid: "F4DFBF47DF89091A", tid: "360"},

	{slug: "party/notice", mid: "B7EB5FEBE48C6A1F"},
	{slug: "party/dynamics", mid: "639B795ABD71EEA4"},
	{slug: "party/topics", mid: "FF4D47A2192F9EFD"},
	{slug: "party/discipline", mid: "7FD9BCBF38B1ECB2"},
	{slug: "party/development", mid: "8A38FE4A5578623F"},
	{slug: "party/care", mid: "76302D048147C5E2"},

	{slug: "research/academic", mid: "A58752D2B803263D"},
	{slug: "research/notice", mid: "94FB6132BF38AC22"},

	{slug: "undergraduate", mid: "B730BEB095B31840"},
	{slug: "undergraduate/notice", mid: "B730BEB095B31840", tid: "350"},
	{slug: "undergraduate/program", mid: "B730BEB095B31840", tid: "351"},
	{slug: "undergraduate/regulations", mid: "B730BEB095B31840", tid: "352"},
	{slug: "undergraduate/downloads", mid: "B730BEB095B31840", tid: "353"},
	{slug: "undergraduate/news", mid: "B730BEB095B31840", tid: "367"},
	{slug: "graduate", mid: "6A69B0E32021446B"},
	{slug: "graduate/notice", mid: "6A69B0E32021446B", tid: "362"},
	{slug: "graduate/training", mid: "6A69B0E32021446B", tid: "363"},
	{slug: "graduate/degree", mid: "6A69B0E32021446B", tid: "364"},
	{slug: "graduate/exit", mid: "6A69B0E32021446B", tid: "365"},
	{slug: "graduate/downloads", mid: "6A69B0E32021446B", tid: "366"},

	{slug: "exchange", mid: "99D2369E2EFC0315"},
	{slug: "exchange/teacher", mid: "99D2369E2EFC0315", tid: "368"},
	{slug: "exchange/student", mid: "99D2369E2EFC0315", tid: "369"},
	{slug: "exchange/inbound", mid: "99D2369E2EFC0315", tid: "370"},
	{slug: "exchange/conference", mid: "99D2369E2EFC0315", tid: "371"},

	{slug: "recruitment/jobs", mid: "B289DCB5E1C69F7C"},
	{slug: "recruitment/publicity", mid: "E2F71BA86B774E97"},
	{slug: "students/notice", mid: "F3D3909EB1861B5D"},
	{slug: "students/news", mid: "9B0C69EAC8A7258F"},
	{slug: "students/publicity", mid: "8EEAD67FC6D491AC"},
	{slug: "students/competition", mid: "2142F5BFAC6C18A3"},

	{slug: "employment/info", mid: "8C9FAC060A7B2F96"},
	{slug: "employment/cooperation", mid: "45CE7200B910DCD0"},
	{slug: "employment/admission", mid: "460A52E2C81B6D0B"},
	{slug: "employment/equivalence", mid: "4A788489F15AE757"},
	{slug: "alumni/news", mid: "756E215058F52683"},
	{slug: "alumni/graduation", mid: "E1CE0987AEFFA8CA"},
}

func scaiSubPages() []string {
	pages := make([]string, 0, len(scaiFeedRoutes))
	for _, route := range scaiFeedRoutes {
		pages = append(pages, route.slug)
	}
	return pages
}

func scaiRouteForSlug(slug string) (scaiFeedRoute, bool) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || strings.EqualFold(slug, "index") {
		return scaiFeedRoute{slug: "index"}, true
	}
	for _, route := range scaiFeedRoutes {
		if strings.EqualFold(slug, route.slug) || strings.EqualFold(slug, route.mid) {
			return route, true
		}
	}
	switch slug {
	case "焦点新闻":
		return scaiFeedRoutes[0], true
	case "通知公告":
		return scaiFeedRoutes[1], true
	case "学院新闻":
		return scaiFeedRoutes[2], true
	case "党建通知公告":
		return scaiFeedRoutes[4], true
	case "科研公告":
		return scaiFeedRoutes[11], true
	case "本科生教育":
		return scaiFeedRoutes[12], true
	case "研究生教育":
		return scaiFeedRoutes[18], true
	case "国际交流":
		return scaiFeedRoutes[24], true
	}
	return scaiFeedRoute{}, false
}

// scaiPageURL maps the browser's logical page number to the query-string
// route emitted by page-module.html. It deliberately keeps tid on sub-tab
// requests; the site's own gotoPage JavaScript drops it, which otherwise
// sends page 2 of every sub-tab back to the aggregate module.
func scaiPageURL(baseURL, slug string, page, _ int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if extension == "" {
		extension = ".htm"
	}
	route, ok := scaiRouteForSlug(slug)
	if !ok {
		// Keep the shared fallback behavior for an opaque caller-supplied slug.
		pathSlug := strings.Trim(slug, "/")
		if page <= 1 {
			return baseURL + "/" + pathSlug + extension
		}
		return baseURL + "/" + pathSlug + "/" + strconv.Itoa(page) + extension
	}
	if route.slug == "index" {
		return baseURL + "/index.html"
	}
	query := url.Values{"mid": []string{route.mid}}
	if route.tid != "" {
		query.Set("tid", route.tid)
	}
	if page > 1 {
		query.Set("page", strconv.Itoa(page))
	}
	return baseURL + "/web/page-module.html?" + query.Encode()
}

// scaiTotalPages reads the page count from the same page-module HTML that the
// browser displays. The shared parser only knows older .p_pages markup, while
// SCAI emits the count in an HTML comment and repeats the last page in
// gotoPage(N). There is no separate count API on this site.
func scaiTotalPages(slug string) (int, error) {
	route, ok := scaiRouteForSlug(slug)
	if !ok {
		return 0, fmt.Errorf("scai: unknown feed %q", slug)
	}
	if route.slug == "index" {
		return 1, nil
	}
	site := AllSites["scai"]
	if site == nil {
		return 0, errors.New("scai: site registration is missing")
	}
	body, err := fetch(scaiPageURL(site.BaseURL, slug, 1, 0, ".html"))
	if err != nil {
		return 0, err
	}
	if scaiIsBlockedPage(body) {
		return 0, errScaiBlockedPage
	}
	if total := scaiTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	if scaiHasPaginationMarker(body) {
		// A valid empty feed advertises zero pages. Returning zero is correct;
		// ListNewsRange has no items from which it could enter a pagination loop.
		return 0, nil
	}
	return 0, errors.New("scai: page-module HTML has no total-page marker")
}

func scaiTotalPagesFromHTML(raw []byte) int {
	if match := scaiTotalPageRe.FindSubmatch(raw); match != nil {
		pageCount, _ := strconv.Atoi(string(match[1]))
		return pageCount
	}
	maxPage := 0
	for _, match := range scaiGotoPageRe.FindAllSubmatch(raw, -1) {
		page, _ := strconv.Atoi(string(match[1]))
		if page > maxPage {
			maxPage = page
		}
	}
	return maxPage
}

func scaiHasPaginationMarker(raw []byte) bool {
	return scaiTotalPageRe.Match(raw) || scaiGotoPageRe.Match(raw)
}

// parseScaiAdapterList extracts each article from its own link and its
// nearest item container. On scai, dates are siblings of ordinary links but
// descendants of featured links, so extracting them from the whole page
// would misalign dates and titles.
func parseScaiAdapterList(html []byte, baseURL string) ([]model.NewsItem, error) {
	if scaiIsBlockedPage(html) {
		return nil, errScaiBlockedPage
	}
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
	if len(items) == 0 && !scaiHasContentSurface(doc) {
		return nil, errScaiNoArticles
	}

	return items, nil
}

func scaiHasContentSurface(doc *goquery.Document) bool {
	return doc.Find(".list-big-title, .list-top, .list-bottom-item, .news, .notice, .learn, .swiper-container").Length() > 0
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
	for _, selector := range []string{".list-item", ".list-top-item", ".item-wrapper", ".news-top-wrapper", ".top-news-item"} {
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

func scaiIsBlockedPage(raw []byte) bool {
	return scaiBlockedPageRe.Match(raw)
}

// parseScaiArticle keeps the shared content/image/attachment extraction but
// owns scai's detail-page metadata hook. The detail template renders a Java
// Date after 发布日期, which is normalized before entering the model.
func parseScaiArticle(html []byte, baseURL string) (*model.Article, error) {
	if scaiIsBlockedPage(html) {
		return nil, errScaiBlockedPage
	}
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
	if source := scaiArticleSource(doc); source != "" {
		article.Source = source
	}
	if date := scaiArticleDate(doc); date != "" {
		article.Date = date
	} else if date := scaiDateFromText(article.Date); date != "" {
		article.Date = date
	}
	article.Content, article.ContentHTML, article.Images, article.Attachments = scaiArticleContent(doc, baseURL)
	return article, nil
}

func scaiArticleSource(doc *goquery.Document) string {
	for _, selector := range []string{".news-content-wrapper .news-info", ".news-info", ".news_source", ".news-source"} {
		found := doc.Find(selector).First()
		if found.Length() == 0 {
			continue
		}
		var source string
		found.Find("span, div, p").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			text := strings.Join(strings.Fields(sel.Text()), " ")
			if strings.HasPrefix(text, "来源：") || strings.HasPrefix(text, "来源:") {
				source = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "来源："), "来源:"))
				return false
			}
			return true
		})
		if source != "" {
			return source
		}
	}
	return ""
}

func scaiArticleContent(doc *goquery.Document, baseURL string) (string, string, []string, []model.Attachment) {
	content := doc.Find(".news-content-wrapper .content-main").First()
	if content.Length() == 0 {
		content = doc.Find(".content-main").First()
	}
	if content.Length() == 0 {
		return "", "", nil, nil
	}

	attachments := scaiArticleAttachments(content, baseURL)
	attachmentURLs := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		attachmentURLs[attachment.URL] = true
	}

	htmlContent := content.Clone()
	seenImages := make(map[string]bool)
	var images []string
	htmlContent.Find("img[src]").Each(func(_ int, sel *goquery.Selection) {
		src := strings.TrimSpace(sel.AttrOr("src", ""))
		if src == "" {
			return
		}
		resolved := resolveURL(baseURL, src)
		if !seenImages[resolved] {
			seenImages[resolved] = true
			images = append(images, resolved)
		}
		sel.SetAttr("src", resolved)
	})
	htmlContent.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if href == "" {
			return
		}
		resolved := resolveURL(baseURL, href)
		if attachmentURLs[resolved] {
			sel.SetAttr("href", resolved)
		}
	})

	textContent := content.Clone()
	textContent.Find(".news-file, script, style, noscript").Remove()
	var parts []string
	textContent.Find("p, h1, h2, h3, h4, h5").Each(func(_ int, sel *goquery.Selection) {
		text := strings.Join(strings.Fields(sel.Text()), " ")
		if text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) == 0 {
		if text := strings.Join(strings.Fields(textContent.Text()), " "); text != "" {
			parts = append(parts, text)
		}
	}
	plainText := strings.Join(parts, "\n\n")
	contentHTML, _ := htmlContent.Html()
	return plainText, contentHTML, images, attachments
}

func scaiArticleAttachments(content *goquery.Selection, baseURL string) []model.Attachment {
	var attachments []model.Attachment
	seen := make(map[string]bool)
	content.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if href == "" || !scaiIsAttachmentLink(sel, href) {
			return
		}
		resolved := resolveURL(baseURL, href)
		if seen[resolved] {
			return
		}
		name := strings.Join(strings.Fields(sel.Text()), " ")
		name = scaiAttachmentNumRe.ReplaceAllString(name, "")
		if name == "" {
			name = strings.TrimSpace(sel.AttrOr("download", ""))
		}
		if name == "" {
			name = path.Base(strings.TrimSuffix(strings.Split(resolved, "?")[0], "/"))
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		attachment := model.Attachment{Name: name, URL: resolved}
		attachment.Size = strings.TrimSpace(sel.AttrOr("data-size", ""))
		attachments = append(attachments, attachment)
	})
	return attachments
}

func scaiIsAttachmentLink(sel *goquery.Selection, href string) bool {
	if sel.Closest(".news-file").Length() > 0 {
		return true
	}
	if strings.TrimSpace(sel.AttrOr("download", "")) != "" {
		return true
	}
	lower := strings.ToLower(href)
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
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
		".news-info",
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
