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

const jwBaseURL = "https://jw.swjtu.edu.cn"

// jwSubPages contains every article-bearing list exposed by the live
// navigation. The parent pages under jwgk and gzjg are introductions, while
// zlxz is a resource-download index and is deliberately not an article feed.
var jwSubPages = []string{
	"gzdt",
	"gzdt/sjdt",
	"gzdt/xxdt",
	"gzdt/ejjw",
	"ywgz",
	"lzwh",
	"jsjy",
	"tzgg",
	"bszn",
}

var jwArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)

// jwArticleDateTimeRE is checked before the date-only expression. Keeping the
// time mandatory here prevents regexp's leftmost-first matching from
// accepting only the date when the source line contains a precise time.
var jwArticleDateTimeRE = regexp.MustCompile(`20\d{2}\s*(?:年|[-/.])\s*\d{1,2}\s*(?:月|[-/.])\s*\d{1,2}\s*(?:日|号)?\s+\d{1,2}:\d{2}(?::\d{2})?`)

// jwArticleDateRE is intentionally restricted to the source/meta line. The
// article body often contains event dates which are not publication dates.
var jwArticleDateRE = regexp.MustCompile(`20\d{2}\s*(?:年|[-/.])\s*\d{1,2}\s*(?:月|[-/.])\s*\d{1,2}\s*(?:日|号)?`)

func init() {
	registerAdapter("jw", SiteAdapter{
		SubPages:     jwSubPages,
		ParseList:    parseAdapter_jwList,
		ParseArticle: parseAdapter_jwArticle,
		PageURL:      jwPageURL,
	})
}

// jwPageURL maps logical pages (1 = newest) to this site's reverse-numbered
// generated files: slug.htm, slug/(N-1).htm, ..., slug/1.htm.
func jwPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		page = 1
	}
	if page == 1 || totalPages <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	physical := totalPages - page + 1
	if physical < 1 {
		physical = 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

func parseAdapter_jwList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("jw list: parse html: %w", err)
	}
	if jwIsHomepage(doc) {
		return jwParseHomepage(doc, baseURL)
	}
	if !jwIsListPage(doc) {
		return nil, fmt.Errorf("jw list: unsupported or error page")
	}

	root := doc.Find(".main_right .conlistComm").First()
	if root.Length() == 0 {
		return nil, fmt.Errorf("jw list: missing conlistComm")
	}
	typ := jwHeading(doc)
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	root.ChildrenFiltered("dl").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("dt a[href*='info/']").Last()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := jwArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := strings.Join(strings.Fields(anchor.AttrOr("title", "")), " ")
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Text()), " ")
		}
		// The URL is already constrained to jw's article route. Do not apply
		// the shared title blacklist here: legitimate jw articles can start
		// with phrases that other sites use as navigation filters.
		if title == "" {
			return
		}
		date, publishedAt := jwDateFields(row.Text())
		rowType := jwTypeText(row.Find("dt span a").First().Text())
		if rowType == "" {
			rowType = typ
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        rowType,
			ParsedAt:    time.Now(),
		})
	})
	// A recognized conlistComm with no rows is a valid empty list. An
	// unrecognized page was rejected above, so this does not hide WAF/login
	// responses as empty feeds.
	return items, nil
}

func jwIsHomepage(doc *goquery.Document) bool {
	return doc.Find(`meta[name="pageType"]`).AttrOr("content", "") == "1" &&
		doc.Find(".news-bd-slider .bd, .news-bd-list, .column-list").Length() > 0
}

func jwIsListPage(doc *goquery.Document) bool {
	if doc.Find(`meta[name="pageType"]`).AttrOr("content", "") != "2" {
		return false
	}
	return doc.Find(".main_right .conlistComm").Length() > 0 &&
		doc.Find(".main_right .current h3").Length() > 0
}

func jwHeading(doc *goquery.Document) string {
	return strings.Join(strings.Fields(doc.Find(".main_right .current h3").First().Text()), " ")
}

