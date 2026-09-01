package scraper

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const hbbBaseURL = "https://hbb.swjtu.edu.cn"

// hbbSubPages is the set of article-list surfaces found by walking the live
// homepage, the top navigation and every sidebar tab.  Profile/service pages,
// the image gallery, the WeChat-only page and external links are deliberately
// not feeds.  The three recruitment shells are retained as legal empty lists.
var hbbSubPages = []string{
	"hjxw/hqxw",
	"tzgg/tzgg",
	"cggl",
	"cggl/cggg",
	"cggl/jggg",
	"zpxx",
	"zpxx/zpxx",
	"zpxx/npgs",
	"djgz1",
	"djgz1/xxzl",
	"djgz1/xxdt",
}

var (
	hbbArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	hbbDateLabelRE   = regexp.MustCompile(`(来源|信息来源|日期|发布时间|作者|编辑|责编|摄影|点击数)\s*[:：]`)
	hbbPageCountRE   = regexp.MustCompile(`共\s*([0-9]+)\s*页`)
	hbbItemCountRE   = regexp.MustCompile(`共\s*([0-9]+)\s*条`)
	hbbJSQLRE        = regexp.MustCompile(`_jsq_\(\s*([0-9]+)\s*,`)
)

var hbbColumnTypes = map[string]string{
	"1018": "通知公告",
	"1019": "采购公告",
	"1021": "后勤新闻",
	"1181": "结果公告",
	"2201": "党建工作",
	"2211": "学习资料",
	"2221": "学习动态",
}

// hbbAdapter keeps this site's home modules, list shell, reverse pagination,
// article metadata and outside-the-body attachment list independent from all
// other 博达 sites.
func init() {
	if site := AllSites["hbb"]; site != nil {
		site.BaseURL = hbbBaseURL
	}
	registerAdapter("hbb", SiteAdapter{
		SubPages:     hbbSubPages,
		ParseList:    parseAdapter_hbbList,
		ParseArticle: parseAdapter_hbbArticle,
		PageURL:      hbbPageURL,
		TotalPages:   hbbTotalPages,
	})
}

