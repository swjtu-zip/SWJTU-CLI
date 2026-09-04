package scraper

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

const xybTestBaseURL = "https://xyb.swjtu.edu.cn"

func readXybFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read xyb fixture %q: %v", name, err)
	}
	return body
}

func TestXybRegistrationUsesOnlyAuditedArticleLists(t *testing.T) {
	want := []string{
		"xwdt1", "xwdt1/djgz", "xwdt1/tzgg", "xwdt1/xwzx",
		"xwdt1/xwzx/jj", "xwdt1/xwzx/xy", "xyzh/xyfc",
	}
	site := AllSites["xyb"]
	if site == nil {
		t.Fatal("xyb site is not registered")
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("xyb feeds = %#v, want %#v", site.SubPages, want)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("xyb adapter hooks are incomplete")
	}
	for _, excluded := range []string{"xyzh/qnxyh", "xyzh/xyfw", "https://xymisv5.swjtu.edu.cn/", "https://foundation.swjtu.edu.cn/"} {
		for _, feed := range site.SubPages {
			if feed == excluded {
				t.Fatalf("excluded xyb route was registered: %q", excluded)
			}
		}
	}
}

func TestXybReversePageURLAndListBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		page  int
		total int
		want  string
	}{
		{name: "first", page: 1, total: 84, want: xybTestBaseURL + "/xwdt1/xwzx.htm"},
		{name: "middle", page: 43, total: 84, want: xybTestBaseURL + "/xwdt1/xwzx/42.htm"},
		{name: "last", page: 84, total: 84, want: xybTestBaseURL + "/xwdt1/xwzx/1.htm"},
		{name: "out-of-range", page: 85, total: 84, want: ""},
		{name: "unknown-total", page: 2, total: 0, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := xybPageURL(xybTestBaseURL, "xwdt1/xwzx", tt.page, tt.total, ".htm"); got != tt.want {
				t.Fatalf("page URL = %q, want %q", got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		fixture string
		title   string
		date    string
	}{
		{fixture: "xyb_list_first.html", title: "新闻资讯第一页", date: "2026-06-09"},
		{fixture: "xyb_list_middle.html", title: "新闻资讯中间页", date: "2023-05-15"},
		{fixture: "xyb_list_last.html", title: "新闻资讯末页", date: "2018-01-06"},
	} {
		items, err := parseAdapter_xybList(readXybFixture(t, tt.fixture), xybTestBaseURL)
		if err != nil {
			t.Fatalf("%s: %v", tt.fixture, err)
		}
		if len(items) != 1 || items[0].Title != tt.title || items[0].Date != tt.date || items[0].PublishedAt != tt.date || items[0].Type != "新闻资讯" {
			t.Fatalf("%s items = %#v", tt.fixture, items)
		}
	}
	if got := xybPageCountFromFixture(t, "xyb_list_first.html"); got != 84 {
		t.Fatalf("first page total = %d, want 84", got)
	}
	if _, err := parseAdapter_xybList(readXybFixture(t, "xyb_list_empty.html"), xybTestBaseURL); err == nil {
		t.Fatal("empty public page was accepted as an article list")
	}
	if _, err := parseAdapter_xybList(readXybFixture(t, "xyb_list_oob.html"), xybTestBaseURL); err == nil {
		t.Fatal("404/WAF list shell was accepted")
	}
}

func TestXybListNewsPageRequestsReversePhysicalPages(t *testing.T) {
	first := readXybFixture(t, "xyb_list_first.html")
	middle := readXybFixture(t, "xyb_list_middle.html")
	last := readXybFixture(t, "xyb_list_last.html")
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/xwdt1/xwzx.htm":
			_, _ = w.Write(first)
		case "/xwdt1/xwzx/42.htm":
			_, _ = w.Write(middle)
		case "/xwdt1/xwzx/1.htm":
			_, _ = w.Write(last)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["xyb"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	t.Cleanup(func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	})

	for _, page := range []int{1, 43, 84} {
		items, err := site.ListNewsPage("xwdt1/xwzx", page)
		if err != nil || len(items) != 1 {
			t.Fatalf("logical page %d: items=%#v err=%v", page, items, err)
		}
	}
	if _, err := site.ListNewsPage("xwdt1/xwzx", 85); err == nil {
		t.Fatal("out-of-range logical page was accepted")
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "/0.htm") || strings.HasSuffix(path, "/85.htm") {
			t.Fatalf("out-of-range physical path was requested: %v", paths)
		}
	}
}

func xybPageCountFromFixture(t *testing.T, name string) int {
	t.Helper()
	doc, err := newXybDocument(readXybFixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return xybPageCount(doc)
}

func newXybDocument(raw []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
}

func TestXybArticleUsesFixedMetadataAndBodyScope(t *testing.T) {
	article, err := parseAdapter_xybArticle(readXybFixture(t, "xyb_article_new.html"), xybTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "校友办新闻详情标题" || article.Type != "新闻资讯" || article.Source != "校友工作办公室" {
		t.Fatalf("metadata = %#v", article)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:03:05" {
		t.Fatalf("date metadata = (%q, %q)", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "正文第一段") || strings.Contains(article.Content, "侧栏不属于正文") {
		t.Fatalf("body escaped fixed content scope: %q", article.Content)
	}

	dateOnly, err := parseAdapter_xybArticle(readXybFixture(t, "xyb_article_date_only.html"), xybTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if dateOnly.Date != "2026-08-30" || dateOnly.PublishedAt != "2026-08-30" || strings.Contains(dateOnly.PublishedAt, "09:30") {
		t.Fatalf("date-only metadata was contaminated by body time: %#v", dateOnly)
	}
}

func TestXybFetchArticleVerifiesResourcesAndSanitizesHTML(t *testing.T) {
	detail := readXybFixture(t, "xyb_article_resources.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/100/9001.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(detail)
		case "/images/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0})
		case "/images/bad.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(w, "<html><body>security verification</body></html>")
		case "/files/good.pdf", "/files/outside.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = io.WriteString(w, "%PDF-1.7 fixture")
		case "/files/captcha.pdf":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "<html><body>请输入验证码下载附件</body></html>")
		case "/files/wrong.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = io.WriteString(w, "not a PDF")
		case "/files/missing.pdf":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldClient, oldNoCache := defaultClient, NoCache
	defaultClient, NoCache = server.Client(), true
	t.Cleanup(func() { defaultClient, NoCache = oldClient, oldNoCache; ClearCache() })

	article, raw, err := xybFetchArticleAt(server.URL+"/info/100/9001.htm", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, detail) {
		t.Fatal("fetcher did not retain original detail response")
	}
	if !reflect.DeepEqual(article.Images, []string{server.URL + "/images/good.png"}) {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 || article.Attachments[0].URL != server.URL+"/files/good.pdf" || article.Attachments[0].Size != strconv.Itoa(len("%PDF-1.7 fixture")) || article.Attachments[1].URL != server.URL+"/files/outside.pdf" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, server.URL+"/images/good.png") || !strings.Contains(article.ContentHTML, server.URL+"/files/good.pdf") {
		t.Fatalf("valid resources missing from content HTML: %s", article.ContentHTML)
	}
	for _, rejected := range []string{"bad.png", "captcha.pdf", "wrong.pdf", "missing.pdf"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource %q survived sanitization: %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, "https://example.com/ordinary-link") || strings.Contains(article.ContentHTML, "javascript:") {
		t.Fatalf("ordinary/pseudo links were not sanitized correctly: %s", article.ContentHTML)
	}
	if _, _, err := xybFetchResource(server.URL+"/files/wrong.pdf", server.URL, server.URL+"/info/100/9001.htm", false); err == nil {
		t.Fatal("wrong PDF magic was accepted")
	}
	if _, _, err := xybFetchResource(server.URL+"/files/missing.pdf", server.URL, server.URL+"/info/100/9001.htm", false); err == nil {
		t.Fatal("non-2xx resource was accepted")
	}
}

func TestXybResourceGuardsRequireStatusTypeAndMagic(t *testing.T) {
	checks := []struct {
		name        string
		contentType string
		body        []byte
		resource    string
		image       bool
		want        bool
	}{
		{name: "valid image", contentType: "image/png", body: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, resource: "good.png", image: true, want: true},
		{name: "captcha shell", contentType: "application/octet-stream", body: []byte("<html>验证码</html>"), resource: "bad.pdf", want: false},
		{name: "wrong magic", contentType: "application/pdf", body: []byte("plain text"), resource: "bad.pdf", want: false},
		{name: "valid pdf", contentType: "application/pdf; charset=binary", body: []byte("%PDF-1.7"), resource: "good.pdf", want: true},
		{name: "missing content type", contentType: "", body: []byte("%PDF-1.7"), resource: "good.pdf", want: false},
	}
	for _, tt := range checks {
		t.Run(tt.name, func(t *testing.T) {
			if got := xybResourceBodyIsUsable(tt.contentType, tt.body, tt.resource, tt.image); got != tt.want {
				t.Fatalf("usable = %v, want %v", got, tt.want)
			}
		})
	}
	if xybResourceBodyIsUsable("application/zip", []byte("PK\x03\x04"), "good.zip", false) != true {
		t.Fatal("valid zip magic was rejected")
	}
	for _, raw := range []string{"javascript:void(0)", "data:image/png;base64,AA", "/captcha/image.png", "file:///tmp/a.pdf"} {
		if got := xybResourceURL(xybTestBaseURL, raw); got != "" {
			t.Fatalf("invalid resource %q resolved to %q", raw, got)
		}
	}
}
