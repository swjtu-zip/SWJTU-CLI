package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func readSROFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read SRO fixture %q: %v", name, err)
	}
	return body
}

func TestSROAdapterRegistrationAndAuditedFeeds(t *testing.T) {
	site := AllSites["sro"]
	if site == nil || site.BaseURL != sroCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("SRO adapter is incomplete: %#v", site)
	}
	want := []string{
		"tzgg1", "tzgg1/zrjj", "tzgg1/zdyf", "tzgg1/qtzx", "tzgg1/jdpt", "tzgg1/kjhz", "tzgg1/kjjl", "tzgg1/xshd", "tzgg1/zhsx",
		"xwdt/xnkyzc", "xwdt/xnkydt", "kjhz/xqhz", "kjhz/xdhz", "kjhz/cgzh", "djgz/zbdt", "xxgs/kjjl", "xxgs/cgzh", "xxgs/qt", "cyxz/cyxz",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("SRO feeds = %#v, want %#v", site.SubPages, want)
	}
}

func TestSROReversePaginationAndPageCount(t *testing.T) {
	base := sroCanonicalBaseURL
	if got := sroPageURL(base, "index", 1, 1, ".htm"); got != base+"/" {
		t.Fatalf("index URL = %q", got)
	}
	if got := sroPageURL(base, "tzgg1", 1, 108, ".htm"); got != base+"/tzgg1.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := sroPageURL(base, "tzgg1", 2, 108, ".htm"); got != base+"/tzgg1/107.htm" {
		t.Fatalf("second page URL = %q", got)
	}
	if got := sroPageURL(base, "tzgg1", 54, 108, ".htm"); got != base+"/tzgg1/55.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := sroPageURL(base, "tzgg1", 108, 108, ".htm"); got != base+"/tzgg1/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	for _, tc := range [][2]int{{0, 108}, {109, 108}, {2, 0}} {
		if got := sroPageURL(base, "tzgg1", tc[0], tc[1], ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc[0], tc[1], got)
		}
	}
	if got := sroPageCount(sroParseFixtureDocument(t, "sro_live_list_first.html")); got != 3 {
		t.Fatalf("fixture page count = %d, want 3", got)
	}
}

func sroParseFixtureDocument(t *testing.T, name string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(readSROFixture(t, name))))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestSROHomepageParsesOnlyLocalNewsModules(t *testing.T) {
	items, err := parseAdapter_sroList(readSROFixture(t, "sro_live_home.html"), sroCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse SRO homepage: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("homepage items = %d, want 4: %#v", len(items), items)
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, sroCanonicalBaseURL+"/info/") {
			t.Errorf("homepage retained non-local article: %#v", item)
		}
		if strings.Contains(item.Title, "过程管理") {
			t.Errorf("procedure card leaked into homepage feed: %#v", item)
		}
	}
	var notice, dynamic bool
	for _, item := range items {
		if item.Title == "本地通知公告" && item.Type == "重点研发" && item.Date == "2026-09-03" {
			notice = true
		}
		if item.Title == "本地科研动态" && item.Type == "科研动态" && item.Date == "2026-08-01" {
			dynamic = true
		}
	}
	if !notice || !dynamic {
		t.Fatalf("homepage metadata missing: notice=%v dynamic=%v items=%#v", notice, dynamic, items)
	}
}

func TestSROListParsesReversePageRowsAndRejectsShells(t *testing.T) {
	for _, name := range []string{"sro_live_list_first.html", "sro_live_list_middle.html", "sro_live_list_last.html"} {
		items, err := parseAdapter_sroList(readSROFixture(t, name), sroCanonicalBaseURL)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if len(items) != 2 {
			t.Fatalf("%s items = %d, want 2", name, len(items))
		}
		for _, item := range items {
			if item.Type != "重点研发" || item.Date == "" || !strings.HasPrefix(item.URL, sroCanonicalBaseURL+"/info/1121/") {
				t.Errorf("%s item metadata = %#v", name, item)
			}
		}
	}
	for _, name := range []string{"sro_live_static.html", "sro_live_waf.html", "sro_live_out_of_range.html"} {
		if items, err := parseAdapter_sroList(readSROFixture(t, name), sroCanonicalBaseURL); err == nil || items != nil {
			t.Errorf("%s was accepted as a list: items=%#v err=%v", name, items, err)
		}
	}
}

func TestSROArticleUsesFixedMetadataBodyAndFiltersPseudoResources(t *testing.T) {
	article, err := parseAdapter_sroArticle(readSROFixture(t, "sro_live_article.html"), sroCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse SRO article: %v", err)
	}
	if article.Title != "关于科研项目申报的通知" || article.Type != "重点研发" || article.Date != "2026-09-03" || article.PublishedAt != "2026-09-03" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "正文第一段") || !strings.Contains(article.Content, "活动时间：2099年1月1日") || strings.Contains(article.Content, "截止时间：2099年") && article.Date != "2026-09-03" {
		t.Fatalf("article body/date handling is wrong: date=%q content=%q", article.Date, article.Content)
	}
	if len(article.Images) != 2 || article.Images[0] != sroCanonicalBaseURL+"/images/real.png" || article.Images[1] != sroCanonicalBaseURL+"/images/bad.png" {
		t.Fatalf("article image candidates = %#v", article.Images)
	}
	if len(article.Attachments) != 2 || article.Attachments[0].Name != "有效附件.pdf" || !strings.Contains(article.Attachments[0].URL, "/files/good.pdf") {
		t.Fatalf("article attachment candidates = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "data:image") || strings.Contains(article.ContentHTML, "spacer.gif") || strings.Contains(article.ContentHTML, "javascript:") {
		t.Fatalf("pseudo resources leaked into ContentHTML: %s", article.ContentHTML)
	}
}

func TestSROFetchArticleVerifiesHTTPContentTypeMagicAndSize(t *testing.T) {
	fixture := readSROFixture(t, "sro_live_article.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1121/32435.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(fixture)
		case "/images/real.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		case "/images/bad.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>验证码</html>"))
		case "/files/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7 fixture"))
		case "/files/bad.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<html>404 not found</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["sro"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	defer func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	}()
	site.BaseURL = server.URL
	defaultClient = server.Client()
	NoCache = true

	article, raw, err := sroFetchArticle(server.URL + "/info/1121/32435.htm")
	if err != nil {
		t.Fatalf("fetch SRO article: %v", err)
	}
	if len(raw) == 0 || article == nil {
		t.Fatal("fetch returned no article snapshot")
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/images/real.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "有效附件.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "bad.png") || strings.Contains(article.ContentHTML, "bad.pdf") {
		t.Fatalf("invalid resources leaked into sanitized HTML: %s", article.ContentHTML)
	}
	if _, _, err := sroFetchResource(server.URL+"/missing.pdf", server.URL+"/info/1121/32435.htm", false); err == nil {
		t.Fatal("HTTP error resource was accepted")
	}
	if sroResourceBodyIsUsable("image/png", []byte("<html>captcha</html>"), "bad.png", true) {
		t.Fatal("HTML captcha body was accepted as an image")
	}
	if sroResourceBodyIsUsable("application/pdf", []byte("not a PDF"), "bad.pdf", false) {
		t.Fatal("wrong PDF magic was accepted")
	}
}
