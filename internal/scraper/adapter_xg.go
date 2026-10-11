package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// xg is a separate ASP/Vue-style application mounted below /web. These are
// the real list endpoints exposed by its current navigation. Static profile
// pages (InfoShow/InstitutionSetting) are not feeds. The three 学生服务
// download endpoints (办事流程/规章制度/表格下载) are deliberately absent:
// their rows are direct xgservice getfile links to .doc/.pdf files, not
// article pages, and every round fetched them as articles and failed. The
// 规章制度 page now even requires a CAS login.
var xgSubPages = []string{
	"Home/PushNewsList?Lmk7LJw34Jmu=010j.shtml",
	"Home/NewsList?LJw34Jmu=011e.shtml",
	"Home/NewsList?LJw34Jmu=011c_8kL82P8kl=lzm4.shtml",
	"Home/ColourfulCollegeNewsList",
	"Home/NewsList?xvw34vmu=010e.shtml",
	"Publicity/List",
	"Home/TroupeActive",
}

type xgFeedSpec struct {
	typ string
}

var xgFeeds = map[string]xgFeedSpec{
	"Home/PushNewsList?Lmk7LJw34Jmu=010j.shtml":        {typ: "通知公告"},
	"Home/NewsList?LJw34Jmu=011e.shtml":                {typ: "扬华新闻"},
	"Home/NewsList?LJw34Jmu=011c_8kL82P8kl=lzm4.shtml": {typ: "菁华岁月"},
	"Home/ColourfulCollegeNewsList":                    {typ: "多彩学院"},
	"Home/NewsList?xvw34vmu=010e.shtml":                {typ: "学工之家"},
	"Publicity/List":                                   {typ: "公示"},
	"Home/TroupeActive":                                {typ: "学生活动"},
}

var (
	xgDetailPathRE  = regexp.MustCompile(`(?i)^/web/home/(?:detail|colourfulcollegedetail)$`)
	xgFilePathRE    = regexp.MustCompile(`(?i)^/service/uploadserver/down/getfile$`)
	xgDateTimeRE    = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})日?\s*(?:[Tt]|\s+)\s*(\d{1,2})[:：](\d{2})(?::(\d{2}))?`)
	xgDateRE        = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})日?`)
	xgPageRE        = regexp.MustCompile(`(?:[?&]page=|第\s*)(\d+)`)
	xgRecordCountRE = regexp.MustCompile(`总共有\s*(\d+)\s*条记录`)
	xgLabelSourceRE = regexp.MustCompile(`(?:信息来源|来源)\s*[:：]\s*([^\s，。；;]+)`)
	xgLabelTypeRE   = regexp.MustCompile(`(?:栏目|类型|类别|分类)\s*[:：]\s*([^\s，。；;]+)`)
	xgOnclickHrefRE = regexp.MustCompile(`window\.location\.href\s*=\s*['"]([^'"]+)['"]`)
)

func init() {
	registerAdapter("xg", SiteAdapter{
		SubPages:     xgSubPages,
		ParseList:    parseAdapter_xgList,
		ParseArticle: parseAdapter_xgArticle,
		ListPage:     xgListPage,
		FetchArticle: xgFetchArticle,
	})
}

func xgBaseURL() string {
	if site := AllSites["xg"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	}
	return "https://xg.swjtu.edu.cn/web"
}

func xgFeedKey(slug string) string {
	return strings.Trim(strings.TrimSpace(slug), "/")
}

func xgFeedSpecFor(slug string) (xgFeedSpec, bool) {
	spec, ok := xgFeeds[xgFeedKey(slug)]
	return spec, ok
}

func xgFeedURL(slug string) string {
	return resolveURL(xgBaseURL()+"/", xgFeedKey(slug))
}

