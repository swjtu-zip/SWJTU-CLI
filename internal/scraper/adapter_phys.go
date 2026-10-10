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

var physSubPages = []string{
	"xwgg/xydt",
	"kxyj/xsdt",
	"rcpy/bkspy",
	"rcpy/bkspy/bkstz",
	"rcpy/bkspy/bkspyfa",
	"rcpy/bkspy/bksxz",
	"rcpy/yjspy",
	"rcpy/yjspy/yjstz",
	"rcpy/yjspy/yjspyfa",
	"xsfz/gqtgz",
	"xsfz/xgdt",
	"xsfz/xgtz",
	"xygz/xyjy",
	"dqgz/jjgz",
	"dqgz/pyfz",
	"dqgz/xxjy",
	"dqgz/ghzjlink/ghhd",
}

const physCanonicalBaseURL = "https://phys.swjtu.edu.cn"

// physAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	// The registry retains the historical HTTP address. The live site is
	// served canonically over HTTPS; keep this correction local to the phys
	// adapter rather than changing the shared registry.
	if site := AllSites["phys"]; site != nil {
		site.BaseURL = physCanonicalBaseURL
	}
	registerAdapter("phys", SiteAdapter{
		SubPages:     physSubPages,
		ParseList:    parseAdapter_physList,
		ParseArticle: parseAdapter_physArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   physTotalPages,
	})
}

func parseAdapter_physList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("phys: parse list HTML: %w", err)
	}
	if physLooksUnavailable(doc) {
		return nil, fmt.Errorf("phys: list response is an error, login, or verification page")
	}
	if physIsHomepage(doc) {
		items := physHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("phys: homepage contains no local article modules")
		}
		return items, nil
	}

	// The live archive uses .newlist1. Once that container is present, an empty
	// result is a valid empty tab; no generic fallback is allowed because it can
	// turn another tab, a static page, or a partial response into this feed.
	if doc.Find(".newlist1").Length() > 0 {
		items := parsePhysInfoRows(doc, baseURL)
		physAnnotateItems(doc, items, baseURL, physPageType(doc))
		return items, nil
	}
	// Keep the older list shell used by the unit test and by a few archived
	// copies, but still scope extraction to that shell.
	if doc.Find(".list_zx").Length() > 0 {
		items := parsePhysInfoRows(doc, baseURL)
		physAnnotateItems(doc, items, baseURL, physPageType(doc))
		return items, nil
	}
	return nil, fmt.Errorf("phys: response is not a supported article list")
}

func physTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if !physKnownFeed(slug) {
		return 0, fmt.Errorf("phys: %q is not an article-list route", slug)
	}
	listURL := fmt.Sprintf("%s/%s.htm", physCanonicalBaseURL, slug)
	body, err := fetch(listURL)
	if err != nil {
		listURL = fmt.Sprintf("%s/%s.html", physCanonicalBaseURL, slug)
		body, err = fetch(listURL)
		if err != nil {
			return 0, fmt.Errorf("phys: fetch first page for %s: %w", slug, err)
		}
	}
	return physTotalPagesFromHTML(body, slug)
}

// physKnownFeed reports the audited article-list routes. The homepage is an
// audited feed too: it renders the .home1/.home3/.home4 news modules that
// physHomepageItems scopes extraction to, and it has no pagination.
func physKnownFeed(slug string) bool {
	if slug == "index" {
		return true
	}
	for _, known := range physSubPages {
		if slug == known {
			return true
		}
	}
	return false
}

func physTotalPagesFromHTML(raw []byte, slug string) (int, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("phys: parse pagination for %s: %w", slug, err)
	}
	if strings.Trim(strings.TrimSpace(slug), "/") == "index" {
		// The homepage feed is a single scoped page: it either is the audited
		// homepage or the response must be rejected, never silently counted.
		if !physIsHomepage(doc) {
			return 0, fmt.Errorf("phys: %s response is not the homepage", slug)
		}
		if physLooksUnavailable(doc) {
			return 0, fmt.Errorf("phys: %s response is an error, login, or verification page", slug)
		}
		return 1, nil
	}
	if physIsHomepage(doc) || doc.Find(".newlist1").Length() == 0 {
		return 0, fmt.Errorf("phys: %s response is not a list page", slug)
	}
	if physLooksUnavailable(doc) {
		return 0, fmt.Errorf("phys: %s response is an error, login, or verification page", slug)
	}
	if total := parseTotalPages(raw); total > 0 {
		return total, nil
	}
	// A recognized archive with no pager is a one-page feed, not an unknown
	// page count.  Any page after it is rejected by reverseBodaPageURL.
	return 1, nil
}

