package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const ggFixtureBaseURL = "https://gg.swjtu.edu.cn"

func readGGFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read gg fixture %s: %v", name, err)
	}
	return data
}

func TestGGAdapterRegistrationAndRealEntryPoints(t *testing.T) {
	site := AllSites["gg"]
	if site == nil || site.BaseURL != ggBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("gg adapter hooks are incomplete: %#v", site)
	}
	if !reflect.DeepEqual(site.SubPages, ggSubPages) {
		t.Fatalf("gg routes = %#v, want %#v", site.SubPages, ggSubPages)
	}
	if len(site.SubPages) != 71 {
		t.Fatalf("gg routes = %d, want all 71 discovered list entry points", len(site.SubPages))
	}
	for _, required := range []string{
		"xwgg/xyxw", "xwgg/xxgg", "rcpy/bkjw/bkjxtz", "rcpy/yjsjw/yjsjxtz",
		"xsgz/wjxz/xsgl", "MPAzx/jxgl/ktdb", "MPAzx/wjxz/bgzl",
		"zsgz/yjszs/tzgg", "xzzx/qrzyjsxg", "xyzj", "xyzj/yydt", "xyzj/yyfc", "xyzj/gywm", "xyzj/yqlj",
	} {
		if !ggContainsString(site.SubPages, required) {
			t.Errorf("missing gg list entry point %q", required)
		}
	}
	for _, excluded := range []string{
		"xygk/xyjj", "xygk/zzjg", "llzx1/yjdw", "risk", "job",
	} {
		if ggContainsString(site.SubPages, excluded) {
			t.Errorf("static/profile route registered as gg feed: %q", excluded)
		}
	}
}

