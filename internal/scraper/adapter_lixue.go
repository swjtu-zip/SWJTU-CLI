package scraper

import (
	"bytes"
	"fmt"
	htmlstd "html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const lixueCanonicalBaseURL = "https://lixue.swjtu.edu.cn"

// lixueSubPages contains the list shells found by walking the live home page,
// its hidden notice tabs, and every linked section. Empty shells are retained
// when the source exposes a real paginated article-list template. Static
// profile/landing pages, external-only pages, and the student-introduction
// parent are deliberately not registered as article feeds.
var lixueSubPages = []string{
	"xwgg/xytz",
	"xwgg/xytz/dj",
	"xwgg/xytz/jx",
	"xwgg/xytz/ky",
	"xwgg/xytz/xg",
	"xwgg/xytz/zj",
	"xwgg/xytz/my",
	"xwgg/xyxw",
	"xwgg/ttxw",
	"xshd",
	"djgz/djdt",
	"djgz/djtz",
	"djgz/jgdj",
	"djgz/xsdj",
	"djgz/llxx",
	"jyjx",
	"jyjx/jxxw",
	"jyjx/kcjs",
	"jyjx/jcjs",
	"jyjx/jxjy",
	"jyjx/jxtd",
	"jyjx/yjsjy",
	"xsky",
	"xsky/xkxw",
	"xsky/xkgk",
	"xsky/xkpt",
	"xsky/xktz",
	"xsky/yjfx",
	"xsky/yjtd",
	"xsgz/xgtz",
	"xsgz/xgdt",
	"xsgz/txhd",
	"xsgz/bjfc1",
	"jwgz",
	"jwgz/jwtz",
	"jwgz/jwdt",
	"ghgz",
	"ghgz/ghtz",
	"ghgz/ghdt",
	"xygz",
	"xygz/xyfc",
	"xygz/xyhd",
	"jlhz",
	"jlhz/gjxshy",
	"jlhz/gjzmzjlf",
	"jlhz/gjhytybg",
	"jlhz/gjhkc",
	"jlhz/lxtd",
	"jlhz/gjxsjgrz",
	"zsjy",
	"zsjy/zsgz",
	"zsjy/yjsds",
	"zsjy/jygz",
	"xzzq",
	"sycc",
}

var (
	lixueArticlePathRE      = regexp.MustCompile(`(?i)^/info/(\d+)/\d+\.html?$`)
	lixueSplitDateRE        = regexp.MustCompile(`(20\d{2})\s+(\d{1,2})\s*[-/.]\s*(\d{1,2})`)
	lixuePublicationLabelRE = regexp.MustCompile(`(?i)(?:发布日期|发布时间|发表日期|发表时间|发布于|日期)\s*[:：]?`)
)

// The column id is part of every detail URL and is the only reliable type
// signal for mixed feeds such as xwgg/xytz. It also repairs the party detail
// pages whose breadcrumb stops at the parent “党建工作”.
var lixueColumnTypes = map[string]string{
	"1022": "科学研究",
	"1024": "科学研究",
	"1028": "理论学习",
	"1031": "学生工作",
	"1032": "学工通知",
	"1034": "团学活动",
	"1035": "班级风采",
	"1044": "国际知名专家来访",
	"1056": "工会动态",
	"1059": "学院新闻",
	"1060": "学院通知",
	"1061": "头条新闻",
	"1226": "党建",
	"1227": "教学",
	"1228": "科研",
	"1229": "学工",
	"1232": "双严传承",
	"1239": "纪委通知",
	"1240": "纪委动态",
	"1245": "课程建设",
	"1249": "教学新闻",
	"1255": "学科新闻",
	"1258": "就业工作",
	"1260": "党建通知",
	"1261": "党建动态",
	"1262": "教工党建",
	"1263": "学生党建",
	"1266": "学工动态",
	"1276": "双严传承",
	"1286": "下载专区",
	"1299": "党建工作",
	"1336": "学术活动",
	"1346": "免研",
	"1347": "研学基地",
}

func init() {
	// sites.go keeps the historical HTTP value for compatibility with the
	// registry. The live source serves the same content over HTTPS, so all
	// emitted local URLs use this canonical origin.
	if site := AllSites["lixue"]; site != nil {
		site.BaseURL = lixueCanonicalBaseURL
	}
	registerAdapter("lixue", SiteAdapter{
		SubPages:     lixueSubPages,
		ParseList:    parseAdapter_lixueList,
		ParseArticle: parseAdapter_lixueArticle,
		PageURL:      lixuePageURL,
		TotalPages:   lixueTotalPages,
		FetchArticle: lixueFetchArticle,
	})
}

func lixuePageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = lixueBase(baseURL)
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if baseURL == "" || slug == "" || page < 1 || totalPages < 0 {
		return ""
	}
	if extension != ".html" {
		extension = ".htm"
	}
	if page == 1 {
		return baseURL + "/" + slug + extension
	}
	if totalPages < 1 {
		// Generated files are numbered from newest to oldest. Without the
		// source-reported total there is no safe mapping for page two onward.
		return ""
	}
	physicalPage := totalPages - page + 1
	if physicalPage < 1 || page > totalPages {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physicalPage, extension)
}

func lixueTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug != "index" && !lixueKnownSubPage(slug) {
		return 0, fmt.Errorf("lixue: %q is not an audited article feed", slug)
	}
	for _, extension := range []string{".htm", ".html"} {
		listURL := lixuePageURL(lixueCanonicalBaseURL, slug, 1, 0, extension)
		body, err := fetch(listURL)
		if err != nil {
			continue
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			return 0, fmt.Errorf("lixue: parse %s: %w", listURL, err)
		}
		if err := lixueResponseError(doc); err != nil {
			return 0, err
		}
		pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
		if slug == "index" && pageType == "1" {
			return 1, nil
		}
		if pageType != "2" || !lixueListShell(doc) {
			return 0, fmt.Errorf("lixue: %s is not a list shell (pageType=%q)", listURL, pageType)
		}
		total := lixuePageCount(doc)
		if total < 1 {
			return 0, fmt.Errorf("lixue: %s has no trustworthy page count", listURL)
		}
		return total, nil
	}
	return 0, fmt.Errorf("lixue: unable to fetch audited feed %q", slug)
}

func lixueKnownSubPage(slug string) bool {
	for _, known := range lixueSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func parseAdapter_lixueList(rawHTML []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(rawHTML)))
	if err != nil {
		return nil, fmt.Errorf("lixue: parse list HTML: %w", err)
	}
	if err := lixueResponseError(doc); err != nil {
		return nil, err
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType == "1" {
		return lixueParseHomepage(doc, baseURL)
	}
	if pageType != "2" {
		return nil, fmt.Errorf("lixue: refusing non-list response (pageType=%q)", pageType)
	}
	if !lixueListShell(doc) {
		return nil, fmt.Errorf("lixue: pageType=2 response has no audited article-list shell")
	}

	typ := strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	items := make([]model.NewsItem, 0)
	localRows := make(map[string]bool)
	invalidRows := 0
	for _, row := range lixueListRows(doc) {
		anchor := lixueFirstArticleAnchor(row, baseURL)
		if anchor == nil || anchor.Length() == 0 {
			continue
		}
		articleURL, ok := lixueArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || localRows[articleURL] {
			continue
		}
		localRows[articleURL] = true
		title := lixueRowTitle(row, anchor)
		if title == "" {
			invalidRows++
			continue
		}
		date, publishedAt := lixueDateValues(lixueRowDate(row))
		itemType := lixueTypeForURL(articleURL)
		if itemType == "" {
			itemType = typ
		}
		if itemType == "" {
			invalidRows++
			continue
		}
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         articleURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        itemType,
			ParsedAt:    time.Now(),
		})
	}
	if invalidRows > 0 {
		return nil, fmt.Errorf("lixue: %d local article rows could not be parsed", invalidRows)
	}
	return items, nil
}

func lixueListShell(doc *goquery.Document) bool {
	return doc.Find(".page-news .hots").Length() > 0 || doc.Find(".conts > .list > ul").Length() > 0
}

func lixueListRows(doc *goquery.Document) []*goquery.Selection {
	rows := make([]*goquery.Selection, 0)
	if found := doc.Find(".page-news .hots > ul > li"); found.Length() > 0 {
		found.Each(func(_ int, row *goquery.Selection) { rows = append(rows, row) })
		return rows
	}
	if found := doc.Find(".conts > .list > ul > li"); found.Length() > 0 {
		found.Each(func(_ int, row *goquery.Selection) { rows = append(rows, row) })
	}
	return rows
}

