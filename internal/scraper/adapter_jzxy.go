package scraper

import (
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

const (
	jzxyCanonicalBaseURL = "https://jzxy.swjtu.edu.cn"
	jzxyHost             = "jzxy.swjtu.edu.cn"
)

// These are the public article feeds found by walking the site's navigation,
// information-center tabs, home-page modules, and the alumni sub-tabs. Static
// introductions, faculty/profile pages, and the external-only party download
// page are deliberately not registered as article feeds.
var jzxySubPages = []string{
	"xxzx/xyxw",         // 学院新闻
	"xxzx/xygg",         // 学院公告
	"xxzx/bkstz",        // 本科生通知
	"xxzx/yjstz",        // 研究生通知
	"xxzx/jyxx",         // 就业信息
	"xxzx/xskj",         // 学生空间
	"xxzx/xshdyg",       // 学术活动预告
	"xxzx/xzzx",         // 下载中心
	"djgz/djtz",         // 党建通知
	"djgz/djxw",         // 党建新闻
	"djgz/jwgz",         // 纪委工作
	"djgz/xxyd",         // 学习园地
	"hzjl/hzxm",         // 合作项目
	"hzjl/jldt",         // 交流动态
	"xsgz/xsgzzd",       // 学生工作制度
	"xsgz/bgxz",         // 表格下载（当前为空栏）
	"xygz",              // 校友工作
	"xygz/xyhd",         // 校友活动
	"xygz/byly",         // 毕业留影
	"xygz/jcxy/tysqxy",  // 唐院时期校友
	"xygz/jcxy/cdbxhxy", // 成都办学后校友
	"zskj",              // 展示空间
}

var jzxyArticlePathRE = regexp.MustCompile(`^/info/[0-9]+/[0-9]+\.html?$`)
var jzxyYearMonthRE = regexp.MustCompile(`(20[0-9]{2})\s*[./-]\s*([0-9]{1,2})`)
var jzxyDateLabelRE = regexp.MustCompile(`(?:日期|发布时间|发布日期|发表时间)\s*[:：]\s*([^\s<]+(?:\s+[0-9]{1,2}:[0-9]{2}(?::[0-9]{2})?)?)`)

// jzxy's generated files count backwards: logical page 2 is total-1.htm and
// the oldest logical page is 1.htm. index.htm is the home-page feed.
func init() {
	if site := AllSites["jzxy"]; site != nil {
		site.BaseURL = jzxyCanonicalBaseURL
	}
	registerAdapter("jzxy", SiteAdapter{
		SubPages:     jzxySubPages,
		ParseList:    parseAdapter_jzxyList,
		ParseArticle: parseAdapter_jzxyArticle,
		PageURL:      jzxyPageURL,
		TotalPages:   jzxyTotalPages,
		FetchArticle: jzxyFetchArticle,
	})
}

func jzxyCanonicalBase(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() != jzxyHost {
		return baseURL
	}
	u.Scheme = "https"
	u.Host = jzxyHost
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	if u.Path == "" {
		return u.Scheme + "://" + u.Host
	}
	return strings.TrimRight(u.String(), "/")
}

func jzxyCanonicalURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.EqualFold(u.Hostname(), jzxyHost) {
		u.Scheme = "https"
		u.Host = jzxyHost
	}
	u.Fragment = ""
	return u.String()
}

func jzxyPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = jzxyCanonicalBase(baseURL)
	extension = strings.TrimSpace(extension)
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		return ""
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, strings.Trim(slug, "/"), extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	physicalPage := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, strings.Trim(slug, "/"), physicalPage, extension)
}

