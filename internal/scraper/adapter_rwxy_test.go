package scraper

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rwxyFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read rwxy fixture %q: %v", name, err)
	}
	return data
}

func TestRwxyAdapterRegistrationAndTabInventory(t *testing.T) {
	site := AllSites["rwxy"]
	if site == nil {
		t.Fatal("rwxy site is not registered")
	}
	if site.BaseURL != rwxyCanonicalBaseURL {
		t.Fatalf("base URL = %q, want %q", site.BaseURL, rwxyCanonicalBaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("rwxy adapter is missing a required hook")
	}
	want := []string{
		"xyxw", "dzbg", "xshd", "jxdt", "jxdt/bkjx", "jxdt/yjsjx",
		"xsgz/tzgg", "xsgz/xgdt", "xsgz/xzfc", "xsgz/jyxx", "jwgz/jwdt",
		"djgz/djdt", "djgz/xygh", "rcpy/yjsjy", "rcpy/bshldz",
	}
	if strings.Join(site.SubPages, "\n") != strings.Join(want, "\n") {
		t.Fatalf("subpages = %#v, want %#v", site.SubPages, want)
	}
}

func TestRwxyListUsesHeadlineAndCommentDate(t *testing.T) {
	html := []byte(`<div class="sidebar"><div class="title"><span>学院新闻</span></div></div>
<div class="loaction"><a href="index.htm">首页</a><a href="xyxw.htm">学院新闻</a></div>
<div class="list-news"><ul><li><a href="info/1142/28674.htm"><div class="intr"><div class="time"><!-- 2026年7月14日--> 北京交通大学外国语与新闻传播学院与西南交通大学人文学院共话新闻传播学科建设</div><div class="more"><a href="info/1142/28674.htm">More →</a></div></div></a></li></ul></div>`)
	items, err := parseAdapter_rwxyList(html, "https://rwxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Title != "北京交通大学外国语与新闻传播学院与西南交通大学人文学院共话新闻传播学科建设" {
		t.Fatalf("title = %q", items[0].Title)
	}
	if items[0].Date != "2026-07-14" || items[0].PublishedAt != "2026-07-14" {
		t.Fatalf("date fields = %q/%q", items[0].Date, items[0].PublishedAt)
	}
	if items[0].Type != "学院新闻" {
		t.Fatalf("type = %q", items[0].Type)
	}
}

func TestRwxyArticleParsesPreciseDate(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="人文学院新闻"></head><body>
<div class="sidebar"><div class="title"><span>学院新闻</span></div></div>
<form name="_newscontent_fromname"><div class="bd"><div class="title"><h3>人文学院新闻</h3><div class="info"><span>作者：杜欣瑞</span><span>日期：2026-07-14 09:56</span></div></div><div class="detail"><div id="vsb_content_501"><div class="v_news_content"><p>这是完整的详情正文。</p></div></div></div></div></form>
</body></html>`)
	article, err := parseAdapter_rwxyArticle(html, "https://rwxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-07-14" || article.PublishedAt != "2026-07-14 09:56:00" {
		t.Fatalf("date fields = %q/%q", article.Date, article.PublishedAt)
	}
	if article.Content != "这是完整的详情正文。" {
		t.Fatalf("content = %q", article.Content)
	}
}

func TestRwxyLegacyMigrationDateIsNotPublishedDate(t *testing.T) {
	if date, publishedAt := rwxyDateValues("日期：2013-01-01 00:00"); date != "" || publishedAt != "" {
		t.Fatalf("legacy migration date leaked: %q/%q", date, publishedAt)
	}

	html := []byte(`<html><body><form name="_newscontent_fromname"><div class="title"><h3>旧文章</h3><div class="info">日期：2013-01-01 00:00</div></div><div class="detail"><div class="v_news_content"><p>这是一段完整的旧文章正文，日期来自迁移模板。</p></div></div></form></body></html>`)
	article, err := parseAdapter_rwxyArticle(html, "https://rwxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("legacy migration date leaked from article: %q/%q", article.Date, article.PublishedAt)
	}
}

func TestRwxyHomeParsesLiveDatesAndTypes(t *testing.T) {
	html := []byte(`<html><body>
<div class="news"><div class="conts"><div class="title"><b>学院新闻</b></div>
  <div class="item"><div class="imgs"><div class="labs"><em>07-14</em><b>2026</b></div></div><div class="text"><div class="tits"><a href="info/1142/28674.htm">学院新闻标题</a></div></div></div>
</div></div>
<div class="activity"><div class="title"><b>学术活动</b></div><div class="conts"><div class="swiper-slide">
  <div class="inform"><div class="time"><span>2026/06/02</span></div></div><div class="tits"><a href="info/1143/27964.htm">学术活动标题</a></div>
</div></div></div>
</body></html>`)

	items, err := parseAdapter_rwxyList(html, "https://rwxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Date != "2026-07-14" || items[0].Type != "学院新闻" {
		t.Errorf("home news item = %#v", items[0])
	}
	if items[1].Date != "2026-06-02" || items[1].Type != "学术活动" {
		t.Errorf("home activity item = %#v", items[1])
	}
}

func TestRwxyAdapterOwnsDisplayedReversePagination(t *testing.T) {
	html := []byte(`<div class="list-news"><ul><li><a href="info/1142/28674.htm"><div class="time"><!-- 2026年7月14日-->标题</div></a></li></ul></div>
<div class="p_pages"><span class="p_no_d">1</span><a href="xyxw/215.htm">2</a><a href="xyxw/1.htm">216</a></div>`)
	if got := rwxyTotalPagesFromHTML(html); got != 216 {
		t.Fatalf("total pages = %d, want 216", got)
	}
	base := "http://rwxy.swjtu.edu.cn"
	if got := rwxyPageURL(base, "xyxw", 2, 216, ".htm"); got != base+"/xyxw/215.htm" {
		t.Fatalf("logical page 2 URL = %q", got)
	}
	if got := rwxyPageURL(base, "xyxw", 217, 216, ".htm"); got != "" {
		t.Fatalf("out-of-range URL = %q", got)
	}
}

func TestRwxyNoticeListFixtureUsesFixedDateAndTabType(t *testing.T) {
	items, err := parseAdapter_rwxyList(rwxyFixture(t, "rwxy_list_notice.html"), rwxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Title != "关于2026年秋季学期学生注册报到的通知" || items[0].Type != "通知公告" {
		t.Fatalf("first item metadata = %#v", items[0])
	}
	if items[0].Date != "2026-09-03" || items[0].PublishedAt != "2026-09-03" {
		t.Fatalf("first item date = %q/%q", items[0].Date, items[0].PublishedAt)
	}
	if items[1].Date != "" || items[1].PublishedAt != "" {
		t.Fatalf("legacy list date leaked = %q/%q", items[1].Date, items[1].PublishedAt)
	}
	if got := rwxyTotalPagesFromHTML(rwxyFixture(t, "rwxy_list_notice.html")); got != 6 {
		t.Fatalf("fixture total pages = %d, want 6", got)
	}
	if got := rwxyPageURL(rwxyCanonicalBaseURL, "xsgz/tzgg", 1, 6, ".htm"); got != rwxyCanonicalBaseURL+"/xsgz/tzgg.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := rwxyPageURL(rwxyCanonicalBaseURL, "xsgz/tzgg", 3, 6, ".htm"); got != rwxyCanonicalBaseURL+"/xsgz/tzgg/4.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := rwxyPageURL(rwxyCanonicalBaseURL, "xsgz/tzgg", 6, 6, ".htm"); got != rwxyCanonicalBaseURL+"/xsgz/tzgg/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := rwxyPageURL(rwxyCanonicalBaseURL, "xsgz/tzgg", 7, 6, ".htm"); got != "" {
		t.Fatalf("out-of-range page URL = %q", got)
	}
}

func TestRwxyExplicitEmptyListIsSuccessful(t *testing.T) {
	items, err := parseAdapter_rwxyList(rwxyFixture(t, "rwxy_empty_list.html"), rwxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty list result = %#v, want non-nil empty slice", items)
	}
}

func TestRwxyArticleFixtureUsesDetailMetadataAndFiltersResources(t *testing.T) {
	article, err := parseAdapter_rwxyArticle(rwxyFixture(t, "rwxy_article_resources.html"), rwxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于2026年秋季学期学生注册报到的通知" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Type != "通知公告" {
		t.Fatalf("type = %q", article.Type)
	}
	if article.Date != "2026-09-03" || article.PublishedAt != "2026-09-03 16:02:47" {
		t.Fatalf("date fields = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "这是完整的通知正文") || !strings.Contains(article.Content, "活动时间：2099年12月31日") {
		t.Fatalf("content lost body text: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != rwxyCanonicalBaseURL+"/__local/demo.jpg" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "材料.pdf" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "spacer.gif") || strings.Contains(article.ContentHTML, "captcha") {
		t.Fatalf("placeholder resource leaked into content HTML: %q", article.ContentHTML)
	}
}

func TestRwxyErrorListResponseIsNotHomepageFallback(t *testing.T) {
	if _, err := parseAdapter_rwxyList(rwxyFixture(t, "rwxy_error.html"), rwxyCanonicalBaseURL); err == nil {
		t.Fatal("expected an explicit error-page response to fail")
	}
	if _, err := parseAdapter_rwxyArticle(rwxyFixture(t, "rwxy_error.html"), rwxyCanonicalBaseURL); err == nil {
		t.Fatal("expected an explicit article error-page response to fail")
	}
}

func TestRwxyAttachmentValidationChecksShellAndMagic(t *testing.T) {
	if rwxyAttachmentBodyIsUsable("text/html; charset=utf-8", []byte("<!doctype html><title>附件下载</title>"), "材料.pdf") {
		t.Fatal("HTML attachment shell was accepted")
	}
	if rwxyAttachmentBodyIsUsable("application/pdf", []byte("not a PDF"), "材料.pdf") {
		t.Fatal("invalid PDF magic was accepted")
	}
	if !rwxyAttachmentBodyIsUsable("application/pdf", []byte("%PDF-1.7\nbody"), "材料.pdf") {
		t.Fatal("valid PDF magic was rejected")
	}
	if !rwxyAttachmentBodyIsUsable("application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("PK\x03\x04docx"), "材料.docx") {
		t.Fatal("valid OOXML magic was rejected")
	}
}

func TestRwxyLiveHomeFixtureParsesHomeFeed(t *testing.T) {
	data := rwxyFixture(t, "rwxy_live_home.html")
	items, err := parseAdapter_rwxyList(data, rwxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 5 {
		t.Fatalf("live homepage items = %d, want at least 5: %#v", len(items), items)
	}
	seenTypes := make(map[string]bool)
	for _, item := range items {
		if !strings.Contains(item.URL, "rwxy.swjtu.edu.cn/info/") {
			t.Errorf("homepage item is not a local article link: %#v", item)
		}
		seenTypes[item.Type] = true
	}
	if !seenTypes["学院新闻"] || !seenTypes["党建动态"] {
		t.Fatalf("homepage section types were not detected: %#v", seenTypes)
	}
}

func TestRwxyLiveHomeImagePathIsNotAPageIndicator(t *testing.T) {
	// The live homepage carries carousel image paths such as
	// /__local/1/99/DB/....jpg; that "1/99" must not be promoted into a
	// total page count, or ListNewsRange walks a nonexistent index/98.html.
	data := rwxyFixture(t, "rwxy_live_home.html")
	if got := rwxyTotalPagesFromHTML(data); got != 0 {
		t.Fatalf("homepage total pages = %d, want 0 (no archive pagination)", got)
	}
}

func TestRwxyIndexFeedIsASingleScopedHomepage(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	oldBase := AllSites["rwxy"].BaseURL
	requested := make([]string, 0, 4)
	defaultClient = &http.Client{Transport: rwxyTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.Path)
		if req.URL.Path == "/index.htm" {
			return rwxyTestResponse(req, http.StatusOK, "text/html; charset=utf-8", rwxyFixture(t, "rwxy_live_home.html")), nil
		}
		return rwxyTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
	})}
	AllSites["rwxy"].BaseURL = rwxyCanonicalBaseURL
	NoCache = true
	ClearCache()
	t.Cleanup(func() {
		defaultClient, NoCache = oldClient, oldNoCache
		AllSites["rwxy"].BaseURL = oldBase
		ClearCache()
	})

	total, err := AllSites["rwxy"].GetTotalPages("index")
	if err != nil || total != 1 {
		t.Fatalf("index total pages = %d, err = %v; want 1/nil", total, err)
	}
	items, err := AllSites["rwxy"].ListNewsRange("index", "2026-01-01", "2026-12-31", 0)
	if err != nil {
		t.Fatalf("index ListNewsRange: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("index feed returned no homepage items")
	}
	for _, path := range requested {
		if strings.HasPrefix(path, "/index/") {
			t.Fatalf("paginated archive path was requested for the homepage: %#v", requested)
		}
	}
}

type rwxyTestRoundTripper func(*http.Request) (*http.Response, error)

func (f rwxyTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func rwxyTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}
}
