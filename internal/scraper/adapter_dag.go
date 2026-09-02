package scraper

import (
	"fmt"
	stdhtml "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const dagCanonicalBaseURL = "https://dag.swjtu.edu.cn"

// The archive site uses one JSP list endpoint for all of its article-bearing
// columns.  The aliases are deliberately descriptive so the CLI does not
// expose opaque wbtree IDs, while dagRouteForSlug also accepts the IDs and
// common legacy aliases for callers that already know the source URLs.
var dagSubPages = []string{
	"work-rules",
	"laws",
	"school-rules",
	"stories",
	"collection-method",
	"special-collection",
	"donations",
	"party-branch",
	"notices",
	"news",
	"links",
	"hotline",
	"service-guide",
	"archives",
	"exhibition-video",
	"online-exhibition",
	"downloads",
}

type dagRoute struct {
	ID   string
	Name string
}

var dagRoutes = []dagRoute{
	{ID: "1111", Name: "工作制度"},
	{ID: "1121", Name: "法律法规"},
	{ID: "1131", Name: "学校制度"},
	{ID: "1151", Name: "交大故事"},
	{ID: "1161", Name: "征集办法"},
	{ID: "1171", Name: "专项征集"},
	{ID: "1181", Name: "捐赠信息"},
	{ID: "1191", Name: "支部建设"},
	{ID: "1211", Name: "通知公告"},
	{ID: "1221", Name: "新闻动态"},
	{ID: "1251", Name: "友情链接"},
	{ID: "1271", Name: "服务热线"},
	{ID: "1281", Name: "服务指南"},
	{ID: "1451", Name: "珍档荟萃"},
	{ID: "1461", Name: "专题展视频"},
	{ID: "1481", Name: "线上专题展"},
	{ID: "1051", Name: "下载专区"},
}

var dagRouteAliases = map[string]string{
	"1111": "1111", "work-rules": "1111", "gzzd": "1111", "工作制度": "1111", "gqjs/gzzd": "1111",
	"1121": "1121", "laws": "1121", "lfgz": "1121", "法律法规": "1121", "dalf/lfgz": "1121",
	"1131": "1131", "school-rules": "1131", "xxzd": "1131", "学校制度": "1131", "dalf/xxzd": "1131",
	"1151": "1151", "stories": "1151", "jdgs": "1151", "交大故事": "1151", "xswh/jdgs": "1151",
	"1161": "1161", "collection-method": "1161", "zjbf": "1161", "征集办法": "1161", "dazj/zjbf": "1161",
	"1171": "1171", "special-collection": "1171", "zxzj": "1171", "专项征集": "1171", "dazj/zxzj": "1171",
	"1181": "1181", "donations": "1181", "jzxx": "1181", "捐赠信息": "1181", "dazj/jzxx": "1181",
	"1191": "1191", "party-branch": "1191", "zbjs": "1191", "支部建设": "1191", "djgz/zbjs": "1191",
	"1211": "1211", "notices": "1211", "tzgg": "1211", "通知公告": "1211",
	"1221": "1221", "news": "1221", "xwdt": "1221", "新闻动态": "1221",
	"1251": "1251", "links": "1251", "yqlj": "1251", "友情链接": "1251",
	"1271": "1271", "hotline": "1271", "fwrx": "1271", "服务热线": "1271",
	"1281": "1281", "service-guide": "1281", "fwzn": "1281", "服务指南": "1281",
	"1451": "1451", "archives": "1451", "zdjc": "1451", "珍档荟萃": "1451", "xswh/zdjc": "1451",
	"1461": "1461", "exhibition-video": "1461", "ztzjsp": "1461", "专题展视频": "1461", "xswh/ztzjsp": "1461",
	"1481": "1481", "online-exhibition": "1481", "xszp": "1481", "线上专题展": "1481", "xswh/xszp": "1481",
	"1051": "1051", "downloads": "1051", "xzzq": "1051", "下载专区": "1051",
}

var (
	dagArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.(?:html?|shtml)$`)
	dagColumnPathRE  = regexp.MustCompile(`(?i)^/info/(\d+)/`)
	dagYearMonthRE   = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})`)
	dagFullDateRE    = regexp.MustCompile(`(20\d{2})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日?`)
	dagNumericDateRE = regexp.MustCompile(`(20\d{2})\s*[-/.]\s*(\d{1,2})\s*[-/.]\s*(\d{1,2})`)
	dagYearRE        = regexp.MustCompile(`(20\d{2})\s*年`)
	dagMonthDayRE    = regexp.MustCompile(`(\d{1,2})\s*月\s*(\d{1,2})\s*日`)
	dagDayRE         = regexp.MustCompile(`\d{1,2}`)
	dagTotalCountRE  = regexp.MustCompile(`共\s*(\d+)\s*条`)

	dagListStateMu sync.RWMutex
	dagListState   = make(map[string]model.NewsItem)
)

