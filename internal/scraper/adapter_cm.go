package scraper

import (
	"bytes"
	"fmt"
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

const cmCanonicalBaseURL = "https://cm.swjtu.edu.cn"

// cmSubPages contains the article feeds exposed by CM's visible navigation
// and by the additional lists embedded in the home page.  The profile pages
// (学院介绍、现任领导、师资队伍 and similar) are deliberately excluded: they
// are real pages, but are not article feeds.  The two differently named
// 科研团队 pages are also intentional: /index/kytd is the home-page feed,
// while /kxyj/kytd is an empty/static shell.
var cmSubPages = []string{
	"index/xyyw",
	"index/tzgg",
	"index/kytd",
	"index/kydt",
	"index/zsjy",
	"index/xysh",
	"index/zt/rczp",
	"index/zt/djdt",
	"index/zt/zsjy",
	"index/zt/jjjd",
	"index/ssfc",
	"index/ssfc/xsfc",
	"index/ssfc/jsfc",
	"index/yxrl",
	"dqgz/djdt",
	"dqgz/ljyx",
	"rcpy/bkspy",
	"rcpy/yjspy",
	"kxyj/kypt",
	"xszc/xyjz",
	"cpyc/rczp",
	"cpyc/jszp",
	"cpyc/bsh",
	"cyxx",
}

var (
	cmArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	cmDateRe        = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?`)
	cmCompactDateRe = regexp.MustCompile(`(20[0-9]{2})\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?`)
	cmShortDateRe   = regexp.MustCompile(`^[0-9]{1,2}\s*[-/.]\s*[0-9]{1,2}$`)
	cmClockRe       = regexp.MustCompile(`[0-9]{1,2}\s*(?::|时)[0-9]{1,2}`)
	cmYearRe        = regexp.MustCompile(`^20[0-9]{2}$`)
	cmDateTimeRe    = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})\s*日?\s*(?:[Tt]|\s+)?\s*([0-9]{1,2})\s*(?::|时)\s*([0-9]{2})(?:\s*(?::|分)\s*([0-9]{2})\s*秒?)?`)
	cmPageCountRe   = regexp.MustCompile(`(?:/|共|第)\s*([0-9]+)\s*页`)
)

func init() {
	if site := AllSites["cm"]; site != nil {
		site.BaseURL = cmCanonicalBaseURL
	}
	registerAdapter("cm", SiteAdapter{
		SubPages:     cmSubPages,
		ParseList:    parseAdapter_cmList,
		ParseArticle: parseAdapter_cmArticle,
		PageURL:      cmPageURL,
		ListPage:     cmListPage,
		TotalPages:   cmTotalPages,
		FetchArticle: cmFetchArticle,
	})
}

// cmPageURL maps the logical page number (1 = newest) to CM's reverse-numbered
// generated files. For example, a 36-page feed advertises logical page 2 as
// /feed/35.htm and its last page as /feed/1.htm.
func cmPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page < 1 || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages <= 1 {
		return ""
	}

	filePage := page
	if totalPages > 1 {
		filePage = totalPages - page + 1
	}
	if filePage < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

// cmTotalPages reads the first page and extracts the displayed logical page
// numbers. CM's hrefs contain reverse generated filenames (35.htm is shown as
// page 2), so the shared href-based page counter cannot be used here.
func cmTotalPages(slug string) (int, error) {
	site, ok := AllSites["cm"]
	if !ok || site == nil {
		return 0, fmt.Errorf("cm site is not registered")
	}

	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		body, err := fetch(cmPageURL(site.BaseURL, slug, 1, 0, extension))
		if err != nil {
			lastErr = err
			continue
		}
		if total := cmPageCountFromHTML(body); total > 0 {
			return total, nil
		}
		lastErr = fmt.Errorf("cm list %q has no page count", slug)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("cm list %q has no page count", slug)
	}
	return 0, lastErr
}

