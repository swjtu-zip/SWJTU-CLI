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

const jgdwBaseURL = "https://jgdw.swjtu.edu.cn"

// jgdwSubPages is the complete set of article-list routes exposed by the
// site's navigation and homepage.  The three bmgk pages and tjxw are checked
// separately: bmgk is static content and tjxw is a legal empty photo-list
// page, not a missing route.
var jgdwSubPages = []string{
	"djyw",
	"jcdj",
	"zgzj",
	"qngz",
	"index/tzgg",
	"index/tjxw",
}

var (
	jgdwArticlePathRE = regexp.MustCompile(`(?i)^/info/[0-9]+/[0-9]+\.html?$`)
	jgdwDateTimeRE    = regexp.MustCompile(`20\d{2}\s*[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}(?:日|号)?(?:\s+|[Tt]+)\d{1,2}:\d{2}(?::\d{2})?`)
	jgdwDateRE        = regexp.MustCompile(`20\d{2}\s*[-/.年]\s*\d{1,2}\s*[-/.月]\s*\d{1,2}`)
)

// jgdwAdapter is intentionally independent from the generic Boda parser.
// This site uses the same CMS family as several other units, but its list
// rows, article heading, and outside-body attachment block are different.
func init() {
	registerAdapter("jgdw", SiteAdapter{
		SubPages:     jgdwSubPages,
		ParseList:    parseAdapter_jgdwList,
		ParseArticle: parseAdapter_jgdwArticle,
		PageURL:      jgdwPageURL,
		TotalPages:   jgdwTotalPages,
	})
}

// jgdwPageURL maps the logical page number (1 = newest) to the generated
// source file.  jgdw numbers generated files backwards:
// slug.htm, slug/(N-1).htm, ..., slug/1.htm.
func jgdwPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	slug = strings.Trim(slug, "/")
	if extension == "" {
		extension = ".htm"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page < 1 {
		return ""
	}
	if slug == "" {
		slug = "index"
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	if totalPages < 1 || page > totalPages {
		return ""
	}
	physical := totalPages - page + 1
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, physical, extension)
}

func jgdwTotalPages(slug string) (int, error) {
	listURL := jgdwPageURL(jgdwBaseURL, slug, 1, 0, ".htm")
	body, err := fetch(listURL)
	if err != nil {
		return 0, fmt.Errorf("fetch jgdw first page %q: %w", slug, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("parse jgdw first page %q: %w", slug, err)
	}
	if !jgdwIsHomepage(doc) && !jgdwIsListPage(doc) {
		return 0, fmt.Errorf("jgdw page %q is not a supported list shell", slug)
	}
	total := parseTotalPages(body)
	if total < 1 {
		// A recognized one-page list, including the legal empty tjxw page, must
		// still expose a pagination shell. A short response without one is not
		// enough evidence that older generated pages do not exist.
		if doc.Find(".pagination, .p_pages, .paging").Length() > 0 {
			return 1, nil
		}
		return 0, fmt.Errorf("jgdw: %s has no recognized pagination", slug)
	}
	return total, nil
}

func parseAdapter_jgdwList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse jgdw list: %w", err)
	}

	if jgdwIsHomepage(doc) {
		return jgdwParseHomepage(doc, baseURL)
	}
	if !jgdwIsListPage(doc) {
		return nil, fmt.Errorf("jgdw page is not a supported article-list shell")
	}

	rows := doc.Find(".text-list > ul > li")
	if rows.Length() == 0 {
		// The recognized tjxw shell is a valid empty list.  Returning an empty
		// slice here is deliberate; an unrecognized page was rejected above.
		return []model.NewsItem{}, nil
	}

	typ := jgdwHeading(doc)
	items := make([]model.NewsItem, 0, rows.Length())
	seen := make(map[string]bool, rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		anchor := row.Find("a[href]").First()
		if anchor.Length() == 0 {
			return
		}
		fullURL, ok := jgdwArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		title := jgdwAnchorTitle(anchor)
		if title == "" || !isValidNewsTitle(title) {
			return
		}
		date, publishedAt := jgdwDateFields(row.Text())
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
	return items, nil
}

func jgdwIsHomepage(doc *goquery.Document) bool {
	return doc.Find(`meta[name="pageType"]`).AttrOr("content", "") == "1" &&
		doc.Find(".section3 .part3k .listt").Length() > 0 &&
		doc.Find(".tzgglist").Length() > 0
}

func jgdwIsListPage(doc *goquery.Document) bool {
	if doc.Find(`meta[name="pageType"]`).AttrOr("content", "") != "2" {
		return false
	}
	return doc.Find(".ny-right .text-list, .text-list").Length() > 0 &&
		doc.Find(".position h1, .nybannerwz").Length() > 0
}

func jgdwHeading(doc *goquery.Document) string {
	for _, selector := range []string{".ny-right .position h1", ".position h1", ".nybannerwz p"} {
		if text := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " "); text != "" {
			return text
		}
	}
	return ""
}

