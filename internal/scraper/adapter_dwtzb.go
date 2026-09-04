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

const dwtzbBaseURL = "https://dwtzb.swjtu.edu.cn"

const dwtzbMaxResourceBytes = 32 << 20

// These are the reachable article-list routes exposed by dwtzb's navigation
// and its homepage.  Department profile pages are static content pages and
// are intentionally not registered as feeds.
var dwtzbSubPages = []string{
	"zhyw/xnxw",
	"zhyw/hlkd",
	"mzdp1",
	"mzdp1/mg/dpgz",
	"mzdp1/mm/dpgz",
	"mzdp1/mj/dpgz",
	"mzdp1/mj1/dpgz",
	"mzdp1/ngd/dpgz",
	"mzdp1/zgd/dpgz",
	"mzdp1/jsxs/dpgz",
	"tztt/ql_xq_",
	"tztt/omtxh_lxrylyh_",
	"tztt/dwzsfzlyh",
	"wdp",
	"mzgz",
	"czyz1",
	"czyz1/rddb",
	"czyz1/zxwy",
	"czyz1/qtdwdbrs",
	"zcws",
	"zcws/ldls",
	"zcws/fzzcjd",
	"zcws/tzllyj",
	"qzj",
	"shzyxy",
	"bszn",
	"tzgg",
	"rwfc",
}

var (
	dwtzbArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	dwtzbMetaLabelRE   = regexp.MustCompile(`(来源|作者|编辑|责编|摄影|日期|发布时间|文章类型|信息类型|类型|类别|分类)\s*[:：]\s*`)
	dwtzbShellRE       = regexp.MustCompile(`(?is)<!doctype\s+html|<html(?:\s|>)|<head(?:\s|>)|<body(?:\s|>)|附件下载|请输入验证码|验证码下载附件|请先登录|请登录后|登录页面|页面不存在|404\s*错误|404\s*not\s*found|access\s*denied|forbidden|cloudflare|just\s+a\s+moment|安全验证|请求已被阻断`)
)

// dwtzbAdapter keeps this site's homepage modules, list markup, reverse
// pagination and article shell validation local to the site.
func init() {
	registerAdapter("dwtzb", SiteAdapter{
		SubPages:     dwtzbSubPages,
		ParseList:    parseAdapter_dwtzbList,
		ParseArticle: parseAdapter_dwtzbArticle,
		PageURL:      dwtzbPageURL,
		TotalPages:   dwtzbTotalPages,
		FetchArticle: dwtzbFetchArticle,
	})
}

// dwtzbPageURL maps logical pages (1 = newest) to the site's generated files:
// slug.htm, slug/{total-1}.htm, ..., slug/1.htm.
func dwtzbPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if slug == "" {
		slug = "index"
	}
	if baseURL == "" || page < 1 {
		return ""
	}
	if page == 1 {
		return baseURL + "/" + slug + extension
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	generatedPage := totalPages - page + 1
	if generatedPage < 1 {
		return ""
	}
	return baseURL + "/" + slug + "/" + strconv.Itoa(generatedPage) + extension
}

func parseAdapter_dwtzbList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse dwtzb list: %w", err)
	}
	if reason := dwtzbHTMLShellReason(doc); reason != "" {
		return nil, fmt.Errorf("dwtzb list response is an error/WAF shell: %s", reason)
	}

	// index.htm is a dashboard.  Its six center panels and two sidebar panels
	// are the only homepage article scopes; carousel, navigation and friend
	// links are deliberately outside those scopes.
	if dwtzbIsHomepage(doc) {
		items := parseDwtzbHomepage(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("dwtzb homepage contains no article entries")
		}
		return items, nil
	}

	newslist := doc.Find("#content.pages .newslist").First()
	if newslist.Length() == 0 {
		return nil, fmt.Errorf("dwtzb page is not a supported article list")
	}

	rows := newslist.Find("ul > li")
	if rows.Length() == 0 {
		// A real, recognized newslist with no rows is a legal empty feed.  An
		// unrecognized/static page is rejected above instead of becoming an
		// indistinguishable empty result.
		return []model.NewsItem{}, nil
	}

	typ := dwtzbListType(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a.title[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := dwtzbArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}

		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(anchor.Text())
		}
		title = cleanTitle(title)
		if title == "" || !isValidNewsTitle(title) {
			return
		}

		date, publishedAt := dwtzbDateValues(row.Find(".pubtime").First().Text())
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
		return nil, fmt.Errorf("dwtzb page contains no valid article rows")
	}
	return items, nil
}

func dwtzbIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	return doc.Find("#slides").Length() > 0 || doc.Find(".posts-loop").Length() > 0
}

// dwtzbHTMLShellReason identifies successful HTTP responses which are not
// usable dwtzb pages.  The site has returned both a generated 404 page and a
// CAPTCHA/WAF shell with status 200; accepting either as an empty feed would
// silently truncate an archive.
func dwtzbHTMLShellReason(doc *goquery.Document) string {
	if doc == nil {
		return "nil document"
	}
	title := strings.Join(strings.Fields(doc.Find("title").First().Text()), " ")
	if dwtzbShellRE.MatchString(title) {
		return title
	}
	body := doc.Find("body").First().Clone()
	if body.Length() > 0 {
		body.Find("script, style, noscript").Remove()
		visible := strings.Join(strings.Fields(body.Text()), " ")
		if dwtzbShellRE.MatchString(visible) {
			return visible
		}
	}
	// A SPA shell has no server-rendered dwtzb content.  Check the distinctive
	// mount nodes here, but do not reject a normal page merely for carrying an
	// unrelated script or div with a similar id.
	if doc.Find("#app, #root, #__next").Length() > 0 && doc.Find("#content.pages").Length() == 0 {
		return "SPA shell without server-rendered content"
	}
	return ""
}

func parseDwtzbHomepage(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	add := func(anchor *goquery.Selection, title, date, typ string) {
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		fullURL, ok := dwtzbArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title = cleanTitle(strings.TrimSpace(title))
		if title == "" || !isValidNewsTitle(title) || strings.TrimSpace(typ) == "" {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     strings.Trim(strings.TrimSpace(date), "[]【】"),
			Type:     strings.TrimSpace(typ),
			ParsedAt: time.Now(),
		})
	}

	doc.Find(".posts-loop").Each(func(_ int, loop *goquery.Selection) {
		post := loop.Find(".post").First()
		if post.Length() == 0 {
			return
		}
		typ := strings.TrimSpace(post.Find("h2.title > a").First().AttrOr("title", ""))
		if typ == "" {
			typ = strings.TrimSpace(post.Find("h2.title > a").First().Text())
		}
		post.Find("ul.post-list > li").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			title := strings.TrimSpace(anchor.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(anchor.Text())
			}
			date := strings.Join(strings.Fields(row.Find(".datetime, .date, .time").First().Text()), " ")
			add(anchor, title, date, typ)
		})
	})

	doc.Find(".sidebar > ul > li.panel").Each(func(_ int, panel *goquery.Selection) {
		typ := strings.Join(strings.Fields(panel.Find("h3 > span").First().Text()), " ")
		if typ == "" {
			return
		}
		panel.Find("ul.list > li, ul.images > li").Each(func(_ int, row *goquery.Selection) {
			anchor := row.Find("a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			title := strings.TrimSpace(anchor.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(row.Find(".name").First().Text())
			}
			add(anchor, title, "", typ)
		})
	})

	return items
}

