package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

const emFixtureBaseURL = "https://em.swjtu.edu.cn"

func TestEmAdapterRegisteredWithAllArticleFeeds(t *testing.T) {
	site := AllSites["em"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("em adapter hooks are not registered")
	}
	if site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("em adapter pagination hooks are not registered")
	}

	want := []string{
		"xwdt",
		"dqgz",
		"dqgz/djgz",
		"dqgz/jwgz",
		"dqgz/ghgz",
		"dqgz/ltgz",
		"rcpy",
		"rcpy/xwdt",
		"rcpy/zszl",
		"rcpy/tzgg",
		"rcpy/glgd",
		"rcpy/wdxz",
		"rcpy/zyjj",
		"jypx3/pxfw",
		"jypx3/pxdt",
		"jypx3/tzgg",
		"tzgg",
		"hqbz",
		"qt",
		"gzdt",
		"xqfg",
		"xyyd",
		"xyyd/xyhd",
		"xyyd/xyjy",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("em feeds = %#v, want %#v", site.SubPages, want)
	}
}

func TestEmPageURLUsesReverseGeneratedNumbers(t *testing.T) {
	tests := []struct {
		name       string
		slug       string
		page       int
		totalPages int
		extension  string
		want       string
	}{
		{"first", "xwdt", 1, 71, ".htm", emFixtureBaseURL + "/xwdt.htm"},
		{"second", "xwdt", 2, 71, ".htm", emFixtureBaseURL + "/xwdt/70.htm"},
		{"middle", "xwdt", 36, 71, ".htm", emFixtureBaseURL + "/xwdt/36.htm"},
		{"last", "xwdt", 71, 71, ".htm", emFixtureBaseURL + "/xwdt/1.htm"},
		{"nested", "rcpy/tzgg", 7, 7, ".html", emFixtureBaseURL + "/rcpy/tzgg/1.html"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := emPageURL(emFixtureBaseURL, tt.slug, tt.page, tt.totalPages, tt.extension); got != tt.want {
				t.Fatalf("emPageURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmPageCountReadsDisplayedLogicalNumbers(t *testing.T) {
	html := []byte(`<div class="p_t">共421条</div><div class="p_pages">
<span class="p_no_d">1</span><span class="p_no"><a href="xwdt/70.htm">2</a></span>
<span class="p_no"><a href="xwdt/1.htm">71</a></span><span class="p_last"><a href="xwdt/1.htm">尾页</a></span>
</div>`)
	if got := emPageCountFromHTML(html); got != 71 {
		t.Fatalf("emPageCountFromHTML() = %d, want 71", got)
	}

	empty := []byte(`<div class="p_t">共0条</div><div class="p_pages"><span class="p_no_d">1</span></div>`)
	if got := emPageCountFromHTML(empty); got != 1 {
		t.Fatalf("empty em page count = %d, want 1", got)
	}
}

func TestEmHomeParsesEveryModuleAndExcludesExternalLinks(t *testing.T) {
	html, err := os.ReadFile("testdata/em.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_emList(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	seen := make(map[string]bool)
	for _, item := range items {
		counts[item.Type]++
		if seen[item.URL] {
			t.Errorf("duplicate home article URL: %q", item.URL)
		}
		seen[item.URL] = true
		if strings.Contains(item.URL, "mp.weixin.qq.com") || strings.Contains(item.URL, "jwc.swjtu.edu.cn") {
			t.Errorf("external home link leaked into feed: %#v", item)
		}
		if item.Date == "" && item.Type != "校区风光" {
			t.Errorf("home item has no date: %#v", item)
		}
	}

	wantCounts := map[string]int{
		"校区要闻": 4,
		"校区公告": 10,
		"党政办公": 10,
		"人才培养": 9,
		"教育培训": 10,
		"后勤保障": 10,
		"工作动态": 4,
		"校区风光": 5,
	}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("home type counts = %#v, want %#v (items=%d)", counts, wantCounts, len(items))
	}
	if len(items) != 62 {
		t.Fatalf("home items = %d, want 62", len(items))
	}
}

func TestEmListParsesDatesTypesAndLegacyContentURLs(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="管理规定"></head><body>
<section class="TextList bgss"><div><ul>
<li><a href="../zzz_content.jsp?urltype=news.NewsContentUrl&amp;wbtreeid=1850&amp;wbnewsid=18316" title="西南交通大学本科生考试违规处理办法"><div class="data"><i>03-09</i><b>2026</b></div><p class="line1">西南交通大学本科生考试违规处理办法</p></a></li>
<li><a href="info/1025/20756.htm" title="社会用工拟聘人员公示"><div class="data"><i>08-31</i><b>2026</b></div><p class="line1">社会用工拟聘人员公示</p></a></li>
<li><a href="https://example.com/info/1/2.htm" title="不属于峨眉校区的文章"><div class="data"><i>08-30</i><b>2026</b></div><p class="line1">不属于峨眉校区的文章</p></a></li>
</ul><span class="p_t">共2条</span><span class="p_pages"><span class="p_no_d">1</span></span></div></section>
</body></html>`)

	items, err := parseAdapter_emList(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d em list items, want 2: %#v", len(items), items)
	}
	if items[0].URL != emFixtureBaseURL+"/zzz_content.jsp?urltype=news.NewsContentUrl&wbtreeid=1850&wbnewsid=18316" ||
		items[0].Date != "2026-03-09" || items[0].Type != "管理规定" {
		t.Errorf("unexpected legacy list item: %#v", items[0])
	}
	if items[1].URL != emFixtureBaseURL+"/info/1025/20756.htm" || items[1].Date != "2026-08-31" || items[1].Type != "管理规定" {
		t.Errorf("unexpected ordinary list item: %#v", items[1])
	}
}

func TestEmEmptyListIsLegalButStaticPageIsNotAFeed(t *testing.T) {
	empty := []byte(`<html><head><meta name="pageTitle" content="其他"></head><body>
<section class="TextList bgss"><div><ul></ul><span class="p_t">共0条</span><span class="p_pages"><span class="p_no_d">1</span></span></div></section>
</body></html>`)
	items, err := parseAdapter_emList(empty, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("empty em list returned %d items", len(items))
	}

	static := []byte(`<html><body><section id="nav_slide"><div class="n_title"><h1>教育培训</h1></div></section><div class="show01"><h5>教育培训</h5></div></body></html>`)
	if _, err := parseAdapter_emList(static, emFixtureBaseURL); err == nil {
		t.Fatal("static em page was accepted as a list feed")
	}
}

func TestEmArticleParsesHeadlineMetadataBodyImagesAndAttachments(t *testing.T) {
	html, err := os.ReadFile("testdata/em_article.html")
	if err != nil {
		t.Fatal(err)
	}

	legacy, err := parseBodaArticle(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Title != "新闻动态" {
		t.Fatalf("fixture no longer reproduces the generic title: %q", legacy.Title)
	}

	article, err := parseAdapter_emArticle(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	wantTitle := "校区纪委组织开展廉洁主题系列教育活动 ——陈诗伟讲党风廉政教育党课"
	if article.Title != wantTitle || article.Date != "2026-07-17" || article.PublishedAt != "2026-07-17 09:27:00" {
		t.Fatalf("unexpected article publication fields: title=%q date=%q published_at=%q", article.Title, article.Date, article.PublishedAt)
	}
	if article.Type != "新闻动态" {
		t.Fatalf("article type = %q, want 新闻动态", article.Type)
	}
	if article.Content != "为深入推进廉洁文化建设，校区纪委组织开展廉洁主题系列教育活动。" {
		t.Fatalf("article content = %q", article.Content)
	}
	wantImage := emFixtureBaseURL + "/__local/body.jpg"
	if !reflect.DeepEqual(article.Images, []string{wantImage}) {
		t.Fatalf("article images = %#v, want %#v", article.Images, []string{wantImage})
	}
	wantAttachments := []model.Attachment{
		{Name: "附件1：廉洁教育材料.pdf", URL: emFixtureBaseURL + "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1455160693&wbfileid=ABC123"},
		{Name: "附件2：活动照片.zip", URL: emFixtureBaseURL + "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1455160693&wbfileid=DEF456"},
	}
	if !reflect.DeepEqual(article.Attachments, wantAttachments) {
		t.Fatalf("article attachments = %#v, want %#v", article.Attachments, wantAttachments)
	}
	if article.PrevURL != "" || article.NextURL != "" {
		t.Fatalf("bare sibling links must not be resolved against the site root: prev=%q next=%q", article.PrevURL, article.NextURL)
	}
}

func TestEmArticleRejectsStaticAndVerificationPages(t *testing.T) {
	static := []byte(`<html><head><meta name="pageType" content="2"></head><body><div class="show01"><h5>教育培训</h5></div><div class="v_news_content"><p>静态内容</p></div></body></html>`)
	if _, err := parseAdapter_emArticle(static, emFixtureBaseURL); err == nil {
		t.Fatal("static em page was accepted as an article")
	}
	verification := []byte(`<html><head><title>验证码验证</title></head><body><div id="captcha">请完成验证码</div></body></html>`)
	if _, err := parseAdapter_emArticle(verification, emFixtureBaseURL); err == nil {
		t.Fatal("verification page was accepted as an article")
	}
}
