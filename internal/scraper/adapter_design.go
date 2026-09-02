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

// designSubPages contains the article-list entry points exposed by the live
// navigation. Profile pages, the empty research landing page, download pages,
// and the online-exhibition gallery are intentionally not mixed into the news
// feeds below. xszt is a resource/gallery list, not a normal news column.
var designSubPages = []string{
	"xydt",
	"xshd",
	"xshd/jz",
	"xshd/yt",
	"xshd/zl",
	"xshd/hd",
	"tzgg",
	"tzgg/tzgg",
	"tzgg/bkjx",
	"tzgg/yjsjx",
	"jyjx",
	"jyjx/jxcg",
	"jyjx/bsxwd",
	"jyjx/sjjd",
	"kysj/kycg",
	"djgz",
	"djgz/zzjs",
	"djgz/xwgg",
	"djgz/xygh",
	"djgz/jwgz",
	"djgz/zcwj",
	"xsgz",
	"xsgz/xgdw",
	"xsgz/tzgg",
	"xsgz/sjzm",
	"xsgz/jyxx",
	"hzjl",
	"xyfc",
}

var (
	designArticlePathRE   = regexp.MustCompile(`(?i)^/info/[^/]+/\d+\.html?$`)
	designAttachmentExtRE = regexp.MustCompile(`(?i)\.(?:docx?|xlsx?|pptx?|pdf|zip|rar|7z|txt|csv)(?:$|[?#])`)
)

func init() {
	registerAdapter("design", SiteAdapter{
		SubPages:     designSubPages,
		ParseList:    parseAdapter_designList,
		ParseArticle: parseAdapter_designArticle,
		PageURL:      designPageURL,
	})
}

// designPageURL maps the browser's logical page number to the reverse-numbered
// static file emitted by the site. For example, page 2 of a 25-page list is
// /24.htm and the last page is /1.htm.
func designPageURL(baseURL, slug string, page, totalPages int, extension string) string {
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

func parseAdapter_designList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse design list: %w", err)
	}

	if designIsHomepage(doc) {
		items := designHomepageItems(doc, baseURL)
		if len(items) == 0 {
			return nil, fmt.Errorf("design: homepage has no article modules")
		}
		return items, nil
	}

	list := designListContainer(doc)
	if list.Length() == 0 {
		return nil, fmt.Errorf("design: response is not a homepage or article list")
	}

	items := make([]model.NewsItem, 0)
	designCollectRows(list, designListType(doc), baseURL, &items, make(map[string]bool))
	// A present list container with no rows is a reachable empty column (for
	// example kysj.htm), which is different from a static page or an error shell.
	return items, nil
}

func parseAdapter_designArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse design article: %w", err)
	}
	if !designHasArticleBody(doc) {
		return nil, fmt.Errorf("design: response is not an article")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	title, typ, rawDate := designArticleHeader(doc, baseURL)
	if title != "" {
		article.Title = title
	}
	if typ != "" {
		article.Type = typ
	}
	if rawDate != "" {
		if date, publishedAt := designDateValues(rawDate); date != "" {
			article.Date = date
			if publishedAt != "" {
				article.PublishedAt = publishedAt
			}
		}
	}

	designCollectArticleAttachments(doc, baseURL, article)
	if strings.TrimSpace(article.Title) == "" {
		return nil, fmt.Errorf("design: article has no title")
	}
	normalizeArticlePublishedAt(article)
	return article, nil
}

func designIsHomepage(doc *goquery.Document) bool {
	return doc.Find(".dynamic .list, .product .content .list, .notice .content .list").Length() > 0
}

func designListContainer(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{".file .list", ".detail .list", ".alumnus .list", ".newList > .list"} {
		if list := doc.Find(selector).First(); list.Length() > 0 {
			return list
		}
	}
	return doc.Find("__design_missing_list__")
}

func designHomepageItems(doc *goquery.Document, baseURL string) []model.NewsItem {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)

	if scope := doc.Find(".dynamic .list").First(); scope.Length() > 0 {
		designCollectRows(scope, "学院动态", baseURL, &items, seen)
	}

	if product := doc.Find(".product").First(); product.Length() > 0 {
		academicTypes := []string{"讲座", "展览", "论坛", "活动"}
		product.Find(".content > .list").Each(func(i int, scope *goquery.Selection) {
			typ := "学术活动"
			if i < len(academicTypes) {
				typ = academicTypes[i]
			}
			designCollectRows(scope, typ, baseURL, &items, seen)
		})
	}

	if notice := doc.Find(".notice").First(); notice.Length() > 0 {
		noticeTypes := []string{"通知公告", "本科生通知", "研究生通知"}
		notice.Find(".content > .list").Each(func(i int, scope *goquery.Selection) {
			typ := "通知公告"
			if i < len(noticeTypes) {
				typ = noticeTypes[i]
			}
			designCollectRows(scope, typ, baseURL, &items, seen)
		})
	}

	return items
}

