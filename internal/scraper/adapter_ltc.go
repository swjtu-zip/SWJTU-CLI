package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const ltcBaseURL = "https://ltc.swjtu.edu.cn"

// ltcSubPages is the set of article-bearing pages found by walking the live
// top-level navigation, its submenu entries, and every homepage module.  The
// generated numbered pages are physical pages of their parent feed and are
// intentionally not registered as separate feeds.
var ltcSubPages = []string{
	"wqdt",
	"wqtz",
	"jcfc",
	"ggw/gzdt",
	"lltd",
	"jkzc",
	"djgz/djdt",
	"djgz/dwgk1",
	"djgz/gzzd",
	"djgz/jwgz",
	"fwzn",
	"fwzn/hdzx",
	"fwzn/lgbfwkbslc",
	"fwzn/txkbslc",
}

var (
	ltcArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	ltcDateTimeRE    = regexp.MustCompile(`(?i)20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?(?:\s+|[Tt])\s*[0-9]{1,2}\s*[:：]\s*[0-9]{2}(?:\s*[:：]\s*[0-9]{2})?`)
	ltcDateRE        = regexp.MustCompile(`(?i)20[0-9]{2}\s*(?:年|[-/.])\s*[0-9]{1,2}\s*(?:月|[-/.])\s*[0-9]{1,2}\s*(?:日|号)?`)
	ltcPageNumberRE  = regexp.MustCompile(`^[0-9]+$`)
	ltcAttachExtRE   = regexp.MustCompile(`(?i)\.(?:docx?|xlsx?|pptx?|pdf|zip|rar|7z|wps|txt|csv)(?:$|[?#&])`)
)

func init() {
	if site := AllSites["ltc"]; site != nil {
		site.BaseURL = ltcBaseURL
	}
	registerAdapter("ltc", SiteAdapter{
		SubPages:     ltcSubPages,
		ParseList:    parseAdapter_ltcList,
		ParseArticle: parseAdapter_ltcArticle,
		PageURL:      ltcPageURL,
		TotalPages:   ltcTotalPages,
	})
}

// ltcPageURL maps the logical page number (1 = newest) to the reverse-numbered
// static files emitted by the site's CMS: slug.htm, slug/(N-1).htm, ...,
// slug/1.htm.
func ltcPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if page < 1 || totalPages < 0 {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if slug == "" || slug == "index" {
		if page != 1 || totalPages > 1 {
			return ""
		}
		return baseURL + "/index" + extension
	}
	if page == 1 {
		return baseURL + "/" + slug + extension
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	physical := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

func ltcKnownFeed(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	for _, known := range ltcSubPages {
		if slug == known {
			return true
		}
	}
	return false
}

func ltcTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		return 1, nil
	}
	if !ltcKnownFeed(slug) {
		return 0, fmt.Errorf("ltc: %q is not a discovered article feed", slug)
	}
	body, err := fetch(ltcPageURL(ltcBaseURL, slug, 1, 0, ".htm"))
	if err != nil {
		return 0, fmt.Errorf("ltc: fetch first page for %s: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("ltc: parse first page for %s: %w", slug, err)
	}
	if err := ltcRejectBlockedPage(doc, "list"); err != nil {
		return 0, err
	}
	if !ltcIsListPage(doc) {
		return 0, fmt.Errorf("ltc: %s is not an article-list shell", slug)
	}
	return ltcPaginationTotal(doc), nil
}

func parseAdapter_ltcList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ltc list: parse html: %w", err)
	}
	if err := ltcRejectBlockedPage(doc, "list"); err != nil {
		return nil, err
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType == "1" {
		return ltcHomepageItems(doc, baseURL)
	}
	if !ltcIsListPage(doc) {
		return nil, fmt.Errorf("ltc list: response is not a discovered article-list shell")
	}

	root := doc.Find(".main_right .conlistComm").First()
	items := make([]model.NewsItem, 0, root.ChildrenFiltered("dl").Length())
	seen := make(map[string]bool)
	localRows := make(map[string]bool)
	missingDate := false
	invalidRow := false
	root.ChildrenFiltered("dl").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("dt a[href*='info/']").Last()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := ltcArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || localRows[fullURL] {
			return
		}
		localRows[fullURL] = true
		title := ltcAnchorTitle(anchor)
		if title == "" {
			invalidRow = true
			return
		}
		date, publishedAt := ltcDateFields(row.Find("dd").First().Text())
		if date == "" {
			date, publishedAt = ltcDateFields(row.Text())
		}
		if date == "" {
			missingDate = true
			invalidRow = true
			return
		}
		itemType := ltcTypeText(row.Find("dt span a").First().Text())
		if itemType == "" {
			itemType = ltcListHeading(doc)
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        itemType,
			ParsedAt:    time.Now(),
		})
	})
	if missingDate {
		return nil, fmt.Errorf("ltc list: local article row has no publication date")
	}
	if invalidRow {
		return nil, fmt.Errorf("ltc list: local article row could not be parsed")
	}
	return items, nil
}

