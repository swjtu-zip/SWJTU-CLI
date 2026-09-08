package scraper

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const youthCanonicalBaseURL = "https://youth.swjtu.edu.cn"

// youthSubPages is the set of article-list routes exposed by the site's
// navigation and homepage modules (团情快讯, 通知公告, 团委发文, 基层团建,
// 学生组织, 青年传媒, 团干加油站, 西部计划). Every route renders the same
// dl[id^=line_] list template.
var youthSubPages = []string{
	"tqkx1",
	"tzgg",
	"twfw",
	"jctj",
	"xszz",
	"qncm",
	"tgjyz",
	"xbjh",
}

var youthArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)

func init() {
	registerAdapter("youth", SiteAdapter{
		SubPages:     youthSubPages,
		ParseList:    parseAdapter_youthList,
		ParseArticle: parseAdapter_youthArticle,
		PageURL:      reverseBodaPageURL,
	})
}

func parseAdapter_youthList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("youth: parse list HTML: %w", err)
	}
	if youthBlockedDocument(doc) {
		return nil, fmt.Errorf("youth: response is an error, login, or verification page")
	}
	if youthIsHomepage(doc) {
		items := youthParseHomepage(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("youth: homepage contains no local article modules")
		}
		return items, nil
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", ""))
	if pageType != "2" {
		return nil, fmt.Errorf("youth: response is not an article list page (pageType=%s)", pageType)
	}

	// Each row is <dl id="line_uX_N">: the dt anchor wraps only a thumbnail,
	// while the dd anchor carries .line1 (title) and the dd's em holds the
	// publication date. The digit after u varies per column, so rows are
	// matched by id prefix.
	rows := doc.Find(`dl[id^="line_"]`)
	if rows.Length() == 0 {
		return nil, fmt.Errorf("youth: list page has no article rows")
	}
	typ := youthColumnLabel(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("dd h4 a[href]").First()
		if anchor.Length() == 0 {
			anchor = row.Find("a.news-link[href]").First()
		}
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := youthArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := strings.Join(strings.Fields(row.Find(".line1").First().Text()), " ")
		if title == "" {
			title = strings.Join(strings.Fields(anchor.AttrOr("title", "")), " ")
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := youthDateFields(row.Find("dd em").First().Text())
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
	if len(items) == 0 {
		return nil, fmt.Errorf("youth: list page has no parseable article rows")
	}
	return items, nil
}

func youthIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", "")) == "1" &&
		doc.Find(".container ul.list").Length() > 0
}

// youthParseHomepage extracts the info/ article links from the homepage
// modules (团情快讯, 通知公告, 基层团建, 学生组织, the 团干加油站/青年传媒 and
// 西部计划/研究生支教团 tab boxes). The homepage dates are month/day only, so
// items intentionally carry no date; the detail page remains authoritative.
func youthParseHomepage(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find(".container ul.list li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := youthArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := strings.Join(strings.Fields(row.Find(".line1").First().Text()), " ")
		if title == "" {
			title = strings.Join(strings.Fields(anchor.AttrOr("title", "")), " ")
		}
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Text()), " ")
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Type:     youthHomepageType(row),
			ParsedAt: time.Now(),
		})
	})
	return items
}

// youthHomepageType resolves the module label for a homepage row. Tab boxes
// pair .titleDb tabs with .boxDb/.boxD panels by position, so the row's own
// panel decides which tab label applies.
func youthHomepageType(row *goquery.Selection) string {
	if box := row.Closest(".partc"); box.Length() > 0 {
		tabs := box.Find(".titleDb li")
		panels := box.Find(".boxDb, .boxD")
		panel := row.Closest(".boxDb, .boxD").First()
		for i := 0; i < panels.Length() && i < tabs.Length(); i++ {
			if panel.Get(0) == panels.Eq(i).Get(0) {
				if typ := youthCleanLabel(tabs.Eq(i).Text()); typ != "" {
					return typ
				}
			}
		}
	}
	if box := row.Closest(".page_box"); box.Length() > 0 {
		if typ := youthCleanLabel(box.Find(".title h1").First().Text()); typ != "" {
			return typ
		}
	}
	if part := row.Closest(".part"); part.Length() > 0 {
		return youthCleanLabel(part.Find(".title h1").First().Text())
	}
	return ""
}

func youthCleanLabel(value string) string {
	return strings.Trim(strings.Join(strings.Fields(value), " "), "[]【】")
}

// youthColumnLabel reads the fixed column heading shared by list and article
// shells (通知公告, 团情快讯, ...). The meta pageTitle is only consulted on
// list pages; on article pages it repeats the article title.
func youthColumnLabel(doc *goquery.Document) string {
	for _, selector := range []string{".main_right .current > h3", ".current > h3", ".menu > h3"} {
		if label := youthCleanLabel(doc.Find(selector).First().Text()); label != "" {
			return label
		}
	}
	if strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", "")) == "2" {
		return youthCleanLabel(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).First().AttrOr("content", ""))
	}
	return ""
}

