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

const zzxyCanonicalBaseURL = "http://zzxy.swjtu.edu.cn"

// zzxySubPages contains every current navigation target that is backed by an
// article list. Static introduction/organization/platform pages are
// intentionally excluded: asking the scraper to crawl those as an empty feed
// would hide a wrong route. The homepage's recruitment block has two direct
// article links but no archive/list target, so it is not inventible as a
// SubPage either.
var zzxySubPages = []string{
	"xyyw",
	"tzgg",
	"szkc",
	"xwdt",
	"xsgg",
	"mtjj",
	"cgzs",
	"djjgh/djgz/djfc",
	"djjgh/djgz/tzgg",
	"djjgh/djgz/dwwj",
	"djjgh/jjgz",
	"djjgh/ghgz/xxzili",
	"djjgh/ltgz/gzdt",
	"xsgz/xsdt",
	"xsgz/tzgg",
	"xsgz/jyxx",
	"kyyxk/kxyj",
	"kyyxk/kxyj/xsdt",
	"kyyxk/kxyj/kytz",
	"kyyxk/kxyj/kycg",
	"kyyxk/kxyj/kyxm",
	"zlxz1/mbxz",
	"zlxz1/wjxz",
}

var zzxyFeedTypes = map[string]string{
	"xyyw":              "学院要闻",
	"tzgg":              "通知公告",
	"szkc":              "思政课程",
	"xwdt":              "新闻动态",
	"xsgg":              "学术公告",
	"mtjj":              "媒体聚焦",
	"cgzs":              "成果展示",
	"djjgh/djgz/djfc":   "党建风采",
	"djjgh/djgz/tzgg":   "通知公告",
	"djjgh/djgz/dwwj":   "党务文件",
	"djjgh/jjgz":        "纪检工作",
	"djjgh/ghgz/xxzili": "学习资料",
	"djjgh/ltgz/gzdt":   "工作动态",
	"xsgz/xsdt":         "学生动态",
	"xsgz/tzgg":         "通知公告",
	"xsgz/jyxx":         "就业信息",
	"kyyxk/kxyj":        "科学研究",
	"kyyxk/kxyj/xsdt":   "学术动态",
	"kyyxk/kxyj/kytz":   "科研通知",
	"kyyxk/kxyj/kycg":   "科研成果",
	"kyyxk/kxyj/kyxm":   "科研项目",
	"zlxz1/mbxz":        "模板下载",
	"zlxz1/wjxz":        "文件下载",
}

var zzxyArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
var zzxyPageCounterRE = regexp.MustCompile(`(?:^|[^0-9])\d+\s*/\s*(\d+)(?:[^0-9]|$)`)
var zzxyPDFImagePathRE = regexp.MustCompile(`(?i)["']((?:https?://|/|\.\.?/)[^"']+\.(?:jpg|jpeg|png|gif|webp)(?:\?[^"']*)?)["']`)
var zzxyEmbeddedPDFRe = regexp.MustCompile(`(?i)showVsbpdfIframe\s*\(\s*["']([^"']+\.pdf(?:\?[^"']*)?)["']`)
var zzxyHomepageDateRe = regexp.MustCompile(`(?i)(\d{1,2})\s*(20\d{2})[./-](\d{1,2})`)
var zzxyHomepageYearMonthRe = regexp.MustCompile(`(?i)^(20\d{2})[./-](\d{1,2})$`)

var zzxyHomepageTypes = []string{"招聘与培训信息"}

// zzxyAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("zzxy", SiteAdapter{
		SubPages:     zzxySubPages,
		ParseList:    parseAdapter_zzxyList,
		ParseArticle: parseAdapter_zzxyArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   zzxyTotalPages,
		FetchArticle: zzxyFetchArticle,
	})
}