// xgListPage fetches page one first even for an older page. The application
// puts its node parameter on the generated pagination links; copying that
// link keeps the request compatible with the live endpoint instead of
// guessing a different parameter name for each list controller.
func xgListPage(slug string, page int) ([]model.NewsItem, int, error) {
	if page < 1 {
		return nil, 0, fmt.Errorf("xg: list page must be positive: %d", page)
	}
	slug = xgFeedKey(slug)
	if slug == "index" {
		return xgHomepageList(page)
	}
	if _, ok := xgFeedSpecFor(slug); !ok {
		return nil, 0, fmt.Errorf("xg: %q is not a supported dynamic list route", slug)
	}
	baseURL := xgBaseURL()
	firstURL := xgFeedURL(slug)
	firstBody, err := fetch(firstURL)
	if err != nil {
		return nil, 0, fmt.Errorf("xg: fetch first page for %s: %w", slug, err)
	}
	firstDoc, err := goquery.NewDocumentFromReader(strings.NewReader(string(firstBody)))
	if err != nil {
		return nil, 0, fmt.Errorf("xg: parse first page for %s: %w", slug, err)
	}
	total := xgPageCount(firstDoc)
	if page > 1 && total > 0 && page > total {
		return nil, total, fmt.Errorf("xg: requested page %d beyond %d for %s", page, total, slug)
	}
	if page == 1 {
		items, parseErr := xgParseListHTML(firstBody, baseURL, xgFeedSpecForMust(slug).typ)
		return items, total, parseErr
	}

	pageURL := xgSourcePageURL(firstDoc, firstURL, page)
	if pageURL == "" {
		pageURL = xgSetPage(firstURL, page)
	}
	body, err := fetch(pageURL)
	if err != nil {
		fallback := xgSetPage(firstURL, page)
		if fallback == pageURL {
			return nil, total, fmt.Errorf("xg: fetch page %d for %s: %w", page, slug, err)
		}
		body, err = fetch(fallback)
		if err != nil {
			return nil, total, fmt.Errorf("xg: fetch page %d for %s: %w", page, slug, err)
		}
	}
	items, parseErr := xgParseListHTML(body, baseURL, xgFeedSpecForMust(slug).typ)
	return items, total, parseErr
}

func xgFeedSpecForMust(slug string) xgFeedSpec {
	spec, _ := xgFeedSpecFor(slug)
	return spec
}

// The served /web homepage renders its section lists server-side. Only the
// four boxes below carry article rows: 公示专栏 rows are javascript:
// placeholders and 学生活动 rows are event cards with a location, so neither
// is an article list. The page is not paginated.
func xgHomepageList(page int) ([]model.NewsItem, int, error) {
	if page > 1 {
		return nil, 1, fmt.Errorf("xg: homepage has a single page")
	}
	baseURL := xgBaseURL()
	body, err := fetch(baseURL)
	if err != nil {
		return nil, 0, fmt.Errorf("xg: fetch homepage: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, fmt.Errorf("xg: parse homepage: %w", err)
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	for _, selector := range []string{".notice-box", ".news-box", ".colorful-college", ".xg-box"} {
		section := doc.Find(selector).First()
		if section.Length() == 0 {
			continue
		}
		typ := strings.TrimSpace(section.Find(".title-name").First().Text())
		section.Find("ul > li").Each(func(_ int, row *goquery.Selection) {
			xgAppendListItem(&items, seen, row.Find("a[href]").First(), row, typ, baseURL)
		})
	}
	if len(items) == 0 {
		return nil, 1, fmt.Errorf("xg: homepage contains no article entries")
	}
	return items, 1, nil
}

func xgSourcePageURL(doc *goquery.Document, firstURL string, page int) string {
	if doc == nil || page < 2 {
		return ""
	}
	var result string
	doc.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return true
		}
		resolved := cleanURL(resolveURL(firstURL, href))
		parsed, err := url.Parse(resolved)
		if err != nil || parsed.Query().Get("page") != fmt.Sprintf("%d", page) {
			return true
		}
		result = resolved
		return false
	})
	return result
}

