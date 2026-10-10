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

// designSubPages contains every same-site navigation entry that exposes real
// /info/<channel>/<id>.htm detail links. Some are one-item introduction pages
// (for example 组织建设), while the profile/download/gallery pages have no
// article list and are intentionally not registered as feeds. 教学成果
// (jyjx/jxcg) and 科研成果 (kysj/kycg) were removed after the live columns
// started returning HTTP 404 on both .htm and .html (checked 2026-10-09).
var designSubPages = []string{
	"xydt",
	"xshd",
	"xshd/jz",
	"xshd/yt",
	"xshd/zl",
	"xshd/hd",
	"tzgg",
	"tzgg/tzgg",
	"tzgg/bkjx",
	"tzgg/yjsjx",
	"jyjx",
	"jyjx/bsxwd",
	"jyjx/sjjd",
	"djgz",
	"djgz/zzjs",
	"djgz/xwgg",
	"djgz/xygh",
	"djgz/jwgz",
	"djgz/zcwj",
	"xsgz",
	"xsgz/xgdw",
	"xsgz/tzgg",
	"xsgz/sjzm",
	"xsgz/jyxx",
	"hzjl",
	"xyfc",
}

var (
	designArticlePathRE   = regexp.MustCompile(`(?i)^/info/[^/]+/\d+\.html?$`)
	designAttachmentExtRE = regexp.MustCompile(`(?i)\.(?:docx?|xlsx?|pptx?|pdf|zip|rar|7z|txt|csv)(?:$|[?#])`)
)

func init() {
	registerAdapter("design", SiteAdapter{
		SubPages:     designSubPages,
		ParseList:    parseAdapter_designList,
		ParseArticle: parseAdapter_designArticle,
		PageURL:      designPageURL,
		TotalPages:   designTotalPages,
		FetchArticle: designFetchArticle,
	})
}

// designPageURL maps the browser's logical page number to the reverse-numbered
// static file emitted by the site. For example, page 2 of a 25-page list is
// /24.htm and the last page is /1.htm.
func designPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
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
	filePage := totalPages - page + 1
	if filePage < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func designTotalPages(slug string) (int, error) {
	baseURL := designBaseURL()
	pageURL := designPageURL(baseURL, slug, 1, 0, ".htm")
	if pageURL == "" {
		return 0, fmt.Errorf("design: invalid list slug %q", slug)
	}
	body, err := fetch(pageURL)
	if err != nil {
		return 0, fmt.Errorf("design: fetch list %s: %w", pageURL, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("design: parse list %s: %w", pageURL, err)
	}
	if designDocumentBlocked(doc) {
		return 0, fmt.Errorf("design: list %s is an error, WAF, login, or CAPTCHA response", pageURL)
	}
	if designIsHomepage(doc) {
		// The implicit "index" feed renders the homepage portal, whose
		// article modules publish no pagination of their own. Accept it only
		// when the scoped module parse finds real entries; a home-shaped
		// shell must keep failing instead of being crawled as one empty page.
		if len(designHomepageItems(doc, baseURL)) == 0 {
			return 0, fmt.Errorf("design: homepage %s has no article modules", pageURL)
		}
		return 1, nil
	}
	if designListContainer(doc).Length() == 0 {
		return 0, fmt.Errorf("design: list %s has no article-list container", pageURL)
	}
	if total := parseTotalPages(body); total > 0 {
		return total, nil
	}
	return 0, fmt.Errorf("design: list %s has no source-reported page count", pageURL)
}

func designBaseURL() string {
	if site := AllSites["design"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(site.BaseURL, "/")
	}
	return "https://design.swjtu.edu.cn"
}

func parseAdapter_designList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse design list: %w", err)
	}
	if designDocumentBlocked(doc) {
		return nil, fmt.Errorf("design: response is an error, WAF, login, or CAPTCHA page")
	}

	if designIsHomepage(doc) {
		items := designHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("design: homepage has no article modules")
		}
		return items, nil
	}

	list := designListContainer(doc)
	if list.Length() == 0 {
		return nil, fmt.Errorf("design: response is not a homepage or article list")
	}

	items := make([]model.NewsItem, 0)
	designCollectRows(list, designListType(doc), baseURL, &items, make(map[string]bool))
	if candidates := designListArticleCandidates(list, baseURL); candidates > 0 && len(items) == 0 {
		return nil, fmt.Errorf("design: list contains %d article links but no parseable rows", candidates)
	}
	// A present list container with no rows is a reachable empty column (for
	// example a temporarily empty source column), which is different from a
	// static page or an error shell.
	return items, nil
}

