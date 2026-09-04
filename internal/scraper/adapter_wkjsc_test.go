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

func readWkjscFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestWkjscAdapterRegistered(t *testing.T) {
	site := AllSites["wkjsc"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("wkjsc adapter hooks are not registered")
	}
}

func TestWkjscSubPagesAreAuditedArticleColumns(t *testing.T) {
	want := []string{
		"tzgg", "wkxw", "xsdt", "wkpt", "gzzd", "gzzd/gj", "gzzd/jyb", "gzzd/qt",
		"xzzx", "xzzx/gj", "xzzx/jyb", "xzzx/qt", "bszn",
	}
	if !reflect.DeepEqual(wkjscSubPages, want) {
		t.Fatalf("wkjsc sub-pages = %#v, want %#v", wkjscSubPages, want)
	}
	for _, excluded := range []string{"wkyx", "gywm", "gzzd/sskl", "xzzx/sskl"} {
		for _, slug := range wkjscSubPages {
			if slug == excluded {
				t.Fatalf("empty/static route %q was registered", excluded)
			}
		}
	}
}

func TestWkjscListUsesFixedColumnAndRejectsNonArticles(t *testing.T) {
	items, err := parseAdapter_wkjscList(readWkjscFixture(t, "wkjsc_live_list.html"), "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 valid same-site articles", len(items))
	}
	for _, item := range items {
		if item.Type != "文科新闻" {
			t.Errorf("item %q type = %q, want 文科新闻", item.Title, item.Type)
		}
		if item.Date == "" || !strings.HasPrefix(item.URL, "https://wkjsc.swjtu.edu.cn/info/") {
			t.Errorf("unexpected list item: %+v", item)
		}
	}
	pageDoc, err := goquery.NewDocumentFromReader(strings.NewReader(string(readWkjscFixture(t, "wkjsc_live_list.html"))))
	if err != nil {
		t.Fatal(err)
	}
	if got := wkjscPageCount(pageDoc); got != 14 {
		t.Fatalf("wkjsc page count = %d, want 14", got)
	}
}

func TestWkjscReversePaginationRejectsBoundaries(t *testing.T) {
	if got := reverseBodaPageURL("https://wkjsc.swjtu.edu.cn", "wkxw", 1, 14, ".htm"); got != "https://wkjsc.swjtu.edu.cn/wkxw.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := reverseBodaPageURL("https://wkjsc.swjtu.edu.cn", "wkxw", 2, 14, ".htm"); got != "https://wkjsc.swjtu.edu.cn/wkxw/13.htm" {
		t.Fatalf("second page URL = %q", got)
	}
	if got := reverseBodaPageURL("https://wkjsc.swjtu.edu.cn", "wkxw", 14, 14, ".htm"); got != "https://wkjsc.swjtu.edu.cn/wkxw/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	for _, page := range []int{0, 15} {
		if got := reverseBodaPageURL("https://wkjsc.swjtu.edu.cn", "wkxw", page, 14, ".htm"); got != "" {
			t.Errorf("out-of-range page %d URL = %q, want empty", page, got)
		}
	}
}

func TestWkjscListUsesDatedNewsRows(t *testing.T) {
	body, err := os.ReadFile("testdata/wkjsc.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_wkjscList(body, "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("got %d items, want 20 dated news rows", len(items))
	}
	for _, item := range items {
		if item.Title == "详细" {
			t.Errorf("slideshow summary leaked as an article: %+v", item)
		}
		if item.Date == "" {
			t.Errorf("article %q has no date", item.Title)
		}
		if !strings.Contains(item.URL, "/info/") {
			t.Errorf("non-article URL leaked: %q", item.URL)
		}
	}
}

func TestWkjscArticleReadsLegacyMetaLine(t *testing.T) {
	body := []byte(`<html><head><title>测试文章-文科建设处</title></head><body>
<div id="div_title"><span id="lb_title">测试文章</span><br>
<span id="lb_hits">[2026-05-18 来源 新华社 编辑: 浏览 3次]</span></div>
<div id="div_wenzi"><div id="vsb_content"><div class="v_news_content"><p>这是一段足够长的正文，用于验证文科建设处旧模板的详情元数据解析。</p></div></div></div>
</body></html>`)
	article, err := parseAdapter_wkjscArticle(body, "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "测试文章" || article.Date != "2026-05-18" || article.Source != "新华社" {
		t.Fatalf("unexpected article: %+v", article)
	}
}