func jzxyTotalPages(slug string) (int, error) {
	listURL := jzxyPageURL(jzxyCanonicalBaseURL, slug, 1, 0, ".htm")
	body, err := fetch(listURL)
	if err != nil {
		listURL = jzxyPageURL(jzxyCanonicalBaseURL, slug, 1, 0, ".html")
		body, err = fetch(listURL)
	}
	if err != nil {
		return 0, err
	}
	doc, err := jzxyDocument(body)
	if err != nil {
		return 0, err
	}
	if err := jzxyCheckResponse(doc); err != nil {
		return 0, err
	}
	if strings.TrimSpace(jzxyPageType(doc)) != "2" {
		return 0, fmt.Errorf("jzxy %s is not an article list page", slug)
	}
	if doc.Find(".notices .list ul").Length() == 0 && doc.Find("#masonry").Length() == 0 {
		return 0, fmt.Errorf("jzxy %s has no article list shell", slug)
	}
	if pages := jzxyPageCount(doc); pages > 0 {
		return pages, nil
	}
	return 0, fmt.Errorf("jzxy %s has no recognized pagination", slug)
}

func jzxyPageCount(doc *goquery.Document) int {
	maxPage := 0
	doc.Find(".pages .p_pages .p_no, .pages .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		value, err := strconv.Atoi(strings.TrimSpace(sel.Text()))
		if err == nil && value > maxPage {
			maxPage = value
		}
	})
	if maxPage == 0 && (doc.Find(".notices .list ul").Length() > 0 || doc.Find("#masonry").Length() > 0) {
		return 1
	}
	return maxPage
}

func parseAdapter_jzxyList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := jzxyDocument(raw)
	if err != nil {
		return nil, err
	}
	if err := jzxyCheckResponse(doc); err != nil {
		return nil, err
	}
	pageType := jzxyPageType(doc)
	if pageType == "1" {
		return jzxyHomeItems(doc, jzxyCanonicalBase(baseURL)), nil
	}
	if pageType != "2" {
		return nil, fmt.Errorf("jzxy response is not a home or article list page (pageType=%q)", pageType)
	}
	if doc.Find("#masonry").Length() > 0 {
		return jzxyGalleryItems(doc, jzxyCanonicalBase(baseURL)), nil
	}
	notices := doc.Find(".notices").First()
	list := notices.Find(".list > ul").First()
	if list.Length() == 0 {
		list = notices.Find(".list ul").First()
	}
	if notices.Length() == 0 || list.Length() == 0 {
		return nil, fmt.Errorf("jzxy response is not an article list: missing .notices .list ul")
	}

	baseURL = jzxyCanonicalBase(baseURL)
	typ := jzxyText(notices.Find(".title .tit").First().Text())
	if typ == "" {
		typ = jzxyText(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	}
	items := make([]model.NewsItem, 0, list.Children().Length())
	seen := make(map[string]bool)
	list.Children().Each(func(_ int, row *goquery.Selection) {
		item, ok := jzxyListItem(row, baseURL, typ)
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	})
	return items, nil
}

func jzxyHomeItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	add := func(scope *goquery.Selection, typ string, includeDate bool) {
		if scope == nil || scope.Length() == 0 {
			return
		}
		a := jzxyFirstArticleAnchor(scope, baseURL)
		if a.Length() == 0 {
			return
		}
		item, ok := jzxyHomeItem(a, scope, baseURL, typ, includeDate)
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	}

	// The desktop and mobile hero carousels contain additional internal
	// articles which are not repeated in the news/notice modules. The external
	// WeChat slide is intentionally rejected by jzxyArticleURL.
	doc.Find(".banner .list .swiper-slide, .wap-bann .swiper-slide").Each(func(_ int, sel *goquery.Selection) {
		add(sel, "", false)
	})

	// News contains one feature, two side cards, and three lower cards. The
	// more-news anchor is a navigation link and cannot pass the /info/ filter.
	doc.Find(".news .intr").Each(func(_ int, sel *goquery.Selection) {
		add(sel, "学院新闻", true)
	})
	doc.Find(".news .side .item").Each(func(_ int, sel *goquery.Selection) {
		add(sel, "学院新闻", true)
	})
	doc.Find(".news > .wid > .list .item").Each(func(_ int, sel *goquery.Selection) {
		add(sel, "学院新闻", true)
	})

	// The three notice lists are hidden behind tabs in the HTML, but all three
	// are server-rendered and must be traversed.
	tabNames := make([]string, 0, 3)
	doc.Find(".notice .tabs li").Each(func(_ int, sel *goquery.Selection) {
		tabNames = append(tabNames, jzxyText(sel.Text()))
	})
	doc.Find(".notice .content > .list").Each(func(index int, sel *goquery.Selection) {
		typ := "通知公告"
		if index < len(tabNames) && tabNames[index] != "" {
			typ = tabNames[index]
		}
		sel.Find(".item").Each(func(_ int, row *goquery.Selection) {
			add(row, typ, true)
		})
	})

	// The displayed 2026.04.24-25 value is the event date, not publication
	// metadata, so the home card intentionally has no date.
	doc.Find(".activity .intr li, .activity .intr .swiper-slide").Each(func(_ int, sel *goquery.Selection) {
		add(sel, "学术活动", false)
	})
	return items
}

func jzxyGalleryItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("#masonry .box").Each(func(_ int, row *goquery.Selection) {
		a := jzxyFirstArticleAnchor(row, baseURL)
		if a.Length() == 0 {
			return
		}
		item, ok := jzxyMakeItem(a, row, baseURL, "毕业留影", true)
		if !ok || seen[item.URL] {
			return
		}
		seen[item.URL] = true
		items = append(items, item)
	})
	return items
}

func jzxyListItem(row *goquery.Selection, baseURL, typ string) (model.NewsItem, bool) {
	a := jzxyFirstArticleAnchor(row, baseURL)
	if a.Length() == 0 {
		return model.NewsItem{}, false
	}
	return jzxyMakeItem(a, row, baseURL, typ, true)
}

func jzxyHomeItem(a, scope *goquery.Selection, baseURL, typ string, includeDate bool) (model.NewsItem, bool) {
	return jzxyMakeItem(a, scope, baseURL, typ, includeDate)
}

func jzxyMakeItem(a, scope *goquery.Selection, baseURL, typ string, includeDate bool) (model.NewsItem, bool) {
	href, _ := a.Attr("href")
	fullURL, ok := jzxyArticleURL(baseURL, href)
	if !ok {
		return model.NewsItem{}, false
	}
	title := jzxyText(a.Text())
	if title == "" {
		title = jzxyText(a.AttrOr("title", ""))
	}
	if title == "" {
		title = jzxyText(scope.Find(".labels .tit a, .text .tit a, .tit a").First().Text())
	}
	if title == "" {
		title = jzxyText(a.Find("img").AttrOr("alt", ""))
	}
	if !jzxyValidTitle(title) || title == "组织简介" || title == "网上监督举报方式" {
		return model.NewsItem{}, false
	}
	date, publishedAt := "", ""
	if includeDate {
		date, publishedAt = jzxyDateFromScope(scope)
	}
	return model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        jzxyText(typ),
		ParsedAt:    time.Now(),
	}, true
}

func jzxyFirstArticleAnchor(scope *goquery.Selection, baseURL string) *goquery.Selection {
	var found *goquery.Selection
	scope.Find("a[href]").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if _, ok := jzxyArticleURL(baseURL, sel.AttrOr("href", "")); ok {
			found = sel
			return false
		}
		return true
	})
	if found == nil {
		return scope.First().Find("a[href]").First()
	}
	return found
}

func jzxyDateFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}
	for _, selector := range []string{".time span", ".time", ".more", ".date", "time"} {
		value := jzxyText(scope.Find(selector).First().Text())
		if date, published := jzxyDateValue(value); date != "" {
			return date, published
		}
	}
	day := jzxyText(scope.Find(".time .day, .day").First().Text())
	month := jzxyText(scope.Find(".time .month, .month").First().Text())
	if day != "" && month != "" {
		if match := jzxyYearMonthRE.FindStringSubmatch(month); match != nil {
			if dayNumber, err := strconv.Atoi(strings.TrimSpace(day)); err == nil && dayNumber >= 1 && dayNumber <= 31 {
				date := fmt.Sprintf("%s-%02s-%02d", match[1], match[2], dayNumber)
				return date, date
			}
		}
	}
	return "", ""
}

