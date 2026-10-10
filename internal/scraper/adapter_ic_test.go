package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const icTestBaseURL = "https://ic.swjtu.edu.cn"

func loadICFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read IC fixture %s: %v", name, err)
	}
	return body
}

func TestICAdapterRegistrationAndFeeds(t *testing.T) {
	site := AllSites["ic"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil {
		t.Fatal("IC adapter is not fully registered")
	}
	want := []string{"xwzx", "xwzx/tzgg"}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("IC feeds = %#v, want %#v", site.SubPages, want)
	}
	if site.BaseURL != icTestBaseURL {
		t.Fatalf("IC base URL = %q, want %q", site.BaseURL, icTestBaseURL)
	}
}

func TestICPageURLUsesReverseGeneratedFiles(t *testing.T) {
	base := icTestBaseURL
	want := map[string]string{
		"first":  base + "/xwzx/xwjj.htm",
		"second": base + "/xwzx/xwjj/2.htm",
		"middle": base + "/xwzx/xwjj/2.htm",
		"last":   base + "/xwzx/xwjj/1.htm",
	}
	got := map[string]string{
		"first":  icPageURL(base, "xwzx", 1, 3, ".htm"),
		"second": icPageURL(base, "xwzx", 2, 3, ".htm"),
		"middle": icPageURL(base, "xwzx", 2, 3, ".htm"),
		"last":   icPageURL(base, "xwzx", 3, 3, ".htm"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reverse page URLs = %#v, want %#v", got, want)
	}
	if got := icPageURL(base, "index", 1, 0, ".htm"); got != base+"/index.htm" {
		t.Fatalf("index page URL = %q, want %s", got, base+"/index.htm")
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 3},
		{4, 3},
		{2, 0},
	} {
		if got := icPageURL(base, "xwzx", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
	if _, _, err := icListPage("xwzx", 0); err == nil {
		t.Fatal("IC custom list accepted page zero")
	}
}

func TestICListPageFetchesFirstMiddleLastAndRejectsOutOfRange(t *testing.T) {
	fixtures := map[string][]byte{
		"/xwzx/xwjj.htm": loadICFixture(t, "ic_list_first.html"),
		"/xwzx/xwjj/2.htm": loadICFixture(t, "ic_list_middle.html"),
		"/xwzx/xwjj/1.htm": loadICFixture(t, "ic_list_last.html"),
		"/xwzx/xwjj/0.htm": loadICFixture(t, "ic_list_out_of_range.html"),
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		body, ok := fixtures[r.URL.Path]
		if !ok || r.URL.Path == "/xwzx/xwjj/0.htm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	site := AllSites["ic"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	t.Cleanup(func() { site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache })

	wantIDs := []string{"4001", "4002", "4003"}
	for page, wantID := range wantIDs {
		items, total, err := icListPage("xwzx", page+1)
		if err != nil {
			t.Fatalf("logical page %d: %v", page+1, err)
		}
		if total != 3 || len(items) != 1 {
			t.Fatalf("logical page %d result = total %d items %#v, want total 3 and one item", page+1, total, items)
		}
		if !strings.HasSuffix(items[0].URL, "/info/1061/"+wantID+".htm") {
			t.Fatalf("logical page %d URL = %q, want article %s", page+1, items[0].URL, wantID)
		}
	}
	if _, total, err := icListPage("xwzx", 4); err == nil || total != 3 || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("out-of-range page result = total %d err %v", total, err)
	}
	if _, _, err := icListPage("gywm", 1); err == nil || !strings.Contains(err.Error(), "non-article feed") {
		t.Fatalf("static route was accepted: %v", err)
	}
	for _, path := range requests {
		if path == "/xwzx/xwjj/0.htm" {
			t.Fatal("out-of-range page requested physical /0.htm")
		}
	}
}

func TestICHomepageParsesAllArticleModulesAndFiltersExternalLinks(t *testing.T) {
	items, err := parseAdapter_icList(loadICFixture(t, "ic.html"), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 9 {
		t.Fatalf("got %d homepage articles, want 9 (6 news + 3 local notice rows): %#v", len(items), items)
	}
	seen := make(map[string]bool)
	for _, item := range items {
		if seen[item.URL] {
			t.Fatalf("duplicate homepage URL %q", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, icTestBaseURL+"/info/") {
			t.Fatalf("external or non-article homepage URL %q", item.URL)
		}
		if item.Title == "" || item.Date == "" || item.Type == "" {
			t.Fatalf("incomplete homepage metadata: %+v", item)
		}
	}
	if !seen[icTestBaseURL+"/info/1061/2031.htm"] || !seen[icTestBaseURL+"/info/1091/1131.htm"] {
		t.Fatalf("visible homepage modules were not retained: %#v", seen)
	}
	if seen["https://iscit.swjtu.edu.cn/info/1102/8490.htm"] {
		t.Fatal("external ISCit notice leaked into IC feed")
	}
}

func TestICListParsesRowsAndExactPageCount(t *testing.T) {
	body := loadICFixture(t, "ic_list.html")
	items, err := parseAdapter_icList(body, icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 10 {
		t.Fatalf("got %d list items, want 10: %#v", len(items), items)
	}
	if items[0].Title != "东部校区第一条新闻" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31 14:20:11" {
		t.Fatalf("first item metadata = %+v", items[0])
	}
	if items[0].Type != "新闻动态" {
		t.Fatalf("list item type = %q, want 新闻动态", items[0].Type)
	}
	total, err := icTotalPagesFromHTML(body, "xwzx")
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total pages = %d, want 3 from 共23条/10 rows", total)
	}
}

func TestICPageCountUsesSourceRowsWhenExternalRowsAreFiltered(t *testing.T) {
	body := string(loadICFixture(t, "ic_list.html"))
	body = strings.Replace(body, "href=\"info/1061/", "href=\"https://external.example/info/1061/", 5)
	items, err := parseAdapter_icList([]byte(body), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("filtered list items = %d, want 5", len(items))
	}
	total, err := icTotalPagesFromHTML([]byte(body), "xwzx")
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total pages = %d, want 3 from 23 source rows/10 rows, not parsed-row count", total)
	}
}

func TestICExplicitEmptyAndResourcePagesAreNotArticles(t *testing.T) {
	items, err := parseAdapter_icList(loadICFixture(t, "ic_empty.html"), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("explicit empty list = %#v, want non-nil empty slice", items)
	}
	if _, err := parseAdapter_icList(loadICFixture(t, "ic_resource.html"), icTestBaseURL); err == nil {
		t.Fatal("resource/download page was accepted as an article list")
	}
}

func TestICRejectsWAFAndNonArticleShells(t *testing.T) {
	if _, err := parseAdapter_icList(loadICFixture(t, "ic_waf.html"), icTestBaseURL); err == nil {
		t.Fatal("WAF shell was accepted as a list")
	}
	if _, err := parseAdapter_icArticle(loadICFixture(t, "ic_waf.html"), icTestBaseURL); err == nil {
		t.Fatal("WAF shell was accepted as an article")
	}
	if _, err := parseAdapter_icArticle(loadICFixture(t, "ic.html"), icTestBaseURL); err == nil {
		t.Fatal("homepage shell was accepted as an article")
	}
}

func TestICArticleExtractsMetadataBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_icArticle(loadICFixture(t, "ic_article.html"), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "东部校区建设工作推进会" || article.Type != "新闻动态" {
		t.Fatalf("title/type = %q/%q", article.Title, article.Type)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:20:11" {
		t.Fatalf("date/published_at = %q/%q", article.Date, article.PublishedAt)
	}
	if article.Source != "东部校区管委会" {
		t.Fatalf("source = %q", article.Source)
	}
	if !strings.Contains(article.Content, "正文第一段") || strings.Contains(article.Content, "下一篇") {
		t.Fatalf("body extraction is inaccurate: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != icTestBaseURL+"/__local/ic-body.png" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("attachments = %#v, want body + outside attachment", article.Attachments)
	}
	if article.Attachments[0].Name != "会议通知.pdf" || article.Attachments[0].URL != icTestBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&wbfileid=ICFIX" {
		t.Fatalf("body attachment = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "附件2：校区安排.xlsx" || article.Attachments[1].URL != icTestBaseURL+"/__local/plan.xlsx" {
		t.Fatalf("outside attachment = %#v", article.Attachments[1])
	}
	if !strings.Contains(article.ContentHTML, `src="https://ic.swjtu.edu.cn/__local/ic-body.png"`) || !strings.Contains(article.ContentHTML, `href="https://ic.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&amp;wbfileid=ICFIX"`) {
		t.Fatalf("resolved resources not retained in content HTML: %s", article.ContentHTML)
	}
}

func TestICArticleDateOnlyDoesNotInventClockOrUseBodyDates(t *testing.T) {
	article, err := parseAdapter_icArticle(loadICFixture(t, "ic_article_date_only.html"), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-08-30" || article.PublishedAt != "2026-08-30" {
		t.Fatalf("date-only metadata = date %q published_at %q", article.Date, article.PublishedAt)
	}
	if strings.Contains(article.PublishedAt, "00:00:00") || strings.Contains(article.PublishedAt, "2027") {
		t.Fatalf("publication clock/body event leaked into metadata: %q", article.PublishedAt)
	}
	if !strings.Contains(article.Content, "活动时间：2027年9月16日") {
		t.Fatalf("fixed article body was not retained: %q", article.Content)
	}
}

func TestICFetchArticleVerifiesAndSanitizesResources(t *testing.T) {
	articleHTML := loadICFixture(t, "ic_article_resources.html")
	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/info/1061/4101.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(articleHTML)
		case "/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(validPNG)
		case "/bad.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("not an image"))
		case "/captcha.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>请输入验证码</html>"))
		case "/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\nfixture"))
		case "/bad.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("not a PDF"))
		case "/captcha.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<html><body>验证码下载附件</body></html>"))
		case "/missing.pdf":
			http.NotFound(w, r)
		case "/large.bin":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.FormatInt(icMaxResourceBytes+1, 10))
			_, _ = w.Write([]byte("too large"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["ic"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	t.Cleanup(func() { site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache })

	article, raw, err := icFetchArticle(server.URL + "/info/1061/4101.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:20:11" {
		t.Fatalf("raw/date metadata = %d/%#v", len(raw), article)
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != server.URL+"/good.pdf" || article.Attachments[0].Size != strconv.Itoa(len("%PDF-1.7\nfixture")) {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	for _, rejected := range []string{"bad.png", "captcha.png", "bad.pdf", "captcha.pdf", "missing.pdf"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource %q survived sanitization: %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, server.URL+"/good.png") || !strings.Contains(article.ContentHTML, server.URL+"/good.pdf") || !strings.Contains(article.ContentHTML, "https://example.com/reference") {
		t.Fatalf("valid resource or external normal link was lost: %s", article.ContentHTML)
	}
	if _, _, err := icFetchResource(server.URL+"/large.bin", server.URL, server.URL+"/info/1061/4101.htm", false); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized resource error = %v", err)
	}
	if len(requests) == 0 {
		t.Fatal("resource/article test made no requests")
	}
}

func TestICResourceBodyValidationChecksContentTypeMagicAndShells(t *testing.T) {
	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	tests := []struct {
		name        string
		contentType string
		body        []byte
		resource    string
		image       bool
		want        bool
	}{
		{"valid png", "image/png", validPNG, "good.png", true, true},
		{"wrong image magic", "image/png", []byte("not png"), "bad.png", true, false},
		{"missing content type", "", []byte("%PDF-1.7"), "good.pdf", false, false},
		{"valid pdf", "application/pdf; charset=binary", []byte("%PDF-1.7"), "good.pdf", false, true},
		{"captcha html", "application/pdf", []byte("<html>请输入验证码下载附件</html>"), "bad.pdf", false, false},
		{"json error", "application/json", []byte(`{"error":"denied"}`), "bad.pdf", false, false},
		{"valid docx zip", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte{'P', 'K', 0x03, 0x04}, "good.docx", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := icResourceBodyIsUsable(tc.contentType, tc.body, tc.resource, tc.image); got != tc.want {
				t.Fatalf("usable = %v, want %v", got, tc.want)
			}
		})
	}
	for _, raw := range []string{"javascript:void(0)", "data:image/png;base64,AA", "/captcha/image.png", "/login/file.pdf", "file:///tmp/a.pdf", "https://other.example/file.pdf"} {
		if got := icResourceURL(icTestBaseURL, raw); got != "" {
			t.Errorf("unsafe resource %q resolved to %q", raw, got)
		}
	}
}

func TestICPageFixtureNamesAreStable(t *testing.T) {
	for page, name := range map[int]string{1: "ic_list_first.html", 2: "ic_list_middle.html", 3: "ic_list_last.html"} {
		items, err := parseAdapter_icList(loadICFixture(t, name), icTestBaseURL)
		if err != nil || len(items) != 1 {
			t.Fatalf("fixture page %d (%s): items=%d err=%v", page, name, len(items), err)
		}
		if !strings.Contains(items[0].Title, fmt.Sprintf("第%d页", page)) {
			t.Fatalf("fixture page %d title = %q", page, items[0].Title)
		}
	}
}

// The live site was re-themed: articles use .show_1 instead of .show01, the
// homepage is a real article list that backs the implicit index feed, and
// xwzx.htm turned into a column hub whose news list moved to xwzx/xwjj.htm.
// The ic_live_* fixtures are copies of the real pages captured on 2026-10-09.

func TestICIndexFeedParsesLiveHomepage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.htm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(loadICFixture(t, "ic_live_home.html"))
	}))
	defer server.Close()

	site := AllSites["ic"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	t.Cleanup(func() { site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache })

	items, total, err := icListPage("index", 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("index total pages = %d, want 1", total)
	}
	if len(items) != 10 {
		t.Fatalf("index items = %d, want 10 (6 news + 4 notices): %#v", len(items), items)
	}
	byType := make(map[string]int)
	for _, item := range items {
		if !strings.Contains(item.URL, "/info/") {
			t.Fatalf("non-article homepage URL %q", item.URL)
		}
		if item.Title == "" || item.Date == "" || item.Type == "" {
			t.Fatalf("incomplete homepage metadata: %+v", item)
		}
		byType[item.Type]++
	}
	if byType["新闻动态"] != 6 || byType["通知公告"] != 4 {
		t.Fatalf("homepage type mix = %#v, want 6 新闻动态 + 4 通知公告", byType)
	}
	if _, _, err := icListPage("index", 2); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("index page 2 error = %v, want out of range", err)
	}
	if _, _, err := icListPage("portal", 1); err == nil || !strings.Contains(err.Error(), "non-article feed") {
		t.Fatalf("unknown feed error = %v, want non-article feed", err)
	}
}

