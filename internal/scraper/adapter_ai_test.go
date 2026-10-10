package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const aiBaseURL = "https://ai.swjtu.edu.cn"

func loadAIFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAIAdapterRegistrationAndReversePagination(t *testing.T) {
	site, ok := AllSites["ai"]
	if !ok {
		t.Fatal("ai site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("AI adapter hooks are incomplete: %+v", site)
	}
	wantSubPages := []string{"tzgg", "xwdt", "xsbg", "kxyj", "jcsj"}
	if len(site.SubPages) != len(wantSubPages) {
		t.Fatalf("unexpected AI sub-pages: %#v", site.SubPages)
	}
	for i, want := range wantSubPages {
		if site.SubPages[i] != want {
			t.Errorf("AI sub-page %d = %q, want %q", i, site.SubPages[i], want)
		}
	}

	cases := []struct {
		slug  string
		page  int
		total int
		want  string
	}{
		{"tzgg", 1, 2, aiBaseURL + "/tzgg.htm"},
		{"tzgg", 2, 2, aiBaseURL + "/tzgg/1.htm"},
		{"xwdt", 1, 4, aiBaseURL + "/xwdt.htm"},
		{"xwdt", 2, 4, aiBaseURL + "/xwdt/3.htm"},
		{"xwdt", 3, 4, aiBaseURL + "/xwdt/2.htm"},
		{"xwdt", 4, 4, aiBaseURL + "/xwdt/1.htm"},
		{"xwdt", 2, 1, aiBaseURL + "/xwdt/3.htm"},
		{"xwdt", 2, 10, aiBaseURL + "/xwdt/9.htm"},
		{"xwdt", 5, 4, ""},
		{"unknown", 2, 0, ""},
		{"xsbg", 2, 2, aiBaseURL + "/xsbg/1.htm"},
		{"kxyj", 2, 2, aiBaseURL + "/kxyj/1.htm"},
		{"jcsj", 1, 1, aiBaseURL + "/jcsj.htm"},
		{"index", 1, 1, aiBaseURL + "/index.htm"},
		{"index", 2, 1, ""},
	}
	for _, tc := range cases {
		if got := site.PageURL(aiBaseURL, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("page URL %s page %d/%d = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestAILiveFixturesCoverAuditedHomepageAndArchivePages(t *testing.T) {
	cases := []struct {
		name, fixture, firstURL, lastURL string
		total, count                     int
	}{
		{"homepage", "ai_live_home.html", aiBaseURL + "/info/1067/1301.htm", aiBaseURL + "/info/1111/1133.htm", 0, 35},
		{"notice first", "ai_live_tzgg_first.html", aiBaseURL + "/info/1067/1301.htm", aiBaseURL + "/info/1067/1095.htm", 2, 10},
		{"notice last", "ai_live_tzgg_last.html", aiBaseURL + "/info/1067/1094.htm", aiBaseURL + "/info/1067/1004.htm", 2, 4},
		{"news first", "ai_live_xwdt_first.html", aiBaseURL + "/info/1051/1312.htm", aiBaseURL + "/info/1051/1205.htm", 4, 10},
		{"news middle", "ai_live_xwdt_middle.html", aiBaseURL + "/info/1051/1202.htm", aiBaseURL + "/info/1051/1151.htm", 4, 10},
		{"news last", "ai_live_xwdt_last.html", aiBaseURL + "/info/1051/1043.htm", aiBaseURL + "/info/1051/1043.htm", 4, 1},
		{"report first", "ai_live_xsbg_first.html", aiBaseURL + "/info/1109/1322.htm", aiBaseURL + "/info/1109/1111.htm", 2, 10},
		{"report last", "ai_live_xsbg_last.html", aiBaseURL + "/info/1109/1105.htm", aiBaseURL + "/info/1109/1050.htm", 2, 10},
		{"research first", "ai_live_kxyj_first.html", aiBaseURL + "/info/1052/1271.htm", aiBaseURL + "/info/1052/1063.htm", 2, 10},
		{"research last", "ai_live_kxyj_last.html", aiBaseURL + "/info/1052/1062.htm", aiBaseURL + "/info/1052/1062.htm", 2, 1},
		{"highlights", "ai_live_jcsj.html", aiBaseURL + "/info/1111/1212.htm", aiBaseURL + "/info/1111/1133.htm", 1, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := loadAIFixture(t, tc.fixture)
			if tc.total > 0 && aiTotalPagesFromHTML(raw) != tc.total {
				t.Fatalf("total pages = %d, want %d", aiTotalPagesFromHTML(raw), tc.total)
			}
			items, err := parseAdapter_aiList(raw, aiBaseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.count {
				t.Fatalf("items = %d, want %d: %#v", len(items), tc.count, items)
			}
			if items[0].URL != tc.firstURL || items[len(items)-1].URL != tc.lastURL {
				t.Fatalf("boundary items = %q ... %q, want %q ... %q", items[0].URL, items[len(items)-1].URL, tc.firstURL, tc.lastURL)
			}
			for _, item := range items {
				if strings.Contains(item.URL, "swjtu.edu.cn/index") || strings.Contains(item.URL, "browsehappy") {
					t.Fatalf("non-article link leaked into feed: %#v", item)
				}
			}
		})
	}
}

func TestAITotalPagesHookReadsLivePageBox(t *testing.T) {
	firstPage := loadAIFixture(t, "ai_live_xwdt_first.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/xwdt.htm" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(firstPage)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	site := AllSites["ai"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	site.BaseURL, NoCache = server.URL, true
	t.Cleanup(func() {
		site.BaseURL, NoCache = oldBase, oldNoCache
		ClearCache()
	})

	total, err := site.GetTotalPages("xwdt")
	if err != nil || total != 4 {
		t.Fatalf("AI total page hook = %d, %v; want 4", total, err)
	}
}

// The homepage exposes the four article modules plus the 精彩瞬间 marquee and
// no pagination control, so the audited "index" feed is exactly one page.
// aiTotalPages must verify the homepage instead of rejecting the route.
func TestAITotalPagesHookAcceptsAuditedHomepageAsSinglePage(t *testing.T) {
	homePage := loadAIFixture(t, "ai_live_home.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.htm" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(homePage)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	site := AllSites["ai"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	site.BaseURL, NoCache = server.URL, true
	t.Cleanup(func() {
		site.BaseURL, NoCache = oldBase, oldNoCache
		ClearCache()
	})

	total, err := site.GetTotalPages("index")
	if err != nil || total != 1 {
		t.Fatalf("AI index total page hook = %d, %v; want 1", total, err)
	}
	if _, err := site.GetTotalPages("not-a-feed"); err == nil {
		t.Fatal("AI total page hook admitted an unknown route")
	}
}

func TestAIHomepageTotalPagesValidation(t *testing.T) {
	if !aiHomepageTotalPagesFromHTML(loadAIFixture(t, "ai_live_home.html")) {
		t.Fatal("captured AI homepage was not recognized as the audited index feed")
	}
	for name, raw := range map[string][]byte{
		"archive list": loadAIFixture(t, "ai_live_xwdt_first.html"),
		"static page":  []byte(`<html><head><meta name="pageType" content="2"></head><body><div class="article"><h1>人才培养</h1></div></body></html>`),
		"waf":          []byte(`<!doctype html><html><head><title>安全验证</title></head><body><h1>请求被拦截</h1></body></html>`),
	} {
		if aiHomepageTotalPagesFromHTML(raw) {
			t.Fatalf("%s was accepted as the AI index homepage", name)
		}
	}
}

func TestAITotalPagesRejectsStaticAndVerificationResponses(t *testing.T) {
	static := []byte(`<html><head><meta name="pageType" content="2"></head><body><div class="article"><h1>人才培养</h1></div></body></html>`)
	if got := aiTotalPagesFromHTML(static); got != 0 {
		t.Fatalf("static page total = %d, want 0", got)
	}
	waf := []byte(`<!doctype html><html><head><title>安全验证</title></head><body><h1>请求被拦截</h1></body></html>`)
	if got := aiTotalPagesFromHTML(waf); got != 0 {
		t.Fatalf("WAF page total = %d, want 0", got)
	}
}

func TestAIHomeParserKeepsOnlyFourArticleModules(t *testing.T) {
	items, err := parseAdapter_aiList(loadAIFixture(t, "ai_home.html"), aiBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("got %d home items, want four list modules plus 精彩瞬间: %#v", len(items), items)
	}
	want := []struct {
		title string
		url   string
		date  string
		typ   string
	}{
		{"通知模块真实文章", aiBaseURL + "/info/1067/1301.htm", "2020-08-07", "通知公告"},
		{"新闻模块真实文章", aiBaseURL + "/info/1051/1312.htm", "2020-09-18", "新闻动态"},
		{"报告模块真实文章", aiBaseURL + "/info/1109/1322.htm", "2021-01-19", "学术报告"},
		{"研究模块真实文章", aiBaseURL + "/info/1052/1271.htm", "2020-04-16", "科学研究"},
		{"精彩瞬间真实文章", aiBaseURL + "/info/1111/1212.htm", "", "精彩瞬间"},
	}
	for i, expected := range want {
		got := items[i]
		if got.Title != expected.title || got.URL != expected.url || got.Date != expected.date || got.Type != expected.typ || got.PublishedAt != "" {
			t.Errorf("home item %d = (%q, %q, %q, %q, %q), want (%q, %q, %q, %q, empty)", i, got.Title, got.URL, got.Date, got.Type, got.PublishedAt, expected.title, expected.url, expected.date, expected.typ)
		}
		if got.Title == "轮播伪文章" || got.Title == "导航伪文章" || strings.Contains(got.Title, "More") {
			t.Errorf("non-article content leaked into home items: %#v", got)
		}
	}
}

func TestAIArchiveParserExtractsRowsDatesAndTypes(t *testing.T) {
	items, err := parseAdapter_aiList(loadAIFixture(t, "ai_list.html"), aiBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d archive items, want 2: %#v", len(items), items)
	}
	want := []struct {
		title       string
		url         string
		date        string
		publishedAt string
	}{
		{"关于人工智能研究院的测试通知", aiBaseURL + "/info/1067/1301.htm", "2020-08-07", "2020-08-07 10:24:00"},
		{"第二条通知公告测试标题", aiBaseURL + "/info/1067/1094.htm", "2019-06-14", ""},
	}
	for i, expected := range want {
		got := items[i]
		if got.Title != expected.title || got.URL != expected.url || got.Date != expected.date || got.PublishedAt != expected.publishedAt || got.Type != "通知公告" {
			t.Errorf("archive item %d = (%q, %q, %q, %q, %q), want (%q, %q, %q, %q, 通知公告)", i, got.Title, got.URL, got.Date, got.PublishedAt, got.Type, expected.title, expected.url, expected.date, expected.publishedAt)
		}
	}
}

func TestAIHighlightsArchiveIsOnePageAndKeepsAllCards(t *testing.T) {
	items, err := parseAdapter_aiList(loadAIFixture(t, "ai_jcsj.html"), aiBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 7 {
		t.Fatalf("got %d 精彩瞬间 items, want all seven cards: %#v", len(items), items)
	}
	if items[0].URL != aiBaseURL+"/info/1111/1212.htm" || items[0].Date != "2019-12-02" || items[0].Type != "精彩瞬间" {
		t.Fatalf("unexpected first 精彩瞬间 item: %#v", items[0])
	}
	if items[len(items)-1].URL != aiBaseURL+"/info/1111/1133.htm" || items[len(items)-1].Date != "2018-11-28" {
		t.Fatalf("unexpected last 精彩瞬间 item: %#v", items[len(items)-1])
	}
}

func TestAIArticleParserExtractsPreciseMetadataResourcesAndAttachments(t *testing.T) {
	article, err := parseAdapter_aiArticle(loadAIFixture(t, "ai_article.html"), aiBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于人工智能研究院的测试通知" {
		t.Fatalf("article title = %q", article.Title)
	}
	if article.Source != "人工智能研究院" || article.Date != "2020-08-07" || article.PublishedAt != "2020-08-07 10:24:56" {
		t.Fatalf("unexpected article metadata: source=%q date=%q published_at=%q", article.Source, article.Date, article.PublishedAt)
	}
	if article.Type != "通知公告" {
		t.Fatalf("article type = %q, want 通知公告", article.Type)
	}
	if !strings.Contains(article.Content, "这是正文内容。") || !strings.Contains(article.Content, "正文第二段。") {
		t.Fatalf("article body is incomplete: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != aiBaseURL+"/__local/body.png" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}

	wantAttachments := map[string]string{
		"附件材料.docx": aiBaseURL + "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1541857851&wbfileid=DOCFIX",
	}
	if len(article.Attachments) != len(wantAttachments) {
		t.Fatalf("got %d attachments, want %d: %#v", len(article.Attachments), len(wantAttachments), article.Attachments)
	}
	for _, attachment := range article.Attachments {
		if want, ok := wantAttachments[attachment.Name]; !ok || want != attachment.URL {
			t.Errorf("unexpected attachment: %#v", attachment)
		}
	}
	if !strings.Contains(article.ContentHTML, `src="https://ai.swjtu.edu.cn/__local/body.png"`) || !strings.Contains(article.ContentHTML, "wbfileid=DOCFIX") || !strings.Contains(article.ContentHTML, "https://example.org/paper.pdf") {
		t.Fatalf("content HTML did not retain resolved resources: %s", article.ContentHTML)
	}
	for _, attachment := range article.Attachments {
		if strings.Contains(attachment.URL, "example.org") {
			t.Fatal("external PDF link was promoted to an AI-site attachment")
		}
	}
}

func TestAILiveDetailFixturesUseFixedMetadataAndKeepBodyLinks(t *testing.T) {
	cases := []struct {
		fixture, title, source, date, publishedAt, typ string
	}{
		{"ai_live_article_notice.html", "关于张钧波博士参与申报 2020年度吴文俊人工智能科学技术奖的公示", "人工智能研究院", "2020-08-07", "2020-08-07 10:24:00", "通知公告"},
		{"ai_live_article_research.html", "研究院与京东城市合作提出快速定位新冠病毒易感人群新方案", "服务管理办公室", "2020-04-16", "2020-04-16 13:00:00", "科学研究"},
		{"ai_live_article_report.html", "西南交通大学 “1896交子讲堂” 学术讲座", "服务管理办公室", "2019-12-25", "2019-12-25 12:03:00", "学术报告"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			article, err := parseAdapter_aiArticle(loadAIFixture(t, tc.fixture), aiBaseURL)
			if err != nil {
				t.Fatal(err)
			}
			if article.Title != tc.title || article.Source != tc.source || article.Date != tc.date || article.PublishedAt != tc.publishedAt || article.Type != tc.typ {
				t.Fatalf("fixed metadata = %#v, want title=%q source=%q date=%q published=%q type=%q", article, tc.title, tc.source, tc.date, tc.publishedAt, tc.typ)
			}
		})
	}

	research, err := parseAdapter_aiArticle(loadAIFixture(t, "ai_live_article_research.html"), aiBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(research.Images) != 2 || !strings.Contains(research.Content, "论文下载链接") || !strings.Contains(research.ContentHTML, "https://arxiv.org/pdf/2004.06653.pdf") {
		t.Fatalf("research body/resources were not retained accurately: content=%q images=%#v html=%s", research.Content, research.Images, research.ContentHTML)
	}
	if len(research.Attachments) != 0 {
		t.Fatalf("external arXiv link was incorrectly emitted as an attachment: %#v", research.Attachments)
	}
}

func TestAIFetchArticleVerifiesResourcesAndDateOnly(t *testing.T) {
	fixture := loadAIFixture(t, "ai_resource_article.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1067/1301.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(fixture)
		case "/images/valid.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})
		case "/images/bad.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<!doctype html><h1>验证码下载附件</h1>"))
		case "/files/valid.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7 fixture"))
		case "/files/bad.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("not a PDF"))
		case "/system/_content/download.jsp":
			if r.URL.Query().Get("wbfileid") == "VALID" {
				w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
				_, _ = w.Write([]byte{'P', 'K', 0x03, 0x04, 'A', 'I'})
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html><body>请输入验证码下载附件</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["ai"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	site.BaseURL, NoCache = server.URL, true
	t.Cleanup(func() {
		site.BaseURL, NoCache = oldBase, oldNoCache
		ClearCache()
	})

	article, raw, err := aiFetchArticle(server.URL + "/info/1067/1301.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || article.Date != "2026-09-04" || article.PublishedAt != "" {
		t.Fatalf("date-only publication was changed: raw=%d date=%q published=%q", len(raw), article.Date, article.PublishedAt)
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/images/valid.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 || article.Attachments[0].Name != "有效文件.pdf" || article.Attachments[1].Name != "有效材料.docx" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	for _, rejected := range []string{"bad.png", "bad.pdf", "CAPTCHA", "loading.gif", "javascript:"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource remained in ContentHTML: %q in %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, "https://external.example/guide") || !strings.Contains(article.ContentHTML, server.URL+"/files/valid.pdf") {
		t.Fatalf("valid/external links were not retained: %s", article.ContentHTML)
	}
}

func TestAIResourceVerificationChecksHTTPStatusTypeAndMagic(t *testing.T) {
	if aiResourceBodyIsUsable("text/html; charset=UTF-8", loadAIFixture(t, "ai_attachment_captcha_live.html"), "download.jsp", false) {
		t.Fatal("captured AI download CAPTCHA HTML was accepted as an attachment")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		case "/fake.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>captcha</html>"))
		case "/valid.pdf":
			w.Header().Set("Content-Type", "application/pdf; charset=binary")
			_, _ = w.Write([]byte("%PDF-1.7\n"))
		case "/fake.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<html>登录页面</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if body, typ, err := aiFetchResource(server.URL+"/valid.png", server.URL, true); err != nil || typ != "image/png" || len(body) == 0 {
		t.Fatalf("valid image fetch = len=%d type=%q err=%v", len(body), typ, err)
	}
	for _, resource := range []string{"/fake.png", "/fake.pdf", "/missing.pdf"} {
		if _, _, err := aiFetchResource(server.URL+resource, server.URL, strings.HasSuffix(resource, ".png")); err == nil {
			t.Fatalf("invalid resource %s was accepted", resource)
		}
	}
	if !aiResourceBodyIsUsable("application/pdf", []byte("%PDF-1.7"), "valid.pdf", false) || aiResourceBodyIsUsable("application/pdf", []byte("not pdf"), "fake.pdf", false) {
		t.Fatal("PDF Content-Type/magic validation is incorrect")
	}
}

func TestAIParsersRejectShellsAndErrorPages(t *testing.T) {
	shell := []byte(`<html><head><title>人工智能研究院</title></head><body><nav><a href="info/1067/1301.htm">导航伪文章</a></nav><div class="list"><a href="info/1067/1301.htm">首页壳文字</a></div></body></html>`)
	if _, err := parseAdapter_aiList(shell, aiBaseURL); err == nil {
		t.Fatal("homepage shell was accepted as an AI news list")
	}
	if _, err := parseAdapter_aiArticle(shell, aiBaseURL); err == nil {
		t.Fatal("homepage shell was accepted as an AI article")
	}

	captcha := []byte(`<html><head><title>附件下载</title></head><body><h1>请输入验证码下载附件</h1></body></html>`)
	if _, err := parseAdapter_aiArticle(captcha, aiBaseURL); err == nil {
		t.Fatal("verification page was accepted as an AI article")
	}
	waf := []byte(`<!doctype html><html><head><title>安全验证</title></head><body><h1>请求被拦截</h1></body></html>`)
	if _, err := parseAdapter_aiList(waf, aiBaseURL); err == nil {
		t.Fatal("WAF response was accepted as an AI list")
	}
}
