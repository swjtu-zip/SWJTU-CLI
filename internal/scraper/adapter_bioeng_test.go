package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func loadBioengTestHTML(filename string) []byte {
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		return nil
	}
	return data
}

func TestBioengAdapterRegistered(t *testing.T) {
	site := AllSites["bioeng"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("bioeng adapter hooks are not registered")
	}
	want := []string{
		"xydt",
		"xydt/xytz",
		"xkdt/xsdt",
		"xkdt/gcyjzx",
		"xkdt/gcyjzx/zxdt",
		"djzc",
		"djzc/dwjj",
		"djzc/jwgz",
		"rcpy",
		"rcpy/bkspy",
		"rcpy/bkspy/zyjs",
		"rcpy/bkspy/tzgg",
		"rcpy/bkspy/gcrz",
		"rcpy/yjspy",
		"rcpy/zwhzbx",
		"xsgz",
		"wjxz",
		"sypt",
		"sypt/tzgg",
		"sypt/gzzd",
		"sypt/bszn",
		"sypt/gxpt",
		"sypt/aqzs",
		"jgzj",
		"xygz",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("bioeng sub-pages = %#v, want %#v", site.SubPages, want)
	}
}

func TestBioengTotalPagesReadsLogicalLabelsAndRecognizesEmptyLists(t *testing.T) {
	if got := bioengTotalPagesFromHTML(loadBioengTestHTML("bioeng_pagination.html")); got != 47 {
		t.Fatalf("bioengTotalPagesFromHTML() = %d, want logical total 47", got)
	}
	if got := bioengTotalPagesFromHTML(loadBioengTestHTML("bioeng_empty_list.html")); got != 1 {
		t.Fatalf("empty bioeng list total = %d, want 1", got)
	}
	if items, err := parseAdapter_bioengList(loadBioengTestHTML("bioeng_empty_list.html"), "https://bioeng.swjtu.edu.cn"); err != nil || len(items) != 0 {
		t.Fatalf("empty bioeng list = %#v, %v; want an accepted empty list", items, err)
	}
	if got := bioengTotalPagesFromHTML([]byte(`<html><title>请完成验证</title><body>请输入验证码</body></html>`)); got != 0 {
		t.Fatalf("verification page total = %d, want 0", got)
	}
	if got := bioengTotalPagesFromHTML([]byte(`<html><body><div class="content">学院简介</div></body></html>`)); got != 0 {
		t.Fatalf("static page total = %d, want 0", got)
	}
}

func TestBioengPageURLUsesReverseGeneratedPages(t *testing.T) {
	base := "https://bioeng.swjtu.edu.cn"
	tests := []struct {
		name       string
		slug       string
		page       int
		totalPages int
		want       string
	}{
		{name: "first", slug: "xydt", page: 1, totalPages: 47, want: base + "/xydt.htm"},
		{name: "second", slug: "xydt", page: 2, totalPages: 47, want: base + "/xydt/46.htm"},
		{name: "middle", slug: "xydt", page: 24, totalPages: 47, want: base + "/xydt/24.htm"},
		{name: "last", slug: "xydt", page: 47, totalPages: 47, want: base + "/xydt/1.htm"},
		{name: "nested second", slug: "rcpy/bkspy/tzgg", page: 2, totalPages: 24, want: base + "/rcpy/bkspy/tzgg/23.htm"},
		{name: "nested last", slug: "rcpy/bkspy/tzgg", page: 24, totalPages: 24, want: base + "/rcpy/bkspy/tzgg/1.htm"},
		{name: "single-page empty tab", slug: "rcpy/bkspy/gcrz", page: 1, totalPages: 1, want: base + "/rcpy/bkspy/gcrz.htm"},
	}
	if got := bioengPageURL(base, "xydt", 1, 47, "htm"); got != base+"/xydt.htm" {
		t.Fatalf("extension without dot = %q, want %q", got, base+"/xydt.htm")
	}
	if got := bioengPageURL(base, "xydt", 1, 47, ""); got != base+"/xydt.htm" {
		t.Fatalf("empty extension = %q, want %q", got, base+"/xydt.htm")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bioengPageURL(base, tt.slug, tt.page, tt.totalPages, ".htm"); got != tt.want {
				t.Fatalf("bioengPageURL() = %q, want %q", got, tt.want)
			}
		})
	}
	for _, tt := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 47},
		{name: "past last", page: 48, totalPages: 47},
		{name: "unknown reverse total", page: 2, totalPages: 0},
	} {
		t.Run("out of range/"+tt.name, func(t *testing.T) {
			if got := bioengPageURL(base, "xydt", tt.page, tt.totalPages, ".htm"); got != "" {
				t.Fatalf("bioengPageURL() = %q for page %d/%d, want empty URL", got, tt.page, tt.totalPages)
			}
		})
	}
}