func jzxyDateValue(value string) (string, string) {
	value = jzxyText(value)
	if value == "" {
		return "", ""
	}
	if published := normalizePublishedAt(value); published != "" {
		return published[:10], published
	}
	if match := jzxyDateLabelRE.FindStringSubmatch(value); match != nil {
		if published := normalizePublishedAt(match[1]); published != "" {
			return published[:10], published
		}
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		if date := normalizeDateParts(match[1], match[2], match[3]); date != "" {
			return date, date
		}
	}
	return "", ""
}

func parseAdapter_jzxyArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := jzxyDocument(raw)
	if err != nil {
		return nil, err
	}
	if err := jzxyCheckResponse(doc); err != nil {
		return nil, err
	}
	if jzxyPageType(doc) != "3" {
		return nil, fmt.Errorf("jzxy response is not an article page (pageType=%q)", jzxyPageType(doc))
	}
	titleSel := doc.Find(".details .tit h5").First()
	bodySel := doc.Find(".details .content .v_news_content").First()
	if titleSel.Length() == 0 || !jzxyValidTitle(jzxyText(titleSel.Text())) {
		return nil, fmt.Errorf("jzxy article page has no complete title")
	}
	if bodySel.Length() == 0 || (jzxyText(bodySel.Text()) == "" && bodySel.Find("img").Length() == 0) {
		return nil, fmt.Errorf("jzxy article page has no article body")
	}

	baseURL = jzxyCanonicalBase(baseURL)
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = jzxyText(titleSel.Text())
	article.Type = jzxyArticleType(doc)
	if article.Type == "" {
		return nil, fmt.Errorf("jzxy article page has no breadcrumb type")
	}
	// The shared parser has broad legacy date fallbacks. For jzxy, only the
	// fixed metadata line in .details .tit is authoritative; never retain a
	// date inferred from body text or a system/footer node.
	article.Date = ""
	article.PublishedAt = ""
	if date, published := jzxyDateFromArticleMeta(doc); date != "" {
		article.Date = date
		article.PublishedAt = published
	}
	for i := range article.Images {
		article.Images[i] = jzxyCanonicalURL(article.Images[i])
	}
	jzxyCollectAttachments(doc, baseURL, article)
	return article, nil
}

// jzxyFetchArticle is the live detail path. The HTML-only parser remains
// deterministic for fixtures, while this path verifies every image and
// attachment against the source response before exposing it to callers.
func jzxyFetchArticle(articleURL string) (*model.Article, []byte, error) {
	canonicalURL, ok := jzxyArticleURL(jzxyCanonicalBaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("jzxy: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonicalURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_jzxyArticle(body, jzxyCanonicalBaseURL)
	if err != nil {
		return nil, nil, err
	}

	candidateAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		if resolved, ok := jzxyAttachmentURL(jzxyCanonicalBaseURL, attachment.URL); ok {
			candidateAttachments[resolved] = true
		}
	}
	article.Images = jzxyVerifyImages(article.Images, canonicalURL)
	article.Attachments = jzxyVerifyAttachments(article.Attachments, canonicalURL)
	allowedImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		allowedImages[image] = true
	}
	allowedAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		allowedAttachments[attachment.URL] = true
	}
	article.ContentHTML = jzxySanitizeContentHTML(article.ContentHTML, jzxyCanonicalBaseURL, allowedImages, candidateAttachments, allowedAttachments)
	return article, body, nil
}

