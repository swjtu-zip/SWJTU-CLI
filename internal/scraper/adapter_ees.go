package scraper

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// eesSubPages contains the real article-list entry points exposed by the
// current navigation. Profile/contact pages and the two hidden "more" links
// that currently render the homepage shell are intentionally omitted.
// Resource-like pages are retained when they are CMS article lists: their
// rows have real /info/... detail pages (for example 资料下载 and 常用表格).
var eesSubPages = []string{
	"xwdt1/xwdt",
	"xwdt1/mtbd",
	"djgz/djtz",
	"djgz/djdt",
	"tzgg/qb",
	"tzgg/zs",
	"tzgg/py",
	"tzgg/sj",
	"tzgg/xg",
	"tzgg/zh",
	"zsxx/zsdt",
	"zsxx/zlxz",
	"rcpy/kcjx",
	"rcpy/gcal",
	"rcpy/rqsj",
	"rcpy/xwgz",
	"xmpt/xmjs",
	"xmpt/xqpt",
	"xmpt/gjjlpt",
	"szdw/xnds",
	"szdw/qyds",
	"xsgz/xgtz",
	"xsgz/xshd",
	"gfwj/zcwj",
	"gfwj/gzzd",
	"gfwj/cybg",
}

var (
	eesArticlePathRe = regexp.MustCompile(`(?i)^/info/\d+/\d+\.html?$`)
	eesDateRe        = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
	eesSplitDateRe   = regexp.MustCompile(`(\d{1,2})\s*[-/.]\s*(\d{1,2})\s*(20\d{2})`)
	eesAttachmentRe  = regexp.MustCompile(`(?i)\.(?:docx?|xlsx?|pptx?|pdf|zip|rar|7z|txt|csv)(?:$|[?#])`)
	eesEmbeddedPDFRe = regexp.MustCompile(`(?i)showVsbpdfIframe\(\s*["']([^"']+\.pdf)["']`)
)

// EES is a reverse-numbered Visual SiteBuilder site, so use the existing
// site-specific URL mapper from ees.go in addition to the custom parsers.
func init() {
	registerAdapter("ees", SiteAdapter{
		SubPages:     eesSubPages,
		ParseList:    parseAdapter_eesList,
		ParseArticle: parseAdapter_eesArticle,
		PageURL:      eesPageURL,
	})
}

// eesPageURL maps logical pages (1 = newest) to EES's reverse-numbered static
// files. A five-page list therefore uses the unsuffixed URL for page 1 and
// /4.htm, /3.htm, /2.htm, /1.htm for pages 2 through 5.
func eesPageURL(baseURL, slug string, page, totalPages int, extension string) string {
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

func parseAdapter_eesList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse ees list: %w", err)
	}

	if eesIsHomepage(doc) {
		items := eesHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("ees homepage has no article modules")
		}
		return items, nil
	}

	list := eesListContainer(doc)
	if list == nil || list.Length() == 0 {
		return nil, fmt.Errorf("ees response is not a homepage or article list")
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	pageType := eesListType(doc)
	list.ChildrenFiltered("li.list-item").Each(func(_ int, row *goquery.Selection) {
		anchor, articleURL := eesRowArticle(row, baseURL)
		if anchor == nil || articleURL == "" {
			return
		}

		title := eesRowTitle(row, anchor)
		if !eesValidTitle(title) {
			return
		}
		date, publishedAt := eesDateValues(row.Find(".left-time, time, .date, .time").First().Text())
		item := model.NewsItem{
			Title:       title,
			URL:         articleURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        pageType,
			ParsedAt:    time.Now(),
		}
		eesAppendItem(&items, seen, item)
	})

	// A present news-list with no list-item rows is a valid empty result. It
	// must not be confused with a static page, which has no list container.
	return items, nil
}

func parseAdapter_eesArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse ees article: %w", err)
	}
	if !eesHasArticleBody(doc) {
		return nil, fmt.Errorf("ees response is not an article")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse ees article: %w", err)
	}

	if title := eesText(doc.Find(".text-heading").First()); title != "" {
		article.Title = title
	}
	source, rawDate := eesArticleMeta(doc)
	if source != "" {
		article.Source = source
	}
	eesSetArticleDate(article, rawDate)
	if typ := eesArticleType(doc, article.Source); typ != "" {
		article.Type = typ
	}

	eesFinalizeContent(article, doc, baseURL)
	eesAppendAttachments(article, doc, baseURL)
	if !eesValidTitle(article.Title) {
		return nil, fmt.Errorf("ees article has no title")
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("ees article has no body or file resource")
	}
	article.RawHTML = string(raw)
	return article, nil
}