// dag.swjtu.edu.cn is currently served canonically over HTTPS.  It is kept
// in this adapter because the common site table predates the HTTPS migration.
func init() {
	if site := AllSites["dag"]; site != nil {
		site.BaseURL = dagCanonicalBaseURL
	}
	registerAdapter("dag", SiteAdapter{
		SubPages:     dagSubPages,
		ParseList:    parseDagNews,
		ParseArticle: parseDagArticle,
		ListPage:     dagListPage,
		FetchArticle: fetchDagArticle,
	})
}

// dagListPage handles the JSP endpoint and its query-string pagination.  A
// page number is logical and follows the source order: 1 is newest, total is
// oldest.  The generated files used by the shared Boda adapter are not used
// here because this site does not expose slug/{page}.htm pages.
func dagListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("dag: page must be positive")
	}

	route, ok := dagRouteForSlug(slug)
	if !ok {
		return nil, 0, fmt.Errorf("dag: unknown list %q", slug)
	}

	baseURL := dagBaseURL()
	if route.ID == "" {
		if page != 1 {
			return nil, 1, fmt.Errorf("dag: home page has no page %d", page)
		}
		body, err := fetch(dagHomeURL(baseURL))
		if err != nil {
			return nil, 0, err
		}
		items, err := parseDagNews(body, baseURL)
		return items, 1, err
	}

	listURL := dagColumnURL(baseURL, route.ID)
	body, err := fetch(listURL)
	if err != nil {
		return nil, 0, err
	}
	totalPages := dagTotalPages(body)
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		return nil, totalPages, fmt.Errorf("dag: page %d is out of range (total %d)", page, totalPages)
	}

	if page > 1 {
		pageURL, pageErr := dagPaginationURL(listURL, body, page, totalPages)
		if pageErr != nil {
			return nil, totalPages, pageErr
		}
		body, err = fetch(pageURL)
		if err != nil {
			return nil, totalPages, err
		}
	}

	items, err := parseDagNews(body, baseURL)
	if err != nil {
		return nil, totalPages, err
	}
	if len(items) == 0 && page > 1 {
		return nil, totalPages, fmt.Errorf("dag: list page %d/%d returned no article rows", page, totalPages)
	}
	for i := range items {
		if items[i].Type == "" {
			items[i].Type = route.Name
			dagRememberListItem(items[i])
		}
	}
	return items, totalPages, nil
}

func dagBaseURL() string {
	if site := AllSites["dag"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return dagCanonicalBaseURL
}

func dagHomeURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/"
}

func dagColumnURL(baseURL, columnID string) string {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/listcolumn.jsp")
	if err != nil {
		return strings.TrimRight(baseURL, "/") + "/listcolumn.jsp?urltype=tree.TreeTempUrl&wbtreeid=" + columnID
	}
	query := u.Query()
	query.Set("urltype", "tree.TreeTempUrl")
	query.Set("wbtreeid", columnID)
	u.RawQuery = query.Encode()
	return u.String()
}

