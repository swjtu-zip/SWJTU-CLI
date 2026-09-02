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

// chemSubPages contains the article feeds exposed by the site's visible
// sections and by the secondary navigation on those section pages. The
// profile/static pages are deliberately not included: they are real pages,
// but they are not news feeds. A few of the linked feeds currently render
// another feed's contents; keeping their URLs here preserves their actual
// reachability without pretending that the source is correctly partitioned.
var chemSubPages = []string{
	"index/xyxw",
	"index/xytz",
	"index/jwgg",
	"index/jwgg/yjsgg",
	"index/ttxw",
	"index/kxyj",
	"rcpy",
	"rcpy/yjsds",
	"rcpy/yjspygg",
	"rcpy/wjzlxz",
	"kxyj",
	"kxyj/kytz",
	"kxyj/kytd",
	"kxyj/kydt",
	"kxyj/xshd",
	"kxyj/sypt",
	"dqgz",
	"dqgz/djc",
	"dqgz/djdt",
	"dqgz/zzjsydyfz",
	"dqgz/jwgz",
	"dqgz/ghgz",
	"xsgz",
	"xsgz/xgtz",
	"xsgz/xgdt",
	"xsgz/gsgg",
	"aqgz",
	"aqgz/aqxz",
	"aqgz/gzzd",
	"aqgz/xgzy",
	"rczp",
	"xygz",
	"xygz/xyfc",
	"xygz/bysfc",
	"hzjl",
	"hzjl/gjhhz",
	"hzjl/xqhz",
	"zsjy",
	"zsjy/zsgz",
	"zsjy/jygz",
}