func eesIsHomepage(doc *goquery.Document) bool {
	return doc.Find("body.home-page, .home-school-news, .home-news-classification, .home-fczy").Length() > 0
}

func eesHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	if scope := doc.Find(".home-school-news").First(); scope.Length() > 0 {
		eesCollectHomeRows(scope.Find(".mid-section > li, .bottom-section li"), "新闻动态", baseURL, &items, seen)
	}

	if scope := doc.Find(".home-news-classification").First(); scope.Length() > 0 {
		scope.Find("ul.content > li").Each(func(_ int, row *goquery.Selection) {
			anchor, articleURL := eesRowArticle(row, baseURL)
			if anchor == nil || articleURL == "" {
				return
			}
			title := eesText(row.Find(".news, .title, .desc-title, .msg-desc").First())
			if title == "" {
				title = eesAnchorTitle(anchor)
			}
			date, publishedAt := eesDateValues(row.Find("time, .time, .date").First().Text())
			typ := eesText(row.Find(".little-title").First())
			if typ == "" {
				typ = "通知公告"
			}
			eesAppendItem(&items, seen, model.NewsItem{
				Title:       title,
				URL:         articleURL,
				Date:        date,
				PublishedAt: publishedAt,
				Type:        typ,
				ParsedAt:    time.Now(),
			})
		})
	}

	// This module is display:none in the source but contains two real CMS
	// article panels. The empty 技术中心 panel and its more link are ignored.
	if scope := doc.Find(".home-fczy").First(); scope.Length() > 0 {
		scope.Find("ul.main-box").Each(func(_ int, panel *goquery.Selection) {
			typ := eesHomePanelType(scope, panel.AttrOr("id", ""))
			panel.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
				anchor, articleURL := eesRowArticle(row, baseURL)
				if anchor == nil || articleURL == "" {
					return
				}
				title := eesText(row.Find(".right-text .title, .title, .desc-title, .msg-desc").First())
				if title == "" {
					title = eesAnchorTitle(anchor)
				}
				eesAppendItem(&items, seen, model.NewsItem{
					Title:    title,
					URL:      articleURL,
					Type:     typ,
					ParsedAt: time.Now(),
				})
			})
		})
	}

	return items
}

func eesCollectHomeRows(rows *goquery.Selection, typ, baseURL string, items *[]model.NewsItem, seen map[string]int) {
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor, articleURL := eesRowArticle(row, baseURL)
		if anchor == nil || articleURL == "" {
			return
		}
		title := eesText(row.Find(".desc-title, .msg-desc, .news, .right-text .title, .title").First())
		if title == "" {
			title = eesAnchorTitle(anchor)
		}
		date, publishedAt := eesDateValues(row.Find(".time, .time_box, time, .date").First().Text())
		eesAppendItem(items, seen, model.NewsItem{
			Title:       title,
			URL:         articleURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        typ,
			ParsedAt:    time.Now(),
		})
	})
}

func eesHomePanelType(scope *goquery.Selection, id string) string {
	if id == "fczj-content-1" {
		return "校企导师"
	}
	if id == "fczj-content-2" {
		return "卓工学生"
	}
	var typ string
	scope.Find(".fczj-tabNav li[data-content]").EachWithBreak(func(_ int, tab *goquery.Selection) bool {
		if tab.AttrOr("data-content", "") == id {
			typ = eesText(tab)
			return false
		}
		return true
	})
	return typ
}

func eesListContainer(doc *goquery.Document) *goquery.Selection {
	if list := doc.Find(".right-main-content > ul.news-list").First(); list.Length() > 0 {
		return list
	}
	var result *goquery.Selection
	doc.Find("ul.news-list").EachWithBreak(func(_ int, list *goquery.Selection) bool {
		if list.ChildrenFiltered("li.list-item").Length() > 0 {
			result = list
			return false
		}
		if result == nil {
			result = list
		}
		return true
	})
	return result
}

func eesRowArticle(row *goquery.Selection, baseURL string) (*goquery.Selection, string) {
	var found *goquery.Selection
	var articleURL string
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		resolved, ok := eesArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return true
		}
		found = anchor
		articleURL = resolved
		return false
	})
	return found, articleURL
}