func lixueFirstArticleAnchor(row *goquery.Selection, baseURL string) *goquery.Selection {
	if row == nil || row.Length() == 0 {
		return nil
	}
	var result *goquery.Selection
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if _, ok := lixueArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			result = anchor
			return false
		}
		return true
	})
	return result
}

func lixueRowTitle(row, anchor *goquery.Selection) string {
	for _, selector := range []string{".text", ".title", "h1", "h2", "h3"} {
		if value := lixueText(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	if title := lixueText(anchor.AttrOr("title", "")); title != "" {
		return title
	}
	return lixueText(anchor.Text())
}

func lixueRowDate(row *goquery.Selection) string {
	for _, selector := range []string{".time", ".date", "time"} {
		if value := lixueText(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func lixuePageCount(doc *goquery.Document) int {
	maxPage := 0
	doc.Find(".p_pages .p_no, .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		value := strings.TrimSpace(sel.Text())
		var page int
		if _, err := fmt.Sscanf(value, "%d", &page); err == nil && page > maxPage {
			maxPage = page
		}
	})
	return maxPage
}

func lixueParseHomepage(doc *goquery.Document, baseURL string) ([]model.NewsItem, error) {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	add := func(scope *goquery.Selection, titleSelectors []string, typ string) {
		if scope == nil || scope.Length() == 0 {
			return
		}
		anchor := lixueFirstArticleAnchor(scope, baseURL)
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		articleURL, ok := lixueArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok {
			return
		}
		var title string
		for _, selector := range titleSelectors {
			if value := lixueText(scope.Find(selector).First().Text()); value != "" {
				title = value
				break
			}
		}
		if title == "" {
			title = lixueText(anchor.AttrOr("title", ""))
		}
		if title == "" {
			title = lixueText(anchor.Text())
		}
		if title == "" {
			return
		}
		date, publishedAt := lixueDateValues(lixueRowDate(scope))
		itemType := lixueTypeForURL(articleURL)
		if itemType == "" {
			itemType = typ
		}
		if index, exists := seen[articleURL]; exists {
			item := &items[index]
			if lixuePreferTitle(item.Title, title) {
				item.Title = title
			}
			if item.Date == "" && date != "" {
				item.Date = date
				item.PublishedAt = publishedAt
			}
			if item.Type == "" || item.Type == "学院新闻" || item.Type == "学院通知" || item.Type == typ {
				item.Type = itemType
			}
			return
		}
		seen[articleURL] = len(items)
		items = append(items, model.NewsItem{
			Title:       title,
			URL:         articleURL,
			Date:        date,
			PublishedAt: publishedAt,
			Type:        itemType,
			ParsedAt:    time.Now(),
		})
	}

	// Desktop banner text is truncated in the source; the later news block
	// supplies the complete title for duplicate URLs.
	doc.Find(".banner .swiper-wrapper > li").Each(func(_ int, sel *goquery.Selection) {
		add(sel, []string{".text"}, "学院新闻")
	})

	// The desktop sidebar contains all hidden tab panels in the initial HTML.
	// Parse those panels directly rather than relying on the tab JavaScript.
	if notice := doc.Find(".sidebar .notices").First(); notice.Length() > 0 {
		tabTypes := make([]string, 0)
		notice.Find(".tabs li").Each(func(_ int, sel *goquery.Selection) {
			tabTypes = append(tabTypes, lixueText(sel.Text()))
		})
		notice.Find(".list > .item").Each(func(index int, panel *goquery.Selection) {
			typ := "学院通知"
			if index < len(tabTypes) && tabTypes[index] != "" && tabTypes[index] != "全部" {
				typ = tabTypes[index]
			}
			panel.Find("ul > li").Each(func(_ int, row *goquery.Selection) {
				add(row, []string{".text"}, typ)
			})
		})
	}

	doc.Find(".news-wrap .news .list .lists > ul > li").Each(func(_ int, sel *goquery.Selection) {
		add(sel, []string{".text"}, "学院新闻")
	})
	doc.Find(".academic-wrap .swiper-container .swiper-wrapper > li").Each(func(_ int, sel *goquery.Selection) {
		// Do not parse .desc: its “时间:” value is the event time, not the
		// publication date, and the source does not expose a publication date.
		add(sel, []string{".tit"}, "学术活动")
	})
	doc.Find(".dynamic-wrap .dynamic .list").Each(func(_ int, section *goquery.Selection) {
		typ := lixueDynamicType(section.Find(".title .text span").First().Text())
		section.Find(".cont .lists > ul > li").Each(func(_ int, row *goquery.Selection) {
			add(row, []string{".text"}, typ)
		})
	})

	if len(items) == 0 {
		return nil, fmt.Errorf("lixue: homepage has no recognized article module")
	}
	return items, nil
}

func lixueDynamicType(value string) string {
	switch lixueText(value) {
	case "党群工作":
		return "党建动态"
	case "教育教学":
		return "教学新闻"
	case "学生工作":
		return "团学活动"
	case "科学研究":
		return "科学研究"
	default:
		return lixueText(value)
	}
}

func lixuePreferTitle(oldTitle, newTitle string) bool {
	if oldTitle == "" {
		return true
	}
	if strings.HasSuffix(oldTitle, "...") || strings.HasSuffix(oldTitle, "…") {
		return true
	}
	return len([]rune(newTitle)) > len([]rune(oldTitle))
}

func lixueText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func lixueDateValues(value string) (string, string) {
	value = lixueText(value)
	if value == "" {
		return "", ""
	}
	if match := dateTimeRe.FindString(value); match != "" {
		published := normalizePublishedAt(match)
		if len(published) >= 10 {
			return published[:10], published
		}
	}
	if match := lixueSplitDateRE.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	if match := bareDateRe.FindStringSubmatch(value); match != nil {
		date := normalizeDateParts(match[1], match[2], match[3])
		return date, date
	}
	return "", ""
}

func parseAdapter_lixueArticle(rawHTML []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(rawHTML)))
	if err != nil {
		return nil, fmt.Errorf("lixue: parse article HTML: %w", err)
	}
	if err := lixueResponseError(doc); err != nil {
		return nil, err
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "3" {
		return nil, fmt.Errorf("lixue: refusing non-article response (pageType=%q)", pageType)
	}
	titleSel := doc.Find(`form[name="_newscontent_fromname"] h1`).First()
	contentSel := doc.Find(`form[name="_newscontent_fromname"] .v_news_content`).First()
	if titleSel.Length() == 0 || lixueText(titleSel.Text()) == "" {
		return nil, fmt.Errorf("lixue: article shell has no detail title")
	}
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("lixue: article shell has no .v_news_content body")
	}

	article, err := parseBodaArticle(rawHTML, lixueBase(baseURL))
	if err != nil {
		return nil, err
	}
	article.Title = lixueText(titleSel.Text())
	article.Type = lixueBreadcrumbType(doc)
	// parseBodaArticle has intentionally broad fallbacks for other Boda
	// templates. This site has a fixed metadata line, so never retain a date
	// that the shared parser may have found in the prose body.
	article.Date = ""
	article.PublishedAt = ""
	if date, publishedAt := lixueDateValues(lixueDetailMetaText(titleSel)); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	lixueAddAttachments(article, doc, baseURL)
	lixueNormalizeArticleResources(article, baseURL)
	allowedImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		allowedImages[image] = true
	}
	candidateAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		candidateAttachments[attachment.URL] = true
	}
	article.ContentHTML = lixueSanitizeContentHTML(article.ContentHTML, baseURL, allowedImages, candidateAttachments, candidateAttachments)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func lixueFetchArticle(articleURL string) (*model.Article, []byte, error) {
	canonicalURL, ok := lixueArticleURL(lixueCanonicalBaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("lixue: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(canonicalURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_lixueArticle(body, lixueCanonicalBaseURL)
	if err != nil {
		return nil, nil, err
	}
	candidateAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		candidateAttachments[attachment.URL] = true
	}
	article.Images = lixueVerifyImages(article.Images, canonicalURL)
	article.Attachments = lixueVerifyAttachments(article.Attachments, canonicalURL)
	allowedImages := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		allowedImages[image] = true
	}
	allowedAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		allowedAttachments[attachment.URL] = true
	}
	article.ContentHTML = lixueSanitizeContentHTML(article.ContentHTML, lixueCanonicalBaseURL, allowedImages, candidateAttachments, allowedAttachments)
	if typ := lixueTypeForURL(canonicalURL); typ != "" {
		article.Type = typ
	}
	return article, body, nil
}