func parseAdapter_ltcArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ltc article: parse html: %w", err)
	}
	if err := ltcRejectBlockedPage(doc, "article"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) != "3" {
		return nil, fmt.Errorf("ltc article: response is not an article page")
	}
	scope := doc.Find(".main_right .article").First()
	if scope.Length() == 0 {
		scope = doc.Find(".article").First()
	}
	if scope.Length() == 0 {
		return nil, fmt.Errorf("ltc article: missing article container")
	}
	title := ltcText(scope.Find("h1").First())
	if title == "" {
		return nil, fmt.Errorf("ltc article: missing detail headline")
	}
	content := ltcArticleContent(scope)
	if content.Length() == 0 || (strings.TrimSpace(content.Text()) == "" && content.Find("img, a, table").Length() == 0) {
		return nil, fmt.Errorf("ltc article: missing or empty body container")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = title
	article.Type = ltcArticleHeading(doc)
	if article.Type == "" {
		return nil, fmt.Errorf("ltc article: missing breadcrumb type")
	}
	if date, publishedAt := ltcDateFields(scope.Find(".source").First().Text()); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	if source := ltcSource(scope.Find(".source").First().Text()); source != "" {
		article.Source = source
	}
	article.Content = ltcArticleText(content)
	article.ContentHTML = ltcContentHTML(content, baseURL)
	article.Images = ltcImages(content, baseURL)
	article.Attachments = ltcAttachments(scope, baseURL)
	return article, nil
}

func ltcIsHomepage(doc *goquery.Document) bool {
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "1" &&
		doc.Find(".iosSlider .slider").Length() > 0 &&
		doc.Find(".second").Length() > 0 &&
		doc.Find(".bgeee").Length() > 0 &&
		doc.Find(".third").Length() > 0
}

func ltcIsListPage(doc *goquery.Document) bool {
	return strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", "")) == "2" &&
		doc.Find(".main_right .current h3").Length() > 0 &&
		doc.Find(".main_right .conlistComm").Length() > 0
}

