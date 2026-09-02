package scraper

import (
	"reflect"
	"strings"
	"testing"
)

func TestEesAdapterRegisteredWithAuditedRoutes(t *testing.T) {
	site := AllSites["ees"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatal("ees adapter hooks are not registered")
	}
	if site.BaseURL != "https://ees.swjtu.edu.cn" {
		t.Fatalf("ees base URL = %q", site.BaseURL)
	}
	want := []string{
		"xwdt1/xwdt", "xwdt1/mtbd", "djgz/djtz", "djgz/djdt",
		"tzgg/qb", "tzgg/zs", "tzgg/py", "tzgg/sj", "tzgg/xg", "tzgg/zh",
		"zsxx/zsdt", "zsxx/zlxz", "rcpy/kcjx", "rcpy/gcal", "rcpy/rqsj", "rcpy/xwgz",
		"xmpt/xmjs", "xmpt/xqpt", "xmpt/gjjlpt", "szdw/xnds", "szdw/qyds",
		"xsgz/xgtz", "xsgz/xshd", "gfwj/zcwj", "gfwj/gzzd", "gfwj/cybg",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("ees sub-pages = %#v, want %#v", site.SubPages, want)
	}
}

func TestEesPageURLUsesReverseGeneratedPages(t *testing.T) {
	base := "https://ees.swjtu.edu.cn"
	tests := []struct {
		name       string
		slug       string
		page       int
		totalPages int
		want       string
	}{
		{name: "first", slug: "xwdt1/xwdt", page: 1, totalPages: 5, want: base + "/xwdt1/xwdt.htm"},
		{name: "second", slug: "xwdt1/xwdt", page: 2, totalPages: 5, want: base + "/xwdt1/xwdt/4.htm"},
		{name: "middle", slug: "xwdt1/xwdt", page: 3, totalPages: 5, want: base + "/xwdt1/xwdt/3.htm"},
		{name: "last", slug: "xwdt1/xwdt", page: 5, totalPages: 5, want: base + "/xwdt1/xwdt/1.htm"},
		{name: "single page", slug: "tzgg/qb", page: 1, totalPages: 1, want: base + "/tzgg/qb.htm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eesPageURL(base, tt.slug, tt.page, tt.totalPages, ".htm"); got != tt.want {
				t.Fatalf("eesPageURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEesHomepageParsesVisibleAndHiddenArticleModules(t *testing.T) {
	items, err := parseAdapter_eesList(loadTestHTML("ees.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 17 {
		t.Fatalf("got %d homepage items, want 8 news + 7 notices + 2 hidden-panel articles", len(items))
	}
	types := make(map[string]int)
	seen := make(map[string]bool)
	for _, item := range items {
		if seen[item.URL] {
			t.Fatalf("duplicate homepage URL: %s", item.URL)
		}
		seen[item.URL] = true
		types[item.Type]++
		if !strings.HasPrefix(item.URL, "https://ees.swjtu.edu.cn/info/") {
			t.Fatalf("non-EES homepage URL leaked: %+v", item)
		}
		if strings.Contains(item.URL, "/info/1871/") {
			t.Fatalf("banner carousel leaked into homepage feed: %+v", item)
		}
	}
	if types["新闻动态"] != 8 || types["培养"] != 4 || types["综合"] != 3 || types["校企导师"] != 1 || types["卓工学生"] != 1 {
		t.Fatalf("unexpected homepage module types: %#v", types)
	}
	if items[0].Title != "竢实耀百卅 扬华铸未来——庆祝西南交通大学建校130周年大会隆重举行" || items[0].Date != "2026-05-18" {
		t.Fatalf("unexpected first homepage item: %+v", items[0])
	}
	if items[len(items)-1].Title != "国家卓越工程师学院迎来2025级新生" || items[len(items)-1].Type != "卓工学生" {
		t.Fatalf("hidden panel item was not retained: %+v", items[len(items)-1])
	}
}

func TestEesListParsesCanonicalRowsDatesAndType(t *testing.T) {
	items, err := parseAdapter_eesList(loadTestHTML("ees_list.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d list items, want three canonical rows", len(items))
	}
	if items[0].Title != "第一条真实新闻标题" || items[0].Date != "2026-07-26" || items[0].Type != "新闻动态" {
		t.Fatalf("unexpected first row: %+v", items[0])
	}
	if items[1].Date != "2026-07-25" || items[1].PublishedAt != "2026-07-25 14:34:00" {
		t.Fatalf("unexpected minute-precision row: %+v", items[1])
	}
	if items[2].Date != "2026-07-14" {
		t.Fatalf("unexpected final row date: %+v", items[2])
	}
	for _, item := range items {
		if strings.Contains(item.Title, "轮播") || strings.Contains(item.URL, "example.org") {
			t.Fatalf("non-list content leaked: %+v", item)
		}
	}
}

func TestEesPresentEmptyListIsAValidEmptyResult(t *testing.T) {
	items, err := parseAdapter_eesList(loadTestHTML("ees_empty_list.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty list result = %#v, want a non-nil empty slice", items)
	}
}

func TestEesArticleParsesMetadataBodyImageAndAttachment(t *testing.T) {
	article, err := parseAdapter_eesArticle(loadTestHTML("ees_article.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于工程实践的通知" || article.Source != "综合" || article.Type != "通知公告" {
		t.Fatalf("unexpected article metadata: %+v", article)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("unexpected article date: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	if article.Content != "第一段正文。\n\n第二段正文，含一个外部说明链接。说明" {
		t.Fatalf("unexpected article content: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://ees.swjtu.edu.cn/assets/article.jpg" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "工程实践申请表.docx" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("unexpected article attachments: %#v", article.Attachments)
	}
	if strings.Contains(article.Content, "工程实践申请表.docx") || strings.Contains(article.ContentHTML, "<style>") {
		t.Fatal("attachment or style leaked into cleaned article body")
	}
	if !strings.Contains(article.ContentHTML, `src="https://ees.swjtu.edu.cn/assets/article.jpg"`) || !strings.Contains(article.RawHTML, "关于工程实践的通知") {
		t.Fatal("article archive HTML did not retain the source/resource evidence")
	}
}

func TestEesArticleKeepsEmbeddedPDFAsVerifiableAttachment(t *testing.T) {
	article, err := parseAdapter_eesArticle(loadTestHTML("ees_article_pdf.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "正文内嵌PDF" || !strings.HasSuffix(article.Attachments[0].URL, "/document.pdf") {
		t.Fatalf("embedded PDF attachment = %#v", article.Attachments)
	}
}

func TestEesArticleRejectsStaticAndVerificationPages(t *testing.T) {
	staticPage := []byte(`<html><head><meta name="pageType" content="2"></head><body><div class="text-heading">静态页面</div><div class="v_news_content"><p>不是文章。</p></div></body></html>`)
	if _, err := parseAdapter_eesArticle(staticPage, "https://ees.swjtu.edu.cn"); err == nil {
		t.Fatal("static page was accepted as an article")
	}
	verificationPage := []byte(`<html><head><title>附件下载</title></head><body><h1>请输入验证码下载附件</h1></body></html>`)
	if _, err := parseAdapter_eesArticle(verificationPage, "https://ees.swjtu.edu.cn"); err == nil {
		t.Fatal("verification page was accepted as an article")
	}
}