func jwParseHomepage(doc *goquery.Document, baseURL string) ([]model.NewsItem, error) {
	items := make([]model.NewsItem, 0)
	byURL := make(map[string]int)

	add := func(anchor *goquery.Selection, scope *goquery.Selection, typ string) {
		fullURL, ok := jwArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		title := strings.Join(strings.Fields(anchor.AttrOr("title", "")), " ")
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Find("p").First().Text()), " ")
		}
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Text()), " ")
		}
		// This is a scoped article anchor with a strict local /info URL, so
		// retain its complete title even when it resembles a global nav label.
		if title == "" {
			return
		}
		date, publishedAt := jwDateFields(scope.Text())
		if date == "" {
			date = jwShortDate(scope)
		}
		item := model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        strings.TrimSpace(typ),
			ParsedAt:    time.Now(),
		}
		if index, exists := byURL[fullURL]; exists {
			// Homepage modules repeat the same article. Keep the first module's
			// type, but complete fields when a later module exposes them.
			if items[index].Date == "" && item.Date != "" {
				items[index].Date = item.Date
			}
			if items[index].PublishedAt == "" && item.PublishedAt != "" {
				items[index].PublishedAt = item.PublishedAt
			}
			if items[index].Type == "" && item.Type != "" {
				items[index].Type = item.Type
			}
			return
		}
		byURL[fullURL] = len(items)
		items = append(items, item)
	}

	// The carousel is a real article module, not a decorative banner. Its
	// entries are all school-dynamics articles on the live site.
	doc.Find(".news-bd-slider .bd li > a[href]").Each(func(_ int, anchor *goquery.Selection) {
		add(anchor, anchor.Closest("li"), "学校动态")
	})

	// Both tab panels are in the HTML; the second one is only hidden with CSS.
	doc.Find(".news-bd-list .tab-bd").Each(func(i int, panel *goquery.Selection) {
		typ := "工作动态"
		if i == 1 {
			typ = "要闻关注"
		}
		panel.Find("h3 a[href], ul li > a[href]").Each(func(_ int, anchor *goquery.Selection) {
			add(anchor, anchor.Closest("li"), typ)
		})
	})

	// The resource-download module is deliberately excluded. The remaining
	// column modules are article-bearing and are independently represented by
	// their own list pages (or by the homepage index).
	doc.Find(".column-list .column-list-item").Each(func(_ int, block *goquery.Selection) {
		typ := strings.Join(strings.Fields(block.Find(".tit").First().Text()), " ")
		if typ == "" || typ == "资料下载" {
			return
		}
		block.Find("dd > a[href]").Each(func(_ int, anchor *goquery.Selection) {
			add(anchor, anchor.Closest("dd"), typ)
		})
	})

	if len(items) == 0 {
		return nil, fmt.Errorf("jw homepage: no scoped article modules")
	}
	return items, nil
}

func jwShortDate(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(scope.Find("span").First().Text()), " ")
	if match := regexp.MustCompile(`^(\d{1,2})[-/.](\d{1,2})$`).FindStringSubmatch(text); match != nil {
		return fmt.Sprintf("%02s-%02s", match[1], match[2])
	}
	return ""
}

func jwDateFields(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.ReplaceAll(raw, "\u00a0", " ")), " ")
	match := jwArticleDateTimeRE.FindString(raw)
	if match == "" {
		match = jwArticleDateRE.FindString(raw)
	}
	if match == "" {
		return "", ""
	}
	precise := normalizePublishedAt(match)
	if precise == "" {
		return "", ""
	}
	return precise[:10], precise
}

func jwTypeText(raw string) string {
	typ := strings.Join(strings.Fields(raw), " ")
	typ = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(typ, "]"), "["))
	return typ
}