func ltcRejectBlockedPage(doc *goquery.Document, kind string) error {
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "" {
		return nil
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	text := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
	for _, marker := range []string{"验证码", "安全验证", "访问过于频繁", "access denied", "cloudflare"} {
		if strings.Contains(text, strings.ToLower(marker)) {
			return fmt.Errorf("ltc %s: source returned verification/block page (%s)", kind, marker)
		}
	}
	return nil
}

func ltcHomepageItems(doc *goquery.Document, baseURL string) ([]model.NewsItem, error) {
	if !ltcIsHomepage(doc) {
		return nil, fmt.Errorf("ltc list: pageType=1 response is not the known homepage shell")
	}
	items := make([]model.NewsItem, 0)
	byURL := make(map[string]int)
	add := func(row *goquery.Selection, itemType string) {
		var chosen *goquery.Selection
		var title string
		row.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			if _, ok := ltcArticleURL(baseURL, anchor.AttrOr("href", "")); !ok {
				return
			}
			candidate := ltcAnchorTitle(anchor)
			if candidate == "" {
				candidate = ltcText(row.Find(".text1, .title-link, .type").First())
			}
			if candidate == "" {
				return
			}
			chosen = anchor
			title = candidate
		})
		if chosen == nil || title == "" {
			return
		}
		fullURL, ok := ltcArticleURL(baseURL, chosen.AttrOr("href", ""))
		if !ok {
			return
		}
		date, publishedAt := ltcDateFields(row.Find(".date, em, time").First().Text())
		item := model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        strings.TrimSpace(itemType),
			ParsedAt:    time.Now(),
		}
		if index, exists := byURL[fullURL]; exists {
			if items[index].Date == "" && item.Date != "" {
				items[index].Date = item.Date
				items[index].PublishedAt = item.PublishedAt
			}
			if (strings.HasSuffix(items[index].Title, "...") || strings.HasSuffix(items[index].Title, "…")) && len(item.Title) > len(items[index].Title) {
				items[index].Title = item.Title
			}
			if items[index].Type == "" && item.Type != "" {
				items[index].Type = item.Type
			}
			return
		}
		byURL[fullURL] = len(items)
		items = append(items, item)
	}

	doc.Find(".iosSlider .slider .item").Each(func(_ int, row *goquery.Selection) {
		add(row, "头条新闻")
	})
	doc.Find(".second .l").Each(func(_ int, section *goquery.Selection) {
		typ := ltcText(section.Find(".title .h3").First())
		section.Find(".section, ul.common-list > li").Each(func(_ int, row *goquery.Selection) { add(row, typ) })
	})
	doc.Find(".second .r").Each(func(_ int, section *goquery.Selection) {
		typ := ltcText(section.Find(".title .h3").First())
		section.Find("ul.common-list1 > li").Each(func(_ int, row *goquery.Selection) { add(row, typ) })
	})
	doc.Find(".bgeee .dtbox").Each(func(_ int, section *goquery.Selection) {
		heading := section.Find("h3.title, .title .h3").First().Clone()
		heading.Find("a, span").Remove()
		typ := ltcText(heading)
		section.Find("ul.list > li").Each(func(_ int, row *goquery.Selection) { add(row, typ) })
	})
	doc.Find(".third .visual-slider").Each(func(_ int, section *goquery.Selection) {
		typ := ltcText(section.Find(".title .h3").First())
		if typ == "" {
			typ = "乐龄天地"
		}
		section.Find(".main-image3-list > li").Each(func(_ int, row *goquery.Selection) { add(row, typ) })
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("ltc homepage contains no local article modules")
	}
	return items, nil
}

func ltcAnchorTitle(anchor *goquery.Selection) string {
	if title := ltcTextFromAttr(anchor, "title"); title != "" {
		return title
	}
	return ltcText(anchor)
}

func ltcTextFromAttr(sel *goquery.Selection, name string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(sel.AttrOr(name, ""))), " ")
}

func ltcText(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	return strings.Join(strings.Fields(strings.TrimSpace(sel.Text())), " ")
}

func ltcTypeText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.TrimSpace(strings.Trim(value, "[]【】"))
	return value
}

func ltcListHeading(doc *goquery.Document) string {
	return ltcTypeText(ltcText(doc.Find(".main_right .current h3").First()))
}

func ltcArticleHeading(doc *goquery.Document) string {
	return ltcTypeText(ltcText(doc.Find(".main_right .current h3").First()))
}

func ltcDateFields(value string) (string, string) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "", ""
	}
	if match := ltcDateTimeRE.FindString(value); match != "" {
		publishedAt := normalizePublishedAt(match)
		if len(publishedAt) >= len("2006-01-02") {
			return publishedAt[:10], publishedAt
		}
	}
	if match := ltcDateRE.FindString(value); match != "" {
		date := normalizePublishedAt(match)
		if len(date) >= len("2006-01-02") {
			return date[:10], date
		}
	}
	return "", ""
}

func ltcArticleContent(scope *goquery.Selection) *goquery.Selection {
	content := scope.Find("#vsb_content_501 .v_news_content").First()
	if content.Length() == 0 {
		content = scope.Find(".v_news_content").First()
	}
	return content
}

