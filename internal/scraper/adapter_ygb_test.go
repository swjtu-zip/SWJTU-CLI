package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

const ygbTestBaseURL = "https://ygb.swjtu.edu.cn"

func ygbFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read ygb fixture %s: %v", name, err)
	}
	return body
}

func TestYgbRegistrationUsesOnlyAuditedNonEmptyFeeds(t *testing.T) {
	want := []string{
		"xwzx/xwdt", "xwzx/tzgg", "xwzx/dcxy", "xwzx/hdyg", "xwzx/ssfc",
		"djyl1/zbfc", "djyl1/xzzg", "djyl1/_hl_xj", "djyl1/gzdl", "bszn/gzzd",
	}
	site := AllSites["ygb"]
	if site == nil {
		t.Fatal("ygb site is not registered")
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("ygb feeds = %#v, want %#v", site.SubPages, want)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("ygb adapter hooks are incomplete")
	}
	for _, excluded := range []string{"xwzx/gsxx", "https://ygb-swjtu-edu-cn-s.vpn.swjtu.edu.cn:8118/", "one.swjtu.edu.cn"} {
		for _, feed := range site.SubPages {
			if feed == excluded {
				t.Fatalf("excluded ygb route was registered: %q", excluded)
			}
		}
	}
}

func TestYgbReversePageURLCoversFirstMiddleLastAndOutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		page  int
		total int
		want  string
	}{
		{name: "first", page: 1, total: 7, want: ygbTestBaseURL + "/xwzx/xwdt.htm"},
		{name: "middle", page: 4, total: 7, want: ygbTestBaseURL + "/xwzx/xwdt/4.htm"},
		{name: "last", page: 7, total: 7, want: ygbTestBaseURL + "/xwzx/xwdt/1.htm"},
		{name: "out-of-range", page: 8, total: 7, want: ""},
		{name: "unknown-total", page: 2, total: 0, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ygbPageURL(ygbTestBaseURL, "xwzx/xwdt", tt.page, tt.total, ".htm"); got != tt.want {
				t.Fatalf("page URL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestYgbListFixturesParseFirstMiddleLastAndRejectOutOfRangeShell(t *testing.T) {
	tests := []struct {
		fixture string
		title   string
		date    string
	}{
		{fixture: "ygb_list_first.html", title: "第一页新闻动态", date: "2026-07-14"},
		{fixture: "ygb_list_middle.html", title: "中间页新闻动态", date: "2026-04-29"},
		{fixture: "ygb_list_last.html", title: "末页新闻动态", date: "2025-01-06"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			items, err := parseAdapter_ygbList(ygbFixture(t, tt.fixture), ygbTestBaseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 {
				t.Fatalf("items = %#v, want one item", items)
			}
			if items[0].Title != tt.title || items[0].Date != tt.date || items[0].PublishedAt != tt.date || items[0].Type != "新闻动态" {
				t.Fatalf("item = %#v", items[0])
			}
		})
	}
	if _, err := parseAdapter_ygbList(ygbFixture(t, "ygb_list_oob.html"), ygbTestBaseURL); err == nil {
		t.Fatal("404/WAF list shell was accepted")
	}
	if got := ygbPageCountFromFixture(t, "ygb_list_first.html"); got != 7 {
		t.Fatalf("first page total = %d, want 7", got)
	}
}

func TestYgbListNewsPageRequestsReversePhysicalPages(t *testing.T) {
	first := ygbFixture(t, "ygb_list_first.html")
	middle := ygbFixture(t, "ygb_list_middle.html")
	last := ygbFixture(t, "ygb_list_last.html")
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/xwzx/xwdt.htm":
			_, _ = w.Write(first)
		case "/xwzx/xwdt/4.htm":
			_, _ = w.Write(middle)
		case "/xwzx/xwdt/1.htm":
			_, _ = w.Write(last)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["ygb"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	defer func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	}()

	for _, page := range []int{1, 4, 7} {
		items, err := site.ListNewsPage("xwzx/xwdt", page)
		if err != nil || len(items) != 1 {
			t.Fatalf("logical page %d: items=%#v err=%v", page, items, err)
		}
	}
	if _, err := site.ListNewsPage("xwzx/xwdt", 8); err == nil {
		t.Fatal("out-of-range logical page was accepted")
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "/0.htm") || strings.HasSuffix(path, "/8.htm") {
			t.Fatalf("out-of-range physical path was requested: %v", paths)
		}
	}
}

func ygbPageCountFromFixture(t *testing.T, name string) int {
	t.Helper()
	doc, err := newYgbDocument(ygbFixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return ygbPageCount(doc)
}

func newYgbDocument(raw []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
}

func TestYgbListRejectsVPNAndStaticLinks(t *testing.T) {
	items, err := parseAdapter_ygbList(ygbFixture(t, "ygb_list_links.html"), ygbTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "本站真实文章标题" {
		t.Fatalf("items = %#v", items)
	}
}

func TestYgbArticleUsesNewAndLegacyMetadataSelectors(t *testing.T) {
	newArticle, err := parseAdapter_ygbArticle(ygbFixture(t, "ygb_article_new.html"), ygbTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if newArticle.Title != "新模板通知标题" || newArticle.Type != "通知公告" || newArticle.Date != "2026-08-31" || newArticle.PublishedAt != "2026-08-31 14:03:00" {
		t.Fatalf("new metadata = %#v", newArticle)
	}
	if !strings.Contains(newArticle.Content, "正文不能被活动日期替代") {
		t.Fatalf("new body/date contamination = %q", newArticle.Content)
	}

	oldArticle, err := parseAdapter_ygbArticle(ygbFixture(t, "ygb_article_old.html"), ygbTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if oldArticle.Title != "旧模板活动标题" || oldArticle.Type != "支部风采" || oldArticle.Date != "2025-01-02" || oldArticle.PublishedAt != "2025-01-02" {
		t.Fatalf("old metadata = %#v", oldArticle)
	}
}

func TestYgbFetchArticleVerifiesAndSanitizesResources(t *testing.T) {
	articleHTML := ygbFixture(t, "ygb_article_resources.html")
	wafHTML := ygbFixture(t, "ygb_resource_waf.html")
	captchaHTML := ygbFixture(t, "ygb_resource_captcha.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/100/9001.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(articleHTML)
		case "/img/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 1})
		case "/img/waf.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(wafHTML)
		case "/files/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\nfixture"))
		case "/files/captcha.pdf":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(captchaHTML)
		case "/files/html.pdf":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><html>not an attachment</html>"))
		case "/files/wrong.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("not a pdf"))
		case "/files/large.zip":
			w.Header().Set("Content-Type", "application/zip")
			body := make([]byte, ygbMaxResourceBytes+1)
			body[0], body[1], body[2], body[3] = 'P', 'K', 3, 4
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["ygb"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	defer func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	}()

	article, raw, err := ygbFetchArticle(server.URL + "/info/100/9001.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("article response body was empty")
	}
	if !reflect.DeepEqual(article.Images, []string{server.URL + "/img/good.png"}) {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != server.URL+"/files/good.pdf" || article.Attachments[0].Name != "good.pdf" || article.Attachments[0].Size != strconv.Itoa(len("%PDF-1.7\nfixture")) {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, server.URL+"/img/good.png") || !strings.Contains(article.ContentHTML, server.URL+"/files/good.pdf") {
		t.Fatalf("valid resources missing from content HTML: %s", article.ContentHTML)
	}
	for _, rejected := range []string{"waf.png", "captcha.pdf", "html.pdf", "wrong.pdf", "large.zip"} {
		if strings.Contains(article.ContentHTML, server.URL+"/img/"+rejected) || strings.Contains(article.ContentHTML, server.URL+"/files/"+rejected) {
			t.Errorf("rejected resource %q remained in content HTML: %s", rejected, article.ContentHTML)
		}
	}
	if strings.Contains(article.ContentHTML, "javascript:") {
		t.Fatalf("pseudo resource remained in content HTML: %s", article.ContentHTML)
	}
}

func TestYgbResourceBodyChecksStatusTypeAndMagic(t *testing.T) {
	checks := []struct {
		name        string
		contentType string
		body        []byte
		resource    string
		image       bool
		want        bool
	}{
		{name: "valid image", contentType: "image/png", body: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, resource: "good.png", image: true, want: true},
		{name: "html pseudo attachment", contentType: "application/octet-stream", body: []byte("<html>captcha</html>"), resource: "bad.pdf", want: false},
		{name: "wrong magic", contentType: "application/pdf", body: []byte("plain text"), resource: "bad.pdf", want: false},
		{name: "valid pdf", contentType: "application/pdf; charset=binary", body: []byte("%PDF-1.4"), resource: "good.pdf", want: true},
	}
	for _, tt := range checks {
		t.Run(tt.name, func(t *testing.T) {
			if got := ygbResourceBodyIsUsable(tt.contentType, tt.body, tt.resource, tt.image); got != tt.want {
				t.Fatalf("usable = %v, want %v", got, tt.want)
			}
		})
	}
	if ygbResourceBodyIsUsable("application/zip", []byte("PK\x03\x04"), "large.zip", false) != true {
		t.Fatal("valid zip magic was rejected")
	}
	if len(ygbSubPages) != 10 {
		t.Fatal("regression: ygb feed count changed")
	}
}
