package scraper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const libCanonicalBaseURL = "https://www.lib.swjtu.edu.cn"

type libFeed struct {
	Slug        string
	ColumnID    string
	Name        string
	ArticleType string
}

var (
	// These are the ten non-empty article columns exposed by the live news
	// column API. The empty VR column and the six "背景图" records are not
	// article feeds, so they are intentionally absent.
	libFeeds = []libFeed{
		{Slug: "resources", ColumnID: "FYg9T6N33e1BRuUo", Name: "资源发布", ArticleType: "资源发布"},
		{Slug: "news", ColumnID: "Ds8XD6N32k1BF9$F", Name: "新闻动态", ArticleType: "新闻动态"},
		{Slug: "image-news", ColumnID: "E8C23AF5-93CB-755E-EAF0-E88A7041BC1A", Name: "图片新闻", ArticleType: "图片新闻"},
		{Slug: "latest", ColumnID: "6bce6a0d-85a8-4862-82e2-fdb1b6094472", Name: "最新动态", ArticleType: "最新动态"},
		{Slug: "notices", ColumnID: "FPJqi6N32bc0Ws2", Name: "通知公告", ArticleType: "通知公告"},
		{Slug: "publicity", ColumnID: "CF50r6N32a0raHf", Name: "公示", ArticleType: "公示"},
		{Slug: "lectures", ColumnID: "d55f1e93-04f7-4d6f-8fa8-6ba19544d985", Name: "讲座", ArticleType: "讲座"},
		{Slug: "activities", ColumnID: "C8rqy6KxDyQnWcp", Name: "活动", ArticleType: "活动"},
		{Slug: "exhibitions", ColumnID: "818ea1e7-c68d-46c4-bb9c-0a94b21483c2", Name: "展览", ArticleType: "展览"},
	}
	libSubPages = func() []string {
		pages := make([]string, 0, len(libFeeds))
		for _, feed := range libFeeds {
			pages = append(pages, feed.Slug)
		}
		return pages
	}()

	libArticlePathRE    = regexp.MustCompile(`(?i)^/(?:news/web_newsDetails|home/servicedetail/[0-9]+|home/service/[0-9]+/[0-9]+|news/newsdetail/[0-9]+|lecture/detail/[0-9]+)$`)
	libPageCountRE      = regexp.MustCompile(`共\s*([0-9]+)\s*页`)
	libItemCountRE      = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
	libYearMonthRE      = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`)
	libDayRE            = regexp.MustCompile(`^[0-9]{1,2}$`)
	libDataJSONRE       = regexp.MustCompile(`(?s)var\s+datajson\s*=\s*(\[.*?\])\s*;`)
	libAttachmentRE     = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
	libResourceMarkerRE = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|附件下载|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|404\s+not\s+found|access\s+denied|cloudflare|just\s+a\s+moment|captcha|turnstile|challenge-platform|安全验证|请求已被阻断`)
)

const (
	libNewsPageSize     = 20
	libMaxResourceBytes = 32 << 20
)

var libTokenState struct {
	sync.Mutex
	value     string
	expiresAt time.Time
	client    *http.Client
}

// The portal is a Smart Library/VIPS application rather than one of the
// university's ordinary 博达 pages.  Keep all selectors and URL rules here;
// sharing the generic parser would turn service/resource links into news.
// The served homepage is a client-rendered SPA shell with no article list,
// so the SDK must not synthesize an implicit "index" feed for it.
func init() {
	if site := AllSites["lib"]; site != nil {
		site.BaseURL = libCanonicalBaseURL
		site.NoIndexFeed = true
	}
	registerAdapter("lib", SiteAdapter{
		SubPages:     libSubPages,
		ParseList:    parseAdapter_libList,
		ParseArticle: parseAdapter_libArticle,
		PageURL:      libPageURL,
		ListPage:     libListPage,
		TotalPages:   libTotalPages,
		FetchArticle: libFetchArticle,
	})
}

// libPageURL uses the portal's query pagination.  The source numbers pages in
// the normal direction: page 1 is newest and the末页 link has the largest
// page number.  The extension argument is intentionally ignored because these
// routes are MVC endpoints, not generated .htm files.
func libPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	_ = extension

	route := strings.Trim(strings.TrimSpace(slug), "/")
	if route == "" || route == "index" {
		if page != 1 {
			return ""
		}
		return libCanonicalBaseURL + "/"
	}
	if page < 1 || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if feed, ok := libFeedForSlug(route); ok {
		_ = baseURL
		u, _ := url.Parse(libCanonicalBaseURL + "/news/web_newsList")
		query := u.Query()
		query.Set("cid", feed.ColumnID)
		query.Set("pageIndex", strconv.Itoa(page))
		u.RawQuery = query.Encode()
		return u.String()
	}

	u, err := url.Parse("/" + route)
	if err != nil {
		return fmt.Sprintf("%s/%s?page=%d", libCanonicalBaseURL, route, page)
	}
	query := u.Query()
	query.Set("page", strconv.Itoa(page))
	u.RawQuery = query.Encode()
	return libCanonicalBaseURL + u.String()
}