func TestBioengHomepageParsesEveryVisibleNewsPanelAndExcludesCarousel(t *testing.T) {
	html := loadBioengTestHTML("bioeng_home.html")
	items, err := parseAdapter_bioengList(html, "https://bioeng.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("got %d homepage items, want one from each of six panels", len(items))
	}
	want := map[string]struct {
		title string
		date  string
		url   string
	}{
		"学院动态":  {title: "学院动态中的完整标题", date: "07-07", url: "/info/1031/20936.htm"},
		"学术动态":  {title: "学术动态中的完整标题", date: "07-01", url: "/info/1003/20986.htm"},
		"党建之窗":  {title: "党建之窗中的完整标题", date: "06-30", url: "/info/1004/20976.htm"},
		"学生工作":  {title: "学生工作中的完整标题", date: "06-24", url: "/info/1006/20966.htm"},
		"本科生培养": {title: "本科生培养中的完整标题", date: "06-30", url: "/info/1019/20846.htm"},
		"研究生培养": {title: "研究生培养中的完整标题", date: "07-17", url: "/info/1020/21036.htm"},
	}
	for _, item := range items {
		expected, ok := want[item.Type]
		if !ok {
			t.Errorf("unexpected homepage panel type %q", item.Type)
			continue
		}
		if item.Title != expected.title || item.Date != expected.date || !strings.HasSuffix(item.URL, expected.url) {
			t.Errorf("homepage item = %+v, want title=%q date=%q url suffix=%q", item, expected.title, expected.date, expected.url)
		}
		delete(want, item.Type)
		if strings.Contains(item.Title, "轮播") {
			t.Errorf("carousel leaked into homepage feed: %+v", item)
		}
	}
	if len(want) != 0 {
		t.Fatalf("homepage panels missing from parsed output: %#v", want)
	}
}

func TestBioengListPrefersHeadlineLinkAndKeepsType(t *testing.T) {
	html := loadBioengTestHTML("bioeng_list.html")
	items, err := parseAdapter_bioengList(html, "https://bioeng.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want 2", len(items))
	}
	if items[0].Title != "关于学院实验教学设备院内开放共享的通知" || items[0].Type != "学院通知" || items[0].Date != "2026-08-31" {
		t.Fatalf("unexpected first list item: %+v", items[0])
	}
	if items[0].PublishedAt != "" {
		t.Fatalf("date-only list item invented a source time: %q", items[0].PublishedAt)
	}
	if items[1].Date != "2026-08-30" || items[1].PublishedAt != "2026-08-30 09:15:00" {
		t.Fatalf("unexpected precise second list date: %+v", items[1])
	}
	for _, item := range items {
		if strings.Contains(item.Title, "头部") || strings.Contains(item.Title, "相关") {
			t.Errorf("non-row link leaked into list: %+v", item)
		}
	}
}

func TestBioengListUsesRowTagWhenItDiffersFromPageHeading(t *testing.T) {
	html := []byte(`<html><body><div class="main_right"><div class="current"><h3>通知公告</h3></div>
<div class="content"><dl><dt><a href="/info/1019/20846.htm">本科生培养通知</a></dt>
<dd><span class="spp"><a href="bkspy.htm">[本科生培养]</a></span><em>2026-07-13</em></dd></dl></div></div></body></html>`)
	items, err := parseAdapter_bioengList(html, "https://bioeng.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != "本科生培养" {
		t.Fatalf("row type = %#v, want 本科生培养", items)
	}
}

