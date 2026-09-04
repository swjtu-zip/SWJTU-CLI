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

const sroCanonicalBaseURL = "https://sro.swjtu.edu.cn"

// sroSubPages is the set of non-empty article-list entry points found in the
// live navigation, its tab panels, and the linked hidden sub-navigation.  The
// parent notice list is intentionally retained: it is a real, independently
// paginated list even though its child lists contain many of the same URLs.
var sroSubPages = []string{
	"tzgg1",
	"tzgg1/zrjj",
	"tzgg1/zdyf",
	"tzgg1/qtzx",
	"tzgg1/jdpt",
	"tzgg1/kjhz",
	"tzgg1/kjjl",
	"tzgg1/xshd",
	"tzgg1/zhsx",
	"xwdt/xnkyzc",
	"xwdt/xnkydt",
	"kjhz/xqhz",
	"kjhz/xdhz",
	"kjhz/cgzh",
	"djgz/zbdt",
	"xxgs/kjjl",
	"xxgs/cgzh",
	"xxgs/qt",
	"cyxz/cyxz",
}

var sroFeedTypes = map[string]string{
	"tzgg1":       "通知公告",
	"tzgg1/zrjj":  "自然基金",
	"tzgg1/zdyf":  "重点研发",
	"tzgg1/qtzx":  "其他纵向",
	"tzgg1/jdpt":  "基地平台",
	"tzgg1/kjhz":  "科技合作",
	"tzgg1/kjjl":  "科技奖励",
	"tzgg1/xshd":  "学术活动",
	"tzgg1/zhsx":  "综合事项",
	"xwdt/xnkyzc": "校内科研政策",
	"xwdt/xnkydt": "校内科研动态",
	"kjhz/xqhz":   "校企合作",
	"kjhz/xdhz":   "校地合作",
	"kjhz/cgzh":   "成果转化",
	"djgz/zbdt":   "支部动态",
	"xxgs/kjjl":   "科技奖励",
	"xxgs/cgzh":   "成果转化",
	"xxgs/qt":     "其他",
	"cyxz/cyxz":   "常用下载",
}

// The parent notice feed does not expose a row category.  Its detail URL
// does, however: the first path component after /info/ is the source column.
var sroColumnTypes = map[string]string{
	"1120": "自然基金",
	"1121": "重点研发",
	"1123": "其他纵向",
	"1140": "项目申报",
	"1141": "基地平台",
	"1142": "校内管理",
	"1143": "知识产权",
	"1144": "科技奖励",
	"1145": "专职科研",
	"1169": "产学研合作",
	"1177": "校内科研政策",
	"1198": "常用下载",
	"1150": "校内科研动态",
}

var (
	sroArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	sroColumnPathRE  = regexp.MustCompile(`(?i)^/info/([0-9]+)/[0-9]+\.html?$`)
	sroPublicationRE = regexp.MustCompile(`(?:发布时间|发布日期|发表日期|发表时间|日期)\s*[:：]?\s*(20\d{2})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日?(?:\s+(\d{1,2})\s*(?:时|点|:)\s*(\d{2})\s*(?:分)?(?:\s*(\d{1,2})\s*秒?)?)?`)
	sroBlockedRE     = regexp.MustCompile(`(?is)安全验证|请输入验证码|验证码下载|access\s+denied|forbidden|cloudflare|just\s+a\s+moment|统一身份认证|应用维护中|页面不存在|<title>\s*(?:404|403|500)`)
)

const sroMaxResourceBytes = 32 << 20

func init() {
	// The source currently serves the site over HTTPS.  Keep the registry
	// value canonical so list URLs, article URLs, and resource referers agree.
	if site := AllSites["sro"]; site != nil {
		site.BaseURL = sroCanonicalBaseURL
	}
	registerAdapter("sro", SiteAdapter{
		SubPages:     sroSubPages,
		ParseList:    parseAdapter_sroList,
		ParseArticle: parseAdapter_sroArticle,
		PageURL:      sroPageURL,
		TotalPages:   sroTotalPages,
		FetchArticle: sroFetchArticle,
	})
}