var (
	chemInfoArticlePathRe = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	chemDateRe            = regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})\s*[-/.月]\s*([0-9]{1,2})`)
	chemSplitDateRe       = regexp.MustCompile(`^\s*([0-9]{1,2})\s*(20[0-9]{2})\s*[-/.]\s*([0-9]{1,2})\s*$`)
)

func init() {
	registerAdapter("chem", SiteAdapter{
		SubPages:     chemSubPages,
		ParseList:    parseAdapter_chemList,
		ParseArticle: parseAdapter_chemArticle,
		PageURL:      chemPageURL,
	})
}

// chemPageURL maps the logical page number (1=newest) to the reverse-numbered
// generated files used by chem.swjtu.edu.cn. For example, a 31-page feed
// advertises page 2 as /feed/30.htm and page 31 as /feed/1.htm.
func chemPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}

	filePage := page
	if totalPages > 1 {
		filePage = totalPages - page + 1
	}
	if filePage < 1 {
		filePage = 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

func parseAdapter_chemList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse chem list: %w", err)
	}

	// Every real chem list page uses this exact container. It is important to
	// return an empty result when the container is present but has no rows (for
	// example the currently empty index/kxyj feed), rather than scanning the
	// page shell and producing navigation links.
	list := doc.Find("main .news-list-wrap ul.list")
	if list.Length() > 0 {
		items := make([]model.NewsItem, 0)
		seen := make(map[string]bool)
		defaultType := chemBreadcrumbType(doc)
		list.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
			addChemListItem(&items, seen, row, defaultType, baseURL)
		})
		return items, nil
	}

	// The home page is an aggregation of several real lists. Restricting the
	// pass to article links below <main> excludes the header, banner, footer,
	// external links, and the page's non-article navigation.
	if chemHasHomeLayout(doc) {
		items := parseChemHome(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("chem home page contains no article entries")
		}
		return items, nil
	}

	return nil, fmt.Errorf("chem page is not a supported article list")
}

func addChemListItem(items *[]model.NewsItem, seen map[string]bool, row *goquery.Selection, defaultType, baseURL string) {
	anchor := row.Find("a[href]").First()
	if anchor.Length() == 0 || isInNavArea(anchor) {
		return
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL, ok := chemArticleURL(baseURL, href)
	if !ok || seen[fullURL] {
		return
	}

	title := chemListTitle(row, anchor)
	if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 100 {
		return
	}

	date, publishedAt := chemDateFromScope(row.Find(".info").First())
	if date == "" {
		for _, selector := range []string{"time", ".date", ".time"} {
			date, publishedAt = chemDateFromScope(row.Find(selector).First())
			if date != "" {
				break
			}
		}
	}

	item := model.NewsItem{
		Title:    title,
		URL:      fullURL,
		Date:     date,
		Type:     chemListType(row, defaultType),
		ParsedAt: time.Now(),
	}
	if chemHasClock(publishedAt) {
		item.PublishedAt = publishedAt
	}
	seen[fullURL] = true
	*items = append(*items, item)
}

func chemListTitle(row, anchor *goquery.Selection) string {
	for _, selector := range []string{"p.toe", ".news-title", "h3", "h4"} {
		if title := strings.TrimSpace(row.Find(selector).First().Text()); title != "" {
			return cleanTitle(title)
		}
	}
	if title := strings.TrimSpace(anchor.AttrOr("title", "")); title != "" {
		return cleanTitle(title)
	}
	return cleanTitle(strings.TrimSpace(anchor.Text()))
}

func chemListType(row *goquery.Selection, defaultType string) string {
	label := strings.TrimSpace(row.Find("a > span").First().Text())
	if label == "" {
		label = strings.TrimSpace(row.Find("a span").First().Text())
	}
	label = strings.TrimSpace(strings.Trim(label, "[]【】"))
	if label != "" {
		return label
	}
	return defaultType
}

func chemHasHomeLayout(doc *goquery.Document) bool {
	return doc.Find("main section.news-list, main .news-section, main .news-box, main .notice-box").Length() > 0
}

func parseChemHome(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	doc.Find("main a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		fullURL, ok := chemArticleURL(baseURL, href)
		if !ok {
			return
		}
		title := chemHomeTitle(anchor)
		if title == "" || !isValidNewsTitle(title) || len([]rune(title)) > 100 {
			return
		}
		date, publishedAt := chemHomeDate(anchor)
		item := model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			Type:     chemHomeType(anchor),
			ParsedAt: time.Now(),
		}
		if chemHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}
		addChemHomeItem(&items, seen, item)
	})
	return items
}

func chemHomeTitle(anchor *goquery.Selection) string {
	for _, selector := range []string{"h4.toe0", "p.toe", "p.toe0", "p.toe2", "h3", "h4"} {
		if title := strings.TrimSpace(anchor.Find(selector).First().Text()); title != "" {
			return cleanTitle(title)
		}
	}
	return cleanTitle(strings.TrimSpace(anchor.AttrOr("title", "")))
}

func chemHomeType(anchor *goquery.Selection) string {
	for _, selector := range []string{".news-box", ".notice-box", "section.news-list", "section"} {
		container := anchor.Closest(selector)
		if container.Length() == 0 {
			continue
		}
		for _, heading := range []string{".global-hd h3", ".hd h3"} {
			if value := strings.TrimSpace(container.Find(heading).First().Text()); value != "" {
				return value
			}
		}
	}
	return ""
}

func chemHomeDate(anchor *goquery.Selection) (string, string) {
	if scope := anchor.Find(".time").First(); scope.Length() > 0 {
		if date, published := chemDateFromScope(scope); date != "" {
			return date, published
		}
	}
	var date, published string
	anchor.Find("time, span, [class*='date']").EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
		date, published = chemDateFromScope(candidate)
		return date == ""
	})
	return date, published
}

func addChemHomeItem(items *[]model.NewsItem, seen map[string]int, item model.NewsItem) {
	if index, ok := seen[item.URL]; ok {
		current := &(*items)[index]
		// The featured card precedes the dated list row on the live home page.
		// Upgrade that first occurrence with the authoritative date and list
		// category while retaining the page's display order.
		if current.Date == "" && item.Date != "" {
			current.Date = item.Date
			current.PublishedAt = item.PublishedAt
		}
		if (current.Type == "" || current.Type == "学院要闻") && item.Type != "" {
			current.Type = item.Type
		}
		return
	}
	seen[item.URL] = len(*items)
	*items = append(*items, item)
}

func chemDateFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}
	day := strings.TrimSpace(scope.Find(".t1").First().Text())
	yearMonth := strings.TrimSpace(scope.Find(".t2").First().Text())
	if day != "" && yearMonth != "" {
		if match := regexp.MustCompile(`(20[0-9]{2})\s*[-/.年]\s*([0-9]{1,2})`).FindStringSubmatch(yearMonth); match != nil {
			if date := normalizeDateParts(match[1], match[2], day); date != "" {
				return date, date
			}
		}
	}
	return chemDateValue(flattenedText(scope))
}

func chemDateValue(value string) (string, string) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "", ""
	}
	if normalized := normalizePublishedAt(value); normalized != "" {
		return normalized[:10], normalized
	}
	if match := chemDateRe.FindStringSubmatch(value); match != nil {
		if date := normalizeDateParts(match[1], match[2], match[3]); date != "" {
			return date, date
		}
	}
	if match := chemSplitDateRe.FindStringSubmatch(value); match != nil {
		if date := normalizeDateParts(match[2], match[3], match[1]); date != "" {
			return date, date
		}
	}
	return "", ""
}

func chemHasClock(value string) bool {
	return strings.Contains(value, ":")
}

func chemArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, strings.TrimSpace(href)))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !isValidNewsURL(fullURL) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}

	if chemInfoArticlePathRe.MatchString(parsed.Path) {
		parsed.Fragment = ""
		return parsed.String(), true
	}
	// The hidden 头条 feed uses a legacy content.jsp URL. It is accepted as a
	// source list entry, while parseAdapter_chemArticle still rejects the
	// current live response when that URL returns the site's error page.
	query := parsed.Query()
	if strings.EqualFold(parsed.Path, "/content.jsp") &&
		strings.Contains(query.Get("urltype"), "NewsContentUrl") && query.Get("wbnewsid") != "" {
		parsed.Fragment = ""
		return parsed.String(), true
	}
	return "", false
}

func parseAdapter_chemArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse chem article: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse chem article html: %w", err)
	}
	if chemLooksLikeErrorPage(doc) {
		return nil, fmt.Errorf("chem article is an error or verification page")
	}

	if title := strings.TrimSpace(doc.Find(".news-list-wrap .title h3, .news-list-wrap .title h1").First().Text()); title != "" {
		article.Title = title
	}
	info := doc.Find(".news-list-wrap .title .info").First()
	info.Find("span").Each(func(_ int, field *goquery.Selection) {
		value := strings.TrimSpace(field.Text())
		switch {
		case strings.HasPrefix(value, "作者：") || strings.HasPrefix(value, "作者:"):
			article.Author = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "作者："), "作者:"))
		case strings.HasPrefix(value, "日期：") || strings.HasPrefix(value, "日期:"):
			date, published := chemDateValue(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "日期："), "日期:")))
			if date != "" {
				article.Date = date
				article.PublishedAt = published
			}
		case strings.HasPrefix(value, "来源：") || strings.HasPrefix(value, "来源:"):
			article.Source = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "来源："), "来源:"))
		case strings.HasPrefix(value, "编辑：") || strings.HasPrefix(value, "编辑:"):
			article.Editor = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "编辑："), "编辑:"))
		}
	})
	if article.Type == "" {
		article.Type = chemBreadcrumbType(doc)
	}

	appendChemOutsideImages(article, doc, baseURL)
	appendChemAttachments(article, doc, baseURL)
	if strings.TrimSpace(article.Title) == "" {
		return nil, fmt.Errorf("chem article has no title")
	}
	if strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0 {
		return nil, fmt.Errorf("chem article has no content")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func chemBreadcrumbType(doc *goquery.Document) string {
	crumb := doc.Find(".news-list-wrap .breadcrumb").First()
	if crumb.Length() > 0 {
		value := ""
		crumb.Find("a").Each(func(_ int, link *goquery.Selection) {
			text := strings.TrimSpace(link.Text())
			if text != "" && text != "首页" {
				value = text
			}
		})
		if value != "" {
			return value
		}
	}
	return strings.TrimSpace(doc.Find(".news-list-wrap > .hd h3").First().Text())
}

func appendChemOutsideImages(article *model.Article, doc *goquery.Document, baseURL string) {
	seen := make(map[string]bool, len(article.Images))
	for _, image := range article.Images {
		seen[image] = true
	}
	doc.Find(".news-list-wrap .detail img").Each(func(_ int, image *goquery.Selection) {
		if image.Closest(".v_news_content").Length() > 0 {
			return
		}
		src := strings.TrimSpace(image.AttrOr("src", ""))
		if src == "" {
			return
		}
		resolved := resolveURL(baseURL, src)
		if !seen[resolved] {
			seen[resolved] = true
			article.Images = append(article.Images, resolved)
		}
	})
}

func appendChemAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	// The CMS emits the attachment <UL> after the detail block and, on some
	// pages, malformed <p><UL> markup changes its apparent parent. Scan all
	// links, while retaining the strict download.jsp URL check so navigation
	// cannot leak in.
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		lower := strings.ToLower(href)
		if !strings.Contains(lower, "download.jsp") && !strings.Contains(lower, "downloadattachurl") {
			return
		}
		name := strings.TrimSpace(link.Text())
		if name == "" {
			return
		}
		resolved := resolveURL(baseURL, href)
		if seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func chemLooksLikeErrorPage(doc *goquery.Document) bool {
	title := strings.TrimSpace(doc.Find("title").First().Text())
	if strings.Contains(title, "系统提示") {
		return true
	}
	body := strings.Join(strings.Fields(doc.Find("body").Text()), " ")
	return strings.Contains(body, "页面未找到") || strings.Contains(body, "请输入验证码下载附件")
}
