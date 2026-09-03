package scraper

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"swjtu-cli/internal/model"
)

const bwcBaseURL = "https://bwc.swjtu.edu.cn"

// These are the site's actual article-list entry points. Static information
// pages (department overview, service instructions, and the empty English
// branches) are deliberately not presented as feeds.
var bwcSubPages = []string{
	"tzgg",
	"gzdt",
	"aqjs",
	"aqjs/aqjy",
	"aqjs/jsal",
	"djzj/xxzj",
	"zcfg/flfg",
	"zcfg/gzzd",
	"fwzn/xzzx",
	"English/About",
	"English/Party_s_Works",
	"English/Party_s_Works/Honest_Administration",
	"English/Laws",
	"English/Rules",
	"English/Announcement",
	"English/Service_Guide",
	"English/Service_Guide/Work_Processes",
}

var (
	bwcArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	bwcPageCounterRE = regexp.MustCompile(`(?:^|[^0-9])[0-9]+\s*/\s*([0-9]+)(?:[^0-9]|$)`)
	bwcAttachmentRE  = regexp.MustCompile(`(?i)\.(?:pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#])`)
)

// bwcAdapter keeps this site's non-standard list and article markup out of
// the shared 博达 parser. In particular, a successful HTML shell is not a
// successful feed: every parser below requires the site's real content nodes.
func init() {
	registerAdapter("bwc", SiteAdapter{
		SubPages:     bwcSubPages,
		ParseList:    parseAdapter_bwcList,
		ParseArticle: parseAdapter_bwcArticle,
		PageURL:      bwcPageURL,
		ListPage:     bwcListPage,
	})
}

func parseAdapter_bwcList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBwcNewsList(html, baseURL)
}

func parseAdapter_bwcArticle(html []byte, baseURL string) (*model.Article, error) {
	return parseBwcArticle(html, baseURL)
}

// bwcPageURL maps a user-facing page number to the generated file name. BWC
// publishes the newest page as slug.htm, then writes logical page 2 to
// slug/{total-1}.htm, with the oldest page at slug/1.htm.
func bwcPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 || (totalPages > 0 && page > totalPages) {
		return ""
	}
	if slug == "" || strings.EqualFold(slug, "index") {
		if page > 1 {
			return ""
		}
		return baseURL + "/index" + extension
	}
	if page == 1 {
		return baseURL + "/" + slug + extension
	}
	if totalPages <= 1 {
		return ""
	}
	generatedPage := totalPages - page + 1
	if generatedPage < 1 {
		return ""
	}
	return baseURL + "/" + slug + "/" + strconv.Itoa(generatedPage) + extension
}

// bwcListPage is used by the shared BodaSite pagination API, but keeps all
// BWC-specific fetching and reverse numbering inside this adapter.
func bwcListPage(slug string, page int) ([]model.NewsItem, int, error) {
	baseURL := bwcBaseURL
	if site := AllSites["bwc"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	if page < 1 {
		page = 1
	}

	totalPages := 0
	if page > 1 {
		firstBody, err := bwcFetchListPage(bwcPageURL(baseURL, slug, 1, 0, ".htm"))
		if err != nil {
			return nil, 0, fmt.Errorf("bwc: read first page for %s: %w", slug, err)
		}
		totalPages = bwcTotalPages(firstBody)
		if totalPages < 1 {
			return nil, 0, fmt.Errorf("bwc: %s does not expose a reachable page count", slug)
		}
	}

	body, err := bwcFetchListPage(bwcPageURL(baseURL, slug, page, totalPages, ".htm"))
	if err != nil {
		return nil, 0, err
	}
	items, err := parseBwcNewsList(body, baseURL)
	if err != nil {
		return nil, 0, err
	}
	if totalPages == 0 {
		totalPages = bwcTotalPages(body)
	}
	if totalPages == 0 {
		totalPages = 1
	}
	return items, totalPages, nil
}

func bwcFetchListPage(pageURL string) ([]byte, error) {
	body, err := fetch(pageURL)
	if err == nil {
		return body, nil
	}
	if strings.HasSuffix(strings.ToLower(pageURL), ".htm") {
		if fallback, fallbackErr := fetch(strings.TrimSuffix(pageURL, ".htm") + ".html"); fallbackErr == nil {
			return fallback, nil
		}
	}
	return nil, fmt.Errorf("bwc: fetch list %s: %w", pageURL, err)
}

func bwcTotalPages(html []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}

	// English pages report e.g. "共65条 1/4" in an element named fanye.
	for _, text := range []string{
		doc.Find("[id^='fanye']").Text(),
		doc.Find(".p_t").Text(),
	} {
		if match := bwcPageCounterRE.FindStringSubmatch(strings.Join(strings.Fields(text), " ")); match != nil {
			if total, err := strconv.Atoi(match[1]); err == nil && total > 0 {
				return total
			}
		}
	}
	if total := parseTotalPages(html); total > 0 {
		return total
	}

	if doc.Find(".text-wrapper_4 .item[href], .advice .item[href], .aq .item[href], .zc .item[href], .xz .item[href]").Length() > 0 {
		return 1
	}
	if doc.Find("div.group_7[href], #MainContent_GridView1 a[href*='info/']").Length() > 0 {
		return 1
	}
	return 0
}

func parseBwcNewsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("bwc: parse list HTML: %w", err)
	}

	var items []model.NewsItem
	seen := make(map[string]bool)
	add := func(href, title, date, typ string) {
		fullURL, ok := bwcArticleURL(baseURL, href)
		if !ok {
			return
		}
		title = cleanTitle(strings.Join(strings.Fields(title), " "))
		if !isValidNewsTitle(title) || seen[fullURL] {
			return
		}
		date, publishedAt := bwcListDate(date)
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

	// The homepage contains article-looking carousel/recommendation URLs.
	// Only these five named modules are article feeds; no document-wide scan is
	// allowed here.
	homeSections := []struct {
		selector string
		typ      string
	}{
		{selector: ".text-wrapper_4", typ: "工作动态"},
		{selector: ".advice", typ: "通知公告"},
		{selector: ".aq", typ: "安全警示"},
		{selector: ".zc", typ: "政策法规"},
		{selector: ".xz", typ: "下载专区"},
	}
	homeFound := false
	for _, section := range homeSections {
		found := doc.Find(section.selector)
		if found.Length() == 0 {
			continue
		}
		homeFound = true
		found.Find(".item[href]").Each(func(_ int, sel *goquery.Selection) {
			title := strings.TrimSpace(sel.Find(".title").First().Text())
			if title == "" {
				title = strings.TrimSpace(sel.Find("a").First().Text())
			}
			date := strings.TrimSpace(sel.Find(".date, .time, .text-group_3").First().Text())
			add(sel.AttrOr("href", ""), title, date, section.typ)
		})
	}
	if homeFound {
		if len(items) == 0 {
			return nil, fmt.Errorf("bwc: homepage modules contained no real article items")
		}
		return items, nil
	}

	// Chinese BWC list pages use a div with the article URL as its href.
	groups := doc.Find(".right div.group_7[href]")
	if groups.Length() == 0 {
		groups = doc.Find("div.group_7[href]")
	}
	if groups.Length() > 0 {
		typ := bwcBreadcrumbType(doc)
		groups.Each(func(_ int, sel *goquery.Selection) {
			title := strings.TrimSpace(sel.Find(".title").First().Text())
			date := strings.TrimSpace(sel.Find(".text-group_3").First().Text())
			add(sel.AttrOr("href", ""), title, date, typ)
		})
		if len(items) == 0 {
			return nil, fmt.Errorf("bwc: group_7 list contained no valid article items")
		}
		return items, nil
	}

	// The old English pages are server-rendered tables. Restrict extraction to
	// this table so header/menu/related links cannot become articles.
	englishTable := doc.Find("#MainContent_GridView1")
	if englishTable.Length() > 0 {
		typ := bwcBreadcrumbType(doc)
		englishTable.Find("tr").Each(func(_ int, row *goquery.Selection) {
			var articleLink *goquery.Selection
			row.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
				if _, ok := bwcArticleURL(baseURL, link.AttrOr("href", "")); ok {
					articleLink = link
					return false
				}
				return true
			})
			if articleLink == nil {
				return
			}
			title := strings.TrimSpace(articleLink.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(articleLink.Text())
			}
			date := strings.TrimSpace(row.Find("span[id*='Label7'], .date, .time").First().Text())
			add(articleLink.AttrOr("href", ""), title, date, typ)
		})
		if len(items) == 0 {
			return nil, fmt.Errorf("bwc: English article table contained no real rows")
		}
		return items, nil
	}

	return nil, fmt.Errorf("bwc: no recognized article-list container")
}

func bwcArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !bwcArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !isValidNewsURL(fullURL) {
		return "", false
	}
	return fullURL, true
}

func bwcListDate(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if normalized := normalizePublishedAt(raw); normalized != "" {
		if strings.Contains(raw, ":") || strings.ContainsAny(raw, "时分秒") {
			return normalized[:10], normalized
		}
		return normalized[:10], ""
	}
	return raw, ""
}

