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
		"tzxw/tzgg", "tzxw/xwdt", "xsjl", "dxal",
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

func TestScyshjHomepagePortalParsesDatedSectionRows(t *testing.T) {
	items, err := parseAdapter_scyshjList(readScyshjFixture(t, "scyshj_home_portal.html"), scyshjCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	// Only the 通知公告 section rows carry full dates; the 新闻动态 preview
	// rows show "05/30"-style tickers whose year cannot be determined, and
	// those articles are still collected by the tzxw/xwdt feed itself.
	if len(items) != 8 {
		t.Fatalf("homepage items = %d, want 8 dated rows: %#v", len(items), items)
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate URL %q", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, scyshjCanonicalBaseURL+"/info/1160/") {
			t.Errorf("non-通知公告 URL %q", item.URL)
		}
		if item.Type != "通知公告" {
			t.Errorf("type = %q, want 通知公告", item.Type)
		}
		if item.Date == "" {
			t.Errorf("item %q has no date", item.Title)
		}
		if strings.HasSuffix(item.Title, "...") {
			t.Errorf("truncated title %q leaked into the feed", item.Title)
		}
	}
}

func TestScyshjPortalRowsTakeTheirSectionType(t *testing.T) {
	const portalHTML = `<html><head><meta name="pageType" content="1"></head><body>
<div class="vsb-space informBox fl">
  <div class="dynamic"><h2>通知公告</h2><span><a href="tzxw/tzgg.htm">more</a></span></div>
  <ul class="dynamic_list dynamic_listb fr">
    <li><a href="info/1160/3128.htm"><span>2026-06-06</span><em>通知公告栏文章标题</em></a></li>
  </ul>
</div>
<div class="vsb-space new_inforBox fl">
  <div class="dynamic"><H2>学术交流</H2><SPAN><A href="xsjl.htm">more</A></SPAN></div>
  <ul class="dynamic_list dynamic_lista fr">
    <li><a href="info/1170/1000.htm"><span>2026-05-01</span><em>学术交流栏文章标题</em></a></li>
  </ul>
</div>
</body></html>`
	items, err := parseAdapter_scyshjList([]byte(portalHTML), scyshjCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Type != "通知公告" || items[1].Type != "学术交流" {
		t.Fatalf("section types = %q/%q", items[0].Type, items[1].Type)
	}
}

func TestScyshjIndexFeedStaysOnThePortalPage(t *testing.T) {
	portalHTML := readScyshjFixture(t, "scyshj_home_portal.html")
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(portalHTML)
	}))
	defer server.Close()

	site := AllSites["scyshj"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	ClearCache()
	defer func() {
		site.BaseURL, NoCache = oldBase, oldNoCache
		ClearCache()
	}()
	site.BaseURL = server.URL
	NoCache = true

	items, err := site.ListNewsRange("index", "2020-01-01", "2030-01-01", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("index feed items = %d, want 8", len(items))
	}
	for _, path := range requests {
		if path != "/index.htm" && path != "/index.html" {
			t.Fatalf("index feed walked beyond the portal page: %v", requests)
		}
	}
}

func TestScyshjEmptyColumnPageIsNotAList(t *testing.T) {
	// zchj.htm is a real column list page whose <ul> is empty; it stays
	// rejected, which is why the zchj feed was dropped from SubPages.
	if items, err := parseAdapter_scyshjList(readScyshjFixture(t, "scyshj_zchj_empty.html"), scyshjCanonicalBaseURL); err == nil || items != nil {
		t.Fatalf("empty column page was accepted: items=%#v err=%v", items, err)
	}
}

func TestScyshjArticleParsesCurrentMainContitTemplate(t *testing.T) {
	// The live detail template now renders the headline in .main_contit and
	// the byline in .main_contit p instead of .article h1 + .news_source.
	article, err := parseAdapter_scyshjArticle(readScyshjFixture(t, "scyshj_article_main_contit.html"), scyshjCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "四川省营商环境咨询委员会章程（修订）" {
		t.Errorf("title = %q", article.Title)
	}
	if article.Type != "通知公告" || article.Date != "2025-08-12" || article.PublishedAt != "2025-08-12 15:27:00" {
		t.Errorf("type/date/published = %q/%q/%q", article.Type, article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "第一章 总 则") || strings.Contains(article.Content, "导航噪声") {
		t.Errorf("content = %q", article.Content)
	}
}