func xgSetPage(rawURL string, page int) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set("page", fmt.Sprintf("%d", page))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func parseAdapter_xgList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	return xgParseListHTML(raw, baseURL, "")
}

func xgParseListHTML(raw []byte, baseURL, typ string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xg: parse list HTML: %w", err)
	}
	if typ == "" {
		typ = xgDocumentType(doc)
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	recognized := false
	// 菁华岁月 renders its photo galleries as ul.video-list cards whose rows
	// carry the detail URL in an onclick handler instead of an anchor href.
	if rows := doc.Find("ul.video-list > li"); rows.Length() > 0 {
		recognized = true
		rows.Each(func(_ int, row *goquery.Selection) {
			match := xgOnclickHrefRE.FindStringSubmatch(row.AttrOr("onclick", ""))
			if match == nil {
				return
			}
			xgAppendOnclickListItem(&items, seen, row, match[1], typ, baseURL)
		})
	}
	for _, selector := range []string{".block-ctxlist", ".block-list", ".news-list", ".list-item", ".activity-list", ".publicity-list"} {
		blocks := doc.Find(selector)
		if blocks.Length() == 0 {
			continue
		}
		recognized = true
		blocks.Each(func(_ int, block *goquery.Selection) {
			anchors := block.Find("a[href]")
			if _, ok := block.Attr("href"); ok {
				anchors = block.AddSelection(block)
			}
			anchors.Each(func(_ int, link *goquery.Selection) {
				xgAppendListItem(&items, seen, link, xgListRow(link, block), typ, baseURL)
			})
		})
	}
	// A few controller responses use no stable row class but do expose their
	// detail links inside the content list. Restrict this fallback to the
	// detail endpoint, never to arbitrary info/ links in the shell.
	if !recognized {
		links := doc.Find("a[href]").FilterFunction(func(_ int, link *goquery.Selection) bool {
			return xgIsDetailURL(cleanURL(resolveURL(baseURL, link.AttrOr("href", ""))))
		})
		if links.Length() > 0 {
			recognized = true
			links.Each(func(_ int, link *goquery.Selection) {
				xgAppendListItem(&items, seen, link, xgListRow(link, link.Parent()), typ, baseURL)
			})
		}
	}
	if !recognized {
		return nil, fmt.Errorf("xg: response has no dynamic list container")
	}
	return items, nil
}