func jwArticleURL(baseURL, href string) (string, bool) {
	resolved := resolveURL(baseURL, strings.TrimSpace(href))
	u, err := url.Parse(resolved)
	if err != nil || u.Host == "" || !jwArticlePathRE.MatchString(u.EscapedPath()) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Host, base.Host) || !strings.EqualFold(u.Scheme, base.Scheme) {
		return "", false
	}
	return cleanURL(resolved), true
}

func parseAdapter_jwArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("jw article: parse html: %w", err)
	}
	if doc.Find(`meta[name="pageType"]`).AttrOr("content", "") != "3" {
		return nil, fmt.Errorf("jw article: unsupported or error page")
	}
	root := doc.Find(".article_content .article").First()
	if root.Length() == 0 {
		return nil, fmt.Errorf("jw article: missing article shell")
	}
	title := strings.Join(strings.Fields(root.Find("h1").First().Text()), " ")
	content := root.Find(".art_con .v_news_content").First()
	if title == "" || content.Length() == 0 {
		return nil, fmt.Errorf("jw article: missing title or content")
	}

	article := &model.Article{Title: title, Type: jwHeading(doc)}
	sourceText := flattenedText(root.Find(".source").First())
	article.Source = jwFieldValue(sourceText, "来源")
	article.Author = jwFieldValue(sourceText, "作者")
	article.Editor = jwFieldValue(sourceText, "编辑")
	if article.Editor == "" {
		article.Editor = jwFieldValue(sourceText, "审核")
	}
	article.Date, article.PublishedAt = jwDateFields(sourceText)
	article.Content = flattenedText(content)

	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		src := strings.TrimSpace(image.AttrOr("src", ""))
		if src == "" {
			return
		}
		resolved := resolveURL(baseURL, src)
		article.Images = appendUniqueString(article.Images, resolved)
		image.SetAttr("src", resolved)
	})

	// jw puts its attachment list after the article body, under
	// .article_content. Only the CMS download endpoint (or an explicit
	// attachment-labelled file link) qualifies; ordinary body links do not.
	seenAttachments := make(map[string]bool)
	doc.Find(".article_content a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if !jwIsAttachmentLink(anchor, href) {
			return
		}
		resolved := resolveURL(baseURL, href)
		if seenAttachments[resolved] {
			return
		}
		name := strings.Join(strings.Fields(anchor.Text()), " ")
		if name == "" {
			name = strings.TrimSpace(anchor.AttrOr("download", ""))
		}
		if name == "" {
			return
		}
		seenAttachments[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		anchor.SetAttr("href", resolved)
	})
	if html, htmlErr := content.Html(); htmlErr == nil {
		article.ContentHTML = html
	}

	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 {
		return nil, fmt.Errorf("jw article: empty content")
	}
	return article, nil
}

func jwFieldValue(text, label string) string {
	if text == "" {
		return ""
	}
	startRE := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(label) + `\s*[:：]\s*`)
	start := startRE.FindStringIndex(text)
	if start == nil {
		return ""
	}
	value := text[start[1]:]
	end := len(value)
	for _, next := range []string{"来源", "作者", "审核", "编辑", "日期", "点击数"} {
		if next == label {
			continue
		}
		nextRE := regexp.MustCompile(`\s+` + regexp.QuoteMeta(next) + `\s*[:：]`)
		if cut := nextRE.FindStringIndex(value); cut != nil && cut[0] < end {
			end = cut[0]
		}
	}
	return strings.TrimSpace(value[:end])
}

func jwIsAttachmentLink(anchor *goquery.Selection, href string) bool {
	if href == "" {
		return false
	}
	if parsed, err := url.Parse(href); err == nil && strings.EqualFold(parsed.Path, "/system/_content/download.jsp") && parsed.Query().Get("urltype") == "news.DownloadAttachUrl" {
		return true
	}
	text := strings.Join(strings.Fields(anchor.Closest("li").Text()), " ")
	if !strings.Contains(text, "附件") {
		return false
	}
	lower := strings.ToLower(href)
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
