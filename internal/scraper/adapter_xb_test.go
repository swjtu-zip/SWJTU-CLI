package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

const xbTestBaseURL = "https://xb.swjtu.edu.cn"

func loadXBFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestXBAdapterRegistrationAndTypeMapping(t *testing.T) {
	site, ok := AllSites["xb"]
	if !ok {
		t.Fatal("xb site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("xb adapter hooks are incomplete: %+v", site)
	}
	wantSubPages := []string{"xxfb/tzgg", "xxfb/xwdt", "cyxz", "fwzn/gflc", "fwzn/glwj", "zdgf/bgswj", "zdgf/xjwj"}
	if len(site.SubPages) != len(wantSubPages) {
		t.Fatalf("unexpected xb sub-pages: %#v", site.SubPages)
	}
	for i, want := range wantSubPages {
		if site.SubPages[i] != want {
			t.Errorf("xb sub-page %d = %q, want %q", i, site.SubPages[i], want)
		}
	}
	wantTypes := map[string]string{
		"xxfb/tzgg":  "通知公告",
		"xxfb/xwdt":  "新闻动态",
		"cyxz":       "常用下载",
		"fwzn/gflc":  "规范流程",
		"fwzn/glwj":  "管理文件",
		"zdgf/bgswj": "办公室文件",
		"zdgf/xjwj":  "校级文件",
	}
	for slug, want := range wantTypes {
		if got := xbFeedTypes[slug]; got != want {
			t.Errorf("xb type for %s = %q, want %q", slug, got, want)
		}
	}

	cases := []struct {
		slug  string
		page  int
		total int
		want  string
	}{
		{"xxfb/xwdt", 1, 37, xbTestBaseURL + "/xxfb/xwdt.htm"},
		{"xxfb/xwdt", 2, 37, xbTestBaseURL + "/xxfb/xwdt/36.htm"},
		{"xxfb/xwdt", 19, 37, xbTestBaseURL + "/xxfb/xwdt/19.htm"},
		{"xxfb/xwdt", 37, 37, xbTestBaseURL + "/xxfb/xwdt/1.htm"},
		{"xxfb/xwdt", 38, 37, ""},
		{"xxfb/tzgg", 2, 2, xbTestBaseURL + "/xxfb/tzgg/1.htm"},
		{"xxfb/tzgg", 3, 2, ""},
		{"fwzn/gflc", 1, 1, xbTestBaseURL + "/fwzn/gflc.htm"},
		{"unknown", 2, 0, ""},
	}
	for _, tc := range cases {
		if got := site.PageURL(xbTestBaseURL, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("page URL %s page %d/%d = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestXBHomepageKeepsOnlyTheTwoRealArticleModules(t *testing.T) {
	items, err := parseAdapter_xbList(loadXBFixture(t, "xb.html"), xbTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 16 {
		t.Fatalf("homepage items = %d, want 16: %#v", len(items), items)
	}
	want := []struct {
		title, url, date, typ string
	}{
		{"闫学东校长参加校长办公室党支部4月主题党日活动", xbTestBaseURL + "/info/1019/18721.htm", "2026-04-03", "新闻动态"},
		{"【通知公告】西南交通大学2026年春季学期校领导接待日安排", xbTestBaseURL + "/info/1017/18711.htm", "2026-03-20", "通知公告"},
	}
	for _, expected := range want {
		found := false
		for _, item := range items {
			if item.URL != expected.url {
				continue
			}
			found = true
			if item.Title != expected.title || item.Date != expected.date || item.PublishedAt != "" || item.Type != expected.typ {
				t.Errorf("homepage item = (%q, %q, %q, %q, %q), want (%q, %q, %q, empty, %q)", item.Title, item.URL, item.Date, item.PublishedAt, item.Type, expected.title, expected.url, expected.date, expected.typ)
			}
		}
		if !found {
			t.Errorf("homepage item %s was not found", expected.url)
		}
	}
	for _, item := range items {
		if strings.Contains(item.URL, "/info/1001/") || item.Type != "新闻动态" && item.Type != "通知公告" {
			t.Fatalf("non-article homepage content leaked into feed: %#v", item)
		}
	}
	if items[7].Type != "新闻动态" || items[8].Type != "通知公告" || items[15].Type != "通知公告" {
		t.Fatalf("homepage module boundary types = %q, %q, %q", items[7].Type, items[8].Type, items[15].Type)
	}
}

func TestXBArchiveFixturesCoverBoundariesAndAllRealTypes(t *testing.T) {
	cases := []struct {
		name, fixture, typ, firstURL, lastURL string
		total, count                          int
	}{
		{"news first", "xb_live_xwdt_first.html", "新闻动态", xbTestBaseURL + "/info/1019/18721.htm", xbTestBaseURL + "/info/1019/5518.htm", 37, 11},
		{"news middle", "xb_live_xwdt_middle.html", "新闻动态", xbTestBaseURL + "/info/1019/5738.htm", xbTestBaseURL + "/info/1019/7638.htm", 37, 11},
		{"news last", "xb_live_xwdt_last.html", "新闻动态", xbTestBaseURL + "/info/1019/1762.htm", xbTestBaseURL + "/info/1019/1727.htm", 37, 7},
		{"notice first", "xb_live_tzgg_first.html", "通知公告", xbTestBaseURL + "/info/1017/18711.htm", xbTestBaseURL + "/info/1017/1782.htm", 2, 11},
		{"notice last", "xb_live_tzgg_last.html", "通知公告", xbTestBaseURL + "/info/1017/1781.htm", xbTestBaseURL + "/info/1017/1047.htm", 2, 5},
		{"common downloads", "xb_live_cyxz.html", "常用下载", xbTestBaseURL + "/info/1005/4438.htm", xbTestBaseURL + "/info/1005/1734.htm", 1, 2},
		{"standard process", "xb_live_gflc.html", "规范流程", xbTestBaseURL + "/info/1431/18671.htm", xbTestBaseURL + "/info/1431/18691.htm", 1, 3},
		{"management files", "xb_live_glwj.html", "管理文件", xbTestBaseURL + "/info/1421/18631.htm", xbTestBaseURL + "/info/1421/18661.htm", 1, 4},
		{"office files", "xb_live_bgswj.html", "办公室文件", xbTestBaseURL + "/info/1064/18441.htm", xbTestBaseURL + "/info/1064/18541.htm", 1, 9},
		{"university files", "xb_live_xjwj.html", "校级文件", xbTestBaseURL + "/info/1054/18581.htm", xbTestBaseURL + "/info/1054/18621.htm", 1, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := loadXBFixture(t, tc.fixture)
			doc, err := parseXBDocument(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := xbPageCount(doc); got != tc.total {
				t.Fatalf("page count = %d, want %d", got, tc.total)
			}
			items, err := parseAdapter_xbList(raw, xbTestBaseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.count {
				t.Fatalf("items = %d, want %d: %#v", len(items), tc.count, items)
			}
			if items[0].URL != tc.firstURL || items[len(items)-1].URL != tc.lastURL {
				t.Fatalf("boundary URLs = %q ... %q, want %q ... %q", items[0].URL, items[len(items)-1].URL, tc.firstURL, tc.lastURL)
			}
			for _, item := range items {
				if item.Type != tc.typ || item.Date == "" || item.PublishedAt != "" {
					t.Errorf("bad normalized list item: %#v", item)
				}
			}
		})
	}
}

func parseXBDocument(raw []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
}

func TestXBTotalPagesHookUsesSourcePaginationAndRejectsOutOfBounds(t *testing.T) {
	fixtures := map[string][]byte{
		"/xxfb/xwdt.htm": loadXBFixture(t, "xb_live_xwdt_first.html"),
		"/xxfb/tzgg.htm": loadXBFixture(t, "xb_live_tzgg_first.html"),
		"/fwzn/gflc.htm": loadXBFixture(t, "xb_live_gflc.html"),
		"/zdgf/xjwj.htm": loadXBFixture(t, "xb_live_xjwj.html"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := fixtures[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	site := AllSites["xb"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	site.BaseURL, NoCache = server.URL, true
	t.Cleanup(func() {
		site.BaseURL, NoCache = oldBase, oldNoCache
		ClearCache()
	})
	for slug, want := range map[string]int{"xxfb/xwdt": 37, "xxfb/tzgg": 2, "fwzn/gflc": 1, "zdgf/xjwj": 1} {
		if got, err := site.GetTotalPages(slug); err != nil || got != want {
			t.Errorf("total pages %s = %d, %v; want %d", slug, got, err, want)
		}
	}
	if _, err := site.GetTotalPages("bmgk"); err == nil {
		t.Fatal("static department page was accepted as an article list")
	}
	if _, err := site.GetTotalPages("unknown"); err == nil {
		t.Fatal("unknown xb route was accepted")
	}
	if _, err := parseAdapter_xbList(loadXBFixture(t, "xb_live_404.html"), xbTestBaseURL); err == nil {
		t.Fatal("404 page was accepted as a list")
	}
	if _, err := parseAdapter_xbList(loadXBFixture(t, "xb_live_waf.html"), xbTestBaseURL); err == nil {
		t.Fatal("verification page was accepted as a list")
	}
}

func TestXBArticleUsesFixedMetadataAndBodyZone(t *testing.T) {
	article, err := parseAdapter_xbArticle(loadXBFixture(t, "xb_live_article.html"), xbTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "西南交通大学2026年春季学期校领导接待日安排" || article.Type != "通知公告" {
		t.Fatalf("fixed article title/type = %q/%q", article.Title, article.Type)
	}
	if article.Date != "2026-03-20" || article.PublishedAt != "" {
		t.Fatalf("date-only metadata was changed: date=%q published=%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "校领导接待日") || strings.Contains(article.Content, "有效附件") || strings.Contains(article.Content, "页面底部") {
		t.Fatalf("body zone was not restricted: %q", article.Content)
	}
	if !strings.Contains(article.ContentHTML, "校领导接待日") || strings.Contains(article.ContentHTML, "页面底部") {
		t.Fatalf("content HTML was not restricted to the fixed body: %s", article.ContentHTML)
	}
	if len(article.Images) != 1 || article.Images[0] != xbTestBaseURL+"/__local/xb-notice.jpg" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 || article.Attachments[0].Name != "有效附件.pdf" || article.Attachments[1].Name != "有效材料.docx" {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
}

func TestXBArticlePreservesPublicationClockAndCorrectType(t *testing.T) {
	article, err := parseAdapter_xbArticle(loadXBFixture(t, "xb_live_article_datetime.html"), xbTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "来信来访处理流程" || article.Type != "规范流程" || article.Date != "2024-09-04" || article.PublishedAt != "2024-09-04 08:07:06" {
		t.Fatalf("fixed date/type metadata = title=%q type=%q date=%q published=%q", article.Title, article.Type, article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "流程正文") || strings.Contains(article.Content, "活动时间：2026年") {
		t.Fatalf("detail body was polluted by outside metadata: %q", article.Content)
	}
}

func TestXBFetchArticleVerifiesResourcesAndPreservesExternalLinks(t *testing.T) {
	fixture := loadXBFixture(t, "xb_live_article_resources.html")
	var resourceRequests []string
	var resourceReferers []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1017/9001.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(fixture)
		case "/images/good.png":
			resourceRequests = append(resourceRequests, r.URL.Path)
			resourceReferers = append(resourceReferers, r.Header.Get("Referer"))
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})
		case "/images/bad.png":
			resourceRequests = append(resourceRequests, r.URL.Path)
			resourceReferers = append(resourceReferers, r.Header.Get("Referer"))
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html><body>验证码</body></html>"))
		case "/images/missing.png":
			resourceRequests = append(resourceRequests, r.URL.Path)
			resourceReferers = append(resourceReferers, r.Header.Get("Referer"))
			http.NotFound(w, r)
		case "/files/good.pdf":
			resourceRequests = append(resourceRequests, r.URL.Path)
			resourceReferers = append(resourceReferers, r.Header.Get("Referer"))
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7 fixture"))
		case "/files/bad.pdf":
			resourceRequests = append(resourceRequests, r.URL.Path)
			resourceReferers = append(resourceReferers, r.Header.Get("Referer"))
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("not a PDF"))
		case "/system/_content/download.jsp":
			resourceRequests = append(resourceRequests, r.URL.Path+"?"+r.URL.RawQuery)
			resourceReferers = append(resourceReferers, r.Header.Get("Referer"))
			if r.URL.Query().Get("wbfileid") == "GOOD" {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write([]byte{'P', 'K', 0x03, 0x04, 'X', 'B'})
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html><body>请输入验证码下载附件</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["xb"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	site.BaseURL, NoCache = server.URL, true
	t.Cleanup(func() {
		site.BaseURL, NoCache = oldBase, oldNoCache
		ClearCache()
	})
	article, raw, err := xbFetchArticle(server.URL + "/info/1017/9001.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || len(article.Images) != 1 || article.Images[0] != server.URL+"/images/good.png" {
		t.Fatalf("raw/images = %d/%#v", len(raw), article.Images)
	}
	if len(article.Attachments) != 2 || article.Attachments[0].Name != "有效PDF.pdf" || article.Attachments[1].Name != "有效材料.docx" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	for _, attachment := range article.Attachments {
		if attachment.Size == "" {
			t.Fatalf("verified attachment has no size: %#v", attachment)
		}
	}
	for _, rejected := range []string{"bad.png", "missing.png", "bad.pdf", "wbfileid=BAD", "验证码材料"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource remained in ContentHTML: %q in %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, server.URL+"/images/good.png") || !strings.Contains(article.ContentHTML, server.URL+"/files/good.pdf") || !strings.Contains(article.ContentHTML, "https://example.org/guide.pdf") {
		t.Fatalf("valid or external link was lost: %s", article.ContentHTML)
	}
	if len(resourceRequests) != 7 {
		t.Fatalf("resource requests = %#v, want image/pdf/download validation for all seven local candidates", resourceRequests)
	}
	for _, referer := range resourceReferers {
		if referer != server.URL+"/info/1017/9001.htm" {
			t.Errorf("resource referer = %q, want detail URL", referer)
		}
	}
}

func TestXBResourceValidationChecksHTTPStatusContentTypeMagicAndCAPTCHA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		case "/fake.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>验证码</html>"))
		case "/ok.pdf":
			w.Header().Set("Content-Type", "application/pdf; charset=binary")
			_, _ = w.Write([]byte("%PDF-1.7"))
		case "/fake.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<html>登录页面</html>"))
		case "/captcha":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html><title>附件下载</title>请输入验证码</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if body, typ, err := xbFetchResource(server.URL+"/ok.png", server.URL+"/info/1017/9001.htm", true); err != nil || typ != "image/png" || len(body) == 0 {
		t.Fatalf("valid image = %d/%q/%v", len(body), typ, err)
	}
	for _, tc := range []struct {
		path  string
		image bool
	}{
		{server.URL + "/fake.png", true},
		{server.URL + "/ok.pdf", true},
		{server.URL + "/fake.pdf", false},
		{server.URL + "/captcha", false},
		{server.URL + "/missing.pdf", false},
	} {
		if _, _, err := xbFetchResource(tc.path, server.URL+"/info/1017/9001.htm", tc.image); err == nil {
			t.Errorf("invalid resource %s was accepted", tc.path)
		}
	}
	if xbResourceBodyIsUsable("application/octet-stream", []byte("<html>captcha</html>"), "download.jsp", false) {
		t.Fatal("HTML CAPTCHA shell was accepted as a file")
	}
}

func TestXBArticleWithoutFixedBodyReturnsErrorNotPanic(t *testing.T) {
	raw := []byte(`<html><head><meta name="pageTitle" content="通知公告"></head><body>
<form name="_newscontent_fromname"><div class="left-top"><div class="news-title">测试标题</div></div></form>
</body></html>`)
	article, err := parseAdapter_xbArticle(raw, "https://xb.swjtu.edu.cn")
	if err == nil || article != nil {
		t.Fatalf("parseAdapter_xbArticle() = article=%v err=%v, want a body-missing error", article, err)
	}
}
