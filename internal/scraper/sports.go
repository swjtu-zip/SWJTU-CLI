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

var sportsArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)

var sportsUnavailableTextRE = regexp.MustCompile(`(?i)just\s+a\s+moment|cloudflare|attention\s+required|access\s+denied|request\s+blocked|checking\s+your\s+browser|cf-chl|captcha|验证码|安全验证|访问过于频繁|页面不存在|404\s+not\s+found|service\s+unavailable|bad\s+gateway`)

var sportsPseudoAttachmentRE = regexp.MustCompile(`(?i)captcha|verify|validation|cloudflare|challenge|waf|access[-_ ]?denied|error\.html?|404\.html?|login`)

var sportsExcludedListTitles = map[string]bool{
	"学院简介":    true,
	"现任领导":    true,
	"组织架构":    true,
	"联系我们":    true,
	"教师名录":    true,
	"课程体系":    true,
	"专业简介":    true,
	"学术动态":    true,
	"科研成果":    true,
	"学工简介":    true,
	"社团简介":    true,
	"视觉交大":    true,
	"体育之星":    true,
	"体育慕课":    true,
	"高水平代表队":  true,
	"阳光体育代表队": true,
	"文件资料":    true,
}

// parseSportsNews parses either the actual homepage modules or one of the
// archive pages.  The archive parser is intentionally scoped to the right
// .news-list: the left "最新推荐" list and the navigation contain article
// links too, but they are not rows of the selected feed.
func parseSportsNews(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := parseSportsDocument(raw)
	if err != nil {
		return nil, err
	}
	if sportsResponseLooksUnavailable(doc) {
		return nil, fmt.Errorf("sports: list response is an error, login, or verification page")
	}
	if sportsIsHomepage(doc) {
		items := sportsHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("sports: homepage contains no local article modules")
		}
		return items, nil
	}

	list := sportsNewsList(doc)
	if list.Length() == 0 || sportsIsExcludedListPage(doc) {
		return nil, fmt.Errorf("sports: response is not a supported article list")
	}
	items, rows := sportsArchiveItems(list, sportsListType(doc), baseURL)
	if len(items) == 0 && rows > 0 {
		return nil, fmt.Errorf("sports: article list contains no local article rows")
	}
	return items, nil
}

func parseSportsDocument(raw []byte) (*goquery.Document, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("sports: parse HTML: %w", err)
	}
	return doc, nil
}

func sportsIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	pageTitle := sportsPageTitle(doc)
	if pageType == "1" || pageTitle == "学院首页" {
		return true
	}
	// xysy.htm is the same rendered homepage as /, but its generated metadata
	// says pageType=2.  These two home-only modules distinguish it from an
	// archive page without relying on the URL that the caller used.
	return doc.Find(".rowa .innews").Length() > 0 && doc.Find(".rowb .tabNav").Length() > 0 && doc.Find(".main-box.news_list_page").Length() == 0
}

func sportsNewsList(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	if list := doc.Find(".main_right .news-list").First(); list.Length() > 0 {
		return list
	}
	// Keep compatibility with a small number of archived snapshots that omit
	// the main_right class, while still requiring the list class itself.
	return doc.Find("ul.news-list").First()
}

func sportsListType(doc *goquery.Document) string {
	if title := sportsPageTitle(doc); title != "" && title != "学院首页" {
		return title
	}
	if title := strings.TrimSpace(doc.Find(".main_right > .title").First().Text()); title != "" {
		return strings.Join(strings.Fields(title), " ")
	}
	return ""
}

func sportsPageTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	return strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).Last().AttrOr("content", "")), " ")
}

func sportsIsExcludedListPage(doc *goquery.Document) bool {
	return sportsExcludedListTitles[sportsListType(doc)]
}

func sportsHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	appendRow := func(row *goquery.Selection, typ string) {
		if item, ok := sportsNewsItemFromRow(row, typ, baseURL); ok && !seen[item.URL] {
			seen[item.URL] = true
			items = append(items, item)
		}
	}

	doc.Find(".rowa .innews").First().ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
		appendRow(row, "新闻动态")
	})

	labels := doc.Find(".rowb .tabNav > li")
	doc.Find(".rowb .tabCon .tabcons").Each(func(index int, tab *goquery.Selection) {
		typ := "通知公告"
		if index < labels.Length() {
			typ = sportsColumnName(labels.Eq(index).Text())
		}
		// Only the rendered tab content belongs to this tab.  In particular,
		// do not walk the surrounding rowb, which would duplicate every tab.
		tab.Find("ul").First().ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
			appendRow(row, typ)
		})
	})

	doc.Find(".rowc .sjmt > ul").First().ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
		appendRow(row, "运动校园")
	})
	return items
}

func sportsColumnName(value string) string {
	return strings.Join(strings.Fields(value), "")
}

