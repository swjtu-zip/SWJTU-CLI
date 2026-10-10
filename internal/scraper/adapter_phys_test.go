package scraper

import (
	"os"
	"strings"
	"testing"
)

const physFixtureBaseURL = "https://phys.swjtu.edu.cn"

func readPhysFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read phys fixture %q: %v", name, err)
	}
	return body
}

func TestPhysAdapterRegistersAuditedArticleFeeds(t *testing.T) {
	site := AllSites["phys"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("phys adapter is not registered")
	}
	want := map[string]bool{
		"xwgg/xydt": true, "kxyj/xsdt": true,
		"rcpy/bkspy": true, "rcpy/bkspy/bkstz": true, "rcpy/bkspy/bkspyfa": true, "rcpy/bkspy/bksxz": true,
		"rcpy/yjspy": true, "rcpy/yjspy/yjstz": true, "rcpy/yjspy/yjspyfa": true,
		"xsfz/gqtgz": true, "xsfz/xgdt": true, "xsfz/xgtz": true,
		"xygz/xyjy": true, "dqgz/jjgz": true, "dqgz/pyfz": true, "dqgz/xxjy": true,
		"dqgz/ghzjlink/ghhd": true,
	}
	got := make(map[string]bool, len(site.SubPages))
	for _, slug := range site.SubPages {
		got[slug] = true
	}
	if len(got) != len(want) {
		t.Fatalf("phys feed count = %d, want %d (%v)", len(got), len(want), site.SubPages)
	}
	for slug := range want {
		if !got[slug] {
			t.Errorf("phys audited feed %q is missing", slug)
		}
	}
}

func TestPhysPageURLUsesReverseStaticNumberingAndRejectsOutOfRange(t *testing.T) {
	cases := []struct {
		slug       string
		page       int
		totalPages int
		want       string
	}{
		{"xwgg/xydt", 1, 174, physFixtureBaseURL + "/xwgg/xydt.htm"},
		{"xwgg/xydt", 2, 174, physFixtureBaseURL + "/xwgg/xydt/173.htm"},
		{"xwgg/xydt", 87, 174, physFixtureBaseURL + "/xwgg/xydt/88.htm"},
		{"xwgg/xydt", 174, 174, physFixtureBaseURL + "/xwgg/xydt/1.htm"},
		{"rcpy/bkspy/bkstz", 2, 52, physFixtureBaseURL + "/rcpy/bkspy/bkstz/51.htm"},
		{"rcpy/bkspy/bkspyfa", 2, 1, ""},
		{"xwgg/xydt", 175, 174, ""},
		{"xwgg/xydt", 2, 0, ""},
		{"xwgg/xydt", 0, 174, ""},
	}
	for _, tc := range cases {
		if got := reverseBodaPageURL(physFixtureBaseURL, tc.slug, tc.page, tc.totalPages, ".htm"); got != tc.want {
			t.Errorf("phys page URL %s page %d/%d = %q, want %q", tc.slug, tc.page, tc.totalPages, got, tc.want)
		}
	}
}

func TestPhysLiveListFixtureKeepsSourceTypeAndTotalPages(t *testing.T) {
	body := readPhysFixture(t, "phys_list_live.html")
	items, err := parseAdapter_physList(body, physFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("phys live list returned %d items, want 3: %#v", len(items), items)
	}
	if items[0].Title != "喜报！物理科学与技术学院本科生在全国大学生物理实验竞赛中获奖" || items[0].Date != "2026-07-01" || items[0].Type != "学院动态" {
		t.Fatalf("unexpected phys list metadata: %#v", items[0])
	}
	if got, err := physTotalPagesFromHTML(body, "xwgg/xydt"); err != nil || got != 174 {
		t.Fatalf("phys live list pages = %d/%v, want 174", got, err)
	}
}

func TestPhysRealEmptyListIsSuccessfulAndStaticShellIsNot(t *testing.T) {
	body := readPhysFixture(t, "phys_empty_list.html")
	items, err := parseAdapter_physList(body, physFixtureBaseURL)
	if err != nil || len(items) != 0 {
		t.Fatalf("phys empty list = %#v, %v; want empty success", items, err)
	}
	if got, err := physTotalPagesFromHTML(body, "rcpy/bkspy/bkspyfa"); err != nil || got != 1 {
		t.Fatalf("phys empty list pages = %d/%v, want 1", got, err)
	}
	if _, err := parseAdapter_physList(readPhysFixture(t, "phys_error.html"), physFixtureBaseURL); err == nil {
		t.Fatal("phys error page was accepted as a list")
	}
	if _, err := parseAdapter_physList([]byte(`<html><body><h2>科研概况</h2><div>静态介绍页</div></body></html>`), physFixtureBaseURL); err == nil {
		t.Fatal("phys static page was accepted as an article list")
	}
}