func parseAdapter_physArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("phys: parse article HTML: %w", err)
	}
	if physLooksUnavailable(doc) || !physHasArticleShell(doc) {
		return nil, fmt.Errorf("phys: response is not a supported article page")
	}
	article, err := parseBodaArticleWithSectionType(html, baseURL)
	if err != nil {
		return nil, err
	}
	if title := physArticleTitle(doc); title != "" {
		article.Title = title
	}
	// The shared parser has broad compatibility fallbacks. For phys, the
	// source's fixed publication node is authoritative; body dates and the
	// migrated template date are never promoted to article metadata.
	article.Date, article.PublishedAt = physArticleDate(doc)
	if typ := physArticleType(doc); typ != "" {
		article.Type = typ
	}
	physFilterArticleResources(article, doc, baseURL)
	return article, nil
}

var physShortDateRE = regexp.MustCompile(`(?:^|\D)(\d{1,2})\s*[-/.]\s*(\d{1,2})(?:\D|$)`)

const physLegacyPlaceholderDate = "2017-04-24"

func parsePhysInfoRows(doc *goquery.Document, baseURL string) []model.NewsItem {
	if doc == nil {
		return nil
	}
	root := doc.Selection
	if list := doc.Find(".newlist1").First(); list.Length() > 0 {
		root = list
	} else if list := doc.Find(".list_zx").First(); list.Length() > 0 {
		root = list
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	root.Find("a[href*='info/']").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if fullURL == "" || seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) {
			return
		}
		row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
		if row.Length() == 0 {
			row = anchor.Parent()
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(row.Find(".xw-ul-tt, .line1, .txts, .news-title, h3, h4, p").First().Text())
		}
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := physDateValues(row)
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         fullURL,
			Date:        date,
			PublishedAt: publishedAt,
			ParsedAt:    time.Now(),
		})
	})
	return items
}

func physIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).FilterFunction(func(_ int, sel *goquery.Selection) bool {
		return strings.TrimSpace(sel.AttrOr("content", "")) == "1"
	}).Length() > 0 && doc.Find(".home1, .home3, .home4").Length() > 0
}

func physHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	doc.Find(".home1 a[href*='info/'], .home3 a[href*='info/'], .home4 a[href*='info/']").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if fullURL == "" || !isValidSiteNewsURL(fullURL, baseURL) {
			return
		}
		row := anchor.Closest("li, dt, dd, .item, .news-item, .list-item").First()
		if row.Length() == 0 {
			row = anchor
		}
		title := physRowTitle(anchor, row)
		if !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := physDateValues(row)
		typ := physModuleType(row)
		if index, exists := seen[fullURL]; exists {
			// The home1 hero carousel may contain a shortened/duplicate copy of
			// the right-hand list. Keep the complete title and precise date.
			if physTitleIsTruncated(items[index].Title) && !physTitleIsTruncated(title) {
				items[index].Title = title
			}
			if items[index].Date == "" && date != "" {
				items[index].Date = date
				items[index].PublishedAt = publishedAt
			}
			if items[index].Type == "" && typ != "" {
				items[index].Type = typ
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	})
	return items
}

func physRowTitle(anchor, row *goquery.Selection) string {
	if row != nil && row.Length() > 0 {
		for _, selector := range []string{"h3", "h4", ".xw-ul-tt", ".news-title"} {
			if title := cleanTitle(row.Find(selector).First().Text()); title != "" {
				return title
			}
		}
	}
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	return cleanTitle(anchor.AttrOr("title", anchor.Text()))
}