func dagRouteForSlug(slug string) (dagRoute, bool) {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.EqualFold(strings.Trim(slug, "/"), "index") || strings.EqualFold(strings.Trim(slug, "/"), "home") {
		return dagRoute{}, true
	}

	if parsed, err := url.Parse(slug); err == nil {
		if id := parsed.Query().Get("wbtreeid"); id != "" {
			return dagRouteForID(id)
		}
	}

	normalized := strings.Trim(strings.ToLower(slug), "/")
	if strings.HasPrefix(normalized, "column/") {
		normalized = strings.TrimPrefix(normalized, "column/")
	}
	if id, ok := dagRouteAliases[normalized]; ok {
		return dagRouteForID(id)
	}
	return dagRoute{}, false
}

func dagRouteForID(id string) (dagRoute, bool) {
	for _, route := range dagRoutes {
		if route.ID == id {
			return route, true
		}
	}
	return dagRoute{}, false
}

func dagPaginationURL(baseURL string, markup []byte, page, totalPages int) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(markup)))
	if err != nil {
		return "", fmt.Errorf("dag: parse pagination html: %w", err)
	}

	var result string
	doc.Find(".p_pages a[href], .p_next a[href], .p_last a[href]").Each(func(_ int, sel *goquery.Selection) {
		if result != "" {
			return
		}
		href := stdhtml.UnescapeString(strings.TrimSpace(sel.AttrOr("href", "")))
		if href == "" {
			return
		}
		resolved := resolveURL(baseURL, href)
		u, parseErr := url.Parse(resolved)
		if parseErr != nil {
			return
		}
		query := u.Query()
		pageKey := dagPaginationKey(query, "p")
		if pageKey == "" {
			return
		}
		query.Set(pageKey, strconv.Itoa(page))
		u.RawQuery = query.Encode()
		u.Fragment = ""
		result = u.String()
	})
	if result != "" {
		return result, nil
	}

	// The current site uses these generated keys.  Keep a conservative
	// fallback for a page whose HTML contains a total but has lost its links.
	u, err := url.Parse(baseURL)
	if err == nil && totalPages > 1 {
		query := u.Query()
		query.Set("a1088771c", "20")
		query.Set("a1088771p", strconv.Itoa(page))
		query.Set("a1088771t", strconv.Itoa(totalPages))
		query.Set("urltype", "tree.TreeTempUrl")
		if columnID := query.Get("wbtreeid"); columnID != "" {
			u.RawQuery = query.Encode()
			return u.String(), nil
		}
	}
	return "", fmt.Errorf("dag: pagination link for page %d/%d not found", page, totalPages)
}

func dagPaginationKey(query url.Values, suffix string) string {
	for key, values := range query {
		if !strings.HasSuffix(strings.ToLower(key), suffix) || len(values) == 0 {
			continue
		}
		if _, err := strconv.Atoi(values[0]); err == nil {
			return key
		}
	}
	return ""
}

func dagTotalPages(markup []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(markup)))
	if err != nil {
		return 0
	}

	total := 0
	maxPage := 0
	pageSize := 20
	doc.Find(".p_pages a[href], .p_next a[href], .p_last a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := stdhtml.UnescapeString(strings.TrimSpace(sel.AttrOr("href", "")))
		resolved := resolveURL("https://dag.swjtu.edu.cn/", href)
		u, parseErr := url.Parse(resolved)
		if parseErr != nil {
			return
		}
		query := u.Query()
		if key := dagPaginationKey(query, "t"); key != "" {
			if value, parseErr := strconv.Atoi(query.Get(key)); parseErr == nil && value > total {
				total = value
			}
		}
		if key := dagPaginationKey(query, "p"); key != "" {
			if value, parseErr := strconv.Atoi(query.Get(key)); parseErr == nil && value > maxPage {
				maxPage = value
			}
		}
		if key := dagPaginationKey(query, "c"); key != "" {
			if value, parseErr := strconv.Atoi(query.Get(key)); parseErr == nil && value > 0 {
				pageSize = value
			}
		}
	})

	pagingText := strings.Join(strings.Fields(doc.Find(".p_t").First().Text()), "")
	countMatch := dagTotalCountRE.FindStringSubmatch(pagingText)
	if countMatch != nil {
		count, _ := strconv.Atoi(countMatch[1])
		calculated := (count + pageSize - 1) / pageSize
		if count == 0 {
			calculated = 1
		}
		if total == 0 {
			total = calculated
		}
	}
	if total > 0 {
		return total
	}
	if maxPage > 0 {
		return maxPage
	}
	return 1
}