func parseAdapter_dwtzbArticle(raw []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse dwtzb article: %w", err)
	}
	if reason := dwtzbHTMLShellReason(doc); reason != "" {
		return nil, fmt.Errorf("dwtzb article response is an error/WAF shell: %s", reason)
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("dwtzb page is not an article (pageType %q)", pageType)
	}

	// dwtzb's detail template is stable: all article fields live below this
	// exact content shell.  In particular, do not fall back to a same-named
	// .artical or .v_news_content from a navigation/mobile/SPA fragment.
	scope := doc.Find("#content.pages .artical").First()
	if scope.Length() == 0 {
		return nil, fmt.Errorf("dwtzb article has no article container")
	}
	content := scope.Find("#vsb_content_2 .v_news_content, #vsb_content .v_news_content").First()
	if content.Length() == 0 {
		return nil, fmt.Errorf("dwtzb article has no content container")
	}
	if strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a").Length() == 0 {
		return nil, fmt.Errorf("dwtzb article has empty content")
	}

	title := strings.Join(strings.Fields(scope.ChildrenFiltered("h4").First().Text()), " ")
	if title == "" {
		return nil, fmt.Errorf("dwtzb article has no title")
	}
	article := &model.Article{Title: title, Type: dwtzbBreadcrumbType(doc)}

	if pubinfo := scope.ChildrenFiltered(".pubinfo").First(); pubinfo.Length() > 0 {
		fields := dwtzbMetaFields(flattenedText(pubinfo))
		if _, ok := fields["来源"]; ok {
			article.Source = fields["来源"]
		}
		if _, ok := fields["作者"]; ok {
			article.Author = fields["作者"]
		}
		if _, ok := fields["编辑"]; ok {
			article.Editor = fields["编辑"]
		}
		if _, ok := fields["摄影"]; ok {
			article.Photographer = fields["摄影"]
		}
		dateValue := fields["日期"]
		if dateValue == "" {
			dateValue = fields["发布时间"]
		}
		if date, publishedAt := dwtzbDateValues(dateValue); date != "" {
			article.Date = date
			article.PublishedAt = publishedAt
		}
	}

	article.Content, article.ContentHTML, article.Images = dwtzbContentValues(content, baseURL)
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && content.Find("table, a").Length() == 0 {
		return nil, fmt.Errorf("dwtzb article has no readable content")
	}

	// The CMS download.jsp link is outside .v_news_content on current dwtzb
	// notices.  Collect it together with same-site direct files, but leave
	// ordinary external links in ContentHTML rather than promoting them to
	// attachments.
	appendDwtzbAttachments(article, scope, baseURL)
	dwtzbParseAdjacent(article, doc, baseURL)
	return article, nil
}

func appendDwtzbAttachments(article *model.Article, scope *goquery.Selection, baseURL string) {
	if article == nil || scope == nil || scope.Length() == 0 {
		return
	}
	article.Attachments = nil
	seen := make(map[string]bool)
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := htmlstd.UnescapeString(strings.TrimSpace(anchor.AttrOr("href", "")))
		if !dwtzbAttachmentCandidate(anchor, href, baseURL) {
			return
		}
		resolved := dwtzbResourceURL(baseURL, href)
		if resolved == "" || seen[resolved] {
			return
		}
		name := dwtzbAttachmentName(anchor, resolved)
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func dwtzbContentValues(content *goquery.Selection, baseURL string) (string, string, []string) {
	if content == nil || content.Length() == 0 {
		return "", "", nil
	}
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	images := make([]string, 0)
	seen := make(map[string]bool)
	clone.Find("img").Each(func(_ int, image *goquery.Selection) {
		var raw string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				raw = value
				break
			}
		}
		resolved := dwtzbResourceURL(baseURL, raw)
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
		if !seen[resolved] {
			seen[resolved] = true
			images = append(images, resolved)
		}
	})
	contentText := dwtzbContentText(clone, baseURL)
	rendered, err := clone.Html()
	if err != nil {
		rendered = ""
	}
	return contentText, rendered, images
}

// dwtzbContentText mirrors the shared text extraction rules but uses the
// dwtzb attachment predicate.  The shared predicate treats every external
// .pdf link as an attachment; dwtzb must keep such links as ordinary article
// links unless the page explicitly marks them as a download.
func dwtzbContentText(content *goquery.Selection, baseURL string) string {
	if content == nil || content.Length() == 0 {
		return ""
	}
	textSel := content.Clone()
	textSel.Find("script, style, noscript").Remove()
	textSel.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if bodaIsPseudoResourceHref(href) && !strings.HasPrefix(strings.ToLower(href), "#") {
			link.Remove()
			return
		}
		if !dwtzbAttachmentCandidate(link, href, baseURL) {
			return
		}
		if row := link.Closest("li"); row.Length() > 0 && strings.Contains(row.Text(), "附件") {
			row.Remove()
			return
		}
		link.Remove()
	})

	parts := make([]string, 0)
	appendText := func(sel *goquery.Selection) {
		if value := strings.Join(strings.Fields(sel.Text()), " "); value != "" {
			parts = append(parts, value)
		}
	}
	textSel.Find("p, h1, h2, h3, h4, li, td, th, div").Each(func(_ int, sel *goquery.Selection) {
		node := sel.Get(0)
		if node == nil {
			return
		}
		switch strings.ToLower(node.Data) {
		case "p", "h1", "h2", "h3", "h4":
			appendText(sel)
		case "li":
			if sel.Find("p, h1, h2, h3, h4, li, td, th").Length() > 0 {
				return
			}
			appendText(sel)
		case "td", "th":
			if sel.Find("p, h1, h2, h3, h4, li, td, th").Length() > 0 {
				return
			}
			appendText(sel)
		case "div":
			if sel.Closest("p, h1, h2, h3, h4, li, td, th").Length() > 0 {
				return
			}
			if sel.Find("p, h1, h2, h3, h4, li, td, th, div").Length() > 0 {
				return
			}
			if sel.Find("a, img").Length() > 0 {
				return
			}
			appendText(sel)
		}
	})
	if len(parts) == 0 {
		return strings.Join(strings.Fields(textSel.Text()), " ")
	}
	return strings.Join(parts, "\n\n")
}