func youthArticleURL(baseURL, href string) (string, bool) {
	resolved := cleanURL(resolveURL(baseURL, strings.TrimSpace(html.UnescapeString(href))))
	u, err := url.Parse(resolved)
	if err != nil || u.Host == "" || !youthArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Scheme, base.Scheme) || !strings.EqualFold(u.Host, base.Host) {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func youthDateFields(value string) (string, string) {
	value = strings.Join(strings.Fields(html.UnescapeString(value)), " ")
	if match := dateTimeRe.FindStringSubmatch(value); match != nil {
		published := normalizePublishedAt(match[0])
		if len(published) >= 10 {
			if publishedAtHasClock(published) {
				return published[:10], published
			}
			return published[:10], published[:10]
		}
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func parseAdapter_youthArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("youth: parse article HTML: %w", err)
	}
	if youthBlockedDocument(doc) {
		return nil, fmt.Errorf("youth: response is an error, login, or verification page")
	}
	if strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).First().AttrOr("content", "")) != "3" {
		return nil, fmt.Errorf("youth: response is not an article page")
	}
	title := strings.Join(strings.Fields(doc.Find(".article h1").First().Text()), " ")
	body := doc.Find(".article .art_con .v_news_content").First()
	if body.Length() == 0 {
		body = doc.Find(".v_news_content").First()
	}
	if title == "" || body.Length() == 0 {
		return nil, fmt.Errorf("youth: article shell is missing heading or content")
	}
	if strings.TrimSpace(extractBodaContentText(body)) == "" && body.Find("img, table").Length() == 0 {
		return nil, fmt.Errorf("youth: article content is empty")
	}

	meta := strings.Join(strings.Fields(html.UnescapeString(doc.Find(".article .source").First().Text())), " ")
	article := &model.Article{
		Title:  title,
		Source: youthMetaValue(meta, "来源", "作者", "日期", "点击数"),
		Author: youthMetaValue(meta, "作者", "日期", "点击数"),
		Type:   youthColumnLabel(doc),
	}
	if index := strings.Index(meta, "日期"); index >= 0 {
		article.Date, article.PublishedAt = youthDateFields(meta[index+len("日期"):])
	}
	youthFinalizeContent(article, body, baseURL)
	return article, nil
}

// youthMetaValue extracts one labelled field from the .source line, stopping
// at the next known label so values never absorb trailing counters.
func youthMetaValue(meta, label string, stops ...string) string {
	index := strings.Index(meta, label)
	if index < 0 {
		return ""
	}
	value := meta[index+len(label):]
	if colon := strings.IndexAny(value, "：:"); colon >= 0 {
		if value[colon] == ':' {
			value = value[colon+1:]
		} else {
			value = value[colon+len("："):]
		}
	}
	cut := len(value)
	for _, stop := range stops {
		if i := strings.Index(value, stop); i >= 0 && i < cut {
			cut = i
		}
	}
	return strings.TrimSpace(value[:cut])
}

// youthFinalizeContent fills Content, ContentHTML, Images, and Attachments
// from the article body, resolving every resource URL against the site.
func youthFinalizeContent(article *model.Article, body *goquery.Selection, baseURL string) {
	if article == nil || body == nil || body.Length() == 0 {
		return
	}
	article.Content = extractBodaContentText(body)
	content := body.Clone()
	content.Find("script, style, noscript").Remove()
	seenImages := make(map[string]bool)
	seenAttachments := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := bodaResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" {
				resolved = candidate
				break
			}
		}
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		if !seenImages[resolved] {
			seenImages[resolved] = true
			article.Images = append(article.Images, resolved)
		}
	})
	content.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(html.UnescapeString(link.AttrOr("href", "")))
		if bodaLooksLikeAttachmentLink(link, href) {
			resolved := bodaResourceURL(baseURL, href)
			if resolved == "" {
				link.Remove()
				return
			}
			if !seenAttachments[resolved] {
				seenAttachments[resolved] = true
				article.Attachments = append(article.Attachments, model.Attachment{
					Name: youthAttachmentName(link, resolved),
					URL:  resolved,
				})
			}
			link.SetAttr("href", resolved)
			return
		}
		if href == "" || bodaIsPseudoResourceHref(href) {
			link.RemoveAttr("href")
			return
		}
		link.SetAttr("href", cleanURL(resolveURL(baseURL, href)))
	})
	if contentHTML, err := content.Html(); err == nil {
		article.ContentHTML = contentHTML
	}
}

func youthAttachmentName(link *goquery.Selection, href string) string {
	for _, value := range []string{
		link.AttrOr("download", ""),
		link.AttrOr("title", ""),
		link.Text(),
	} {
		if value = strings.Trim(strings.Join(strings.Fields(html.UnescapeString(value)), " "), "【】[]"); value != "" {
			return value
		}
	}
	if parsed, err := url.Parse(href); err == nil {
		if name := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]; name != "" {
			return html.UnescapeString(name)
		}
	}
	return href
}

// youthBlockedDocument distinguishes a real source page from the small error,
// login, and verification shells the CMS also returns as HTML.
func youthBlockedDocument(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{
		"404", "403", "500", "access denied", "forbidden", "安全验证", "请输入验证码",
		"just a moment", "cloudflare", "页面不存在", "系统提示",
	} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	return false
}
