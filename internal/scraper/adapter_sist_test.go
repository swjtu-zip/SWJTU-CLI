package scraper

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSistAdapterRegistered(t *testing.T) {
	site, ok := AllSites["sist"]
	if !ok {
		t.Fatal("sist site is not registered")
	}
	if site.BaseURL != "http://sist.swjtu.edu.cn" {
		t.Fatalf("unexpected base URL: %q", site.BaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("sist adapter hooks are incomplete: %+v", site)
	}
	wantPages := []string{
		"notice", "undergraduate", "postgraduate", "news",
		"party/dynamics", "party/development", "party/discipline", "party/union-house", "party/youth",
		"undergraduate/graduation", "undergraduate/regulations", "undergraduate/practice", "undergraduate/admission", "undergraduate/recommendation",
		"postgraduate/dissertation", "postgraduate/training", "postgraduate/admission",
		"research/notice", "research/academic", "discipline/progress",
		"students/notice", "students/news", "students/style", "students/competition/3s", "students/competition/coaches", "students/competition/info",
		"students/employment/undergraduate", "students/employment/postgraduate", "students/employment/internship",
		"alumni/news", "alumni/outstanding", "alumni/graduation", "service/guide", "service/faq",
	}
	if !reflect.DeepEqual(site.SubPages, wantPages) {
		t.Fatalf("unexpected sist sub-pages: got=%#v want=%#v", site.SubPages, wantPages)
	}
	if len(site.SubPages) != len(sistFeedRoutes) {
		t.Fatalf("registry and route table have different sub-page counts: %d vs %d", len(site.SubPages), len(sistFeedRoutes))
	}
	seen := make(map[string]bool, len(site.SubPages))
	for _, slug := range site.SubPages {
		if seen[slug] {
			t.Fatalf("duplicate sist sub-page: %q", slug)
		}
		seen[slug] = true
	}
	for _, want := range []string{"news", "party/dynamics", "undergraduate/graduation", "postgraduate/dissertation", "research/notice", "students/employment/internship", "service/faq"} {
		if !seen[want] {
			t.Errorf("missing sist sub-page %q", want)
		}
	}
}

func TestSistPageURL(t *testing.T) {
	const base = "http://sist.swjtu.edu.cn"
	if got := sistPageURL(base, "index", 1, 0, ".htm"); got != base+"/home?lang=cn" {
		t.Fatalf("unexpected index URL: %q", got)
	}
	if got := sistPageURL(base, "undergraduate", 1, 0, ".htm"); got != base+"/pc/9JpfFAQClhKDd-As8MXBYS8/KW_T7dJ2lt5sC-RJZSnPtk0?lang=cn" {
		t.Fatalf("unexpected undergraduate URL: %q", got)
	}
	if got := sistPageURL(base, "notice", 1, 0, ".htm"); got != base+"/pc/Ag_9Jx2oC93JQcGt0VS0d4I?type=2&lang=cn" {
		t.Fatalf("unexpected notice URL: %q", got)
	}
	if got := sistPageURL(base, "party/dynamics", 2, 88, ".html"); got != base+"/pc/N8YFxHPHwy6PyHt7J-gIDCU?type=2&lang=cn" {
		t.Fatalf("unexpected party URL: %q", got)
	}
	if got := sistPageURL(base, "students/competition/info", 1, 11, ".htm"); got != base+"/pc/Nn3neKHRyK4hLbkwL2Wv1j8/S1iNDeRnJkb8Hi8x1GSUJV0?lang=cn" {
		t.Fatalf("unexpected nested competition URL: %q", got)
	}
	if got := sistPageURL(base, "not-a-real-feed", 1, 1, ".htm"); got != "" {
		t.Fatalf("unknown feed unexpectedly became a home URL: %q", got)
	}
}

func TestSistSimpleTabsUseTheLivePageSize(t *testing.T) {
	wantTen := map[string]bool{
		"party/dynamics":               true,
		"undergraduate/regulations":    true,
		"undergraduate/admission":      true,
		"undergraduate/recommendation": true,
		"postgraduate/training":        true,
		"discipline/progress":          true,
		"students/notice":              true,
	}
	for _, route := range sistFeedRoutes {
		want := 5
		if wantTen[route.slug] {
			want = 10
		}
		if got := sistPageSize(route.slug); got != want {
			t.Errorf("unexpected page size for %s: got=%d want=%d", route.slug, got, want)
		}
	}
}

func TestSistUnknownFeedDoesNotFallBackToNews(t *testing.T) {
	items, pages, err := sistListPage("not-a-real-feed", 1)
	if items != nil || pages != 0 {
		t.Fatalf("unknown feed returned data: items=%#v pages=%d", items, pages)
	}
	if err == nil || !strings.Contains(err.Error(), "unknown feed") {
		t.Fatalf("expected an explicit unknown-feed error, got %v", err)
	}
}

func TestSistAPIListHasRealItemsAndNormalizedDates(t *testing.T) {
	body, err := os.ReadFile("testdata/sist_api_list.json")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseSistNews(body, "https://sist.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("expected 4 usable articles, got %d: %#v", len(items), items)
	}
	if items[0].Title != "我院学子在第19届中国大学生计算机设计大赛中再获佳绩" {
		t.Fatalf("unexpected first title: %q", items[0].Title)
	}
	if items[0].Date != "2026-08-29" {
		t.Fatalf("unexpected first date: %q", items[0].Date)
	}
	if items[0].PublishedAt != "2026-08-29 16:10:00" {
		t.Fatalf("source time was not preserved: %q", items[0].PublishedAt)
	}
	wantURL := "https://sist.swjtu.edu.cn/pc/" + sistNewsNavID + "?id=uzbA5Cgk2sZdIHrZYyeuCs38&type=2&lang=cn"
	if items[0].URL != wantURL {
		t.Fatalf("unexpected first URL: %q", items[0].URL)
	}
	seen := make(map[string]bool)
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate article URL: %q", item.URL)
		}
		seen[item.URL] = true
		if strings.Contains(item.Title, "新闻中心") || strings.Contains(item.Title, "通知公告") {
			t.Errorf("navigation title leaked into result: %q", item.Title)
		}
		if !strings.HasPrefix(item.Date, "2026-") {
			t.Errorf("date was not normalized: %q", item.Date)
		}
	}
}