func jgdwParseHomepage(doc *goquery.Document, baseURL string) ([]model.NewsItem, error) {
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	add := func(scope *goquery.Selection, typ string) {
		scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			fullURL, ok := jgdwArticleURL(baseURL, anchor.AttrOr("href", ""))
			if !ok || seen[fullURL] {
				return
			}
			title := jgdwAnchorTitle(anchor)
			if title == "" || !isValidNewsTitle(title) {
				return
			}
			row := anchor.Closest("li")
			date, publishedAt := jgdwDateFields(row.Text())
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
	}

	homeTypes := []string{"党建要闻", "基层党建", "职工之家"}
	doc.Find(".section3 .part3k").Each(func(i int, part *goquery.Selection) {
		if i < len(homeTypes) {
			add(part.Find(".listt > ul"), homeTypes[i])
		}
	})
	add(doc.Find(".tzgglist").First(), "通知公告")
	if len(items) == 0 {
		return nil, fmt.Errorf("jgdw homepage contains no scoped article modules")
	}
	return items, nil
}

// jgdwAnchorTitle handles the homepage notice template where the article
// anchor wraps both <div class="title"> and <div class="time">. Reading the
// whole anchor would append the month/day to the headline.
func jgdwAnchorTitle(anchor *goquery.Selection) string {
	if anchor == nil || anchor.Length() == 0 {
		return ""
	}
	if title := strings.TrimSpace(anchor.AttrOr("title", "")); title != "" {
		return strings.Join(strings.Fields(title), " ")
	}
	for _, selector := range []string{".title", "h1", "h2", "h3", "h4"} {
		if title := strings.Join(strings.Fields(anchor.Find(selector).First().Text()), " "); title != "" {
			return title
		}
	}
	clone := anchor.Clone()
	clone.Find(".time, .date, time, .p_time").Remove()
	return strings.Join(strings.Fields(clone.Text()), " ")
}

func jgdwArticleURL(baseURL, href string) (string, bool) {
	resolved := resolveURL(baseURL, strings.TrimSpace(href))
	u, err := url.Parse(resolved)
	if err != nil || u.Host == "" || !jgdwArticlePathRE.MatchString(u.Path) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Host, base.Host) || !strings.EqualFold(u.Scheme, base.Scheme) {
		return "", false
	}
	return cleanURL(u.String()), true
}

func jgdwDateFields(value string) (string, string) {
	value = strings.Join(strings.Fields(value), " ")
	if match := jgdwDateTimeRE.FindString(value); match != "" {
		publishedAt := normalizePublishedAt(match)
		if len(publishedAt) >= 10 {
			return publishedAt[:10], publishedAt
		}
	}
	if match := jgdwDateRE.FindString(value); match != "" {
		date := normalizePublishedAt(match)
		return date, date
	}
	return "", ""
}

func parseAdapter_jgdwArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse jgdw article: %w", err)
	}
	if doc.Find(`meta[name="pageType"]`).AttrOr("content", "") != "3" {
		return nil, fmt.Errorf("jgdw page is not an article shell")
	}
	if doc.Find(".art-tit h3").First().Length() == 0 ||
		doc.Find("#vsb_content .v_news_content, .v_news_content").First().Length() == 0 {
		return nil, fmt.Errorf("jgdw article shell is missing heading or content")
	}

	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse jgdw article fields: %w", err)
	}
	article.Title = jgdwArticleTitle(doc)
	article.Type = jgdwHeading(doc)

	meta := strings.Join(strings.Fields(doc.Find(".art-tit h2").First().Text()), " ")
	if date, publishedAt := jgdwDateFields(meta); date != "" {
		article.Date = date
		article.PublishedAt = publishedAt
	}
	if article.Source == "" {
		article.Source = jgdwMetaValue(meta, "来源")
	}
	jgdwAppendOutsideAttachments(doc, article, baseURL)
	return article, nil
}

func jgdwArticleTitle(doc *goquery.Document) string {
	return strings.Join(strings.Fields(doc.Find(".art-tit h3").First().Text()), " ")
}

func jgdwMetaValue(meta, label string) string {
	index := strings.Index(meta, label+"：")
	separatorLen := len(label) + len("：")
	if index < 0 {
		index = strings.Index(meta, label+":")
		separatorLen = len(label) + len(":")
	}
	if index < 0 {
		return ""
	}
	value := strings.TrimSpace(meta[index+separatorLen:])
	for _, stop := range []string{"浏览次数", "点击量", "阅读量"} {
		if cut := strings.Index(value, stop); cut >= 0 {
			value = value[:cut]
		}
	}
	return strings.TrimSpace(value)
}

func jgdwAppendOutsideAttachments(doc *goquery.Document, article *model.Article, baseURL string) {
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}

	doc.Find("form[name='_newscontent_fromname'] li a[href], .art-main > p li a[href]").Each(func(_ int, anchor *goquery.Selection) {
		row := anchor.Closest("li")
		if row.Length() == 0 || !strings.Contains(row.Text(), "附件") {
			return
		}
		resolved := resolveURL(baseURL, anchor.AttrOr("href", ""))
		if !jgdwSameOriginURL(resolved, baseURL) || seen[resolved] {
			return
		}
		name := strings.Join(strings.Fields(anchor.Text()), " ")
		if name == "" {
			name = strings.Join(strings.Fields(anchor.AttrOr("download", "")), " ")
		}
		if name == "" {
			return
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func jgdwSameOriginURL(value, baseURL string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return false
	}
	base, err := url.Parse(baseURL)
	return err == nil && strings.EqualFold(u.Host, base.Host) && strings.EqualFold(u.Scheme, base.Scheme)
}