func designCollectRows(scope *goquery.Selection, typ, baseURL string, items *[]model.NewsItem, seen map[string]bool) {
	if scope == nil || scope.Length() == 0 {
		return
	}
	if cards := scope.Find(".item"); cards.Length() > 0 {
		cards.Each(func(_ int, row *goquery.Selection) {
			designAddRow(items, seen, row, typ, baseURL)
		})
		return
	}
	scope.Find("ul > li").Each(func(_ int, row *goquery.Selection) {
		designAddRow(items, seen, row, typ, baseURL)
	})
}

func designAddRow(items *[]model.NewsItem, seen map[string]bool, row *goquery.Selection, typ, baseURL string) {
	if row == nil || row.Length() == 0 {
		return
	}

	var articleURL string
	row.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if resolved, ok := designInternalArticleURL(baseURL, anchor.AttrOr("href", "")); ok {
			articleURL = resolved
			return false
		}
		return true
	})
	if articleURL == "" || seen[articleURL] {
		return
	}

	title := designRowTitle(row)
	if title == "" || title == "查看更多" || title == "更多" {
		return
	}
	date, publishedAt := designDateValues(designRowDate(row))
	seen[articleURL] = true
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         articleURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        typ,
		ParsedAt:    time.Now(),
	})
}

func designRowTitle(row *goquery.Selection) string {
	for _, selector := range []string{".txts", ".tits", ".text h5", "h5", "h4"} {
		if value := designCompactText(row.Find(selector).First().Text()); value != "" {
			return cleanTitle(value)
		}
	}
	if anchor := row.Find("a[href]").First(); anchor.Length() > 0 {
		if value := designCompactText(anchor.AttrOr("title", "")); value != "" {
			return cleanTitle(value)
		}
		return cleanTitle(designCompactText(anchor.Text()))
	}
	return ""
}

func designRowDate(row *goquery.Selection) string {
	for _, selector := range []string{".time", ".date", ".labs .box", ".labs p", ".text p", "time"} {
		var found string
		row.Find(selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			value := designCompactText(sel.Text())
			if date, _ := designDateValues(value); date != "" {
				found = value
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

func designListType(doc *goquery.Document) string {
	for _, selector := range []string{".file > .title", ".alumnus > .title", ".newList > .title", ".bann .title span"} {
		if value := designCompactText(doc.Find(selector).First().Text()); value != "" && value != "内容" && value != "文章内容" {
			return value
		}
	}
	return ""
}

func designArticleHeader(doc *goquery.Document, baseURL string) (title, typ, rawDate string) {
	if block := doc.Find(".detail .title").First(); block.Length() > 0 {
		title = designCompactText(block.Find("h4").First().Text())
		if title != "" {
			typ = designCompactText(block.Find("h5").First().Text())
			rawDate = designArticleDate(block)
			return cleanTitle(title), typ, rawDate
		}
	}

	if block := doc.Find(".informs .intrs .title").First(); block.Length() > 0 {
		title = designCompactText(block.Find("h5").First().Text())
		rawDate = designArticleDate(block)
		if title != "" {
			return cleanTitle(title), designArticleSectionType(doc, baseURL), rawDate
		}
	}

	if heading := doc.Find(".newList .content h1, .newList .content .news-title").First(); heading.Length() > 0 {
		title = designCompactText(heading.Text())
	}
	if title == "" {
		title = designCompactText(doc.Find("h1.news_tit, h1").First().Text())
	}
	return cleanTitle(title), designArticleSectionType(doc, baseURL), ""
}

func designArticleDate(block *goquery.Selection) string {
	if value := designCompactText(block.Find("h6").First().Text()); value != "" {
		return value
	}
	var dateText string
	block.Find(".labs p, .labs .box").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		value := designCompactText(sel.Text())
		if date, _ := designDateValues(value); date != "" {
			dateText = value
			return false
		}
		return true
	})
	return dateText
}

func designArticleSectionType(doc *goquery.Document, baseURL string) string {
	var typ string
	doc.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		if designCompactText(anchor.Text()) != "返回列表" {
			return true
		}
		resolved := resolveURL(baseURL, anchor.AttrOr("href", ""))
		if slug := designSlugFromURL(resolved); slug != "" {
			typ = designFeedType(slug)
		}
		return false
	})
	if typ != "" {
		return typ
	}
	for _, selector := range []string{".newList > .title", ".informs .sidebar .title span", ".bann .title span"} {
		value := designCompactText(doc.Find(selector).First().Text())
		if value != "" && value != "内容" && value != "文章内容" {
			return value
		}
	}
	return ""
}