func parseAdapter_zzxyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}

	// The ordinary feeds use a plain <ul class="list">; szkc uses the
	// conlistComm/dl shell. Neither shape is covered by the generic parser.
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".right ul.list > li").Each(func(_ int, row *goquery.Selection) {
		a := row.Find(".tit a[href]").First()
		if a.Length() == 0 {
			return
		}
		title := cleanTitle(a.Text())
		href := strings.TrimSpace(a.AttrOr("href", ""))
		if title == "" || href == "" || !isValidNewsTitle(title) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, href))
		typ := strings.TrimSpace(row.Find(".tag").Text())
		typ = strings.TrimSpace(strings.Trim(typ, "[]【】"))
		if typ == "" {
			typ = zzxyDocumentType(doc)
		}
		if seen[fullURL] || !zzxyListURLAllowed(fullURL, baseURL, typ) {
			return
		}

		dateText := strings.TrimSpace(row.Find(".time").Text())
		date, publishedAt := zzxyDateValues(dateText)
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt,
			Type: typ, ParsedAt: time.Now(),
		})
	})
	// Homepage columns use the same list class but put the title in <p> and
	// obtain the type from the adjacent global-title heading. The older .tit
	// parser above is retained for archive pages and legacy templates.
	doc.Find("ul.global-list.list > li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Find("p, .tit, .title, em, h3, h4").First().Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		typ := zzxyDocumentTypeForRow(row)
		if seen[fullURL] || !zzxyListURLAllowed(fullURL, baseURL, typ) {
			return
		}
		date, publishedAt := zzxyDateValues(row.Find(".time, .date").First().Text())
		if date == "" {
			date, publishedAt = zzxyHomepageDateValues(row)
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	// The academic-announcement column uses notice-list, the recruitment
	// block uses a plain ul, and the achievements carousel uses swiper-wrapper.
	// These are homepage-only previews; their linked detail pages remain the
	// authority for full title/time metadata.
	doc.Find(".section-list ul.notice-list.list > li, .join-us-wrap ul > li, .case-wrap .swiper-wrapper > li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		typ := zzxyDocumentTypeForRow(row)
		if seen[fullURL] || !zzxyListURLAllowed(fullURL, baseURL, typ) {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Find(".txt p").First().Text())
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Find(".txt").First().Text())
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Find("p").First().Text())
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := zzxyHomepageDateValues(row)
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	doc.Find(".conlistComm dl").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("dt a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if !zzxyArticleURL(fullURL, baseURL) || seen[fullURL] {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := zzxyDateValues(row.Find("dd, .time, .date").First().Text())
		typ := strings.Trim(strings.TrimSpace(row.Find("dt .tag, dt .type, dt span").First().Text()), "[]【】")
		if typ == "" {
			typ = zzxyDocumentType(doc)
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	if len(items) > 0 {
		return items, nil
	}
	if doc.Find(".right ul.list, ul.global-list.list, .conlistComm, .section-list ul.notice-list.list, .join-us-wrap ul, .case-wrap .swiper-wrapper").Length() > 0 && zzxyDocumentType(doc) != "" {
		return items, nil
	}
	if doc.Find(".right ul.list, ul.global-list.list, .conlistComm, .section-list ul.notice-list.list, .join-us-wrap ul, .case-wrap .swiper-wrapper").Length() > 0 {
		return nil, fmt.Errorf("zzxy: response has an unrecognised list section")
	}
	return parseBodaNewsList(html, baseURL)
}

func zzxyDocumentTypeForRow(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	for _, scopeSelector := range []string{".right", ".left", ".news-list", ".section-list", ".case-wrap", ".join-us-wrap"} {
		scope := row.Closest(scopeSelector)
		if scope.Length() == 0 {
			continue
		}
		value := strings.Join(strings.Fields(scope.Find(".global-title h3, .title h3, .title, .hd h3").First().Text()), " ")
		if typ := zzxyTypeFromText(value); typ != "" {
			return typ
		}
	}
	return ""
}

func zzxyDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-title", ".position h1", ".right .title h3", ".right .title", ".main-right .current h3", ".current h3", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if typ := zzxyTypeFromText(value); typ != "" {
			return typ
		}
	}
	value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
	return zzxyTypeFromText(value)
}

