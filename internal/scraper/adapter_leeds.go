package scraper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
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

// leedsSubPages contains the article-bearing tabs confirmed in the current
// Leeds navigation, home-page modules, and their linked archive pages.
// Aggregate pages (xytz.htm/xxgs.htm) are intentionally not duplicated as
// feeds: their three child tabs below contain the same rows. Profile,
// contact, complaint, external, and anniversary microsite pages are not
// article lists.
var leedsSubPages = []string{
	"index/xyxw",
	"xytz/dzbg",
	"xytz/jwjx",
	"xytz/xszc",
	"xxgs/dzbg",
	"xxgs/jwjx",
	"xxgs/xszc",
	"index/hdyg",
	"index/zsxx",
	"index/zyxz",
	"xszc/xshd",
	"xszc/xsfc",
	"xszc/gjjl",
	"djysz/djyd",
	"djysz/jszj",
	"ztjy/xjpxsdzgtsshzysxztjy/xxdt",
	"yyzj",
}

var (
	leedsEventJSONSingleRE = regexp.MustCompile("(?s)newsListDataArray\\[[^\\]]+\\]\\s*=\\s*JSON\\.parse\\(\\s*'((\\\\.|[^'])*)'\\s*\\)")
	leedsEventJSONDoubleRE = regexp.MustCompile("(?s)newsListDataArray\\[[^\\]]+\\]\\s*=\\s*JSON\\.parse\\(\\s*\"((\\\\.|[^\"])*)\"\\s*\\)")
	leedsArticlePathRE     = regexp.MustCompile("(?i)^/info/[^/]+/\\d+\\.(?:htm|html)$")
)

// Leeds has its own article template.  In particular, the publish date is
// rendered in .m-textq1 .info .date instead of one of the metadata blocks
// understood by the generic 博达 article parser.  Keep both hooks attached to
// the site so future Leeds list changes do not have to be folded into the
// shared parser.
func init() {
	registerAdapter("leeds", SiteAdapter{
		SubPages:     leedsSubPages,
		ParseList:    parseLeedsNews,
		ParseArticle: parseLeedsArticle,
		PageURL:      reverseBodaPageURL,
		TotalPages:   leedsTotalPages,
		FetchArticle: leedsFetchArticle,
	})
}

func parseLeedsNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse leeds list html: %w", err)
	}
	if reason := leedsBlockedReason(doc); reason != "" {
		return nil, fmt.Errorf("leeds: response is blocked by WAF/verification (%s)", reason)
	}
	if leedsIsHomepage(doc) {
		items := leedsHomepageItems(doc, html, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("leeds: homepage contains no local article modules")
		}
		return items, nil
	}

	// Current Leeds archives use two deliberately different list templates.
	// Parse them by container so the dashboard/sidebars and media links cannot
	// be mistaken for article rows.  The old list_zx fixture remains supported
	// below for older generated pages.
	if root := doc.Find(".ny_news > ul").First(); root.Length() > 0 {
		items := leedsNyNewsItems(root, leedsPageType(doc), baseURL)
		if len(items) == 0 {
			if leedsExplicitEmptyList(root) {
				return []model.NewsItem{}, nil
			}
			return nil, fmt.Errorf("leeds: ny_news contains no local article rows")
		}
		return items, nil
	}
	if root := doc.Find(".nywp > ul.ul-list-d3").First(); root.Length() > 0 {
		items := leedsNywpItems(root, leedsPageType(doc), baseURL)
		if len(items) == 0 {
			if leedsExplicitEmptyList(root) {
				return []model.NewsItem{}, nil
			}
			return nil, fmt.Errorf("leeds: nywp contains no local article rows")
		}
		return items, nil
	}

	if root := doc.Find("ul.list_zx").First(); root.Length() > 0 {
		items := leedsLegacyListItems(root, leedsPageType(doc), baseURL)
		if len(items) == 0 {
			if leedsExplicitEmptyList(root) {
				return []model.NewsItem{}, nil
			}
			return nil, fmt.Errorf("leeds: list_zx contains no local article rows")
		}
		return items, nil
	}

	if leedsLooksLikeSPA(doc) {
		return nil, fmt.Errorf("leeds: response is an SPA shell without a server-rendered article list")
	}
	return nil, fmt.Errorf("leeds: response contains no recognisable article list")
}

