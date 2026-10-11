package scraper

import (
	"bytes"
	"fmt"
	htmlstd "html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// These are the article-list surfaces exposed by the psychology centre's
// homepage tabs.  The homepage itself only shows a small preview; every
// linked list must be crawled as a feed so that older notices are not lost.
var xinliSubPages = []string{
	// Homepage news and announcement tabs.
	"zxyw",
	"tzgg/qb",
	"tzgg/xljkjy",
	"tzgg/dzbg",
	"tzgg/bks",
	"tzgg/yjs",
	"tzgg/kygz",
	"tzgg/xsgz",
	"tzgg/sygz",
	"tzgg/gsgg",

	// Every article-bearing list exposed by the main navigation.  The static
	// profile pages (中心概况 and 师资队伍) are deliberately not feeds.
	"xljkjy/xljkkc",
	"xljkjy/xljkcp",
	"xljkjy/xlzxfw",
	"xljkjy/xlwhhd",
	"xljkjy/shxlfw",
	"rcpy/bksjy",
	"rcpy/yjsjy",
	"kxsj/kydt",
	"kxsj/sysx",
	"dqgz1/djgz/djdt",
	"dqgz1/djhd",
	"dqgz1/jwgz",
	"dqgz1/ghgz",
	"xsgz/xgdw",
	"xsgz/xsfc",
	"xsgz/xshd",
	"xsgz/jyxx",
	"hzjl",
	"stfc/dxsxlxh",
	"stfc/czxh",
	"stfc/yjsxlxh",
	"stfc/xcm",
	"kpzy/spkt",
}

var (
	xinliArticlePathRE     = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	xinliColumnPathRE      = regexp.MustCompile(`(?i)^/info/([0-9]+)/[0-9]+\.html?$`)
	xinliShortDateRE       = regexp.MustCompile(`^(0?[1-9]|1[0-2])[-/.](0?[1-9]|[12][0-9]|3[01])$`)
	xinliChineseDateTimeRE = regexp.MustCompile(`(20\d{2})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*(?:日|号)?\s+(\d{1,2})\s*时\s*(\d{2})\s*分(?:\s*(\d{2})\s*秒)?`)
	xinliResourceMarkerRE  = regexp.MustCompile(`(?i)/(?:spacer|blank|transparent|placeholder|noimage|no-image|loading|captcha|verify|waf|login|access[-_]denied)(?:[./?]|$)|/pixel\.gif(?:[?]|$)|createimage\.jsp`)
	xinliResourceShellRE   = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|您访问的页面不存在|无权限访问|access\s+denied|request\s+blocked|just\s+a\s+moment|cloudflare|please\s+verify|verify\s+you\s+are\s+human|安全验证|安全威胁|请求已被阻断`)
	xinliErrorPageRE       = regexp.MustCompile(`(?i)404\s*错误|页面不存在|您访问的页面不存在|访问被拒绝|请求被拦截|访问频繁|安全验证|人机验证|请输入验证码|验证码下载附件|access\s+denied|request\s+blocked|just\s+a\s+moment|cloudflare|please\s+verify|verify\s+you\s+are\s+human|forbidden|too\s+many\s+requests`)
)

const xinliMaxResourceBytes = 32 << 20

var xinliColumnTypes = map[string]string{
	"1022": "中心要闻",
	"1142": "心理健康课程",
	"1143": "心理健康测评",
	"1144": "心理咨询服务",
	"1147": "心理文化活动",
	"1151": "社会心理服务",
	"1212": "师资队伍",
	"1302": "科研动态",
	"1322": "实验实训",
	"1352": "党建动态",
	"1372": "工会工作",
	"1402": "学工队伍",
	"1412": "学生风采",
	"1452": "合作交流",
	"1042": "党建活动",
	"1047": "学生活动",
	"1502": "心理健康教育",
	"1512": "党政办公",
	"1522": "本科生",
	"1532": "研究生",
	"1542": "科研工作",
	"1552": "学生工作",
	"1562": "实验工作",
	"1572": "公示公告",
	"1622": "视频课堂",
	"1662": "研究生教育",
	"1672": "应用心理学专业",
	"1682": "应用心理学辅修专业",
	"1692": "应用心理学微专业",
	"1702": "国家级与省级一流课程",
	"1712": "教学管理规定",
	"1822": "大学生心理学会",
	"1832": "创造协会",
	"1842": "研究生心理学会",
	"1852": "心传媒",
}

// xinliAdapter gives this website its own extension point.  Although it uses
// the same CMS as several other units, its homepage and list templates have
// different DOM shapes and its generated pages are reverse numbered.
func init() {
	registerAdapter("xinli", SiteAdapter{
		SubPages:     xinliSubPages,
		ParseList:    parseAdapter_xinliList,
		ParseArticle: parseAdapter_xinliArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   xinliTotalPages,
		FetchArticle: xinliFetchArticle,
	})
}

func parseAdapter_xinliList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("xinli: parse list: %w", err)
	}
	if xinliLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("xinli: received an error or verification page instead of a news list")
	}

	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType == "2" || xinliListRegion(doc) != nil {
		list := xinliListRegion(doc)
		if list == nil || list.Length() == 0 {
			return nil, fmt.Errorf("xinli: list page is missing the audited .ej_list shell")
		}
		rows := list.Find("li")
		if rows.Length() == 0 {
			return []model.NewsItem{}, nil
		}
		return xinliCollectRows(rows, baseURL, xinliListType(doc)), nil
	}

	if pageType == "1" || xinliHomepage(doc) {
		items := make([]model.NewsItem, 0)
		seen := make(map[string]int)
		xinliCollectRowsInto(&items, seen, doc.Find(".p2_left ul.list_p1 > li"), baseURL, "中心要闻")
		doc.Find(".p3_box").Each(func(_ int, section *goquery.Selection) {
			typ := strings.Join(strings.Fields(section.Find(".xtit h2").First().Text()), " ")
			if typ == "" {
				return
			}
			xinliCollectRowsInto(&items, seen, section.Find(".p3_ul > li"), baseURL, typ)
		})
		if len(items) == 0 {
			return nil, fmt.Errorf("xinli: homepage contains no local article entries")
		}
		return items, nil
	}

	return nil, fmt.Errorf("xinli: response is not a homepage or article list (pageType=%q)", pageType)
}

func parseAdapter_xinliArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("xinli: parse article: %w", err)
	}
	if xinliLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("xinli: received an error or verification page instead of an article")
	}
	titleSelection := doc.Find(".box1 .bt").First()
	contentSelection := xinliArticleContent(doc)
	if titleSelection.Length() == 0 || contentSelection.Length() == 0 {
		return nil, fmt.Errorf("xinli: article shell or fixed body container is missing")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	// The xinli detail template renders the section heading in an earlier
	// <h1> (for example, "中心要闻"). The article headline is the .box1 .bt
	// paragraph in the detail area and must take precedence over that heading.
	if title := strings.TrimSpace(titleSelection.Text()); title != "" {
		article.Title = strings.Join(strings.Fields(title), " ")
	}
	if typ := xinliArticleType(doc, article.Title); typ != "" {
		article.Type = typ
	}
	// Do not retain a date guessed by the shared parser from an embedded
	// forwarded body.  If the Xinli outer metadata is absent, an empty date is
	// safer than attributing another site's publication time to this article.
	article.Date = ""
	article.PublishedAt = ""
	if date, publishedAt := xinliArticlePublication(doc); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}

	// Use only Xinli's fixed body container. In forwarded notices the nested
	// .news-content is the copied article and excludes the outer site's heading
	// and copied publication metadata.
	contentSelection.Find("script, style, noscript").Remove()
	xinliFinalizeArticleResources(article, doc, contentSelection, baseURL)
	if content := strings.TrimSpace(extractBodaContentText(contentSelection)); content != "" {
		article.Content = content
	}
	if contentHTML, contentErr := contentSelection.Html(); contentErr == nil {
		article.ContentHTML = contentHTML
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("xinli: fixed article body is empty")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func xinliArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	if content := doc.Find(".box2 .news-content").First(); content.Length() > 0 {
		return content
	}
	return doc.Find(".box2 .v_news_content").First()
}

func xinliListRegion(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	for _, selector := range []string{".ej_list", ".ej_list_pic"} {
		if list := doc.Find(selector).First(); list.Length() > 0 {
			return list
		}
	}
	return nil
}

func xinliLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	if xinliErrorPageRE.MatchString(title) {
		return true
	}
	body := doc.Find("body").First()
	if body.Length() == 0 {
		return false
	}
	text := strings.ToLower(strings.Join(strings.Fields(body.Text()), " "))
	return xinliErrorPageRE.MatchString(text)
}

// xinliFetchArticle bypasses the shared precision-enrichment pass.  Xinli's
// forwarded notices can contain a second site's "日期：..." inside the body;
// the adapter has already selected the authoritative outer .box1 .xx date.
func xinliFetchArticle(articleURL string) (*model.Article, []byte, error) {
	site := AllSites["xinli"]
	if site == nil {
		return nil, nil, fmt.Errorf("xinli: site is not registered")
	}
	fullURL, ok := xinliArticleURL(site.BaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("xinli: refusing article URL outside the site: %s", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_xinliArticle(body, site.BaseURL)
	if err != nil {
		return nil, nil, err
	}
	article.Images = xinliVerifyImages(article.Images, site.BaseURL)
	article.Attachments = xinliVerifyAttachments(article.Attachments, site.BaseURL)
	return article, body, nil
}

// xinliTotalPages keeps the shared reverse-page URL safe for one-page lists.
// Those pages expose "共N条" but no numbered pagination link, which the
// generic parser cannot distinguish from an unknown total. The homepage is a
// single-page feed as well.
func xinliTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "index" {
		return 1, nil
	}
	if !xinliKnownSubPage(slug) {
		return 0, fmt.Errorf("xinli: %q is not an audited article-list route", slug)
	}
	site := AllSites["xinli"]
	if site == nil {
		return 0, fmt.Errorf("xinli: site is not registered")
	}
	listURL := site.listPageURL(slug, 1, 0, ".htm")
	body, err := fetch(listURL)
	if err != nil {
		listURL = site.listPageURL(slug, 1, 0, ".html")
		body, err = fetch(listURL)
	}
	if err != nil {
		return 0, err
	}
	if total := xinliTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	return 0, fmt.Errorf("xinli: %s has no recognized pagination", slug)
}

func xinliTotalPagesFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil || xinliLooksLikeErrorPage(doc) || xinliListRegion(doc) == nil {
		return 0
	}
	if pages := parseTotalPages(raw); pages > 0 {
		return pages
	}
	if doc.Find(".p_pages").Length() > 0 {
		return 1
	}
	return 0
}

func xinliKnownSubPage(slug string) bool {
	for _, known := range xinliSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func xinliArticlePublication(doc *goquery.Document) (string, string) {
	meta := doc.Find(".box1 .xx").First()
	if meta.Length() == 0 {
		return "", ""
	}

	date, publishedAt := "", ""
	for _, selector := range []string{".sj", "time[datetime]", ".date", ".time", "time"} {
		meta.Find(selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			raw := strings.TrimSpace(sel.AttrOr("datetime", ""))
			if raw == "" {
				raw = flattenedText(sel)
			}
			if valueDate, valuePublishedAt := xinliDateValues(raw); valueDate != "" {
				date, publishedAt = valueDate, valuePublishedAt
				return false
			}
			return true
		})
		if date != "" {
			break
		}
	}
	if date != "" {
		return date, publishedAt
	}
	return xinliDateValues(flattenedText(meta))
}

func xinliHomepage(doc *goquery.Document) bool {
	return doc.Find(".p2_left ul.list_p1, .p3_box .p3_ul").Length() > 0
}

func xinliListType(doc *goquery.Document) string {
	for _, selector := range []string{".ej_main_left .lmmc_yj", "#nav_slide h1", ".lmmc_yj"} {
		if value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
}

func xinliCollectRows(rows *goquery.Selection, baseURL, typ string) []model.NewsItem {
	items := make([]model.NewsItem, 0, rows.Length())
	xinliCollectRowsInto(&items, make(map[string]int), rows, baseURL, typ)
	return items
}

func xinliCollectRowsInto(items *[]model.NewsItem, seen map[string]int, rows *goquery.Selection, baseURL, typ string) {
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		fullURL, ok := xinliArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		index, exists := seen[fullURL]
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(row.Find(".bt").First().Text())
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !xinliUsableTitle(title) {
			return
		}

		date, publishedAt := xinliDateValues(row.Find(".sj, .date, .time, time, em").First().Text())
		rowType := xinliTypeForURL(fullURL)
		if rowType == "" {
			rowType = strings.TrimSpace(typ)
		}
		item := model.NewsItem{
			Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt,
			Type: rowType, ParsedAt: time.Now(),
		}
		if !exists {
			seen[fullURL] = len(*items)
			*items = append(*items, item)
			return
		}
		// A homepage preview can repeat an article in the centre-news block and
		// an announcement tab. Keep the more specific column type and any date
		// precision discovered in the second occurrence.
		current := &(*items)[index]
		if current.Date == "" && item.Date != "" {
			current.Date = item.Date
		}
		if current.PublishedAt == "" && item.PublishedAt != "" {
			current.PublishedAt = item.PublishedAt
		}
		if (current.Type == "" || current.Type == "全部" || current.Type == "中心要闻") && item.Type != "" && item.Type != "全部" {
			current.Type = item.Type
		}
	})
}

func xinliUsableTitle(title string) bool {
	if title == "" {
		return false
	}
	// isValidNewsTitle treats a trailing 》 as a navigation marker. Xinli's
	// video classroom has real course titles enclosed in book-title marks.
	if strings.HasPrefix(title, "《") && strings.HasSuffix(title, "》") && len([]rune(title)) >= 4 {
		return true
	}
	return isValidNewsTitle(title)
}

func xinliArticleURL(baseURL, href string) (string, bool) {
	resolved := cleanURL(resolveURL(baseURL, strings.TrimSpace(htmlstd.UnescapeString(href))))
	u, err := url.Parse(resolved)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return "", false
	}
	if !xinliArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	u.RawQuery = ""
	u.Fragment = ""
	return cleanURL(u.String()), true
}

// xinliResourceURL only admits same-origin HTTP(S) assets. Ordinary external
// links remain in ContentHTML, but an image or attachment field must not turn
// an external/document-looking navigation link into downloadable data.
func xinliResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(htmlstd.UnescapeString(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	u, err := url.Parse(resolved)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return ""
	}
	resourceText := strings.ToLower(u.Path + "?" + u.RawQuery)
	if xinliResourceMarkerRE.MatchString(resourceText) {
		return ""
	}
	return cleanURL(u.String())
}

func xinliFinalizeArticleResources(article *model.Article, doc *goquery.Document, content *goquery.Selection, baseURL string) {
	if article == nil || doc == nil || content == nil || content.Length() == 0 {
		return
	}

	images := make([]string, 0)
	seenImages := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := xinliResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				resolved = candidate
				break
			}
		}
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		if !seenImages[resolved] {
			seenImages[resolved] = true
			images = append(images, resolved)
		}
	})
	article.Images = images

	attachments := make([]model.Attachment, 0)
	seenAttachments := make(map[string]bool)
	doc.Find(".ej_nry a[href], .box2 a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(htmlstd.UnescapeString(anchor.AttrOr("href", "")))
		if !bodaLooksLikeAttachmentLink(anchor, href) {
			return
		}
		resolved := xinliResourceURL(baseURL, href)
		if resolved == "" || seenAttachments[resolved] {
			return
		}
		name := xinliAttachmentName(anchor, href)
		if name == "" {
			return
		}
		seenAttachments[resolved] = true
		attachments = append(attachments, model.Attachment{Name: name, URL: resolved})
		anchor.SetAttr("href", resolved)
	})
	article.Attachments = attachments
}

func xinliAttachmentName(anchor *goquery.Selection, href string) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	for _, value := range []string{
		anchor.AttrOr("download", ""), anchor.AttrOr("data-filename", ""),
		anchor.AttrOr("data-name", ""), anchor.AttrOr("name", ""),
		anchor.Text(), anchor.AttrOr("title", ""),
	} {
		if name := strings.TrimSpace(strings.Join(strings.Fields(htmlstd.UnescapeString(value)), " ")); name != "" {
			return strings.Trim(name, "[]【】")
		}
	}
	if parsed, err := url.Parse(href); err == nil {
		name := strings.TrimSpace(path.Base(parsed.Path))
		if name != "." && name != "/" && name != "" {
			if decoded, decodeErr := url.PathUnescape(name); decodeErr == nil {
				return decoded
			}
			return name
		}
	}
	return ""
}

func xinliVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool)
	for _, raw := range images {
		resolved := xinliResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := xinliFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func xinliVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		resolved := xinliResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := xinliFetchResource(resolved, baseURL, false)
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

func xinliFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := xinliResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("xinli: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("xinli: create resource request: %w", err)
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
		return nil, "", fmt.Errorf("xinli: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("xinli: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, xinliMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("xinli: read resource %s: %w", resolved, err)
	}
	if len(body) > xinliMaxResourceBytes || !xinliResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("xinli: resource %s returned an invalid shell or file", resolved)
	}
	return body, contentType, nil
}

func xinliResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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
	if xinliResourceShellRE.Match(probe) {
		return false
	}
	magic := xinliResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && strings.HasPrefix(magic, "image-")
	}
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return false
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values"
	case strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func xinliResourceMagic(body []byte) string {
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
		return "image-jpeg"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image-png"
	case len(body) >= 6 && (bytes.Equal(body[:6], []byte("GIF87a")) || bytes.Equal(body[:6], []byte("GIF89a"))):
		return "image-gif"
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		return "image-webp"
	case len(body) >= 2 && bytes.Equal(body[:2], []byte("BM")):
		return "image-bmp"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{'I', 'I', '*', 0}) || bytes.Equal(body[:4], []byte{'M', 'M', 0, '*'})):
		return "image-tiff"
	case len(body) >= 7 && bytes.Equal(body[:7], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00}):
		return "rar"
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	}
	return ""
}

func xinliTypeForURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	match := xinliColumnPathRE.FindStringSubmatch(u.Path)
	if match == nil {
		return ""
	}
	return xinliColumnTypes[match[1]]
}

func xinliDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	if match := dateTimeRe.FindString(raw); match != "" {
		if publishedAt := normalizePublishedAt(match); publishedAt != "" {
			return publishedAt[:10], publishedAt
		}
	}
	if match := xinliChineseDateTimeRE.FindStringSubmatch(raw); match != nil {
		second := match[6]
		if second == "" {
			second = "00"
		}
		if date := normalizeDateParts(match[1], match[2], match[3]); date != "" {
			hour, hourErr := strconv.Atoi(match[4])
			minute, minuteErr := strconv.Atoi(match[5])
			secondValue, secondErr := strconv.Atoi(second)
			if hourErr != nil || minuteErr != nil || secondErr != nil || hour < 0 || hour > 23 || minute > 59 || secondValue > 59 {
				return "", ""
			}
			return date, fmt.Sprintf("%s %02d:%02d:%02d", date, hour, minute, secondValue)
		}
	}
	// The source occasionally writes a Chinese time unit (时/分), which is
	// accepted by normalizePublishedAt only when the field label is removed.
	if colon := strings.IndexAny(raw, ":："); colon >= 0 && colon+1 < len(raw) {
		if publishedAt := normalizePublishedAt(strings.TrimSpace(raw[colon+1:])); publishedAt != "" {
			return publishedAt[:10], publishedAt
		}
	}
	if match := labelDateRe.FindStringSubmatch(raw); match != nil {
		if date := normalizeDateParts(match[1], match[2], match[3]); date != "" {
			return date, ""
		}
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	if match := xinliShortDateRE.FindStringSubmatch(raw); match != nil {
		return fmt.Sprintf("%02s-%02s", match[1], match[2]), ""
	}
	return "", ""
}

func xinliArticleType(doc *goquery.Document, articleTitle string) string {
	// On live detail pages .lmmc_yj is the parent section (通知公告,
	// 科普资源, ...); the active sidebar item is the actual column.
	for _, selector := range []string{
		".sider_nav li.title.on .sun a.on",
		"#nav_slide h1",
	} {
		if value := xinliActiveTypeCandidate(doc.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	for _, selector := range []string{".dqwz a", ".loaction a", ".main_titT a"} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			if value := bodaTypeCandidate(selection.Eq(i).Text(), articleTitle); value != "" {
				return value
			}
		}
	}
	for _, selector := range []string{".sider_nav li.title.on > a p", ".sider_nav li.title.on > a"} {
		if value := xinliActiveTypeCandidate(doc.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	for _, selector := range []string{".lmmc_yj", ".ej_main_left > h2"} {
		if value := bodaTypeCandidate(doc.Find(selector).First().Text(), articleTitle); value != "" {
			return value
		}
	}
	return ""
}

func xinliActiveTypeCandidate(raw string) string {
	value := strings.Join(strings.Fields(raw), " ")
	if value == "" || value == "首页" || value == "正文" || value == "文章详情" || value == "新闻中心" || value == "新闻资讯" || value == "网站首页" || value == "LINK" {
		return ""
	}
	if strings.Contains(value, "当前位置") || len([]rune(value)) > 40 {
		return ""
	}
	// Active sidebar/breadcrumb labels are authoritative even when the article
	// headline happens to have the same text (for example, a course page).
	return value
}