func dwtzbAttachmentCandidate(sel *goquery.Selection, href, baseURL string) bool {
	if sel == nil || sel.Length() == 0 || href == "" || bodaIsPseudoResourceHref(href) {
		return false
	}
	if strings.TrimSpace(sel.AttrOr("download", "")) != "" {
		return true
	}
	lower := strings.ToLower(href)
	if strings.Contains(lower, "download.jsp") && strings.Contains(lower, "downloadattachurl") {
		return true
	}
	for _, selector := range []string{".attachment", ".attachments", ".news-attachment", ".download"} {
		if ancestor := sel.Closest(selector); ancestor.Length() > 0 && strings.Contains(ancestor.Text(), "附件") {
			return true
		}
	}
	resolved := dwtzbResourceURL(baseURL, href)
	if resolved == "" || !dwtzbSameHost(baseURL, resolved) {
		return false
	}
	resourceName := strings.ToLower(resolved)
	for _, ext := range bodaAttachmentExts {
		if strings.Contains(resourceName, ext) {
			return true
		}
	}
	return false
}

func dwtzbAttachmentName(sel *goquery.Selection, resolved string) string {
	for _, attr := range []string{"download", "name"} {
		if value := strings.Join(strings.Fields(htmlstd.UnescapeString(sel.AttrOr(attr, ""))), " "); value != "" {
			return value
		}
	}
	if value := strings.Join(strings.Fields(sel.Text()), " "); value != "" {
		return value
	}
	parsed, err := url.Parse(resolved)
	if err == nil {
		if value := strings.TrimSpace(parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]); value != "" {
			return value
		}
	}
	return ""
}

func dwtzbParseAdjacent(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	doc.Find("#content.pages .news_sxp li a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		resolved := cleanURL(resolveURL(baseURL, href))
		if href == "" || resolved == "" {
			return
		}
		text := strings.Join(strings.Fields(link.Text()), " ")
		switch {
		case strings.HasPrefix(text, "上一篇"):
			article.PrevURL, article.PrevTitle = resolved, text
		case strings.HasPrefix(text, "下一篇"):
			article.NextURL, article.NextTitle = resolved, text
		}
	})
}

func dwtzbMetaFields(text string) map[string]string {
	text = strings.Join(strings.Fields(text), " ")
	fields := make(map[string]string)
	matches := dwtzbMetaLabelRE.FindAllStringSubmatchIndex(text, -1)
	for i, match := range matches {
		if len(match) < 4 {
			continue
		}
		label := text[match[2]:match[3]]
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		value := strings.TrimSpace(text[match[1]:end])
		fields[label] = value
	}
	return fields
}

func dwtzbDateValues(raw string) (string, string) {
	raw = strings.Join(strings.Fields(raw), " ")
	if raw == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(raw); normalized != "" && len(normalized) >= 10 {
		date := normalized[:10]
		if publishedAtHasClock(normalized) {
			return date, normalized
		}
		return date, ""
	}
	return "", ""
}

func dwtzbBreadcrumbType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	typ := ""
	doc.Find("#content.pages .thumb a").Each(func(_ int, anchor *goquery.Selection) {
		value := strings.Join(strings.Fields(anchor.Text()), " ")
		value = strings.Trim(value, "[]【】")
		if value == "" || value == "首页" || value == "正文" {
			return
		}
		typ = value
	})
	return typ
}