func libTotalPages(slug string) (int, error) {
	_, pages, err := libListPage(slug, 1)
	return pages, err
}

func libFeedForSlug(slug string) (libFeed, bool) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	for _, feed := range libFeeds {
		if feed.Slug == slug {
			return feed, true
		}
	}
	return libFeed{}, false
}

func libFeedForColumnID(columnID string) (libFeed, bool) {
	for _, feed := range libFeeds {
		if feed.ColumnID == strings.TrimSpace(columnID) {
			return feed, true
		}
	}
	return libFeed{}, false
}

type libNewsAPIListItem struct {
	ContentID    string   `json:"contentID"`
	MainColumnID string   `json:"mainColumnID"`
	Title        string   `json:"title"`
	PublishDate  string   `json:"publishDate"`
	JumpLink     string   `json:"jumpLink"`
	ExternalLink string   `json:"externalLink"`
	Lables       string   `json:"lables"`
	LablesName   []string `json:"lablesName"`
}

type libNewsAPIListResponse struct {
	StatusCode json.RawMessage `json:"statusCode"`
	Data       struct {
		PageIndex  int                  `json:"pageIndex"`
		PageSize   int                  `json:"pageSize"`
		TotalCount int                  `json:"totalCount"`
		TotalPages int                  `json:"totalPages"`
		Items      []libNewsAPIListItem `json:"items"`
	} `json:"data"`
}

func libListPage(slug string, page int) ([]model.NewsItem, int, error) {
	feed, ok := libFeedForSlug(slug)
	if !ok {
		return nil, 0, fmt.Errorf("lib: %q is not an audited article column", slug)
	}
	if page < 1 {
		return nil, 0, fmt.Errorf("lib: page must be positive: %d", page)
	}
	body := map[string]interface{}{
		"pageIndex":        page,
		"pageSize":         libNewsPageSize,
		"columnID":         feed.ColumnID,
		"contentCutLength": 600,
		"lableID":          "",
		"searchKey":        "",
	}
	raw, err := libNewsAPIRequest(http.MethodPost, "/news/api/news/pront-news-list-data-get", url.Values{"columnID": {feed.ColumnID}}, body)
	if err != nil {
		return nil, 0, fmt.Errorf("lib: list %s page %d: %w", slug, page, err)
	}
	items, pages, err := parseLibAPIList(raw, feed)
	if err != nil {
		return nil, 0, fmt.Errorf("lib: parse list %s page %d: %w", slug, page, err)
	}
	return items, pages, nil
}

func parseLibAPIList(raw []byte, feed libFeed) ([]model.NewsItem, int, error) {
	if len(raw) == 0 || libBlockedResponse(raw) {
		return nil, 0, fmt.Errorf("response is empty or a CAPTCHA/WAF page")
	}
	var response libNewsAPIListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, 0, fmt.Errorf("decode API response: %w", err)
	}
	if !libAPIStatusOK(response.StatusCode) {
		return nil, 0, fmt.Errorf("API status is not successful")
	}
	pageSize := response.Data.PageSize
	if pageSize < 1 {
		pageSize = libNewsPageSize
	}
	totalPages := response.Data.TotalPages
	if totalPages < 1 {
		totalPages = (response.Data.TotalCount + pageSize - 1) / pageSize
		if totalPages < 1 {
			totalPages = 1
		}
	}

	items := make([]model.NewsItem, 0, len(response.Data.Items))
	seen := make(map[string]bool, len(response.Data.Items))
	for _, entry := range response.Data.Items {
		title := strings.Join(strings.Fields(strings.TrimSpace(entry.Title)), " ")
		if !libIsArticleTitle(title) {
			continue
		}
		articleURL, ok := libAPIItemURL(feed, entry)
		if !ok || seen[articleURL] {
			continue
		}
		date, publishedAt := libAPIDateValues(entry.PublishDate)
		typ := libAPIItemType(feed, entry)
		seen[articleURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         articleURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	}
	if len(response.Data.Items) > 0 && len(items) == 0 {
		return nil, 0, fmt.Errorf("API rows contain no valid local articles")
	}
	return items, totalPages, nil
}