// hbbPageURL maps logical pages (1 = newest) to HBB's reverse-numbered static
// files: slug.htm, slug/{total-1}.htm, ..., slug/1.htm.
func hbbPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if baseURL == "" {
		baseURL = hbbBaseURL
	}
	if slug == "" {
		slug = "index"
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		page = 1
	}
	if page <= 1 || totalPages <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		filePage = 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func parseAdapter_hbbList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if hbbLooksBlocked(raw) {
		return nil, fmt.Errorf("hbb list is an error, login or verification page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse hbb list: %w", err)
	}

	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType == "1" || hbbIsHomepage(doc) {
		items := parseHBBHomepage(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("hbb homepage contains no local article entries")
		}
		return items, nil
	}
	if pageType != "" && pageType != "2" {
		return nil, fmt.Errorf("hbb page is not an article list (pageType %q)", pageType)
	}

	content := doc.Find(".article-list .content").First()
	if content.Length() == 0 || doc.Find(".pb_sys_common").Length() == 0 {
		return nil, fmt.Errorf("hbb page is not a supported article list")
	}
	rows := content.Find(".news-list > ul.list > li")
	if rows.Length() == 0 {
		if hbbExplicitEmptyList(doc) {
			return []model.NewsItem{}, nil
		}
		return nil, fmt.Errorf("hbb recognized list has no article rows")
	}

	typ := hbbListType(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		fullURL, ok := hbbArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(row.Find("h4, h3").First().Text())
		if title == "" {
			title = strings.TrimSpace(anchor.AttrOr("title", ""))
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if !hbbIsArticleTitle(title) {
			return
		}
		date, publishedAt := hbbDateValues(row.Find(".date, .time, time, em, span").Last().Text())
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

	// A recognized list can contain only external WeChat rows (学习资料 is
	// currently mostly such rows).  Those rows are intentionally omitted, not
	// converted into local articles or treated as a parser failure.
	return items, nil
}

func parseHBBHomepage(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	add := func(row *goquery.Selection, typ string) {
		if row == nil || row.Length() == 0 {
			return
		}
		anchor := row.Find("a[href]").First()
		fullURL, ok := hbbArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(row.Find("h4, h3, p").First().Text())
		if title == "" {
			title = strings.TrimSpace(anchor.AttrOr("title", ""))
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if !hbbIsArticleTitle(title) {
			return
		}
		date, publishedAt := hbbDateValues(row.Find(".date, .time, time, em, span").Last().Text())
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

	// The two article modules are the only local news surfaces on index.htm.
	// The featured news card currently has href="#" and is therefore skipped;
	// the notice card and all list rows are real /info links.
	for _, section := range []struct {
		selector string
		typ      string
	}{
		{selector: ".list-wrap .news-list", typ: "新闻动态"},
		{selector: ".list-wrap .notice-list", typ: "通知公告"},
	} {
		if selected := doc.Find(section.selector).First(); selected.Length() > 0 {
			selected.Find(".big").Each(func(_ int, row *goquery.Selection) {
				add(row, section.typ)
			})
			selected.Find("ul.list > li").Each(func(_ int, row *goquery.Selection) {
				add(row, section.typ)
			})
		}
	}
	return items
}

func parseAdapter_hbbArticle(raw []byte, baseURL string) (*model.Article, error) {
	if hbbLooksBlocked(raw) {
		return nil, fmt.Errorf("hbb article is an error, login or verification page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse hbb article: %w", err)
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("hbb page is not an article (pageType %q)", pageType)
	}

	scope := doc.Find(".detail-wrap").First()
	heading := scope.Find(".hd").First()
	if scope.Length() == 0 || heading.Length() == 0 {
		return nil, fmt.Errorf("hbb article has no detail container")
	}
	title := strings.Join(strings.Fields(heading.Find("h3, h1, h2").First().Text()), " ")
	content := scope.Find(".article_text [id^='vsb_content'] .v_news_content").First()
	if content.Length() == 0 {
		content = scope.Find(".v_news_content").First()
	}
	if title == "" {
		title = strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	}
	if title == "" {
		return nil, fmt.Errorf("hbb article has no title")
	}
	if content.Length() == 0 || (strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a").Length() == 0) {
		return nil, fmt.Errorf("hbb article has empty content")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = title
	if fields := hbbMetaFields(flattenedText(heading.Find(".info").First())); len(fields) > 0 {
		article.Source = fields["来源"]
		if article.Source == "" {
			article.Source = fields["信息来源"]
		}
		article.Author = fields["作者"]
		article.Editor = fields["编辑"]
		if article.Editor == "" {
			article.Editor = fields["责编"]
		}
		article.Photographer = fields["摄影"]
		dateValue := fields["日期"]
		if dateValue == "" {
			dateValue = fields["发布时间"]
		}
		article.Date, article.PublishedAt = hbbDateValues(dateValue)
	}
	article.Type = hbbArticleType(doc)
	appendHBBAttachments(article, scope, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func hbbIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".list-wrap .news-list, .list-wrap .notice-list").Length() > 0 &&
		doc.Find(".beauty-wrap").Length() > 0
}

func hbbListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	typ := strings.Join(strings.Fields(doc.Find(".article-list .content > .title h3").First().Text()), " ")
	if typ == "" {
		typ = strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	}
	return typ
}

// hbbIsArticleTitle keeps the shared navigation-title protection but allows
// legitimate local HBB headlines ending in a Chinese closing quote.  HBB's
// live list contains such real /info articles; the shared helper treats every
// title ending in "》" as a section heading.
func hbbIsArticleTitle(title string) bool {
	if title == "" {
		return false
	}
	if isValidNewsTitle(title) {
		return true
	}
	return len(title) >= 6 && strings.HasSuffix(title, "》")
}

func hbbArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	typ := ""
	doc.Find(".breadcrumbs-wrap .container a").Each(func(_ int, anchor *goquery.Selection) {
		value := strings.Join(strings.Fields(anchor.Text()), " ")
		if value != "" && value != "首页" && value != "正文" {
			typ = value
		}
	})
	if typ != "" {
		return typ
	}
	if match := hbbJSQLRE.FindStringSubmatch(doc.Text()); len(match) == 2 {
		if typ := hbbColumnTypes[match[1]]; typ != "" {
			return typ
		}
	}
	return ""
}

func hbbArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !hbbArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	return fullURL, true
}

func hbbDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	normalized := normalizePublishedAt(raw)
	if normalized == "" || len(normalized) < len("2006-01-02") {
		return "", ""
	}
	date := normalized[:10]
	if publishedAtHasClock(normalized) {
		return date, normalized
	}
	return date, ""
}