func sportsArchiveItems(list *goquery.Selection, typ, baseURL string) ([]model.NewsItem, int) {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	rows := 0
	list.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
		if row.Find("a[href]").Length() == 0 {
			return
		}
		rows++
		if item, ok := sportsNewsItemFromRow(row, typ, baseURL); ok && !seen[item.URL] {
			seen[item.URL] = true
			items = append(items, item)
		}
	})
	return items, rows
}

func sportsNewsItemFromRow(row *goquery.Selection, typ, baseURL string) (model.NewsItem, bool) {
	if row == nil || row.Length() == 0 || isInNavArea(row) {
		return model.NewsItem{}, false
	}

	var anchor *goquery.Selection
	row.Find("a[href]").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
		fullURL := cleanURL(resolveURL(baseURL, candidate.AttrOr("href", "")))
		if sportsIsLocalArticleURL(fullURL, baseURL) {
			anchor = candidate
			return false
		}
		return true
	})
	if anchor == nil {
		return model.NewsItem{}, false
	}

	title := sportsRowText(row, []string{".line1", ".line2", "p.line2", ".desc", ".text-title"})
	if title == "" {
		title = strings.TrimSpace(anchor.AttrOr("title", ""))
	}
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(title)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 120 {
		return model.NewsItem{}, false
	}

	dateText := sportsRowText(row, []string{".time", ".time-box", "time", ".date"})
	if dateText == "" {
		row.Find("span").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
			value := strings.TrimSpace(candidate.Text())
			if normalizePublishedAt(value) != "" {
				dateText = value
				return false
			}
			return true
		})
	}
	publishedAt := normalizePublishedAt(dateText)
	date := ""
	if len(publishedAt) >= len("2006-01-02") {
		date = publishedAt[:len("2006-01-02")]
	}

	return model.NewsItem{
		Title: title, URL: cleanURL(resolveURL(baseURL, anchor.AttrOr("href", ""))),
		Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now(),
	}, true
}

func sportsRowText(row *goquery.Selection, selectors []string) string {
	for _, selector := range selectors {
		if value := strings.Join(strings.Fields(row.Find(selector).First().Text()), " "); value != "" {
			return value
		}
	}
	return ""
}

func sportsIsLocalArticleURL(href, baseURL string) bool {
	if !isValidSiteNewsURL(href, baseURL) {
		return false
	}
	parsed, err := url.Parse(href)
	return err == nil && sportsArticlePathRE.MatchString(parsed.Path)
}

func sportsResponseLooksUnavailable(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	if sportsIsHomepage(doc) || sportsHasArticleShell(doc) {
		return false
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	text := strings.Join(strings.Fields(clone.Text()), " ")
	if sportsUnavailableTextRE.MatchString(text) {
		return true
	}
	if sportsNewsList(doc).Length() > 0 {
		return false
	}
	// A client-rendered shell with no server-rendered list/article is not a
	// successful empty feed.  It needs an API adapter rather than guessing.
	if doc.Find("#app, #root, #__next, [data-reactroot]").Length() > 0 && len([]rune(text)) < 1000 {
		return true
	}
	return false
}

func sportsHasArticleShell(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(".right_box .right-title").Length() > 0 && sportsArticleContent(doc).Length() > 0
}

func sportsArticleContent(doc *goquery.Document) *goquery.Selection {
	if doc == nil {
		return &goquery.Selection{}
	}
	for _, selector := range []string{
		".right_box .article .v_news_content",
		".right_box .v_news_content",
		".main_right .article .v_news_content",
		".article .v_news_content",
		".v_news_content",
	} {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return &goquery.Selection{}
}

func parseSportsArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := parseSportsDocument(raw)
	if err != nil {
		return nil, err
	}
	if sportsResponseLooksUnavailable(doc) || !sportsHasArticleShell(doc) {
		return nil, fmt.Errorf("sports: response is not a supported article page")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	if title := sportsArticleTitle(doc); title != "" {
		article.Title = title
	}
	article.Type = sportsArticleType(doc, article.Title)
	article.Source, article.Date, article.PublishedAt = sportsArticleFixedMeta(doc)
	sportsNormalizeArticleResources(article, doc, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func sportsArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".right_box .right-title", ".main_right .right-title", ".right-title"} {
		if title := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); title != "" {
			return title
		}
	}
	return strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).Last().AttrOr("content", "")), " ")
}

func sportsArticleType(doc *goquery.Document, title string) string {
	if doc == nil {
		return ""
	}
	if crumb := doc.Find(".opsi").First(); crumb.Length() > 0 {
		typ := ""
		crumb.Find("a").Each(func(_ int, anchor *goquery.Selection) {
			if candidate := bodaTypeCandidate(strings.Join(strings.Fields(anchor.Text()), " "), title); candidate != "" {
				typ = candidate
			}
		})
		if typ != "" {
			return typ
		}
	}
	for _, selector := range []string{".right_box > .title", ".right_box .section-title", ".main_right > .title"} {
		if candidate := bodaTypeCandidate(strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "), title); candidate != "" {
			return candidate
		}
	}
	return ""
}