// cmListPage owns CM's reverse-numbered page fetch and repairs list metadata
// which is known to be incomplete on the legacy 师生风采 template.  Keeping
// this in the CM adapter means the shared range walker receives authoritative
// calendar dates before it decides whether to stop at the one-year boundary.
func cmListPage(slug string, page int) ([]model.NewsItem, int, error) {
	site, ok := AllSites["cm"]
	if !ok || site == nil {
		return nil, 0, fmt.Errorf("cm site is not registered")
	}
	if page < 1 {
		return nil, 0, fmt.Errorf("cm: page must be positive: %d", page)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	firstBody, err := cmFetchListPage(baseURL, slug, 1, 0)
	if err != nil {
		return nil, 0, err
	}

	totalPages := 1
	if slug != "" && !strings.EqualFold(slug, "index") {
		totalPages = cmPageCountFromHTML(firstBody)
		if totalPages == 0 {
			return nil, 0, fmt.Errorf("cm: %s has no recognized pagination", slug)
		}
	}
	if page > totalPages {
		return nil, totalPages, fmt.Errorf("cm: page %d exceeds %d pages for %s", page, totalPages, slug)
	}

	body := firstBody
	if page > 1 {
		body, err = cmFetchListPage(baseURL, slug, page, totalPages)
		if err != nil {
			return nil, 0, err
		}
	}
	// Rows whose only target links off-site (currently WeChat reposts of job
	// notices) are deliberately skipped by the parser: they cannot be handed
	// to the CM article parser, while every local row on the page is kept.
	// A page whose rows are all external is reported as an empty feed.
	items, err := parseAdapter_cmList(body, baseURL)
	if err != nil {
		return nil, totalPages, err
	}
	if err := cmRepairListMetadata(items, baseURL); err != nil {
		return nil, totalPages, err
	}
	return items, totalPages, nil
}

func cmFetchListPage(baseURL, slug string, page, totalPages int) ([]byte, error) {
	var lastErr error
	for _, extension := range []string{".htm", ".html"} {
		pageURL := cmPageURL(baseURL, slug, page, totalPages, extension)
		body, err := fetch(pageURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("unable to fetch %s page %d", slug, page)
	}
	return nil, fmt.Errorf("cm: fetch list %s page %d: %w", slug, page, lastErr)
}

// cmFetchArticle is the live detail path.  Parsing remains deterministic, but
// CM's CMS download URLs and a few image URLs can return HTTP 200 HTML shells;
// verify resources here before exposing them to callers.
func cmFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := cmCanonicalBaseURL
	if site := AllSites["cm"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	fullURL, ok := cmArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("cm: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_cmArticle(body, baseURL)
	if err != nil {
		return nil, body, err
	}
	article.Images = cmVerifyImages(article.Images, baseURL)
	article.Attachments = cmVerifyAttachments(article.Attachments, baseURL)
	cmSanitizeContentHTML(article, baseURL)
	return article, body, nil
}

const cmMaxResourceBytes = 32 << 20

var cmResourceShellMarkerRe = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|附件下载|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|404\s+not\s+found|access\s+denied|cloudflare|just\s+a\s+moment|安全验证|请求已被阻断`)

func cmVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := cmResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := cmFetchResource(resolved, baseURL, true)
		if err != nil || !cmResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func cmVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := cmResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := cmFetchResource(resolved, baseURL, false)
		if err != nil || !cmResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func cmFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := cmResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("cm: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("cm: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
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
		return nil, "", fmt.Errorf("cm: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode != http.StatusOK {
		return nil, contentType, fmt.Errorf("cm: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, cmMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("cm: read resource %s: %w", resolved, err)
	}
	if len(body) > cmMaxResourceBytes {
		return nil, contentType, fmt.Errorf("cm: resource %s exceeds %d bytes", resolved, cmMaxResourceBytes)
	}
	return body, contentType, nil
}

func cmResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if cmResourceShellMarkerRe.Match(probe) {
		return false
	}
	if image {
		return strings.HasPrefix(mediaType, "image/") && cmIsImageMagic(cmResourceMagic(body))
	}

	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return cmResourceMagic(body) == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return cmResourceMagic(body) == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return cmResourceMagic(body) == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		magic := cmResourceMagic(body)
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return cmResourceMagic(body) != ""
	}
}

func cmResourceMagic(body []byte) string {
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
	default:
		return ""
	}
}

func cmIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff":
		return true
	default:
		return false
	}
}

func cmSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		if resolved := cmResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved := cmResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="cm-content-root">` + article.ContentHTML + `</div>`))
	if err != nil {
		return
	}
	content := doc.Find("#cm-content-root").First()
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "orisrc", "data-src", "data-original", "data-lazy-src"} {
			if candidate := cmResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				resolved = candidate
				break
			}
		}
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"orisrc", "data-src", "data-original", "data-lazy-src"} {
			image.RemoveAttr(attr)
		}
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		attachmentLike := cmIsAttachmentLink(href, link) || (bodaIsPseudoResourceHref(href) && strings.Contains(strings.ToLower(href), "download"))
		if !attachmentLike {
			return
		}
		resolved := cmResourceURL(baseURL, href)
		if !validAttachments[resolved] {
			link.Remove()
			return
		}
		link.SetAttr("href", resolved)
	})
	if html, err := content.Html(); err == nil {
		article.ContentHTML = html
	}
}