func eesRowTitle(row, anchor *goquery.Selection) string {
	title := eesText(row.Find(".right-text-desc.line1, .right-text-desc .line1, .title, .desc-title, .msg-desc, .news").First())
	if title == "" {
		title = eesAnchorTitle(anchor)
	}
	return cleanTitle(htmlstd.UnescapeString(title))
}

func eesAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if title := eesTextValue(anchor.AttrOr("title", "")); title != "" {
		return cleanTitle(htmlstd.UnescapeString(title))
	}
	return cleanTitle(htmlstd.UnescapeString(eesText(anchor)))
}

func eesListType(doc *goquery.Document) string {
	if typ := eesText(doc.Find(".selectionbar-right-title").First()); typ != "" {
		return typ
	}
	if typ := eesTextValue(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")); typ != "" {
		return typ
	}
	title := eesText(doc.Find("title").First())
	if index := strings.IndexAny(title, "-—"); index > 0 {
		title = title[:index]
	}
	return strings.TrimSpace(title)
}

func eesArticleURL(baseURL, href string) (string, bool) {
	href = htmlstd.UnescapeString(strings.TrimSpace(href))
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, ok := eesInternalURL(baseURL, fullURL)
	if !ok || !eesArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func eesInternalURL(baseURL, rawURL string) (*url.URL, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return nil, false
	}
	return parsed, true
}

func eesValidTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 200 {
		return false
	}
	switch title {
	case "首页", "更多", "查看更多", "正文", "登录", "注册", "联系我们",
		"学院概况", "党建工作", "招生信息", "人才培养", "项目平台", "师资队伍", "学生工作", "规范文件":
		return false
	}
	return true
}

func eesDateValues(raw string) (string, string) {
	raw = htmlstd.UnescapeString(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
	if raw == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(raw); normalized != "" {
		if len(normalized) > 10 && strings.Contains(normalized, ":") {
			return normalized[:10], normalized
		}
		return normalized[:10], ""
	}
	if match := eesDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	if match := eesSplitDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[3], match[1], match[2]), ""
	}
	return "", ""
}

func eesArticleMeta(doc *goquery.Document) (string, string) {
	tips := doc.Find(".left-text .tips, .tips").First()
	var source, rawDate string
	tips.Find("span").Each(func(_ int, field *goquery.Selection) {
		value := eesText(field)
		switch {
		case strings.HasPrefix(value, "来源："):
			source = strings.TrimSpace(strings.TrimPrefix(value, "来源："))
		case strings.HasPrefix(value, "来源:"):
			source = strings.TrimSpace(strings.TrimPrefix(value, "来源:"))
		case strings.HasPrefix(value, "日期："):
			rawDate = strings.TrimSpace(strings.TrimPrefix(value, "日期："))
		case strings.HasPrefix(value, "日期:"):
			rawDate = strings.TrimSpace(strings.TrimPrefix(value, "日期:"))
		}
	})
	return source, rawDate
}

func eesArticleType(doc *goquery.Document, source string) string {
	var typ string
	doc.Find(".right-link a").Each(func(_ int, link *goquery.Selection) {
		value := eesText(link)
		if value != "" && value != "首页" && value != "正文" {
			typ = value
		}
	})
	if typ != "" {
		return typ
	}
	if typ = eesText(doc.Find(".second-title span").First()); typ != "" {
		return typ
	}
	return strings.TrimSpace(source)
}

func eesHasArticleBody(doc *goquery.Document) bool {
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return false
	}
	if eesIsVerificationPage(doc) {
		return false
	}
	return doc.Find(".text-heading").Length() > 0 && eesArticleContent(doc).Length() > 0
}

func eesIsVerificationPage(doc *goquery.Document) bool {
	title := eesText(doc.Find("title").First())
	body := eesText(doc.Find("body").First())
	return title == "附件下载" || strings.Contains(body, "请输入验证码下载附件") || strings.Contains(body, "验证码下载附件")
}

func eesArticleContent(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		".left-text .text-body .v_news_content",
		".text-body .v_news_content",
		".v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return doc.Find("__ees_missing_content__")
}