func parseAdapter_designArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse design article: %w", err)
	}
	if designDocumentBlocked(doc) {
		return nil, fmt.Errorf("design: response is an error, WAF, login, or CAPTCHA page")
	}
	if !designHasArticleBody(doc) {
		return nil, fmt.Errorf("design: response is not an article")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	title, typ, rawDate := designArticleHeader(doc, baseURL)
	if title != "" {
		article.Title = title
	}
	if typ != "" {
		article.Type = typ
	}
	// A registered design adapter must not retain a date guessed from the
	// article body. Keep only a date from the fixed title/meta header (or the
	// fixed metadata row of the older static-content template).
	article.Date = ""
	article.PublishedAt = ""
	if date, publishedAt := designDateValues(rawDate); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}

	designCollectArticleAttachments(doc, baseURL, article)
	if strings.TrimSpace(article.Title) == "" {
		return nil, fmt.Errorf("design: article has no title")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func designIsHomepage(doc *goquery.Document) bool {
	return doc.Find(".dynamic .list, .product .content .list, .notice .content .list").Length() > 0
}

func designDocumentBlocked(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(designCompactText(doc.Find("title").First().Text()))
	for _, marker := range []string{
		"404", "403", "500", "not found", "access denied", "错误提示", "页面不存在",
		"安全验证", "网站防火墙", "访问被拒绝", "验证码", "请先登录",
	} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	if doc.Find(`#captcha, .captcha, input[name="codeValue"]`).Length() > 0 {
		return true
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(designCompactText(clone.Find("body").Text()))
	for _, marker := range []string{
		"请输入验证码", "验证码下载附件", "请求已被阻断", "访问异常", "安全验证",
		"access denied", "just a moment", "cloudflare", "page not found", "页面不存在",
	} {
		if strings.Contains(visible, marker) {
			return true
		}
	}
	return false
}

func designListContainer(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{".file .list", ".detail .list", ".alumnus .list", ".newList > .list"} {
		if list := doc.Find(selector).First(); list.Length() > 0 {
			return list
		}
	}
	return doc.Find("__design_missing_list__")
}

func designHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)

	if scope := doc.Find(".dynamic .list").First(); scope.Length() > 0 {
		designCollectRows(scope, "学院动态", baseURL, &items, seen)
	}

	if product := doc.Find(".product").First(); product.Length() > 0 {
		academicTypes := designTabLabels(product, []string{"讲座", "展览", "论坛", "活动"})
		product.Find(".content > .list").Each(func(i int, scope *goquery.Selection) {
			typ := "学术活动"
			if i < len(academicTypes) {
				typ = academicTypes[i]
			}
			designCollectRows(scope, typ, baseURL, &items, seen)
		})
	}

	if notice := doc.Find(".notice").First(); notice.Length() > 0 {
		noticeTypes := designTabLabels(notice, []string{"通知公告", "本科生通知", "研究生通知"})
		notice.Find(".content > .list").Each(func(i int, scope *goquery.Selection) {
			typ := "通知公告"
			if i < len(noticeTypes) {
				typ = noticeTypes[i]
			}
			designCollectRows(scope, typ, baseURL, &items, seen)
		})
	}

	return items
}

func designTabLabels(scope *goquery.Selection, fallback []string) []string {
	labels := make([]string, 0, len(fallback))
	if scope != nil {
		scope.Find(".tabs > ul > li").Each(func(_ int, tab *goquery.Selection) {
			if label := designCompactText(tab.Text()); label != "" {
				labels = append(labels, label)
			}
		})
	}
	if len(labels) == 0 {
		return fallback
	}
	return labels
}

func designCollectRows(scope *goquery.Selection, typ, baseURL string, items *[]model.NewsItem, seen map[string]bool) {
	if scope == nil || scope.Length() == 0 {
		return
	}
	if cards := scope.Find(".item"); cards.Length() > 0 {
		cards.Each(func(_ int, row *goquery.Selection) {
			designAddRow(items, seen, row, typ, baseURL)
		})
		return
	}
	scope.Find("ul > li").Each(func(_ int, row *goquery.Selection) {
		designAddRow(items, seen, row, typ, baseURL)
	})
}