func TestICLiveArticleParsesNewShowTemplate(t *testing.T) {
	article, err := parseAdapter_icArticle(loadICFixture(t, "ic_live_article.html"), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "西南交通大学东部（国际）校区求实餐厅档口（饮品）技术合作项目成交公告" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Type != "新闻动态" {
		t.Fatalf("type = %q, want 新闻动态 from the 新闻中心 breadcrumb", article.Type)
	}
	if article.Date != "2026-09-29" || article.PublishedAt != "2026-09-29" {
		t.Fatalf("date/published_at = %q/%q, want date-only metadata", article.Date, article.PublishedAt)
	}
	if strings.Contains(article.PublishedAt, ":") {
		t.Fatalf("publication clock invented from date-only meta row: %q", article.PublishedAt)
	}
	if !strings.Contains(article.Content, "评审工作已经结束") {
		t.Fatalf("body text missing: %q", article.Content)
	}
	if strings.Contains(article.Content, "推荐阅读") {
		t.Fatalf("related-reading block leaked into body: %q", article.Content)
	}
	if len(article.Images) != 0 || len(article.Attachments) != 0 {
		t.Fatalf("resources invented on a text-only article: images=%v attachments=%v", article.Images, article.Attachments)
	}
}

func TestICLiveXwjjListHasReversePagination(t *testing.T) {
	body := loadICFixture(t, "ic_live_list_xwjj.html")
	items, err := parseAdapter_icList(body, icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 9 {
		t.Fatalf("xwzx items = %d, want 9: %#v", len(items), items)
	}
	first := items[0]
	if first.Title != "四川省教育考试院调研东部（国际）校区标准化考场建设" || first.Date != "2026-09-20" {
		t.Fatalf("first item = %+v", first)
	}
	if first.Type != "新闻动态" {
		t.Fatalf("first item type = %q, want 新闻动态 (新闻聚焦 nav)", first.Type)
	}
	total, err := icTotalPagesFromHTML(body, "xwzx")
	if err != nil {
		t.Fatal(err)
	}
	if total != 18 {
		t.Fatalf("xwzx total pages = %d, want 18 from .p_pages", total)
	}
}

func TestICListPageMapsXwzxLogicalPagesToLiveFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		switch r.URL.Path {
		case "/xwzx/xwjj.htm":
			body = loadICFixture(t, "ic_live_list_xwjj.html")
		case "/xwzx/xwjj/17.htm":
			body = loadICFixture(t, "ic_live_list_xwjj_p2.html")
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	site := AllSites["ic"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	t.Cleanup(func() { site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache })

	items, total, err := icListPage("xwzx", 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 18 || len(items) != 9 {
		t.Fatalf("xwzx page 1 = total %d items %d, want 18/9", total, len(items))
	}
	items, total, err = icListPage("xwzx", 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 18 || len(items) != 9 {
		t.Fatalf("xwzx page 2 = total %d items %d, want 18/9", total, len(items))
	}
	if !strings.HasSuffix(items[0].URL, "/info/1061/2841.htm") {
		t.Fatalf("xwzx page 2 first URL = %q, want /info/1061/2841.htm", items[0].URL)
	}
	if _, _, err := icListPage("xwzx", 19); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("xwzx page 19 error = %v, want out of range", err)
	}
}

func TestICLiveTzggListFiltersExternalRows(t *testing.T) {
	body := loadICFixture(t, "ic_live_list_tzgg.html")
	items, err := parseAdapter_icList(body, icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 11 {
		t.Fatalf("tzgg items = %d, want 11 local rows (external ISCit row filtered): %#v", len(items), items)
	}
	if items[0].Title != "西南交通大学东部（国际）校区求实餐厅档口（饮品）技术合作项目成交公告" || items[0].Date != "2026-09-29" {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[0].Type != "通知公告" {
		t.Fatalf("first item type = %q, want 通知公告", items[0].Type)
	}
	for _, item := range items {
		if strings.Contains(item.URL, "iscit.swjtu.edu.cn") {
			t.Fatalf("external notice leaked into tzgg feed: %q", item.URL)
		}
	}
	total, err := icTotalPagesFromHTML(body, "xwzx/tzgg")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("tzgg total pages = %d, want 1", total)
	}
}

func TestICLiveColumnHubIsNotAPaginatedList(t *testing.T) {
	body := loadICFixture(t, "ic_live_hub.html")
	if _, err := icTotalPagesFromHTML(body, "xwzx"); err == nil || !strings.Contains(err.Error(), "no source pagination") {
		t.Fatalf("hub total pages error = %v, want no source pagination", err)
	}
}

func TestICListNewsRangeEndToEndOverLiveFixtures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		switch r.URL.Path {
		case "/index.htm":
			body = loadICFixture(t, "ic_live_home.html")
		case "/xwzx/xwjj.htm":
			body = loadICFixture(t, "ic_live_list_xwjj.html")
		case "/xwzx/xwjj/17.htm":
			body = loadICFixture(t, "ic_live_list_xwjj_p2.html")
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	site := AllSites["ic"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	t.Cleanup(func() { site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache })

	indexItems, err := site.ListNewsRange("index", "2000-01-01", "2099-12-31", 0)
	if err != nil {
		t.Fatalf("index ListNewsRange: %v", err)
	}
	if len(indexItems) != 10 {
		t.Fatalf("index ListNewsRange items = %d, want 10", len(indexItems))
	}
	xwzxItems, err := site.ListNewsRange("xwzx", "2000-01-01", "2099-12-31", 2)
	if err != nil {
		t.Fatalf("xwzx ListNewsRange: %v", err)
	}
	if len(xwzxItems) != 18 {
		t.Fatalf("xwzx ListNewsRange items = %d, want 18 (9 + 9 over two pages)", len(xwzxItems))
	}
}
