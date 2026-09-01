package scraper

import (
	"encoding/json"
	"errors"
	"fmt"
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
	ID              string `json:"id"`
	Title           string `json:"title"`
	PublishTime     string `json:"publishTime"`
	CreateTime      string `json:"createTime"`
	Content         string `json:"content"`
	Description     string `json:"description"`
	PublishUserName string `json:"publishUserName"`
	SiteType        string `json:"siteType"`
	URL             string `json:"url"`
	Link            string `json:"link"`
}

type sistListEnvelope struct {
	Code int             `json:"code"`
	Rows []sistAPIItem   `json:"rows"`
	Data json.RawMessage `json:"data"`
}

func init() {
	registerAdapter("sist", SiteAdapter{
		SubPages:     []string{"notice", "undergraduate", "postgraduate"},
		ParseList:    parseSistNews,
		ParseArticle: parseSistArticle,
		PageURL:      sistPageURL,
	})
}

// sistPageURL returns an actual browser route for the corresponding column.
// The route itself is a SPA shell; the API payload is parsed when a caller
// supplies it to parseSistNews.  The shared BodaSite fetcher only performs GET
// requests, so silently pretending that this route is a static list would be
// incorrect.
func sistPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	base := strings.TrimRight(baseURL, "/")
	section := sistSectionForSlug(slug)
	if section.rootID == "" {
		return base + "/home?lang=cn"
	}
	if section.rootID == section.navID {
		return base + "/pc/" + section.navID + "?type=2&lang=cn"
	}
	return base + "/pc/" + section.rootID + "/" + section.navID + "?type=2&lang=cn"
}

type sistSection struct {
	navID  string
	rootID string
}

func sistSectionForSlug(slug string) sistSection {
	switch strings.ToLower(strings.TrimSpace(slug)) {
	case "", "index", "news", "news-center", "新闻中心":
		return sistSection{navID: sistNewsNavID}
	case "notice", "tzgg", "通知公告":
		return sistSection{navID: sistNoticeNavID}
	case "undergraduate", "undergrad", "本科", "本科教育":
		return sistSection{navID: sistUndergraduateID, rootID: sistUndergraduateRoot}
	case "postgraduate", "graduate", "研究生", "研究生教育":
		return sistSection{navID: sistPostgraduateID, rootID: sistPostgraduateRoot}
	default:
		// Opaque navigation IDs are useful to callers that already obtained
		// the site's menu.  Do not invent a route for an unknown slug.
		return sistSection{}
	}
}

// parseSistNews accepts either a captured sist API response or a rendered
// HTML list containing real /pc/...?...id=... links.  The checked-in
// testdata/sist.html is only the SPA shell and therefore returns an error.
func parseSistNews(body []byte, baseURL string) ([]model.NewsItem, error) {
	if rows, recognized, err := decodeSistList(body); recognized {
		if err != nil {
			return nil, err
		}
		return sistNewsItems(rows, baseURL, sistNewsNavID)
	}

	items, err := parseSistRenderedList(body, baseURL)
	if err == nil {
		return items, nil
	}
	if isSistShell(body) {
		return nil, fmt.Errorf("%w: /dev-api/web/articlelist (sitetype %s)", errSistDynamicPage, sistSiteType)
	}
	return nil, err
}

func decodeSistList(body []byte) ([]sistAPIItem, bool, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, false, nil
	}

	if trimmed[0] == '[' {
		var rows []sistAPIItem
		if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
			return nil, true, fmt.Errorf("sist: decode list JSON: %w", err)
		}
		return rows, true, nil
	}

	var envelope sistListEnvelope
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return nil, true, fmt.Errorf("sist: decode list JSON: %w", err)
	}
	if envelope.Code != 0 && envelope.Code != 200 && envelope.Code != 199 {
		return nil, true, fmt.Errorf("sist: list API returned code %d", envelope.Code)
	}
	if envelope.Rows != nil {
		return envelope.Rows, true, nil
	}
	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		var nested struct {
			Rows []sistAPIItem   `json:"rows"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(envelope.Data, &nested); err == nil && nested.Rows != nil {
			return nested.Rows, true, nil
		}
		var rows []sistAPIItem
		if err := json.Unmarshal(envelope.Data, &rows); err == nil {
			return rows, true, nil
		}
	}
	if envelope.Code == 199 {
		return nil, true, fmt.Errorf("%w: list API reported no rows", errSistNoArticles)
	}
	return nil, true, fmt.Errorf("%w: JSON did not contain rows", errSistNoArticles)
}

func sistNewsItems(rows []sistAPIItem, baseURL, navID string) ([]model.NewsItem, error) {
	items := make([]model.NewsItem, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		title := cleanTitle(row.Title)
		if row.ID == "" || title == "" || len(title) > 200 || !isSistArticleTitle(title) {
			continue
		}
		itemURL := sistItemURL(row, baseURL, navID)
		if itemURL == "" || seen[itemURL] || !isSistArticleLink(itemURL, baseURL) {
			continue
		}
		seen[itemURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      itemURL,
			Date:     normalizeSistDate(row.PublishTime),
			ParsedAt: time.Now(),
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
	if section.rootID != "" {
		path = "/pc/" + section.rootID + "/" + navID
	}
	return resolveURL(baseURL, path+"?id="+url.QueryEscape(id)+"&type=2&lang=cn")
}

func sistSectionForNavID(navID string) sistSection {
	switch navID {
	case sistNewsNavID, sistNoticeNavID:
		return sistSection{navID: navID}
	case sistUndergraduateID:
		return sistSection{navID: navID, rootID: sistUndergraduateRoot}
	case sistPostgraduateID:
		return sistSection{navID: navID, rootID: sistPostgraduateRoot}
	default:
		return sistSection{navID: navID}
	}
}

func parseSistRenderedList(body []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("sist: parse rendered list: %w", err)
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
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
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     normalizeSistDate(dateText),
			ParsedAt: time.Now(),
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
		Title:  cleanTitle(item.Title),
		Date:   normalizeSistDate(item.PublishTime),
		Author: strings.TrimSpace(item.PublishUserName),
	}
	if item.SiteType == "1" {
		article.Source = "信息科学与技术学院"
	}
	article.Content, article.ContentHTML, article.Images, article.Attachments = parseSistContent(item.Content, baseURL)
	if article.Title == "" {
		return nil, fmt.Errorf("%w: article API title is empty", errSistNoArticles)
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
	article.Content, article.ContentHTML, article.Images, article.Attachments = parseSistContentFromSelection(content, baseURL)
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
		lower := strings.ToLower(href)
		isAttachment := false
		for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
			if strings.Contains(lower, ext) {
				isAttachment = true
				break
			}
		}
		if isAttachment && !seenAttachments[resolved] {
			seenAttachments[resolved] = true
			name := strings.TrimSpace(sel.Text())
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