func lixueDetailMetaText(titleSel *goquery.Selection) string {
	if titleSel == nil || titleSel.Length() == 0 {
		return ""
	}
	parent := titleSel.Parent()
	var result string
	titleNode := titleSel.Get(0)
	seenTitle := false
	parent.Children().EachWithBreak(func(_ int, child *goquery.Selection) bool {
		if child.Get(0) == titleNode {
			seenTitle = true
			return true
		}
		if !seenTitle {
			return true
		}
		if child.Is("hr") || child.Find(".v_news_content").Length() > 0 {
			return false
		}
		if _, _, ok := lixueFixedMetaDate(child); ok {
			result = lixueText(child.Text())
			return false
		}
		return true
	})
	if result != "" {
		return result
	}
	form := titleSel.Closest("form")
	for _, selector := range []string{".news_source", ".top-info", ".article-meta", ".detail-meta", ".news-meta", "time[datetime]"} {
		found := ""
		form.Find(selector).EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
			if candidate.Closest(".v_news_content").Length() > 0 {
				return true
			}
			if _, _, ok := lixueFixedMetaDate(candidate); ok {
				found = lixueText(candidate.Text())
				if found == "" {
					found = lixueText(candidate.AttrOr("datetime", ""))
				}
				return false
			}
			return true
		})
		if found != "" {
			return found
		}
	}
	return ""
}

