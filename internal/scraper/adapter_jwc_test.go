package scraper

import (
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

func jwcFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestJWCAdapterRegistersAllArticleFeeds(t *testing.T) {
	site := AllSites["jwc"]
	if site == nil || site.BaseURL != jwcCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.ListPageFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("jwc adapter is incomplete: %#v", site)
	}
	if len(jwcSubPages) != 55 {
		t.Fatalf("got %d jwc feeds, want 55", len(jwcSubPages))
	}
	if !reflect.DeepEqual(site.SubPages, jwcSubPages) {
		t.Fatalf("site sub-pages differ from jwc adapter routes")
	}
	for _, slug := range jwcSubPages {
		if _, ok := jwcFindRoute(slug); !ok {
			t.Errorf("sub-page %q has no route definition", slug)
		}
	}
	for _, excluded := range []string{"rules", "files", "service/login", "CourseAction", "PublicInfoQueryAction", "jiaowu.swjtu.edu.cn"} {
		if containsJWCFeed(jwcSubPages, excluded) {
			t.Errorf("non-feed %q was registered as a jwc feed", excluded)
		}
	}
}

func containsJWCFeed(feeds []string, want string) bool {
	for _, feed := range feeds {
		if feed == want || strings.Contains(feed, want) {
			return true
		}
	}
	return false
}