func designListArticleCandidates(list *goquery.Selection, baseURL string) int {
	if list == nil || list.Length() == 0 {
		return 0
	}
	seen := make(map[string]bool)
	list.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if resolved, ok := designInternalArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			seen[resolved] = true
		}
	})
	return len(seen)
}

func designAddRow(items *[]model.NewsItem, seen map[string]bool, row *goquery.Selection, typ, baseURL string) {
	if row == nil || row.Length() == 0 {
		return
	}

	var articleURL string
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if resolved, ok := designInternalArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			articleURL = resolved
			return false
		}
		return true
	})
	if articleURL == "" || seen[articleURL] {
		return
	}

	title := designRowTitle(row)
	if title == "" || title == "查看更多" || title == "更多" {
		return
	}
	date, publishedAt := designDateValues(designRowDate(row))
	seen[articleURL] = true
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         articleURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        typ,
		ParsedAt:    time.Now(),
	})
}

func designRowTitle(row *goquery.Selection) string {
	for _, selector := range []string{".tits", ".txts", ".text h5", "h5", "h4", "span"} {
		if value := designCompactText(row.Find(selector).First().Text()); value != "" {
			return cleanTitle(value)
		}
	}
	if anchor := row.Find("a[href]").First(); anchor.Length() > 0 {
		if value := designCompactText(anchor.AttrOr("title", "")); value != "" {
			return cleanTitle(value)
		}
		return cleanTitle(designCompactText(anchor.Text()))
	}
	return ""
}

func designRowDate(row *goquery.Selection) string {
	for _, selector := range []string{".time", ".date", ".labs .box", ".labs p", ".text p", "time", "i"} {
		var found string
		row.Find(selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			value := designCompactText(sel.Text())
			if date, _ := designDateValues(value); date != "" {
				found = value
				return false
			}
			return true
		})
		if found != "" {
			return found
		}
	}
	return ""
}

func designListType(doc *goquery.Document) string {
	for _, selector := range []string{".file > .title", ".alumnus > .title", ".newList > .title", ".bann .title span"} {
		if value := designCompactText(doc.Find(selector).First().Text()); value != "" && value != "内容" && value != "文章内容" {
			return value
		}
	}
	return ""
}

func designArticleHeader(doc *goquery.Document, baseURL string) (title, typ, rawDate string) {
	if block := doc.Find(".detail .title").First(); block.Length() > 0 {
		title = designCompactText(block.Find("h4").First().Text())
		if title != "" {
			typ = designCompactText(block.Find("h5").First().Text())
			rawDate = designArticleDate(block)
			return cleanTitle(title), typ, rawDate
		}
	}

	if block := doc.Find(".informs .intrs .title").First(); block.Length() > 0 {
		title = designCompactText(block.Find("h5").First().Text())
		rawDate = designArticleDate(block)
		if title != "" {
			return cleanTitle(title), designArticleSectionType(doc, baseURL), rawDate
		}
	}

	if heading := doc.Find(".newList .content h1, .newList .content .news-title").First(); heading.Length() > 0 {
		title = designCompactText(heading.Text())
		rawDate = designFixedDateNearHeading(heading)
	}
	if title == "" {
		heading := doc.Find("h1.news_tit, h1").First()
		title = designCompactText(heading.Text())
		if rawDate == "" {
			rawDate = designFixedDateNearHeading(heading)
		}
	}
	return cleanTitle(title), designArticleSectionType(doc, baseURL), rawDate
}

func designArticleDate(block *goquery.Selection) string {
	if value := designCompactText(block.Find("h6").First().Text()); value != "" {
		if date, _ := designDateValues(value); date != "" {
			return value
		}
	}
	var dateText string
	block.Find(".labs p, .labs .box, .time, .date").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		value := designCompactText(sel.Text())
		if date, _ := designDateValues(value); date != "" {
			dateText = value
			return false
		}
		return true
	})
	return dateText
}