func TestBioengListParsesLegacyConlistCommMenus(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="党委简介"></head><body>
<div class="main_right"><div class="current"><h3>党委简介</h3></div>
<div class="conlistComm">
<dl><dt><span class="spp"><a href="dwjj.htm">[党委简介]</a></span><a href="../info/1162/8978.htm">党委简介</a></dt><dd>发布时间：2022-12-23</dd></dl>
<dl><dt><span class="spp"><a href="dwjj.htm">[党委简介]</a></span><a href="../info/1162/13066.htm">党章党规（常用）</a></dt><dd>发布时间：2024-06-07</dd></dl>
</div></div></body></html>`)
	items, err := parseAdapter_bioengList(html, "https://bioeng.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Title != "党委简介" || items[0].Date != "2022-12-23" || items[1].Date != "2024-06-07" {
		t.Fatalf("legacy menu items = %#v", items)
	}
	for _, item := range items {
		if item.Type != "党委简介" {
			t.Fatalf("legacy menu type = %q, want 党委简介", item.Type)
		}
	}
}

func TestBioengArticleParsesSourceBodyTypeAndExternalAttachments(t *testing.T) {
	html := loadBioengTestHTML("bioeng_article.html")
	article, err := parseAdapter_bioengArticle(html, "https://bioeng.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于学院实验教学设备院内开放共享的通知" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Source != "实验中心" || article.Author != "张帼威" || article.Type != "学院通知" {
		t.Fatalf("unexpected article metadata: source=%q author=%q type=%q", article.Source, article.Author, article.Type)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:27" {
		t.Fatalf("unexpected article date: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	wantContent := "第一段正文，介绍院内开放共享安排。\n\n第二段正文，说明设备预约和使用要求。\n\n正文中的事项时间：2026年9月16日，不能覆盖源日期。"
	if article.Content != wantContent {
		t.Fatalf("content = %q, want %q", article.Content, wantContent)
	}
	if strings.Contains(article.Content, "附件1：综合素质评价办法.pdf") {
		t.Fatal("attachment outside body was mixed into article content")
	}
	if !strings.Contains(article.ContentHTML, "第二段正文，说明设备预约和使用要求") || strings.Contains(article.ContentHTML, "综合素质评价办法.pdf") {
		t.Fatalf("content HTML boundary is wrong: %q", article.ContentHTML)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件1：综合素质评价办法.pdf" || article.Attachments[0].URL != "https://bioeng.swjtu.edu.cn/shengmingkexueyugongchengxueyuanbenkeshengzonghesuzhipingjiabanfa_2024nian_xiuding.pdf" {
		t.Fatalf("unexpected attachments: %#v", article.Attachments)
	}
}

func TestBioengResourceFiltersRejectPseudoLinksAndPlaceholders(t *testing.T) {
	if _, ok := bioengAttachmentURL("https://bioeng.swjtu.edu.cn", "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&wbfileid=ABC", "附件.docx"); ok {
		t.Fatal("unverified CMS download endpoint was accepted as a file")
	}
	if _, ok := bioengAttachmentURL("https://bioeng.swjtu.edu.cn", "javascript:void(0)", "附件.pdf"); ok {
		t.Fatal("javascript attachment link was accepted")
	}
	if _, ok := bioengImageURL("https://bioeng.swjtu.edu.cn", "/dfiles/images/code.png"); ok {
		t.Fatal("verification image was accepted as article media")
	}
	if got, ok := bioengImageURL("https://bioeng.swjtu.edu.cn", "/__local/article/figure.png"); !ok || got != "https://bioeng.swjtu.edu.cn/__local/article/figure.png" {
		t.Fatalf("valid image URL = %q, %v", got, ok)
	}
}

func TestBioengParsersRejectShellsAndNonArticles(t *testing.T) {
	homeShell := []byte(`<html><body><nav><a href="info/1000/1.htm">导航文章标题</a></nav><div class="main_visual"><a href="info/1000/2.htm">轮播文章标题</a></div></body></html>`)
	if _, err := parseAdapter_bioengList(homeShell, "https://bioeng.swjtu.edu.cn"); err == nil {
		t.Fatal("homepage shell with navigation/carousel was accepted as a list")
	}
	listShell := []byte(`<html><body><div class="content"><dl><dt>非文章链接</dt></dl></div></body></html>`)
	if _, err := parseAdapter_bioengList(listShell, "https://bioeng.swjtu.edu.cn"); err == nil {
		t.Fatal("list shell without valid article rows was accepted as a list")
	}
	nonArticle := []byte(`<html><head><meta name="pageType" content="1"></head><body><h1>错误页面</h1><div class="v_news_content"><p>不是文章</p></div></body></html>`)
	if _, err := parseAdapter_bioengArticle(nonArticle, "https://bioeng.swjtu.edu.cn"); err == nil {
		t.Fatal("non-article CMS page was accepted as an article")
	}
}

func TestBioengDateValuesOnlyPublishesSourceClock(t *testing.T) {
	if date, publishedAt := bioengDateValues("日期：2026-08-31"); date != "2026-08-31" || publishedAt != "" {
		t.Fatalf("date-only value = %q, %q", date, publishedAt)
	}
	if date, publishedAt := bioengDateValues("日期：2026-08-31 14:34"); date != "2026-08-31" || publishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("precise value = %q, %q", date, publishedAt)
	}
}