func sroBaseURL() string {
	if site := AllSites["sro"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return sroCanonicalBaseURL
}

func sroKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return true
	}
	_, ok := sroFeedTypes[slug]
	return ok
}

// sroPageURL maps logical newest-first pages to the reverse-numbered files
// emitted by Visual SiteBuilder: slug.htm, slug/{total-1}.htm, ..., slug/1.htm.
func sroPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if baseURL == "" || page < 1 || !sroKnownFeed(slug) {
		return ""
	}
	if slug == "" || slug == "index" {
		if page != 1 {
			return ""
		}
		return baseURL + "/"
	}
	return reverseBodaPageURL(baseURL, slug, page, totalPages, extension)
}

func sroTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if !sroKnownFeed(slug) {
		return 0, fmt.Errorf("sro: %q is not an audited article feed", slug)
	}
	listURL := sroPageURL(sroBaseURL(), slug, 1, 0, ".htm")
	body, err := fetch(listURL)
	if err != nil {
		return 0, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("sro: parse page count: %w", err)
	}
	if err := sroValidateListDocument(doc); err != nil {
		return 0, err
	}
	if total := sroPageCount(doc); total > 0 {
		return total, nil
	}
	// A real one-page list has only disabled pagination controls and therefore
	// no numeric link.  Treat it as one page; never guess a reverse page.
	return 1, nil
}

func parseAdapter_sroList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if sroBlocked(raw) {
		return nil, fmt.Errorf("sro: response is a blocked, login, or error page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("sro: parse list HTML: %w", err)
	}
	switch strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", "")) {
	case "1":
		return sroParseHomepage(doc, baseURL)
	case "2":
		if err := sroValidateListDocument(doc); err != nil {
			return nil, err
		}
		return sroParseListDocument(doc, baseURL)
	default:
		return nil, fmt.Errorf("sro: refusing non-list response")
	}
}

func sroValidateListDocument(doc *goquery.Document) error {
	if doc == nil {
		return fmt.Errorf("sro: nil list document")
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	if pageType != "2" {
		return fmt.Errorf("sro: page is not an article list (pageType=%q)", pageType)
	}
	if doc.Find(".subRight02 > ul.list03, .subRight02 ul.list03").Length() == 0 {
		return fmt.Errorf("sro: pageType=2 response has no article-list shell")
	}
	return nil
}

func sroParseListDocument(doc *goquery.Document, baseURL string) ([]model.NewsItem, error) {
	list := doc.Find(".subRight02 > ul.list03, .subRight02 ul.list03").First()
	defaultType := sroListType(doc)
	items := make([]model.NewsItem, 0, list.Find("li").Length())
	seen := make(map[string]bool)
	localRows := 0
	invalidRows := 0
	list.Find("li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		fullURL, ok := sroArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		localRows++
		if seen[fullURL] {
			return
		}
		title := sroCleanTitle(anchor.Text())
		if !sroArticleTitleOK(title) {
			invalidRows++
			return
		}
		date, publishedAt := sroDateValues(row.Find("span").First().Text())
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        sroListItemType(defaultType, fullURL),
			ParsedAt:    time.Now(),
		})
	})
	if localRows > 0 && invalidRows > 0 {
		return nil, fmt.Errorf("sro: %d local article rows could not be parsed", invalidRows)
	}
	return items, nil
}