// xgAppendOnclickListItem mirrors xgAppendListItem for the 菁华岁月 card rows,
// whose URL lives in the row onclick rather than in an anchor href.
func xgAppendOnclickListItem(items *[]model.NewsItem, seen map[string]bool, row *goquery.Selection, href, typ, baseURL string) {
	if row == nil || row.Length() == 0 {
		return
	}
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	if href == "" || seen[fullURL] || !xgIsDetailURL(fullURL) {
		return
	}
	title := strings.TrimSpace(row.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(row.Find(".des-line span").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(row.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 240 {
		return
	}
	date, publishedAt := xgRowDate(row)
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func xgListRow(link, fallback *goquery.Selection) *goquery.Selection {
	if link == nil || link.Length() == 0 {
		return fallback
	}
	for _, selector := range []string{"li", ".block-ctxlist-item", ".block-ctxlist-row", ".news-item", ".list-item", ".activity-item", ".publicity-item", ".item"} {
		if row := link.Closest(selector); row.Length() > 0 {
			return row
		}
	}
	if parent := link.Parent(); parent.Length() > 0 {
		return parent
	}
	return fallback
}

func xgAppendListItem(items *[]model.NewsItem, seen map[string]bool, link, row *goquery.Selection, typ, baseURL string) {
	if link == nil || link.Length() == 0 || isInNavArea(link) {
		return
	}
	href := strings.TrimSpace(link.AttrOr("href", ""))
	fullURL := cleanURL(resolveURL(baseURL, href))
	// getfile rows are downloadable resources, not articles; swjtu-archive
	// fetches every feed item as an article, so file links must never become
	// feed items (they failed every sync round as "no article shell").
	if href == "" || seen[fullURL] || !xgIsDetailURL(fullURL) {
		return
	}
	title := strings.TrimSpace(link.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(link.Find(".block-ctxlist-title, .block-title, .news-title, .title, .name, h3, h4, p").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(link.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 240 {
		return
	}
	date, publishedAt := xgRowDate(row)
	if typ == "" {
		typ = strings.Trim(strings.TrimSpace(row.Find(".tag, .type, .category").First().Text()), "[]【】")
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
}

func xgFileName(link *goquery.Selection, fullURL string) string {
	for _, value := range []string{link.AttrOr("download", ""), link.AttrOr("name", ""), link.AttrOr("title", ""), link.Text()} {
		if value = strings.Join(strings.Fields(value), " "); value != "" && value != "下载" && value != "点击下载" {
			return value
		}
	}
	parsed, err := url.Parse(fullURL)
	if err == nil {
		if value := parsed.Query().Get("name"); value != "" {
			return value
		}
	}
	return fullURL
}

func xgRowDate(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	for _, selector := range []string{".date", ".time", ".block-ctxlist-date", ".block-ctxlist-time", ".newsDate", ".noticeDate", "time"} {
		if value := strings.Join(strings.Fields(row.Find(selector).First().Text()), " "); value != "" {
			if date, publishedAt := xgDateValues(value); date != "" {
				return date, publishedAt
			}
		}
	}
	return xgDateValues(strings.Join(strings.Fields(row.Text()), " "))
}

func xgDateValues(value string) (string, string) {
	value = strings.Join(strings.Fields(value), " ")
	if match := xgDateTimeRE.FindStringSubmatch(value); match != nil {
		second := match[6]
		if second == "" {
			second = "00"
		}
		published := fmt.Sprintf("%s-%02s-%02s %02s:%s:%s", match[1], match[2], match[3], match[4], match[5], second)
		return published[:10], published
	}
	if match := xgDateRE.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func xgPageCount(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	maxPage := 0
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := link.AttrOr("href", "")
		parsed, err := url.Parse(href)
		if err != nil {
			return
		}
		if value := parsed.Query().Get("page"); value != "" {
			var page int
			fmt.Sscanf(value, "%d", &page)
			if page > maxPage {
				maxPage = page
			}
		}
	})
	if maxPage > 0 {
		return maxPage
	}
	text := strings.Join(strings.Fields(doc.Text()), " ")
	if match := xgPageRE.FindStringSubmatch(text); match != nil {
		var page int
		fmt.Sscanf(match[1], "%d", &page)
		if page > 0 {
			return page
		}
	}
	if xgRecordCountRE.MatchString(text) {
		// The controller returned a recognized list but no numbered links. It
		// is a valid one-page result; do not infer multiple pages from row count.
		return 1
	}
	if doc.Find(".block-ctxlist, .block-list, .news-list, .list-item, .activity-list, .publicity-list, ul.video-list").Length() > 0 {
		return 1
	}
	return 0
}

func xgDocumentType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".detail-breadcrumb", ".breadcrumb", ".location", ".list-title", ".page-title", "h1", "h2"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if match := xgLabelTypeRE.FindStringSubmatch(value); match != nil {
			return strings.TrimSpace(match[1])
		}
		for _, spec := range xgFeeds {
			if strings.Contains(value, spec.typ) {
				return spec.typ
			}
		}
	}
	return ""
}

func parseAdapter_xgArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("xg: parse article HTML: %w", err)
	}
	var titleSel *goquery.Selection
	for _, selector := range []string{".detail-content-title h3", ".detail-content-title h2", ".detail-content-title", ".detail-title"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			titleSel = found
			break
		}
	}
	contentSel := doc.Find(".detail-content-text .v_news_content, .detail-content-text, .detail-content, .content-text").First()
	if titleSel.Length() == 0 || strings.TrimSpace(titleSel.Text()) == "" {
		return nil, fmt.Errorf("xg: dynamic detail has no title")
	}
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("xg: dynamic detail has no detail-content-text body")
	}
	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = strings.Join(strings.Fields(titleSel.Text()), " ")
	metaText := strings.Join(strings.Fields(titleSel.Parent().Text()), " ")
	if date, publishedAt := xgDateValues(metaText); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	if article.Source == "" {
		if match := xgLabelSourceRE.FindStringSubmatch(metaText); match != nil {
			article.Source = strings.TrimSpace(match[1])
		}
	}
	if article.Type == "" {
		article.Type = xgDocumentType(doc)
	}
	xgFinalizeArticleContent(article, contentSel, doc, baseURL)
	if strings.TrimSpace(article.Content) == "" && contentSel.Find("img, table, a, object, iframe").Length() == 0 {
		return nil, fmt.Errorf("xg: dynamic detail body is empty")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func xgFinalizeArticleContent(article *model.Article, contentSel *goquery.Selection, doc *goquery.Document, baseURL string) {
	if article == nil || contentSel == nil || contentSel.Length() == 0 {
		return
	}
	article.Content = extractBodaContentText(contentSel)
	article.Images = nil
	seenImages := make(map[string]bool)
	contentSel.Find("img").Each(func(_ int, image *goquery.Selection) {
		src := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				src = value
				break
			}
		}
		resolved := cleanURL(resolveURL(baseURL, src))
		if resolved == "" || seenImages[resolved] {
			return
		}
		seenImages[resolved] = true
		article.Images = append(article.Images, resolved)
		image.SetAttr("src", resolved)
	})
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	add := func(link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		resolved := cleanURL(resolveURL(baseURL, href))
		if href == "" || seen[resolved] || !xgLooksLikeAttachment(link, href) {
			return
		}
		name := xgFileName(link, resolved)
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		link.SetAttr("href", resolved)
	}
	contentSel.Find("a[href]").Each(func(_ int, link *goquery.Selection) { add(link) })
	// xg's download rows sit beside detail-content-text rather than inside it;
	// the exact wrapper differs between the three controllers, so scan only
	// non-navigation links and let the xg file/extension predicate decide.
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if isInNavArea(link) {
			return
		}
		add(link)
	})
	if html, err := contentSel.Html(); err == nil {
		article.ContentHTML = html
	}
}