func eesFinalizeContent(article *model.Article, doc *goquery.Document, baseURL string) {
	content := eesArticleContent(doc)
	if content.Length() == 0 {
		return
	}
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()

	parts := make([]string, 0)
	clone.Find("p, h1, h2, h3, h4, h5, h6").Each(func(_ int, part *goquery.Selection) {
		if text := eesText(part); text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) == 0 {
		if text := eesText(clone); text != "" {
			parts = append(parts, text)
		}
	}
	article.Content = strings.Join(parts, "\n\n")

	images := make([]string, 0)
	seenImages := make(map[string]bool)
	clone.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		resolved := cleanURL(resolveURL(baseURL, image.AttrOr("src", "")))
		if resolved == "" || seenImages[resolved] {
			return
		}
		seenImages[resolved] = true
		images = append(images, resolved)
		image.SetAttr("src", resolved)
	})
	article.Images = images
	clone.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if href := strings.TrimSpace(link.AttrOr("href", "")); href != "" {
			link.SetAttr("href", cleanURL(resolveURL(baseURL, href)))
		}
	})
	if htmlText, err := clone.Html(); err == nil {
		article.ContentHTML = htmlText
	}
}

func eesAppendAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}

	doc.Find(".left-text a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if !eesIsAttachmentLink(link, href) {
			return
		}
		resolved, ok := eesInternalURL(baseURL, cleanURL(resolveURL(baseURL, href)))
		if !ok {
			return
		}
		fullURL := resolved.String()
		if seen[fullURL] {
			return
		}
		name := eesAttachmentName(link, href)
		if name == "" {
			return
		}
		seen[fullURL] = true
		article.Attachments = append(article.Attachments, model.Attachment{
			Name: name,
			URL:  fullURL,
			Size: strings.TrimSpace(link.AttrOr("data-size", "")),
		})
	})

	// Some EES resource details render a PDF through JavaScript instead of an
	// anchor. Keep that real file URL as an attachment so a caller can verify
	// it with an ordinary GET; the script itself is removed from ContentHTML.
	content := eesArticleContent(doc)
	content.Find("script").Each(func(_ int, script *goquery.Selection) {
		for _, match := range eesEmbeddedPDFRe.FindAllStringSubmatch(script.Text(), -1) {
			if len(match) < 2 {
				continue
			}
			resolved, ok := eesInternalURL(baseURL, cleanURL(resolveURL(baseURL, match[1])))
			if !ok || seen[resolved.String()] {
				continue
			}
			seen[resolved.String()] = true
			article.Attachments = append(article.Attachments, model.Attachment{
				Name: "正文内嵌PDF",
				URL:  resolved.String(),
			})
		}
	})
}

func eesIsAttachmentLink(link *goquery.Selection, href string) bool {
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	name := strings.Join([]string{link.AttrOr("download", ""), link.AttrOr("name", ""), link.AttrOr("title", ""), link.Text(), href}, " ")
	if !eesAttachmentRe.MatchString(name) {
		return false
	}
	marker := link.Closest("li")
	return marker.Length() > 0 && strings.Contains(eesText(marker), "附件")
}

func eesAttachmentName(link *goquery.Selection, href string) string {
	for _, value := range []string{
		link.AttrOr("download", ""),
		link.AttrOr("data-filename", ""),
		link.AttrOr("name", ""),
		link.AttrOr("title", ""),
		link.Text(),
	} {
		if value = eesTextValue(value); value != "" {
			return strings.Trim(value, "【】[]")
		}
	}
	if parsed, err := url.Parse(href); err == nil {
		if name := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]; name != "" {
			return name
		}
	}
	return ""
}

func eesSetArticleDate(article *model.Article, rawDate string) {
	for _, candidate := range []string{rawDate, article.PublishedAt, article.Date} {
		if date, publishedAt := eesDateValues(candidate); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
			return
		}
	}
	article.Date = ""
	article.PublishedAt = ""
}

func eesAppendItem(items *[]model.NewsItem, seen map[string]int, item model.NewsItem) {
	if index, ok := seen[item.URL]; ok {
		current := &(*items)[index]
		if current.Title == "" && item.Title != "" {
			current.Title = item.Title
		}
		if current.Date == "" && item.Date != "" {
			current.Date = item.Date
			current.PublishedAt = item.PublishedAt
		}
		if current.Type == "" && item.Type != "" {
			current.Type = item.Type
		}
		return
	}
	seen[item.URL] = len(*items)
	*items = append(*items, item)
}

func eesText(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	return eesTextValue(sel.Text())
}

func eesTextValue(value string) string {
	return strings.Join(strings.Fields(htmlstd.UnescapeString(strings.TrimSpace(value))), " ")
}
