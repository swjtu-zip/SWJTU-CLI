package scraper

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// sist is a Vue application rather than a Visual SiteBuilder/static HTML
// site.  The browser loads the list and article data with POST requests after
// the initial document has loaded.  Keep this adapter separate from the
// Visual SiteBuilder parser so the SPA shell cannot be mistaken for a list.
const (
	sistSiteType = "NEd5n92EMIpyyBslaNqsRgE"
	sistAPIBase  = "https://sist.swjtu.edu.cn"

	sistNewsNavID         = "DpHktHeXJp3MX-FZoZdjz3w"
	sistNoticeNavID       = "Ag_9Jx2oC93JQcGt0VS0d4I"
	sistUndergraduateID   = "KW_T7dJ2lt5sC-RJZSnPtk0"
	sistUndergraduateRoot = "9JpfFAQClhKDd-As8MXBYS8"
	sistPostgraduateID    = "1yaM_T4qiBsFULvLXHbMflQ"
	sistPostgraduateRoot  = "bDdH8HttGDwUH0nMDrXEiDA"
)

var (
	// These errors are intentionally returned for the checked-in SPA shell.
	// A zero-item, nil-error result would make an unavailable dynamic feed look
	// like a successful empty feed.
	errSistDynamicPage = errors.New("sist: received Vue SPA shell; data is loaded by the site's POST API")
	errSistNoArticles  = errors.New("sist: no real article links or API rows found")

	sistDateRE = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
)

var sistNavigationTitles = map[string]bool{
	"首页":   true,
	"学院首页": true,
	"新闻中心": true,
	"通知公告": true,
	"更多":   true,
}

// The values below are the fields returned by /dev-api/web/articlelist and
// /dev-api/web/node.  Content is also returned by those endpoints, although
// it is not needed while parsing a list.
type sistAPIItem struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	PublishTime     string     `json:"publishTime"`
	CreateTime      string     `json:"createTime"`
	Content         string     `json:"content"`
	Description     string     `json:"description"`
	PublishUserName string     `json:"publishUserName"`
	SiteType        string     `json:"siteType"`
	Type            sistString `json:"type"`
	NavID           string     `json:"navId"`
	URL             string     `json:"url"`
	Link            string     `json:"link"`
}

// sistString handles the site's mixed JSON representation: article payloads
// currently return type as null, while /web/info menu nodes return a number.
type sistString string

func (s *sistString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || len(data) == 0 {
		*s = ""
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = sistString(value)
		return nil
	}
	var value json.Number
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = sistString(value.String())
	return nil
}

type sistListEnvelope struct {
	Code  int             `json:"code"`
	Total int             `json:"total"`
	Rows  []sistAPIItem   `json:"rows"`
	Data  json.RawMessage `json:"data"`
}

func init() {
	registerAdapter("sist", SiteAdapter{
		SubPages:     sistSubPages(),
		ParseList:    parseSistNews,
		ParseArticle: parseSistArticle,
		PageURL:      sistPageURL,
		ListPage:     sistListPage,
		FetchArticle: sistFetchArticle,
	})
}

// sistListPage is the live-data path for this Vue site. Its public page URLs
// are only shells; the node API is the source of truth for both rows and the
// total page count.
func sistListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("sist: requested page must be positive: %d", page)
	}
	route := sistRouteForSlug(slug)
	if route.navID == "" {
		route = sistRouteForSlug("news")
	}
	pageSize := sistPageSize(route.slug)
	payload := map[string]any{
		"id":       route.navID,
		"pageNum":  page,
		"pageSize": pageSize,
		"title":    "",
		"sitetype": sistSiteType,
		"lang":     "cn",
	}
	body, err := sistPostJSON("/dev-api/web/node", payload)
	if err != nil {
		return nil, 0, err
	}
	rows, total, recognized, err := decodeSistList(body)
	if err != nil {
		return nil, 0, err
	}
	if !recognized {
		return nil, 0, fmt.Errorf("sist: node API returned a non-JSON response")
	}
	if total == 0 && len(rows) == 0 {
		return []model.NewsItem{}, 0, nil
	}
	items, err := sistNewsItems(rows, sistAPIBase, route.navID)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		total = len(rows)
	}
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		if page > 1 {
			return nil, 0, fmt.Errorf("sist: requested page %d but %s is empty", page, slug)
		}
		return items, 0, nil
	}
	if page > totalPages {
		return nil, 0, fmt.Errorf("sist: requested page %d but %s has %d pages", page, slug, totalPages)
	}
	return items, totalPages, nil
}