func physModuleType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	if column := row.Closest(".left, .right").First(); column.Length() > 0 {
		if typ := physCleanType(column.Find(".title .con h3").First().Text()); typ != "" {
			return typ
		}
	}
	if section := row.Closest(".home1, .home3, .home4").First(); section.Length() > 0 {
		return physCleanType(section.Find(".title .con h3").First().Text())
	}
	return ""
}

func physTitleIsTruncated(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasSuffix(value, "...") || strings.HasSuffix(value, "…")
}

func physAnnotateItems(doc *goquery.Document, items []model.NewsItem, baseURL, pageType string) {
	for i := range items {
		anchor := physFindAnchor(doc, items[i].URL, baseURL)
		if anchor != nil && anchor.Length() > 0 {
			row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
			if row.Length() == 0 {
				row = anchor.Parent()
			}
			if date, publishedAt := physDateValues(row); date != "" {
				items[i].Date = date
				items[i].PublishedAt = publishedAt
			}
			if typ := physRowType(row); typ != "" {
				items[i].Type = typ
			}
		}
		if items[i].Type == "" {
			items[i].Type = pageType
		}
		if date, publishedAt := physNormalizeDate(items[i].Date); date != "" {
			items[i].Date = date
			if items[i].PublishedAt == "" && publishedAt != "" {
				items[i].PublishedAt = publishedAt
			}
		}
	}
}

func physFindAnchor(doc *goquery.Document, itemURL, baseURL string) *goquery.Selection {
	var found *goquery.Selection
	doc.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if cleanURL(resolveURL(baseURL, anchor.AttrOr("href", ""))) == itemURL {
			found = anchor
			return false
		}
		return true
	})
	return found
}

func physPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	pageKind := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	if pageKind != "3" {
		if title := strings.TrimSpace(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).Last().AttrOr("content", "")); title != "" && title != "首页" {
			return physCleanType(title)
		}
	}
	if typ := bodaArticleSectionType(doc, ""); typ != "" {
		return typ
	}
	for _, selector := range []string{".list-title", ".list-head", ".ny-title", ".main_titT", "h1", "h2"} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			value := strings.Join(strings.Fields(selection.Eq(i).Text()), " ")
			if value != "" && value != "首页" && value != "新闻公告" && value != "当前位置" {
				return value
			}
		}
	}
	return ""
}

func physRowType(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	for _, selector := range []string{".type", ".category", ".column", ".lanmu", ".source", ".tag"} {
		if value := physCleanType(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func physCleanType(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "[]【】〖〗")
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "栏目："), "栏目:"))
}

func physDateValues(row *goquery.Selection) (string, string) {
	if row == nil || row.Length() == 0 {
		return "", ""
	}
	raw := ""
	for _, selector := range []string{".time", ".date", "time", "em", ".xw-ul-date"} {
		raw = strings.TrimSpace(row.Find(selector).First().Text())
		if raw != "" {
			break
		}
	}
	if raw == "" {
		row.Find("p, span").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
			value := strings.TrimSpace(candidate.Text())
			if normalizePublishedAt(value) != "" {
				raw = value
				return false
			}
			return true
		})
	}
	// Do not search the whole row: a title can contain an event date or a
	// deadline, neither of which is the list's fixed publication field.
	return physNormalizeDate(raw)
}

func physNormalizeDate(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" || physHasLegacyPlaceholder(raw) {
		return "", ""
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		if physHasLegacyPlaceholder(publishedAt) {
			return "", ""
		}
		if publishedAtHasClock(publishedAt) {
			return publishedAt[:10], publishedAt
		}
		return publishedAt[:10], ""
	}
	if match := physShortDateRE.FindStringSubmatch(raw); len(match) == 3 {
		return fmt.Sprintf("%02s-%02s", match[1], match[2]), ""
	}
	return "", ""
}

func physHasLegacyPlaceholder(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), physLegacyPlaceholderDate)
}

