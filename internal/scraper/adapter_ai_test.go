package scraper

import (
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
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
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
		// The audited total wins over a stale caller hint; otherwise page 2
		// would incorrectly request /xwdt/0.htm.
		{"xwdt", 2, 1, aiBaseURL + "/xwdt/3.htm"},
		{"xwdt", 2, 10, aiBaseURL + "/xwdt/9.htm"},
		{"xsbg", 2, 2, aiBaseURL + "/xsbg/1.htm"},
		{"kxyj", 2, 2, aiBaseURL + "/kxyj/1.htm"},
		{"jcsj", 1, 1, aiBaseURL + "/jcsj.htm"},
	}
	for _, tc := range cases {
		if got := site.PageURL(aiBaseURL, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("page URL %s page %d/%d = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
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
		"论文.pdf":    "https://example.org/paper.pdf",
	}
	if len(article.Attachments) != len(wantAttachments) {
		t.Fatalf("got %d attachments, want %d: %#v", len(article.Attachments), len(wantAttachments), article.Attachments)
	}
	for _, attachment := range article.Attachments {
		if want, ok := wantAttachments[attachment.Name]; !ok || want != attachment.URL {
			t.Errorf("unexpected attachment: %#v", attachment)
		}
	}
	if !strings.Contains(article.ContentHTML, `src="https://ai.swjtu.edu.cn/__local/body.png"`) || !strings.Contains(article.ContentHTML, "wbfileid=DOCFIX") {
		t.Fatalf("content HTML did not retain resolved resources: %s", article.ContentHTML)
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
}