// sistPageSize follows the site's isSimple=1 tabs. Those tabs render ten
// rows per request; ordinary node pages render five.
func sistPageSize(slug string) int {
	switch slug {
	case "party/dynamics", "undergraduate/regulations", "undergraduate/admission", "undergraduate/recommendation", "students/notice":
		return 10
	default:
		return 5
	}
}

func sistFetchArticle(articleURL string) (*model.Article, []byte, error) {
	parsed, err := url.Parse(articleURL)
	if err != nil || parsed.Query().Get("id") == "" {
		return nil, nil, fmt.Errorf("sist: article URL has no id: %q", articleURL)
	}
	body, err := sistPostJSON("/dev-api/web/article", map[string]any{
		"id":       parsed.Query().Get("id"),
		"sitetype": sistSiteType,
		"lang":     "cn",
	})
	if err != nil {
		return nil, nil, err
	}
	article, err := parseSistArticle(body, sistAPIBase)
	if err != nil {
		return nil, body, err
	}
	return article, body, nil
}

func sistPostJSON(endpoint string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("sist: encode %s request: %w", endpoint, err)
	}
	req, err := http.NewRequest(http.MethodPost, sistAPIBase+endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("sist: create %s request: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; swjtu-cli/1.0)")
	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sist: POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("sist: POST %s: status %d", endpoint, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sist: read %s response: %w", endpoint, err)
	}
	return body, nil
}

// sistPageURL returns an actual browser route for the corresponding column.
// The route itself is a SPA shell; the API payload is parsed when a caller
// supplies it to parseSistNews.  The shared BodaSite fetcher only performs GET
// requests, so silently pretending that this route is a static list would be
// incorrect.  The browser uses POST /dev-api/web/node for tab pages and
// POST /dev-api/web/articlelist for homepage blocks; page and totalPages are
// intentionally not encoded because the browser sends pageNum/pageSize in the
// POST body instead of creating pagination URLs.
func sistPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	base := strings.TrimRight(baseURL, "/")
	section := sistSectionForSlug(slug)
	if section.navID == "" {
		return base + "/home?lang=cn"
	}
	if !section.nested {
		return base + "/pc/" + section.navID + "?type=2&lang=cn"
	}
	return base + "/pc/" + section.rootID + "/" + section.navID + "?lang=cn"
}

type sistSection struct {
	navID  string
	rootID string
	nested bool
}

type sistFeedRoute struct {
	slug   string
	navID  string
	rootID string
	nested bool
}