// physLooksUnavailable distinguishes a real source page from the small error,
// login, and verification pages that the CMS also returns as HTML/200.
func physLooksUnavailable(doc *goquery.Document) bool {
	if doc == nil {
		return true
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	for _, marker := range []string{"404", "403", "access denied", "request rejected", "安全验证", "登录", "验证码下载附件"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	text := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
	for _, marker := range []string{"access denied", "request rejected", "页面不存在", "请输入验证码下载附件", "请先登录"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func physArticleTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".article h2.tit", ".article h1.tit", "h2.tit", "h1.news_tit", ".news-title", ".article-title", "h1"} {
		if title := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); title != "" {
			return title
		}
	}
	return ""
}

func physHasArticleShell(doc *goquery.Document) bool {
	return physArticleTitle(doc) != "" && doc.Find(".v_news_content").Length() > 0
}

func physArticleDate(doc *goquery.Document) (string, string) {
	if doc == nil {
		return "", ""
	}
	for _, selector := range []string{".conttime span", ".conttime", ".news_source span", ".news_source", ".top-info span", ".top-info"} {
		var date, publishedAt string
		doc.Find(selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			raw := strings.TrimSpace(sel.Text())
			if raw == "" {
				return true
			}
			if !strings.Contains(raw, "发布时间") && !strings.Contains(raw, "发布日期") && !strings.Contains(raw, "发表") && !strings.Contains(raw, "日期") && !sel.HasClass("date") {
				return true
			}
			date, publishedAt = physNormalizeDate(raw)
			return date == ""
		})
		if date != "" {
			return date, publishedAt
		}
	}
	return "", ""
}

func physArticleType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	return physCleanType(bodaArticleSectionType(doc, physArticleTitle(doc)))
}

func physFilterArticleResources(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil {
		return
	}
	validImages := make(map[string]bool)
	if doc != nil {
		doc.Find(".v_news_content img").Each(func(_ int, sel *goquery.Selection) {
			width := strings.TrimSpace(sel.AttrOr("width", ""))
			height := strings.TrimSpace(sel.AttrOr("height", ""))
			if width == "1" && height == "1" {
				return
			}
			for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
				if raw := strings.TrimSpace(sel.AttrOr(attr, "")); raw != "" {
					if resolved := bodaResourceURL(baseURL, raw); resolved != "" && !physIsPlaceholderResource(resolved) {
						validImages[resolved] = true
					}
					break
				}
			}
		})
	}
	filteredImages := article.Images[:0]
	for _, image := range article.Images {
		if validImages[image] || len(validImages) == 0 && physUsableResourceURL(image) && !physIsPlaceholderResource(image) {
			if !physContainsString(filteredImages, image) {
				filteredImages = append(filteredImages, image)
			}
		}
	}
	article.Images = filteredImages

	filteredAttachments := article.Attachments[:0]
	for _, attachment := range article.Attachments {
		if !physUsableResourceURL(attachment.URL) || physBlockedAttachmentURL(attachment.URL) || physIsPlaceholderResource(attachment.URL) {
			continue
		}
		if !physContainsAttachmentURL(filteredAttachments, attachment.URL) {
			filteredAttachments = append(filteredAttachments, attachment)
		}
	}
	article.Attachments = filteredAttachments
}

func physUsableResourceURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "http" || scheme == "https"
}

func physIsPlaceholderResource(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	lower := strings.ToLower(parsed.Path)
	for _, marker := range []string{"/spacer", "/blank", "/transparent", "/placeholder", "/noimage", "/no-image", "/loading"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func physBlockedAttachmentURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	if strings.EqualFold(parsed.Path, "/system/_content/download.jsp") && parsed.Query().Get("urltype") == "news.DownloadAttachUrl" {
		// The live phys endpoint returns an HTML CAPTCHA gate (200 text/html),
		// not the advertised file. It must not be exposed as a verified asset.
		return true
	}
	lower := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	return strings.Contains(lower, "login") || strings.Contains(lower, "captcha") || strings.Contains(lower, "verify")
}

func physContainsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func physContainsAttachmentURL(values []model.Attachment, wanted string) bool {
	for _, value := range values {
		if value.URL == wanted {
			return true
		}
	}
	return false
}