func dwtzbListType(doc *goquery.Document) string {
	if typ := dwtzbBreadcrumbType(doc); typ != "" {
		return typ
	}
	return strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", "")), " ")
}

func dwtzbArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !dwtzbArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	return fullURL, true
}

func dwtzbSameHost(baseURL, candidate string) bool {
	base, baseErr := url.Parse(baseURL)
	target, targetErr := url.Parse(candidate)
	return baseErr == nil && targetErr == nil && base.Host != "" && target.Host != "" && strings.EqualFold(base.Host, target.Host)
}

func dwtzbResourceURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"/captcha", "captcha", "verifycode", "verification", "createimage", "getcode", "codevalue",
		"placeholder", "transparent", "spacer", "blank.gif", "loading.gif", "pixel.gif", "access-denied", "waf",
		"验证码",
	} {
		if strings.Contains(resourceText, marker) {
			return ""
		}
	}
	parsed.Fragment = ""
	return cleanURL(parsed.String())
}

func dwtzbLinkURL(baseURL, raw string) string {
	raw = htmlstd.UnescapeString(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "#") {
		return raw
	}
	return dwtzbResourceURL(baseURL, raw)
}

func dwtzbFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := dwtzbBaseURL
	if site := AllSites["dwtzb"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	fullURL, ok := dwtzbArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("dwtzb: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("dwtzb: fetch article %s: %w", fullURL, err)
	}
	article, err := parseAdapter_dwtzbArticle(body, baseURL)
	if err != nil {
		return nil, body, fmt.Errorf("dwtzb: parse article %s: %w", fullURL, err)
	}
	article.Images = dwtzbVerifyImages(article.Images, baseURL, fullURL)
	article.Attachments = dwtzbVerifyAttachments(article.Attachments, baseURL, fullURL)
	article.ContentHTML = dwtzbSanitizeContentHTML(article.ContentHTML, baseURL, article.Images, article.Attachments)
	return article, body, nil
}

func dwtzbVerifyImages(images []string, baseURL, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := dwtzbResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := dwtzbFetchResource(resolved, baseURL, referer, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func dwtzbVerifyAttachments(attachments []model.Attachment, baseURL, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := dwtzbResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := dwtzbFetchResource(resolved, baseURL, referer, false)
		if err != nil {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func dwtzbFetchResource(rawURL, baseURL, referer string, image bool) ([]byte, string, error) {
	resolved := dwtzbResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("dwtzb: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("dwtzb: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("dwtzb: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("dwtzb: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, dwtzbMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("dwtzb: read resource %s: %w", resolved, err)
	}
	if len(body) > dwtzbMaxResourceBytes || !dwtzbResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("dwtzb: resource %s returned an invalid type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func dwtzbResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if dwtzbShellRE.Match(probe) {
		return false
	}
	magic := dwtzbResourceMagic(body)
	if image {
		if !strings.HasPrefix(mediaType, "image/") || !dwtzbIsImageMagic(magic) {
			return false
		}
		switch mediaType {
		case "image/jpeg", "image/jpg":
			return magic == "jpeg"
		case "image/png":
			return magic == "png"
		case "image/gif":
			return magic == "gif"
		case "image/webp":
			return magic == "webp"
		case "image/bmp":
			return magic == "bmp"
		case "image/tiff":
			return magic == "tiff"
		case "image/svg+xml":
			return magic == "svg"
		default:
			return true
		}
	}

	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case strings.Contains(lowerName, ".txt") || strings.Contains(lowerName, ".csv") || strings.HasPrefix(mediaType, "text/"):
		return true
	default:
		return magic != ""
	}
}

func dwtzbIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func dwtzbResourceMagic(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x03, 0x04}):
		return "zip"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x05, 0x06}):
		return "zip"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x07, 0x08}):
		return "zip"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}):
		return "ole"
	case len(body) >= 3 && bytes.Equal(body[:3], []byte{0xff, 0xd8, 0xff}):
		return "jpeg"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "png"
	case len(body) >= 6 && (bytes.Equal(body[:6], []byte("GIF87a")) || bytes.Equal(body[:6], []byte("GIF89a"))):
		return "gif"
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		return "webp"
	case len(body) >= 2 && bytes.Equal(body[:2], []byte("BM")):
		return "bmp"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{'I', 'I', '*', 0}) || bytes.Equal(body[:4], []byte{'M', 'M', 0, '*'})):
		return "tiff"
	case len(body) >= 7 && bytes.Equal(body[:7], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00}):
		return "rar"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x01, 0x00}):
		return "rar"
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	if len(trimmed) >= 4 && strings.HasPrefix(strings.ToLower(string(trimmed)), "<svg") {
		return "svg"
	}
	return ""
}