func cmRepairListMetadata(items []model.NewsItem, baseURL string) error {
	for i := range items {
		if !cmListItemNeedsDetail(items[i]) {
			continue
		}
		body, err := fetch(items[i].URL)
		if err != nil {
			return fmt.Errorf("cm: fetch detail for list item %q: %w", items[i].URL, err)
		}
		article, err := parseAdapter_cmArticle(body, baseURL)
		if err != nil {
			return fmt.Errorf("cm: parse detail for list item %q: %w", items[i].URL, err)
		}
		if cmTitleIsTruncated(items[i].Title) && article.Title != "" && !cmTitleIsTruncated(article.Title) {
			items[i].Title = article.Title
		}
		if parseDate(items[i].Date).IsZero() {
			items[i].Date = article.Date
		}
		if items[i].PublishedAt == "" {
			items[i].PublishedAt = article.PublishedAt
		}
		if items[i].Type == "" {
			items[i].Type = article.Type
		}
		if parseDate(items[i].Date).IsZero() {
			return fmt.Errorf("cm: detail for list item %q has no complete publication date", items[i].URL)
		}
	}
	return nil
}

func cmListItemNeedsDetail(item model.NewsItem) bool {
	return cmTitleIsTruncated(item.Title) || parseDate(item.Date).IsZero() || strings.TrimSpace(item.Type) == ""
}

func cmTitleIsTruncated(title string) bool {
	title = strings.TrimSpace(title)
	return strings.HasSuffix(title, "...") || strings.HasSuffix(title, "…")
}

// cmExternalListRowCount counts list rows whose only article target links
// off-site (e.g. WeChat reposts of job notices on the cpyc and xyyw feeds).
// parseAdapter_cmList deliberately skips those rows — they cannot be handed
// to the CM article parser — while keeping the local rows on the same page,
// and treats a page whose rows are all external as an empty feed. The count
// is what distinguishes that case from a genuinely broken or empty list.
func cmExternalListRowCount(html []byte, baseURL string) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}
	count := 0
	doc.Find(".n_list > ul > li, .n_listTxt > ul > li").Each(func(_ int, row *goquery.Selection) {
		if cmArticleAnchor(row, baseURL) != nil {
			return
		}
		row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
			if isInNavArea(anchor) || strings.TrimSpace(anchor.Text()) == "" && strings.TrimSpace(anchor.AttrOr("title", "")) == "" {
				return true
			}
			resolved := cleanURL(resolveURL(baseURL, strings.TrimSpace(anchor.AttrOr("href", ""))))
			parsed, parseErr := url.Parse(resolved)
			if parseErr == nil && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) && parsed.Hostname() != "" {
				base, baseErr := url.Parse(baseURL)
				if baseErr == nil && !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
					count++
				}
			}
			return false
		})
	})
	return count
}

func cmPageCountFromHTML(html []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}

	// Prefer an explicit page counter if a particular CM template provides one.
	for _, text := range []string{doc.Find(".p_t").Text(), doc.Find(".p_pages").Text()} {
		if match := cmPageCountRe.FindStringSubmatch(strings.Join(strings.Fields(text), "")); match != nil {
			if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
				return total
			}
		}
	}

	// On the current site the displayed number is the logical page, while the
	// href is the reverse generated filename. Read the text from p_no instead
	// of interpreting the href, including the last visible number.
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, page *goquery.Selection) {
		value, err := strconv.Atoi(strings.TrimSpace(page.Text()))
		if err == nil && value > maxPage {
			maxPage = value
		}
	})
	if maxPage > 0 {
		return maxPage
	}

	// Do not guess from total rows and the current response's row count. A
	// WAF/login/static response can contain a few list-looking rows without
	// being a complete paginated feed; source pagination markup is authoritative.
	return 0
}

