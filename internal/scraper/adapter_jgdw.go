package scraper

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const jgdwBaseURL = "https://jgdw.swjtu.edu.cn"

// jgdwSubPages is the complete set of article-list routes exposed by the
// site's navigation and homepage.  The three bmgk pages and tjxw are checked
// separately: bmgk is static content and tjxw is a legal empty photo-list
// page, not a missing route.
var jgdwSubPages = []string{
	"djyw",
	"jcdj",
	"zgzj",
	"qngz",
	"index/tzgg",
	"index/tjxw",
}

var (
	jgdwArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	jgdwDateTimeRE    = regexp.MustCompile(`20\d{2}\s*[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}(?:日|号)?(?:\s+|[Tt]+)\d{1,2}:\d{2}(?::\d{2})?`)
	jgdwDateRE        = regexp.MustCompile(`20\d{2}\s*[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}`)
)

// jgdwAdapter is intentionally independent from the generic Boda parser.
// This site uses the same CMS family as several other units, but its list
// rows, article heading, and outside-body attachment block are different.
func init() {
	registerAdapter("jgdw", SiteAdapter{
		SubPages:     jgdwSubPages,
		ParseList:    parseAdapter_jgdwList,
		ParseArticle: parseAdapter_jgdwArticle,
		PageURL:      jgdwPageURL,
		TotalPages:   jgdwTotalPages,
		FetchArticle: jgdwFetchArticle,
	})
}

// jgdwPageURL maps the logical page number (1 = newest) to the generated
// source file.  jgdw numbers generated files backwards:
// slug.htm, slug/(N-1).htm, ..., slug/1.htm.
func jgdwPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if baseURL == "" {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		return ""
	}
	if slug == "" {
		slug = "index"
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

func jgdwTotalPages(slug string) (int, error) {
	listURL := jgdwPageURL(jgdwBaseURL, slug, 1, 0, ".htm")
	body, err := fetch(listURL)
	if err != nil {
		return 0, fmt.Errorf("fetch jgdw first page %q: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("parse jgdw first page %q: %w", slug, err)
	}
	if !jgdwIsHomepage(doc) && !jgdwIsListPage(doc) {
		return 0, fmt.Errorf("jgdw page %q is not a supported list shell", slug)
	}
	total := parseTotalPages(body)
	if total < 1 {
		// A recognized one-page list, including the legal empty tjxw page, must
		// still expose a pagination shell. A short response without one is not
		// enough evidence that older generated pages do not exist.
		if doc.Find(".pagination, .p_pages, .paging").Length() > 0 {
			return 1, nil
		}
		return 0, fmt.Errorf("jgdw: %s has no recognized pagination", slug)
	}
	return total, nil
}

func parseAdapter_jgdwList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse jgdw list: %w", err)
	}
	if jgdwErrorDocument(doc) {
		return nil, fmt.Errorf("jgdw list is an error, login, or verification page")
	}

	if jgdwIsHomepage(doc) {
		return jgdwParseHomepage(doc, baseURL)
	}
	if !jgdwIsListPage(doc) {
		return nil, fmt.Errorf("jgdw page is not a supported article-list shell")
	}

	rows := doc.Find(".text-list > ul > li")
	if rows.Length() == 0 {
		// The recognized tjxw shell is a valid empty list.  Returning an empty
		// slice here is deliberate; an unrecognized page was rejected above.
		return []model.NewsItem{}, nil
	}

	typ := jgdwHeading(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := jgdwArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := jgdwAnchorTitle(anchor)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := jgdwDateFields(row.Text())
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	})
	return items, nil
}

func jgdwIsHomepage(doc *goquery.Document) bool {
	return doc.Find(`meta[name="pageType"]`).AttrOr("content", "") == "1" &&
		doc.Find(".section3 .part3k .listt").Length() > 0 &&
		doc.Find(".tzgglist").Length() > 0
}

func jgdwIsListPage(doc *goquery.Document) bool {
	if doc.Find(`meta[name="pageType"]`).AttrOr("content", "") != "2" {
		return false
	}
	return doc.Find(".ny-right .text-list, .text-list").Length() > 0 &&
		doc.Find(".position h1, .nybannerwz").Length() > 0
}