var leedsBlockedMarkers = []string{
	"captcha",
	"access denied",
	"403 forbidden",
	"404 not found",
	"page not found",
	"cloudflare",
	"just a moment",
	"安全验证",
	"请求已被阻断",
	"请输入验证码",
	"验证码",
	"请先登录",
	"登录页面",
}

func leedsBlockedReason(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	title := strings.ToLower(strings.Join(strings.Fields(doc.Find("title").First().Text()), " "))
	probe := title
	if body := doc.Find("body").First(); body.Length() > 0 {
		clone := body.Clone()
		clone.Find("script, style, noscript").Remove()
		visible := strings.ToLower(strings.Join(strings.Fields(clone.Text()), " "))
		if len(visible) > 2048 {
			visible = visible[:2048]
		}
		probe += " " + visible
	}
	for _, marker := range leedsBlockedMarkers {
		if strings.Contains(probe, marker) {
			return marker
		}
	}
	return ""
}

func leedsLooksLikeSPA(doc *goquery.Document) bool {
	if doc == nil || doc.Find("#app, #root, #__next").Length() == 0 {
		return false
	}
	body := doc.Find("body").First()
	if body.Length() == 0 {
		return true
	}
	clone := body.Clone()
	clone.Find("script, style, noscript").Remove()
	return strings.TrimSpace(clone.Text()) == ""
}

func leedsExplicitEmptyList(root *goquery.Selection) bool {
	if root == nil || root.Length() == 0 {
		return false
	}
	text := strings.Join(strings.Fields(root.Parent().Text()), "")
	for _, marker := range []string{"暂无数据", "暂无内容", "没有相关", "没有文章", "共0条", "0条信息"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func leedsLegacyListItems(root *goquery.Selection, typ, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	root.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		title := ""
		for _, selector := range []string{".xw-ul-tt", ".news-title", "h3", "h4", "h5"} {
			title = strings.Join(strings.Fields(row.Find(selector).First().Text()), " ")
			if title != "" {
				break
			}
		}
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Text()), " ")
		}
		date, publishedAt := leedsNormalizeDate("")
		for _, selector := range []string{".time", ".date", "time", ".xw-ul-date", "em", "span[class*='time']"} {
			if candidate := strings.TrimSpace(row.Find(selector).First().Text()); candidate != "" {
				date, publishedAt = leedsNormalizeDate(candidate)
				break
			}
		}
		leedsAppendListItem(&items, seen, anchor, title, date, publishedAt, typ, baseURL)
	})
	return items
}

func leedsIsHomepage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	pageType := strings.TrimSpace(doc.Find(`meta[name="pageType"], meta[name="pagetype"]`).Last().AttrOr("content", ""))
	return pageType == "1" && doc.Find(".news_bg, .noitce_bg, .Events").Length() > 0
}