func lixueFixedMetaDate(sel *goquery.Selection) (string, string, bool) {
	if sel == nil || sel.Length() == 0 || sel.Closest(".v_news_content").Length() > 0 {
		return "", "", false
	}
	value := lixueText(sel.Text())
	if datetime := lixueText(sel.AttrOr("datetime", "")); datetime != "" {
		if date, published := lixueDateValues(datetime); date != "" {
			return date, published, true
		}
	}
	if value == "" {
		return "", "", false
	}
	if match := lixuePublicationLabelRE.FindStringIndex(value); match != nil {
		if date, published := lixueDateValues(value[match[1]:]); date != "" {
			return date, published, true
		}
		return "", "", false
	}
	date, published := lixueDateValues(value)
	if date == "" {
		return "", "", false
	}
	return date, published, true
}

func lixueBreadcrumbType(doc *goquery.Document) string {
	for _, selector := range []string{".page-contents .pages", ".page-contents .location", ".location", ".pages"} {
		crumb := doc.Find(selector).First()
		if crumb.Length() == 0 {
			continue
		}
		var typ string
		crumb.Find("a").Each(func(_ int, link *goquery.Selection) {
			value := lixueText(link.Text())
			if value != "" && value != "首页" && value != "正文" && value != "力航首页" {
				typ = value
			}
		})
		if typ != "" {
			return typ
		}
	}
	return ""
}

func lixueAddAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	normalized := make([]model.Attachment, 0, len(article.Attachments))
	for _, attachment := range article.Attachments {
		resolved, ok := lixueAttachmentURL(baseURL, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		attachment.URL = resolved
		seen[resolved] = true
		normalized = append(normalized, attachment)
	}
	article.Attachments = normalized
	form := doc.Find(`form[name="_newscontent_fromname"]`).First()
	form.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := link.AttrOr("href", "")
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		downloadAttr := strings.TrimSpace(link.AttrOr("download", ""))
		name := lixueText(link.Text())
		if name == "" {
			name = lixueText(downloadAttr)
		}
		resolved, ok := lixueAttachmentURL(baseURL, href)
		if name == "" || !ok || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func lixueNormalizeArticleResources(article *model.Article, baseURL string) {
	if article == nil {
		return
	}
	images := make([]string, 0, len(article.Images))
	seenImages := make(map[string]bool, len(article.Images))
	for _, raw := range article.Images {
		resolved, ok := lixueResourceURL(baseURL, raw)
		if !ok || seenImages[resolved] {
			continue
		}
		seenImages[resolved] = true
		images = append(images, resolved)
	}
	article.Images = images

	attachments := make([]model.Attachment, 0, len(article.Attachments))
	seenAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		resolved, ok := lixueAttachmentURL(baseURL, attachment.URL)
		if !ok || seenAttachments[resolved] {
			continue
		}
		attachment.URL = resolved
		seenAttachments[resolved] = true
		attachments = append(attachments, attachment)
	}
	article.Attachments = attachments
}

