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

const dwtzbBaseURL = "https://dwtzb.swjtu.edu.cn"

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

func parseAdapter_dwtzbArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse dwtzb article: %w", err)
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"]`).AttrOr("content", ""))
	if pageType != "" && pageType != "3" {
		return nil, fmt.Errorf("dwtzb page is not an article (pageType %q)", pageType)
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	scope := doc.Find("#content.pages .artical, .artical").First()
	if scope.Length() == 0 {
		return nil, fmt.Errorf("dwtzb article has no article container")
	}
	content := scope.Find(".v_news_content").First()
	if content.Length() == 0 {
		return nil, fmt.Errorf("dwtzb article has no content container")
	}
	if strings.TrimSpace(content.Text()) == "" && content.Find("img, table, a").Length() == 0 {
		return nil, fmt.Errorf("dwtzb article has empty content")
	}

	if title := strings.Join(strings.Fields(scope.Find("h4").First().Text()), " "); title != "" {
		article.Title = title
	}
	if strings.TrimSpace(article.Title) == "" {
		return nil, fmt.Errorf("dwtzb article has no title")
	}
	if typ := dwtzbBreadcrumbType(doc); typ != "" {
		article.Type = typ
	}

	if pubinfo := scope.Find(".pubinfo").First(); pubinfo.Length() > 0 {
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

	// The download.jsp link is outside .v_news_content on dwtzb.  The shared
	// parser intentionally scopes ordinary attachment extraction to the body,
	// so include only this site's explicit CMS download links here.
	appendDwtzbAttachments(article, scope, baseURL)
	return article, nil
}

func appendDwtzbAttachments(article *model.Article, scope *goquery.Selection, baseURL string) {
	if article == nil || scope == nil || scope.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		lowerHref := strings.ToLower(href)
		if href == "" || !strings.Contains(lowerHref, "download.jsp") || !strings.Contains(lowerHref, "downloadattachurl") {
			return
		}
		name := strings.Join(strings.Fields(anchor.Text()), " ")
		if name == "" {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
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
	doc.Find("#content.pages .thumb a, .thumb a").Each(func(_ int, anchor *goquery.Selection) {
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

func dwtzbTotalPages(slug string) (int, error) {
	if strings.EqualFold(strings.Trim(slug, "/"), "index") {
		return 1, nil
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
	if err == nil {
		return body, nil
	}
	if strings.HasSuffix(strings.ToLower(pageURL), ".htm") {
		if fallback, fallbackErr := fetch(strings.TrimSuffix(pageURL, ".htm") + ".html"); fallbackErr == nil {
			return fallback, nil
		}
	}
	return nil, err
}

func dwtzbHasNewslist(html []byte) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	return err == nil && doc.Find("#content.pages .newslist").Length() > 0
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