func leedsHomepageItems(doc *goquery.Document, raw []byte, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)
	add := func(row *goquery.Selection, typ string, dateOverride string) {
		if row == nil || row.Length() == 0 {
			return
		}
		anchor := leedsRowArticleAnchor(row)
		if anchor == nil || anchor.Length() == 0 {
			return
		}
		title := strings.TrimSpace(anchor.AttrOr("title", ""))
		if title == "" {
			for _, selector := range []string{"h5", "h3", ".EventsBoxLTopText"} {
				if value := strings.Join(strings.Fields(row.Find(selector).First().Text()), " "); value != "" {
					title = value
					break
				}
			}
		}
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Text()), " ")
		}
		date := dateOverride
		if date == "" {
			date = leedsRowDate(row)
		}
		date, publishedAt := leedsNormalizeDate(date)
		fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
		if !isValidSiteNewsURL(fullURL, baseURL) || !isValidNewsTitle(title) {
			return
		}
		if index, exists := seen[fullURL]; exists {
			if items[index].Date == "" && date != "" {
				items[index].Date, items[index].PublishedAt = date, publishedAt
			}
			if items[index].Type == "" {
				items[index].Type = typ
			}
			return
		}
		seen[fullURL] = len(items)
		items = append(items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: typ, ParsedAt: time.Now()})
	}

	doc.Find(".news_bg .news_box1 li").Each(func(_ int, row *goquery.Selection) { add(row, "学院新闻", "") })
	doc.Find("#NoitceCenterTobox .swiper-slide").Each(func(index int, slide *goquery.Selection) {
		typ := leedsNoticeType(slide, index)
		slide.Find("dl").Each(func(_ int, row *goquery.Selection) { add(row, typ, "") })
	})
	// Event dates are absent from the visible HTML but are present in the
	// source's JSON model.  Use only that model, never the event body text.
	eventDates := leedsHomepageEventDates(raw, baseURL)
	doc.Find(".EventsBoxLTop").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		date := ""
		if anchor.Length() > 0 {
			date = eventDates[cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))]
		}
		add(row, "活动预告", date)
	})
	return items
}

// leedsRowArticleAnchor picks the anchor that carries the row's article
// link. Current home-page rows lead with a textless picture-card anchor;
// choosing it by DOM order alone would discard the row because no title can
// be recovered from it.
func leedsRowArticleAnchor(row *goquery.Selection) *goquery.Selection {
	if row == nil || row.Length() == 0 {
		return nil
	}
	anchors := row.Find("a[href*='info/']")
	if anchors.Length() == 0 {
		return nil
	}
	chosen := anchors.First()
	anchors.Each(func(_ int, candidate *goquery.Selection) {
		if chosen != candidate && leedsAnchorLabel(chosen) == "" && leedsAnchorLabel(candidate) != "" {
			chosen = candidate
		}
	})
	return chosen
}

func leedsAnchorLabel(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if title := strings.TrimSpace(anchor.AttrOr("title", "")); title != "" {
		return title
	}
	return strings.Join(strings.Fields(anchor.Text()), " ")
}

