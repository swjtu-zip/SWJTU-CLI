package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func readScyshjFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read scyshj fixture %q: %v", name, err)
	}
	return body
}

func TestScyshjAdapterRegistrationAndAuditedFeeds(t *testing.T) {
	site := AllSites["scyshj"]
	want := []string{
		"tzxw/tzgg", "tzxw/xwdt", "xsjl", "zchj", "dxal",
		"scsjczxwyh/wygd", "scsjczxwyh/hdly",
	}
	if site == nil || site.BaseURL != scyshjCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("scyshj adapter is incomplete: %#v", site)
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("scyshj feeds = %#v, want %#v", site.SubPages, want)
	}
}

func TestScyshjListReversePaginationAndRejectsNonLists(t *testing.T) {
	base := scyshjCanonicalBaseURL
	if got := scyshjPageURL(base, "tzxw/tzgg", 1, 3, ".htm"); got != base+"/tzxw/tzgg.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := scyshjPageURL(base, "tzxw/tzgg", 2, 3, ".htm"); got != base+"/tzxw/tzgg/2.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := scyshjPageURL(base, "tzxw/tzgg", 3, 3, ".htm"); got != base+"/tzxw/tzgg/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	for _, page := range []int{0, 4} {
		if got := scyshjPageURL(base, "tzxw/tzgg", page, 3, ".htm"); got != "" {
			t.Errorf("out-of-range page %d returned %q", page, got)
		}
	}

	for _, fixture := range []string{"scyshj_list_first.html", "scyshj_list_middle.html", "scyshj_list_last.html"} {
		items, err := parseAdapter_scyshjList(readScyshjFixture(t, fixture), base)
		if err != nil {
			t.Fatalf("parse %s: %v", fixture, err)
		}
		if len(items) != 2 || items[0].Type != "通知公告" || items[0].Date == "" {
			t.Fatalf("%s items = %#v", fixture, items)
		}
	}
	if got := scyshjTotalPagesFromHTML(readScyshjFixture(t, "scyshj_list_first.html")); got != 3 {
		t.Fatalf("page count = %d, want 3", got)
	}
	for _, fixture := range []string{"scyshj_static.html", "scyshj_empty.html", "scyshj_waf.html", "scyshj_list_out_of_range.html"} {
		if items, err := parseAdapter_scyshjList(readScyshjFixture(t, fixture), base); err == nil || items != nil {
			t.Errorf("%s was accepted: items=%#v err=%v", fixture, items, err)
		}
	}
}

func TestScyshjArticleUsesFixedMetadataAndDatePrecision(t *testing.T) {
	article, err := parseAdapter_scyshjArticle(readScyshjFixture(t, "scyshj_article.html"), scyshjCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse article: %v", err)
	}
	if article.Title != "四川省营商环境咨询委员会章程（修订）" || article.Type != "通知公告" || article.Date != "2025-08-12" || article.PublishedAt != "2025-08-12 15:27:00" || article.Source != "四川省营商环境研究院" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "正文第一段") || !strings.Contains(article.Content, "活动时间：2099年1月1日") || strings.Contains(article.Content, "导航噪声") {
		t.Fatalf("article content = %q", article.Content)
	}
	if len(article.Images) != 2 || len(article.Attachments) != 3 {
		t.Fatalf("candidate resources = images=%#v attachments=%#v", article.Images, article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "data:image") || strings.Contains(article.ContentHTML, "javascript:") {
		t.Fatalf("pseudo resources leaked into content HTML: %s", article.ContentHTML)
	}

	dateOnly, err := parseAdapter_scyshjArticle(readScyshjFixture(t, "scyshj_article_date_only.html"), scyshjCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse date-only article: %v", err)
	}
	if dateOnly.Date != "2025-07-10" || dateOnly.PublishedAt != "2025-07-10" || strings.Contains(dateOnly.PublishedAt, ":00") {
		t.Fatalf("date-only metadata = date=%q published=%q", dateOnly.Date, dateOnly.PublishedAt)
	}
	if _, err := parseAdapter_scyshjArticle(readScyshjFixture(t, "scyshj_waf.html"), scyshjCanonicalBaseURL); err == nil {
		t.Fatal("WAF page was accepted as an article")
	}
}

func TestScyshjFetchArticleVerifiesResources(t *testing.T) {
	articleHTML := readScyshjFixture(t, "scyshj_article.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1160/2788.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(articleHTML)
		case "/images/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		case "/images/captcha.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>验证码</html>"))
		case "/images/wrong.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("not a png"))
		case "/files/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7 fixture"))
		case "/files/bad.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<html>404 not found</html>"))
		case "/files/missing.pdf":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["scyshj"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	defer func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	}()
	site.BaseURL = server.URL
	defaultClient = server.Client()
	NoCache = true
	ClearCache()

	article, raw, err := scyshjFetchArticle(server.URL + "/info/1160/2788.htm")
	if err != nil {
		t.Fatalf("fetch article: %v", err)
	}
	if len(raw) == 0 || len(article.Images) != 1 || article.Images[0] != server.URL+"/images/good.png" {
		t.Fatalf("verified images = %#v raw=%d", article.Images, len(raw))
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "有效附件.pdf" || article.Attachments[0].URL != server.URL+"/files/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	for _, rejected := range []string{"captcha.png", "wrong.png", "bad.pdf", "missing.pdf", "data:image"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("invalid resource %q leaked into content HTML: %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, "https://example.com/reference") {
		t.Fatalf("ordinary external link was lost: %s", article.ContentHTML)
	}

	if !scyshjResourceBodyIsUsable("image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "good.png", true) || scyshjResourceBodyIsUsable("image/png", []byte("not png"), "wrong.png", true) || scyshjResourceBodyIsUsable("application/pdf", []byte("not pdf"), "bad.pdf", false) {
		t.Fatal("resource magic/content-type guard is incorrect")
	}
}

func TestScyshjResourceRejectsExternalAndPseudoURLs(t *testing.T) {
	base := scyshjCanonicalBaseURL
	for _, raw := range []string{"https://example.com/a.png", "javascript:void(0)", "data:image/png;base64,AA", "file:///tmp/a.pdf", "/captcha/image.png", "/spacer.gif"} {
		if got := scyshjResourceURL(base, raw); got != "" {
			t.Errorf("resource %q resolved to %q", raw, got)
		}
	}
}