func TestGGPageURLUsesReverseStaticNumbering(t *testing.T) {
	cases := []struct {
		slug        string
		page, total int
		want        string
	}{
		{"xwgg/xyxw", 1, 26, ggFixtureBaseURL + "/xwgg/xyxw.htm"},
		{"xwgg/xyxw", 2, 26, ggFixtureBaseURL + "/xwgg/xyxw/25.htm"},
		{"xwgg/xyxw", 13, 26, ggFixtureBaseURL + "/xwgg/xyxw/14.htm"},
		{"xwgg/xyxw", 26, 26, ggFixtureBaseURL + "/xwgg/xyxw/1.htm"},
		{"xzzx", 2, 2, ggFixtureBaseURL + "/xzzx/1.html"},
		{"xwgg/xyxw", 2, 0, ""},
	}
	for _, tc := range cases {
		extension := ".htm"
		if strings.HasSuffix(tc.want, ".html") {
			extension = "html"
		}
		got := ggPageURL(ggFixtureBaseURL+"/", tc.slug, tc.page, tc.total, extension)
		if got != tc.want {
			t.Errorf("ggPageURL(%q, %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestGGHomepageScopesAllArticleModules(t *testing.T) {
	items, err := parseAdapter_ggList(readGGFixture(t, "gg_home.html"), ggFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("homepage items = %d, want five unique articles across four modules: %#v", len(items), items)
	}
	wantTypes := map[string]bool{
		"学院新闻": true,
		"通知公告": true,
		"学术科研": true,
		"教学信息": true,
	}
	gotTypes := make(map[string]int)
	for _, item := range items {
		if !wantTypes[item.Type] {
			t.Errorf("unexpected homepage item type: %#v", item)
		}
		gotTypes[item.Type]++
		if !strings.HasPrefix(item.URL, ggFixtureBaseURL+"/info/") {
			t.Errorf("non-article homepage URL leaked: %#v", item)
		}
	}
	if gotTypes["学院新闻"] != 2 || gotTypes["通知公告"] != 1 || gotTypes["学术科研"] != 1 || gotTypes["教学信息"] != 1 {
		t.Fatalf("homepage module counts = %#v", gotTypes)
	}
	for _, rejected := range []string{"导航伪文章", "外部文章", "招生工作", "下载中心", "院友之家"} {
		for _, item := range items {
			if item.Title == rejected {
				t.Errorf("non-list homepage item leaked: %#v", item)
			}
		}
	}
}

func TestGGListParserExtractsTypeDatesAndRejectsNonArticles(t *testing.T) {
	items, err := parseAdapter_ggList(readGGFixture(t, "gg_list.html"), ggFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("list items = %d, want two internal rows: %#v", len(items), items)
	}
	if items[0].Title != "真实列表通知" || items[0].URL != ggFixtureBaseURL+"/info/1010/30575.htm" || items[0].Date != "2026-05-25" || items[0].Type != "信息公告" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[1].Date != "2026-05-21" || items[1].PublishedAt != "" {
		t.Fatalf("date-only list item = %#v", items[1])
	}
	if _, err := parseAdapter_ggList([]byte(`<html><body><div class="article">静态页</div></body></html>`), ggFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as a gg list")
	}
}

func TestGGAdapterDistinguishesReachableEmptyListFromShell(t *testing.T) {
	items, err := parseAdapter_ggList(readGGFixture(t, "gg_empty.html"), ggFixtureBaseURL)
	if err != nil {
		t.Fatalf("empty list should parse: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty list result = %#v, want a non-nil empty slice", items)
	}
	if _, err := parseAdapter_ggList([]byte(`<html><head><meta name="pageType" content="2"></head><body><div id="app"></div></body></html>`), ggFixtureBaseURL); err == nil {
		t.Fatal("unclassified shell was accepted as a gg list")
	}
}

func TestGGArticleFieldsImagesAndOutsideAttachments(t *testing.T) {
	article, err := parseAdapter_ggArticle(readGGFixture(t, "gg_article.html"), ggFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "公共管理学院详情测试通知" || article.Type != "信息公告" {
		t.Fatalf("article title/type = %#v", article)
	}
	if article.Date != "2026-09-02" || article.PublishedAt != "2026-09-02 14:34:00" {
		t.Fatalf("article date/time = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "正文段落") || strings.Contains(article.Content, "侧栏推荐") {
		t.Fatalf("article body scope = %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != ggFixtureBaseURL+"/__local/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "测试附件.xlsx" || article.Attachments[0].URL != ggFixtureBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=fixture&wbfileid=one" {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
}

func TestGGArticleRejectsStaticErrorAndCaptchaPages(t *testing.T) {
	for name, body := range map[string][]byte{
		"static":  []byte(`<html><head><meta name="pageType" content="2"></head><body><div class="article"><h1>静态页</h1></div></body></html>`),
		"error":   []byte(`<html><head><title>404错误</title></head><body>404</body></html>`),
		"captcha": []byte(`<html><head><title>附件下载</title></head><body>请输入验证码下载附件</body></html>`),
	} {
		if _, err := parseAdapter_ggArticle(body, ggFixtureBaseURL); err == nil {
			t.Errorf("%s page was accepted as a gg article", name)
		}
	}
}

func TestGGDateValuesDoNotInventClock(t *testing.T) {
	if date, publishedAt := ggDateValues("日期：2026-08-31"); date != "2026-08-31" || publishedAt != "" {
		t.Fatalf("date-only value = %q/%q", date, publishedAt)
	}
	if date, publishedAt := ggDateValues("日期：2026-08-31 14:34"); date != "2026-08-31" || publishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("precise value = %q/%q", date, publishedAt)
	}
}

func TestGGArticleDoesNotUseBodyDeadlineAsPublicationDate(t *testing.T) {
	const html = `<html><head><meta name="pageType" content="3"></head><body>
<div class="article"><h1>推免加分申请补充通知</h1>
<div class="source">日期：2026-09-03</div>
<div class="v_news_content"><p>请于2026年9月6日12:00前提交申请。</p></div>
</div></body></html>`
	article, err := parseAdapter_ggArticle([]byte(html), ggFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-09-03" || article.PublishedAt != "" {
		t.Fatalf("body deadline replaced publication date: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
}

func ggContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