func leedsRowDate(row *goquery.Selection) string {
	if row == nil || row.Length() == 0 {
		return ""
	}
	for _, selector := range []string{".date", ".time", "time", "i", ".txt > span", ".xw-ul-date", "span[class*='date']", "span[class*='time']"} {
		if value := strings.TrimSpace(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func leedsNoticeType(slide *goquery.Selection, index int) string {
	if slide != nil && slide.Length() > 0 {
		heading := slide.Closest(".notice-section, .notice-box, .notice-item, .swiper-container, .noitce_box").First()
		if heading.Length() == 0 {
			heading = slide.Parent()
		}
		for _, selector := range []string{"h1", "h2", "h3", "h4", ".title", ".tit", ".name"} {
			candidate := strings.Join(strings.Fields(heading.Find(selector).First().Text()), " ")
			switch {
			case strings.Contains(candidate, "公示"):
				return "信息公示"
			case strings.Contains(candidate, "通知"):
				return "学院通知"
			}
		}
	}
	if index == 0 {
		return "学院通知"
	}
	return "信息公示"
}

func leedsHomepageEventDates(raw []byte, baseURL string) map[string]string {
	result := make(map[string]string)
	matches := append(
		leedsEventJSONSingleRE.FindAllSubmatch(raw, -1),
		leedsEventJSONDoubleRE.FindAllSubmatch(raw, -1)...,
	)
	for _, match := range matches {
		var records []struct {
			URL       string `json:"url"`
			ShowDate  string `json:"showDate"`
			YearMonth string `json:"showDate1"`
			Day       string `json:"showDate2"`
		}
		jsonText := leedsDecodeJSLiteral(string(match[1]))
		if err := json.Unmarshal([]byte(jsonText), &records); err != nil {
			continue
		}
		for _, record := range records {
			date := record.ShowDate
			if date == "" && record.YearMonth != "" && record.Day != "" {
				date = record.YearMonth + "." + record.Day
			}
			if normalized, _ := leedsNormalizeDate(date); normalized != "" {
				result[cleanURL(resolveURL(baseURL, record.URL))] = normalized
			}
		}
	}
	return result
}

func leedsDecodeJSLiteral(value string) string {
	value = html.UnescapeString(value)
	var decoded strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != 92 || i+1 >= len(value) {
			decoded.WriteByte(value[i])
			continue
		}
		i++
		switch value[i] {
		case 92, 39, 34, 47:
			decoded.WriteByte(value[i])
		case 'n':
			decoded.WriteByte(10)
		case 'r':
			decoded.WriteByte(13)
		case 't':
			decoded.WriteByte(9)
		case 'u':
			if i+4 < len(value) {
				if code, err := strconv.ParseUint(value[i+1:i+5], 16, 16); err == nil {
					decoded.WriteRune(rune(code))
					i += 4
					continue
				}
			}
			decoded.WriteByte('u')
		default:
			decoded.WriteByte(value[i])
		}
	}
	return decoded.String()
}

func leedsNyNewsItems(root *goquery.Selection, typ, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	root.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		title := strings.Join(strings.Fields(row.Find(".txt h5").First().Text()), " ")
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Text()), " ")
		}
		date, publishedAt := leedsNormalizeDate(leedsRowDate(row))
		leedsAppendListItem(&items, seen, anchor, title, date, publishedAt, typ, baseURL)
	})
	return items
}

func leedsNywpItems(root *goquery.Selection, typ, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	root.ChildrenFiltered("li").Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a.con[href*='info/'], a[href*='info/']").First()
		if anchor.Length() == 0 {
			return
		}
		title := strings.Join(strings.Fields(row.Find(".g-tit-d").First().Text()), " ")
		if title == "" {
			title = strings.Join(strings.Fields(anchor.Text()), " ")
		}
		date, publishedAt := leedsDateBox(row.Find(".g-info-d").First())
		if date == "" {
			date, publishedAt = leedsNormalizeDate(leedsRowDate(row))
		}
		leedsAppendListItem(&items, seen, anchor, title, date, publishedAt, typ, baseURL)
	})
	return items
}

func leedsDateBox(box *goquery.Selection) (string, string) {
	if box == nil || box.Length() == 0 {
		return "", ""
	}
	day := strings.TrimSpace(box.Find("em").First().Text())
	month := strings.TrimSpace(box.Clone().Find("em").Remove().End().Text())
	if day != "" && month != "" {
		if date, publishedAt := leedsNormalizeDate(month + "." + day); date != "" {
			return date, publishedAt
		}
	}
	return leedsNormalizeDate(box.Text())
}

func leedsAppendListItem(items *[]model.NewsItem, seen map[string]bool, anchor *goquery.Selection, title, date, publishedAt, typ, baseURL string) {
	fullURL := cleanURL(resolveURL(baseURL, anchor.AttrOr("href", "")))
	if seen[fullURL] || !isValidSiteNewsURL(fullURL, baseURL) || !isValidNewsTitle(title) {
		return
	}
	seen[fullURL] = true
	*items = append(*items, model.NewsItem{Title: title, URL: fullURL, Date: date, PublishedAt: publishedAt, Type: strings.TrimSpace(typ), ParsedAt: time.Now()})
}

func leedsNormalizeDate(raw string) (string, string) {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" {
		return "", ""
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	return "", ""
}