func TestSistRenderedListKeepsOnlyArticleLinks(t *testing.T) {
	body := []byte(`<html><body>
<nav><a href="/pc/9JpfFAQClhKDd-As8MXBYS8?type=2">本科教育</a></nav>
<div class="menu"><a href="/pc/DpHktHeXJp3MX-FZoZdjz3w?id=nav-only&type=2">这是导航伪文章链接</a></div>
<main class="article_list">
  <a href="/pc/DpHktHeXJp3MX-FZoZdjz3w?id=one&type=2&lang=cn"><span class="article-title">第一条真实学院新闻</span><time>2026年8月29日</time></a>
  <a href="/pc/DpHktHeXJp3MX-FZoZdjz3w?id=two&type=2&lang=cn"><span class="article-title">第二条真实学院新闻</span><time>2026/08/23 00:00:00</time></a>
  <a href="https://example.invalid/pc/news?id=three">外部导航项</a>
</main>
</body></html>`)
	items, err := parseSistNews(body, "https://sist.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 article links, got %d: %#v", len(items), items)
	}
	if items[0].Date != "2026-08-29" || items[1].Date != "2026-08-23" {
		t.Fatalf("unexpected dates: %#v", items)
	}
}

func TestSistArticleAPIFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/sist_api_article.json")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseSistArticle(body, "https://sist.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "我院学子在第19届中国大学生计算机设计大赛中再获佳绩" {
		t.Fatalf("unexpected title: %q", article.Title)
	}
	if article.Date != "2026-08-29" {
		t.Fatalf("unexpected date: %q", article.Date)
	}
	if article.PublishedAt != "2026-08-29 16:10:00" {
		t.Fatalf("source time was not preserved: %q", article.PublishedAt)
	}
	if article.Source != "信息科学与技术学院" || article.Author != "吴卫伟" {
		t.Fatalf("unexpected metadata: source=%q author=%q", article.Source, article.Author)
	}
	if !strings.Contains(article.Content, "第19届中国大学生计算机设计大赛") {
		t.Fatalf("article content was not extracted: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://sist.swjtu.edu.cn/attached/2026/08/29/f78c0101-e286-413f-b1af-05a3f45cf438.png" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
}

func TestSistAPIListUsesRowNavigationAndType(t *testing.T) {
	body := []byte(`{"code":200,"rows":[{"id":"nested-id","title":"本科教育分页测试文章","publishTime":"2026-03-16 09:19:00","navId":"1GPPT6YeIhXPHpMRyxKjlE4","type":"通知公告"}]}`)
	items, err := parseSistNews(body, "https://sist.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %#v", items)
	}
	if items[0].URL != "https://sist.swjtu.edu.cn/pc/9JpfFAQClhKDd-As8MXBYS8/1GPPT6YeIhXPHpMRyxKjlE4?id=nested-id&lang=cn" {
		t.Fatalf("unexpected nested article URL: %q", items[0].URL)
	}
	if items[0].PublishedAt != "2026-03-16 09:19:00" || items[0].Type != "通知公告" {
		t.Fatalf("precise metadata was not retained: %#v", items[0])
	}
}

func TestSistConfirmedEmptyAPIColumnIsNotTheSPAError(t *testing.T) {
	items, err := parseSistNews([]byte(`{"code":200,"total":0,"rows":[]}`), "https://sist.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected an explicitly empty API column, got %#v", items)
	}
}

func TestSistArticleMetadataAndAttachmentsFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/sist_api_notice_resources.json")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseSistArticle(body, "https://sist.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "2026—2027 学年秋季学期开学相关时间安排" {
		t.Fatalf("unexpected title: %q", article.Title)
	}
	if article.Date != "2026-08-29" || article.PublishedAt != "2026-08-29 16:14:12" {
		t.Fatalf("unexpected precise date: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	if article.Source != "信息科学与技术学院" || article.Author != "周晓星" || article.Type != "通知公告" {
		t.Fatalf("unexpected metadata: source=%q author=%q type=%q", article.Source, article.Author, article.Type)
	}
	if !strings.Contains(article.Content, "开学") {
		t.Fatalf("article body was not extracted: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://sist.swjtu.edu.cn/attached/2026/08/29/home-notice.png" {
		t.Fatalf("unexpected images: %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://sist.swjtu.edu.cn/attached/2026/08/29/home-notice.png"`) {
		t.Fatalf("ContentHTML did not retain the resolved image URL: %q", article.ContentHTML)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("expected two attachments, got %#v", article.Attachments)
	}
	if article.Attachments[0].URL != "https://sro.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1611485322&wbfileid=doc-id" {
		t.Fatalf("extensionless DOCX URL was not retained: %#v", article.Attachments)
	}
	if article.Attachments[1].URL != "https://sist.swjtu.edu.cn/attached/2026/08/29/notice.pdf" {
		t.Fatalf("relative PDF URL was not resolved: %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, `href="https://sist.swjtu.edu.cn/attached/2026/08/29/notice.pdf"`) {
		t.Fatalf("ContentHTML did not retain the resolved attachment URL: %q", article.ContentHTML)
	}
}

func TestSistContentRecognizesExtensionlessDownloadEndpoint(t *testing.T) {
	content, html, _, attachments := parseSistContent(
		`<p>正文中的下载项。</p><p><a href="/system/_content/download.jsp?urltype=news.DownloadAttachUrl&amp;owner=1611485322&amp;wbfileid=doc-id">下载</a></p>`,
		"https://sist.swjtu.edu.cn",
	)
	if content != "正文中的下载项。\n\n下载" {
		t.Fatalf("unexpected plain content: %q", content)
	}
	if len(attachments) != 1 || attachments[0].URL != "https://sist.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1611485322&wbfileid=doc-id" {
		t.Fatalf("extensionless download endpoint was not classified as an attachment: %#v", attachments)
	}
	if !strings.Contains(html, `href="https://sist.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&amp;owner=1611485322&amp;wbfileid=doc-id"`) {
		t.Fatalf("ContentHTML did not preserve the resolved download URL: %q", html)
	}
}

func TestSistInfoPayloadIsNotAnArticle(t *testing.T) {
	body, err := os.ReadFile("testdata/sist_api_info.json")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseSistArticle(body, "https://sist.swjtu.edu.cn")
	if article != nil {
		t.Fatalf("/web/info category payload was parsed as article: %#v", article)
	}
	if !errors.Is(err, errSistNoArticles) {
		t.Fatalf("expected non-article error, got %v", err)
	}
}

func TestSistShellIsNotASuccessfulEmptyFeed(t *testing.T) {
	body, err := os.ReadFile("testdata/sist.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseSistNews(body, "http://sist.swjtu.edu.cn")
	if len(items) != 0 {
		t.Fatalf("SPA shell unexpectedly produced items: %#v", items)
	}
	if !errors.Is(err, errSistDynamicPage) {
		t.Fatalf("expected explicit SPA/API error, got %v", err)
	}
	if !strings.Contains(err.Error(), "/dev-api/web/articlelist") {
		t.Fatalf("error did not identify the required API: %v", err)
	}
}

func TestSistShellArticleIsNotParsedAsArticle(t *testing.T) {
	body, err := os.ReadFile("testdata/sist.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseSistArticle(body, "http://sist.swjtu.edu.cn")
	if article != nil {
		t.Fatalf("SPA shell unexpectedly produced article: %#v", article)
	}
	if !errors.Is(err, errSistDynamicPage) {
		t.Fatalf("expected explicit SPA/API error, got %v", err)
	}
	if !strings.Contains(err.Error(), "/dev-api/web/article") {
		t.Fatalf("error did not identify the required API: %v", err)
	}
}

func TestSistLoginOrWAFHTMLIsNotAnEmptyResult(t *testing.T) {
	body := []byte(`<html><head><title>登录</title></head><body>统一身份认证</body></html>`)
	items, listErr := parseSistNews(body, "https://sist.swjtu.edu.cn")
	if len(items) != 0 || !errors.Is(listErr, errSistUnavailablePage) {
		t.Fatalf("login HTML was not diagnosed as unavailable list data: items=%#v err=%v", items, listErr)
	}
	article, articleErr := parseSistArticle(body, "https://sist.swjtu.edu.cn")
	if article != nil || !errors.Is(articleErr, errSistUnavailablePage) {
		t.Fatalf("login HTML was not diagnosed as unavailable article data: article=%#v err=%v", article, articleErr)
	}
}