func libAPIItemURL(feed libFeed, entry libNewsAPIListItem) (string, bool) {
	if strings.TrimSpace(entry.ExternalLink) != "" {
		if link, ok := libArticleURL(libCanonicalBaseURL, entry.ExternalLink); ok {
			return link, true
		}
		return "", false
	}
	if strings.TrimSpace(entry.JumpLink) != "" {
		if link, ok := libArticleURL(libCanonicalBaseURL, entry.JumpLink); ok {
			return link, true
		}
	}
	if strings.TrimSpace(entry.ContentID) == "" {
		return "", false
	}
	return libNewsDetailURL(feed.ColumnID, entry.ContentID), true
}

func libNewsDetailURL(columnID, contentID string) string {
	u, _ := url.Parse(libCanonicalBaseURL + "/news/web_newsDetails")
	query := u.Query()
	query.Set("cid", strings.TrimSpace(columnID))
	query.Set("id", strings.TrimSpace(contentID))
	u.RawQuery = query.Encode()
	return u.String()
}

func libAPIItemType(feed libFeed, entry libNewsAPIListItem) string {
	for _, value := range entry.LablesName {
		if value = strings.Join(strings.Fields(strings.TrimSpace(value)), " "); value != "" {
			return value
		}
	}
	if value := strings.Join(strings.Fields(strings.TrimSpace(entry.Lables)), " "); value != "" {
		return value
	}
	return feed.ArticleType
}

func libAPIDateValues(raw string) (string, string) {
	date, publishedAt := libRawDateValues(raw)
	if date == "" {
		return "", ""
	}
	raw = strings.TrimSpace(raw)
	// The portal encodes date-only publication fields as midnight. Do not turn
	// that sentinel into a fabricated time-of-day; preserve real source times.
	if !strings.ContainsAny(raw, "Tt ") || strings.HasSuffix(raw, "T00:00:00") || strings.HasSuffix(raw, " 00:00:00") {
		return date, date
	}
	return date, publishedAt
}

type libNewsAPIArticleResponse struct {
	StatusCode json.RawMessage `json:"statusCode"`
	Data       struct {
		Content libNewsAPIArticle `json:"content"`
	} `json:"data"`
}