func dwtzbSanitizeContentHTML(raw, baseURL string, images []string, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	validImages := make(map[string]bool, len(images))
	for _, image := range images {
		if resolved := dwtzbResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	validAttachments := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		if resolved := dwtzbResourceURL(baseURL, attachment.URL); resolved != "" {
			validAttachments[resolved] = true
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"dwtzb-content-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	root := doc.Find("#dwtzb-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		var rawSource string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				rawSource = value
				break
			}
		}
		resolved := dwtzbResourceURL(baseURL, rawSource)
		if resolved == "" || !validImages[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "orisrc"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := htmlstd.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		if href == "" {
			return
		}
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(strings.ToLower(href), "#") {
				link.Remove()
			}
			return
		}
		resolved := dwtzbLinkURL(baseURL, href)
		if resolved == "" {
			link.Remove()
			return
		}
		if dwtzbAttachmentCandidate(link, href, baseURL) {
			resourceURL := dwtzbResourceURL(baseURL, resolved)
			if resourceURL == "" || !validAttachments[resourceURL] {
				link.Remove()
				return
			}
			resolved = resourceURL
		}
		link.SetAttr("href", resolved)
	})
	rendered, err := root.Html()
	if err != nil {
		return ""
	}
	return rendered
}

func dwtzbTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if strings.EqualFold(slug, "index") {
		return 1, nil
	}
	if !dwtzbIsKnownListRoute(slug) {
		return 0, fmt.Errorf("dwtzb: %s is not an audited article-list route", slug)
	}
	baseURL := dwtzbBaseURL
	if site := AllSites["dwtzb"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = site.BaseURL
	}
	firstURL := dwtzbPageURL(baseURL, slug, 1, 0, ".htm")
	body, err := dwtzbFetchListPage(firstURL)
	if err != nil {
		return 0, fmt.Errorf("dwtzb: read first page for %s: %w", slug, err)
	}
	if !dwtzbHasNewslist(body) {
		return 0, fmt.Errorf("dwtzb: %s is not a supported article list", slug)
	}
	if total := dwtzbTotalPagesFromHTML(body); total > 0 {
		return total, nil
	}
	return 1, nil
}

func dwtzbFetchListPage(pageURL string) ([]byte, error) {
	body, err := fetch(pageURL)
	if err == nil && dwtzbHasNewslist(body) {
		return body, nil
	}
	if strings.HasSuffix(strings.ToLower(pageURL), ".htm") {
		if fallback, fallbackErr := fetch(strings.TrimSuffix(pageURL, ".htm") + ".html"); fallbackErr == nil {
			if dwtzbHasNewslist(fallback) {
				return fallback, nil
			}
			if err != nil {
				return nil, fallbackErr
			}
		}
	}
	if err == nil {
		return body, nil
	}
	return nil, err
}

func dwtzbHasNewslist(html []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	return err == nil && dwtzbHTMLShellReason(doc) == "" && doc.Find("#content.pages .newslist").Length() > 0
}

func dwtzbIsKnownListRoute(slug string) bool {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	for _, route := range dwtzbSubPages {
		if route == slug {
			return true
		}
	}
	return false
}

func dwtzbTotalPagesFromHTML(html []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}
	maxPage := 0
	doc.Find("#content.pages .p_pages .p_no, #content.pages .p_pages .p_no_d").Each(func(_ int, sel *goquery.Selection) {
		value, err := strconv.Atoi(strings.TrimSpace(sel.Text()))
		if err == nil && value > maxPage {
			maxPage = value
		}
	})
	return maxPage
}
