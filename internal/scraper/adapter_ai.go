package scraper

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// ai.swjtu.edu.cn is an older Visual SiteBuilder site, but it does not use
// the list markup handled safely by the generic 博达 parser.  In particular,
// its archive rows are <dl> elements, its home page has four independent
// article blocks, and generated page numbers run backwards after page one.
func init() {
	registerAdapter("ai", SiteAdapter{
		SubPages:     aiSubPages,
		ParseList:    parseAdapter_aiList,
		ParseArticle: parseAdapter_aiArticle,
		PageURL:      aiPageURL,
	})
}

var (
	aiArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	aiPageTotals    = map[string]int{
		"tzgg": 2,
		"xwdt": 4,
		"xsbg": 2,
		"kxyj": 2,
		"jcsj": 1,
	}
	aiSubPages = []string{"tzgg", "xwdt", "xsbg", "kxyj", "jcsj"}
)

var aiAttachmentExtensions = []string{
	".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf",
	".zip", ".rar", ".7z", ".txt", ".csv",
}

// aiPageURL maps the logical page number to the site's generated file.  The
// live site exposes the actual totals in the page-number labels. Keeping the
// audited totals here supplies a safe fallback when a caller has no usable
// total-page hint; a valid runtime total is still honored.
func aiPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}

	if totalPages <= 1 {
		if knownTotal, ok := aiPageTotals[slug]; ok {
			totalPages = knownTotal
		}
	}
	if totalPages <= 1 {
		return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, page, extension)
	}

	filePage := totalPages - page + 1
	if filePage < 1 {
		// A request beyond the audited archive must not silently repeat the last
		// page and create duplicate news.
		filePage = 0
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func parseAdapter_aiList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ai: parse list HTML: %w", err)
	}
	if aiLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("ai: received an error or verification page instead of a news list")
	}

	// Category pages use .conlistComm > dl.  This is deliberately tried first
	// so a future page shell containing a stray home-style block cannot widen
	// extraction to arbitrary document links.
	items := parseAIArchiveRows(doc, baseURL)
	if len(items) == 0 {
		// index.htm has four visible article modules. The dedicated bottom
		// marquee is added by parseAIHomeRows; the top promotional carousel and
		// navigation are not feed entries.
		items = parseAIHomeRows(doc, baseURL)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("ai: page contains no recognized article list")
	}
	return items, nil
}

func parseAIArchiveRows(doc *goquery.Document, baseURL string) []model.NewsItem {
	rows := doc.Find(".conlistComm dl")
	if rows.Length() == 0 {
		return nil
	}

	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	pageType := aiPageType(doc)
	rows.Each(func(_ int, row *goquery.Selection) {
		var anchor *goquery.Selection
		row.Find("dt a[href]").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
			if href := candidate.AttrOr("href", ""); href != "" {
				if _, ok := aiArticleURL(baseURL, href); ok {
					anchor = candidate
					return false
				}
			}
			return true
		})
		if anchor == nil || anchor.Length() == 0 {
			return
		}

		fullURL, ok := aiArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := aiAnchorTitle(anchor)
		if !aiIsArticleTitle(title) {
			return
		}
		date, publishedAt := aiDateFields(row.Find("dd").First().Text())
		if date == "" {
			date, publishedAt = aiDateFields(row.Find("time, .time, .date").First().Text())
		}
		typ := aiTypeLabel(row.Find("dt span a[href]").First().Text())
		if typ == "" {
			typ = pageType
		}
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
	return items
}

func parseAIHomeRows(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("div.fl-box, div.fr-box").Each(func(_ int, section *goquery.Selection) {
		title := section.Find(".title h3").First().Clone()
		if title.Length() == 0 {
			return
		}
		title.Find("a").Remove()
		typ := aiTypeLabel(title.Text())
		if typ == "" {
			return
		}

		section.Find(".list li").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			fullURL, ok := aiArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok || seen[fullURL] {
				return
			}
			articleTitle := aiAnchorTitle(anchor)
			if !aiIsArticleTitle(articleTitle) {
				return
			}
			date, publishedAt := aiDateFields(row.Find(".time, time, .date").First().Text())
			seen[fullURL] = true
			items = append(items, model.NewsItem{
				Title:       articleTitle,
				URL:         fullURL,
				Date:        date,
				PublishedAt: publishedAt,
				Type:        typ,
				ParsedAt:    time.Now(),
			})
		})
	})

	// The bottom marquee is the homepage's "精彩瞬间" module.  Its cards are
	// real internal articles (info/1111/...), while the top .flexslider only
	// contains external promotional links and must not enter the feed.
	doc.Find(".jd-background #u_u7_demo1 td").Each(func(_ int, card *goquery.Selection) {
		anchor := card.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := aiArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := aiAnchorTitle(anchor)
		if title == "" {
			title = aiCleanText(card.Find(".titlestyle244104").First().Text())
		}
		if !aiIsArticleTitle(title) {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Type:     "精彩瞬间",
			ParsedAt: time.Now(),
		})
	})
	return items
}

