package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestXczxFeedsMatchLiveNavigation(t *testing.T) {
	want := []string{"tzxw/tzgg", "tzxw/xwdt"}
	if !reflect.DeepEqual(xczxSubPages, want) {
		t.Fatalf("feeds = %#v, want %#v", xczxSubPages, want)
	}
	if got := xczxFeedTypes["tzxw/tzgg"]; got != "通知公告" {
		t.Fatalf("notice type = %q", got)
	}
	if got := xczxFeedTypes["tzxw/xwdt"]; got != "新闻动态" {
		t.Fatalf("news type = %q", got)
	}
}

func TestXczxParseListAndReversePagination(t *testing.T) {
	first := loadXczxFixture(t, "xczx_list_first.html")
	items, err := parseAdapter_xczxList(first, "https://xczx.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("first page items = %#v", items)
	}
	if items[0].Title != "乡村振兴研究院首条新闻" || items[0].Type != "新闻动态" || items[0].Date != "2025-03-24" || items[0].PublishedAt != "2025-03-24" {
		t.Fatalf("first item = %#v", items[0])
	}
	last := loadXczxFixture(t, "xczx_list_last.html")
	items, err = parseAdapter_xczxList(last, "https://xczx.swjtu.edu.cn")
	if err != nil || len(items) != 1 || items[0].Title != "乡村振兴研究院末条新闻" {
		t.Fatalf("last page items = %#v, err=%v", items, err)
	}
	if got := reverseBodaPageURL("https://xczx.swjtu.edu.cn", "tzxw/xwdt", 1, 2, ".htm"); got != "https://xczx.swjtu.edu.cn/tzxw/xwdt.htm" {
		t.Fatalf("first URL = %q", got)
	}
	if got := reverseBodaPageURL("https://xczx.swjtu.edu.cn", "tzxw/xwdt", 2, 2, ".htm"); got != "https://xczx.swjtu.edu.cn/tzxw/xwdt/1.htm" {
		t.Fatalf("last URL = %q", got)
	}
	if got := reverseBodaPageURL("https://xczx.swjtu.edu.cn", "tzxw/xwdt", 3, 2, ".htm"); got != "" {
		t.Fatalf("out-of-range URL = %q", got)
	}
}

func TestXczxHomepageIsScopedToRealArticles(t *testing.T) {
	raw := loadXczxFixture(t, "xczx.html")
	items, err := parseAdapter_xczxList(raw, "https://xczx.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("homepage returned no real article links")
	}
	for _, item := range items {
		if !xczxArticleURL(item.URL, "https://xczx.swjtu.edu.cn") {
			t.Fatalf("homepage leaked non-article URL: %#v", item)
		}
		if item.Type != "新闻动态" && item.Type != "通知公告" {
			t.Fatalf("homepage type = %q", item.Type)
		}
	}
}

func TestXczxTotalPagesReadsReverseCounter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tzxw/xwdt.htm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(loadXczxFixture(t, "xczx_list_first.html"))
	}))
	defer server.Close()

	site := AllSites["xczx"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	defer func() {
		site.BaseURL = oldBase
		NoCache = oldNoCache
		ClearCache()
	}()
	site.BaseURL = server.URL
	NoCache = true
	ClearCache()
	if got, err := xczxTotalPages("tzxw/xwdt"); err != nil || got != 2 {
		t.Fatalf("total pages = %d, err=%v", got, err)
	}
}

func TestXczxArticleUsesFixedMetadataAndBody(t *testing.T) {
	article, err := parseAdapter_xczxArticle(loadXczxFixture(t, "xczx_article_live.html"), "https://xczx.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "乡村振兴研究院详情测试" || article.Type != "新闻动态" || article.Date != "2025-03-24" || article.PublishedAt != "2025-03-24 15:57:00" {
		t.Fatalf("metadata = %#v", article)
	}
	if strings.Contains(article.Date, "2099") || !strings.Contains(article.Content, "正文固定区域") {
		t.Fatalf("body date leaked into metadata or body missing: %#v", article)
	}
	if len(article.Images) != 2 || len(article.Attachments) != 2 {
		t.Fatalf("raw resources = images=%#v attachments=%#v", article.Images, article.Attachments)
	}
}

func TestXczxFetchArticleVerifiesAndCleansResources(t *testing.T) {
	fixture := loadXczxFixture(t, "xczx_article_live.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1161/1173.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(fixture)
		case "/images/good.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10})
		case "/images/bad.jpg":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xe0})
		case "/upload/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\nvalid"))
		case "/upload/fake.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<!doctype html><html><body>验证码</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldClient, oldBase, oldNoCache := defaultClient, AllSites["xczx"].BaseURL, NoCache
	defer func() {
		defaultClient = oldClient
		AllSites["xczx"].BaseURL = oldBase
		NoCache = oldNoCache
		ClearCache()
	}()
	defaultClient = &http.Client{Transport: http.DefaultTransport}
	AllSites["xczx"].BaseURL = server.URL
	NoCache = true
	ClearCache()

	article, raw, err := xczxFetchArticle(server.URL + "/info/1161/1173.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || len(article.Images) != 1 || article.Images[0] != server.URL+"/images/good.jpg" {
		t.Fatalf("verified images = %#v raw=%d", article.Images, len(raw))
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != server.URL+"/upload/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "bad.jpg") || strings.Contains(article.ContentHTML, "fake.pdf") || !strings.Contains(article.ContentHTML, "good.pdf") {
		t.Fatalf("content HTML was not sanitized: %s", article.ContentHTML)
	}
}

func TestXczxResourceBodyChecksTypeMagicAndShell(t *testing.T) {
	cases := []struct {
		name, contentType, resource string
		body                        []byte
		image, want                 bool
	}{
		{"valid image", "image/jpeg", "/images/a.jpg", []byte{0xff, 0xd8, 0xff}, true, true},
		{"wrong image magic", "image/png", "/images/a.png", []byte{0xff, 0xd8, 0xff}, true, false},
		{"valid pdf", "application/pdf", "/files/a.pdf", []byte("%PDF-1.7"), false, true},
		{"captcha pdf", "application/pdf", "/files/a.pdf", []byte("<html>验证码</html>"), false, false},
		{"missing type", "", "/files/a.bin", []byte("binary"), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := xczxResourceBodyIsUsable(tc.contentType, tc.body, tc.resource, tc.image); got != tc.want {
				t.Fatalf("usable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestXczxRejects404Shell(t *testing.T) {
	if _, err := parseAdapter_xczxList(loadXczxFixture(t, "xczx_404.html"), "https://xczx.swjtu.edu.cn"); err == nil {
		t.Fatal("404 shell was accepted as a list")
	}
}

func loadXczxFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
