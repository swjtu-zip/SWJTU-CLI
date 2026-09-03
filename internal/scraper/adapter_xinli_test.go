package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestXinliAdapterRegistered(t *testing.T) {
	site := AllSites["xinli"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("xinli adapter hooks are not registered")
	}
	want := []string{
		"zxyw", "tzgg/qb", "tzgg/xljkjy", "tzgg/dzbg", "tzgg/bks",
		"tzgg/yjs", "tzgg/kygz", "tzgg/xsgz", "tzgg/sygz", "tzgg/gsgg",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("xinli feeds = %#v, want %#v", site.SubPages, want)
	}
	if site.PageURL == nil {
		t.Fatal("xinli reverse pagination hook is missing")
	}
}

func TestXinliHomepageCollectsEveryVisibleNewsTab(t *testing.T) {
	html, err := os.ReadFile("testdata/xinli.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_xinliList(html, "http://xinli.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 70 {
		t.Fatalf("homepage items = %d, want all centre and notice tabs", len(items))
	}

	types := make(map[string]int)
	for _, item := range items {
		types[item.Type]++
		if !strings.HasPrefix(item.URL, "http://xinli.swjtu.edu.cn/info/") {
			t.Errorf("non-local item URL = %q", item.URL)
		}
	}
	for _, typ := range []string{"中心要闻", "心理健康教育", "党政办公", "本科生", "研究生", "科研工作", "学生工作", "实验工作", "公示公告"} {
		if types[typ] == 0 {
			t.Errorf("homepage has no items for type %q; types=%#v", typ, types)
		}
	}
	for _, item := range items {
		if item.Title == "心理咨询值班" || strings.Contains(item.URL, "/info/1020/") {
			t.Errorf("service link was treated as a news item: %#v", item)
		}
	}
}

func TestXinliListUsesRealRowsAndColumnTypes(t *testing.T) {
	html := []byte(`<!doctype html><html><head>
<meta name="pageType" content="2"><meta name="pageTitle" content="公示公告">
</head><body><section class="ej_main_left"><h2 class="lmmc_yj">公示公告</h2>
<div class="ej_list ej_list_ss"><div><UL>
<li><a href="info/1572/47856.htm" title="真实公告"><div class="box"><p class="bt">真实公告</p><p class="sj">2026-08-24</p></div></a></li>
<li><a href="https://example.com/not-an-article">外部链接</a></li>
<li><a href="info/1522/48086.htm"><div class="box"><p class="bt">本科通知</p><p class="sj">2026-08-23</p></div></a></li>
</UL></div></div></section></body></html>`)
	items, err := parseAdapter_xinliList(html, "http://xinli.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v, want two local article rows", items)
	}
	if items[0].Type != "公示公告" || items[0].Date != "2026-08-24" || items[0].PublishedAt != "2026-08-24" {
		t.Fatalf("first item metadata = %#v", items[0])
	}
	if items[1].Type != "本科生" {
		t.Fatalf("column type = %q, want 本科生", items[1].Type)
	}
}

func TestXinliReversePageURL(t *testing.T) {
	got := reverseBodaPageURL("http://xinli.swjtu.edu.cn", "zxyw", 2, 150, ".htm")
	if got != "http://xinli.swjtu.edu.cn/zxyw/149.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
}

func TestXinliArticleUsesDetailTitleInsteadOfSectionHeading(t *testing.T) {
	html, err := os.ReadFile("testdata/xinli_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_xinliArticle(html, "http://xinli.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	wantTitle := "第九届“演说中国”全国青少年演讲与朗诵艺术展示全国总展演隆重启幕"
	if article.Title != wantTitle {
		t.Fatalf("title = %q, want detail title %q", article.Title, wantTitle)
	}
	if article.Title == "中心要闻" {
		t.Fatal("section heading was incorrectly used as article title")
	}
	if article.Date != "2026-08-13" {
		t.Fatalf("date = %q, want 2026-08-13", article.Date)
	}
	if article.Type != "中心要闻" {
		t.Fatalf("type = %q, want 中心要闻", article.Type)
	}
	if !strings.Contains(article.Content, "全国总展演开幕式") {
		t.Fatalf("article content was not extracted: %q", article.Content)
	}
}