func jzxyVerifyImages(images []string, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := bodaResourceURL(jzxyCanonicalBaseURL, raw)
		if resolved == "" {
			continue
		}
		resolved = jzxyCanonicalURL(resolved)
		if seen[resolved] {
			continue
		}
		body, contentType, err := jzxyFetchResource(resolved, referer)
		if err != nil || !jzxyResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func jzxyVerifyAttachments(attachments []model.Attachment, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved, ok := jzxyAttachmentURL(jzxyCanonicalBaseURL, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := jzxyFetchResource(resolved, referer)
		if err != nil || !jzxyResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func jzxyFetchResource(rawURL, referer string) ([]byte, string, error) {
	resolved := bodaResourceURL(jzxyCanonicalBaseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("jzxy: refusing non-http resource %q", rawURL)
	}
	resolved = jzxyCanonicalURL(resolved)
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("jzxy: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/*,application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", referer)
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("jzxy: fetch resource %s: %w", resolved, err)
	}
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK {
		return nil, contentType, fmt.Errorf("jzxy: resource %s: HTTP %d", resolved, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, contentType, fmt.Errorf("jzxy: read resource %s: %w", resolved, err)
	}
	return body, contentType, nil
}

func jzxyResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	probeLen := len(body)
	if probeLen > 4096 {
		probeLen = 4096
	}
	probe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(body[:probeLen]))), "\ufeff")
	for _, marker := range []string{"<!doctype html", "<html", "<head", "<body", "请输入验证码", "验证码下载附件", "captcha", "请先登录", "请登录后", "access denied", "just a moment", "cloudflare", "请求已被阻断", "安全威胁"} {
		if strings.Contains(probe, marker) {
			return false
		}
	}
	if image {
		switch mediaType {
		case "image/png":
			return len(body) >= 8 && string(body[:8]) == "\x89PNG\r\n\x1a\n"
		case "image/jpeg", "image/jpg":
			return len(body) >= 3 && string(body[:3]) == "\xff\xd8\xff"
		case "image/gif":
			return len(body) >= 6 && (string(body[:6]) == "GIF87a" || string(body[:6]) == "GIF89a")
		case "image/webp":
			return len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP"
		case "image/bmp":
			return len(body) >= 2 && string(body[:2]) == "BM"
		case "image/svg+xml":
			return strings.HasPrefix(probe, "<svg") || strings.Contains(probe, "<svg ")
		default:
			return false
		}
	}

	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf":
		return len(body) >= 5 && string(body[:5]) == "%PDF-"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(lowerName, ".zip") || mediaType == "application/zip":
		return len(body) >= 4 && string(body[:4]) == "PK\x03\x04"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword":
		return len(body) >= 8 && string(body[:8]) == "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "application/octet-stream"
	default:
		return mediaType != "" && mediaType != "text/html" && mediaType != "application/xhtml+xml" && mediaType != "application/json" && mediaType != "text/xml" && mediaType != "application/xml"
	}
}

