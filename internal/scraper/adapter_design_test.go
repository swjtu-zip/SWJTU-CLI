package scraper

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

const designFixtureBase = "https://design.swjtu.edu.cn"

func TestDesignAdapterRegistrationAndPageURL(t *testing.T) {
	site, ok := AllSites["design"]
	if !ok {
		t.Fatal("design site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("design adapter hooks are incomplete: %+v", site)
	}

	if len(site.SubPages) != len(designSubPages) {
		t.Fatalf("design sub-pages = %d, want %d", len(site.SubPages), len(designSubPages))
	}
	for i, want := range designSubPages {
		if site.SubPages[i] != want {
			t.Errorf("design sub-page[%d] = %q, want %q", i, site.SubPages[i], want)
		}
	}
	for _, excluded := range []string{"xygk", "jyjx/bkzy", "jyjx/ssxwd", "jyjx/cybgxz", "kysj", "cpyc", "xszt"} {
		for _, slug := range site.SubPages {
			if slug == excluded {
				t.Errorf("static/resource/empty route %q was registered as a news feed", excluded)
			}
		}
	}

	cases := []struct {
		slug        string
		page, total int
		want        string
	}{
		{"xydt", 1, 25, designFixtureBase + "/xydt.htm"},
		{"xydt", 2, 25, designFixtureBase + "/xydt/24.htm"},
		{"xydt", 13, 25, designFixtureBase + "/xydt/13.htm"},
		{"xydt", 25, 25, designFixtureBase + "/xydt/1.htm"},
		{"xshd/jz", 2, 8, designFixtureBase + "/xshd/jz/7.htm"},
		{"xshd/jz", 8, 8, designFixtureBase + "/xshd/jz/1.htm"},
		{"xshd/jz", 9, 8, ""},
	}
	for _, tc := range cases {
		if got := designPageURL(designFixtureBase+"/", tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("designPageURL(%q, %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestDesignTotalPagesRequiresSourcePagination(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	defaultClient = &http.Client{Transport: designTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := `<html><body><div class="file"><div class="list"><ul><li><a href="/info/1016/1.htm"><span>源站文章</span><i>2026-09-01</i></a></li></ul></div><div class="paging"><span class="p_no_d">25</span></div></div></body></html>`
		if r.URL.Path == "/xshd.htm" {
			body = `<html><body><div class="file"><div class="list"><ul><li><a href="/info/1116/1.htm"><span>源站文章</span><i>2026-09-01</i></a></li></ul></div></div></body></html>`
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	NoCache = true
	t.Cleanup(func() { defaultClient, NoCache = oldClient, oldNoCache; ClearCache() })

	if total, err := designTotalPages("xydt"); err != nil || total != 25 {
		t.Fatalf("source-reported total = %d, err=%v; want 25", total, err)
	}
	if total, err := designTotalPages("xshd"); err == nil || total != 0 {
		t.Fatalf("missing source pagination was accepted: total=%d err=%v", total, err)
	}
}

func TestDesignOfficialHomepageFixtureCoversHiddenTabs(t *testing.T) {
	items, err := parseAdapter_designList(readDesignFixture(t, "design.html"), designFixtureBase)
	if err != nil {
		t.Fatalf("parse official design homepage: %v", err)
	}
	wantTypes := map[string]int{
		"学院动态":  5,
		"讲座":    5,
		"展览":    5,
		"论坛":    5,
		"活动":    5,
		"通知公告":  7,
		"本科生通知": 7,
		"研究生通知": 7,
	}
	gotTypes := make(map[string]int)
	for _, item := range items {
		gotTypes[item.Type]++
	}
	if len(items) != 46 {
		t.Fatalf("official homepage returned %d items, want 46: %#v", len(items), gotTypes)
	}
	for typ, want := range wantTypes {
		if gotTypes[typ] != want {
			t.Errorf("official homepage type %q count = %d, want %d", typ, gotTypes[typ], want)
		}
	}
	var foundNotice, foundEvent bool
	for _, item := range items {
		if item.Title == "设计艺术学院关于公开选聘校外生涯导师的邀请函" {
			foundNotice = item.Type == "通知公告" && item.Date == "2025-05-30"
		}
		if item.Title == "品物浅深：山水画诗意展现与笔墨精神" {
			foundEvent = item.Type == "讲座" && item.Date == "2026-06-14"
		}
		if strings.Contains(item.Title, "2025.05.30") {
			t.Fatalf("notice date leaked into title: %#v", item)
		}
	}
	if !foundNotice || !foundEvent {
		t.Fatalf("official homepage fixed title/date evidence missing (notice=%v event=%v)", foundNotice, foundEvent)
	}
}

func TestDesignHomepageScopesVisibleAndHiddenArticleModules(t *testing.T) {
	html := readDesignFixture(t, "design_adapter_home.html")
	items, err := parseAdapter_designList(html, designFixtureBase)
	if err != nil {
		t.Fatalf("parse design homepage: %v", err)
	}
	if len(items) != 8 {
		t.Fatalf("got %d homepage articles, want 8: %#v", len(items), items)
	}

	if items[0].Title != "学院动态真实文章" || items[0].Type != "学院动态" || items[0].Date != "2026-09-01" {
		t.Fatalf("unexpected dynamic item: %#v", items[0])
	}
	if items[1].Type != "讲座" || items[1].Date != "2026-07-10" || items[1].PublishedAt != "2026-07-10 14:30:00" {
		t.Fatalf("event timestamp was not preserved: %#v", items[1])
	}
	if items[3].Type != "论坛" || items[4].Type != "活动" {
		t.Fatalf("hidden academic tab types were not retained: %#v", items[2:5])
	}
	if items[5].Type != "通知公告" || items[6].Type != "本科生通知" || items[7].Type != "研究生通知" {
		t.Fatalf("hidden notice tab types were not retained: %#v", items[5:])
	}
	for _, item := range items {
		if strings.Contains(item.Title, "导航") || strings.Contains(item.Title, "轮播") || strings.Contains(item.URL, "mp.weixin") {
			t.Fatalf("non-module link leaked into homepage items: %#v", item)
		}
	}
}

func TestDesignListRejectsNavigationAndExternalLinks(t *testing.T) {
	html := readDesignFixture(t, "design_adapter_list.html")
	items, err := parseAdapter_designList(html, designFixtureBase)
	if err != nil {
		t.Fatalf("parse design list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list articles, want 2: %#v", len(items), items)
	}
	if items[0].Title != "学院动态列表文章一" || items[0].Date != "2026-08-01" || items[0].Type != "学院动态" {
		t.Fatalf("unexpected first list item: %#v", items[0])
	}
	if items[1].Title != "学院动态列表文章二" || items[1].URL != designFixtureBase+"/info/1016/2.htm" {
		t.Fatalf("unexpected second list item: %#v", items[1])
	}
}

func TestDesignAdapterDistinguishesEmptyColumnFromShell(t *testing.T) {
	empty := []byte(`<html><body><div class="file"><div class="title">科研实践</div><div class="list"><ul></ul></div></div></body></html>`)
	items, err := parseAdapter_designList(empty, designFixtureBase)
	if err != nil {
		t.Fatalf("reachable empty column should parse: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty column produced articles: %#v", items)
	}
	if _, err := parseAdapter_designList([]byte(`<html><body><div id="app"></div></body></html>`), designFixtureBase); err == nil {
		t.Fatal("static shell was accepted as a design list")
	}
}

func TestDesignArticleFieldsImagesAndAttachments(t *testing.T) {
	html := readDesignFixture(t, "design_adapter_article.html")
	article, err := parseAdapter_designArticle(html, designFixtureBase)
	if err != nil {
		t.Fatalf("parse design article: %v", err)
	}
	if article.Title != "设计艺术学院详情测试通知" || article.Date != "2026-09-01" || article.PublishedAt != "2026-09-01 12:34:00" {
		t.Fatalf("unexpected article title/time: %+v", article)
	}
	if article.Type != "学院动态" {
		t.Fatalf("unexpected article type: %+v", article)
	}
	if !strings.Contains(article.Content, "正文段落") || strings.Contains(article.Content, "侧栏推荐") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != designFixtureBase+"/__local/article.jpg" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("got %d attachments, want 2: %#v", len(article.Attachments), article.Attachments)
	}
	if article.Attachments[0].Name != "正文附件.pdf" || article.Attachments[0].URL != designFixtureBase+"/files/body.pdf" {
		t.Fatalf("unexpected body attachment: %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "外部附件.xlsx" || article.Attachments[1].URL != designFixtureBase+"/system/_content/download.jsp?id=fixture" {
		t.Fatalf("unexpected outside attachment: %#v", article.Attachments[1])
	}
}

func TestDesignAcademicArticleUsesEventHeader(t *testing.T) {
	html := readDesignFixture(t, "design_adapter_event_article.html")
	article, err := parseAdapter_designArticle(html, designFixtureBase)
	if err != nil {
		t.Fatalf("parse design event article: %v", err)
	}
	if article.Title != "人因与智能交互驱动创新设计的四种模式" || article.Type != "讲座" {
		t.Fatalf("unexpected event header: %+v", article)
	}
	if article.Date != "2026-07-10" || article.PublishedAt != "2026-07-10 14:30:00" {
		t.Fatalf("event date/time was not retained: %+v", article)
	}
	if !strings.Contains(article.Content, "学术活动正文") || len(article.Images) != 1 {
		t.Fatalf("event body/image was not parsed: %+v", article)
	}
}

func TestDesignArticleRejectsNonArticleResponse(t *testing.T) {
	if _, err := parseAdapter_designArticle([]byte(`<html><head><title>404错误提示</title></head><body>404</body></html>`), designFixtureBase); err == nil {
		t.Fatal("error page was accepted as an article")
	}
	if _, err := parseAdapter_designArticle([]byte(`<div class="news-title">首页壳</div><div id="app"></div>`), designFixtureBase); err == nil {
		t.Fatal("homepage shell was accepted as an article")
	}
}

func TestDesignArticleUsesFixedHeaderDateOnly(t *testing.T) {
	html := []byte(`<html><body><div class="newList"><div class="title">政策文件</div><div class="content"><div><h1>政策正文标题</h1><div>2022-09-01 点击量：[1]</div><hr><div id="vsb_content"><div class="v_news_content"><p>活动时间：2099年12月31日 23:59</p></div></div></div></div></div></body></html>`)
	article, err := parseAdapter_designArticle(html, designFixtureBase)
	if err != nil {
		t.Fatalf("parse static-content design article: %v", err)
	}
	if article.Date != "2022-09-01" || article.PublishedAt != "2022-09-01" {
		t.Fatalf("body date replaced fixed header date: %+v", article)
	}
	if article.Type != "政策文件" {
		t.Fatalf("static-content type = %q, want 政策文件", article.Type)
	}
}

func TestDesignListRejectsUnparseableArticleRows(t *testing.T) {
	html := []byte(`<html><body><div class="file"><div class="title">学院动态</div><div class="list"><ul><li><a href="/info/1016/77.htm"></a></li></ul></div></div></body></html>`)
	if _, err := parseAdapter_designList(html, designFixtureBase); err == nil {
		t.Fatal("article link without a parseable row was accepted")
	}
}

func TestDesignFetchArticleVerifiesResources(t *testing.T) {
	html := readDesignFixture(t, "design_adapter_resource_article.html")
	oldClient, oldNoCache := defaultClient, NoCache
	defaultClient = &http.Client{Transport: designTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		var contentType, body string
		switch r.URL.Path {
		case "/info/1016/77.htm":
			contentType = "text/html"
			body = strings.ReplaceAll(string(html), "__DESIGN_TEST_BASE__", designFixtureBase)
		case "/valid.png":
			contentType = "image/png"
			body = string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})
		case "/bad.png":
			contentType = "image/png"
			body = "<html><body>验证码</body></html>"
		case "/valid.pdf":
			contentType = "application/pdf"
			body = "%PDF-1.7\n"
		case "/bad.pdf":
			contentType = "application/pdf"
			body = "not a PDF"
		case "/captcha.pdf":
			contentType = "application/octet-stream"
			body = "<html><h1>请输入验证码下载附件</h1></html>"
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found")), Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	NoCache = true
	t.Cleanup(func() { defaultClient, NoCache = oldClient, oldNoCache; ClearCache() })

	article, raw, err := designFetchArticle(designFixtureBase + "/info/1016/77.htm")
	if err != nil {
		t.Fatalf("fetch design article: %v", err)
	}
	if len(raw) == 0 || len(article.Images) != 1 || article.Images[0] != designFixtureBase+"/valid.png" {
		t.Fatalf("resource image verification failed: raw=%d images=%#v", len(raw), article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != designFixtureBase+"/valid.pdf" {
		t.Fatalf("resource attachment verification failed: %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, designFixtureBase+"/valid.png") || !strings.Contains(article.ContentHTML, designFixtureBase+"/valid.pdf") {
		t.Fatalf("verified resources missing from ContentHTML: %q", article.ContentHTML)
	}
	for _, rejected := range []string{"bad.png", "bad.pdf", "captcha.pdf", "javascript:download"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource/link remained in ContentHTML: %q", rejected)
		}
	}
}

type designTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper designTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func readDesignFixture(t *testing.T, name string) []byte {
	t.Helper()
	html, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return html
}