func lixueVerifyImages(images []string, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved, ok := lixueResourceURL(lixueCanonicalBaseURL, raw)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := lixueFetchResource(resolved, referer)
		if err != nil || !lixueResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func lixueVerifyAttachments(attachments []model.Attachment, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved, ok := lixueAttachmentURL(lixueCanonicalBaseURL, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := lixueFetchResource(resolved, referer)
		if err != nil || !lixueResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func lixueFetchResource(rawURL, referer string) ([]byte, string, error) {
	resolved, ok := lixueResourceURL(lixueCanonicalBaseURL, rawURL)
	if !ok {
		resolved, ok = lixueAttachmentURL(lixueCanonicalBaseURL, rawURL)
	}
	if !ok {
		return nil, "", fmt.Errorf("lixue: refusing non-local resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("lixue: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/*,application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	if referer = strings.TrimSpace(referer); referer != "" {
		req.Header.Set("Referer", referer)
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if _, ok := lixueLocalURL(lixueCanonicalBaseURL, next.URL.String()); !ok {
			return http.ErrUseLastResponse
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("lixue: fetch resource %s: %w", resolved, err)
	}
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK {
		return nil, contentType, fmt.Errorf("lixue: resource %s: HTTP %d", resolved, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, lixueMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("lixue: read resource %s: %w", resolved, err)
	}
	if int64(len(body)) > lixueMaxResourceBytes {
		return nil, contentType, fmt.Errorf("lixue: resource %s exceeds %d bytes", resolved, lixueMaxResourceBytes)
	}
	return body, contentType, nil
}

const lixueMaxResourceBytes int64 = 32 << 20

func lixueResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if mediaType == "" {
		return false
	}
	probeLen := len(body)
	if probeLen > 4096 {
		probeLen = 4096
	}
	probe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(body[:probeLen]))), "\ufeff")
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "请输入验证码", "验证码下载附件",
		"captcha", "请先登录", "请登录后", "access denied", "just a moment", "cloudflare",
		"请求已被阻断", "安全威胁", "系统提示",
	} {
		if strings.Contains(probe, marker) {
			return false
		}
	}
	if image {
		switch mediaType {
		case "image/png":
			return bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n"))
		case "image/jpeg", "image/jpg":
			return bytes.HasPrefix(body, []byte{0xff, 0xd8, 0xff})
		case "image/gif":
			return bytes.HasPrefix(body, []byte("GIF87a")) || bytes.HasPrefix(body, []byte("GIF89a"))
		case "image/webp":
			return len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP"))
		case "image/bmp":
			return bytes.HasPrefix(body, []byte("BM"))
		case "image/tiff":
			return bytes.HasPrefix(body, []byte{0x49, 0x49, 0x2a, 0x00}) || bytes.HasPrefix(body, []byte{0x4d, 0x4d, 0x00, 0x2a})
		case "image/svg+xml":
			return strings.Contains(probe, "<svg")
		default:
			return false
		}
	}

	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf":
		return bytes.HasPrefix(body, []byte("%PDF-"))
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(lowerName, ".zip") || mediaType == "application/zip":
		return bytes.HasPrefix(body, []byte("PK\x03\x04")) || bytes.HasPrefix(body, []byte("PK\x05\x06")) || bytes.HasPrefix(body, []byte("PK\x07\x08"))
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword":
		return bytes.HasPrefix(body, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1})
	case strings.Contains(lowerName, ".rar"):
		return bytes.HasPrefix(body, []byte("Rar!\x1a\x07"))
	case strings.Contains(lowerName, ".7z"):
		return bytes.HasPrefix(body, []byte{0x37, 0x7a, 0xbc, 0xaf, 0x27, 0x1c})
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv"):
		return mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "application/octet-stream"
	default:
		return lixueKnownBinaryMagic(body) && mediaType != "text/html" && mediaType != "application/xhtml+xml" && mediaType != "application/json" && mediaType != "text/xml" && mediaType != "application/xml"
	}
}

func lixueKnownBinaryMagic(body []byte) bool {
	return bytes.HasPrefix(body, []byte("%PDF-")) ||
		bytes.HasPrefix(body, []byte("PK\x03\x04")) ||
		bytes.HasPrefix(body, []byte("PK\x05\x06")) ||
		bytes.HasPrefix(body, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) ||
		bytes.HasPrefix(body, []byte("Rar!\x1a\x07")) ||
		bytes.HasPrefix(body, []byte{0x37, 0x7a, 0xbc, 0xaf, 0x27, 0x1c}) ||
		(bytes.HasPrefix(body, []byte{0xff, 0xd8, 0xff})) ||
		bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n"))
}

func lixueSanitizeContentHTML(rawHTML, baseURL string, allowedImages, candidateAttachments, allowedAttachments map[string]bool) string {
	if strings.TrimSpace(rawHTML) == "" {
		return rawHTML
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="lixue-content-root">` + rawHTML + `</div>`))
	if err != nil {
		return rawHTML
	}
	root := doc.Find("#lixue-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var resolved string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc", "vurl"} {
			if value := strings.TrimSpace(htmlstd.UnescapeString(image.AttrOr(attr, ""))); value != "" {
				if candidate, ok := lixueResourceURL(baseURL, value); ok {
					resolved = candidate
					break
				}
			}
		}
		if resolved == "" || !allowedImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc", "vurl", "vsbhref", "word_img"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(htmlstd.UnescapeString(anchor.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) {
			anchor.RemoveAttr("href")
			return
		}
		resolved, ok := lixueAttachmentURL(baseURL, href)
		if !ok || !candidateAttachments[resolved] {
			return
		}
		if !allowedAttachments[resolved] {
			anchor.RemoveAttr("href")
			return
		}
		anchor.SetAttr("href", resolved)
	})
	result, err := root.Html()
	if err != nil {
		return rawHTML
	}
	return result
}

func lixueArticleURL(baseURL, href string) (string, bool) {
	href = htmlstd.UnescapeString(strings.TrimSpace(href))
	if href == "" || bodaIsPseudoResourceHref(href) {
		return "", false
	}
	resolved := resolveURL(lixueBase(baseURL), href)
	u, ok := lixueParsedLocalURL(resolved)
	if !ok || !lixueArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), true
}

func lixueLocalURL(baseURL, href string) (string, bool) {
	href = htmlstd.UnescapeString(strings.TrimSpace(href))
	if href == "" || bodaIsPseudoResourceHref(href) {
		return "", false
	}
	resolved := resolveURL(lixueBase(baseURL), href)
	u, ok := lixueParsedLocalURL(resolved)
	if !ok {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func lixueResourceURL(baseURL, raw string) (string, bool) {
	resolved, ok := lixueLocalURL(baseURL, raw)
	if !ok {
		return "", false
	}
	u, err := url.Parse(resolved)
	if err != nil {
		return "", false
	}
	lowerPath := strings.ToLower(u.Path)
	for _, placeholder := range []string{"/spacer.gif", "/blank.gif", "/transparent.gif"} {
		if strings.HasSuffix(lowerPath, placeholder) {
			return "", false
		}
	}
	return resolved, true
}

func lixueAttachmentURL(baseURL, raw string) (string, bool) {
	return lixueLocalURL(baseURL, raw)
}

func lixueParsedLocalURL(rawURL string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" || !strings.EqualFold(u.Hostname(), "lixue.swjtu.edu.cn") {
		return nil, false
	}
	u.Scheme = "https"
	u.Host = "lixue.swjtu.edu.cn"
	return u, true
}

func lixueBase(baseURL string) string {
	if parsed, ok := lixueParsedLocalURL(strings.TrimRight(strings.TrimSpace(baseURL), "/")); ok {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	return lixueCanonicalBaseURL
}

func lixueTypeForURL(articleURL string) string {
	u, err := url.Parse(articleURL)
	if err != nil {
		return ""
	}
	match := lixueArticlePathRE.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return ""
	}
	return lixueColumnTypes[match[1]]
}

func lixueResponseError(doc *goquery.Document) error {
	if doc == nil {
		return fmt.Errorf("lixue: empty response")
	}
	title := strings.ToLower(lixueText(doc.Find("title").First().Text()))
	for _, marker := range []string{
		"cloudflare", "just a moment", "captcha", "access denied", "forbidden", "页面不存在",
		"404", "500 internal server error", "系统提示", "附件下载", "请输入验证码", "用户登录",
		"统一身份认证", "login",
	} {
		if strings.Contains(title, marker) {
			return fmt.Errorf("lixue: WAF/error response (%q)", title)
		}
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType == "1" || pageType == "2" || pageType == "3" {
		return nil
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(lixueText(clone.Text()))
	for _, marker := range []string{
		"请完成安全验证", "安全验证", "access denied", "just a moment", "cf-chl-",
		"请输入验证码", "验证码下载附件", "请先登录", "请登录后", "访问被拒绝", "请求已被阻断",
		"系统错误", "页面不存在", "系统提示",
	} {
		if strings.Contains(visible, marker) {
			return fmt.Errorf("lixue: WAF/verification response")
		}
	}
	return nil
}