func bwcBreadcrumbType(doc *goquery.Document) string {
	var values []string
	doc.Find(".nav .text").Each(func(_ int, sel *goquery.Selection) {
		if value := strings.Join(strings.Fields(sel.Text()), " "); value != "" {
			values = append(values, value)
		}
	})
	if len(values) == 0 {
		doc.Find("#MainContent_LinkHome").Each(func(_ int, sel *goquery.Selection) {
			if value := strings.Join(strings.Fields(sel.Text()), " "); value != "" {
				values = append(values, value)
			}
		})
	}
	for i := len(values) - 1; i >= 0; i-- {
		switch strings.ToLower(strings.TrimSpace(values[i])) {
		case "", "首页", "主页", "正文", "home", "中文":
			continue
		default:
			return values[i]
		}
	}
	return ""
}

func parseBwcArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("bwc: parse article HTML: %w", err)
	}

	var title string
	var titleSel *goquery.Selection
	for _, selector := range []string{
		".c1088738_title",
		"#MainContent_LabelTitle",
		".news-title",
		"h1",
	} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			if value := strings.Join(strings.Fields(found.Text()), " "); value != "" {
				title = cleanTitle(value)
				titleSel = found
				break
			}
		}
	}
	if title == "" {
		title = cleanTitle(strings.TrimSpace(doc.Find("meta[name='pageTitle']").AttrOr("content", "")))
	}

	var contentSel *goquery.Selection
	for _, selector := range []string{
		"#vsb_content .v_news_content",
		".v_news_content",
	} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			contentSel = found
			break
		}
	}
	if title == "" || contentSel == nil || contentSel.Length() == 0 {
		return nil, fmt.Errorf("bwc: page is not a rendered article detail")
	}

	date, publishedAt := bwcArticleDate(doc)
	article := &model.Article{
		Title:       title,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        bwcBreadcrumbType(doc),
		Source:      bwcSource(doc),
		Author:      bwcAuthor(doc),
		Content:     bwcArticleText(contentSel),
		Images:      bwcArticleImages(contentSel, baseURL),
	}
	if titleSel == nil {
		titleSel = doc.Find("meta[name='pageTitle']").First()
	}
	_ = titleSel // retained above to make the title source explicit while parsing.

	attachments := bwcArticleAttachments(doc, contentSel, baseURL)
	article.Attachments = attachments

	contentSel.Find("img[src]").Each(func(_ int, sel *goquery.Selection) {
		sel.SetAttr("src", cleanURL(resolveURL(baseURL, sel.AttrOr("src", ""))))
	})
	contentSel.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		sel.SetAttr("href", cleanURL(resolveURL(baseURL, sel.AttrOr("href", ""))))
	})
	if contentHTML, err := contentSel.Html(); err == nil {
		article.ContentHTML = contentHTML
	}

	return article, nil
}

func bwcArticleDate(doc *goquery.Document) (string, string) {
	for _, selector := range []string{".c1088738_date", "#MainContent_LabelDate", ".news_date", ".date"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			if normalized := normalizePublishedAt(found.Text()); normalized != "" {
				return normalized[:10], normalized
			}
		}
	}
	return "", ""
}