type libNewsAPIArticle struct {
	ID            string `json:"id"`
	ColumnID      string `json:"columnID"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	Author        string `json:"author"`
	Publisher     string `json:"publisher"`
	PublisherName string `json:"publisherName"`
	PublishDate   string `json:"publishDate"`
	DataSource    string `json:"dataSource"`
}

func parseLibAPIArticle(raw []byte, baseURL string) (*model.Article, error) {
	if len(raw) == 0 || libBlockedResponse(raw) {
		return nil, fmt.Errorf("response is empty or a CAPTCHA/WAF page")
	}
	var response libNewsAPIArticleResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode API response: %w", err)
	}
	if !libAPIStatusOK(response.StatusCode) {
		return nil, fmt.Errorf("API status is not successful")
	}
	content := response.Data.Content
	title := strings.Join(strings.Fields(strings.TrimSpace(content.Title)), " ")
	if !libIsArticleTitle(title) {
		return nil, fmt.Errorf("API detail has no valid title")
	}
	feed, ok := libFeedForColumnID(content.ColumnID)
	if !ok {
		return nil, fmt.Errorf("API detail has an unknown article column %q", content.ColumnID)
	}
	date, publishedAt := libAPIDateValues(content.PublishDate)
	article := &model.Article{
		Title:       title,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        feed.ArticleType,
		Source:      strings.TrimSpace(content.Publisher),
		Author:      strings.TrimSpace(content.Author),
	}
	if article.Source == "" {
		article.Source = strings.TrimSpace(content.PublisherName)
	}
	if article.Source == "" {
		article.Source = strings.TrimSpace(content.DataSource)
	}

	bodyDoc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"lib-api-body\">" + content.Content + "</div>"))
	if err != nil {
		return nil, fmt.Errorf("parse API body: %w", err)
	}
	body := bodyDoc.Find("#lib-api-body").First()
	body.Find("script, style, noscript").Remove()
	if body.Length() == 0 {
		return nil, fmt.Errorf("API detail has no body")
	}
	libCollectAPIResources(article, body, baseURL)
	article.Content = extractBodaContentText(body)
	if article.Content == "" {
		article.Content = strings.Join(strings.Fields(body.Text()), " ")
	}
	if html, htmlErr := body.Html(); htmlErr == nil {
		article.ContentHTML = html
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("API detail body is empty")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func libCollectAPIResources(article *model.Article, body *goquery.Selection, baseURL string) {
	if article == nil || body == nil || body.Length() == 0 {
		return
	}
	seenImages := make(map[string]bool)
	body.Find("img").Each(func(_ int, image *goquery.Selection) {
		var raw string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				raw = value
				break
			}
		}
		resolved := libResourceURL(baseURL, raw)
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		if !seenImages[resolved] {
			seenImages[resolved] = true
			article.Images = append(article.Images, resolved)
		}
	})

	seenAttachments := make(map[string]bool)
	body.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") || strings.HasPrefix(strings.ToLower(href), "data:") {
			link.Remove()
			return
		}
		resolved := libResolveURL(baseURL, href)
		if libLooksLikeAttachment(link, resolved) {
			resolved = libResourceURL(baseURL, href)
			if resolved == "" {
				link.Remove()
				return
			}
			name := strings.TrimSpace(link.AttrOr("download", ""))
			if name == "" {
				name = strings.TrimSpace(link.AttrOr("name", ""))
			}
			if name == "" {
				name = strings.Join(strings.Fields(link.Text()), " ")
			}
			if name == "" {
				name = libURLFilename(resolved)
			}
			if name == "" || seenAttachments[resolved] {
				return
			}
			seenAttachments[resolved] = true
			article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
			link.SetAttr("href", resolved)
			return
		}
		parsed, err := url.Parse(resolved)
		if err != nil || parsed.Scheme == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			link.Remove()
			return
		}
		link.SetAttr("href", resolved)
	})
}

func libFetchArticle(articleURL string) (*model.Article, []byte, error) {
	fullURL, ok := libArticleURL(libCanonicalBaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("lib: refusing non-local or non-detail URL %q", articleURL)
	}
	parsed, _ := url.Parse(fullURL)
	columnID := parsed.Query().Get("cid")
	contentID := parsed.Query().Get("id")
	if columnID == "" || contentID == "" {
		return nil, nil, fmt.Errorf("lib: detail URL has no cid/id: %q", articleURL)
	}
	raw, err := libNewsAPIRequest(http.MethodGet, "/news/api/news/pront-news-content-get", url.Values{
		"contentid": {contentID},
		"columnid":  {columnID},
	}, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("lib: fetch article %s: %w", fullURL, err)
	}
	article, err := parseLibAPIArticle(raw, libCanonicalBaseURL)
	if err != nil {
		return nil, raw, fmt.Errorf("lib: parse article %s: %w", fullURL, err)
	}
	libVerifyArticleResources(article, libCanonicalBaseURL)
	return article, raw, nil
}

func libAPIStatusOK(raw json.RawMessage) bool {
	var number int
	if json.Unmarshal(raw, &number) == nil {
		return number == http.StatusOK
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value) == "200"
	}
	return false
}

func libHTTPClient() *http.Client {
	if defaultClient != nil {
		return defaultClient
	}
	return http.DefaultClient
}

func libNewsAPIToken() (string, error) {
	client := libHTTPClient()
	libTokenState.Lock()
	defer libTokenState.Unlock()
	if libTokenState.client == client && libTokenState.value != "" && time.Now().Before(libTokenState.expiresAt) {
		return libTokenState.value, nil
	}
	req, err := http.NewRequest(http.MethodPost, libTokenURL, strings.NewReader(`{"orgCode":"swjtu"}`))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("token request: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}
	var result struct {
		StatusCode json.RawMessage `json:"statusCode"`
		Data       struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil || !libAPIStatusOK(result.StatusCode) || strings.TrimSpace(result.Data.Token) == "" {
		return "", fmt.Errorf("token response is invalid")
	}
	libTokenState.client = client
	libTokenState.value = result.Data.Token
	libTokenState.expiresAt = time.Now().Add(25 * time.Minute)
	return libTokenState.value, nil
}

func libNewsAPIRequest(method, path string, query url.Values, payload interface{}) ([]byte, error) {
	token, err := libNewsAPIToken()
	if err != nil {
		return nil, err
	}
	endpoint := libGateway + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var body io.Reader
	if payload != nil {
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return nil, fmt.Errorf("encode API request: %w", marshalErr)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := libHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("API request: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, libMaxResourceBytes))
	if err != nil {
		return nil, fmt.Errorf("read API response: %w", err)
	}
	if libBlockedResponse(raw) {
		return nil, fmt.Errorf("API request returned a CAPTCHA/WAF response")
	}
	return raw, nil
}

func libResourceURL(baseURL, raw string) string {
	resolved := libImageURL(baseURL, raw)
	if resolved == "" || !libIsLocalURL(resolved) {
		return ""
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return ""
	}
	value := strings.ToLower(parsed.EscapedPath() + "?" + parsed.RawQuery)
	for _, marker := range []string{"captcha", "verify", "verification", "turnstile", "challenge", "createimage", "getcode"} {
		if strings.Contains(value, marker) {
			return ""
		}
	}
	return resolved
}

func libVerifyArticleResources(article *model.Article, baseURL string) {
	if article == nil {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		resolved := libResourceURL(baseURL, image)
		if resolved == "" {
			continue
		}
		if _, _, err := libFetchResource(resolved, baseURL, true); err == nil {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	attachmentSizes := make(map[string]string, len(article.Attachments))
	for _, attachment := range article.Attachments {
		resolved := libResourceURL(baseURL, attachment.URL)
		if resolved == "" {
			continue
		}
		body, _, err := libFetchResource(resolved, baseURL, false)
		if err == nil {
			validAttachments[resolved] = true
			attachmentSizes[resolved] = strconv.Itoa(len(body))
		}
	}
	filteredImages := make([]string, 0, len(validImages))
	for _, image := range article.Images {
		if resolved := libResourceURL(baseURL, image); validImages[resolved] {
			filteredImages = append(filteredImages, resolved)
		}
	}
	article.Images = filteredImages
	filteredAttachments := make([]model.Attachment, 0, len(validAttachments))
	for _, attachment := range article.Attachments {
		resolved := libResourceURL(baseURL, attachment.URL)
		if validAttachments[resolved] {
			attachment.URL = resolved
			attachment.Size = attachmentSizes[resolved]
			filteredAttachments = append(filteredAttachments, attachment)
		}
	}
	article.Attachments = filteredAttachments
	libSanitizeVerifiedContentHTML(article, baseURL, validImages, validAttachments)
}

func libSanitizeVerifiedContentHTML(article *model.Article, baseURL string, validImages, validAttachments map[string]bool) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"lib-verified-body\">" + article.ContentHTML + "</div>"))
	if err != nil {
		return
	}
	body := doc.Find("#lib-verified-body").First()
	body.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := libResourceURL(baseURL, image.AttrOr("src", ""))
		if !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	body.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := link.AttrOr("href", "")
		if !libLooksLikeAttachment(link, href) {
			return
		}
		resolved := libResourceURL(baseURL, href)
		if !validAttachments[resolved] {
			link.Remove()
			return
		}
		link.SetAttr("href", resolved)
	})
	if html, htmlErr := body.Html(); htmlErr == nil {
		article.ContentHTML = html
	}
}

func libFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := libResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("lib: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("lib: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126 Safari/537.36")
	req.Header.Set("Referer", baseURL)
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *libHTTPClient()
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || libIsLocalURL(next.URL.String()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("lib: fetch resource %s: %w", resolved, err)
	}
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("lib: resource %s: HTTP %d", resolved, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, libMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("lib: read resource %s: %w", resolved, err)
	}
	if len(body) > libMaxResourceBytes || !libResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("lib: resource %s returned invalid Content-Type, shell, or magic", resolved)
	}
	return body, contentType, nil
}

func libResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 || strings.TrimSpace(contentType) == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" || mediaType == "application/json" || mediaType == "text/xml" || mediaType == "application/xml" {
		return false
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if libResourceMarkerRE.Match(probe) {
		return false
	}
	magic := libResourceMagic(body)
	if image {
		if !strings.HasPrefix(mediaType, "image/") {
			return false
		}
		expected := map[string]string{
			"image/jpeg": "jpeg", "image/jpg": "jpeg", "image/png": "png",
			"image/gif": "gif", "image/webp": "webp", "image/bmp": "bmp",
			"image/tiff": "tiff", "image/svg+xml": "svg",
		}[mediaType]
		return magic != "" && (expected == "" || magic == expected)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "officedocument") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".rtf") || mediaType == "application/rtf" || mediaType == "text/rtf":
		return magic == "rtf"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func libResourceMagic(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x03, 0x04}) || bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x05, 0x06}) || bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x07, 0x08})):
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
	case bytes.HasPrefix(bytes.TrimSpace(body), []byte("{\\rtf")):
		return "rtf"
	case bytes.Contains(bytes.ToLower(body), []byte("<svg")):
		return "svg"
	default:
		return ""
	}
}

func parseAdapter_libList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if libBlockedResponse(raw) {
		return nil, fmt.Errorf("lib list is a CAPTCHA/WAF or error response")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse lib list: %w", err)
	}

	if libIsHomepage(doc) {
		items := parseLibHomepage(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("lib homepage contains no local article entries")
		}
		return items, nil
	}
	if libIsSPAShell(doc) {
		return nil, fmt.Errorf("lib page is an unrendered SPA shell; article API response is unavailable")
	}

	if libIsResourcePage(doc) {
		return nil, fmt.Errorf("lib page is a resource/service catalog, not an article list")
	}
	list := libListContainer(doc)
	if list.Length() == 0 {
		return nil, fmt.Errorf("lib page is not an article list (static/resource/login page)")
	}

	rows := list.Find("dl")
	if rows.Length() == 0 {
		if libExplicitEmptyList(doc) {
			return []model.NewsItem{}, nil
		}
		return nil, fmt.Errorf("lib recognized article list has no rows")
	}

	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	typ := libDocumentType(doc)
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("dt a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := libArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := libAnchorTitle(anchor)
		if !libIsArticleTitle(title) {
			return
		}
		date, publishedAt := libDateValues(row.Find(".time, .date, time, em").First())
		rowType := strings.TrimSpace(row.Find(".type").First().Text())
		if rowType == "" {
			rowType = typ
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        rowType,
			ParsedAt:    time.Now(),
		})
	})

	if len(items) == 0 {
		if libExplicitEmptyList(doc) {
			return []model.NewsItem{}, nil
		}
		return nil, fmt.Errorf("lib article list contains no local article rows")
	}
	return items, nil
}

func parseLibHomepage(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	add := func(anchor *goquery.Selection, title, rawDate, typ string) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		fullURL, ok := libArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		if title == "" {
			title = libAnchorTitle(anchor)
		} else {
			title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
		}
		if !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := libRawDateValues(rawDate)
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        strings.TrimSpace(typ),
			ParsedAt:    time.Now(),
		})
	}

	// 新闻动态 is a separate m-box.  Restrict extraction to that box so
	// database/resource/service cards cannot become fake articles.
	doc.Find("#part2 .m-box").Each(func(_ int, box *goquery.Selection) {
		heading := strings.Join(strings.Fields(box.Find(".m-box-title h2").First().Text()), " ")
		if strings.Contains(heading, "新闻动态") {
			box.Find(".news-list li").Each(func(_ int, row *goquery.Selection) {
				anchor := row.Find("a[href]").First()
				add(anchor, libAnchorTitle(anchor), row.Find(".time, .date, time").First().Text(), row.Find(".type").First().Text())
			})
			// The module also contains a carousel. Resource cards in the same
			// carousel are rejected by libArticleURL; local article cards are
			// real 图片新闻 entries and must not be lost.
			box.Find(".pic-show .pic-move li").Each(func(_ int, row *goquery.Selection) {
				anchor := row.Find("a[href]").First()
				add(anchor, libAnchorTitle(anchor), "", "图片新闻")
			})
			return
		}
		if !strings.Contains(heading, "讲座活动") {
			return
		}

		// The lecture tab is populated from a complete JSON array embedded in
		// the page, not just the first few visible rows.
		box.Find("script").Each(func(_ int, script *goquery.Selection) {
			match := libDataJSONRE.FindStringSubmatch(script.Text())
			if len(match) != 2 {
				return
			}
			var records []struct {
				Title    string `json:"title"`
				Datetime string `json:"datetime"`
				Href     string `json:"href"`
			}
			if json.Unmarshal([]byte(match[1]), &records) != nil {
				return
			}
			for _, record := range records {
				fullURL, ok := libArticleURL(baseURL, record.Href)
				if !ok || seen[fullURL] || !libIsArticleTitle(record.Title) {
					continue
				}
				date, publishedAt := libRawDateValues(record.Datetime)
				seen[fullURL] = true
				items = append(items, model.NewsItem{
					Title:       strings.Join(strings.Fields(record.Title), " "),
					URL:         fullURL,
					Date:        date,
					PublishedAt: publishedAt,
					Type:        "讲座",
					ParsedAt:    time.Now(),
				})
			}
		})

		box.Find(".hd-list2 .lecture-list dl, .hd-list2 .news-list dl").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("dt a[href]").First()
			add(anchor, libAnchorTitle(anchor), row.Find(".time, .date, time").First().Text(), "展览")
		})
		box.Find(".hd-list3 .lecture-list dl, .hd-list3 .news-list dl").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("dt a[href]").First()
			add(anchor, libAnchorTitle(anchor), row.Find(".time, .date, time").First().Text(), "活动")
		})
		box.Find(".hd-list3 .news-list li").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			add(anchor, libAnchorTitle(anchor), row.Find(".time, .date, time").First().Text(), "活动")
		})
	})

	return items
}

func parseAdapter_libArticle(raw []byte, baseURL string) (*model.Article, error) {
	if libBlockedResponse(raw) {
		return nil, fmt.Errorf("lib article is a CAPTCHA/WAF or error response")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse lib article: %w", err)
	}
	detail := doc.Find(".news-detail").First()
	if detail.Length() == 0 {
		return nil, fmt.Errorf("lib page is not an article detail")
	}
	contentSel := detail.Find(".news-content").First()
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("lib article has no news-content body")
	}
	title := strings.Join(strings.Fields(detail.Find("h1").First().Text()), " ")
	if !libIsArticleTitle(title) {
		return nil, fmt.Errorf("lib article has no valid title")
	}

	meta := detail.Find("em").First()
	date, publishedAt := libDateValues(meta)
	article := &model.Article{
		Title:       title,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        libDocumentType(doc),
		Source:      "图书馆",
	}
	meta.Find("span").Each(func(_ int, field *goquery.Selection) {
		text := strings.TrimSpace(field.Text())
		for _, prefix := range []string{"发布人：", "发布人:", "来源：", "来源:", "作者：", "作者:"} {
			if strings.HasPrefix(text, prefix) {
				value := strings.TrimSpace(strings.TrimPrefix(text, prefix))
				if value != "" && !strings.HasPrefix(prefix, "作者") {
					article.Source = value
				}
				if strings.HasPrefix(prefix, "作者") {
					article.Author = value
				}
				break
			}
		}
	})

	article.Content = strings.Join(strings.Fields(contentSel.Text()), " ")
	if html, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = html
	}

	seenImages := make(map[string]bool)
	contentSel.Find("img").Each(func(_ int, image *goquery.Selection) {
		var source string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				source = value
				break
			}
		}
		resolved := libImageURL(baseURL, source)
		if resolved == "" || seenImages[resolved] {
			return
		}
		seenImages[resolved] = true
		article.Images = append(article.Images, resolved)
		image.SetAttr("src", resolved)
	})

	seenAttachments := make(map[string]bool)
	appendAttachment := func(link *goquery.Selection) {
		if link == nil || link.Length() == 0 {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		resolved := libResolveURL(baseURL, href)
		if !libIsLocalURL(resolved) || seenAttachments[resolved] || !libLooksLikeAttachment(link, resolved) {
			return
		}
		name := strings.TrimSpace(link.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(link.AttrOr("name", ""))
		}
		if name == "" {
			name = strings.Join(strings.Fields(link.Text()), " ")
		}
		if name == "" {
			name = libURLFilename(resolved)
		}
		if name == "" {
			return
		}
		seenAttachments[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	}
	contentSel.Find("a[href]").Each(func(_ int, link *goquery.Selection) { appendAttachment(link) })
	// Some versions render an attachment list beside the editor body.  Only
	// labeled/local links are eligible; the empty .media placeholder is not.
	detail.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if link.Closest(".news-content").Length() == 0 {
			appendAttachment(link)
		}
	})
	if html, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = html
	}

	normalizeArticlePublishedAt(article)
	return article, nil
}

func libListContainer(doc *goquery.Document) *goquery.Selection {
	return doc.Find(".main-content .news-list").First()
}

func libIsHomepage(doc *goquery.Document) bool {
	return doc.Find("#part2 .m-box").Length() > 0 && doc.Find("#header, #top-nav").Length() > 0
}

func libIsSPAShell(doc *goquery.Document) bool {
	if doc == nil || doc.Find("#home_page").Length() == 0 {
		return false
	}
	return doc.Find(".m-box, .main-content .news-list, .news-detail").Length() == 0
}

func libImageURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	for _, prefix := range []string{"data:", "javascript:", "file:", "blob:"} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	resolved := libResolveURL(baseURL, raw)
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	return resolved
}

func libIsResourcePage(doc *goquery.Document) bool {
	column := strings.TrimSpace(doc.Find("#pageColumn").First().Text())
	if strings.Contains(column, "快速链接") || strings.Contains(column, "古籍特藏") {
		return true
	}
	return doc.Find(".news-detail").Length() == 0 &&
		doc.Find("a[href*='/pagelinklog/'], a[href*='/link/']").Length() > 0 &&
		doc.Find(".main-content .news-list").Length() > 0
}

func libExplicitEmptyList(doc *goquery.Document) bool {
	text := strings.Join(strings.Fields(doc.Find(".pages, .pagination, .page, .news-list").Text()), " ")
	if strings.Contains(text, "暂无") || strings.Contains(text, "没有相关") {
		return true
	}
	return regexp.MustCompile(`共\s*0\s*(?:页|条)`).MatchString(text)
}

func libPageCount(doc *goquery.Document) (int, bool) {
	pageText := strings.Join(strings.Fields(doc.Find(".pages, .pagination, .page").Text()), " ")
	if match := libPageCountRE.FindStringSubmatch(pageText); len(match) == 2 {
		pages, _ := strconv.Atoi(match[1])
		return pages, true
	}

	// A few portal revisions omit the text counter but retain the hidden total
	// article count and 10 rows per page.
	countText := doc.Find("#hidPageCount").AttrOr("value", "")
	if countText == "" {
		countText = doc.Find("input[name='hidPageCount']").AttrOr("value", "")
	}
	if countText == "" {
		if match := libItemCountRE.FindStringSubmatch(pageText); len(match) == 2 {
			countText = match[1]
		}
	}
	count, err := strconv.Atoi(strings.TrimSpace(countText))
	rows := doc.Find(".main-content .news-list dl").Length()
	if err == nil && count >= 0 && rows > 0 {
		return (count + rows - 1) / rows, true
	}
	return 0, false
}

func libDocumentType(doc *goquery.Document) string {
	for _, selector := range []string{".main-site a", ".news-guid li.current a"} {
		if text := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); text != "" {
			return text
		}
	}
	text := strings.Join(strings.Fields(doc.Find("#pageColumn").First().Text()), " ")
	if text == "讲座展览活动" {
		return "讲座"
	}
	return text
}

func libAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	return strings.Join(strings.Fields(title), " ")
}

func libIsArticleTitle(title string) bool {
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	if len([]rune(title)) < 4 || strings.HasSuffix(title, ">>") {
		return false
	}
	switch title {
	case "首页", "更多", "查看更多", "登录", "注册", "搜索", "资源", "服务", "概况", "我的图书馆":
		return false
	}
	return true
}

func libDateValues(sel *goquery.Selection) (string, string) {
	if sel == nil || sel.Length() == 0 {
		return "", ""
	}
	raw := strings.TrimSpace(sel.Text())
	date, publishedAt := libRawDateValues(raw)
	if date != "" {
		return date, publishedAt
	}
	// The list template concatenates <span>day</span> and YYYY-MM, so Text()
	// may be "132024-12" without a separator.
	month := libYearMonthRE.FindStringSubmatch(raw)
	day := strings.TrimSpace(sel.Find("span").First().Text())
	if len(month) == 3 && libDayRE.MatchString(day) {
		date = fmt.Sprintf("%s-%02s-%02s", month[1], month[2], day)
		return date, normalizePublishedAt(date)
	}
	return "", ""
}

func libRawDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	if match := dateTimeRe.FindString(raw); match != "" {
		if publishedAt := normalizePublishedAt(match); publishedAt != "" {
			return publishedAt[:10], publishedAt
		}
	}
	if match := bareDateRe.FindString(raw); match != "" {
		if publishedAt := normalizePublishedAt(match); publishedAt != "" {
			return publishedAt[:10], publishedAt
		}
	}
	return "", ""
}

func libArticleURL(baseURL, href string) (string, bool) {
	resolved := libResolveURL(baseURL, href)
	parsed, err := url.Parse(resolved)
	if err != nil || !libIsLocalURL(resolved) || !libArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	return resolved, true
}

func libResolveURL(baseURL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = libCanonicalBaseURL
	}
	// Do not use the shared cleanURL/resolveURL path here: its historical
	// TrimRight cleanup treats a trailing article ID such as /5887 as an
	// artifact and silently drops the final digit.
	href = strings.TrimSuffix(strings.TrimSuffix(href, "%7D"), "}")
	base, baseErr := url.Parse(baseURL)
	ref, refErr := url.Parse(href)
	if baseErr != nil || refErr != nil {
		return href
	}
	resolved := base.ResolveReference(ref).String()
	parsed, err := url.Parse(resolved)
	if err != nil {
		return resolved
	}
	if libIsLocalHostname(parsed.Hostname()) {
		parsed.Scheme = "https"
		parsed.Host = "www.lib.swjtu.edu.cn"
		return parsed.String()
	}
	return resolved
}

func libIsLocalURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && libIsLocalHostname(parsed.Hostname())
}

func libIsLocalHostname(hostname string) bool {
	return strings.EqualFold(hostname, "www.lib.swjtu.edu.cn") || strings.EqualFold(hostname, "lib.swjtu.edu.cn")
}

func libLooksLikeAttachment(link *goquery.Selection, resolved string) bool {
	href := strings.TrimSpace(link.AttrOr("href", ""))
	text := strings.TrimSpace(link.Text())
	class := strings.ToLower(link.AttrOr("class", ""))
	if link.AttrOr("download", "") != "" || libAttachmentRE.MatchString(resolved) || libAttachmentRE.MatchString(href) {
		return true
	}
	if strings.Contains(text, "附件") || strings.Contains(class, "attach") || strings.Contains(class, "download") {
		return strings.Contains(strings.ToLower(href), "download") || strings.Contains(strings.ToLower(href), "file") || strings.Contains(strings.ToLower(href), "attach")
	}
	return false
}

func libURLFilename(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func libBlockedResponse(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	for _, marker := range []string{
		"cf-chl-", "challenge-platform", "turnstile", "captcha", "验证码",
		"安全验证", "access denied", "just a moment",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