func designFixedDateNearHeading(heading *goquery.Selection) string {
	if heading == nil || heading.Length() == 0 {
		return ""
	}
	parent := heading.Parent()
	if parent.Length() == 0 {
		return ""
	}
	var dateText string
	parent.Children().EachWithBreak(func(_ int, child *goquery.Selection) bool {
		if heading.Get(0) != nil && child.Get(0) == heading.Get(0) {
			return true
		}
		if child.Is("#vsb_content, #vsb_content_501, .v_news_content") {
			return false
		}
		value := designCompactText(child.Text())
		if date, _ := designDateValues(value); date != "" {
			dateText = value
			return false
		}
		return true
	})
	return dateText
}

func designArticleSectionType(doc *goquery.Document, baseURL string) string {
	var typ string
	doc.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if designCompactText(anchor.Text()) != "返回列表" {
			return true
		}
		resolved := resolveURL(baseURL, anchor.AttrOr("href", ""))
		if slug := designSlugFromURL(resolved); slug != "" {
			typ = designFeedType(slug)
		}
		return false
	})
	if typ != "" {
		return typ
	}
	for _, selector := range []string{".newList > .title", ".informs .sidebar .title span", ".bann .title span"} {
		value := designCompactText(doc.Find(selector).First().Text())
		if value != "" && value != "内容" && value != "文章内容" {
			return value
		}
	}
	return ""
}

func designFeedType(slug string) string {
	switch strings.Trim(slug, "/") {
	case "xydt":
		return "学院动态"
	case "xshd":
		return "学术活动"
	case "xshd/jz":
		return "讲座"
	case "xshd/yt":
		return "研讨"
	case "xshd/zl":
		return "展览"
	case "xshd/hd":
		return "活动"
	case "tzgg", "tzgg/tzgg":
		return "通知公告"
	case "tzgg/bkjx":
		return "本科教学"
	case "tzgg/yjsjx":
		return "研究生教学"
	case "jyjx":
		return "教育教学"
	case "jyjx/jxcg":
		return "教学成果"
	case "jyjx/bsxwd":
		return "博士学位点"
	case "jyjx/sjjd":
		return "实践基地"
	case "kysj/kycg":
		return "科研成果"
	case "djgz":
		return "党建工作"
	case "djgz/zzjs":
		return "组织建设"
	case "djgz/xwgg":
		return "新闻公告"
	case "djgz/xygh":
		return "学院工会"
	case "djgz/jwgz":
		return "纪委工作"
	case "djgz/zcwj":
		return "政策文件"
	case "xsgz":
		return "学生工作"
	case "xsgz/xgdw":
		return "学工队伍"
	case "xsgz/tzgg":
		return "通知公告"
	case "xsgz/sjzm":
		return "设计之美"
	case "xsgz/jyxx":
		return "就业信息"
	case "hzjl":
		return "合作交流"
	case "xyfc":
		return "校友风采"
	case "xszt":
		return "线上展厅"
	default:
		return ""
	}
}

func designSlugFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	path := strings.Trim(u.Path, "/")
	if strings.HasPrefix(path, "info/") {
		return ""
	}
	lowerPath := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lowerPath, ".html"):
		return path[:len(path)-len(".html")]
	case strings.HasSuffix(lowerPath, ".htm"):
		return path[:len(path)-len(".htm")]
	default:
		return ""
	}
}