func parseDagNews(markup []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(markup)))
	if err != nil {
		return nil, fmt.Errorf("parse dag list html: %w", err)
	}

	var items []model.NewsItem
	indexes := make(map[string]int)
	add := func(row *goquery.Selection, defaultType string) {
		anchor := row.Find("a.articlelist1_a_title[href]").First()
		if anchor.Length() == 0 {
			anchor = row.Find("a[href]").FilterFunction(func(_ int, sel *goquery.Selection) bool {
				_, ok := dagArticleURL(baseURL, sel.AttrOr("href", ""))
				return ok
			}).First()
		}
		if anchor.Length() == 0 {
			return
		}

		fullURL, ok := dagArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" {
			return
		}

		date, publishedAt := dagRowDate(row)
		typeName := strings.TrimSpace(defaultType)
		if typeName == "" {
			typeName = dagArticleTypeForURL(fullURL)
		}
		item := model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typeName,
			ParsedAt:    time.Now(),
		}
		if index, exists := indexes[fullURL]; exists {
			if items[index].Date == "" && item.Date != "" {
				items[index].Date = item.Date
			}
			if items[index].PublishedAt == "" && item.PublishedAt != "" {
				items[index].PublishedAt = item.PublishedAt
			}
			if items[index].Type == "" && item.Type != "" {
				items[index].Type = item.Type
			}
			dagRememberListItem(items[index])
			return
		}
		indexes[fullURL] = len(items)
		items = append(items, item)
		dagRememberListItem(item)
	}

	// List pages have a dedicated UL.  Restricting extraction to this UL is
	// important: its left navigation contains the hidden service article
	// /info/1431/4811.htm, which is not a list entry.
	list := doc.Find(".list-content ul.common-ul-right").First()
	if list.Length() > 0 {
		typeName := strings.TrimSpace(doc.Find(".navgate-right").First().Text())
		list.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
			add(row, typeName)
		})
	} else {
		// The homepage exposes the two tabs as .articlelist3_tr blocks.  The
		// date is split into year-month and day elements in this template.
		doc.Find(".articlelist3_tr").Each(func(_ int, row *goquery.Selection) {
			defaultType := ""
			if row.Closest(".xn-index-xwdtdiv").Length() > 0 {
				defaultType = "新闻动态"
			} else if row.Closest(".xn-index-tzggdiv").Length() > 0 {
				defaultType = "通知公告"
			}
			add(row, defaultType)
		})
	}

	if len(items) == 0 {
		if dagExplicitEmpty(doc) {
			return []model.NewsItem{}, nil
		}
		if reason := dagPageErrorKind(doc); reason != "" {
			return nil, fmt.Errorf("dag: %s", reason)
		}
		return nil, fmt.Errorf("dag: no article rows found")
	}
	return items, nil
}

func dagArticleURL(baseURL, href string) (string, bool) {
	href = stdhtml.UnescapeString(strings.TrimSpace(href))
	if href == "" {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, href))
	target, err := url.Parse(fullURL)
	if err != nil || target.Host == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(target.Host, base.Host) {
		return "", false
	}
	if !dagArticlePathRE.MatchString(target.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	target.Fragment = ""
	return cleanURL(target.String()), true
}