func parseAdapter_cmList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse cm list: %w", err)
	}

	// Ordinary list pages use n_list. The two 师生风采 child pages use the
	// older n_listTxt name. Scoping to these containers is what keeps the
	// navigation links (including the mobile 首页 h1) out of the feed.
	if list := doc.Find(".n_list > ul").First(); list.Length() > 0 {
		items := parseCmListRows(list.ChildrenFiltered("li"), cmPageType(doc), baseURL, true)
		if len(items) == 0 {
			if cmExternalListRowCount(html, baseURL) > 0 {
				// The list is real but every row links off-site; nothing here
				// can be collected, so report a valid empty feed.
				return []model.NewsItem{}, nil
			}
			return nil, fmt.Errorf("cm list contains no article entries")
		}
		return items, nil
	}
	if list := doc.Find(".n_listTxt > ul").First(); list.Length() > 0 {
		// CM's legacy 师生风采 template currently emits the same stale
		// 2023-04-12 value for every row. The detail page has the real
		// publication date, so do not expose this list-only placeholder.
		items := parseCmListRows(list.ChildrenFiltered("li"), cmPageType(doc), baseURL, false)
		if len(items) == 0 {
			if cmExternalListRowCount(html, baseURL) > 0 {
				return []model.NewsItem{}, nil
			}
			return nil, fmt.Errorf("cm list contains no article entries")
		}
		return items, nil
	}

	// index.htm is an aggregation page. Each home section has its own heading;
	// collect every article link from all sections instead of only the first
	// visible list. The calendar is populated from /bless/data1.json and its
	// complete article list is exposed separately as index/yxrl.
	if cmHasHomeLayout(doc) {
		items := parseCmHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("cm home page contains no article entries")
		}
		return items, nil
	}

	return nil, fmt.Errorf("cm page is not a supported article list")
}

func parseCmListRows(rows *goquery.Selection, pageType, baseURL string, includeDates bool) []model.NewsItem {
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := cmArticleAnchor(row, baseURL)
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		fullURL, ok := cmArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}

		title := cmListTitle(row, anchor)
		if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 160 {
			return
		}
		date, publishedAt := "", ""
		if includeDates {
			date, publishedAt = cmDateFromScope(row.Find("time, .time, .date").First())
		}

		seen[fullURL] = true
		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     pageType,
			ParsedAt: time.Now(),
		}
		if cmHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}
		items = append(items, item)
	})
	return items
}

func cmArticleAnchor(scope *goquery.Selection, baseURL string) *goquery.Selection {
	if scope == nil || scope.Length() == 0 {
		return nil
	}
	var result *goquery.Selection
	scope.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := cmArticleURL(baseURL, anchor.AttrOr("href", "")); !ok {
			return true
		}
		if result == nil {
			result = anchor
		}
		// A thumbnail wrapper can precede the headline in the same row.
		if strings.TrimSpace(anchor.AttrOr("title", "")) != "" || strings.TrimSpace(anchor.Text()) != "" {
			result = anchor
			return false
		}
		return true
	})
	return result
}

func cmListTitle(row, anchor *goquery.Selection) string {
	if title := strings.TrimSpace(anchor.AttrOr("title", "")); title != "" {
		return cleanTitle(title)
	}
	for _, selector := range []string{"h2", "h3", "h4", ".title", ".aline_r", ".eclip"} {
		if title := strings.TrimSpace(row.Find(selector).First().Text()); title != "" {
			return cleanTitle(title)
		}
	}
	return cleanTitle(strings.TrimSpace(anchor.Text()))
}

func cmPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if title := strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")); title != "" {
		return title
	}
	return strings.TrimSpace(doc.Find(".ny-tit h4").First().Text())
}

func cmHasHomeLayout(doc *goquery.Document) bool {
	return doc.Find("section.homea, section.homeb, section.homec, section.homed, section.homee").Length() > 0
}

func parseCmHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	// These selectors correspond to the site's current server-rendered home
	// sections. The article URL filter below also makes this safe if the site
	// adds ordinary links to one of the sections later.
	selectors := []string{
		"section.homea a[href]",
		"section.homeb a[href]",
		"section.homec a[href]",
		"section.homed a[href]",
	}
	for _, selector := range selectors {
		doc.Find(selector).Each(func(_ int, anchor *goquery.Selection) {
			if isInNavArea(anchor) {
				return
			}
			fullURL, ok := cmArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok {
				return
			}
			title := cmListTitle(anchor, anchor)
			if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 160 {
				return
			}
			date, publishedAt := cmDateFromScope(anchor.Find("time").First())
			item := model.NewsItem{
				Title:    title,
				URL:      fullURL,
				Date:     date,
				Type:     cmHomeType(anchor),
				ParsedAt: time.Now(),
			}
			if cmHasClock(publishedAt) {
				item.PublishedAt = publishedAt
			}
			cmAddHomeItem(&items, seen, item)
		})
	}
	return items
}

