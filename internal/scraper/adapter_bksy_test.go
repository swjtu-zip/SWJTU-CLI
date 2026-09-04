package scraper

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

func TestBksyAdapterRegistrationCoversRealEntryPoints(t *testing.T) {
	site, ok := AllSites["bksy"]
	if !ok {
		t.Fatal("bksy site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("bksy adapter hooks are incomplete: %+v", site)
	}

	required := []string{
		"tzgg/qb", "tzgg/jxyx", "tzgg/jxjs", "tzgg/sjjx", "tzgg/syjx", "tzgg/cxcy", "tzgg/zljs",
		"jxyx/xjxg", "jxjs/jxgg", "jsfz/jsjxzz",
		"syjx/sfzx", "sjjx/xkjs", "cxcy/cxcyhd", "zljj/zlbz",
	}
	seen := make(map[string]bool, len(site.SubPages))
	for _, slug := range site.SubPages {
		if seen[slug] {
			t.Fatalf("duplicate bksy sub-page: %q", slug)
		}
		seen[slug] = true
	}
	for _, slug := range required {
		if !seen[slug] {
			t.Errorf("missing bksy sub-page %q", slug)
		}
	}
	if len(site.SubPages) != len(bksySubPages) || len(site.SubPages) != 32 {
		t.Fatalf("unexpected bksy sub-page coverage: %d (%#v)", len(site.SubPages), site.SubPages)
	}
	for _, excluded := range []string{
		"tzgg/tsjy", "tzgg/glgd", "jgsz/glgzzd", "jgsz/xgwjbg",
		"jsfz/jcjxzz", "jsfz/jsjxjs", "jsfz/jxnlts", "syjx/xfzx",
		"sjjx/cjrh", "cxcy/cxcykc", "cxcy/cxcyxm", "cxcy/cxcypt", "zljj/ztsj",
	} {
		if seen[excluded] {
			t.Errorf("empty/static bksy page was registered as an article feed: %q", excluded)
		}
	}
}

func TestBksyAdapterPageURLUsesReverseStaticNumbering(t *testing.T) {
	const base = "https://bksy.swjtu.edu.cn/"
	cases := []struct {
		slug, want  string
		page, total int
	}{
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb.htm", 1, 243},
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb/242.htm", 2, 243},
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb/122.htm", 122, 243},
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb/2.htm", 242, 243},
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb/1.htm", 243, 243},
		{"tzgg/syjx", "https://bksy.swjtu.edu.cn/tzgg/syjx/2.htm", 2, 3},
		{"tzgg/qb", "", 2, 0},
	}
	for _, tc := range cases {
		if got := bksyAdapterPageURL(base, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("page URL %s page %d/%d = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestBksyAdapterPageURLRejectsOutOfRange(t *testing.T) {
	const base = "https://bksy.swjtu.edu.cn/"
	for _, tc := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 243},
		{name: "past last", page: 244, totalPages: 243},
		{name: "past one-page feed", page: 2, totalPages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bksyAdapterPageURL(base, "tzgg/qb", tc.page, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("bksyAdapterPageURL() = %q for page %d/%d, want empty URL", got, tc.page, tc.totalPages)
			}
		})
	}
}

func TestBksyAdapterHomepageScopesOnlyTheNoticeTab(t *testing.T) {
	html, err := os.ReadFile("testdata/bksy_adapter_home.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_bksyList(html, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse homepage: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d homepage articles, want one real internal article: %#v", len(items), items)
	}
	item := items[0]
	if item.Title != "本科生院真实通知" || item.URL != "https://bksy.swjtu.edu.cn/info/1121/94301.htm" {
		t.Fatalf("unexpected homepage item: %#v", item)
	}
	if item.Date != "2026/07/08" || item.PublishedAt != "2026-07-08" || item.Type != "全部" {
		t.Fatalf("unexpected homepage metadata: %#v", item)
	}
	for _, rejected := range []string{"轮播文章", "隐藏 tab 文章", "侧栏推荐", "直接下载文件", "跨站 SPA 文章"} {
		if item.Title == rejected {
			t.Fatalf("non-list item leaked: %#v", item)
		}
	}
}