func dagRowDate(row *goquery.Selection) (string, string) {
	yearMonth := strings.TrimSpace(row.Find(".xn-index-tzggny").First().Text())
	day := strings.TrimSpace(row.Find(".xn-index-tzggm").First().Text())
	if yearMonth != "" && day != "" {
		if dayMatch := dagDayRE.FindString(day); dayMatch != "" {
			if date, publishedAt := dagDateFields(yearMonth + "." + dayMatch); date != "" {
				return date, publishedAt
			}
		}
	}

	for _, selector := range []string{"time[datetime]", "[data-date]", ".date", ".time", "span"} {
		var date, publishedAt string
		row.Find(selector).Each(func(_ int, sel *goquery.Selection) {
			if date != "" {
				return
			}
			for _, value := range []string{
				sel.AttrOr("datetime", ""),
				sel.AttrOr("data-date", ""),
				sel.Text(),
			} {
				date, publishedAt = dagDateFields(value)
				if date != "" {
					return
				}
			}
		})
		if date != "" {
			return date, publishedAt
		}
	}
	return "", ""
}

// dagListDate is retained as a small compatibility helper for the existing
// homepage fixture and tests.
func dagListDate(row *goquery.Selection) string {
	date, _ := dagRowDate(row)
	return date
}

func dagDateFields(text string) (string, string) {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return "", ""
	}
	if publishedAt := normalizePublishedAt(text); publishedAt != "" && len(publishedAt) >= 10 {
		if publishedAtHasClock(publishedAt) {
			return publishedAt[:10], publishedAt
		}
		return publishedAt[:10], ""
	}
	if match := dagFullDateRE.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	if match := dagNumericDateRE.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func parseDagArticle(markup []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(markup)))
	if err != nil {
		return nil, fmt.Errorf("parse dag article html: %w", err)
	}
	title := dagDetailTitle(doc)
	contentSel := dagArticleContent(doc)
	if title == "" || contentSel.Length() == 0 {
		if reason := dagPageErrorKind(doc); reason != "" {
			return nil, fmt.Errorf("dag: %s", reason)
		}
		return nil, fmt.Errorf("dag: response is not an article page")
	}

	article, err := parseBodaArticle(markup, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = title
	if article.Type == "" {
		article.Type = dagArticleType(doc)
	}

	if article.Date == "" {
		date, publishedAt := dagArticleDateFields(doc, title)
		article.Date = date
		if article.PublishedAt == "" {
			article.PublishedAt = publishedAt
		}
	}
	normalizeArticlePublishedAt(article)
	dagEnrichArticleResources(article, contentSel, baseURL)
	return article, nil
}

func fetchDagArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := dagBaseURL()
	if parsed, err := url.Parse(articleURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		baseURL = parsed.Scheme + "://" + parsed.Host
	}
	if _, ok := dagArticleURL(baseURL, articleURL); !ok {
		return nil, nil, fmt.Errorf("dag: URL is not an archive article: %s", articleURL)
	}
	body, err := fetch(articleURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseDagArticle(body, baseURL)
	if err != nil {
		return nil, nil, err
	}

	// The live detail template omits its publication metadata.  The list
	// date is the source's publication date, so use it when the caller reached
	// this article through a parsed list (and retain any detail time precision
	// when the detail itself supplied one).
	if cached, ok := dagCachedListItem(articleURL); ok {
		if cached.Date != "" {
			article.Date = cached.Date
		}
		if cached.PublishedAt != "" {
			article.PublishedAt = cached.PublishedAt
		}
		if cached.Type != "" {
			article.Type = cached.Type
		}
	}
	if article.Type == "" {
		article.Type = dagArticleTypeForURL(articleURL)
	}
	normalizeArticlePublishedAt(article)
	return article, body, nil
}

func dagDetailTitle(doc *goquery.Document) string {
	for _, selector := range []string{".article_title", ".article-title", ".detail-title"} {
		if title := strings.TrimSpace(doc.Find(selector).First().Text()); title != "" {
			return title
		}
	}
	return ""
}

func dagArticleContent(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		"#vsb_content_501 .v_news_content",
		".v_news_content",
		"#vsb_content_501",
		"[id^='vsb_content']",
		".news-content-wrapper",
		".content-main",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return doc.Find(".__dag_missing_content__").First()
}

