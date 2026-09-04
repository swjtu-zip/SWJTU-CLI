package scraper

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const smeCanonicalBaseURL = "https://sme.swjtu.edu.cn"

// smeSubPages is the set of article-bearing feeds found by walking the live
// homepage, hidden tabs, and every navigation leaf. Static profile/project
// pages are not feeds. The download/* routes are also intentionally omitted:
// they are file catalogs (many rows are direct .docx/.pdf links), not article
// lists, so treating them as NewsItem feeds would manufacture articles.
var smeSubPages = []string{
	"xwtz/xyxw",
	"xwtz/xshd",
	"dqgz/p/tzgg",
	"rcpy/bks/tzgg",
	"rcpy/yjs/tzgg",
	"kxyj/tzgg",
	"xsgz/tzgg",
	"xwtz/tmzl",
	"xwtz/qtgg",
	"xwtz/gszl",
	"dqgz/j/jwgz",
	"dqgz/g/ghdt",
	"dqgz/tx/gzdt",
	"xsgz/t/txdt",
	"xsgz/bjjs",
	"xsgz/gzzd",
	"xygz/byxc",
	"xygz/xydt",
	"zsjy/jyzd",
	"g/gnhz",
}

var smeColumnTypes = map[string]string{
	"1026": "本科生培养",
	"1043": "纪委工作",
	"1051": "团学动态",
	"1052": "班级建设",
	"1054": "规章制度",
	"1055": "学生工作",
	"1066": "就业指导",
	"1074": "工会工作",
	"1088": "校友动态",
	"1090": "毕业相册",
	"1094": "学院新闻",
	"1096": "科学研究",
	"1097": "本科生教学",
	"1098": "研究生教学",
	"1101": "推免专栏",
	"1541": "公示专栏",
	"1551": "多彩机械",
	"1621": "党政办公",
	"1791": "其他",
	"1811": "离退休工作",
	"1921": "国内合作",
	"1941": "学术活动",
}

// The mechanical engineering site uses the same Visual SiteBuilder article
// URLs as many other SWJTU sites, but its home page and category pages contain
// several different lists. The generic parser stops after the first matching
// list and consequently misses most articles (and can mistake a section link
// for an article). Keep the extraction scoped to SME article URLs here.
func init() {
	if site := AllSites["sme"]; site != nil {
		// The source redirects HTTP to HTTPS. Canonicalizing at the adapter
		// boundary keeps emitted image, attachment, and article URLs usable.
		site.BaseURL = smeCanonicalBaseURL
	}
	registerAdapter("sme", SiteAdapter{
		SubPages:     smeSubPages,
		ParseList:    parseSmeNews,
		ParseArticle: parseSmeArticle,
		PageURL:      smePageURL,
		TotalPages:   smeTotalPages,
		FetchArticle: smeFetchArticle,
	})
}