func cmHomeType(anchor *goquery.Selection) string {
	for _, selector := range []string{".homea .left", ".homea .right", ".homeb .wp", ".homec .item"} {
		container := anchor.Closest(selector)
		if container.Length() == 0 {
			continue
		}
		for _, heading := range []string{".title001 h2", ".tit002 h2"} {
			if value := strings.TrimSpace(container.Find(heading).First().Text()); value != "" {
				return value
			}
		}
	}
	if container := anchor.Closest(".homed .list2w1, .homed .list2w2"); container.Length() > 0 {
		if container.HasClass("list2w1") {
			return "学生风采"
		}
		return "教师风采"
	}
	return "师生风采"
}

func cmAddHomeItem(items *[]model.NewsItem, seen map[string]int, item model.NewsItem) {
	if index, ok := seen[item.URL]; ok {
		current := &(*items)[index]
		if current.Date == "" && item.Date != "" {
			current.Date = item.Date
			current.PublishedAt = item.PublishedAt
		}
		if current.Type == "" && item.Type != "" {
			current.Type = item.Type
		}
		return
	}
	seen[item.URL] = len(*items)
	*items = append(*items, item)
}

func cmDateFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}

	// The notice card on the home page separates the year and month/day into
	// <time><span>2026</span><em>07-08</em></time>.
	year := strings.TrimSpace(scope.Find("span").First().Text())
	monthDay := strings.TrimSpace(scope.Find("em").First().Text())
	if cmYearRe.MatchString(year) && cmShortDateRe.MatchString(monthDay) {
		return cmDateValues(year + "-" + monthDay)
	}
	return cmDateValues(strings.TrimSpace(scope.Text()))
}

func cmDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(raw), " ")
	if raw == "" {
		return "", ""
	}
	if match := cmDateTimeRe.FindStringSubmatch(raw); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		if date == "" {
			return "", ""
		}
		second := match[6]
		if second == "" {
			second = "00"
		}
		return date, fmt.Sprintf("%s %02s:%s:%s", date, match[4], match[5], second)
	}
	if normalized := normalizePublishedAt(raw); normalized != "" {
		if cmHasClock(raw) || strings.Contains(raw, "分") || strings.Contains(raw, "秒") {
			return normalized[:10], normalized
		}
		return normalized[:10], ""
	}
	if match := cmDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	if match := cmCompactDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	if cmShortDateRe.MatchString(raw) {
		return strings.ReplaceAll(strings.ReplaceAll(raw, "/", "-"), ".", "-"), ""
	}
	return "", ""
}

func cmHasClock(value string) bool {
	return cmClockRe.MatchString(value)
}

func cmArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !cmArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func cmResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := bodaResourceURL(baseURL, raw)
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return ""
	}
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{"/captcha", "/verify", "/login", "/signin", "/placeholder", "/transparent", "/spacer", "/blank", "/loading", "/pixel.gif"} {
		if strings.Contains(resourceText, marker) {
			return ""
		}
	}
	return cleanURL(parsed.String())
}

func parseAdapter_cmArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse cm article: %w", err)
	}

	titleSel := doc.Find(".dtl-tit h4").First()
	if titleSel.Length() == 0 || strings.TrimSpace(titleSel.Text()) == "" {
		return nil, fmt.Errorf("cm page is not an article: missing .dtl-tit h4")
	}
	contentSel := doc.Find(".dtl-txt .v_news_content").First()
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".dtl-txt #vsb_content, .dtl-txt").First()
	}
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("cm article has no content container")
	}

	article := &model.Article{
		Title: strings.TrimSpace(titleSel.Text()),
		Type:  cmArticleType(doc),
	}
	cmFillArticleMeta(article, doc)

	article.Content = cmContentText(contentSel)
	cmAppendImages(article, contentSel, baseURL)
	detail := doc.Find(".detail").First()
	if detail.Length() == 0 {
		detail = doc.Selection
	}
	cmAppendAttachments(article, detail, baseURL)
	cmAppendPrevNext(article, detail, baseURL)
	if html, htmlErr := contentSel.Html(); htmlErr == nil {
		article.ContentHTML = html
	}

	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("cm article has empty content")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func cmArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if value := strings.TrimSpace(doc.Find(".ny-tit h4").First().Text()); value != "" {
		return value
	}
	var value string
	doc.Find(".position a").Each(func(_ int, link *goquery.Selection) {
		text := strings.TrimSpace(link.Text())
		if text != "" && text != "首页" && text != "正文" {
			value = text
		}
	})
	return value
}