func TestBksyAdapterCategoryListRejectsFilesAndExternalRoutes(t *testing.T) {
	html, err := os.ReadFile("testdata/bksy_adapter_list.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_bksyList(html, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse category: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d category articles, want one internal article: %#v", len(items), items)
	}
	if items[0].Title != "教学运行真实通知" || items[0].Type != "教学运行" || items[0].Date != "2026/09/01" {
		t.Fatalf("unexpected category item: %#v", items[0])
	}
}

func TestBksyAdapterDistinguishesEmptyListFromShell(t *testing.T) {
	empty := []byte(`<main class="list-container"><div class="content-nav">通识教育</div><ul class="right-list"><li class="pagination">首页</li></ul></main>`)
	items, err := parseAdapter_bksyList(empty, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("reachable empty list should parse: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list produced articles: %#v", items)
	}

	if _, err := parseAdapter_bksyList([]byte(`<html><body><div id="app"></div></body></html>`), "https://bksy.swjtu.edu.cn"); err == nil {
		t.Fatal("SPA shell was accepted as a bksy list")
	}
}

func TestBksyAdapterArticleFieldsAndOutsideAttachment(t *testing.T) {
	html, err := os.ReadFile("testdata/bksy_adapter_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_bksyArticle(html, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse article: %v", err)
	}
	if article.Title != "本科生院详情测试通知" || article.Date != "2026-08-06" || article.PublishedAt != "2026-08-06 12:34:56" {
		t.Fatalf("unexpected title/time: %+v", article)
	}
	if article.Type != "教学改革与研究" || article.Source != "本科生院" {
		t.Fatalf("unexpected type/source: %+v", article)
	}
	if !strings.Contains(article.Content, "正文段落") || strings.Contains(article.Content, "侧栏推荐") || strings.Contains(article.Content, "外部附件.xlsx") {
		t.Fatalf("body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://bksy.swjtu.edu.cn/__local/body.png" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("got %d attachments, want body + outside attachment: %#v", len(article.Attachments), article.Attachments)
	}
	if article.Attachments[0].Name != "正文附件.pdf" || article.Attachments[0].URL != "https://bksy.swjtu.edu.cn/__local/body.pdf?e=.pdf" {
		t.Fatalf("unexpected body attachment: %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "外部附件.xlsx" || article.Attachments[1].URL != "https://bksy.swjtu.edu.cn/system/_content/download.jsp?id=fixture" {
		t.Fatalf("unexpected outside attachment: %#v", article.Attachments[1])
	}
}

func TestBksyAdapterRejectsNonArticleResponse(t *testing.T) {
	if _, err := parseAdapter_bksyArticle([]byte(`<html><head><title>404错误提示</title></head><body>404</body></html>`), "https://bksy.swjtu.edu.cn"); err == nil {
		t.Fatal("error page was accepted as an article")
	}
	if _, err := parseAdapter_bksyArticle([]byte(`<div class="news-title">首页壳</div><div id="app"></div>`), "https://bksy.swjtu.edu.cn"); err == nil {
		t.Fatal("homepage shell was accepted as an article")
	}
}

func readBksyFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read bksy fixture %s: %v", name, err)
	}
	return body
}