func jgdwHeading(doc *goquery.Document) string {
	for _, selector := range []string{".ny-right .position h1", ".position h1", ".nybannerwz p"} {
		if text := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); text != "" {
			return text
		}
	}
	return ""
}

func jgdwParseHomepage(doc *goquery.Document, baseURL string) ([]model.NewsItem, error) {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	add := func(scope *goquery.Selection, typ string) {
		scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			fullURL, ok := jgdwArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok || seen[fullURL] {
				return
			}
			title := jgdwAnchorTitle(anchor)
			if title == "" || !isValidNewsTitle(title) {
				return
			}
			row := anchor.Closest("li")
			date, publishedAt := jgdwDateFields(row.Text())
			seen[fullURL] = true
			items = append(items, model.NewsItem{
				Title:       title,
				URL:         fullURL,
				Date:        date,
				PublishedAt: publishedAt,
				Type:        typ,
				ParsedAt:    time.Now(),
			})
		})
	}

	homeTypes := []string{"党建要闻", "基层党建", "职工之家"}
	doc.Find(".section3 .part3k").Each(func(i int, part *goquery.Selection) {
		if i < len(homeTypes) {
			add(part.Find(".listt > ul"), homeTypes[i])
		}
	})
	add(doc.Find(".tzgglist").First(), "通知公告")
	if len(items) == 0 {
		return nil, fmt.Errorf("jgdw homepage contains no scoped article modules")
	}
	return items, nil
}

// jgdwAnchorTitle handles the homepage notice template where the article
// anchor wraps both <div class="title"> and <div class="time">. Reading the
// whole anchor would append the month/day to the headline.
func jgdwAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if title := strings.TrimSpace(anchor.AttrOr("title", "")); title != "" {
		return strings.Join(strings.Fields(title), " ")
	}
	for _, selector := range []string{".title", "h1", "h2", "h3", "h4"} {
		if title := strings.Join(strings.Fields(anchor.Find(selector).First().Text()), " "); title != "" {
			return title
		}
	}
	clone := anchor.Clone()
	clone.Find(".time, .date, time, .p_time").Remove()
	return strings.Join(strings.Fields(clone.Text()), " ")
}

func jgdwArticleURL(baseURL, href string) (string, bool) {
	resolved := resolveURL(baseURL, strings.TrimSpace(href))
	u, err := url.Parse(resolved)
	if err != nil || u.Host == "" || !jgdwArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Host, base.Host) || !strings.EqualFold(u.Scheme, base.Scheme) {
		return "", false
	}
	return cleanURL(u.String()), true
}

func jgdwDateFields(value string) (string, string) {
	value = strings.Join(strings.Fields(value), " ")
	if match := jgdwDateTimeRE.FindString(value); match != "" {
		publishedAt := normalizePublishedAt(match)
		if len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	if match := jgdwDateRE.FindString(value); match != "" {
		date := normalizePublishedAt(match)
		return date, date
	}
	return "", ""
}

func parseAdapter_jgdwArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse jgdw article: %w", err)
	}
	if jgdwErrorDocument(doc) {
		return nil, fmt.Errorf("jgdw article is an error, login, or verification page")
	}
	if doc.Find(`meta[name="pageType"]`).AttrOr("content", "") != "3" {
		return nil, fmt.Errorf("jgdw page is not an article shell")
	}
	content := jgdwArticleContent(doc)
	if doc.Find(".art-tit h3").First().Length() == 0 || content == nil || content.Length() == 0 {
		return nil, fmt.Errorf("jgdw article shell is missing heading or content")
	}
	if strings.TrimSpace(extractBodaContentText(content)) == "" && content.Find("img, table").Length() == 0 {
		return nil, fmt.Errorf("jgdw article content is empty")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse jgdw article fields: %w", err)
	}
	article.Title = jgdwArticleTitle(doc)
	article.Type = jgdwHeading(doc)

	meta := strings.Join(strings.Fields(doc.Find(".art-tit h2").First().Text()), " ")
	article.Date = ""
	article.PublishedAt = ""
	if date, publishedAt := jgdwDateFields(meta); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	article.Source = jgdwMetaValue(meta, "来源")
	if article.Title == "" || article.Type == "" || article.Date == "" {
		return nil, fmt.Errorf("jgdw article is missing fixed title, type, or publication date")
	}
	article.Content = extractBodaContentText(content)
	jgdwNormalizeContentMarkup(content, baseURL)
	if contentHTML, htmlErr := content.Html(); htmlErr == nil {
		article.ContentHTML = contentHTML
	}
	article.Images = jgdwCollectImages(content, baseURL)
	jgdwAppendOutsideAttachments(doc, article, baseURL)
	article.Attachments = jgdwLocalAttachments(article.Attachments, baseURL)
	return article, nil
}

