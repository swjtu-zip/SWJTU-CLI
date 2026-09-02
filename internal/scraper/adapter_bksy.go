package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// bksySubPages contains the actual article-list entry points exposed by the
// site's notice tabs and navigation. The site has two parallel trees: the
// notice archive under /tzgg and the full navigation archive under the
// subject-specific directories. Empty one-page sections are kept here too so
// callers can distinguish a reachable empty section from an omitted section.
var bksySubPages = []string{
	"tzgg/qb",
	"tzgg/jxyx",
	"tzgg/jxjs",
	"tzgg/jsfz",
	"tzgg/tsjy",
	"tzgg/sjjx",
	"tzgg/syjx",
	"tzgg/cxcy",
	"tzgg/zljs",
	"tzgg/glgd",

	"jgsz/glgzzd",
	"jgsz/xgwjbg",
	"jxyx/xjxg",
	"jxyx/cjxg",
	"jxyx/pkxg",
	"jxyx/xkxg",
	"jxyx/ksxg",
	"jxjs/jxgg",
	"jxjs/zyjs",
	"jxjs/kcsj",
	"jxjs/jcjs",
	"jsfz/jcjxzz",
	"jsfz/jsjxzz",
	"jsfz/jsjxjs",
	"jsfz/jxnlts",
	"syjx/syjx",
	"syjx/sysjs",
	"syjx/sfzx",
	"syjx/xfzx",
	"syjx/syjs",
	"syjx/gxhsy",
	"sjjx/sxsj",
	"sjjx/bysj_lw_",
	"sjjx/xkjs",
	"sjjx/kcxl",
	"sjjx/cjrh",
	"cxcy/cxcyhd",
	"cxcy/cxcykc",
	"cxcy/cxcyxm",
	"cxcy/cxcypt",
	"zljj/zlbz",
	"zljj/kcpg",
	"zljj/zyrz",
	"zljj/shpg",
	"zljj/ztsj",
}

var bksyArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)

var bksyAttachmentExts = []string{
	".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
	".pdf", ".zip", ".rar", ".7z", ".txt", ".csv",
}

func init() {
	registerAdapter("bksy", SiteAdapter{
		SubPages:     bksySubPages,
		ParseList:    parseAdapter_bksyList,
		ParseArticle: parseAdapter_bksyArticle,
		PageURL:      bksyAdapterPageURL,
	})
}

// bksyAdapterPageURL maps the logical page number (1 = newest) to the
// reverse-numbered static file used by bksy.swjtu.edu.cn. Page 1 is the
// unsuffixed page; for a 243-page list, logical page 2 is /242.htm and the
// last page is /1.htm. If the total is unknown, use the forward file number
// instead of producing an invalid zero/negative path.
func bksyAdapterPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}

	fileNumber := page
	if totalPages > 1 {
		fileNumber = totalPages - page + 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, fileNumber, extension)
}

// parseAdapter_bksyList only accepts the two real list shapes on the site:
// #content-1 on the homepage and ul.right-list in a list-container page. A
// document-wide info/ scan would pull in banners, sidebars, navigation and
// direct file links, so a missing list shape is reported as an error rather
// than being treated as a successful empty list.
func parseAdapter_bksyList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse bksy list: %w", err)
	}

	var listItems *goquery.Selection
	var section string
	if homepage := doc.Find("#content-1").First(); homepage.Length() > 0 {
		list := homepage.Find("ul").First()
		if list.Length() == 0 {
			return nil, fmt.Errorf("bksy: homepage has no notice list")
		}
		listItems = list.ChildrenFiltered("li")
		section = bksyHomepageSection(doc)
	} else {
		list := doc.Find("main.list-container ul.right-list, .list-container ul.right-list, ul.right-list").First()
		if list.Length() == 0 {
			return nil, fmt.Errorf("bksy: response is not a homepage or list page")
		}
		listItems = list.ChildrenFiltered("li")
		section = bksyListSection(doc)
	}

	items := make([]model.NewsItem, 0, listItems.Length())
	seen := make(map[string]bool, listItems.Length())
	listItems.Each(func(_ int, item *goquery.Selection) {
		addBksyAdapterItem(&items, seen, item, baseURL, section)
	})
	return items, nil
}

func bksyHomepageSection(doc *goquery.Document) string {
	if tab := doc.Find(`.tabNav li[data-content="content-1"]`).First(); tab.Length() > 0 {
		return bksyCompactText(tab.Text())
	}
	return "全部"
}

func bksyListSection(doc *goquery.Document) string {
	for _, selector := range []string{
		"main.list-container .content-nav",
		".list-container .content-nav",
		".content-nav",
	} {
		if heading := doc.Find(selector).First(); heading.Length() > 0 {
			if value := bksyCompactText(heading.Text()); value != "" {
				return value
			}
		}
	}
	return ""
}

func addBksyAdapterItem(items *[]model.NewsItem, seen map[string]bool, item *goquery.Selection, baseURL, section string) {
	if item == nil || item.Length() == 0 || isInNavArea(item) {
		return
	}

	anchor := item
	if _, ok := item.Attr("href"); !ok {
		anchor = item.Find("a[href]").First()
	}
	if anchor.Length() == 0 {
		return
	}

	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL, ok := bksyInternalArticleURL(baseURL, href)
	if !ok || seen[fullURL] {
		return
	}

	title := ""
	for _, selector := range []string{".content", ".line1.news", ".line1"} {
		if candidate := anchor.Find(selector).First(); candidate.Length() > 0 {
			title = bksyCompactText(candidate.Text())
			if title != "" {
				break
			}
		}
	}
	if title == "" {
		title = bksyCompactText(anchor.AttrOr("title", ""))
	}
	if title == "" {
		clone := anchor.Clone()
		clone.Find("time, .time, [class*='date'], [class*='Date']").Remove()
		title = bksyCompactText(clone.Text())
	}
	title = cleanTitle(title)
	if title == "" || utf8.RuneCountInString(title) > 100 || !isValidNewsTitle(title) || isBksyNavigationTitle(title) {
		return
	}

	date := bksyListDate(anchor)
	seen[fullURL] = true
	newsItem := model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     date,
		Type:     section,
		ParsedAt: time.Now(),
	}
	if precise := normalizePublishedAt(date); precise != "" {
		newsItem.PublishedAt = precise
	}
	*items = append(*items, newsItem)
}