// The menu returned by /dev-api/web/getnavall contains 35 type=2 nodes.  The
// internal-notice node is deliberately omitted: the menu labels it as not
// displayed on the public website.  Empty public nodes remain registered so
// callers can distinguish an empty API feed from a missing column.
var sistFeedRoutes = []sistFeedRoute{
	{slug: "notice", navID: sistNoticeNavID},
	{slug: "undergraduate", navID: sistUndergraduateID, rootID: sistUndergraduateRoot, nested: true},
	{slug: "postgraduate", navID: sistPostgraduateID, rootID: sistPostgraduateRoot, nested: true},
	{slug: "news", navID: sistNewsNavID},

	{slug: "party/dynamics", navID: "N8YFxHPHwy6PyHt7J-gIDCU"},
	{slug: "party/development", navID: "VEnOxwfnlEEFwv4OMm-TeSY"},
	{slug: "party/discipline", navID: "_q1NEkqggtnR4zz6zSz7_Cc"},
	{slug: "party/union-house", navID: "cvAfN6_rw-3ji0nNsvnaLSg"},
	{slug: "party/youth", navID: "CFDXsIU66Lcy9ry1c9hXeyk"},

	{slug: "undergraduate/graduation", navID: "1GPPT6YeIhXPHpMRyxKjlE4", rootID: sistUndergraduateRoot, nested: true},
	{slug: "undergraduate/regulations", navID: "3cnMJC1M_RyqJirEx3cqck8", rootID: sistUndergraduateRoot, nested: true},
	{slug: "undergraduate/practice", navID: "exZQqY900wj-Vh0NbhUGr1E", rootID: sistUndergraduateRoot, nested: true},
	{slug: "undergraduate/admission", navID: "VqQyAR3BDR_natN7fJLVI1I", rootID: sistUndergraduateRoot, nested: true},
	{slug: "undergraduate/recommendation", navID: "DGhkBE8BOPDltGurtKQ8Za0", rootID: sistUndergraduateRoot, nested: true},

	{slug: "postgraduate/dissertation", navID: "m3HLR2Uvz3WM9kUxGii6BVU", rootID: sistPostgraduateRoot, nested: true},
	{slug: "postgraduate/training", navID: "8c9GXgtIppUE4jks9lyZLlY", rootID: sistPostgraduateRoot, nested: true},
	{slug: "postgraduate/admission", navID: "AjbijiMwrUQ3bntVzWSB6lc", rootID: sistPostgraduateRoot, nested: true},

	{slug: "research/notice", navID: "b4Lm82Ct_eaF2u5XDABv0TM"},
	{slug: "research/academic", navID: "47sdOe8NQs0B4EHmM-wFWjQ"},
	{slug: "discipline/progress", navID: "9rCv9KyTwKNs2lSyzNpe7To"},

	{slug: "students/notice", navID: "4DuvOht7AfSyo-pr_HYN-Dw"},
	{slug: "students/news", navID: "suwuThw3-mTtfJCA3bWDzD0"},
	{slug: "students/style", navID: "MN33D4ukaVwhPOZ9gXv5QD4"},
	{slug: "students/competition/3s", navID: "2rZoyvT_ARP4kqdWMov1H1s", rootID: "Nn3neKHRyK4hLbkwL2Wv1j8", nested: true},
	{slug: "students/competition/coaches", navID: "DVtNt7TrXiwWffpud9sYcFw", rootID: "Nn3neKHRyK4hLbkwL2Wv1j8", nested: true},
	{slug: "students/competition/info", navID: "S1iNDeRnJkb8Hi8x1GSUJV0", rootID: "Nn3neKHRyK4hLbkwL2Wv1j8", nested: true},
	{slug: "students/employment/undergraduate", navID: "v7keDfpeIi5QV0zyZVyBDIM", rootID: "reDQofaPap1M0FL500jtfkA", nested: true},
	{slug: "students/employment/postgraduate", navID: "lhyuv1nN3wOrHouwYS22XYQ", rootID: "reDQofaPap1M0FL500jtfkA", nested: true},
	{slug: "students/employment/internship", navID: "BtOsEr45WDpD2yx_NaHavYU", rootID: "reDQofaPap1M0FL500jtfkA", nested: true},

	{slug: "alumni/news", navID: "oDGuDtTbvq0YAai18sKeZEI"},
	{slug: "alumni/outstanding", navID: "8jsXt9XZE8Hp93S6VylQ_0Y"},
	{slug: "alumni/graduation", navID: "ElVZyHcHnaBCNGvw1Hdc_Uc"},
	{slug: "service/guide", navID: "8hrSi3EuhAMNN4bxiLuum2Y", rootID: "YOY5_NN6UXhITjbPgdOCTEk", nested: true},
	{slug: "service/faq", navID: "hHpLiDXEz-uG68cnzFTNJWc", rootID: "YOY5_NN6UXhITjbPgdOCTEk", nested: true},
}

func sistSubPages() []string {
	pages := make([]string, 0, len(sistFeedRoutes))
	for _, route := range sistFeedRoutes {
		pages = append(pages, route.slug)
	}
	return pages
}

func sistRouteForSlug(slug string) sistFeedRoute {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(slug), "/"))
	switch normalized {
	case "", "index", "news-center", "新闻中心":
		normalized = "news"
	case "tzgg", "通知公告":
		normalized = "notice"
	case "undergrad", "本科", "本科教育":
		normalized = "undergraduate"
	case "graduate", "研究生", "研究生教育":
		normalized = "postgraduate"
	}
	for _, route := range sistFeedRoutes {
		if normalized == strings.ToLower(route.slug) || slug == route.navID {
			return route
		}
	}
	return sistFeedRoute{}
}

func sistSectionForSlug(slug string) sistSection {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(slug), "/"))
	if normalized == "" || normalized == "index" {
		return sistSection{}
	}
	route := sistRouteForSlug(slug)
	if route.navID != "" {
		return sistSection{navID: route.navID, rootID: route.rootID, nested: route.nested}
	}
	// Opaque navigation IDs are useful to callers that already obtained the
	// site's menu.  Do not invent a route for an unknown slug.
	return sistSection{}
}