func designDateValues(raw string) (date, publishedAt string) {
	raw = designCompactText(raw)
	if raw == "" {
		return "", ""
	}
	if match := dateTimeRe.FindString(raw); match != "" {
		publishedAt = normalizePublishedAt(match)
		if len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	if match := bareDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func designInternalArticleURL(baseURL, href string) (string, bool) {
	if strings.TrimSpace(href) == "" {
		return "", false
	}
	resolved := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	if !designArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	return resolved, true
}

func designHasArticleBody(doc *goquery.Document) bool {
	return doc != nil && designArticleBody(doc).Length() > 0
}

func designArticleBody(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return nil
	}
	for _, selector := range []string{"#vsb_content .v_news_content", "#vsb_content_501 .v_news_content", ".v_news_content", ".content-main"} {
		body := doc.Find(selector).First()
		if body.Length() == 0 {
			continue
		}
		if designCompactText(body.Text()) != "" || body.Find("img, video, audio, iframe, table, a").Length() > 0 {
			return body
		}
	}
	return doc.Find("__design_missing_body__")
}

func designCollectArticleAttachments(doc *goquery.Document, baseURL string, article *model.Article) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if !designIsAttachmentLink(anchor, href) {
			return
		}
		resolved := designResourceURL(baseURL, href)
		if resolved == "" || seen[resolved] {
			return
		}
		name := designCompactText(anchor.Text())
		if name == "" {
			name = designCompactText(anchor.AttrOr("download", ""))
		}
		if name == "" {
			name = resolved
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func designIsAttachmentLink(anchor *goquery.Selection, href string) bool {
	if href == "" || bodaIsPseudoResourceHref(href) || designIsChallengeURL(href) {
		return false
	}
	if designAttachmentExtRE.MatchString(href) || strings.Contains(strings.ToLower(href), "download") {
		return true
	}
	parent := anchor
	for i := 0; i < 4 && parent.Length() > 0; i++ {
		tag := strings.ToLower(goquery.NodeName(parent))
		if tag == "body" || tag == "html" {
			break
		}
		className := strings.ToLower(parent.AttrOr("class", ""))
		if strings.Contains(className, "attach") || strings.Contains(className, "download") {
			return true
		}
		if tag == "li" {
			return strings.Contains(parent.Text(), "附件")
		}
		parent = parent.Parent()
	}
	return false
}

func designCompactText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func designFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := designBaseURL()
	fullURL, ok := designInternalArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("design: invalid article URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_designArticle(body, baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("design article %s: %w", fullURL, err)
	}
	article.Images = designVerifyImages(article.Images, baseURL)
	article.Attachments = designVerifyAttachments(article.Attachments, baseURL)
	designSanitizeContentHTML(article, baseURL)
	return article, body, nil
}

func designResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) || designIsChallengeURL(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	path := strings.ToLower(parsed.Path)
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif"} {
		if strings.HasSuffix(path, placeholder) {
			return ""
		}
	}
	return resolved
}

func designIsChallengeURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	for _, marker := range []string{"captcha", "createimage.jsp", "verifycode", "codevalue", "验证码"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func designVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := designResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := designFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func designVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := designResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := designFetchResource(resolved, baseURL, false)
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

func designFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := designResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("design: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("design: create resource request: %w", err)
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
		return nil, "", fmt.Errorf("design: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("design: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("design: read resource %s: %w", resolved, err)
	}
	if len(body) > 32<<20 || !designResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("design: resource %s returned an invalid shell or file", resolved)
	}
	return body, contentType, nil
}

func designResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	textProbe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(probe))), "\ufeff")
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "附件下载", "请输入验证码", "验证码下载附件",
		"请先登录", "请登录后", "登录页面", "页面不存在", "404 not found", "access denied",
		"cloudflare", "just a moment", "安全验证", "请求已被阻断",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	magic := designResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && designIsImageMagic(magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "zip") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || strings.Contains(mediaType, "msword") || strings.Contains(mediaType, "ms-excel"):
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values":
		return true
	default:
		return magic != ""
	}
}

func designIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff":
		return true
	default:
		return false
	}
}

func designResourceMagic(body []byte) string {
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
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	default:
		return ""
	}
}

func designSanitizeContentHTML(article *model.Article, baseURL string) {
	if article == nil || strings.TrimSpace(article.ContentHTML) == "" {
		return
	}
	validImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		validImages[designResourceURL(baseURL, image)] = true
	}
	validAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		validAttachments[designResourceURL(baseURL, attachment.URL)] = true
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="design-content">` + article.ContentHTML + `</div>`))
	if err != nil {
		return
	}
	content := doc.Find("#design-content").First()
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := designResourceURL(baseURL, image.AttrOr("src", ""))
		if !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		attachmentLike := designIsAttachmentLink(link, href) || designIsChallengeURL(href) || (bodaIsPseudoResourceHref(href) && strings.Contains(strings.ToLower(href), "download"))
		if attachmentLike && !validAttachments[designResourceURL(baseURL, href)] {
			link.Remove()
			return
		}
		if designIsAttachmentLink(link, href) {
			link.SetAttr("href", designResourceURL(baseURL, href))
		}
	})
	if html, err := content.Html(); err == nil {
		article.ContentHTML = html
	}
}