func jzxySanitizeContentHTML(rawHTML, baseURL string, allowedImages, candidateAttachments, allowedAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="jzxy-content-root">` + rawHTML + `</div>`))
	if err != nil {
		return rawHTML
	}
	root := doc.Find("#jzxy-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var resolved string
		for _, attr := range []string{"src", "data-src", "data-original", "orisrc", "vurl"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				if candidate := bodaResourceURL(baseURL, value); candidate != "" {
					resolved = jzxyCanonicalURL(candidate)
					break
				}
			}
		}
		if resolved == "" || !allowedImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "orisrc", "vurl", "vsbhref"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		resolved, ok := jzxyAttachmentURL(baseURL, href)
		if !ok || !candidateAttachments[resolved] {
			return
		}
		if !allowedAttachments[resolved] {
			anchor.RemoveAttr("href")
			return
		}
		anchor.SetAttr("href", resolved)
	})
	result, err := root.Html()
	if err != nil {
		return rawHTML
	}
	return result
}

func jzxyDateFromArticleMeta(doc *goquery.Document) (string, string) {
	meta := jzxyText(doc.Find(".details .tit p").First().Text())
	if meta == "" {
		return "", ""
	}
	if match := dateTimeRe.FindString(meta); match != "" {
		if published := normalizePublishedAt(match); published != "" {
			return published[:10], published
		}
	}
	if match := jzxyDateLabelRE.FindStringSubmatch(meta); match != nil {
		if published := normalizePublishedAt(match[1]); published != "" {
			return published[:10], published
		}
	}
	if match := bareDateRe.FindStringSubmatch(meta); match != nil {
		if date := normalizeDateParts(match[1], match[2], match[3]); date != "" {
			return date, date
		}
	}
	return "", ""
}

func jzxyArticleType(doc *goquery.Document) string {
	typ := ""
	doc.Find(".details .title .page a, .notices .title .page a").Each(func(_ int, sel *goquery.Selection) {
		value := jzxyText(sel.Text())
		if value != "" && value != "首页" && value != "正文" {
			typ = value
		}
	})
	return typ
}

func jzxyCollectAttachments(doc *goquery.Document, baseURL string, article *model.Article) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find(".details ul li a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		name := jzxyText(sel.Text())
		if href == "" || name == "" {
			return
		}
		if !jzxyAttachmentCandidate(sel, href) {
			return
		}
		resolved, ok := jzxyAttachmentURL(baseURL, href)
		if !ok || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func jzxyAttachmentCandidate(sel *goquery.Selection, href string) bool {
	if bodaLooksLikeAttachmentLink(sel, href) {
		return true
	}
	if !strings.Contains(jzxyText(sel.Parent().Text()), "附件") {
		return false
	}
	lowerName := strings.ToLower(jzxyText(sel.Text()))
	lowerHref := strings.ToLower(href)
	for _, ext := range bodaAttachmentExts {
		if strings.HasSuffix(lowerName, ext) || strings.Contains(lowerHref, ext) {
			return true
		}
	}
	return false
}

func jzxyAttachmentURL(baseURL, href string) (string, bool) {
	if bodaIsPseudoResourceHref(href) {
		return "", false
	}
	resolved := jzxyCanonicalURL(resolveURL(jzxyCanonicalBase(baseURL), href))
	u, err := url.Parse(resolved)
	if err != nil || !jzxySameHost(resolved) || u.Path == "" || u.Path == "/" {
		return "", false
	}
	return resolved, true
}

func jzxyArticleURL(baseURL, href string) (string, bool) {
	if strings.TrimSpace(href) == "" {
		return "", false
	}
	resolved := jzxyCanonicalURL(resolveURL(jzxyCanonicalBase(baseURL), href))
	u, err := url.Parse(resolved)
	if err != nil || !jzxySameHost(resolved) || !jzxyArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	return resolved, true
}

func jzxySameHost(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.EqualFold(u.Hostname(), jzxyHost)
}

func jzxyValidTitle(title string) bool {
	title = jzxyText(title)
	if len([]rune(title)) < 2 || strings.HasSuffix(title, ">>") {
		return false
	}
	switch title {
	case "首页", "更多", "+MORE", "尾页", "上页", "下页", "无", "组织简介", "网上监督举报方式":
		return false
	}
	return true
}

func jzxyText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func jzxyDocument(raw []byte) (*goquery.Document, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse jzxy html: %w", err)
	}
	return doc, nil
}

func jzxyPageType(doc *goquery.Document) string {
	return jzxyText(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
}

func jzxyCheckResponse(doc *goquery.Document) error {
	title := strings.ToLower(jzxyText(doc.Find("title").First().Text()))
	pageTitle := strings.ToLower(jzxyText(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")))
	for _, marker := range []string{"系统提示", "附件下载", "access denied", "page not found", "404 not found", "404错误", "403 forbidden", "403错误", "500错误", "安全验证", "请输入验证码", "登录", "请求报文存在语法错误"} {
		if strings.Contains(title, strings.ToLower(marker)) || strings.Contains(pageTitle, strings.ToLower(marker)) {
			return fmt.Errorf("jzxy error or verification response: %s", marker)
		}
	}
	visible := doc.Clone()
	visible.Find("script, style, noscript").Remove()
	text := strings.ToLower(jzxyText(visible.Text()))
	for _, marker := range []string{"请求报文存在语法错误", "页面不存在", "访问被拒绝", "404错误提示", "请输入验证码下载附件", "请先登录"} {
		if strings.Contains(text, strings.ToLower(marker)) {
			return fmt.Errorf("jzxy error or verification response: %s", marker)
		}
	}
	return nil
}