// parseSistNews accepts either a captured sist API response or a rendered
// HTML list containing real /pc/...?...id=... links.  The checked-in
// testdata/sist.html is only the SPA shell and therefore returns an error.
func parseSistNews(body []byte, baseURL string) ([]model.NewsItem, error) {
	return parseSistNewsForNav(body, baseURL, sistNewsNavID)
}

func parseSistNewsForNav(body []byte, baseURL, defaultNavID string) ([]model.NewsItem, error) {
	if rows, _, recognized, err := decodeSistList(body); recognized {
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			// An API-confirmed empty column is different from the Vue shell.
			return []model.NewsItem{}, nil
		}
		return sistNewsItems(rows, baseURL, defaultNavID)
	}

	items, err := parseSistRenderedList(body, baseURL)
	if err == nil {
		return items, nil
	}
	if isSistShell(body) {
		return nil, fmt.Errorf("%w: /dev-api/web/node or /dev-api/web/articlelist (sitetype %s)", errSistDynamicPage, sistSiteType)
	}
	return nil, err
}

func decodeSistList(body []byte) ([]sistAPIItem, int, bool, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, 0, false, nil
	}

	if trimmed[0] == '[' {
		var rows []sistAPIItem
		if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
			return nil, 0, true, fmt.Errorf("sist: decode list JSON: %w", err)
		}
		return rows, len(rows), true, nil
	}

	var envelope sistListEnvelope
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return nil, 0, true, fmt.Errorf("sist: decode list JSON: %w", err)
	}
	if envelope.Code != 0 && envelope.Code != 200 && envelope.Code != 199 {
		return nil, 0, true, fmt.Errorf("sist: list API returned code %d", envelope.Code)
	}
	if envelope.Rows != nil {
		return envelope.Rows, envelope.Total, true, nil
	}
	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		var nested struct {
			Total int             `json:"total"`
			Rows  []sistAPIItem   `json:"rows"`
			Data  json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(envelope.Data, &nested); err == nil && nested.Rows != nil {
			total := envelope.Total
			if total == 0 {
				total = nested.Total
			}
			return nested.Rows, total, true, nil
		}
		var rows []sistAPIItem
		if err := json.Unmarshal(envelope.Data, &rows); err == nil {
			return rows, len(rows), true, nil
		}
	}
	if envelope.Code == 199 {
		return nil, envelope.Total, true, fmt.Errorf("%w: list API reported no rows", errSistNoArticles)
	}
	return nil, envelope.Total, true, fmt.Errorf("%w: JSON did not contain rows", errSistNoArticles)
}

func sistNewsItems(rows []sistAPIItem, baseURL, navID string) ([]model.NewsItem, error) {
	items := make([]model.NewsItem, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		title := cleanTitle(row.Title)
		if row.ID == "" || title == "" || len(title) > 200 || !isSistArticleTitle(title) {
			continue
		}
		rowNavID := strings.TrimSpace(row.NavID)
		if rowNavID == "" {
			rowNavID = navID
		}
		itemURL := sistItemURL(row, baseURL, rowNavID)
		if itemURL == "" || seen[itemURL] || !isSistArticleLink(itemURL, baseURL) {
			continue
		}
		seen[itemURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         itemURL,
			Date:        normalizeSistDate(row.PublishTime),
			PublishedAt: normalizePublishedAt(row.PublishTime),
			Type:        strings.TrimSpace(string(row.Type)),
			ParsedAt:    time.Now(),
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: API rows had no usable id/title", errSistNoArticles)
	}
	return items, nil
}

func sistItemURL(row sistAPIItem, baseURL, navID string) string {
	for _, candidate := range []string{row.URL, row.Link} {
		if candidate == "" {
			continue
		}
		resolved := cleanURL(resolveURL(baseURL, candidate))
		if isSistArticleLink(resolved, baseURL) {
			return resolved
		}
	}
	return sistArticleURL(baseURL, navID, row.ID)
}

func sistArticleURL(baseURL, navID, id string) string {
	if id == "" || navID == "" {
		return ""
	}
	section := sistSectionForNavID(navID)
	path := "/pc/" + navID
	if section.nested {
		path = "/pc/" + section.rootID + "/" + navID
	}
	query := "?id=" + url.QueryEscape(id)
	if !section.nested {
		query += "&type=2"
	}
	query += "&lang=cn"
	return resolveURL(baseURL, path+query)
}

func sistSectionForNavID(navID string) sistSection {
	for _, route := range sistFeedRoutes {
		if route.navID == navID {
			return sistSection{navID: route.navID, rootID: route.rootID, nested: route.nested}
		}
	}
	return sistSection{navID: navID}
}

func parseSistRenderedList(body []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("sist: parse rendered list: %w", err)
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		if isInNavArea(sel) {
			return
		}
		href, _ := sel.Attr("href")
		fullURL := cleanURL(resolveURL(baseURL, href))
		if !isSistArticleLink(fullURL, baseURL) || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(sel.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(sel.Find(".article_name, .article-title, .title").First().Text())
		}
		if title == "" {
			title = strings.TrimSpace(sel.Text())
		}
		title = cleanTitle(title)
		if title == "" || len(title) > 200 || !isSistArticleTitle(title) {
			return
		}
		dateText := strings.TrimSpace(sel.Find("time, .article_time, .date, .article-date").First().Text())
		if dateText == "" {
			dateText = strings.TrimSpace(sel.Parent().Find("time, .article_time, .date, .article-date").First().Text())
		}
		if dateText == "" {
			dateText = strings.TrimSpace(sel.Closest("li, .article, .article-item, .list-item, .tw").Find("time, .article_time, .date, .article-date").First().Text())
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        normalizeSistDate(dateText),
			PublishedAt: normalizePublishedAt(dateText),
			ParsedAt:    time.Now(),
		})
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: rendered HTML has no /pc article link with an id query", errSistNoArticles)
	}
	return items, nil
}