func sportsArticleFixedMeta(doc *goquery.Document) (source, date, publishedAt string) {
	meta := doc.Find(".right_box .lm-desc").First()
	if meta.Length() == 0 {
		meta = doc.Find(".lm-desc").First()
	}
	if meta.Length() == 0 {
		return "", "", ""
	}
	meta.Find(".part").Each(func(_ int, part *goquery.Selection) {
		text := strings.Join(strings.Fields(part.Text()), " ")
		switch {
		case strings.HasPrefix(text, "来源"):
			source = sportsMetaValue(text, "来源")
		case strings.HasPrefix(text, "日期") || strings.HasPrefix(text, "发布日期") || strings.HasPrefix(text, "发布时间"):
			rawDate := text
			for _, label := range []string{"发布日期", "发布时间", "日期"} {
				if strings.HasPrefix(rawDate, label) {
					rawDate = sportsMetaValue(rawDate, label)
					break
				}
			}
			if normalized := normalizePublishedAt(rawDate); normalized != "" {
				publishedAt = normalized
				date = normalized[:len("2006-01-02")]
			}
		}
	})
	return source, date, publishedAt
}

func sportsMetaValue(text, label string) string {
	value := strings.TrimSpace(strings.TrimPrefix(text, label))
	value = strings.TrimLeft(value, " ：:：\t")
	return strings.TrimSpace(value)
}

func sportsNormalizeArticleResources(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	content := sportsArticleContent(doc)
	if content.Length() == 0 {
		return
	}
	content.Find("script, style, noscript").Remove()

	article.Images = nil
	seenImages := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if candidate := bodaResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" && !sportsPseudoAttachmentRE.MatchString(strings.ToLower(candidate)) {
				resolved = candidate
				break
			}
		}
		if resolved == "" {
			image.Remove()
			return
		}
		if !seenImages[resolved] {
			seenImages[resolved] = true
			article.Images = append(article.Images, resolved)
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vsbhref", "vurl"} {
			image.RemoveAttr(attr)
		}
	})

	article.Attachments = nil
	seenAttachments := make(map[string]bool)
	appendAttachment := func(anchor *goquery.Selection) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		name := strings.Join(strings.Fields(anchor.Text()), " ")
		if !sportsAttachmentAnchor(anchor, href) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		allowed := sportsAttachmentURLAllowed(resolved, name)
		if !allowed {
			anchor.Remove()
			return
		}
		if seenAttachments[resolved] {
			anchor.SetAttr("href", resolved)
			return
		}
		if name == "" {
			name = resolved
		}
		seenAttachments[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		anchor.SetAttr("href", resolved)
	}
	content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		appendAttachment(anchor)
	})

	// 体育学院 places the attachment UL after .article, still inside
	// .right_box.  Restrict the second pass to that detail wrapper so sidebar
	// and global navigation links cannot become attachments.
	scope := doc.Find(".right_box").First()
	if scope.Length() == 0 {
		scope = doc.Find(".main_right").First()
	}
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if anchor.Closest(".v_news_content").Length() > 0 {
			return
		}
		appendAttachment(anchor)
	})

	article.Content = extractBodaContentText(content)
	if html, err := content.Html(); err == nil {
		article.ContentHTML = html
	}
}

func sportsAttachmentAnchor(anchor *goquery.Selection, href string) bool {
	if anchor == nil || anchor.Length() == 0 || href == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	if bodaLooksLikeAttachmentLink(anchor, href) {
		return true
	}
	// A paragraph can contain several unrelated links (for example, an
	// external article link next to an attachment).  Only use the surrounding
	// prose as evidence when it belongs to a dedicated attachment container or
	// to a one-link row; otherwise one sibling's word “附件” would classify all
	// links in the paragraph as files.
	for _, selector := range []string{".attachment", ".attachments", ".news-attachment", ".download", "li"} {
		if container := anchor.Closest(selector); container.Length() > 0 {
			text := container.Text()
			if strings.Contains(text, "附件") || strings.Contains(text, "下载") {
				return true
			}
		}
	}
	parent := anchor.Parent()
	if parent.Length() > 0 && parent.Find("a[href]").Length() == 1 {
		text := parent.Text()
		return strings.Contains(text, "附件") || strings.Contains(text, "下载")
	}
	return false
}

func sportsAttachmentURLAllowed(href, name string) bool {
	if href == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	parsed, err := url.Parse(href)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return false
	}
	lower := strings.ToLower(href + " " + name)
	if sportsPseudoAttachmentRE.MatchString(lower) {
		return false
	}
	path := strings.ToLower(parsed.Path)
	if strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".htm") {
		return false
	}
	if strings.Contains(lower, "download.jsp") || strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, ext := range bodaAttachmentExts {
		if strings.Contains(path, ext) || strings.Contains(strings.ToLower(parsed.RawQuery), ext) || strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), ext) {
			return true
		}
	}
	return false
}