func zzxyTypeFromText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	for _, slug := range zzxySubPages {
		if typ := zzxyFeedTypes[slug]; typ != "" && strings.Contains(value, typ) {
			return typ
		}
	}
	for _, typ := range zzxyHomepageTypes {
		if strings.Contains(value, typ) {
			return typ
		}
	}
	return ""
}

func zzxyListURLAllowed(rawURL, baseURL, typ string) bool {
	if zzxyArticleURL(rawURL, baseURL) {
		return true
	}
	return typ == "媒体聚焦" && zzxyHTTPURL(rawURL)
}

func zzxyHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "http" || scheme == "https"
}

func zzxyHomepageDateValues(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	if date, publishedAt := zzxyDateValues(row.Find(".time").First().Text()); date != "" {
		return date, publishedAt
	}
	dateBlock := row.Find(".date").First()
	day := strings.TrimSpace(dateBlock.Find(".t1").First().Text())
	yearMonth := strings.TrimSpace(dateBlock.Find(".t2").First().Text())
	if day != "" && yearMonth != "" {
		if match := zzxyHomepageYearMonthRe.FindStringSubmatch(yearMonth); len(match) == 3 {
			date := normalizeDateParts(match[1], match[2], day)
			if date != "" {
				return date, date
			}
		}
	}
	text := strings.Join(strings.Fields(dateBlock.Text()), " ")
	match := zzxyHomepageDateRe.FindStringSubmatch(text)
	if len(match) != 4 {
		return "", ""
	}
	date := normalizeDateParts(match[2], match[3], match[1])
	return date, date
}

func zzxyTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if zzxyFeedTypes[slug] == "" {
		return 0, fmt.Errorf("zzxy: %q is not an article-list route", slug)
	}
	baseURL := "http://zzxy.swjtu.edu.cn"
	if site := AllSites["zzxy"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	body, err := fetch(reverseBodaPageURL(baseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("zzxy: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("zzxy: parse first page for %s: %w", slug, err)
	}
	if doc.Find(".right ul.list, .conlistComm").Length() == 0 || zzxyDocumentType(doc) == "" {
		return 0, fmt.Errorf("zzxy: %s is not an article list page", slug)
	}
	if total := zzxyPageCount(doc); total > 0 {
		return total, nil
	}
	return 1, nil
}

func zzxyPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	text := strings.Join(strings.Fields(doc.Find("[id^='fanye'], .fanye, .p_pages, .paging, .page").Text()), " ")
	if match := zzxyPageCounterRE.FindStringSubmatch(text); match != nil {
		var total int
		fmt.Sscanf(match[1], "%d", &total)
		return total
	}
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d, .p_no, .p_no_d").Each(func(_ int, node *goquery.Selection) {
		var value int
		fmt.Sscanf(strings.TrimSpace(node.Text()), "%d", &value)
		if value > maxPage {
			maxPage = value
		}
	})
	return maxPage
}

func parseAdapter_zzxyArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("zzxy: parse article HTML: %w", err)
	}
	if doc.Find(".v_news_content, #vsb_content_501").Length() == 0 {
		return nil, fmt.Errorf("zzxy: response has no article body")
	}
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil || article == nil {
		return article, err
	}

	if title := zzxyDetailTitle(doc); title != "" {
		article.Title = title
	}
	if typ := zzxyDocumentType(doc); typ != "" {
		article.Type = typ
	}
	if date, publishedAt := zzxyFixedArticleDate(doc); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	} else {
		// The shared parser deliberately has broad fallbacks for other Boda
		// templates. zzxy's body contains event/deadline/signature dates, so a
		// missing fixed metadata node must remain missing rather than inherit a
		// date guessed from prose.
		article.Date = ""
		article.PublishedAt = ""
	}
	zzxyAppendPDFViewerResources(article, doc, baseURL)
	zzxyAppendOutsideAttachments(article, doc, baseURL)
	return article, nil
}