func leedsTotalPages(slug string) (int, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" || slug == "index" {
		// The homepage is a single preview page: reverseBodaPageURL maps
		// page 1 to /index.htm and parseLeedsNews parses its news modules.
		return 1, nil
	}
	if !leedsKnownFeed(slug) {
		return 0, fmt.Errorf("leeds: %q is not an article-list route", slug)
	}
	baseURL := leedsBaseURL()
	var body []byte
	var err error
	for _, extension := range []string{".htm", ".html"} {
		body, err = fetch(fmt.Sprintf("%s/%s%s", strings.TrimRight(baseURL, "/"), slug, extension))
		if err == nil {
			break
		}
	}
	if err != nil {
		return 0, fmt.Errorf("leeds: fetch first page for %s: %w", slug, err)
	}
	return leedsTotalPagesFromHTML(body, slug)
}

func leedsBaseURL() string {
	if site := AllSites["leeds"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		return strings.TrimRight(site.BaseURL, "/")
	}
	return "http://leeds.swjtu.edu.cn"
}

func leedsKnownFeed(slug string) bool {
	for _, known := range leedsSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func leedsTotalPagesFromHTML(raw []byte, slug string) (int, error) {
	if slug = strings.Trim(strings.TrimSpace(slug), "/"); slug == "" || slug == "index" {
		return 1, nil
	}
	if !leedsKnownFeed(slug) {
		return 0, fmt.Errorf("leeds: %q is not an article-list route", slug)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("leeds: parse pagination for %s: %w", slug, err)
	}
	if reason := leedsBlockedReason(doc); reason != "" {
		return 0, fmt.Errorf("leeds: %s pagination response is blocked by WAF/verification (%s)", slug, reason)
	}
	if leedsIsHomepage(doc) {
		return 0, fmt.Errorf("leeds: %s response is not an article list", slug)
	}
	listRoot := doc.Find(".ny_news > ul, .nywp > ul.ul-list-d3, ul.list_zx").First()
	if listRoot.Length() == 0 {
		return 0, fmt.Errorf("leeds: %s response is not an article list", slug)
	}
	if leedsExplicitEmptyList(listRoot) {
		return 1, nil
	}
	if leedsLooksLikeSPA(doc) {
		return 0, fmt.Errorf("leeds: %s pagination response is an SPA shell", slug)
	}
	if total := parseTotalPages(raw); total > 0 {
		return total, nil
	}
	return 1, nil
}

func parseLeedsArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse leeds article html: %w", err)
	}
	if reason := leedsBlockedReason(doc); reason != "" {
		return nil, fmt.Errorf("leeds: article response is blocked by WAF/verification (%s)", reason)
	}
	scope := doc.Find(".m-textq1").First()
	if scope.Length() == 0 {
		return nil, fmt.Errorf("leeds: response is not a recognisable article page")
	}
	title := leedsArticleTitle(scope)
	if title == "" {
		return nil, fmt.Errorf("leeds: article has no fixed title node")
	}
	content := leedsArticleContent(scope)
	if content.Length() == 0 {
		return nil, fmt.Errorf("leeds: article has no fixed content container")
	}

	date, publishedAt := leedsArticleDate(doc)
	article := &model.Article{
		Title:       title,
		Source:      leedsArticleSource(scope),
		Date:        date,
		PublishedAt: publishedAt,
		Type:        leedsArticleType(doc, title),
		Content:     extractBodaContentText(content),
	}
	leedsCollectArticleResources(article, content, baseURL)
	leedsCollectOutsideAttachments(article, scope, baseURL)
	leedsArticleNavigation(article, doc, baseURL)
	return article, nil
}

func leedsArticleTitle(scope *goquery.Selection) string {
	for _, selector := range []string{"h1", "h1.news_tit", ".news-title", ".article-title", ".detail-title"} {
		if title := strings.Join(strings.Fields(scope.Find(selector).First().Text()), " "); title != "" {
			return title
		}
	}
	return ""
}