func dagArticleType(doc *goquery.Document) string {
	typeName := ""
	doc.Find(".current-position a, .list-position a").Each(func(_ int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())
		if text != "" && text != "首页" && text != "正文" {
			typeName = text
		}
	})
	if typeName != "" {
		return typeName
	}
	return strings.TrimSpace(doc.Find(".navgate-right").First().Text())
}

func dagArticleTypeForURL(articleURL string) string {
	parsed, err := url.Parse(articleURL)
	if err != nil {
		return ""
	}
	match := dagColumnPathRE.FindStringSubmatch(parsed.Path)
	if match == nil {
		return ""
	}
	route, ok := dagRouteForID(match[1])
	if !ok {
		return ""
	}
	return route.Name
}

func dagArticleDateFields(doc *goquery.Document, title string) (string, string) {
	for _, selector := range []string{
		`meta[name="date"]`,
		`meta[name="publishdate"]`,
		`meta[name="publishDate"]`,
		`meta[name="publish_date"]`,
		`meta[property="article:published_time"]`,
		"time[datetime]",
		"[data-date]",
	} {
		selection := doc.Find(selector).First()
		if selection.Length() == 0 {
			continue
		}
		for _, value := range []string{
			selection.AttrOr("content", ""),
			selection.AttrOr("datetime", ""),
			selection.AttrOr("data-date", ""),
			selection.Text(),
		} {
			if date, publishedAt := dagDateFields(value); date != "" {
				return date, publishedAt
			}
		}
	}

	for _, candidate := range []string{
		strings.TrimSpace(doc.Find(".article_title").First().Text()),
		title,
		doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""),
		doc.Find("title").First().Text(),
	} {
		if date := dagDateFromText(candidate); date != "" {
			return date, ""
		}
	}
	return "", ""
}

func dagDateFromText(text string) string {
	if date, _ := dagDateFields(text); date != "" {
		return date
	}

	// A common archive headline is "2026年暑假值班表（7月20日-9月2日）".
	// The year and month/day are separated by the notice subject, so handle
	// that template without scanning the article body for unrelated dates.
	year := dagYearRE.FindStringSubmatch(text)
	monthDay := dagMonthDayRE.FindStringSubmatch(text)
	if year != nil && monthDay != nil {
		return normalizeDateParts(year[1], monthDay[1], monthDay[2])
	}
	return ""
}

func dagEnrichArticleResources(article *model.Article, contentSel *goquery.Selection, baseURL string) {
	if contentSel.Length() == 0 {
		return
	}

	imageSeen := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		imageSeen[image] = true
	}
	contentSel.Find("img").Each(func(_ int, sel *goquery.Selection) {
		src := strings.TrimSpace(sel.AttrOr("src", ""))
		if src == "" {
			src = strings.TrimSpace(sel.AttrOr("data-src", ""))
		}
		if src == "" {
			src = strings.TrimSpace(sel.AttrOr("orisrc", ""))
		}
		if src == "" {
			return
		}
		resolved := resolveURL(baseURL, src)
		if !imageSeen[resolved] {
			article.Images = append(article.Images, resolved)
			imageSeen[resolved] = true
		}
		sel.SetAttr("src", resolved)
	})

	attachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		attachments[attachment.URL] = true
	}
	contentSel.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if href == "" || !dagIsAttachmentLink(sel, href) {
			return
		}
		resolved := resolveURL(baseURL, href)
		if !dagSameHostURL(baseURL, resolved) {
			return
		}
		name := dagAttachmentName(sel, resolved)
		if name == "" {
			return
		}
		sel.SetAttr("href", resolved)
		if attachments[resolved] {
			return
		}
		attachments[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})

	if strings.TrimSpace(article.Content) == "" {
		article.Content = dagContentText(contentSel)
	}
	if contentHTML, err := contentSel.Html(); err == nil {
		article.ContentHTML = contentHTML
	}
}

