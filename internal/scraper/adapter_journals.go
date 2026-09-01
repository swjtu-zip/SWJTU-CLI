package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const journalsCanonicalBaseURL = "https://journals.swjtu.edu.cn"

var journalsArticlePathRE = regexp.MustCompile(`(?i)^/info/\d+/\d+\.(?:html?|shtml)$`)

// journalsAdapter is deliberately kept separate from the other unit-site
// adapters.  The current public endpoint is a journal-directory landing page,
// not a 博达 article-list site.  In particular, links to the individual
// journals lead to other hosts and must not be collected as local news.
func init() {
	if site := AllSites["journals"]; site != nil {
		site.BaseURL = journalsCanonicalBaseURL
	}
	registerAdapter("journals", SiteAdapter{
		ParseList:    parseAdapter_journalsList,
		ParseArticle: parseAdapter_journalsArticle,
	})
}

func parseAdapter_journalsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	if journalsBlockedResponse(html) {
		return nil, fmt.Errorf("journals: response is an unavailable, login, WAF, or error page")
	}
	items, err := parseBodaNewsList(html, baseURL)
	if err != nil {
		return nil, err
	}

	// The landing page contains links to journal homes and submission systems.
	// Keep only same-host article URLs with the standard public detail shape;
	// this prevents external journal portals and static directory cards from
	// becoming a handful of false-positive articles if the generic parser sees
	// their surrounding list markup.
	local := make([]model.NewsItem, 0, len(items))
	for _, item := range items {
		parsed, parseErr := url.Parse(item.URL)
		if parseErr != nil || !journalsLocalURL(parsed, baseURL) || !journalsArticlePathRE.MatchString(parsed.Path) {
			continue
		}
		local = append(local, item)
	}
	if len(local) == 0 {
		return nil, fmt.Errorf("journals: response is a static journal directory; no public article list was found")
	}
	return local, nil
}

func parseAdapter_journalsArticle(html []byte, baseURL string) (*model.Article, error) {
	if journalsBlockedResponse(html) {
		return nil, fmt.Errorf("journals: response is an unavailable, login, WAF, or error page")
	}
	if !journalsArticleShell(html) {
		return nil, fmt.Errorf("journals: response is not a public article page")
	}
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(article.Title) == "" ||
		(strings.TrimSpace(article.Content) == "" && len(article.Images) == 0 && len(article.Attachments) == 0) {
		return nil, fmt.Errorf("journals: public article page has no title or content")
	}
	article.Type = journalsArticleType(html, article.Type)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func journalsArticleType(raw []byte, current string) string {
	if strings.TrimSpace(current) != "" {
		return strings.TrimSpace(current)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return ""
	}
	for _, selector := range []string{`meta[name="articleType"]`, `meta[name="category"]`, `[data-article-type]`} {
		sel := doc.Find(selector).First()
		if sel.Length() == 0 {
			continue
		}
		value := strings.TrimSpace(sel.AttrOr("content", ""))
		if value == "" {
			value = strings.TrimSpace(sel.AttrOr("data-article-type", ""))
		}
		if value != "" {
			return value
		}
	}
	return strings.TrimSpace(doc.Find(".breadcrumb a, .position a, .crumb a").Last().Text())
}

func journalsLocalURL(candidate *url.URL, baseURL string) bool {
	if candidate == nil || candidate.Hostname() == "" {
		return false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" {
		base, _ = url.Parse(journalsCanonicalBaseURL)
	}
	return strings.EqualFold(candidate.Hostname(), base.Hostname())
}

func journalsArticleShell(html []byte) bool {
	page := strings.ToLower(string(html))
	articleMarkers := []string{
		"class=\"v_news_content\"", "class='v_news_content'", "id=\"vsb_content",
		"class=\"news_source\"", "class='news_source'", "class=\"news_tit\"", "class='news_tit'",
		"name=\"pagetype\"", "name='pagetype'", "class=\"article-title\"", "class='article-title'",
	}
	for _, marker := range articleMarkers {
		if strings.Contains(page, marker) {
			return true
		}
	}
	return false
}

func journalsBlockedResponse(html []byte) bool {
	page := strings.ToLower(strings.TrimSpace(string(html)))
	if page == "" {
		return true
	}
	for _, marker := range []string{
		"access denied", "403 forbidden", "404 not found", "500 internal server error",
		"cloudflare", "just a moment", "请输入验证码下载附件", "验证码下载附件",
		"登录后查看", "cas/login", "webexp.swjtu.edu.cn/login",
	} {
		if strings.Contains(page, marker) {
			return true
		}
	}
	return false
}