func leedsArticleContent(scope *goquery.Selection) *goquery.Selection {
	for _, selector := range []string{
		".txt #vsb_content_2 .v_news_content",
		".txt #vsb_content_501 .v_news_content",
		".txt .v_news_content",
		".txt [id^='vsb_content'] .v_news_content",
		".v_news_content",
	} {
		if content := scope.Find(selector).First(); content.Length() > 0 {
			return content
		}
	}
	return scope.Find(".txt [id^='vsb_content']").First()
}

func leedsArticleSource(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	var source string
	scope.Find(".info .source, .info [class*='source'], .info span").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		text := strings.Join(strings.Fields(sel.Text()), " ")
		for _, prefix := range []string{"来源：", "来源:", "信息来源：", "信息来源:"} {
			if strings.HasPrefix(text, prefix) {
				value := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "信息来源："), "信息来源:"))
				value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "来源："), "来源:"))
				for _, marker := range []string{"编辑：", "编辑:", "审核人：", "审核人:"} {
					if index := strings.Index(value, marker); index >= 0 {
						value = strings.TrimSpace(value[:index])
					}
				}
				source = value
				return false
			}
		}
		return true
	})
	return source
}

func leedsArticleType(doc *goquery.Document, title string) string {
	if doc == nil {
		return ""
	}
	for _, selector := range []string{".list-banner-tt", ".loaction a", ".location a", ".breadcrumb a", ".breadcrumbs a"} {
		selection := doc.Find(selector)
		for i := selection.Length() - 1; i >= 0; i-- {
			candidate := leedsCleanType(strings.Join(strings.Fields(selection.Eq(i).Text()), " "))
			if candidate == "" || candidate == "首页" || candidate == "正文" || candidate == title {
				continue
			}
			return candidate
		}
	}
	if typ := strings.Join(strings.Fields(doc.Find("meta[name='pageTitle'], meta[name='pagetitle']").Last().AttrOr("content", "")), " "); typ != "" && typ != "首页" && typ != title {
		return typ
	}
	return ""
}

func leedsCollectArticleResources(article *model.Article, content *goquery.Selection, baseURL string) {
	if article == nil || content == nil || content.Length() == 0 {
		return
	}
	contentClone := content.Clone()
	imageSeen := make(map[string]bool)
	attachmentSeen := make(map[string]bool)
	contentClone.Find("img").Each(func(_ int, sel *goquery.Selection) {
		var raw string
		for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "orisrc"} {
			if value := strings.TrimSpace(sel.AttrOr(attr, "")); value != "" {
				raw = value
				break
			}
		}
		resolved := leedsResourceURL(baseURL, raw)
		if resolved == "" || imageSeen[resolved] {
			return
		}
		imageSeen[resolved] = true
		article.Images = append(article.Images, resolved)
		sel.SetAttr("src", resolved)
	})
	contentClone.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if href == "" || bodaIsPseudoResourceHref(href) || !bodaLooksLikeAttachmentLink(sel, href) {
			return
		}
		resolved := leedsResourceURL(baseURL, href)
		if resolved == "" || attachmentSeen[resolved] {
			return
		}
		name := strings.TrimSpace(sel.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(sel.AttrOr("name", ""))
		}
		if name == "" {
			name = strings.Join(strings.Fields(sel.Text()), " ")
		}
		if name == "" {
			name = href
		}
		attachmentSeen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		sel.SetAttr("href", resolved)
	})
	if rendered, err := contentClone.Html(); err == nil {
		article.ContentHTML = rendered
	}
}