func bwcSource(doc *goquery.Document) string {
	for _, selector := range []string{".c1088738_source", ".news_source", "[class*='source']"} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			value := strings.Join(strings.Fields(found.Text()), " ")
			value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "来源："), "来源:"))
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func bwcAuthor(doc *goquery.Document) string {
	if found := doc.Find(".c1088738_author, .news_author").First(); found.Length() > 0 {
		if value := strings.Join(strings.Fields(found.Text()), " "); value != "" {
			return value
		}
	}
	dateSel := doc.Find("#MainContent_LabelDate").First()
	if dateSel.Length() > 0 {
		authorRE := regexp.MustCompile(`录入\s*[:：]?\s*([^\s]+)`)
		if match := authorRE.FindStringSubmatch(strings.Join(strings.Fields(dateSel.Parent().Text()), " ")); len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func bwcArticleText(contentSel *goquery.Selection) string {
	clone := contentSel.Clone()
	clone.Find("script, style, noscript").Remove()
	const nestedBlocks = "p,h1,h2,h3,h4,h5,h6,li,td,th,caption,blockquote"
	var parts []string
	clone.Find(nestedBlocks).Each(func(_ int, sel *goquery.Selection) {
		node := sel.Get(0)
		if node == nil {
			return
		}
		tag := strings.ToLower(node.Data)
		if (tag == "li" || tag == "td" || tag == "th" || tag == "caption" || tag == "blockquote") && sel.Find(nestedBlocks).Length() > 0 {
			return
		}
		if value := strings.Join(strings.Fields(sel.Text()), " "); value != "" {
			parts = append(parts, value)
		}
	})
	if len(parts) == 0 {
		return strings.Join(strings.Fields(clone.Text()), " ")
	}
	return strings.Join(parts, "\n")
}

func bwcArticleImages(contentSel *goquery.Selection, baseURL string) []string {
	seen := make(map[string]bool)
	var images []string
	contentSel.Find("img[src]").Each(func(_ int, sel *goquery.Selection) {
		fullURL := cleanURL(resolveURL(baseURL, sel.AttrOr("src", "")))
		if fullURL != "" && !seen[fullURL] {
			seen[fullURL] = true
			images = append(images, fullURL)
		}
	})
	return images
}

func bwcArticleAttachments(doc *goquery.Document, contentSel *goquery.Selection, baseURL string) []model.Attachment {
	root := contentSel.Closest(".text-group_1, form").First()
	if root.Length() == 0 {
		root = contentSel.Parent()
	}

	seen := make(map[string]bool)
	var attachments []model.Attachment
	collect := func(scope *goquery.Selection) {
		if scope == nil || scope.Length() == 0 {
			return
		}
		scope.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
			href := strings.TrimSpace(link.AttrOr("href", ""))
			fullURL := cleanURL(resolveURL(baseURL, href))
			if href == "" || fullURL == "" || seen[fullURL] || !bwcIsAttachmentLink(link, href) {
				return
			}
			seen[fullURL] = true
			attachments = append(attachments, model.Attachment{
				Name: bwcAttachmentName(link, fullURL),
				URL:  fullURL,
			})
		})
	}
	collect(contentSel)
	collect(root) // catches BWC's attachment blocks outside .v_news_content
	// Some templates place the accessory block beside the article wrapper.
	for _, selector := range []string{".attachments", ".attachment", ".accessories", ".accessory", ".annex", "[id*='Attach']"} {
		collect(doc.Find(selector))
	}
	return attachments
}

func bwcIsAttachmentLink(link *goquery.Selection, href string) bool {
	if bwcAttachmentRE.MatchString(strings.ToLower(href)) {
		return true
	}
	for _, attr := range []string{"download", "data-download", "data-file", "data-attachment", "data-filename", "data-name"} {
		if value, ok := link.Attr(attr); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	text := strings.ToLower(strings.Join(strings.Fields(link.Text()), " "))
	if strings.Contains(text, "下载") || strings.Contains(text, "download") {
		return true
	}
	for _, node := range append(link.Nodes, link.Parents().Nodes...) {
		marker := strings.ToLower(node.Data + " " + nodeAttr(node, "class") + " " + nodeAttr(node, "id"))
		if strings.Contains(marker, "attachment") || strings.Contains(marker, "accessor") ||
			strings.Contains(marker, "annex") || strings.Contains(marker, "appendix") ||
			strings.Contains(marker, "fujian") || strings.Contains(marker, "附件") ||
			strings.Contains(marker, "download") || strings.Contains(marker, "file") {
			return true
		}
	}
	return false
}

func nodeAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

func bwcAttachmentName(link *goquery.Selection, fullURL string) string {
	for _, attr := range []string{"download", "data-filename", "data-name", "name"} {
		if value := bwcCleanAttachmentName(link.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	if value := bwcCleanAttachmentName(link.Text()); value != "" {
		return value
	}
	if row := link.Closest("tr"); row.Length() > 0 {
		var value string
		row.Find("td, th").EachWithBreak(func(_ int, cell *goquery.Selection) bool {
			candidate := bwcCleanAttachmentName(cell.Text())
			if candidate == "" || candidate == bwcCleanAttachmentName(link.Text()) {
				return true
			}
			value = candidate
			return false
		})
		if value != "" {
			return value
		}
	}
	parsed, err := url.Parse(fullURL)
	if err == nil {
		if value, err := url.PathUnescape(path.Base(parsed.Path)); err == nil && value != "." && value != "/" {
			return value
		}
	}
	return "附件"
}

func bwcCleanAttachmentName(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	for _, generic := range []string{"下载", "点击下载", "下载附件", "download", "click to download", "附件"} {
		if lower == generic {
			return ""
		}
	}
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "附件："), "附件:"))
	return value
}