var (
	smeArticlePathPattern   = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	smeDateTimePattern      = regexp.MustCompile(`(?i)(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*(?:日|号)?\s*(?:[Tt]|\s+)\s*(\d{1,2})\s*[:：]\s*(\d{2})(?:\s*[:：]\s*(\d{2}))?`)
	smeSplitDateTimePattern = regexp.MustCompile(`(?i)(?:^|\s)(\d{1,2})\s+(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*(?:月)?\s+(\d{1,2})\s*[:：]\s*(\d{2})(?:\s*[:：]\s*(\d{2}))?`)
	smeFullDatePattern      = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?`)
	smeDayYearMonthPattern  = regexp.MustCompile(`(?i)(?:^|\s)(\d{1,2})\s+(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*(?:月)?`)
	smeYearMonthDayPattern  = regexp.MustCompile(`(?i)(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*(?:月)?\s+(\d{1,2})\s*日?`)
)

func smeCanonicalBase(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return baseURL
	}
	if strings.EqualFold(parsed.Hostname(), "sme.swjtu.edu.cn") {
		parsed.Scheme = "https"
		parsed.Host = "sme.swjtu.edu.cn"
		parsed.Path = ""
		parsed.RawPath = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return strings.TrimRight(parsed.String(), "/")
	}
	return baseURL
}

func smeCanonicalResourceURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Hostname(), "sme.swjtu.edu.cn") {
		return cleanURL(rawURL)
	}
	parsed.Scheme = "https"
	parsed.Host = "sme.swjtu.edu.cn"
	return cleanURL(parsed.String())
}

func smeResolveURL(baseURL, rawURL string) string {
	return smeCanonicalResourceURL(cleanURL(resolveURL(smeCanonicalBase(baseURL), rawURL)))
}

func smeKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "index" {
		return true
	}
	for _, known := range smeSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

// smePageURL maps the logical page number (1=newest) to the generated file
// number used by SME (1=oldest). For example, the live xyxw page advertises
// 177 pages and its logical page 2 is /xwtz/xyxw/176.htm.
func smePageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = smeCanonicalBase(baseURL)
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if baseURL == "" || slug == "" || page < 1 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	pageNumber := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, pageNumber, extension)
}

// smeTotalPagesFromHTML validates the response before trusting its pager.
// A source page without a recognized list shell is an error, not a one-page
// list: SME also has static pages and occasional login/WAF responses under
// otherwise plausible navigation URLs.
func smeTotalPagesFromHTML(raw []byte, slug string) (int, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("sme: parse page count for %s: %w", slug, err)
	}
	if strings.Trim(strings.TrimSpace(slug), "/") == "index" {
		if !smeIsHomepage(doc) {
			return 0, smeDocumentError(doc, "list")
		}
		return 1, nil
	}
	if err := smeDocumentError(doc, "list"); err != nil {
		return 0, err
	}
	if !smeIsListPage(doc) {
		return 0, fmt.Errorf("sme: %s is not an article list (static/resource page or unsupported shell)", slug)
	}
	if total := parseTotalPages(raw); total > 0 {
		return total, nil
	}
	if doc.Find(".p_pages, .p_t, .pagination, .paging").Length() > 0 || smeExplicitEmptyList(doc) {
		return 1, nil
	}
	return 0, fmt.Errorf("sme: %s has no recognized pagination", slug)
}

func smeTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if !smeKnownFeed(slug) {
		return 0, fmt.Errorf("sme: %q is not an audited article feed", slug)
	}
	if slug == "index" {
		body, err := fetch(smePageURL(smeCanonicalBaseURL, slug, 1, 0, ".htm"))
		if err != nil {
			return 0, err
		}
		return smeTotalPagesFromHTML(body, slug)
	}
	for _, extension := range []string{".htm", ".html"} {
		listURL := smePageURL(smeCanonicalBaseURL, slug, 1, 0, extension)
		body, err := fetch(listURL)
		if err != nil {
			continue
		}
		return smeTotalPagesFromHTML(body, slug)
	}
	return 0, fmt.Errorf("sme: unable to fetch audited list %s", slug)
}

func smeIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
	return pageType == "1" || doc.Find("#news, #ac, [id^='Tab']").Length() > 0
}

func smeIsListPage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find("main.list_main_box, .list_main_box, .news-list, .notice-list").Length() > 0
}

func smeIsArticlePage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	hasContainer := doc.Find("main.show_detail_box, main.article, .show_content_detail").Length() > 0
	hasHeading := doc.Find("h1.news_tit, .article-title, .news-title, .detail-title, .cont-tit h1, h1").Length() > 0
	return hasContainer && hasHeading
}

func smeExplicitEmptyList(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	text := strings.Join(strings.Fields(clone.Text()), "")
	for _, marker := range []string{"暂无信息", "暂无数据", "暂无文章", "没有相关内容", "共0条"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func smeResponseMarker(doc *goquery.Document) string {
	if doc == nil {
		return "empty response"
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	text := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []struct {
		value string
		name  string
	}{
		{"web application firewall", "WAF challenge"},
		{"access denied", "access-denied page"},
		{"forbidden", "forbidden page"},
		{"安全验证", "security challenge"},
		{"验证码", "verification page"},
		{"请登录", "login page"},
		{"统一身份认证", "login page"},
		{"单点登录", "login page"},
		{"请输入验证码", "verification page"},
		{"页面不存在", "404 page"},
		{"404错误", "404 page"},
		{"not found", "404 page"},
		{"请求过于频繁", "rate-limit page"},
	} {
		if strings.Contains(title, marker.value) || strings.Contains(text, marker.value) {
			return marker.name
		}
	}
	return ""
}

func smeDocumentError(doc *goquery.Document, kind string) error {
	if doc == nil {
		return fmt.Errorf("sme: %s response is empty", kind)
	}
	if (kind == "list" && (smeIsHomepage(doc) || smeIsListPage(doc))) ||
		(kind == "article" && smeIsArticlePage(doc)) {
		return nil
	}
	if marker := smeResponseMarker(doc); marker != "" {
		return fmt.Errorf("sme: %s response appears to be %s (possible WAF/login/SPA/error page)", kind, marker)
	}
	return nil
}

func parseSmeNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse SME list: %w", err)
	}
	if err := smeDocumentError(doc, "list"); err != nil {
		return nil, err
	}
	if !smeIsHomepage(doc) && !smeIsListPage(doc) {
		return nil, fmt.Errorf("sme: page is not a supported article list (static/resource/WAF/SPA shell)")
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
		fullURL := smeResolveURL(baseURL, href)
		if !isSmeArticleURL(fullURL, baseURL) || !isValidNewsURL(fullURL) {
			return
		}

		card := smeArticleCard(anchor)
		title := smeArticleTitle(anchor, card)
		if !isSmeNewsTitle(title) {
			return
		}
		date, publishedAt := smeArticleDateValuesFromList(anchor, card, baseURL)
		// List breadcrumbs on SME frequently collapse every feed to
		// “通知公告”. The column id in /info/<column>/<id>.htm is the
		// stronger, source-owned classification; homepage tab context is a
		// fallback for an unrecognised legacy column.
		typ := smeArticleTypeForURL(fullURL)
		if typ == "" {
			typ = smeArticleType(anchor, pageType)
		}
		addSmeItem(&items, seen, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	})

	if len(items) == 0 && doc.Find(".list_li_box").Length() > 0 {
		return nil, fmt.Errorf("sme: recognized list has rows but no local article links (resource catalog or malformed response)")
	}
	if len(items) == 0 && smeIsListPage(doc) &&
		doc.Find(".p_pages, .p_t, .pagination, .paging").Length() == 0 && !smeExplicitEmptyList(doc) {
		return nil, fmt.Errorf("sme: list shell has no rows or empty-list marker")
	}
	if smeIsHomepage(doc) && len(items) == 0 {
		return nil, fmt.Errorf("sme: homepage contains no local article entries")
	}
	return items, nil
}

func parseSmeArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("sme: parse article HTML: %w", err)
	}
	if err := smeDocumentError(doc, "article"); err != nil {
		return nil, err
	}
	if !smeIsArticlePage(doc) {
		return nil, fmt.Errorf("sme: page is not a supported article detail (WAF/login/SPA/template shell)")
	}
	titleSel := smeArticleTitleSelection(doc)
	if titleSel.Length() == 0 || !isSmeArticleHeadline(cleanSmeTitle(titleSel.Text())) {
		return nil, fmt.Errorf("sme: article detail has no usable headline")
	}

	canonicalBase := smeCanonicalBase(baseURL)
	article, err := parseBodaArticle(html, canonicalBase)
	if err != nil {
		return nil, err
	}

	// parseBodaArticle already handles the common SME article template. The
	// wrapper is deliberately site-specific so that its dotted/Chinese date
	// form is normalized even when the generic source block is used.
	if normalized := normalizeSmeDate(article.Date); normalized != "" {
		article.Date = normalized
	}

	if article.Title == "" && titleSel.Length() > 0 {
		article.Title = strings.TrimSpace(titleSel.Text())
	}
	// parseBodaArticle has a deliberately broad generic precision fallback.
	// It is useful for many sites, but a SME notice body can contain a dated
	// deadline or event time. Keep only precision found in the detail header's
	// own metadata, never a time inferred from the body.
	metaDate, metaPublishedAt := smeArticleMetaValues(doc, titleSel)
	article.PublishedAt = ""
	if metaDate != "" {
		article.Date = metaDate
	}
	if metaPublishedAt != "" {
		article.PublishedAt = metaPublishedAt
		article.Date = metaPublishedAt[:10]
	}
	if article.Date == "" {
		article.Date = smeArticleMetaDate(doc, titleSel)
	}
	if article.Type == "" {
		article.Type = smeBreadcrumbType(doc)
	}
	appendSmeAttachments(article, doc, canonicalBase)
	canonicalizeSmeArticleResources(article)
	normalizeArticlePublishedAt(article)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("sme: article detail has no body text or downloadable resources")
	}
	return article, nil
}

func smeFetchArticle(articleURL string) (*model.Article, []byte, error) {
	canonicalURL := smeCanonicalResourceURL(articleURL)
	if !isSmeArticleURL(canonicalURL, smeCanonicalBaseURL) {
		return nil, nil, fmt.Errorf("sme: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonicalURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseSmeArticle(body, smeCanonicalBaseURL)
	if err != nil {
		return nil, nil, err
	}
	if typ := smeArticleTypeForURL(canonicalURL); typ != "" {
		article.Type = typ
	}
	return article, body, nil
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

func isSmeArticleHeadline(title string) bool {
	if title == "" || utf8.RuneCountInString(title) > 300 {
		return false
	}
	return !strings.HasPrefix(title, "登录") && !strings.HasPrefix(title, "验证码") &&
		!strings.HasPrefix(title, "页面不存在")
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
	date, _ := smeArticleDateValuesFromList(anchor, card, baseURL)
	return date
}

func smeArticleDateValuesFromList(anchor, card *goquery.Selection, baseURL string) (string, string) {
	date, publishedAt := "", ""
	merge := func(candidateDate, candidatePublishedAt string) {
		if candidatePublishedAt != "" {
			publishedAt = candidatePublishedAt
			date = candidatePublishedAt[:10]
		}
		if date == "" && candidateDate != "" {
			date = candidateDate
		}
	}

	for _, attr := range []string{"datetime", "data-date", "data-time"} {
		candidateDate, candidatePublishedAt := smeDateValues(anchor.AttrOr(attr, ""))
		merge(candidateDate, candidatePublishedAt)
	}
	// SME's home-page tabs put the date in a span inside the anchor. Check
	// the anchor before its card because an academic carousel item can hold
	// two articles with different dates.
	candidateDate, candidatePublishedAt := smeDateValuesFromSelection(anchor)
	merge(candidateDate, candidatePublishedAt)
	if card != nil && card.Length() > 0 {
		candidateDate, candidatePublishedAt = smeDateValuesFromSelection(card)
		merge(candidateDate, candidatePublishedAt)
	}

	// A few generated pages put the date beside the card rather than inside
	// it. Only walk through ancestors that still contain one unique article
	// URL; this prevents taking the date belonging to the next list item.
	depth := 0
	anchor.Parents().EachWithBreak(func(_ int, parent *goquery.Selection) bool {
		depth++
		if depth > 5 || smeArticleLinkCount(parent, baseURL) > 1 {
			return false
		}
		beforeDate, beforePublishedAt := date, publishedAt
		candidateDate, candidatePublishedAt := smeDateValuesFromSelection(parent)
		merge(candidateDate, candidatePublishedAt)
		return date == beforeDate && publishedAt == beforePublishedAt
	})
	return date, publishedAt
}

var smeDateSelectors = `time, [datetime], [data-date], [data-time], .date, .time, span.flex-none, [class*="date"], [class*="Date"], [class*="time"], [class*="Time"]`

func smeDateFromSelection(sel *goquery.Selection) string {
	date, _ := smeDateValuesFromSelection(sel)
	return date
}

func smeDateValuesFromSelection(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
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
	date, publishedAt := "", ""
	for _, part := range parts {
		if publishedAt == "" {
			publishedAt = normalizeSmePublishedAt(part)
		}
		if date == "" {
			date = normalizeSmeDate(part)
		}
	}
	joined := strings.Join(parts, " ")
	if publishedAt == "" {
		publishedAt = normalizeSmePublishedAt(joined)
	}
	if date == "" {
		date = normalizeSmeDate(joined)
	}
	if publishedAt != "" {
		date = publishedAt[:10]
	}
	return date, publishedAt
}

func smeArticleLinkCount(sel *goquery.Selection, baseURL string) int {
	seen := make(map[string]bool)
	sel.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href, _ := anchor.Attr("href")
		fullURL := smeResolveURL(baseURL, href)
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
		if (*items)[index].PublishedAt == "" && item.PublishedAt != "" {
			(*items)[index].PublishedAt = item.PublishedAt
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

func smeArticleTypeForURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "sme.swjtu.edu.cn") {
		return ""
	}
	match := regexp.MustCompile(`(?i)^/info/(\d+)/\d+\.html?$`).FindStringSubmatch(parsed.Path)
	if match == nil {
		return ""
	}
	return smeColumnTypes[match[1]]
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
		resolved := smeResolveURL(baseURL, href)
		if seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func canonicalizeSmeArticleResources(article *model.Article) {
	if article == nil {
		return
	}
	for i, imageURL := range article.Images {
		article.Images[i] = smeCanonicalResourceURL(imageURL)
	}
	for i, attachment := range article.Attachments {
		article.Attachments[i].URL = smeCanonicalResourceURL(attachment.URL)
	}
	article.PrevURL = smeCanonicalResourceURL(article.PrevURL)
	article.NextURL = smeCanonicalResourceURL(article.NextURL)
	article.ContentHTML = strings.ReplaceAll(article.ContentHTML, "http://sme.swjtu.edu.cn", smeCanonicalBaseURL)
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

func smeDateValues(value string) (string, string) {
	date := normalizeSmeDate(value)
	publishedAt := normalizeSmePublishedAt(value)
	if publishedAt != "" {
		date = publishedAt[:10]
	}
	return date, publishedAt
}

// normalizeSmePublishedAt deliberately returns an empty string for a
// date-only value. The shared normalizer treats a date as midnight, which is
// useful for legacy callers but would falsely claim that SME exposed a time.
func normalizeSmePublishedAt(value string) string {
	value = htmlstd.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	if normalized := normalizePublishedAt(value); publishedAtHasClock(normalized) {
		return normalized
	}
	if match := smeDateTimePattern.FindStringSubmatch(value); match != nil {
		second := match[6]
		if second == "" {
			second = "00"
		}
		return formatSmePublished(match[1], match[2], match[3], match[4], match[5], second)
	}
	if match := smeSplitDateTimePattern.FindStringSubmatch(value); match != nil {
		second := match[6]
		if second == "" {
			second = "00"
		}
		return formatSmePublished(match[2], match[3], match[1], match[4], match[5], second)
	}
	return ""
}

func formatSmePublished(year, month, day, hour, minute, second string) string {
	date := normalizeDateParts(year, month, day)
	if date == "" {
		return ""
	}
	h, errH := strconv.Atoi(hour)
	m, errM := strconv.Atoi(minute)
	s, errS := strconv.Atoi(second)
	if errH != nil || errM != nil || errS != nil || h < 0 || h > 23 || m < 0 || m > 59 || s < 0 || s > 59 {
		return ""
	}
	return fmt.Sprintf("%s %02d:%02d:%02d", date, h, m, s)
}

func smeArticleMetaValues(doc *goquery.Document, titleSel *goquery.Selection) (string, string) {
	if doc == nil {
		return "", ""
	}
	consume := func(selection *goquery.Selection, requireLabel bool) (string, string, bool) {
		if selection == nil || selection.Length() == 0 {
			return "", "", false
		}
		for _, attr := range []string{"content", "datetime", "data-date", "data-time"} {
			if value := strings.TrimSpace(selection.AttrOr(attr, "")); value != "" {
				if date, publishedAt := smeDateValues(value); date != "" {
					return date, publishedAt, true
				}
			}
		}
		value := strings.TrimSpace(flattenedText(selection))
		if value == "" {
			value = strings.TrimSpace(selection.Text())
		}
		if requireLabel && !smePublicationLabel(value) {
			return "", "", false
		}
		if date, publishedAt := smeDateValues(value); date != "" {
			return date, publishedAt, true
		}
		return "", "", false
	}

	for _, selector := range []string{
		`meta[name="publishdate"]`, `meta[name="publish_date"]`,
		`meta[name="PubDate"]`, `meta[property="article:published_time"]`,
		".show_detail_box > .text-gray-500", ".show_detail_box > .article-meta",
		".show_detail_box > .article-info", ".article-meta", ".article-info",
		".news-info", ".publish-date", ".article-date", ".news_source", ".top-info",
	} {
		if date, publishedAt, ok := consume(doc.Find(selector).First(), false); ok {
			return date, publishedAt
		}
	}

	// The current SME template has no stable metadata class: the header line
	// is simply a direct child of show_detail_box after the h1/h2. Walk only
	// that pre-body header region, so event/deadline times in .v_news_content
	// can never become publication times.
	container := &goquery.Selection{}
	if titleSel != nil {
		container = titleSel.Closest("main.show_detail_box, main.article").First()
	}
	if container.Length() > 0 {
		var resultDate, resultPublishedAt string
		container.Children().EachWithBreak(func(_ int, child *goquery.Selection) bool {
			if child.Is(".show_content_detail, .v_news_content, [id^='vsb_content']") {
				return false
			}
			value := strings.TrimSpace(flattenedText(child))
			class := strings.ToLower(child.AttrOr("class", ""))
			hasDateHint := smePublicationLabel(value) || strings.Contains(class, "date") || strings.Contains(class, "time") || strings.Contains(class, "publish")
			if !hasDateHint {
				return true
			}
			if date, publishedAt, ok := consume(child, true); ok {
				resultDate, resultPublishedAt = date, publishedAt
				return false
			}
			return true
		})
		if resultDate != "" || resultPublishedAt != "" {
			return resultDate, resultPublishedAt
		}
	}
	return "", ""
}

func smePublicationLabel(value string) bool {
	return strings.Contains(value, "发布时间") || strings.Contains(value, "发布日期") ||
		strings.Contains(value, "发表日期") || strings.Contains(value, "发表时间") ||
		strings.Contains(value, "日期:") || strings.Contains(value, "日期：")
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
	if date, _ := smeArticleMetaValues(doc, titleSel); date != "" {
		return date
	}

	for _, selector := range []string{
		`meta[name="publishdate"]`, `meta[name="publish_date"]`,
		`meta[name="PubDate"]`, `meta[property="article:published_time"]`,
		".show_detail_box > .text-gray-500", ".show_detail_box > .article-meta",
		".show_detail_box > .article-info", ".article-meta", ".article-info",
		".news-info", ".publish-date", ".article-date", ".news_source", ".top-info",
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