func designFeedType(slug string) string {
	switch strings.Trim(slug, "/") {
	case "xydt":
		return "学院动态"
	case "xshd":
		return "学术活动"
	case "xshd/jz":
		return "讲座"
	case "xshd/yt":
		return "研讨"
	case "xshd/zl":
		return "展览"
	case "xshd/hd":
		return "活动"
	case "tzgg", "tzgg/tzgg":
		return "通知公告"
	case "tzgg/bkjx":
		return "本科教学"
	case "tzgg/yjsjx":
		return "研究生教学"
	case "jyjx":
		return "教育教学"
	case "jyjx/jxcg":
		return "教学成果"
	case "jyjx/bsxwd":
		return "博士学位点"
	case "jyjx/sjjd":
		return "实践基地"
	case "kysj/kycg":
		return "科研成果"
	case "djgz":
		return "党建工作"
	case "djgz/zzjs":
		return "组织建设"
	case "djgz/xwgg":
		return "新闻公告"
	case "djgz/xygh":
		return "学院工会"
	case "djgz/jwgz":
		return "纪委工作"
	case "djgz/zcwj":
		return "政策文件"
	case "xsgz":
		return "学生工作"
	case "xsgz/xgdw":
		return "学工队伍"
	case "xsgz/tzgg":
		return "通知公告"
	case "xsgz/sjzm":
		return "设计之美"
	case "xsgz/jyxx":
		return "就业信息"
	case "hzjl":
		return "合作交流"
	case "xyfc":
		return "校友风采"
	case "xszt":
		return "线上展厅"
	default:
		return ""
	}
}

func designSlugFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	path := strings.Trim(u.Path, "/")
	if strings.HasPrefix(path, "info/") || !strings.HasSuffix(strings.ToLower(path), ".htm") {
		return ""
	}
	return strings.TrimSuffix(path, path[len(path)-4:])
}

func designDateValues(raw string) (date, publishedAt string) {
	raw = designCompactText(raw)
	if raw == "" {
		return "", ""
	}
	if match := dateTimeRe.FindString(raw); match != "" {
		publishedAt = normalizePublishedAt(match)
		if len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	if match := bareDateRe.FindStringSubmatch(raw); match != nil {
		return normalizeDateParts(match[1], match[2], match[3]), ""
	}
	return "", ""
}

func designInternalArticleURL(baseURL, href string) (string, bool) {
	if strings.TrimSpace(href) == "" {
		return "", false
	}
	resolved := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(parsed.Hostname(), base.Hostname()) {
		return "", false
	}
	if !designArticlePathRE.MatchString(parsed.Path) {
		return "", false
	}
	return resolved, true
}

func designHasArticleBody(doc *goquery.Document) bool {
	return doc.Find("#vsb_content .v_news_content, #vsb_content_501 .v_news_content, .v_news_content, .content-main").Length() > 0
}

func designCollectArticleAttachments(doc *goquery.Document, baseURL string, article *model.Article) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) {
			return
		}
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		if !designIsAttachmentLink(anchor, href) {
			return
		}
		resolved := cleanURL(resolveURL(baseURL, href))
		if resolved == "" || seen[resolved] {
			return
		}
		name := designCompactText(anchor.Text())
		if name == "" {
			name = designCompactText(anchor.AttrOr("download", ""))
		}
		if name == "" {
			name = resolved
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func designIsAttachmentLink(anchor *goquery.Selection, href string) bool {
	if href == "" {
		return false
	}
	if designAttachmentExtRE.MatchString(href) || strings.Contains(strings.ToLower(href), "download") {
		return true
	}
	parent := anchor
	for i := 0; i < 4 && parent.Length() > 0; i++ {
		tag := strings.ToLower(goquery.NodeName(parent))
		if tag == "body" || tag == "html" {
			break
		}
		className := strings.ToLower(parent.AttrOr("class", ""))
		if strings.Contains(className, "attach") || strings.Contains(className, "download") {
			return true
		}
		if tag == "li" {
			return strings.Contains(parent.Text(), "附件")
		}
		parent = parent.Parent()
	}
	return false
}

func designCompactText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