func TestPhysHomepageParsesAllRenderedNewsModules(t *testing.T) {
	html := []byte(`<html><head><meta name="pageType" content="1"><meta name="pageTitle" content="物理科学与技术学院"></head><body>
<div class="home1"><div class="title"><div class="con"><h3>学院动态</h3></div></div><ul><li><a href="info/1022/1.htm"><h3>学院动态首页文章</h3><p>2026.07.01</p></a></li></ul></div>
<div class="home3"><div class="left"><div class="title"><div class="con"><h3>本科生培养</h3></div></div><ul><li><a href="info/1031/2.htm"><h3>本科生培养文章</h3><span>2026.07.02</span></a></li></ul></div><div class="right"><div class="title"><div class="con"><h3>研究生培养</h3></div></div><ul><li><a href="info/1034/3.htm"><h3>研究生培养文章</h3><span>2026.07.03</span></a></li></ul></div></div>
<div class="home4"><div class="left"><div class="title"><div class="con"><h3>学工动态</h3></div></div><ul><li><a href="info/1040/4.htm"><h3>学工动态文章</h3><p>2026.07.04</p></a></li></ul></div></div></body></html>`)
	items, err := parseAdapter_physList(html, "http://phys.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("phys homepage items = %#v, want all 4 module entries", items)
	}
	if items[1].Type != "本科生培养" || items[1].Date != "2026-07-02" || items[2].Type != "研究生培养" || items[3].Type != "学工动态" {
		t.Fatalf("phys homepage metadata = %#v", items)
	}
}

func TestPhysTotalPagesFromHTMLRejectsNonList(t *testing.T) {
	if got, err := physTotalPagesFromHTML([]byte(`<html><div class="newlist1"><ul class="list"><li><a href="info/1/1.htm">x</a></li></ul><div class="p_pages"><a href="x/4.htm">4</a></div></div></html>`), "xwgg/xydt"); err != nil || got != 4 {
		t.Fatalf("phys page count = %d, %v; want 4", got, err)
	}
	if _, err := physTotalPagesFromHTML([]byte(`<html><div class="home1">首页</div></html>`), "xwgg/xydt"); err == nil {
		t.Fatal("phys homepage was accepted as an archive list")
	}
}

// The live homepage renders five news modules (.home1/.home3/.home4) of real
// local articles, so "index" is an audited single-page feed: the adapter must
// scope extraction to those modules and report exactly one page for it.
func TestPhysLiveHomepageFeedIsAuditedAndSinglePage(t *testing.T) {
	body := readPhysFixture(t, "phys_live_home.html")
	items, err := parseAdapter_physList(body, physFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 34 {
		t.Fatalf("phys live homepage returned %d items, want 34: %#v", len(items), items)
	}
	if items[0].Title != "喜报！我院应用物理系教师党支部荣获“四川省高校先进基层党组织”称号" || items[0].URL != physFixtureBaseURL+"/info/1022/58241.htm" || items[0].Date != "2026-07-01" || items[0].Type != "学院动态" {
		t.Fatalf("unexpected first homepage item: %#v", items[0])
	}
	if items[len(items)-1].URL != physFixtureBaseURL+"/info/1038/58341.htm" || items[len(items)-1].Type != "学术动态" {
		t.Fatalf("unexpected last homepage item: %#v", items[len(items)-1])
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, physFixtureBaseURL+"/info/") {
			t.Fatalf("non-local article URL leaked into homepage feed: %#v", item)
		}
		if item.Type == "" || item.Date == "" {
			t.Fatalf("homepage item lost module type or date: %#v", item)
		}
	}
	if total, err := physTotalPagesFromHTML(body, "index"); err != nil || total != 1 {
		t.Fatalf("phys homepage total = %d/%v, want 1", total, err)
	}
	if !physKnownFeed("index") {
		t.Fatal("physKnownFeed does not admit the audited index feed")
	}
	if physKnownFeed("not-a-feed") {
		t.Fatal("physKnownFeed admitted an unknown route")
	}
	// The index feed is only the audited homepage: archive shells and error
	// pages must stay rejected for that route.
	if _, err := physTotalPagesFromHTML(readPhysFixture(t, "phys_list_live.html"), "index"); err == nil {
		t.Fatal("phys archive list was accepted as the index homepage")
	}
	if _, err := physTotalPagesFromHTML(readPhysFixture(t, "phys_error.html"), "index"); err == nil {
		t.Fatal("phys error page was accepted as the index homepage")
	}
}