func TestBksyCapturedLiveHomepageAndReversePages(t *testing.T) {
	items, err := parseAdapter_bksyList(readBksyFixture(t, "bksy_live_home.html"), bksyCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse captured homepage: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("captured homepage returned %d local articles, want 5: %#v", len(items), items)
	}
	for _, item := range items {
		if !bksyArticlePathRE.MatchString(strings.TrimPrefix(item.URL, bksyCanonicalBaseURL)) {
			t.Fatalf("homepage leaked non-article URL: %#v", item)
		}
		if item.Type != "全部" || item.Date == "" || item.PublishedAt == "" {
			t.Fatalf("homepage metadata = %#v", item)
		}
	}

	pages := []struct {
		fixture string
		want    string
	}{
		{"bksy_live_tzgg_qb_first.html", "西南交通大学2025-2026学年第2学期补考交通车时刻表"},
		{"bksy_live_tzgg_qb_middle.html", ""},
		{"bksy_live_tzgg_qb_last.html", ""},
	}
	for _, page := range pages {
		html := readBksyFixture(t, page.fixture)
		items, err := parseAdapter_bksyList(html, bksyCanonicalBaseURL)
		if err != nil {
			t.Fatalf("parse %s: %v", page.fixture, err)
		}
		if len(items) == 0 {
			t.Fatalf("%s produced no local article rows", page.fixture)
		}
		if page.want != "" && items[0].Title != page.want {
			t.Fatalf("%s first title = %q, want %q", page.fixture, items[0].Title, page.want)
		}
	}
	if got := parseTotalPages(readBksyFixture(t, "bksy_live_tzgg_qb_first.html")); got != 243 {
		t.Fatalf("captured tzgg/qb total pages = %d, want 243", got)
	}
	if got := bksyAdapterPageURL(bksyCanonicalBaseURL, "tzgg/qb", 243, 243, ".htm"); got != bksyCanonicalBaseURL+"/tzgg/qb/1.htm" {
		t.Fatalf("last logical page URL = %q", got)
	}
}

func TestBksyCapturedEmptyAndDownloadPagesAreNotArticleFeeds(t *testing.T) {
	for _, fixture := range []string{"bksy_live_empty_tsjy.html", "bksy_live_file_glgzzd.html"} {
		items, err := parseAdapter_bksyList(readBksyFixture(t, fixture), bksyCanonicalBaseURL)
		if err != nil {
			t.Fatalf("parse reachable non-feed %s: %v", fixture, err)
		}
		if len(items) != 0 {
			t.Fatalf("%s leaked non-article rows: %#v", fixture, items)
		}
	}
}