func cmFillArticleMeta(article *model.Article, doc *goquery.Document) {
	var date, publishedAt string
	doc.Find(".dtl-tit .dtl-ar span, .dtl-tit .dtl-ar time, .dtl-tit .dtl-ar dd").Each(func(_ int, field *goquery.Selection) {
		text := strings.TrimSpace(field.Text())
		switch {
		case strings.HasPrefix(text, "发布者：") || strings.HasPrefix(text, "发布者:"):
			article.Author = cmMetaValue(text, "发布者")
		case strings.HasPrefix(text, "作者：") || strings.HasPrefix(text, "作者:"):
			article.Author = cmMetaValue(text, "作者")
		case strings.HasPrefix(text, "摄影：") || strings.HasPrefix(text, "摄影:"):
			article.Photographer = cmMetaValue(text, "摄影")
		case strings.HasPrefix(text, "编辑：") || strings.HasPrefix(text, "编辑:"):
			article.Editor = cmMetaValue(text, "编辑")
		case strings.HasPrefix(text, "来源：") || strings.HasPrefix(text, "来源:"):
			article.Source = cmMetaValue(text, "来源")
		case strings.HasPrefix(text, "发布时间：") || strings.HasPrefix(text, "发布时间:"):
			date, publishedAt = cmDateValues(cmMetaValue(text, "发布时间"))
		case strings.HasPrefix(text, "日期：") || strings.HasPrefix(text, "日期:"):
			date, publishedAt = cmDateValues(cmMetaValue(text, "日期"))
		}
	})

	if date == "" {
		for _, selector := range []string{
			`meta[name="publishdate"]`,
			`meta[name="publish_date"]`,
			`meta[property="article:published_time"]`,
		} {
			value := strings.TrimSpace(doc.Find(selector).First().AttrOr("content", ""))
			if value == "" {
				continue
			}
			date, publishedAt = cmDateValues(value)
			if date != "" {
				break
			}
		}
	}
	article.Date = date
	article.PublishedAt = publishedAt
}

func cmMetaValue(text, label string) string {
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, label+"："), label+":"))
	return value
}

func cmContentText(content *goquery.Selection) string {
	return extractBodaContentText(content)
}

func cmAppendImages(article *model.Article, content *goquery.Selection, baseURL string) {
	seen := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "orisrc", "data-src", "data-original", "data-lazy-src"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				if strings.Contains(strings.ToLower(value), "cm.swjtu.edu.cnnull") {
					continue
				}
				if candidate := cmResourceURL(baseURL, value); candidate != "" {
					resolved = candidate
					break
				}
			}
		}
		if resolved == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Images = append(article.Images, resolved)
		image.SetAttr("src", resolved)
	})
}

func cmAppendAttachments(article *model.Article, detail *goquery.Selection, baseURL string) {
	if detail == nil || detail.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	detail.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if bodaIsPseudoResourceHref(href) || !cmIsAttachmentLink(href, link) {
			return
		}
		resolved := cmResourceURL(baseURL, href)
		if resolved == "" || seen[resolved] {
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
			if parsed, err := url.Parse(resolved); err == nil {
				name = strings.TrimPrefix(parsed.Path, "/")
			}
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	})
}

func cmIsAttachmentLink(href string, link *goquery.Selection) bool {
	if href == "" {
		return false
	}
	if strings.TrimSpace(link.AttrOr("download", "")) != "" {
		return true
	}
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, extension := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(lower, extension) {
			return true
		}
	}
	return false
}

func cmAppendPrevNext(article *model.Article, detail *goquery.Selection, baseURL string) {
	detail.Find(".p-page a").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		text := strings.TrimSpace(link.Text())
		parentText := strings.TrimSpace(link.Parent().Text())
		// A detail parser does not receive the current article URL, so do not
		// manufacture a wrong root-relative URL for CM's 31301.htm links.
		resolved := ""
		if strings.HasPrefix(href, "/") || strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
			resolved = resolveURL(baseURL, href)
		}
		if strings.Contains(parentText, "上一篇") {
			article.PrevURL, article.PrevTitle = resolved, text
		} else if strings.Contains(parentText, "下一篇") {
			article.NextURL, article.NextTitle = resolved, text
		}
	})
}
