package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCmAdapterRegistration(t *testing.T) {
	site, ok := AllSites["cm"]
	if !ok {
		t.Fatal("cm site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("CM adapter hooks are incomplete: %+v", site)
	}
	if !reflect.DeepEqual(site.SubPages, cmSubPages) {
		t.Fatalf("CM sub-pages = %#v, want %#v", site.SubPages, cmSubPages)
	}
}

func TestCmPageCountUsesDisplayedLogicalNumbers(t *testing.T) {
	body := []byte(`<div class="p_t">共537条</div><span class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="xyyw/35.htm">2</a></span><span class="p_no"><a href="xyyw/34.htm">3</a></span><span class="p_no"><a href="xyyw/1.htm">36</a></span></span>`)
	if got := cmPageCountFromHTML(body); got != 36 {
		t.Fatalf("cmPageCountFromHTML() = %d, want 36 from displayed logical page numbers", got)
	}
}

func TestCmPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	base := "https://cm.swjtu.edu.cn"
	cases := []struct {
		name  string
		slug  string
		page  int
		total int
		want  string
	}{
		{name: "first", slug: "index/xyyw", page: 1, total: 36, want: base + "/index/xyyw.htm"},
		{name: "second", slug: "index/xyyw", page: 2, total: 36, want: base + "/index/xyyw/35.htm"},
		{name: "middle", slug: "index/xyyw", page: 18, total: 36, want: base + "/index/xyyw/19.htm"},
		{name: "last", slug: "index/xyyw", page: 36, total: 36, want: base + "/index/xyyw/1.htm"},
		{name: "nested last", slug: "index/ssfc/xsfc", page: 2, total: 2, want: base + "/index/ssfc/xsfc/1.htm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmPageURL(base, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
				t.Fatalf("cmPageURL(%q, %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
			}
		})
	}
}

func TestCmPageURLRejectsOutOfRange(t *testing.T) {
	const base = "https://cm.swjtu.edu.cn"
	for _, tc := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 36},
		{name: "past last", page: 37, totalPages: 36},
		{name: "past one-page feed", page: 2, totalPages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmPageURL(base, "index/xyyw", tc.page, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("cmPageURL() = %q for page %d/%d, want empty URL", got, tc.page, tc.totalPages)
			}
		})
	}
}

func TestCmHomepageParsesAllNewsModulesAndExcludesChrome(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_home.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_cmList(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("got %d homepage items, want all home modules: %#v", len(items), items)
	}

	want := map[string]struct {
		title string
		date  string
		typ   string
	}{
		"/info/1031/1.htm": {title: "学院要闻中的完整标题", date: "2026-07-29", typ: "学院要闻"},
		"/info/1032/2.htm": {title: "通知公告中的完整标题", date: "2026-07-08", typ: "通知公告"},
		"/info/1033/3.htm": {title: "科研团队中的完整标题", typ: "科研团队"},
		"/info/1034/4.htm": {title: "科研动态中的完整标题", date: "01-12", typ: "科研动态"},
		"/info/1035/5.htm": {title: "招生就业中的完整标题", date: "06-18", typ: "招生就业"},
		"/info/1036/6.htm": {title: "校园生活中的完整标题", date: "06-22", typ: "校园生活"},
		"/info/1512/7.htm": {title: "学生风采中的完整标题", typ: "学生风采"},
		"/info/1522/8.htm": {title: "教师风采中的完整标题", typ: "教师风采"},
	}
	for _, item := range items {
		path := strings.TrimPrefix(item.URL, "https://cm.swjtu.edu.cn")
		expected, ok := want[path]
		if !ok {
			t.Errorf("unexpected homepage item: %+v", item)
			continue
		}
		if item.Title != expected.title || item.Date != expected.date || item.Type != expected.typ {
			t.Errorf("homepage item = %+v, want title=%q date=%q type=%q", item, expected.title, expected.date, expected.typ)
		}
		delete(want, path)
	}
	if len(want) != 0 {
		t.Fatalf("homepage modules missing from parsed output: %#v", want)
	}
	for _, item := range items {
		if strings.Contains(item.Title, "导航") || strings.Contains(item.Title, "轮播") || strings.Contains(item.Title, "静态") {
			t.Errorf("page chrome leaked into homepage feed: %+v", item)
		}
	}
}

func TestCmListParsesRowsAndPreciseTime(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_cmList(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want 2: %#v", len(items), items)
	}
	if items[0].Title != "列表中的精确时间通知" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31 14:34:00" || items[0].Type != "通知公告" {
		t.Fatalf("unexpected precise list item: %+v", items[0])
	}
	if items[1].Title != "列表中的日期公告" || items[1].Date != "2026-08-30" || items[1].PublishedAt != "" {
		t.Fatalf("unexpected date-only list item: %+v", items[1])
	}
}

func TestCmFengcaiListUsesNListTxt(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_fengcai.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_cmList(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Type != "教师风采" || items[1].Title != "第二位教师风采" {
		t.Fatalf("unexpected n_listTxt items: %#v", items)
	}
}

func TestCmArticleParserPrefersDtlTitleOverSidebarHome(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_cmArticle(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "国家卫生健康委员会专家组到医学院开展生物安全飞行检查" {
		t.Fatalf("article title = %q, want the .dtl-tit h4 headline", article.Title)
	}
	if article.Title == "首页" {
		t.Fatal("sidebar title leaked into article title")
	}
	if article.Date != "2026-07-29" {
		t.Fatalf("article date = %q, want 2026-07-29", article.Date)
	}
	if article.Type != "学院要闻" || article.Author != "智伟、赵春花" {
		t.Fatalf("article metadata = type %q author %q", article.Type, article.Author)
	}
	if article.Content == "" {
		t.Fatal("expected article content")
	}
}

func TestCmArticleParsesImagesAttachmentsAndMinutePrecision(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_article_resources.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_cmArticle(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "带资源的医学院通知" || article.Type != "常用信息" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("unexpected article metadata: %+v", article)
	}
	if article.Content != "正文第一段。\n\n正文第二段。" {
		t.Fatalf("content = %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://cm.swjtu.edu.cn/__local/cm-body.png" {
		t.Fatalf("unexpected images: %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("unexpected attachments: %#v", article.Attachments)
	}
	if article.Attachments[0].Name != "附件一.docx" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("unexpected first attachment: %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "附件二.pdf" || article.Attachments[1].URL != "https://cm.swjtu.edu.cn/__local/cm-file.pdf" {
		t.Fatalf("unexpected second attachment: %#v", article.Attachments[1])
	}
	if !strings.Contains(article.ContentHTML, "https://cm.swjtu.edu.cn/__local/cm-body.png") {
		t.Fatal("content HTML did not retain a resolved body image")
	}
}

func TestCmParsersRejectStaticShells(t *testing.T) {
	if _, err := parseAdapter_cmList([]byte(`<html><body><nav><a href="/info/1000/1.htm">导航文章标题</a></nav><section class="banner"><a href="/info/1000/2.htm">轮播文章标题</a></section></body></html>`), "https://cm.swjtu.edu.cn"); err == nil {
		t.Fatal("static page shell was accepted as a list")
	}
	if _, err := parseAdapter_cmArticle([]byte(`<html><body><h1>首页</h1><div class="v_news_content"><p>不是文章</p></div></body></html>`), "https://cm.swjtu.edu.cn"); err == nil {
		t.Fatal("static page shell was accepted as an article")
	}
}