func sroParseHomepage(doc *goquery.Document, baseURL string) ([]model.NewsItem, error) {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	add := func(anchor *goquery.Selection, date, typ string) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		fullURL, ok := sroArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := sroCleanTitle(anchor.Text())
		if title == "" {
			title = sroCleanTitle(anchor.AttrOr("title", ""))
		}
		if !sroArticleTitleOK(title) {
			return
		}
		date, publishedAt := sroDateValues(date)
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	}

	// Only the two server-rendered news boxes are article surfaces on the
	// homepage.  The many info/ links in .tabs-list are procedure cards, not a
	// news feed, so a document-wide info/ scan would create false records.
	doc.Find(".back-sm .link-box").Each(func(_ int, box *goquery.Selection) {
		heading := strings.Join(strings.Fields(box.Find(".link-tt").First().Text()), " ")
		if heading != "通知公告" && heading != "新闻动态" {
			return
		}
		box.Find("ul.news-list li").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			typ := heading
			if label := strings.TrimSpace(row.Find("b").First().Text()); label != "" {
				label = strings.Trim(label, "【】[] ")
				switch label {
				case "政策":
					typ = "科研政策"
				case "动态":
					typ = "科研动态"
				default:
					typ = label
				}
			}
			add(anchor, row.Find("span").Last().Text(), typ)
		})
	})

	// The carousel is a real local news module.  It has no publication date;
	// the normal missing-date enrichment will obtain it from the detail page.
	doc.Find(".back-sm .sy_ban .swiper-slide a[href]").Each(func(_ int, anchor *goquery.Selection) {
		add(anchor, "", "科研动态")
	})

	if len(items) == 0 {
		return nil, fmt.Errorf("sro: homepage contains no local server-rendered news")
	}
	return items, nil
}

func sroListType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).Last().AttrOr("content", "")), " "); value != "" {
		return value
	}
	return sroBreadcrumbType(doc)
}

func sroListItemType(defaultType, articleURL string) string {
	if match := sroColumnPathRE.FindStringSubmatch(articleURL); len(match) == 2 {
		if value := sroColumnTypes[match[1]]; value != "" && (defaultType == "通知公告" || defaultType == "常用下载") {
			return value
		}
	}
	return defaultType
}

func sroPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	return parseTotalPagesFromSroPagination(doc.Find(".subRight02 .pb_sys_common, .subRight02 .p_pages").First())
}

func parseTotalPagesFromSroPagination(scope *goquery.Selection) int {
	if scope == nil || scope.Length() == 0 {
		return 0
	}
	maxPage := 0
	scope.Find(".p_no a, .p_no_d, .p_no").Each(func(_ int, node *goquery.Selection) {
		for _, value := range []string{strings.TrimSpace(node.Text()), strings.TrimSpace(node.AttrOr("href", ""))} {
			if n := sroTrailingPageNumber(value); n > maxPage {
				maxPage = n
			}
		}
	})
	if last := scope.Find(".p_last a, .p_last").First(); last.Length() > 0 {
		if n := sroTrailingPageNumber(last.AttrOr("href", "")); n > maxPage {
			maxPage = n
		}
		if n := sroTrailingPageNumber(last.Text()); n > maxPage {
			maxPage = n
		}
	}
	return maxPage
}

func sroTrailingPageNumber(value string) int {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	match := regexp.MustCompile(`(?:^|/)(\d+)(?:\.(?:htm|html))?(?:[?#].*)?$`).FindStringSubmatch(value)
	if len(match) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(match[1])
	return n
}

