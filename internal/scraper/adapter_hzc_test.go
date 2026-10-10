package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func hzcFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return body
}

func TestHzcRegistersAuditedFeedsAndReversePagination(t *testing.T) {
	site, ok := AllSites["hzc"]
	if !ok {
		t.Fatal("hzc site is not registered")
	}
	want := []string{
		"xwdt/zx", "xwdt/zh", "tzgs", "xzzq/xzzq",
		"dwhz/hzqk", "dwhz/zdwj", "jxjy/xxgs", "jxjy/zdwj",
		"ddbf/rwfc", "zbjs/djyd",
	}
	if !reflect.DeepEqual(site.SubPages, want) || site.ArticleFetchFunc == nil {
		t.Fatalf("hzc feeds = %#v, want %#v", site.SubPages, want)
	}
	if site.BaseURL != hzcCanonicalBaseURL || site.PageURL == nil || site.ListPageFunc == nil {
		t.Fatalf("hzc registration lost canonical URL or pagination hooks: %#v", site)
	}
	if got := hzcPageURL(hzcCanonicalBaseURL, "xwdt/zx", 1, 38, ".htm"); got != "https://hzc.swjtu.edu.cn/xwdt/zx.htm" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := hzcPageURL(hzcCanonicalBaseURL, "xwdt/zx", 2, 38, ".htm"); got != "https://hzc.swjtu.edu.cn/xwdt/zx/37.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
	if got := hzcPageURL(hzcCanonicalBaseURL, "xwdt/zx", 38, 38, ".htm"); got != "https://hzc.swjtu.edu.cn/xwdt/zx/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := hzcPaginationTotal(parseHzcDocument(t, "hzc_list.html")); got != 38 {
		t.Fatalf("pagination total = %d, want 38", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 38},
		{39, 38},
		{2, 0},
	} {
		if got := hzcPageURL(hzcCanonicalBaseURL, "xwdt/zx", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
	if hzcKnownFeed("xwdt/dw") {
		t.Fatal("external-only xwdt/dw was registered as a local article feed")
	}
}

func parseHzcDocument(t *testing.T, name string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(hzcFixture(t, name))))
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return doc
}