func TestBksyCapturedLiveArticleUsesFixedMetadataAndBodyBoundary(t *testing.T) {
	article, err := parseAdapter_bksyArticle(readBksyFixture(t, "bksy_live_article_94301.html"), bksyCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse captured detail: %v", err)
	}
	if article.Title != "关于开展2026年本科生转专业相关工作的通知" || article.Type != "学籍与注册事务" || article.Source != "本科生院" {
		t.Fatalf("fixed detail metadata = %#v", article)
	}
	if article.Date != "2026-07-08" || article.PublishedAt != "2026-07-08 09:24" {
		t.Fatalf("fixed publication timestamp = (%q, %q)", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "2026年7月20至9月3日") || strings.Contains(article.Content, "阅读量") || strings.Contains(article.Content, "本科生院详情测试通知") {
		t.Fatalf("captured body scope is wrong: %q", article.Content)
	}
	if strings.Contains(article.ContentHTML, "right-news-details") || !strings.Contains(article.ContentHTML, "本科生转专业申请") {
		t.Fatalf("captured ContentHTML is not the article fragment: %q", article.ContentHTML)
	}
	if len(article.Attachments) != 1 || !strings.Contains(article.Attachments[0].Name, "本科生转专业管理办法") {
		t.Fatalf("captured attachment = %#v", article.Attachments)
	}
}

func TestBksyDateOnlyDoesNotInventClockFromBody(t *testing.T) {
	article, err := parseAdapter_bksyArticle(readBksyFixture(t, "bksy_adapter_article_date_only.html"), bksyCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse date-only detail: %v", err)
	}
	if article.Date != "2026-08-07" || article.PublishedAt != "2026-08-07" {
		t.Fatalf("date-only publication = (%q, %q)", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "2026年8月8日 10:00") {
		t.Fatalf("body activity time was lost: %q", article.Content)
	}
}

func TestBksyCaptured404AndSPAShellsAreRejected(t *testing.T) {
	if _, err := parseAdapter_bksyList(readBksyFixture(t, "bksy_live_oob_404.html"), bksyCanonicalBaseURL); err == nil {
		t.Fatal("captured bksy 404 page was accepted as a list")
	}
	if _, err := parseAdapter_bksyArticle(readBksyFixture(t, "bksy_adapter_spa.html"), bksyCanonicalBaseURL); err == nil {
		t.Fatal("bksy SPA shell was accepted as an article")
	}
}

type bksyTestRoundTripper func(*http.Request) (*http.Response, error)

func (f bksyTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func bksyTestHTTPResponse(request *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}
}

func TestBksyFetchArticleVerifiesResourcesAndPreservesExternalLinks(t *testing.T) {
	baseURL := "https://bksy.swjtu.edu.cn"
	articleHTML := strings.ReplaceAll(string(readBksyFixture(t, "bksy_adapter_resources_live.html")), "__BK_BASE__", baseURL)
	oldClient, oldNoCache := defaultClient, NoCache
	oldBaseURL := AllSites["bksy"].BaseURL
	defaultClient = &http.Client{Transport: bksyTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		status, contentType := http.StatusOK, "application/octet-stream"
		var body []byte
		switch request.URL.Path {
		case "/info/1000/1.htm":
			contentType, body = "text/html; charset=utf-8", []byte(articleHTML)
		case "/good.png":
			contentType, body = "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
		case "/bad.png":
			contentType, body = "image/png", []byte("not an image")
		case "/captcha.png":
			contentType, body = "image/png", []byte("<!doctype html><html><body>请输入验证码</body></html>")
		case "/good.pdf":
			contentType, body = "application/pdf", []byte("%PDF-1.7\n")
		case "/bad.pdf":
			contentType, body = "application/pdf", []byte("not a PDF")
		case "/bad-status.pdf":
			status, contentType, body = http.StatusNotFound, "application/pdf", []byte("%PDF-1.7\n")
		case "/captcha.xlsx":
			contentType, body = "text/html; charset=utf-8", []byte("<html><body>验证码下载附件</body></html>")
		case "/external.gif":
			contentType, body = "image/gif", []byte("GIF89a\x00\x00")
		default:
			status, body = http.StatusNotFound, []byte("not found")
		}
		return bksyTestHTTPResponse(request, status, contentType, body), nil
	})}
	NoCache = true
	AllSites["bksy"].BaseURL = baseURL
	t.Cleanup(func() {
		defaultClient, NoCache = oldClient, oldNoCache
		AllSites["bksy"].BaseURL = oldBaseURL
		ClearCache()
	})
	ClearCache()

	article, raw, err := AllSites["bksy"].GetArticleSnapshot(baseURL + "/info/1000/1.htm")
	if err != nil {
		t.Fatalf("fetch bksy article: %v", err)
	}
	if len(raw) == 0 || article.Title != "资源验证通知" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 09:27:00" {
		t.Fatalf("unexpected live article result: article=%+v body=%d", article, len(raw))
	}
	if len(article.Images) != 2 || article.Images[0] != baseURL+"/good.png" || article.Images[1] != "https://cdn.example.test/external.gif" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != baseURL+"/good.pdf" || article.Attachments[0].Size != "9" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, baseURL+"/good.png") || !strings.Contains(article.ContentHTML, baseURL+"/good.pdf") || !strings.Contains(article.ContentHTML, "https://outside.example/guide") || !strings.Contains(article.ContentHTML, "https://cdn.example.test/external.gif") {
		t.Fatalf("valid resources/external link missing from ContentHTML: %q", article.ContentHTML)
	}
	for _, rejected := range []string{"bad.png", "captcha.png", "bad.pdf", "bad-status.pdf", "captcha.xlsx", "javascript:download"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource remained in ContentHTML: %q", rejected)
		}
	}

	if got := bksyVerifyAttachments([]model.Attachment{{Name: "有效.pdf", URL: baseURL + "/good.pdf"}, {Name: "错误.pdf", URL: baseURL + "/bad.pdf"}}, baseURL); len(got) != 1 || got[0].URL != baseURL+"/good.pdf" {
		t.Fatalf("direct attachment verification = %#v", got)
	}
}