func jgdwArticleTitle(doc *goquery.Document) string {
	return strings.Join(strings.Fields(doc.Find(".art-tit h3").First().Text()), " ")
}

func jgdwMetaValue(meta, label string) string {
	index := strings.Index(meta, label+"：")
	separatorLen := len(label) + len("：")
	if index < 0 {
		index = strings.Index(meta, label+":")
		separatorLen = len(label) + len(":")
	}
	if index < 0 {
		return ""
	}
	value := strings.TrimSpace(meta[index+separatorLen:])
	for _, stop := range []string{"浏览次数", "点击量", "阅读量"} {
		if cut := strings.Index(value, stop); cut >= 0 {
			value = value[:cut]
		}
	}
	return strings.TrimSpace(value)
}

// jgdwArticleContent is deliberately narrower than the shared article parser's
// fallback chain. The live template keeps the article body under #vsb_content;
// accepting an arbitrary .content-main here could turn a shell or footer into
// an article body.
func jgdwArticleContent(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		"#vsb_content .v_news_content",
		".art-body .v_news_content",
		".art-main .v_news_content",
		".v_news_content",
	} {
		content := doc.Find(selector).First()
		if content.Length() > 0 && content.Closest("header, nav, footer, .top, .head, .foot").Length() == 0 {
			return content
		}
	}
	return nil
}

func jgdwCollectImages(content *goquery.Selection, baseURL string) []string {
	images := make([]string, 0)
	seen := make(map[string]bool)
	if content == nil || content.Length() == 0 {
		return images
	}
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if resolved := jgdwResourceURL(baseURL, image.AttrOr(attr, "")); resolved != "" {
				if !seen[resolved] {
					seen[resolved] = true
					images = append(images, resolved)
				}
				return
			}
		}
	})
	return images
}

func jgdwNormalizeContentMarkup(content *goquery.Selection, baseURL string) {
	if content == nil || content.Length() == 0 {
		return
	}
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := jgdwResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				resolved = candidate
				break
			}
		}
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
		if strings.HasPrefix(href, "#") {
			return
		}
		if resolved := jgdwResourceURL(baseURL, href); resolved != "" {
			anchor.SetAttr("href", resolved)
		} else {
			anchor.RemoveAttr("href")
		}
	})
}

func jgdwLocalAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	local := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved, ok := jgdwSameOriginURL(attachment.URL, baseURL)
		if !ok || strings.TrimSpace(attachment.Name) == "" || seen[resolved] {
			continue
		}
		attachment.URL = resolved
		attachment.Name = strings.Join(strings.Fields(html.UnescapeString(attachment.Name)), " ")
		if attachment.Name == "" {
			continue
		}
		seen[resolved] = true
		local = append(local, attachment)
	}
	return local
}