func bksyListDate(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	for _, selector := range []string{"time", ".time", "[class*='date']", "[class*='Date']"} {
		if date := bksyCompactText(anchor.Find(selector).First().Text()); date != "" {
			if len(date) <= 32 && strings.Count(date, " ") <= 4 {
				return date
			}
		}
	}
	return ""
}

// bksyInternalArticleURL deliberately rejects direct downloads and external
// SPA routes in a list. Attachments in an actual bksy article are handled
// separately by appendBksyAttachments and may legitimately be cross-host.
func bksyInternalArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !bksyArticlePathRE.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	return fullURL, true
}

func parseAdapter_bksyArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse bksy article: %w", err)
	}
	content := bksyContentSelection(doc)
	if doc.Find(".news-title").First().Length() == 0 || content.Length() == 0 {
		return nil, fmt.Errorf("bksy: response is not an article page")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	if title := bksyCompactText(doc.Find(".news-title").First().Text()); title != "" {
		article.Title = title
	}
	if value := bksyMetaValue(doc.Find(".tips .middle1").First()); value != "" {
		article.Type = value
	}
	if value := bksyMetaValue(doc.Find(".tips .middle2").First()); value != "" {
		article.Source = value
	}
	if rawDate := bksyCompactText(doc.Find(".tips .left-details").First().Text()); rawDate != "" {
		if precise := normalizePublishedAt(rawDate); precise != "" {
			article.Date = precise[:10]
			article.PublishedAt = precise
		}
	}

	if strings.TrimSpace(article.Content) == "" {
		clone := content.Clone()
		clone.Find("script, style, noscript").Remove()
		article.Content = bksyCompactText(clone.Text())
	}
	if len(article.Images) == 0 {
		content.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
			if src := strings.TrimSpace(image.AttrOr("src", "")); src != "" {
				article.Images = append(article.Images, resolveURL(baseURL, src))
			}
		})
	}
	appendBksyAttachments(article, doc, content, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func bksyContentSelection(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{
		".article-content .v_news_content",
		".v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return doc.Find(".never-a-bksy-content").First()
}

func bksyMetaValue(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	value := bksyCompactText(sel.Text())
	for _, label := range []string{"分类：", "分类:", "来源：", "来源:"} {
		if strings.HasPrefix(value, label) {
			return strings.TrimSpace(strings.TrimPrefix(value, label))
		}
	}
	return value
}

func appendBksyAttachments(article *model.Article, doc *goquery.Document, content *goquery.Selection, baseURL string) {
	if article == nil || doc == nil {
		return
	}

	seen := make(map[string]int, len(article.Attachments))
	for i, attachment := range article.Attachments {
		seen[attachment.URL] = i
	}

	add := func(anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		name := bksyAttachmentName(anchor)
		if name == "" {
			return
		}
		if index, ok := seen[resolved]; ok {
			if !bksyLooksLikeFilename(article.Attachments[index].Name) && bksyLooksLikeFilename(name) {
				article.Attachments[index].Name = name
			}
			return
		}
		seen[resolved] = len(article.Attachments)
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	}

	if content != nil && content.Length() > 0 {
		content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			if bksyLooksLikeAttachment(anchor) {
				add(anchor)
			}
		})
	}

	// Current bksy pages render the attachment list after .v_news_content,
	// still inside .left-news-details. Scope by the containing li's label so
	// ordinary article/sidebar links are never promoted to attachments.
	doc.Find(".left-news-details li a[href]").Each(func(_ int, anchor *goquery.Selection) {
		li := anchor.Closest("li")
		if li.Length() == 0 || !strings.Contains(bksyCompactText(li.Text()), "附件") {
			return
		}
		add(anchor)
	})
}

func bksyLooksLikeAttachment(anchor *goquery.Selection) bool {
	if anchor == nil || anchor.Length() == 0 {
		return false
	}
	href := strings.ToLower(strings.TrimSpace(anchor.AttrOr("href", "")))
	for _, value := range []string{
		anchor.AttrOr("download", ""),
		anchor.AttrOr("name", ""),
		anchor.AttrOr("title", ""),
		anchor.Text(),
	} {
		if bksyLooksLikeFilename(value) {
			return true
		}
	}
	return strings.Contains(href, "download") || strings.Contains(href, "attachment")
}

func bksyAttachmentName(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"download", "name", "title"} {
		value := bksyCompactText(anchor.AttrOr(attr, ""))
		if bksyLooksLikeFilename(value) {
			return value
		}
	}
	if value := bksyCompactText(anchor.Text()); value != "" {
		return value
	}
	for _, attr := range []string{"download", "name", "title"} {
		if value := bksyCompactText(anchor.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func bksyLooksLikeFilename(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, ext := range bksyAttachmentExts {
		if strings.Contains(value, ext) {
			return true
		}
	}
	return false
}

func bksyCompactText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