func parseAdapter_sroArticle(raw []byte, baseURL string) (*model.Article, error) {
	if sroBlocked(raw) {
		return nil, fmt.Errorf("sro: article is a blocked, login, or error page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("sro: parse article HTML: %w", err)
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("sro: response is not an article (pageType=%q)", pageType)
	}
	scope := doc.Find(".pageArticle").First()
	if scope.Length() == 0 {
		return nil, fmt.Errorf("sro: article has no .pageArticle container")
	}
	titleSel := scope.Find("form[name='_newscontent_fromname'] .articleTitle h3, .articleTitle h3").First()
	contentSel := scope.Find("form[name='_newscontent_fromname'] #vsb_content .v_news_content, form[name='_newscontent_fromname'] .v_news_content").First()
	if titleSel.Length() == 0 || strings.TrimSpace(titleSel.Text()) == "" {
		return nil, fmt.Errorf("sro: article has no fixed detail title")
	}
	if contentSel.Length() == 0 || (strings.TrimSpace(contentSel.Text()) == "" && contentSel.Find("img, table, a").Length() == 0) {
		return nil, fmt.Errorf("sro: article has no fixed .v_news_content body")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = sroCleanTitle(titleSel.Text())
	article.Type = sroBreadcrumbType(doc)
	article.Source = ""
	article.Author = ""
	article.Editor = ""
	article.Photographer = ""
	article.Date, article.PublishedAt = sroDateValues(scope.Find(".articleAuthor").First().Text())

	article.Content = extractBodaContentText(contentSel)
	article.Images = nil
	article.Attachments = nil
	contentClone := contentSel.Clone()
	seenImages := make(map[string]bool)
	seenAttachments := make(map[string]bool)
	contentClone.Find("img").Each(func(_ int, image *goquery.Selection) {
		rawURL := sroFirstResourceAttr(image)
		resolved := sroResourceURL(baseURL, rawURL)
		if resolved == "" || seenImages[resolved] {
			image.Remove()
			return
		}
		seenImages[resolved] = true
		article.Images = append(article.Images, resolved)
		image.SetAttr("src", resolved)
	})
	contentClone.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if sroIsPseudoAttachmentLink(link, href) {
			link.Remove()
			return
		}
		if !sroIsAttachmentLink(link, href) {
			return
		}
		resolved := sroResourceURL(baseURL, href)
		if resolved == "" || seenAttachments[resolved] {
			link.Remove()
			return
		}
		name := strings.Join(strings.Fields(link.AttrOr("download", "")), " ")
		if name == "" {
			name = strings.Join(strings.Fields(link.Text()), " ")
		}
		if name == "" {
			name = href
		}
		seenAttachments[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	})
	if html, htmlErr := contentClone.Html(); htmlErr == nil {
		article.ContentHTML = html
	}
	if article.Content == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("sro: article body is empty")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func sroBreadcrumbType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	bread := doc.Find(".bread").First()
	if bread.Length() == 0 {
		return ""
	}
	typ := ""
	bread.Find("a").Each(func(_ int, link *goquery.Selection) {
		value := strings.Join(strings.Fields(link.Text()), " ")
		if value != "" && value != "首页" && value != "网站首页" {
			typ = value
		}
	})
	return typ
}

func sroDateValues(raw string) (string, string) {
	text := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if text == "" {
		return "", ""
	}
	match := sroPublicationRE.FindStringSubmatch(text)
	if match == nil {
		if bare := bareDateRe.FindStringSubmatch(text); bare != nil {
			date := normalizeDateParts(bare[1], bare[2], bare[3])
			return date, date
		}
		return "", ""
	}
	date := normalizeDateParts(match[1], match[2], match[3])
	if date == "" {
		return "", ""
	}
	if match[4] == "" {
		return date, date
	}
	second := match[6]
	if second == "" {
		second = "00"
	}
	publishedAt := fmt.Sprintf("%s %02s:%s:%s", date, match[4], match[5], second)
	return date, publishedAt
}

func sroCleanTitle(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func sroArticleTitleOK(title string) bool {
	if title == "" || len([]rune(title)) > 200 {
		return false
	}
	switch title {
	case "首页", "通知公告", "新闻动态", "更多", "查看更多", "常用下载":
		return false
	default:
		return true
	}
}

func sroArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	base, baseErr := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || baseErr != nil || parsed.Host == "" || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return "", false
	}
	if !sroArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func sroBlocked(raw []byte) bool {
	return sroBlockedRE.Match(raw)
}

func sroFirstResourceAttr(sel *goquery.Selection) string {
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
		if value := strings.TrimSpace(sel.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func sroIsAttachmentLink(sel *goquery.Selection, href string) bool {
	if href == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	return bodaLooksLikeAttachmentLink(sel, href)
}

func sroIsPseudoAttachmentLink(sel *goquery.Selection, href string) bool {
	if sel == nil || sel.Length() == 0 || !bodaIsPseudoResourceHref(href) {
		return false
	}
	text := strings.ToLower(strings.Join(strings.Fields(sel.Text()), " "))
	return strings.Contains(text, "附件") || strings.Contains(text, "下载") || strings.TrimSpace(sel.AttrOr("download", "")) != ""
}

func sroResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return ""
	}
	path := strings.ToLower(parsed.Path)
	for _, marker := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif", "/pixel.gif", "/loading.gif", "/loading.png", "/placeholder"} {
		if strings.HasSuffix(path, marker) || strings.Contains(path, marker) {
			return ""
		}
	}
	return parsed.String()
}

func sroFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := sroBaseURL()
	fullURL, ok := sroArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("sro: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_sroArticle(body, baseURL)
	if err != nil {
		return nil, nil, err
	}
	candidateImages := append([]string(nil), article.Images...)
	candidateAttachments := append([]model.Attachment(nil), article.Attachments...)
	article.Images = sroVerifyImages(candidateImages, fullURL)
	article.Attachments = sroVerifyAttachments(candidateAttachments, fullURL)
	article.ContentHTML = sroSanitizeContentHTML(article.ContentHTML, baseURL, article.Images, article.Attachments)
	return article, body, nil
}

func sroVerifyImages(images []string, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := sroResourceURL(referer, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := sroFetchResource(resolved, referer, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func sroVerifyAttachments(attachments []model.Attachment, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := sroResourceURL(referer, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := sroFetchResource(resolved, referer, false)
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

func sroFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	resolved := sroResourceURL(referer, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("sro: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("sro: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := http.DefaultClient
	if defaultClient != nil {
		copyClient := *defaultClient
		copyClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
				return nil
			}
			return http.ErrUseLastResponse
		}
		client = &copyClient
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("sro: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("sro: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, sroMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("sro: read resource %s: %w", resolved, err)
	}
	if len(body) > sroMaxResourceBytes || !sroResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("sro: resource %s returned an invalid type, size, magic signature, or error page", resolved)
	}
	return body, contentType, nil
}

func sroResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
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
	textProbe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(probe))), "\ufeff")
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "请输入验证码", "验证码下载附件", "请先登录", "登录页面", "页面不存在", "404 not found", "access denied", "cloudflare", "just a moment", "安全验证", "请求已被阻断",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	magic := sroResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && sroIsImageMagic(magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "zip") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || strings.Contains(mediaType, "msword") || strings.Contains(mediaType, "ms-excel") || strings.Contains(mediaType, "ms-powerpoint"):
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values":
		return true
	default:
		return magic != ""
	}
}

func sroIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff":
		return true
	default:
		return false
	}
}

func sroResourceMagic(body []byte) string {
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

func sroSanitizeContentHTML(rawHTML, baseURL string, verifiedImages []string, verifiedAttachments []model.Attachment) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + rawHTML + "</div>"))
	if err != nil {
		return rawHTML
	}
	root := doc.Find("body > div").First()
	if root.Length() == 0 {
		return rawHTML
	}
	images := make(map[string]bool, len(verifiedImages))
	for _, image := range verifiedImages {
		if resolved := sroResourceURL(baseURL, image); resolved != "" {
			images[resolved] = true
		}
	}
	attachments := make(map[string]bool, len(verifiedAttachments))
	for _, attachment := range verifiedAttachments {
		if resolved := sroResourceURL(baseURL, attachment.URL); resolved != "" {
			attachments[resolved] = true
		}
	}
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := sroResourceURL(baseURL, sroFirstResourceAttr(image))
		if resolved == "" || !images[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if sroIsPseudoAttachmentLink(link, href) {
			link.Remove()
			return
		}
		if !sroIsAttachmentLink(link, href) {
			return
		}
		resolved := sroResourceURL(baseURL, href)
		if resolved == "" || !attachments[resolved] {
			link.Remove()
			return
		}
		link.SetAttr("href", resolved)
	})
	result, err := root.Html()
	if err != nil {
		return rawHTML
	}
	return result
}
