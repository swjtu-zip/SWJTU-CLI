package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const sjFixtureBaseURL = "https://sj.swjtu.edu.cn"

func readSJFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read sj fixture %s: %v", name, err)
	}
	return body
}

func TestSjAdapterRegistrationAndAuditedFeeds(t *testing.T) {
	site := AllSites["sj"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("sj adapter hooks are incomplete: %#v", site)
	}
	want := []string{
		"index/xwdt", "gggs", "gzcx/cwsj", "gzcx/zxsj", "gzcx/gcsj", "zbjs",
		"zcfg/gjxgfgzd", "zcfg/bwxgfgzd", "zcfg/scsxgfgzd", "zcfg/xxxggzzd",
		"zlxz/cwsjxgzl", "zlxz/gcsjxgzl", "zlxz/zxsjxgzl", "xxyd", "sjsy",
	}
	if got := strings.Join(site.SubPages, "\x00"); got != strings.Join(want, "\x00") {
		t.Fatalf("sj article-list routes = %#v, want %#v", site.SubPages, want)
	}
	for _, excluded := range []string{"jgsz/bmjj", "czxx", "lxwm", "zcfg/sjcxggzzd", "gcsj.swjtu.edu.cn", "auditsj.swjtu.edu.cn"} {
		for _, route := range site.SubPages {
			if route == excluded {
				t.Fatalf("static, empty, or external route %q was registered", excluded)
			}
		}
	}
}

func TestSjReversePaginationAndListFields(t *testing.T) {
	if got := reverseBodaPageURL(sjFixtureBaseURL, "index/xwdt", 1, 15, ".htm"); got != sjFixtureBaseURL+"/index/xwdt.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := reverseBodaPageURL(sjFixtureBaseURL, "index/xwdt", 8, 15, ".htm"); got != sjFixtureBaseURL+"/index/xwdt/8.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := reverseBodaPageURL(sjFixtureBaseURL, "index/xwdt", 15, 15, ".htm"); got != sjFixtureBaseURL+"/index/xwdt/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := reverseBodaPageURL(sjFixtureBaseURL, "index/xwdt", 16, 15, ".htm"); got != "" {
		t.Fatalf("out-of-range URL = %q", got)
	}
	if got := parseTotalPages(readSJFixture(t, "sj_list_first.html")); got != 15 {
		t.Fatalf("first page total = %d, want 15", got)
	}

	for _, tc := range []struct {
		fixture string
		want    string
		date    string
	}{
		{"sj_list_first.html", "最新审计动态", "2026-08-20"},
		{"sj_list_middle.html", "中间页审计动态", "2026-07-20"},
		{"sj_list_last.html", "最末页审计动态", "2020-01-02"},
	} {
		items, err := parseAdapter_sjList(readSJFixture(t, tc.fixture), sjFixtureBaseURL)
		if err != nil || len(items) != 2 {
			t.Fatalf("%s: items=%#v err=%v", tc.fixture, items, err)
		}
		if items[0].Title != tc.want || items[0].Type != "审计动态" || items[0].Date != tc.date {
			t.Fatalf("%s first item = %#v", tc.fixture, items[0])
		}
		if items[0].PublishedAt != "" {
			t.Fatalf("date-only list item invented a clock: %#v", items[0])
		}
	}
	if _, err := parseAdapter_sjList(readSJFixture(t, "sj_list_oob.html"), sjFixtureBaseURL); err == nil {
		t.Fatal("out-of-range/error page was accepted as a list")
	}
}

func TestSjHomepageSectionsParseAsIndexFeed(t *testing.T) {
	// The homepage (pageType 1) exposes real article rows: the 审计动态 tab
	// pane with split day/year dates and four 首页栏目 blocks that reuse the
	// column-page list-cell/list-col row shapes under .index-news.
	items, err := parseAdapter_sjList(readSJFixture(t, "sj_home_live.html"), sjFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 29 {
		t.Fatalf("homepage items = %d, want 29: %#v", len(items), items)
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Type]++
		if item.Date == "" || item.Title == "" {
			t.Fatalf("incomplete item: %#v", item)
		}
		if !strings.HasPrefix(item.URL, sjFixtureBaseURL+"/info/") {
			t.Fatalf("non-local item URL: %#v", item)
		}
	}
	want := map[string]int{"审计动态": 5, "审计公告": 6, "支部建设": 6, "审计视野": 6, "政策法规": 6}
	for typ, n := range want {
		if counts[typ] != n {
			t.Fatalf("section %q rows = %d, want %d: %#v", typ, counts[typ], n, items)
		}
	}
	first := items[0]
	if first.Title != "学校召开大型仪器设备使用与共享绩效评价工作启动会" || first.Date != "2026-06-02" {
		t.Fatalf("split-date item = %#v", first)
	}
	if first.PublishedAt != "" {
		t.Fatalf("date-only homepage item invented a clock: %#v", first)
	}
}

func TestSjIndexFeedFetchesHomepage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.htm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(readSJFixture(t, "sj_home_live.html"))
	}))
	defer server.Close()

	site := AllSites["sj"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	})

	if total, err := site.GetTotalPages("index"); err != nil || total != 1 {
		t.Fatalf("index total = %d, err=%v", total, err)
	}
	items, err := site.ListNewsPage("index", 1)
	if err != nil || len(items) != 29 {
		t.Fatalf("index items = %d, err=%v", len(items), err)
	}
}

func TestSjArticleUsesFixedMetadataAndDateOnly(t *testing.T) {
	article, err := parseAdapter_sjArticle(readSJFixture(t, "sj_article.html"), sjFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "固定详情标题" || article.Type != "审计动态" || article.Date != "2026-08-20" || article.PublishedAt != "" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "正文只取详情内容") || !strings.Contains(article.Content, "会议时间 2026年9月16日 14:30") {
		t.Fatalf("article body = %q", article.Content)
	}
	if strings.Contains(article.Content, "站点页脚") || strings.Contains(article.Title, "页面假标题") {
		t.Fatalf("unscoped shell content leaked: %#v", article)
	}
	if len(article.Images) != 2 || len(article.Attachments) != 3 {
		t.Fatalf("resource candidates = images=%#v attachments=%#v", article.Images, article.Attachments)
	}
}

func TestSjFetchArticleValidatesResourcesAndShells(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/100/1.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(readSJFixture(t, "sj_article.html"))
		case "/images/ok.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		case "/images/bad.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("<html>captcha</html>"))
		case "/files/ok.pdf", "/files/outside.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\nvalid"))
		case "/files/fake.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<!doctype html><html>验证码下载</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["sj"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	})

	article, _, err := sjFetchArticle(server.URL + "/info/100/1.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/images/ok.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 || !strings.Contains(article.Attachments[0].URL, "/files/ok.pdf") {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "bad.jpg") || strings.Contains(article.ContentHTML, "fake.pdf") {
		t.Fatalf("invalid resources remained in ContentHTML: %s", article.ContentHTML)
	}
	if strings.Contains(article.ContentHTML, "验证码下载") {
		t.Fatalf("captcha attachment text remained in ContentHTML: %s", article.ContentHTML)
	}
	if _, err := parseAdapter_sjArticle(readSJFixture(t, "sj_captcha.html"), sjFixtureBaseURL); err == nil {
		t.Fatal("captcha shell was accepted as an article")
	}
}