func isSistArticleTitle(title string) bool {
	return !sistNavigationTitles[title] && isValidNewsTitle(title)
}

func isSistArticleLink(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/pc/") || parsed.Query().Get("id") == "" {
		return false
	}
	base, err := url.Parse(baseURL)
	if err == nil && base.Host != "" && parsed.Host != "" && !strings.EqualFold(base.Host, parsed.Host) {
		return false
	}
	return true
}

func isSistShell(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, `id="app"`) &&
		(strings.Contains(text, "sist_web") || strings.Contains(text, "/static/js/app."))
}

func normalizeSistDate(value string) string {
	match := sistDateRE.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return ""
	}
	year, month, day := 0, 0, 0
	if _, err := fmt.Sscanf(match[1]+"-"+match[2]+"-"+match[3], "%d-%d-%d", &year, &month, &day); err != nil {
		return ""
	}
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if date.Year() != year || int(date.Month()) != month || date.Day() != day {
		return ""
	}
	return date.Format("2006-01-02")
}

// parseSistArticle accepts the JSON returned by /dev-api/web/article and a
// post-rendered Details component.  It deliberately rejects the initial
// shell, since that document contains neither the requested id nor article
// content.
func parseSistArticle(body []byte, baseURL string) (*model.Article, error) {
	if item, recognized, err := decodeSistArticle(body); recognized {
		if err != nil {
			return nil, err
		}
		return sistArticleFromAPI(item, baseURL)
	}

	article, err := parseSistRenderedArticle(body, baseURL)
	if err == nil {
		return article, nil
	}
	if isSistShell(body) {
		return nil, fmt.Errorf("%w: /dev-api/web/article requires POST id", errSistDynamicPage)
	}
	return nil, err
}

func decodeSistArticle(body []byte) (sistAPIItem, bool, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed[0] != '{' {
		return sistAPIItem{}, false, nil
	}
	var root struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return sistAPIItem{}, true, fmt.Errorf("sist: decode article JSON: %w", err)
	}
	if root.Code != 0 && root.Code != 200 && root.Code != 199 {
		return sistAPIItem{}, true, fmt.Errorf("sist: article API returned code %d", root.Code)
	}
	if len(root.Data) == 0 || string(root.Data) == "null" {
		return sistAPIItem{}, true, fmt.Errorf("sist: article API returned no data")
	}
	var wrapper struct {
		Data sistAPIItem `json:"data"`
	}
	if err := json.Unmarshal(root.Data, &wrapper); err == nil && wrapper.Data.Title != "" {
		return wrapper.Data, true, nil
	}
	var item sistAPIItem
	if err := json.Unmarshal(root.Data, &item); err != nil {
		return sistAPIItem{}, true, fmt.Errorf("sist: decode article data: %w", err)
	}
	if item.Title == "" {
		return sistAPIItem{}, true, fmt.Errorf("sist: article API data has no title")
	}
	return item, true, nil
}