func TestJWCListURLUsesTheObservedVATUURouteKinds(t *testing.T) {
	tests := []struct {
		slug       string
		page       int
		pageParam  string
		wantBig    string
		wantSmall  string
		wantSelect string
	}{
		{slug: "all", page: 1},
		{slug: "news", page: 1, wantBig: "FB2D32BEAD066637", wantSelect: "bigType"},
		{slug: "quality/teacher-development", page: 2, pageParam: "pageNum", wantSmall: "B4F513430B5EBC91", wantSelect: "smallType"},
	}
	for _, tc := range tests {
		raw, err := jwcListURL(jwcCanonicalBaseURL, tc.slug, tc.page, tc.pageParam)
		if err != nil {
			t.Fatalf("%s: %v", tc.slug, err)
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if query.Get("setAction") != "newsList" || parsed.Path != "/vatuu/WebAction" {
			t.Errorf("%s URL = %s", tc.slug, raw)
		}
		if query.Get("bigTypeId") != tc.wantBig || query.Get("keyWord") != tc.wantSmall || query.Get("selectType") != tc.wantSelect {
			t.Errorf("%s query = %#v", tc.slug, query)
		}
		if tc.page == 1 && (query.Get("pageNum") != "" || query.Get("currentPage") != "") {
			t.Errorf("page one unexpectedly has a page parameter: %s", raw)
		}
		if tc.page > 1 && query.Get(tc.pageParam) != "2" {
			t.Errorf("page %d URL = %s", tc.page, raw)
		}
	}
	if _, err := jwcListURL(jwcCanonicalBaseURL, "management-rules", 1, ""); err == nil {
		t.Fatal("unknown/static route unexpectedly accepted")
	}
}

func TestJWCListParserKeepsCompleteRowsAndPrecision(t *testing.T) {
	items, err := parseAdapter_jwcList(jwcFixture(t, "jwc_list.html"), jwcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want 2: %#v", len(items), items)
	}
	if items[0].Title != "关于做好2026-2027学年第一学期选课工作的通知（完整列表标题）" || items[0].Date != "2026-07-02" || items[0].PublishedAt != "2026-07-02 08:45:10" || items[0].Type != "排课选课事务" {
		t.Errorf("first item = %#v", items[0])
	}
	if items[1].Title != "2025—2026学年第二学期期末考试情况通报（一）完整标题" || items[1].Date != "2026-06-15" || items[1].PublishedAt != "2026-06-15 17:35:18" || items[1].Type != "要闻及动态" {
		t.Errorf("second item = %#v", items[1])
	}
	for _, item := range items {
		if strings.Contains(item.URL, "news.swjtu.edu.cn") || strings.Contains(item.URL, "setAction=download") {
			t.Errorf("non-local article/resource leaked into list: %#v", item)
		}
	}
	if got := jwcPageParamFromHTML(jwcFixture(t, "jwc_list.html"), 2); got != "pageNum" {
		t.Fatalf("detected page parameter = %q, want pageNum", got)
	}
}

func TestJWCPaginationAndLegalEmptyList(t *testing.T) {
	info, err := jwcPaginationFromHTML(jwcFixture(t, "jwc_list.html"))
	if err != nil {
		t.Fatal(err)
	}
	if info.totalItems != 123 || info.current != 2 || info.perPage != 50 || info.totalPages != 3 {
		t.Fatalf("pagination = %#v", info)
	}
	empty, err := parseAdapter_jwcList(jwcFixture(t, "jwc_empty.html"), jwcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("legal empty list = %#v", empty)
	}
	emptyInfo, err := jwcPaginationFromHTML(jwcFixture(t, "jwc_empty.html"))
	if err != nil || emptyInfo.totalPages != 1 {
		t.Fatalf("empty pagination = %#v, err=%v", emptyInfo, err)
	}
}

func TestJWCRejectsHomepageStaticErrorAndLoginShells(t *testing.T) {
	for _, fixture := range []string{"jwc.html", "jwc_static.html", "jwc_error.html", "jwc_login.html"} {
		items, err := parseAdapter_jwcList(jwcFixture(t, fixture), jwcCanonicalBaseURL)
		if err == nil || items != nil {
			t.Errorf("%s: items=%#v err=%v; shell/error must not be success", fixture, items, err)
		}
	}
	if _, err := parseAdapter_jwcArticle(jwcFixture(t, "jwc_error.html"), jwcCanonicalBaseURL); err == nil {
		t.Fatal("error page accepted as article")
	}
	if _, _, err := jwcFetchArticle("https://news.swjtu.edu.cn/info/1011/108875.htm"); err == nil {
		t.Fatal("external article redirect/URL accepted")
	}
}

func TestJWCArticleExtractsMetadataBodyImagesAndOnlyFileLinks(t *testing.T) {
	article, err := parseAdapter_jwcArticle(jwcFixture(t, "jwc_article.html"), jwcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于做好2026-2027学年第一学期选课工作的通知" || article.Source != "本科生院" || article.Type != "排课选课事务" || article.Date != "2026-07-02" || article.PublishedAt != "2026-07-02 08:45:10" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "现将2026-2027学年第一学期选课工作的安排通知如下") || !strings.Contains(article.Content, "选课") {
		t.Fatalf("article body lost text/table content: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != jwcCanonicalBaseURL+"/images/1782952979192063301.jpg" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	want := map[string]string{
		"附件一：移动端选课使用操作手册.pdf": jwcCanonicalBaseURL + "/vatuu/WebAction?fileId=PDF-FIXTURE&fileType=file&setAction=download",
		"选课指南.docx": jwcCanonicalBaseURL + "/files/selection-guide.docx",
	}
	for _, attachment := range article.Attachments {
		if want[attachment.Name] != attachment.URL {
			t.Errorf("unexpected attachment = %#v", attachment)
		}
		delete(want, attachment.Name)
	}
	if len(want) != 0 {
		t.Errorf("missing attachments = %#v", want)
	}
	for _, attachment := range article.Attachments {
		if strings.Contains(attachment.URL, "yhxt.swjtu.edu.cn") || strings.Contains(attachment.URL, "error.html") {
			t.Errorf("non-file HTML/external body link was retained as an attachment: %#v", attachment)
		}
	}
	if !strings.Contains(article.ContentHTML, "fileId=PDF-FIXTURE") {
		t.Errorf("fileType=file download link was removed from content HTML: %s", article.ContentHTML)
	}
	if strings.Contains(article.ContentHTML, "yhxt.swjtu.edu.cn") || strings.Contains(article.ContentHTML, "error.html") {
		t.Errorf("unsafe external/HTML body link was retained in content HTML: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, `src="https://jwc.swjtu.edu.cn/images/1782952979192063301.jpg"`) {
		t.Errorf("image URL was not resolved in content HTML: %s", article.ContentHTML)
	}
}

func TestJWCAttachmentResponseRejectsHTMLAndVerificationShells(t *testing.T) {
	valid := []struct {
		contentType string
		body        []byte
	}{
		{contentType: "application/pdf", body: []byte("%PDF-1.7\nfixture")},
		{contentType: "application/octet-stream", body: []byte{0x50, 0x4b, 0x03, 0x04, 0x01}},
		{contentType: "text/plain", body: []byte("这是一个真实的文本附件")},
	}
	for _, tc := range valid {
		if !jwcAttachmentBodyIsUsable(tc.contentType, tc.body) {
			t.Errorf("valid attachment rejected: content_type=%q body=%q", tc.contentType, tc.body)
		}
	}
	invalid := []struct {
		contentType string
		body        []byte
	}{
		{contentType: "text/html; charset=utf-8", body: []byte("<html><body>403</body></html>")},
		{contentType: "application/octet-stream", body: []byte("<!doctype html><h1>请输入验证码下载附件</h1>")},
		{contentType: "application/json", body: []byte(`{"error":"not authorized"}`)},
		{contentType: "application/pdf", body: []byte("<html><title>Cloudflare</title></html>")},
		{contentType: "application/pdf", body: nil},
	}
	for _, tc := range invalid {
		if jwcAttachmentBodyIsUsable(tc.contentType, tc.body) {
			t.Errorf("error shell accepted: content_type=%q body=%q", tc.contentType, tc.body)
		}
	}
}