func TestPhysListKeepsHeadingTypeAndDatePrecision(t *testing.T) {
	html := []byte(`<html><body>
<div class="loaction"><a>首页</a><a>新闻公告</a><a>学院动态</a></div>
<ul class="list_zx"><li><a href="info/1024/9272.htm">学院动态文章标题</a><span class="time">2026-07-01 09:05</span></li></ul>
</body></html>`)
	items, err := parseAdapter_physList(html, "http://phys.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != "学院动态" || items[0].Date != "2026-07-01" || items[0].PublishedAt != "2026-07-01 09:05:00" {
		t.Fatalf("phys list item = %#v", items)
	}
}

func TestPhysLegacyMigrationDateIsNotPublishedDate(t *testing.T) {
	if date, publishedAt := physNormalizeDate("2017-04-24"); date != "" || publishedAt != "" {
		t.Fatalf("legacy migration date leaked from list: %q/%q", date, publishedAt)
	}

	html := []byte(`<html><body><h1>旧物理学院文章</h1><div class="news_source"><span>发布时间：2017-04-24</span></div><div class="v_news_content"><p>这是一段完整的旧文章正文，日期来自迁移模板。</p></div></body></html>`)
	article, err := parseAdapter_physArticle(html, "http://phys.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("legacy migration date leaked from article: %q/%q", article.Date, article.PublishedAt)
	}
	legacy, err := parseAdapter_physArticle(readPhysFixture(t, "phys_article_legacy.html"), physFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Date != "" || legacy.PublishedAt != "" {
		t.Fatalf("fixture migration date leaked from article: %q/%q", legacy.Date, legacy.PublishedAt)
	}
}

func TestPhysArticleKeepsBodyImageAndAttachment(t *testing.T) {
	html := []byte(`<html><body><div class="loaction"><a>首页</a><a>学院动态</a></div><h1>物理学院文章标题</h1><div class="news_source"><span class="date">2026-07-01</span></div><div class="v_news_content"><p>物理学院正文内容。</p><img data-src="/images/body.jpg"><a download="附件.pdf" href="/download?id=1">下载附件</a></div></body></html>`)
	article, err := parseAdapter_physArticle(html, "http://phys.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "学院动态" || !strings.Contains(article.Content, "正文内容") || len(article.Images) != 1 || len(article.Attachments) != 1 {
		t.Fatalf("phys article = %#v", article)
	}
}

func TestPhysLiveArticleUsesFixedPublishNodeAndFiltersUnverifiedResources(t *testing.T) {
	article, err := parseAdapter_physArticle(readPhysFixture(t, "phys_article_live.html"), physFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "创源大讲堂——太阳爆发的起源与早期演化的数值模拟研究（格物致理讲坛第117期）" {
		t.Fatalf("phys detail title = %q", article.Title)
	}
	if article.Type != "学术动态" || article.Date != "2026-07-02" || article.PublishedAt != "" {
		t.Fatalf("phys detail metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "时间：2026年7月10日") || !strings.Contains(article.Content, "太阳爆发是太阳系内最剧烈的天文现象") {
		t.Fatalf("phys detail body lost or used as metadata: %q", article.Content)
	}
	if len(article.Images) != 1 || !strings.HasSuffix(article.Images[0], "C757C.png") {
		t.Fatalf("phys detail images = %#v", article.Images)
	}
	if len(article.Attachments) != 0 {
		t.Fatalf("CAPTCHA HTML attachment was exposed as a file: %#v", article.Attachments)
	}
}

func TestPhysArticlePreservesClockOnlyFromFixedPublishNode(t *testing.T) {
	html := []byte(`<html><body><div class="inner_right"><div class="local"><h2>学院动态</h2></div><div class="article"><h2 class="tit">带精确时间的物理学院通知</h2><p class="conttime"><span>发布时间：2026-07-01 09:05:07</span></p><div class="v_news_content"><p>活动时间：2026年7月10日 14:00。</p></div></div></div></body></html>`)
	article, err := parseAdapter_physArticle(html, physFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-07-01" || article.PublishedAt != "2026-07-01 09:05:07" {
		t.Fatalf("phys precise detail date = %q/%q", article.Date, article.PublishedAt)
	}
}
