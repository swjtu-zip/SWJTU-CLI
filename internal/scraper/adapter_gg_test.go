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
	if site == nil || site.BaseURL != ggBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("gg adapter hooks are incomplete: %#v", site)
	}
	if !reflect.DeepEqual(site.SubPages, ggSubPages) {
		t.Fatalf("gg routes = %#v, want %#v", site.SubPages, ggSubPages)
	}
	if len(site.SubPages) != 68 {
		t.Fatalf("gg routes = %d, want all 68 discovered list entry points", len(site.SubPages))
	}
	for _, required := range []string{
		"xwgg/xyxw", "xwgg/xxgg", "rcpy/bkjw/bkjxtz", "rcpy/yjsjw/yjsjxtz",
		"xsgz/wjxz/xsgl", "MPAzx/jxgl/ktdb", "MPAzx/wjxz/bgzl",
		"zsgz/yjszs/tzgg", "xzzx/qrzyjsxg", "xyzj", "xyzj/yydt", "xyzj/yyfc",
	} {
		if !ggContainsString(site.SubPages, required) {
			t.Errorf("missing gg list entry point %q", required)
		}
	}
	for _, excluded := range []string{
		"xygk/xyjj", "xygk/zzjg", "llzx1/yjdw", "szdw", "xyzj/gywm", "xyzj/yqlj", "risk", "job",
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
	noDate, err := parseAdapter_ggList([]byte(`<html><head><meta name="pageType" content="2"></head><body><div class="list-col"><h4>信息公告</h4><div class="list-list"><ul><li><a href="/info/1010/1.htm" title="活动将于2026年9月6日举行">活动将于2026年9月6日举行</a></li></ul></div></div></body></html>`), ggFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(noDate) != 1 || noDate[0].Date != "" {
		t.Fatalf("row title date leaked into publication date: %#v", noDate)
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
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "测试附件.xlsx" || article.Attachments[0].URL != ggFixtureBaseURL+"/__local/fixture.xlsx?e=.xlsx" {
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

func TestGGLiveHomepageAndListFixtures(t *testing.T) {
	items, err := parseAdapter_ggList(readGGFixture(t, "gg_live_home.html"), ggFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("live homepage items = %d, want five deduplicated module entries: %#v", len(items), items)
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, ggFixtureBaseURL+"/info/") || item.Type == "" {
			t.Fatalf("live homepage leaked a non-article or missing type: %#v", item)
		}
	}

	listHTML := readGGFixture(t, "gg_live_list_xyxw_first.html")
	items, err = parseAdapter_ggList(listHTML, ggFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Title != "凝心聚力启新程 笃行实干向未来——公共管理学院召开新学期全院大会" || items[0].Date != "2026-09-03" || items[0].Type != "学院新闻" {
		t.Fatalf("live list result = %#v", items)
	}
	if total := parseTotalPages(listHTML); total != 26 {
		t.Fatalf("live list total pages = %d, want 26", total)
	}
	if got := ggPageURL(ggFixtureBaseURL, "xwgg/xyxw", 13, 26, ".htm"); got != ggFixtureBaseURL+"/xwgg/xyxw/14.htm" {
		t.Fatalf("live middle page URL = %q", got)
	}
}

func TestGGLiveEmptyAndStaticFixtures(t *testing.T) {
	items, err := parseAdapter_ggList(readGGFixture(t, "gg_live_empty_xkjs.html"), ggFixtureBaseURL)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("real empty list = %#v, err=%v", items, err)
	}
	if _, err := parseAdapter_ggList(readGGFixture(t, "gg_live_static_szdw.html"), ggFixtureBaseURL); err == nil {
		t.Fatal("static faculty directory was accepted as an empty feed")
	}
	if _, err := parseAdapter_ggList(readGGFixture(t, "gg_live_waf.html"), ggFixtureBaseURL); err == nil {
		t.Fatal("live WAF page was accepted as a list")
	}
}

func TestGGLiveArticleFixturesUseFixedMetadataAndCompleteBody(t *testing.T) {
	recent, err := parseAdapter_ggArticle(readGGFixture(t, "gg_live_article_recent.html"), ggFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if recent.Title != "第十五届党委第六轮巡察第二巡察组结束工作公告" || recent.Type != "信息公告" || recent.Date != "2026-07-07" || recent.PublishedAt != "" {
		t.Fatalf("live recent article metadata = %#v", recent)
	}
	if !strings.Contains(recent.Content, "2026年7月6日") || !strings.Contains(recent.ContentHTML, "特此公告") {
		t.Fatalf("live recent article body is incomplete: content=%q html=%q", recent.Content, recent.ContentHTML)
	}

	old, err := parseAdapter_ggArticle(readGGFixture(t, "gg_live_article_old.html"), ggFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if old.Title != "公共管理学院关于2014届申请创新实践的通知及相关表格" || old.Type != "本科相关" || old.Date != "2014-06-06" {
		t.Fatalf("live old article metadata = %#v", old)
	}
	if len(old.Attachments) != 1 || old.Attachments[0].URL != ggFixtureBaseURL+"/__local/7/8B/1A/2EA3B20C0B540A614342F2417A2_764BC3A8_15C74.rar?e=.rar" {
		t.Fatalf("live old article attachments = %#v", old.Attachments)
	}
}

func TestGGResourceValidationRejectsCaptchaHTMLAndBadMagic(t *testing.T) {
	captcha := readGGFixture(t, "gg_live_captcha_download.html")
	if ggResourceBodyIsUsable("text/html;charset=UTF-8", captcha, "附件.rar", false) {
		t.Fatal("captcha HTML passed attachment validation")
	}
	if !ggResourceBodyIsUsable("image/jpeg", []byte{0xff, 0xd8, 0xff, 0xe0}, "photo.jpg", true) {
		t.Fatal("JPEG magic was rejected")
	}
	if ggResourceBodyIsUsable("image/jpeg", []byte("not an image"), "photo.jpg", true) {
		t.Fatal("bad JPEG magic was accepted")
	}
	if !ggResourceBodyIsUsable("application/x-rar-compressed", []byte("Rar!\x1a\x07\x00\x00"), "附件.rar", false) {
		t.Fatal("RAR magic was rejected")
	}
	if ggAttachmentURL(ggFixtureBaseURL, "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&wbfileid=CAPTCHA") != "" {
		t.Fatal("bare CMS download URL was retained as an attachment")
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