func leedsCollectOutsideAttachments(article *model.Article, scope *goquery.Selection, baseURL string) {
	if article == nil || scope == nil || scope.Length() == 0 {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	scope.Find(".attachment a[href], .attachments a[href], .news-attachment a[href], .download a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" || bodaIsPseudoResourceHref(href) || !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := leedsResourceURL(baseURL, href)
		if resolved == "" || seen[resolved] {
			return
		}
		name := strings.TrimSpace(link.AttrOr("download", ""))
		if name == "" {
			name = strings.Join(strings.Fields(link.Text()), " ")
		}
		if name == "" {
			name = href
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func leedsArticleNavigation(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	doc.Find(".news_sxp li a[href]").Each(func(_ int, sel *goquery.Selection) {
		text := strings.Join(strings.Fields(sel.Text()), " ")
		href := cleanURL(resolveURL(baseURL, sel.AttrOr("href", "")))
		if href == "" {
			return
		}
		if strings.HasPrefix(text, "上一篇") {
			article.PrevURL, article.PrevTitle = href, text
		} else if strings.HasPrefix(text, "下一篇") {
			article.NextURL, article.NextTitle = href, text
		}
	})
}

func leedsArticleDate(doc *goquery.Document) (string, string) {
	for _, selector := range []string{
		".m-textq1 > .info .date",
		".m-textq1 .info .date",
	} {
		date := strings.TrimSpace(doc.Find(selector).First().Text())
		if date == "" {
			continue
		}
		if publishedAt := normalizePublishedAt(date); publishedAt != "" {
			return publishedAt[:10], publishedAt
		}
		if match := bareDateRe.FindStringSubmatch(date); match != nil {
			if normalized := normalizeDateParts(match[1], match[2], match[3]); normalized != "" {
				return normalized, normalized
			}
		}
	}
	return "", ""
}

func leedsPageType(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	if typ := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"], meta[name="pagetitle"]`).Last().AttrOr("content", "")), " "); typ != "" && typ != "首页" {
		return typ
	}
	if typ := bodaArticleSectionType(doc, ""); typ != "" {
		return typ
	}
	for _, selector := range []string{".list-title", ".list-head", ".m-textq1 > h1", ".m-textq1 > h2", ".m-textq1 > h3"} {
		candidate := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if candidate != "" && candidate != "首页" {
			return candidate
		}
	}
	return ""
}

func leedsItemType(doc *goquery.Document, itemURL string) string {
	anchor := leedsFindItemAnchor(doc, itemURL)
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
	if row.Length() == 0 {
		row = anchor.Parent()
	}
	for _, selector := range []string{".type", ".category", ".column", ".lanmu", ".source", ".tag"} {
		if value := leedsCleanType(row.Find(selector).First().Text()); value != "" {
			return value
		}
	}
	return ""
}

func leedsFindItemAnchor(doc *goquery.Document, itemURL string) *goquery.Selection {
	var found *goquery.Selection
	doc.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		for _, base := range []string{"http://leeds.swjtu.edu.cn", "https://leeds.swjtu.edu.cn"} {
			resolved := cleanURL(resolveURL(base, anchor.AttrOr("href", "")))
			if resolved == itemURL {
				found = anchor
				return false
			}
		}
		return true
	})
	return found
}

func leedsItemDate(doc *goquery.Document, itemURL, fallback string) (string, string) {
	raw := ""
	if anchor := leedsFindItemAnchor(doc, itemURL); anchor != nil && anchor.Length() > 0 {
		row := anchor.Closest("li, dd, .item, .news-item, .list-item").First()
		if row.Length() == 0 {
			row = anchor.Parent()
		}
		for _, selector := range []string{".date", ".time", "time", ".xw-ul-date", "em"} {
			raw = strings.TrimSpace(row.Find(selector).First().Text())
			if raw != "" {
				break
			}
		}
	}
	if raw == "" {
		raw = fallback
	}
	if publishedAt := normalizePublishedAt(raw); publishedAt != "" {
		return publishedAt[:10], publishedAt
	}
	return strings.TrimSpace(raw), ""
}

func leedsCleanType(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "[]【】〖〗")
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "栏目："), "栏目:"))
}

func leedsFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := leedsBaseURL()
	fullURL, ok := leedsArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("leeds: invalid or external article URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, fmt.Errorf("leeds: fetch article %s: %w", fullURL, err)
	}
	article, err := parseLeedsArticle(body, baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("leeds: parse article %s: %w", fullURL, err)
	}
	article.Images = leedsVerifyImages(article.Images, baseURL)
	article.Attachments = leedsVerifyAttachments(article.Attachments, baseURL)
	article.ContentHTML = leedsSanitizeContentHTML(article.ContentHTML, baseURL, article.Images, article.Attachments)
	return article, body, nil
}

func leedsArticleURL(baseURL, raw string) (string, bool) {
	resolved := cleanURL(resolveURL(baseURL, strings.TrimSpace(raw)))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" || !leedsArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return resolved, true
}

func leedsVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := leedsResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := leedsFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func leedsVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := leedsResourceURL(baseURL, attachment.URL)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, _, err := leedsFetchResource(resolved, baseURL, false)
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

func leedsFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := leedsResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("leeds: refusing invalid resource %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("leeds: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Referer", baseURL)
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		scheme := strings.ToLower(next.URL.Scheme)
		if scheme != "http" && scheme != "https" {
			return fmt.Errorf("leeds: refusing non-HTTP resource redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("leeds: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("leeds: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("leeds: read resource %s: %w", resolved, err)
	}
	if len(body) > 32<<20 || !leedsResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("leeds: resource %s returned an invalid Content-Type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func leedsResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	path := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"captcha", "verifycode", "verification", "createimage", "getcode",
		"placeholder", "transparent", "spacer", "blank.gif", "loading.gif",
		"pixel.gif", "access-denied", "waf",
	} {
		if strings.Contains(path, marker) {
			return ""
		}
	}
	return resolved
}

func leedsResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
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
	textProbe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(probe))), "\ufeff")
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "附件下载",
		"请输入验证码", "验证码下载附件", "请先登录", "请登录后",
		"登录页面", "页面不存在", "404 not found", "access denied",
		"cloudflare", "just a moment", "安全验证", "请求已被阻断",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	magic := leedsResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && leedsIsImageMagic(magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "zip") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || strings.Contains(mediaType, "msword") || strings.Contains(mediaType, "ms-excel"):
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values":
		return true
	default:
		return magic != ""
	}
}

func leedsIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff", "svg":
		return true
	default:
		return false
	}
}

func leedsResourceMagic(body []byte) string {
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
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	case bytes.Contains(bytes.ToLower(body[:leedsMinInt(len(body), 4096)]), []byte("<svg")):
		return "svg"
	default:
		return ""
	}
}

func leedsMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func leedsSanitizeContentHTML(raw, baseURL string, images []string, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	imageSet := make(map[string]bool, len(images))
	for _, image := range images {
		imageSet[leedsResourceURL(baseURL, image)] = true
	}
	attachmentSet := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		attachmentSet[leedsResourceURL(baseURL, attachment.URL)] = true
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"leeds-content-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	root := doc.Find("#leeds-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		rawURL := strings.TrimSpace(image.AttrOr("src", ""))
		resolved := leedsResourceURL(baseURL, rawURL)
		if resolved == "" || !imageSet[resolved] {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
	})
	root.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" {
			return
		}
		if bodaIsPseudoResourceHref(href) {
			if !strings.HasPrefix(href, "#") {
				link.Remove()
			}
			return
		}
		if !bodaLooksLikeAttachmentLink(link, href) {
			return
		}
		resolved := leedsResourceURL(baseURL, href)
		if resolved == "" || !attachmentSet[resolved] {
			link.Remove()
			return
		}
		link.SetAttr("href", resolved)
	})
	rendered, err := root.Html()
	if err != nil {
		return ""
	}
	return rendered
}