func ltcArticleText(content *goquery.Selection) string {
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	const blocks = "p,h1,h2,h3,h4,h5,h6,li,td,th,blockquote,pre"
	parts := make([]string, 0)
	clone.Find(blocks).Each(func(_ int, sel *goquery.Selection) {
		if sel.ParentsFiltered(blocks).Length() > 0 {
			return
		}
		if value := ltcText(sel); value != "" {
			parts = append(parts, value)
		}
	})
	if len(parts) == 0 {
		return ltcText(clone)
	}
	return strings.Join(parts, "\n\n")
}

func ltcContentHTML(content *goquery.Selection, baseURL string) string {
	clone := content.Clone()
	clone.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		image.SetAttr("src", resolveURL(baseURL, image.AttrOr("src", "")))
	})
	clone.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if fullURL, ok := ltcSameSiteURL(baseURL, anchor.AttrOr("href", "")); ok && ltcIsAttachmentLink(anchor) {
			anchor.SetAttr("href", fullURL)
		}
	})
	html, err := clone.Html()
	if err != nil {
		return ""
	}
	return html
}

func ltcImages(content *goquery.Selection, baseURL string) []string {
	images := make([]string, 0)
	seen := make(map[string]bool)
	content.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		fullURL := resolveURL(baseURL, image.AttrOr("src", ""))
		if fullURL == "" || seen[fullURL] {
			return
		}
		seen[fullURL] = true
		images = append(images, fullURL)
	})
	return images
}

func ltcAttachments(scope *goquery.Selection, baseURL string) []model.Attachment {
	attachments := make([]model.Attachment, 0)
	seen := make(map[string]bool)
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if !ltcIsAttachmentLink(anchor) {
			return
		}
		fullURL, ok := ltcSameSiteURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		name := ltcTextFromAttr(anchor, "download")
		if name == "" {
			name = ltcTextFromAttr(anchor, "name")
		}
		if name == "" {
			name = ltcText(anchor)
		}
		if name == "" {
			return
		}
		seen[fullURL] = true
		attachments = append(attachments, model.Attachment{Name: name, URL: fullURL})
	})
	return attachments
}

func ltcIsAttachmentLink(anchor *goquery.Selection) bool {
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	value := strings.Join([]string{
		href,
		anchor.AttrOr("download", ""),
		anchor.AttrOr("name", ""),
		anchor.AttrOr("title", ""),
		anchor.Text(),
	}, " ")
	if ltcAttachExtRE.MatchString(value) {
		return true
	}
	lowerHref := strings.ToLower(href)
	return strings.Contains(lowerHref, "download.jsp") && strings.Contains(anchor.Text(), "附件")
}

func ltcArticleURL(baseURL, href string) (string, bool) {
	fullURL, ok := ltcSameSiteURL(baseURL, href)
	if !ok {
		return "", false
	}
	u, err := url.Parse(fullURL)
	if err != nil || !ltcArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	return u.String(), true
}

func ltcSameSiteURL(baseURL, href string) (string, bool) {
	if strings.TrimSpace(href) == "" {
		return "", false
	}
	fullURL := resolveURL(baseURL, strings.TrimSpace(href))
	u, err := url.Parse(fullURL)
	if err != nil || u.Host == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return u.String(), true
}

func ltcSource(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	for _, label := range []string{"来源：", "来源:"} {
		if index := strings.Index(value, label); index >= 0 {
			value = strings.TrimSpace(value[index+len(label):])
			for _, next := range []string{"发布者：", "发布者:", "日期：", "日期:"} {
				if cut := strings.Index(value, next); cut >= 0 {
					value = strings.TrimSpace(value[:cut])
				}
			}
			return value
		}
	}
	return ""
}

func ltcPaginationTotal(doc *goquery.Document) int {
	pages := doc.Find(".main_right .p_pages").First()
	if pages.Length() == 0 {
		pages = doc.Find(".p_pages").First()
	}
	if pages.Length() == 0 {
		return 1
	}
	maxPage := 0
	pages.Find("a, span").Each(func(_ int, sel *goquery.Selection) {
		value := strings.TrimSpace(sel.Text())
		if !ltcPageNumberRE.MatchString(value) {
			return
		}
		page, err := strconv.Atoi(value)
		if err == nil && page > maxPage {
			maxPage = page
		}
	})
	if maxPage == 0 {
		maxPage = 1
	}
	return maxPage
}