func TestHzcHomepageParsesVisibleAndHiddenModules(t *testing.T) {
	items, err := parseAdapter_hzcList(hzcFixture(t, "hzc_home.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("homepage items = %d, want 4 local article entries", len(items))
	}
	wantTypes := map[string]string{
		"https://hzc.swjtu.edu.cn/info/1039/19817.htm": "新闻动态",
		"https://hzc.swjtu.edu.cn/info/1039/19807.htm": "新闻动态",
		"https://hzc.swjtu.edu.cn/info/1052/19637.htm": "通知公告",
		"https://hzc.swjtu.edu.cn/info/1031/19707.htm": "下载专区",
	}
	for _, item := range items {
		if got := wantTypes[item.URL]; got == "" || item.Type != got {
			t.Errorf("homepage item %q type = %q, want %q", item.URL, item.Type, got)
		}
		if item.URL != "https://hzc.swjtu.edu.cn/info/1031/19707.htm" && (item.Date == "" || item.PublishedAt == "") {
			t.Errorf("homepage item %q lost date precision: %#v", item.URL, item)
		}
		if strings.Contains(item.URL, "news.swjtu") || strings.HasSuffix(item.URL, ".doc") {
			t.Errorf("homepage retained an external/direct resource: %#v", item)
		}
	}
}

func TestHzcListParsesDlRowsMetadataAndFiltersNonArticles(t *testing.T) {
	items, err := parseAdapter_hzcList(hzcFixture(t, "hzc_list.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("list items = %d, want 3 local article rows", len(items))
	}
	if items[0].Title != "完整的列表标题一" || items[0].Date != "2026-07-15" || items[0].PublishedAt != "2026-07-15 12:34:56" || items[0].Type != "最新" {
		t.Fatalf("first item metadata = %#v", items[0])
	}
	if items[1].Date != "2026-06-10" || items[1].Type != "对外" || items[2].Type != "综合" {
		t.Fatalf("row metadata = %#v", items)
	}
}

func TestHzcEmptyListIsAllowedButStaticPageIsRejected(t *testing.T) {
	items, err := parseAdapter_hzcList(hzcFixture(t, "hzc_empty.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatalf("legal empty list rejected: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list returned items: %#v", items)
	}
	if _, err := parseAdapter_hzcList(hzcFixture(t, "hzc_static.html"), hzcCanonicalBaseURL); err == nil {
		t.Fatal("static introduction page was accepted as article list")
	}
}

func TestHzcArticleExtractsMetadataBodyImagesAndOutsideAttachments(t *testing.T) {
	article, err := parseAdapter_hzcArticle(hzcFixture(t, "hzc_article.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "测试文章" || article.Date != "2026-07-15" || article.PublishedAt != "2026-07-15 12:34:56" || article.Type != "最新" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || strings.Contains(article.Content, "2026年9月16日，不是发布日期") == false {
		t.Fatalf("article body was not retained: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://hzc.swjtu.edu.cn/images/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://hzc.swjtu.edu.cn/images/body.png"`) {
		t.Fatalf("content HTML did not resolve body image: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if article.Attachments[0].URL != "https://hzc.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=2054591004&wbfileid=ABC" || article.Attachments[1].URL != "https://hzc.swjtu.edu.cn/files/guide.pdf" {
		t.Fatalf("attachment URLs were not resolved: %#v", article.Attachments)
	}
}

func TestHzcRejectsCaptchaAndErrorResponsesAsArticles(t *testing.T) {
	for _, fixture := range []string{"hzc_captcha.html", "hzc_error.html", "hzc_static.html"} {
		if _, err := parseAdapter_hzcArticle(hzcFixture(t, fixture), hzcCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as an article", fixture)
		}
	}
}

func TestHzcDateOnlyDoesNotBorrowBodyDeadline(t *testing.T) {
	article, err := parseAdapter_hzcArticle(hzcFixture(t, "hzc_article_date_only.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-07-15" || article.PublishedAt != "2026-07-15" {
		t.Fatalf("date-only metadata was changed: %#v", article)
	}
	if strings.Contains(article.PublishedAt, "00:00:00") || strings.Contains(article.PublishedAt, "2026-09-16") {
		t.Fatalf("body date contaminated publication timestamp: %#v", article)
	}
}

func TestHzcResourceBodyChecksContentTypeMagicAndShell(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        []byte
		resource    string
		image       bool
		want        bool
	}{
		{"valid png", "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "good.png", true, true},
		{"wrong image magic", "image/png", []byte{0xff, 0xd8, 0xff}, "bad.png", true, false},
		{"valid pdf", "application/pdf", []byte("%PDF-1.7"), "good.pdf", false, true},
		{"wrong pdf magic", "application/pdf", []byte("plain text"), "bad.pdf", false, false},
		{"captcha shell", "application/pdf", []byte("<!doctype html><p>请输入验证码下载附件</p>"), "captcha.pdf", false, false},
		{"missing type", "", []byte("%PDF-1.7"), "good.pdf", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hzcResourceBodyIsUsable(tc.contentType, tc.body, tc.resource, tc.image); got != tc.want {
				t.Fatalf("resource usability = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHzcFetchArticleVerifiesResourcesAndSanitizesHTML(t *testing.T) {
	page := hzcFixture(t, "hzc_article_resources.html")
	detailPath := "/info/1039/9001.htm"
	detailURL := ""
	var resourceRequests int
	var badReferer bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == detailPath {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
			return
		}
		resourceRequests++
		if r.Referer() == "" || r.Referer() != detailURL {
			badReferer = true
		}
		switch r.URL.Path {
		case "/images/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		case "/images/bad.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>captcha</html>"))
		case "/files/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7 valid"))
		case "/files/wrong.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("not a pdf"))
		case "/system/_content/download.jsp":
			if r.URL.Query().Get("wbfileid") != "GOOD" {
				http.Error(w, "missing", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte{'P', 'K', 3, 4, 'o', 'k'})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	detailURL = server.URL + detailPath

	article, raw, err := hzcFetchArticleAt(detailURL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || resourceRequests != 6 || badReferer {
		t.Fatalf("resource validation requests = %d, bad referer=%v, raw=%d", resourceRequests, badReferer, len(raw))
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/images/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, "/images/good.png") || !strings.Contains(article.ContentHTML, "https://example.com/read-more") {
		t.Fatalf("valid resources/external link missing: %s", article.ContentHTML)
	}
	for _, rejected := range []string{"bad.png", "wrong.pdf", "CAPTCHA", "missing.pdf"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource %q survived: %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.Content, "正文保留") || strings.Contains(article.Content, "伪附件") {
		t.Fatalf("sanitized article text = %q", article.Content)
	}
	if article.Attachments[0].Size == "" || article.Attachments[1].Size == "" {
		t.Fatalf("verified attachment sizes missing: %#v", article.Attachments)
	}
}

func TestHzcFetchResourceRejectsNon2xxAndCrossOriginRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing.pdf":
			http.NotFound(w, r)
		case "/redirect.pdf":
			http.Redirect(w, r, "https://example.com/file.pdf", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7"))
		}
	}))
	defer server.Close()
	for _, path := range []string{"/missing.pdf", "/redirect.pdf"} {
		if _, _, err := hzcFetchResource(server.URL+path, server.URL, server.URL+"/info/1039/9001.htm", fmt.Sprintf("%s", path), false); err == nil {
			t.Errorf("resource %s was accepted", path)
		}
	}
}

func TestHzcZxLivePagesKeepLocalRowsAndAllowExternalOnly(t *testing.T) {
	first := parseHzcDocument(t, "hzc_live_zx_first.html")
	if got := hzcPaginationTotal(first); got != 39 {
		t.Fatalf("live zx pagination total = %d, want 39", got)
	}
	items, err := parseAdapter_hzcList(hzcFixture(t, "hzc_live_zx_first.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("live zx first page kept %d local rows, want 5 of 10: %#v", len(items), items)
	}
	for _, item := range items {
		if !strings.Contains(item.URL, "hzc.swjtu.edu.cn/info/") {
			t.Fatalf("external row leaked into local feed: %#v", item)
		}
	}

	// Physical page 35 (logical 5/39) consists entirely of news.swjtu.edu.cn
	// rows. It must parse as a legal empty page, not fail the whole crawl.
	externalOnly, err := parseAdapter_hzcList(hzcFixture(t, "hzc_live_zx_external_only.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatalf("external-only live page was rejected: %v", err)
	}
	if len(externalOnly) != 0 {
		t.Fatalf("external-only live page returned items: %#v", externalOnly)
	}

	mixed, err := parseAdapter_hzcList(hzcFixture(t, "hzc_live_zx_mixed.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(mixed) != 3 {
		t.Fatalf("mixed live page kept %d local rows, want 3: %#v", len(mixed), mixed)
	}
	last, err := parseAdapter_hzcList(hzcFixture(t, "hzc_live_zx_last.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(last) != 1 {
		t.Fatalf("last live page items = %#v, want 1 local row", last)
	}
}

// hzcRenderListPage builds a minimal but faithful hzc list page: pageType=2,
// a .conlistComm of dl rows, and reverse-numbered pagination markup.
// localTitles are rendered as numeric ../../info/1039/ rows; externalTitles
// as absolute news.swjtu.edu.cn rows.
func hzcRenderListPage(current, total int, localTitles, externalTitles []string) string {
	var rows strings.Builder
	writeRow := func(tab, href, title string) {
		fmt.Fprintf(&rows, `<dl><dt><span> <a href="../%s.htm">[%s]</a></span>`+
			`<a target="_blank " title="%s" href="%s">%s</a></dt>`+
			`<dd>时间：2025-11-19 &nbsp;&nbsp;&nbsp;阅读次数：</dd></dl>`, tab, tab, title, href, title)
	}
	for i, title := range localTitles {
		writeRow("最新", fmt.Sprintf("../../info/1039/195%02d.htm", current*10+i), title)
	}
	for i, title := range externalTitles {
		writeRow("对外", fmt.Sprintf("https://news.swjtu.edu.cn/info/1011/90%03d.htm", i), title)
	}
	pageLinks := fmt.Sprintf(`<span class="p_no_d">%d</span>`, current)
	if current > 1 {
		pageLinks = fmt.Sprintf(`<span class="p_no"><a href="%d.htm">%d</a></span>`, total-current+1, current)
	}
	return fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="UTF-8">`+
		`<meta name="pageType" content="2"><title>最新-合作发展处</title></head><body>`+
		`<div class="current"><h3>最新</h3></div><div class="conlistComm">%s</div>`+
		`<div class="pb_sys_common"><span class="p_pages">%s`+
		`<span class="p_dot">...</span><span class="p_no"><a href="1.htm">%d</a></span>`+
		`<span class="p_last p_fun"><a href="1.htm">尾页</a></span></span></div>`+
		`</body></html>`, rows.String(), pageLinks, total)
}

func TestHzcListPageCompactsLogicalPagesAcrossExternalOnlyPages(t *testing.T) {
	// Five physical pages; position 2 (file 4.htm) is external-only, exactly
	// like the live xwdt/zx archive. Local articles exist before AND after it.
	pages := map[string]string{
		"/xwdt/zx.htm":   hzcRenderListPage(1, 5, []string{"本地消息一", "本地消息二"}, nil),
		"/xwdt/zx/4.htm": hzcRenderListPage(2, 5, nil, []string{"外站消息甲", "外站消息乙"}),
		"/xwdt/zx/3.htm": hzcRenderListPage(3, 5, []string{"本地消息三"}, []string{"外站消息丙"}),
		"/xwdt/zx/2.htm": hzcRenderListPage(4, 5, []string{"本地消息四", "本地消息五"}, nil),
		"/xwdt/zx/1.htm": hzcRenderListPage(5, 5, []string{"本地消息六"}, nil),
	}
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		requests++
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	site := AllSites["hzc"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	site.BaseURL, NoCache = server.URL, true
	t.Cleanup(func() {
		site.BaseURL, NoCache = oldBase, oldNoCache
		ClearCache()
	})

	items, err := site.ListNewsRange("xwdt/zx", "2000-01-01", "2099-12-31", 0)
	if err != nil {
		t.Fatalf("range crawl across an external-only page failed: %v", err)
	}
	wantTitles := []string{"本地消息一", "本地消息二", "本地消息三", "本地消息四", "本地消息五", "本地消息六"}
	if len(items) != len(wantTitles) {
		t.Fatalf("range crawl kept %d items, want %d: %#v", len(items), len(wantTitles), items)
	}
	for i, want := range wantTitles {
		if items[i].Title != want {
			t.Fatalf("item %d title = %q, want %q (order lost a local page)", i, items[i].Title, want)
		}
	}
	if requests != len(pages) {
		t.Fatalf("server requests = %d, want exactly %d (each physical page fetched once)", requests, len(pages))
	}

	// Logical page numbers compact onto pages with local articles: page 2 is
	// physical position 3, and asking past the archive end is a legal empty
	// page rather than an error.
	page2, err := site.ListNewsPage("xwdt/zx", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 1 || page2[0].Title != "本地消息三" {
		t.Fatalf("compacted logical page 2 = %#v, want 本地消息三", page2)
	}
	beyond, err := site.ListNewsPage("xwdt/zx", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(beyond) != 0 {
		t.Fatalf("page past the archive end = %#v, want empty", beyond)
	}
	if _, err := site.ListNewsPage("xwdt/zx", 0); err == nil {
		t.Fatal("page 0 was accepted")
	}
	if _, err := site.ListNewsPage("not-a-feed", 1); err == nil {
		t.Fatal("unknown route was accepted")
	}
}
