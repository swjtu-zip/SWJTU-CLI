package scraper

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// The mechanical engineering site uses the same Visual SiteBuilder article
// URLs as many other SWJTU sites, but its home page and category pages contain
// several different lists. The generic parser stops after the first matching
// list and consequently misses most articles (and can mistake a section link
// for an article). Keep the extraction scoped to SME article URLs here.
func init() {
	registerAdapter("sme", SiteAdapter{
		SubPages: []string{
			"xwtz/xyxw",
			"xwtz/xshd",
			"dqgz/p/tzgg",
			"rcpy/bks/tzgg",
			"rcpy/yjs/tzgg",
			"kxyj/tzgg",
			"xsgz/tzgg",
			"xwtz/tmzl",
			"xwtz/qtgg",
		},
		ParseList:    parseSmeNews,
		ParseArticle: parseSmeArticle,
		PageURL:      smePageURL,
	})
}

var (
	smeArticlePathPattern  = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	smeFullDatePattern     = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?`)
	smeDayYearMonthPattern = regexp.MustCompile(`(?i)(?:^|\s)(\d{1,2})\s+(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*(?:月)?`)
	smeYearMonthDayPattern = regexp.MustCompile(`(?i)(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*(?:月)?\s+(\d{1,2})\s*日?`)
)

// smePageURL maps the logical page number (1=newest) to the generated file
// number used by SME (1=oldest). For example, the live xyxw page advertises
// 177 pages and its logical page 2 is /xwtz/xyxw/176.htm.
func smePageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page < 1 {
		page = 1
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}

	pageNumber := page
	if totalPages > 1 {
		pageNumber = totalPages - page + 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, pageNumber, extension)
}

func parseSmeNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse SME list: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	pageType := smeBreadcrumbType(doc)
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if isSmeNonContentLink(anchor) {
			return
		}

		href, ok := anchor.Attr("href")
		if !ok || href == "" {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, href))
		if !isSmeArticleURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}

		card := smeArticleCard(anchor)
		title := smeArticleTitle(anchor, card)
		if !isSmeNewsTitle(title) {
			return
		}
		date := smeArticleDateFromList(anchor, card, baseURL)
		addSmeItem(&items, seen, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     smeArticleType(anchor, pageType),
			ParsedAt: time.Now(),
		})
	})

	return items, nil
}

func parseSmeArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	// parseBodaArticle already handles the common SME article template. The
	// wrapper is deliberately site-specific so that its dotted/Chinese date
	// form is normalized even when the generic source block is used.
	if normalized := normalizeSmeDate(article.Date); normalized != "" {
		article.Date = normalized
	}

	doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if docErr != nil {
		return article, nil
	}
	titleSel := smeArticleTitleSelection(doc)
	if article.Title == "" && titleSel.Length() > 0 {
		article.Title = strings.TrimSpace(titleSel.Text())
	}
	if article.Date == "" {
		article.Date = smeArticleMetaDate(doc, titleSel)
	}
	if article.Type == "" {
		article.Type = smeBreadcrumbType(doc)
	}
	appendSmeAttachments(article, doc, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func isSmeArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return false
	}
	return smeArticlePathPattern.MatchString(parsed.Path)
}

var smeNonContentSelectors = []string{
	".banner", ".swiper", ".swiper-container", ".swiper-wrapper", ".swiper-slide",
	".slider", ".slick", ".carousel", ".focus", ".hero", ".floadAd", ".float-ad",
	".advert", ".advertisement", ".popup", ".modal", ".breadcrumb", ".crumb",
	".pagination", ".p_pages", ".share",
	`[class*="banner"]`, `[class*="swiper"]`, `[class*="carousel"]`,
	`[class*="slider"]`, `[class*="focus"]`,
}

func isSmeNonContentLink(sel *goquery.Selection) bool {
	// The home page uses the generic .carousel class for two real article
	// sections. Other carousels (banners/focus widgets) are still excluded.
	if isSmeArticleCarousel(sel) {
		return false
	}
	if isInNavArea(sel) {
		return true
	}
	for _, selector := range smeNonContentSelectors {
		if sel.Closest(selector).Length() > 0 {
			return true
		}
	}
	return false
}

func isSmeArticleCarousel(sel *goquery.Selection) bool {
	for _, id := range []string{"news", "ac"} {
		if sel.Closest("#"+id).Length() > 0 {
			return true
		}
	}
	return false
}

func smeArticleCard(anchor *goquery.Selection) *goquery.Selection {
	var card *goquery.Selection
	anchor.Parents().EachWithBreak(func(_ int, parent *goquery.Selection) bool {
		tag := strings.ToLower(goquery.NodeName(parent))
		class := strings.ToLower(parent.AttrOr("class", ""))
		if tag == "li" || tag == "article" || isSmeCardClass(class) {
			card = parent
			return false
		}
		return true
	})
	if card != nil {
		return card
	}
	return anchor.Parent()
}

func isSmeCardClass(class string) bool {
	for _, token := range strings.Fields(class) {
		switch token {
		case "item", "card", "entry", "article", "news", "notice", "contentli":
			return true
		}
		if strings.Contains(token, "news-item") || strings.Contains(token, "notice-item") ||
			strings.HasSuffix(token, "-card") || strings.HasSuffix(token, "-entry") ||
			strings.HasSuffix(token, "-item") {
			return true
		}
	}
	return false
}

var smeTitleSelectors = []string{
	"h1, h2, h3, h4, h5",
	".article-title, .news-title, .item-title, .notice-title, .title, .name",
	`[class*="title"]`,
}

func smeArticleTitle(anchor, card *goquery.Selection) string {
	for _, attr := range []string{"title", "data-title", "aria-label"} {
		if value := cleanSmeTitle(anchor.AttrOr(attr, "")); isSmeNewsTitle(value) {
			return value
		}
	}

	if title := smeTitleFromSelection(anchor); isSmeNewsTitle(title) {
		return title
	}
	if card != nil && card.Length() > 0 {
		if title := smeTitleFromSelection(card); isSmeNewsTitle(title) {
			return title
		}
	}

	return cleanSmeTitle(anchor.Text())
}

func smeTitleFromSelection(sel *goquery.Selection) string {
	for _, selector := range smeTitleSelectors {
		var title string
		sel.Find(selector).EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
			value := cleanSmeTitle(candidate.Text())
			if isSmeNewsTitle(value) {
				title = value
				return false
			}
			return true
		})
		if title != "" {
			return title
		}
	}

	var imageTitle string
	sel.Find("img[title], img[alt]").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
		for _, attr := range []string{"title", "alt"} {
			value := cleanSmeTitle(candidate.AttrOr(attr, ""))
			if isSmeNewsTitle(value) {
				imageTitle = value
				return false
			}
		}
		return true
	})
	return imageTitle
}

func cleanSmeTitle(value string) string {
	value = cleanTitle(strings.Join(strings.Fields(value), " "))
	return strings.TrimSpace(strings.Trim(value, " →"))
}

func isSmeNewsTitle(title string) bool {
	if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) {
		return false
	}
	switch title {
	case "学院新闻", "Faculty News", "学术活动", "Academic and Scientific Research",
		"通知公告", "Notices", "多彩机械", "Colorful Machinery", "数据机械", "Data Machinery",
		"专题网站", "Special Website", "查看详情", "查看详情 →":
		return false
	}
	return !strings.HasPrefix(title, "查看详情")
}

func smeArticleDateFromList(anchor, card *goquery.Selection, baseURL string) string {
	for _, attr := range []string{"datetime", "data-date", "data-time"} {
		if date := normalizeSmeDate(anchor.AttrOr(attr, "")); date != "" {
			return date
		}
	}
	// SME's home-page tabs put the date in a span inside the anchor. Check
	// the anchor before its card because an academic carousel item can hold
	// two articles with different dates.
	if date := smeDateFromSelection(anchor); date != "" {
		return date
	}
	if card != nil && card.Length() > 0 {
		if date := smeDateFromSelection(card); date != "" {
			return date
		}
	}

	// A few generated pages put the date beside the card rather than inside
	// it. Only walk through ancestors that still contain one unique article
	// URL; this prevents taking the date belonging to the next list item.
	date := ""
	depth := 0
	anchor.Parents().EachWithBreak(func(_ int, parent *goquery.Selection) bool {
		depth++
		if depth > 5 || smeArticleLinkCount(parent, baseURL) > 1 {
			return false
		}
		if date = smeDateFromSelection(parent); date != "" {
			return false
		}
		return true
	})
	return date
}

var smeDateSelectors = `time, [datetime], [data-date], [data-time], .date, .time, span.flex-none, [class*="date"], [class*="Date"], [class*="time"], [class*="Time"]`

func smeDateFromSelection(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}

	parts := make([]string, 0, 4)
	sel.Find(smeDateSelectors).Each(func(_ int, dateSel *goquery.Selection) {
		for _, attr := range []string{"datetime", "data-date", "data-time"} {
			if value := strings.TrimSpace(dateSel.AttrOr(attr, "")); value != "" {
				parts = append(parts, value)
			}
		}
		// Selection.Text() concatenates adjacent day and year-month spans
		// (for example "132026-07"). Preserve tag boundaries so the
		// date normalizer can distinguish the two source fields.
		if value := strings.TrimSpace(flattenedText(dateSel)); value != "" {
			parts = append(parts, value)
		}
	})
	for _, part := range parts {
		if date := normalizeSmeDate(part); date != "" {
			return date
		}
	}
	if date := normalizeSmeDate(strings.Join(parts, " ")); date != "" {
		return date
	}
	return ""
}

func smeArticleLinkCount(sel *goquery.Selection, baseURL string) int {
	seen := make(map[string]bool)
	sel.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href, _ := anchor.Attr("href")
		fullURL := cleanURL(resolveURL(baseURL, href))
		if isSmeArticleURL(fullURL, baseURL) {
			seen[fullURL] = true
		}
	})
	return len(seen)
}

func addSmeItem(items *[]model.NewsItem, seen map[string]int, item model.NewsItem) {
	if index, ok := seen[item.URL]; ok {
		// Home page cards commonly repeat the same article. Keep the first
		// occurrence's order, but let a later list copy supply its date.
		if (*items)[index].Date == "" && item.Date != "" {
			(*items)[index].Date = item.Date
		}
		if !isSmeNewsTitle((*items)[index].Title) && isSmeNewsTitle(item.Title) {
			(*items)[index].Title = item.Title
		}
		if (*items)[index].Type == "" && item.Type != "" {
			(*items)[index].Type = item.Type
		}
		return
	}
	seen[item.URL] = len(*items)
	*items = append(*items, item)
}

func smeArticleType(anchor *goquery.Selection, pageType string) string {
	for id, typ := range map[string]string{
		"news":  "学院新闻",
		"ac":    "学术活动",
		"Tab1":  "党政办公",
		"Tab2":  "本科生教学",
		"Tab3":  "研究生教学",
		"Tab4":  "科学研究",
		"Tab5":  "学生工作",
		"Tab6":  "推免专栏",
		"Tab7":  "其他",
		"Tab8":  "其他",
		"Tab9":  "推免专栏",
		"Tab10": "公示专栏",
	} {
		if anchor.Closest("#"+id).Length() > 0 {
			return typ
		}
	}
	return pageType
}

func smeBreadcrumbType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	typ := ""
	doc.Find(".path_info_box a").Each(func(_ int, sel *goquery.Selection) {
		value := cleanSmeTitle(sel.Text())
		if value == "" || value == "学院首页" || value == "首页" {
			return
		}
		typ = value
	})
	return typ
}

func appendSmeAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}

	// SME places its DownloadAttachUrl links in a UL immediately after
	// .v_news_content, so the generic content-only attachment scan misses
	// them. Restrict this scan to the article detail container.
	doc.Find(`main.show_detail_box a[href], .show_content_detail a[href]`).Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if href == "" {
			return
		}
		hrefLower := strings.ToLower(href)
		if !strings.Contains(hrefLower, "downloadattachurl") && !strings.Contains(hrefLower, "download.jsp") {
			return
		}
		name := strings.TrimSpace(htmlstd.UnescapeString(strings.Join(strings.Fields(sel.Text()), " ")))
		if name == "" {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func normalizeSmeDate(value string) string {
	value = htmlstd.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.Join(strings.Fields(value), " ")
	if match := smeFullDatePattern.FindStringSubmatch(value); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	if match := smeDayYearMonthPattern.FindStringSubmatch(value); match != nil {
		return normalizeDateParts(match[2], match[3], match[1])
	}
	if match := smeYearMonthDayPattern.FindStringSubmatch(value); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	return ""
}

func smeArticleTitleSelection(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		"h1.news_tit", ".article-title", ".news-title", ".detail-title", ".cont-tit h1", "h1",
	} {
		if selection := doc.Find(selector).First(); selection.Length() > 0 {
			return selection
		}
	}
	return doc.Find("title").First()
}

func smeArticleMetaDate(doc *goquery.Document, titleSel *goquery.Selection) string {
	// The live article template places 发布时间 immediately beside the h1.
	// Reuse the shared meta-zone guard so dates mentioned in the body are not
	// mistaken for publication dates.
	if titleSel != nil && titleSel.Length() > 0 {
		if date := findDateInMetaZone(titleSel, nil); date != "" {
			return date
		}
	}

	for _, selector := range []string{
		`meta[name="publishdate"]`, `meta[name="publish_date"]`,
		`meta[name="PubDate"]`, `meta[property="article:published_time"]`,
		".article-meta", ".article-info", ".news-info", ".publish-date", ".article-date",
	} {
		selection := doc.Find(selector).First()
		if selection.Length() == 0 {
			continue
		}
		for _, attr := range []string{"content", "datetime", "data-date"} {
			if date := normalizeSmeDate(selection.AttrOr(attr, "")); date != "" {
				return date
			}
		}
		if date := normalizeSmeDate(selection.Text()); date != "" {
			return date
		}
	}
	return ""
}