func sistArticleFromAPI(item sistAPIItem, baseURL string) (*model.Article, error) {
	article := &model.Article{
		Title:       cleanTitle(item.Title),
		Date:        normalizeSistDate(item.PublishTime),
		PublishedAt: normalizePublishedAt(item.PublishTime),
		Type:        strings.TrimSpace(string(item.Type)),
		Author:      strings.TrimSpace(item.PublishUserName),
	}
	if item.SiteType == "1" {
		article.Source = "信息科学与技术学院"
	}
	article.Content, article.ContentHTML, article.Images, article.Attachments = parseSistContent(item.Content, baseURL)
	if article.Title == "" || !isSistArticleTitle(article.Title) {
		return nil, fmt.Errorf("%w: article API title is empty or is a navigation label", errSistNoArticles)
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("%w: article API body is empty", errSistNoArticles)
	}
	return article, nil
}

func parseSistRenderedArticle(body []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("sist: parse rendered article: %w", err)
	}
	title := strings.TrimSpace(doc.Find(".details .title, .context .title, h1").First().Text())
	content := doc.Find(".details .text, .context .text, .v_news_content").First()
	if title == "" || content.Length() == 0 {
		return nil, fmt.Errorf("%w: rendered article has no title/content", errSistNoArticles)
	}
	article := &model.Article{Title: cleanTitle(title)}
	meta := strings.TrimSpace(doc.Find(".details_title, .article-meta, .news_source").First().Text())
	article.Date = normalizeSistDate(meta)
	article.PublishedAt = normalizePublishedAt(meta)
	article.Content, article.ContentHTML, article.Images, article.Attachments = parseSistContentFromSelection(content, baseURL)
	if !isSistArticleTitle(article.Title) {
		return nil, fmt.Errorf("%w: rendered article title is a navigation label", errSistNoArticles)
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("%w: rendered article body is empty", errSistNoArticles)
	}
	return article, nil
}

func parseSistContent(raw, baseURL string) (string, string, []string, []model.Attachment) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return strings.TrimSpace(raw), raw, nil, nil
	}
	return parseSistContentFromSelection(doc.Find("body").First(), baseURL)
}

func parseSistContentFromSelection(content *goquery.Selection, baseURL string) (string, string, []string, []model.Attachment) {
	if content == nil || content.Length() == 0 {
		return "", "", nil, nil
	}
	var paragraphs []string
	content.Find("p, h1, h2, h3, h4, li").Each(func(_ int, sel *goquery.Selection) {
		if text := strings.TrimSpace(sel.Text()); text != "" {
			paragraphs = append(paragraphs, text)
		}
	})
	if len(paragraphs) == 0 {
		if text := strings.TrimSpace(content.Text()); text != "" {
			paragraphs = append(paragraphs, text)
		}
	}

	images := make([]string, 0)
	seenImages := make(map[string]bool)
	content.Find("img[src]").Each(func(_ int, sel *goquery.Selection) {
		if resolved := resolveURL(baseURL, sel.AttrOr("src", "")); resolved != "" {
			if !seenImages[resolved] {
				seenImages[resolved] = true
				images = append(images, resolved)
			}
			sel.SetAttr("src", resolved)
		}
	})
	attachments := make([]model.Attachment, 0)
	seenAttachments := make(map[string]bool)
	content.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := sel.AttrOr("href", "")
		resolved := resolveURL(baseURL, href)
		if resolved == "" {
			return
		}
		name := strings.TrimSpace(sel.Text())
		isAttachment := isSistAttachmentReference(href, name)
		if isAttachment && !seenAttachments[resolved] {
			seenAttachments[resolved] = true
			if name == "" {
				name = href
			}
			attachments = append(attachments, model.Attachment{Name: name, URL: resolved})
		}
		sel.SetAttr("href", resolved)
	})

	htmlText, _ := content.Html()
	return strings.Join(paragraphs, "\n\n"), htmlText, images, attachments
}

func isSistAttachmentReference(href, name string) bool {
	for _, value := range []string{href, name} {
		lower := strings.ToLower(value)
		for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
			if strings.Contains(lower, ext) {
				return true
			}
		}
	}
	return false
}