func TestWkjscArticleUsesDetailTitleInsteadOfSiteName(t *testing.T) {
	body := readWkjscFixture(t, "wkjsc_article.html")

	generic, err := parseBodaArticle(body, "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if generic.Title != wkjscSiteName {
		t.Fatalf("fixture no longer reproduces generic site-name title: %q", generic.Title)
	}

	article, err := parseAdapter_wkjscArticle(body, "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}

	want := "学校获四川省社会科学优秀成果特等奖"
	if article.Title != want {
		t.Fatalf("article title = %q, want %q", article.Title, want)
	}
	if article.Title == wkjscSiteName {
		t.Fatalf("site name leaked into article title: %q", article.Title)
	}
	if article.Date != "2026-05-20" {
		t.Fatalf("article date = %q, want 2026-05-20", article.Date)
	}
	if article.Source != "新华社" {
		t.Fatalf("article source = %q, want 新华社", article.Source)
	}
	if !strings.Contains(article.Content, "四川省社会科学评奖委员会") {
		t.Fatalf("article content is missing: %q", article.Content)
	}
}

func TestWkjscArticleUsesStrictMetadataAndBody(t *testing.T) {
	article, err := parseAdapter_wkjscArticle(readWkjscFixture(t, "wkjsc_live_article_resources.html"), "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "严格发布日期测试标题" || article.Type != "通知公告" {
		t.Fatalf("unexpected fixed metadata: %+v", article)
	}
	if article.Date != "2026-08-30" || article.PublishedAt != "2026-08-30 09:08:07" {
		t.Fatalf("unexpected publication time: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "截止时间：2099年12月31日") {
		t.Fatalf("body was not retained: %q", article.Content)
	}
	if article.Date == "2099-12-31" || !strings.Contains(article.ContentHTML, "/good.png") {
		t.Fatalf("publication date was contaminated or image was not resolved: %+v", article)
	}
	if len(article.Attachments) != 3 {
		t.Fatalf("got %d attachment candidates, want 3 before HTTP verification", len(article.Attachments))
	}
}

func TestWkjscArticleRejectsErrorShell(t *testing.T) {
	if _, err := parseAdapter_wkjscArticle([]byte(`<html><head><meta name="pageType" content="1"></head><body><div id="div_title"><span id="lb_title">验证码</span></div><div id="div_wenzi"><div id="vsb_content"><div class="v_news_content">安全验证</div></div></div></body></html>`), "https://wkjsc.swjtu.edu.cn"); err == nil {
		t.Fatal("CAPTCHA/error shell was accepted as an article")
	}
}

func TestWkjscFetchArticleVerifiesResources(t *testing.T) {
	articleHTML := readWkjscFixture(t, "wkjsc_live_article_resources.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1002/9001.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(articleHTML)
		case "/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})
		case "/bad.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>captcha</html>"))
		case "/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\nvalid"))
		case "/bad.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("not a pdf"))
		case "/missing.pdf":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldClient, oldNoCache := defaultClient, NoCache
	defaultClient, NoCache = server.Client(), true
	t.Cleanup(func() { defaultClient, NoCache = oldClient, oldNoCache })

	article, _, err := wkjscFetchArticleAt(server.URL+"/info/1002/9001.htm", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != server.URL+"/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "bad.png") || strings.Contains(article.ContentHTML, "bad.pdf") {
		t.Fatalf("invalid resources remained in content HTML: %s", article.ContentHTML)
	}
}

func TestWkjscResourceBodyRequiresTypeAndMagic(t *testing.T) {
	if wkjscResourceBodyIsUsable("image/png", []byte("<html>captcha</html>"), "bad.png", true) {
		t.Fatal("HTML/CAPTCHA body accepted as image")
	}
	if wkjscResourceBodyIsUsable("application/pdf", []byte("plain text"), "bad.pdf", false) {
		t.Fatal("plain text accepted as PDF")
	}
	if !wkjscResourceBodyIsUsable("application/pdf", []byte("%PDF-1.7\n"), "good.pdf", false) {
		t.Fatal("valid PDF signature rejected")
	}
}