func jgdwAttachmentLink(anchor *goquery.Selection, resolved string) bool {
	if anchor == nil || anchor.Length() == 0 || resolved == "" {
		return false
	}
	lower := strings.ToLower(resolved)
	if strings.TrimSpace(anchor.AttrOr("download", "")) != "" ||
		strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, ext := range bodaAttachmentExts {
		if strings.Contains(strings.ToLower(anchor.AttrOr("name", "")), ext) || strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

func jgdwResourceURL(baseURL, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	u, err := url.Parse(resolved)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	path := strings.ToLower(u.Path + "?" + u.RawQuery)
	for _, marker := range []string{
		"/captcha", "createimage", "verifycode", "verification", "access-denied",
		"/spacer.gif", "/blank.gif", "/transparent.gif", "/loading.gif", "/pixel.gif",
	} {
		if strings.Contains(path, marker) {
			return ""
		}
	}
	u.Fragment = ""
	return u.String()
}

func jgdwErrorDocument(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{
		"404", "403", "500", "access denied", "forbidden", "安全验证", "请输入验证码",
		"验证码下载附件", "附件下载", "just a moment", "cloudflare",
	} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	return false
}

func jgdwFetchArticle(articleURL string) (*model.Article, []byte, error) {
	return jgdwFetchArticleAt(articleURL, jgdwBaseURL)
}

func jgdwFetchArticleAt(articleURL, baseURL string) (*model.Article, []byte, error) {
	canonical, ok := jgdwArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("jgdw: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonical)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_jgdwArticle(body, baseURL)
	if err != nil {
		return nil, body, err
	}
	candidateImages := append([]string(nil), article.Images...)
	candidateAttachments := append([]model.Attachment(nil), article.Attachments...)
	article.Images = jgdwVerifyImages(candidateImages, baseURL, canonical)
	article.Attachments = jgdwVerifyAttachments(candidateAttachments, baseURL, canonical)
	article.ContentHTML = jgdwSanitizeContentHTML(article.ContentHTML, baseURL, article.Images, candidateAttachments, article.Attachments)
	return article, body, nil
}

func jgdwVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := jgdwResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := jgdwFetchResource(resolved, baseURL, referer, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func jgdwVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved, ok := jgdwSameOriginURL(attachment.URL, baseURL)
		if !ok || seen[resolved] {
			continue
		}
		body, _, err := jgdwFetchResourceNamed(resolved, baseURL, referer, attachment.Name, false)
		if err != nil {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func jgdwSanitizeContentHTML(raw, baseURL string, images []string, candidateAttachments, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	validImages := make(map[string]bool, len(images))
	for _, image := range images {
		if resolved := jgdwResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	candidateFiles := make(map[string]bool, len(candidateAttachments))
	for _, attachment := range candidateAttachments {
		if resolved, ok := jgdwSameOriginURL(attachment.URL, baseURL); ok {
			candidateFiles[resolved] = true
		}
	}
	validFiles := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		if resolved, ok := jgdwSameOriginURL(attachment.URL, baseURL); ok {
			validFiles[resolved] = true
		}
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="jgdw-content-root">` + raw + `</div>`))
	if err != nil {
		return ""
	}
	root := doc.Find("#jgdw-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := jgdwResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" && validImages[candidate] {
				resolved = candidate
				break
			}
		}
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vsbhref", "vurl", "vheight", "vwidth"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := html.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
		if strings.HasPrefix(href, "#") {
			return
		}
		resolved := jgdwResourceURL(baseURL, href)
		if resolved == "" {
			anchor.RemoveAttr("href")
			return
		}
		if local, ok := jgdwSameOriginURL(resolved, baseURL); ok && jgdwAttachmentLink(anchor, local) {
			if !candidateFiles[local] || !validFiles[local] {
				anchor.Remove()
				return
			}
			anchor.SetAttr("href", local)
			return
		}
		anchor.SetAttr("href", resolved)
	})
	rendered, err := root.Html()
	if err != nil {
		return ""
	}
	return rendered
}

const jgdwMaxResourceBytes int64 = 32 << 20

func jgdwFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	return jgdwFetchResourceNamed(rawURL, baseURL, referer, rawURL, image)
}

func jgdwFetchResourceNamed(rawURL, baseURL, referer, nameOrURL string, image bool) ([]byte, string, error) {
	resolved := ""
	var ok bool
	if image {
		resolved = jgdwResourceURL(baseURL, rawURL)
		ok = resolved != ""
	} else {
		resolved, ok = jgdwSameOriginURL(rawURL, baseURL)
	}
	if !ok {
		return nil, "", fmt.Errorf("jgdw: refusing invalid resource %q", rawURL)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" || (strings.ToLower(parsed.Scheme) != "http" && strings.ToLower(parsed.Scheme) != "https") {
		return nil, "", fmt.Errorf("jgdw: refusing non-HTTP resource %q", resolved)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("jgdw: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", referer)
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if scheme := strings.ToLower(next.URL.Scheme); scheme != "http" && scheme != "https" {
			return fmt.Errorf("jgdw: refusing non-HTTP resource redirect")
		}
		if !jgdwURLAuthorityEqual(parsed, next.URL) {
			return fmt.Errorf("jgdw: refusing cross-site resource redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("jgdw: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("jgdw: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, jgdwMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("jgdw: read resource %s: %w", resolved, err)
	}
	if strings.TrimSpace(nameOrURL) == "" {
		nameOrURL = resolved
	}
	if int64(len(body)) > jgdwMaxResourceBytes || !jgdwResourceBodyIsUsable(contentType, body, nameOrURL, image) {
		return nil, contentType, fmt.Errorf("jgdw: resource %s returned invalid Content-Type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func jgdwResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
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
	lowerProbe := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(string(probe)), "\ufeff"))
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "请输入验证码", "验证码下载附件",
		"captcha", "createimage", "请先登录", "请登录后", "access denied", "just a moment",
		"cloudflare", "请求已被阻断", "安全威胁", "系统提示",
	} {
		if strings.Contains(lowerProbe, marker) {
			return false
		}
	}

	magic := jgdwResourceMagic(body)
	if image {
		return jgdwImageTypeMatchesMagic(mediaType, magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return (mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf") || mediaType == "application/octet-stream") && magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return (mediaType == "application/octet-stream" || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml")) && magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return (mediaType == "application/octet-stream" || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint") && magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return (mediaType == "application/octet-stream" || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed") && (magic == "zip" || magic == "rar" || magic == "7z")
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "application/octet-stream"
	default:
		return magic != ""
	}
}

func jgdwImageTypeMatchesMagic(mediaType, magic string) bool {
	switch mediaType {
	case "image/jpeg", "image/jpg", "image/pjpeg":
		return magic == "jpeg"
	case "image/png":
		return magic == "png"
	case "image/gif":
		return magic == "gif"
	case "image/webp":
		return magic == "webp"
	case "image/bmp":
		return magic == "bmp"
	case "image/tiff":
		return magic == "tiff"
	case "image/svg+xml":
		return magic == "svg"
	default:
		return false
	}
}

func jgdwResourceMagic(body []byte) string {
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
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	probe := trimmed
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if bytes.Contains(bytes.ToLower(probe), []byte("<svg")) {
		return "svg"
	}
	return ""
}

func jgdwURLAuthorityEqual(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func jgdwAppendOutsideAttachments(doc *goquery.Document, article *model.Article, baseURL string) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved, ok := jgdwSameOriginURL(attachment.URL, baseURL); ok {
			seen[resolved] = true
		}
	}

	doc.Find("form[name='_newscontent_fromname'] li a[href], .art-main > p li a[href]").Each(func(_ int, anchor *goquery.Selection) {
		row := anchor.Closest("li")
		if row.Length() == 0 || !strings.Contains(row.Text(), "附件") {
			return
		}
		href := html.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
		resolved, ok := jgdwSameOriginURL(href, baseURL)
		if !ok || !jgdwAttachmentLink(anchor, resolved) || seen[resolved] {
			return
		}
		name := strings.Join(strings.Fields(html.UnescapeString(anchor.Text())), " ")
		if name == "" {
			name = strings.Join(strings.Fields(anchor.AttrOr("download", "")), " ")
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func jgdwSameOriginURL(value, baseURL string) (string, bool) {
	value = html.UnescapeString(strings.TrimSpace(value))
	resolved := cleanURL(resolveURL(baseURL, value))
	u, err := url.Parse(resolved)
	base, baseErr := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || baseErr != nil || u.Host == "" || base.Host == "" {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, base.Scheme) || !strings.EqualFold(u.Host, base.Host) {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}