func dagContentText(content *goquery.Selection) string {
	var parts []string
	content.Find("p, h1, h2, h3, h4, h5, h6, li, td, th, caption").Each(func(_ int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())
		if text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	return strings.TrimSpace(content.Text())
}

func dagIsAttachmentLink(sel *goquery.Selection, href string) bool {
	hrefLower := strings.ToLower(href)
	if strings.Contains(hrefLower, "download.jsp") || strings.Contains(hrefLower, "downloadattachurl") {
		return true
	}
	for _, value := range []string{
		strings.TrimSpace(sel.AttrOr("download", "")),
		strings.TrimSpace(sel.AttrOr("name", "")),
		strings.TrimSpace(sel.AttrOr("title", "")),
		strings.TrimSpace(sel.Text()),
	} {
		if dagHasFileExtension(value) {
			return true
		}
	}
	if sel.Parent().Find("img[src*='fileTypeImages']").Length() > 0 {
		return true
	}
	return strings.Contains(strings.ToLower(sel.Parent().Text()), "附件")
}

func dagHasFileExtension(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(value, ext) {
			return true
		}
	}
	return false
}

func dagAttachmentName(sel *goquery.Selection, resolvedURL string) string {
	for _, value := range []string{
		strings.TrimSpace(sel.AttrOr("download", "")),
		strings.TrimSpace(sel.AttrOr("name", "")),
		strings.TrimSpace(sel.Text()),
	} {
		if value != "" {
			return strings.Join(strings.Fields(value), " ")
		}
	}
	if parsed, err := url.Parse(resolvedURL); err == nil {
		return strings.TrimSpace(parsed.Path[strings.LastIndex(parsed.Path, "/")+1:])
	}
	return ""
}

func dagExplicitEmpty(doc *goquery.Document) bool {
	pagingText := strings.Join(strings.Fields(doc.Find(".p_t").First().Text()), "")
	match := dagTotalCountRE.FindStringSubmatch(pagingText)
	return match != nil && match[1] == "0"
}

func dagSameHostURL(baseURL, candidate string) bool {
	base, baseErr := url.Parse(baseURL)
	target, targetErr := url.Parse(candidate)
	return baseErr == nil && targetErr == nil && base.Host != "" &&
		target.Host != "" && strings.EqualFold(base.Host, target.Host)
}

func dagPageErrorKind(doc *goquery.Document) string {
	title := strings.ToLower(strings.TrimSpace(doc.Find("title").First().Text()))
	body := strings.ToLower(strings.Join(strings.Fields(doc.Find("body").Text()), " "))
	if strings.Contains(title, "验证码") || strings.Contains(body, "请输入验证码") || strings.Contains(body, "captcha") {
		return "captcha page"
	}
	if strings.Contains(title, "404") || strings.Contains(title, "错误") || strings.Contains(body, "页面不存在") || strings.Contains(body, "请求错误") {
		return "error page"
	}
	return ""
}

func dagRememberListItem(item model.NewsItem) {
	key := dagArticleKey(item.URL)
	if key == "" {
		return
	}
	dagListStateMu.Lock()
	defer dagListStateMu.Unlock()
	previous, exists := dagListState[key]
	if !exists {
		dagListState[key] = item
		return
	}
	if item.Date != "" {
		previous.Date = item.Date
	}
	if item.PublishedAt != "" {
		previous.PublishedAt = item.PublishedAt
	}
	if item.Type != "" {
		previous.Type = item.Type
	}
	if item.Title != "" {
		previous.Title = item.Title
	}
	dagListState[key] = previous
}

func dagCachedListItem(articleURL string) (model.NewsItem, bool) {
	key := dagArticleKey(articleURL)
	dagListStateMu.RLock()
	item, ok := dagListState[key]
	dagListStateMu.RUnlock()
	return item, ok
}

func dagArticleKey(articleURL string) string {
	parsed, err := url.Parse(articleURL)
	if err != nil || parsed.Path == "" {
		return ""
	}
	return strings.ToLower(parsed.Host) + strings.TrimRight(parsed.Path, "/") + "?" + parsed.RawQuery
}