func hbbMetaFields(text string) map[string]string {
	text = strings.Join(strings.Fields(text), " ")
	fields := make(map[string]string)
	matches := hbbDateLabelRE.FindAllStringSubmatchIndex(text, -1)
	for i, match := range matches {
		if len(match) < 4 {
			continue
		}
		label := text[match[2]:match[3]]
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		fields[label] = strings.TrimSpace(text[match[1]:end])
	}
	return fields
}

func appendHBBAttachments(article *model.Article, scope *goquery.Selection, baseURL string) {
	if article == nil || scope == nil || scope.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	scope.Find(".article_text a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		lower := strings.ToLower(href)
		if href == "" || (!strings.Contains(lower, "download.jsp") && !strings.Contains(lower, "downloadattachurl")) {
			return
		}
		resolved, ok := hbbSameHostURL(baseURL, href)
		if !ok || seen[resolved] {
			return
		}
		name := strings.Join(strings.Fields(htmlstd.UnescapeString(anchor.Text())), " ")
		if name == "" {
			name = strings.TrimSpace(anchor.AttrOr("download", ""))
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func hbbSameHostURL(baseURL, href string) (string, bool) {
	resolved := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(resolved)
	base, baseErr := url.Parse(baseURL)
	if err != nil || baseErr != nil || parsed.Host == "" || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	return resolved, true
}

func hbbExplicitEmptyList(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	text := strings.Join(strings.Fields(doc.Find(".pb_sys_common").Text()), " ")
	return hbbItemCountRE.FindStringSubmatch(text) != nil &&
		strings.Contains(text, "共0条")
}

func hbbLooksBlocked(raw []byte) bool {
	page := strings.ToLower(string(raw))
	for _, marker := range []string{
		"just a moment...",
		"cf-chl-",
		"cloudflare ray id",
		"access denied",
		"安全验证",
		"请输入验证码",
		"验证码下载",
		"<title>404",
		"<title>403",
		"<title>500",
	} {
		if strings.Contains(page, marker) {
			return true
		}
	}
	return false
}

func hbbTotalPages(slug string) (int, error) {
	if strings.EqualFold(strings.Trim(slug, "/"), "index") {
		return 1, nil
	}
	site := AllSites["hbb"]
	if site == nil {
		return 0, fmt.Errorf("hbb site registration is missing")
	}
	body, err := fetch(hbbPageURL(site.BaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, err
	}
	if err := hbbListResponseError(body); err != nil {
		return 0, err
	}
	if total := hbbPageCountFromHTML(body); total > 0 {
		return total, nil
	}
	if hbbExplicitEmptyHTML(body) {
		return 1, nil
	}
	return 0, fmt.Errorf("hbb %q has no recognized pagination", slug)
}

func hbbPageCountFromHTML(raw []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find(".pb_sys_common").Text()), " ")
	match := hbbPageCountRE.FindStringSubmatch(text)
	if len(match) != 2 {
		return 0
	}
	total, _ := strconv.Atoi(match[1])
	return total
}

func hbbExplicitEmptyHTML(raw []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	return err == nil && hbbExplicitEmptyList(doc)
}

func hbbListResponseError(raw []byte) error {
	if hbbLooksBlocked(raw) {
		return fmt.Errorf("hbb response is an error or verification page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	if strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) != "2" ||
		doc.Find(".article-list .content").Length() == 0 || doc.Find(".pb_sys_common").Length() == 0 {
		return fmt.Errorf("hbb response is not a supported article list")
	}
	return nil
}