func xgLooksLikeAttachment(link *goquery.Selection, href string) bool {
	if link == nil || link.Length() == 0 || href == "" {
		return false
	}
	if xgIsFileURL(cleanURL(resolveURL(xgBaseURL(), href))) || bodaLooksLikeAttachmentLink(link, href) {
		return true
	}
	return strings.Contains(strings.ToLower(link.Parent().Text()), "附件")
}

func xgFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := xgBaseURL()
	if !xgIsDetailURL(articleURL) {
		return nil, nil, fmt.Errorf("xg: refusing non-detail URL %q", articleURL)
	}
	body, err := fetch(articleURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_xgArticle(body, baseURL)
	if err != nil {
		return nil, nil, err
	}
	if article.Type == "" {
		article.Type = xgDetailType(articleURL)
	}
	return article, body, nil
}

func xgDetailType(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, "010j"):
		return "通知公告"
	case strings.Contains(lower, "011e"):
		return "扬华新闻"
	case strings.Contains(lower, "011c"):
		return "菁华岁月"
	case strings.Contains(lower, "010e"):
		return "学工之家"
	default:
		return ""
	}
}

func xgIsDetailURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	base, baseErr := url.Parse(xgBaseURL())
	return err == nil && baseErr == nil && xgDetailPathRE.MatchString(parsed.Path) && strings.EqualFold(parsed.Host, base.Host)
}

func xgIsFileURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || !xgFilePathRE.MatchString(parsed.Path) {
		return false
	}
	return parsed.Query().Get("file") != "" || parsed.Query().Get("name") != ""
}