var zzxyDateRe = regexp.MustCompile(`20\d{2}\s*[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}日?(?:\s+\d{1,2}:\d{2}(?::\d{2})?)?`)

func zzxyDateValues(text string) (string, string) {
	match := zzxyDateRe.FindString(strings.Join(strings.Fields(text), " "))
	if match == "" {
		return "", ""
	}
	precise := normalizePublishedAt(match)
	if precise == "" {
		return "", ""
	}
	return precise[:10], precise
}

func zzxyDetailTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{
		"form[name='_newscontent_fromname'] .hd h3",
		".article-text .hd h3",
		".hd h3",
	} {
		if title := doc.Find(selector).First(); title.Length() > 0 {
			value := strings.Join(strings.Fields(title.Text()), " ")
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func zzxyFixedArticleDate(doc *goquery.Document) (string, string) {
	if doc == nil {
		return "", ""
	}
	for _, selector := range []string{
		"form[name='_newscontent_fromname'] .hd .author",
		".article-text .hd .author",
		".hd .author",
	} {
		if meta := doc.Find(selector).First(); meta.Length() > 0 {
			if date, publishedAt := zzxyDateValues(meta.Text()); date != "" {
				return date, publishedAt
			}
		}
	}
	return "", ""
}

func zzxyArticleURL(rawURL, baseURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(baseURL)
	return err == nil && baseErr == nil && zzxyArticlePathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}

func zzxySameOriginURL(baseURL, rawURL string) (string, bool) {
	resolved := cleanURL(resolveURL(baseURL, htmlstd.UnescapeString(strings.TrimSpace(rawURL))))
	parsed, err := url.Parse(resolved)
	base, baseErr := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || baseErr != nil || parsed.Hostname() == "" || base.Hostname() == "" {
		return "", false
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return "", false
	}
	if !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	return resolved, true
}

func zzxyAppendOutsideAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) {
			return
		}
		href := strings.TrimSpace(link.AttrOr("href", ""))
		resolved, sameOrigin := zzxySameOriginURL(baseURL, href)
		if href == "" || !sameOrigin || zzxyArticleURL(resolved, baseURL) || !bodaLooksLikeAttachmentLink(link, href) || seen[resolved] {
			return
		}
		name := strings.Join(strings.Fields(link.AttrOr("download", "")), " ")
		if name == "" {
			name = strings.Join(strings.Fields(link.AttrOr("name", "")), " ")
		}
		if name == "" {
			name = strings.Join(strings.Fields(link.Text()), " ")
		}
		if name == "" {
			name = href
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func zzxyAppendPDFViewerResources(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seenImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		seenImages[image] = true
	}
	seenAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seenAttachments[attachment.URL] = true
	}
	content := doc.Find(".v_news_content, #vsb_content_501").First()
	if content.Length() == 0 {
		return
	}
	content.Find("script").Each(func(_ int, script *goquery.Selection) {
		text := htmlstd.UnescapeString(script.Text())
		for _, match := range zzxyPDFImagePathRE.FindAllStringSubmatch(text, -1) {
			if len(match) < 2 {
				continue
			}
			resolved := bodaResourceURL(baseURL, match[1])
			if resolved == "" || seenImages[resolved] {
				continue
			}
			seenImages[resolved] = true
			article.Images = append(article.Images, resolved)
		}
		for _, match := range zzxyEmbeddedPDFRe.FindAllStringSubmatch(text, -1) {
			if len(match) < 2 {
				continue
			}
			resolved, ok := zzxySameOriginURL(baseURL, match[1])
			if !ok || seenAttachments[resolved] {
				continue
			}
			name := ""
			if parsed, parseErr := url.Parse(resolved); parseErr == nil {
				name = path.Base(parsed.Path)
			}
			if name == "" || name == "." || name == "/" {
				name = "embedded.pdf"
			}
			seenAttachments[resolved] = true
			article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		}
	})
}