func parseAdapter_aiArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("ai: parse article HTML: %w", err)
	}
	if aiLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("ai: received an error or verification page instead of an article")
	}

	root := doc.Find(".content .article").First()
	if root.Length() == 0 {
		root = doc.Find(".article").First()
	}
	titleSel := root.Find("h1").First()
	contentSel := aiArticleContentSelection(root)
	if root.Length() == 0 || titleSel.Length() == 0 || contentSel.Length() == 0 {
		return nil, fmt.Errorf("ai: article shell or body is missing")
	}

	article, err := parseBodaArticle(raw, baseURL)
	if err != nil {
		return nil, err
	}
	article.Title = aiCleanText(titleSel.Text())

	// AI uses .source rather than .news_source.  Use the scoped source block
	// first, so body text such as “来源：...” cannot become article metadata.
	if sourceSel := root.Find(".source").First(); sourceSel.Length() > 0 {
		sourceText := flattenedText(sourceSel)
		if source := aiMetaValue(sourceText, "来源"); source != "" {
			article.Source = source
		}
		if rawDate := aiMetaValue(sourceText, "日期"); rawDate != "" {
			if date, publishedAt := aiDateFields(rawDate); date != "" {
				article.Date = date
				article.PublishedAt = publishedAt
			}
		}
	}
	if typ := aiArticleType(doc); typ != "" {
		article.Type = typ
	}

	aiFinalizeArticleResources(article, contentSel, baseURL)
	if strings.TrimSpace(article.Content) == "" {
		if bodyText := aiBodyText(contentSel); bodyText != "" {
			article.Content = bodyText
		}
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("ai: article body is empty")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func aiArticleContentSelection(scope *goquery.Selection) *goquery.Selection {
	if scope == nil || scope.Length() == 0 {
		return nil
	}
	for _, selector := range []string{
		".v_news_content",
		"#vsb_content",
		"[id^='vsb_content']",
	} {
		if found := scope.Find(selector).First(); found.Length() > 0 {
			if selector != ".v_news_content" {
				if nested := found.Find(".v_news_content").First(); nested.Length() > 0 {
					return nested
				}
			}
			return found
		}
	}
	return nil
}

func aiFinalizeArticleResources(article *model.Article, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}

	images := make([]string, 0)
	seenImages := make(map[string]bool)
	content.Find("img[src]").Each(func(_ int, image *goquery.Selection) {
		src := strings.TrimSpace(image.AttrOr("src", ""))
		if src == "" {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, src))
		if resolved == "" || seenImages[resolved] {
			return
		}
		seenImages[resolved] = true
		images = append(images, resolved)
		image.SetAttr("src", resolved)
	})
	article.Images = images

	seenAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seenAttachments[attachment.URL] = true
	}
	content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if href == "" || !aiIsAttachmentLink(anchor, href) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if resolved == "" {
			return
		}
		anchor.SetAttr("href", resolved)
		if seenAttachments[resolved] {
			return
		}
		seenAttachments[resolved] = true
		attachment := model.Attachment{
			Name: aiAttachmentName(anchor, href),
			URL:  resolved,
			Size: strings.TrimSpace(anchor.AttrOr("data-size", "")),
		}
		if attachment.Name == "" {
			return
		}
		article.Attachments = append(article.Attachments, attachment)
	})
	if htmlText, err := content.Html(); err == nil {
		article.ContentHTML = htmlText
	}
}