func zzxySiteBaseURL() string {
	if site := AllSites["zzxy"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return zzxyCanonicalBaseURL
}

func zzxyFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := zzxySiteBaseURL()
	canonicalURL, ok := zzxySameOriginURL(baseURL, articleURL)
	if !ok || !zzxyArticleURL(canonicalURL, baseURL) {
		return nil, nil, fmt.Errorf("zzxy: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonicalURL)
	if err != nil {
		return nil, nil, fmt.Errorf("zzxy: fetch article %s: %w", canonicalURL, err)
	}
	article, err := parseAdapter_zzxyArticle(body, baseURL)
	if err != nil {
		return nil, body, err
	}
	article.Images = zzxyVerifyImages(article.Images, baseURL)
	article.Attachments = zzxyVerifyAttachments(article.Attachments, baseURL)
	return article, body, nil
}

// zzxyVerifyImages and zzxyVerifyAttachments run only in the live article
// fetch path. The HTML parser remains deterministic for offline fixtures, but
// a URL is not exposed as a usable resource until its own response is a real
// binary/image rather than a login, CAPTCHA, WAF, or error shell.
func zzxyVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, image := range images {
		resolved, ok := zzxySameOriginURL(baseURL, image)
		if !ok || seen[resolved] {
			continue
		}
		if _, err := zzxyFetchResource(resolved, baseURL); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func zzxyVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved, ok := zzxySameOriginURL(baseURL, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		body, err := zzxyFetchResource(resolved, baseURL)
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

func zzxyFetchResource(rawURL, baseURL string) ([]byte, error) {
	resolved, ok := zzxySameOriginURL(baseURL, rawURL)
	if !ok {
		return nil, fmt.Errorf("zzxy: refusing non-local resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, fmt.Errorf("zzxy: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/octet-stream,application/pdf,image/*,*/*;q=0.8")
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zzxy: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("zzxy: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20+1))
	if err != nil {
		return nil, fmt.Errorf("zzxy: read resource %s: %w", resolved, err)
	}
	if len(body) > 32<<20 || !zzxyResourceBodyIsUsable(response.Header.Get("Content-Type"), body) {
		return nil, fmt.Errorf("zzxy: resource %s returned an HTML/error shell or invalid file", resolved)
	}
	return body, nil
}

func zzxyResourceBodyIsUsable(contentType string, body []byte) bool {
	if len(body) == 0 {
		return false
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if contentType == invalid {
			return false
		}
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	lowerProbe := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(string(probe)), "\ufeff"))
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "附件下载", "请输入验证码", "验证码下载附件",
		"请先登录", "请登录后", "cloudflare", "just a moment", "安全验证", "请求已被阻断",
	} {
		if strings.Contains(lowerProbe, marker) {
			return false
		}
	}

	magic := zzxyResourceMagic(body)
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return magic == "jpeg" || magic == "png" || magic == "gif" || magic == "webp" || magic == "bmp" || magic == "tiff"
	case contentType == "application/pdf" || strings.HasSuffix(contentType, "+pdf"):
		return magic == "pdf"
	case contentType == "text/plain" || contentType == "text/csv" || contentType == "text/tab-separated-values":
		return true
	case contentType == "application/octet-stream" || strings.Contains(contentType, "zip") || strings.Contains(contentType, "wordprocessingml") || strings.Contains(contentType, "spreadsheetml") || strings.Contains(contentType, "msword") || strings.Contains(contentType, "ms-excel"):
		return magic == "pdf" || magic == "zip" || magic == "ole" || magic == "jpeg" || magic == "png" || magic == "gif" || magic == "webp" || magic == "bmp" || magic == "tiff"
	case contentType == "":
		return magic != ""
	default:
		return magic != ""
	}
}

func zzxyResourceMagic(body []byte) string {
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
	default:
		return ""
	}
}