func aiIsAttachmentLink(anchor *goquery.Selection, href string) bool {
	lowerHref := strings.ToLower(href)
	if strings.Contains(lowerHref, "downloadattachurl") ||
		strings.Contains(lowerHref, "download.jsp") ||
		strings.Contains(lowerHref, "/download/") {
		return true
	}
	for _, attr := range []string{"download", "data-download", "data-filename", "data-name"} {
		if strings.TrimSpace(anchor.AttrOr(attr, "")) != "" {
			return true
		}
	}
	for _, value := range []string{
		href,
		anchor.AttrOr("name", ""),
		anchor.AttrOr("title", ""),
		anchor.Text(),
	} {
		if aiHasAttachmentExtension(value) {
			return true
		}
	}
	return false
}

func aiHasAttachmentExtension(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, extension := range aiAttachmentExtensions {
		if strings.Contains(lower, extension) {
			return true
		}
	}
	return false
}

func aiAttachmentName(anchor *goquery.Selection, href string) string {
	for _, attr := range []string{"download", "data-filename", "data-name", "name", "title"} {
		if value := aiCleanText(anchor.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	if value := aiCleanText(anchor.Text()); value != "" {
		return value
	}
	if parsed, err := url.Parse(href); err == nil {
		if value := aiCleanText(parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]); value != "" {
			return value
		}
	}
	return ""
}

func aiArticleURL(baseURL, href string) (string, bool) {
	href = strings.TrimSpace(htmlstd.UnescapeString(href))
	if href == "" {
		return "", false
	}
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !aiArticlePathRe.MatchString(parsed.Path) || !isValidNewsURL(fullURL) {
		return "", false
	}
	return fullURL, true
}

func aiAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if title := aiCleanText(anchor.AttrOr("title", "")); title != "" {
		return title
	}
	if alt := aiCleanText(anchor.Find("img[alt]").First().AttrOr("alt", "")); alt != "" {
		return alt
	}
	return aiCleanText(anchor.Text())
}

func aiIsArticleTitle(title string) bool {
	return title != "" && utf8.RuneCountInString(title) <= 200 && isValidNewsTitle(title)
}

func aiDateFields(raw string) (string, string) {
	if normalized := normalizePublishedAt(raw); len(normalized) >= 10 {
		if len(normalized) > 10 {
			return normalized[:10], normalized
		}
		return normalized, ""
	}
	return "", ""
}

func aiPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".main_left .menu h3", ".main_right .current h3", ".current h3"} {
		if typ := aiTypeLabel(doc.Find(selector).First().Text()); typ != "" {
			return typ
		}
	}
	return aiTypeLabel(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
}

func aiArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	// The current breadcrumb is the strongest signal on detail pages because
	// the same article may be linked from more than one home-page module.
	typ := ""
	doc.Find(".current a[href]").Each(func(_ int, anchor *goquery.Selection) {
		value := aiTypeLabel(anchor.Text())
		if value != "" && value != "首页" {
			typ = value
		}
	})
	if typ != "" {
		return typ
	}
	return aiPageType(doc)
}

func aiTypeLabel(value string) string {
	value = aiCleanText(value)
	value = strings.TrimSpace(strings.Trim(value, "[]【】"))
	if value == "" || value == "首页" || value == "正文" ||
		value == "西南交通大学人工智能研究院" || strings.EqualFold(value, "more +") {
		return ""
	}
	return value
}

func aiMetaValue(text, label string) string {
	text = htmlstd.UnescapeString(text)
	text = strings.Join(strings.Fields(text), " ")
	start := strings.Index(text, label+"：")
	separatorLength := len(label) + len("：")
	if start < 0 {
		start = strings.Index(text, label+":")
		separatorLength = len(label) + 1
	}
	if start < 0 {
		return ""
	}
	rest := text[start+separatorLength:]
	end := len(rest)
	for _, nextLabel := range []string{"来源：", "来源:", "发布者：", "发布者:", "日期：", "日期:", "点击数：", "点击数:"} {
		if index := strings.Index(rest, nextLabel); index >= 0 && index < end {
			end = index
		}
	}
	return strings.TrimSpace(rest[:end])
}

func aiBodyText(content *goquery.Selection) string {
	if content == nil || content.Length() == 0 {
		return ""
	}
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	return strings.Join(strings.Fields(htmlstd.UnescapeString(clone.Text())), " ")
}

func aiCleanText(value string) string {
	value = htmlstd.UnescapeString(value)
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func aiLooksLikeErrorPage(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(aiCleanText(doc.Find("title").First().Text()))
	for _, marker := range []string{"404", "403", "error", "错误", "不存在", "验证码", "安全验证", "附件下载"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	return false
}
