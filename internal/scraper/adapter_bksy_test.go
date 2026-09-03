package scraper

import (
	"os"
	"strings"
	"testing"
)

func TestBksyAdapterRegistrationCoversRealEntryPoints(t *testing.T) {
	site, ok := AllSites["bksy"]
	if !ok {
		t.Fatal("bksy site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("bksy adapter hooks are incomplete: %+v", site)
	}

	required := []string{
		"tzgg/qb", "tzgg/tsjy", "tzgg/glgd",
		"jgsz/glgzzd", "jgsz/xgwjbg",
		"jxyx/xjxg", "jxjs/jxgg", "jsfz/jsjxzz",
		"syjx/xfzx", "sjjx/xkjs", "cxcy/cxcyhd", "zljj/zlbz",
	}
	seen := make(map[string]bool, len(site.SubPages))
	for _, slug := range site.SubPages {
		if seen[slug] {
			t.Fatalf("duplicate bksy sub-page: %q", slug)
		}
		seen[slug] = true
	}
	for _, slug := range required {
		if !seen[slug] {
			t.Errorf("missing bksy sub-page %q", slug)
		}
	}
	if len(site.SubPages) != len(bksySubPages) || len(site.SubPages) < 40 {
		t.Fatalf("unexpected bksy sub-page coverage: %d (%#v)", len(site.SubPages), site.SubPages)
	}
}

func TestBksyAdapterPageURLUsesReverseStaticNumbering(t *testing.T) {
	const base = "https://bksy.swjtu.edu.cn/"
	cases := []struct {
		slug, want  string
		page, total int
	}{
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb.htm", 1, 243},
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb/242.htm", 2, 243},
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb/122.htm", 122, 243},
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb/2.htm", 242, 243},
		{"tzgg/qb", "https://bksy.swjtu.edu.cn/tzgg/qb/1.htm", 243, 243},
		{"tzgg/syjx", "https://bksy.swjtu.edu.cn/tzgg/syjx/2.htm", 2, 3},
		{"tzgg/qb", "", 2, 0},
	}
	for _, tc := range cases {
		if got := bksyAdapterPageURL(base, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("page URL %s page %d/%d = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestBksyAdapterPageURLRejectsOutOfRange(t *testing.T) {
	const base = "https://bksy.swjtu.edu.cn/"
	for _, tc := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 243},
		{name: "past last", page: 244, totalPages: 243},
		{name: "past one-page feed", page: 2, totalPages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bksyAdapterPageURL(base, "tzgg/qb", tc.page, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("bksyAdapterPageURL() = %q for page %d/%d, want empty URL", got, tc.page, tc.totalPages)
			}
		})
	}
}

func TestBksyAdapterHomepageScopesOnlyTheNoticeTab(t *testing.T) {
	html, err := os.ReadFile("testdata/bksy_adapter_home.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_bksyList(html, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse homepage: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d homepage articles, want one real internal article: %#v", len(items), items)
	}
	item := items[0]
	if item.Title != "本科生院真实通知" || item.URL != "https://bksy.swjtu.edu.cn/info/1121/94301.htm" {
		t.Fatalf("unexpected homepage item: %#v", item)
	}
	if item.Date != "2026/07/08" || item.PublishedAt != "2026-07-08" || item.Type != "全部" {
		t.Fatalf("unexpected homepage metadata: %#v", item)
	}
	for _, rejected := range []string{"轮播文章", "隐藏 tab 文章", "侧栏推荐", "直接下载文件", "跨站 SPA 文章"} {
		if item.Title == rejected {
			t.Fatalf("non-list item leaked: %#v", item)
		}
	}
}

func TestBksyAdapterCategoryListRejectsFilesAndExternalRoutes(t *testing.T) {
	html, err := os.ReadFile("testdata/bksy_adapter_list.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_bksyList(html, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse category: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d category articles, want one internal article: %#v", len(items), items)
	}
	if items[0].Title != "教学运行真实通知" || items[0].Type != "教学运行" || items[0].Date != "2026/09/01" {
		t.Fatalf("unexpected category item: %#v", items[0])
	}
}

func TestBksyAdapterDistinguishesEmptyListFromShell(t *testing.T) {
	empty := []byte(`<main class="list-container"><div class="content-nav">通识教育</div><ul class="right-list"><li class="pagination">首页</li></ul></main>`)
	items, err := parseAdapter_bksyList(empty, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("reachable empty list should parse: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list produced articles: %#v", items)
	}

	if _, err := parseAdapter_bksyList([]byte(`<html><body><div id="app"></div></body></html>`), "https://bksy.swjtu.edu.cn"); err == nil {
		t.Fatal("SPA shell was accepted as a bksy list")
	}
}

func TestBksyAdapterArticleFieldsAndOutsideAttachment(t *testing.T) {
	html, err := os.ReadFile("testdata/bksy_adapter_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_bksyArticle(html, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse article: %v", err)
	}
	if article.Title != "本科生院详情测试通知" || article.Date != "2026-08-06" || article.PublishedAt != "2026-08-06 12:34:56" {
		t.Fatalf("unexpected title/time: %+v", article)
	}
	if article.Type != "教学改革与研究" || article.Source != "本科生院" {
		t.Fatalf("unexpected type/source: %+v", article)
	}
	if !strings.Contains(article.Content, "正文段落") || strings.Contains(article.Content, "侧栏推荐") || strings.Contains(article.Content, "外部附件.xlsx") {
		t.Fatalf("body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://bksy.swjtu.edu.cn/__local/body.png" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("got %d attachments, want body + outside attachment: %#v", len(article.Attachments), article.Attachments)
	}
	if article.Attachments[0].Name != "正文附件.pdf" || article.Attachments[0].URL != "https://bksy.swjtu.edu.cn/__local/body.pdf?e=.pdf" {
		t.Fatalf("unexpected body attachment: %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "外部附件.xlsx" || article.Attachments[1].URL != "https://bksy.swjtu.edu.cn/system/_content/download.jsp?id=fixture" {
		t.Fatalf("unexpected outside attachment: %#v", article.Attachments[1])
	}
}

func TestBksyAdapterRejectsNonArticleResponse(t *testing.T) {
	if _, err := parseAdapter_bksyArticle([]byte(`<html><head><title>404错误提示</title></head><body>404</body></html>`), "https://bksy.swjtu.edu.cn"); err == nil {
		t.Fatal("error page was accepted as an article")
	}
	if _, err := parseAdapter_bksyArticle([]byte(`<div class="news-title">首页壳</div><div id="app"></div>`), "https://bksy.swjtu.edu.cn"); err == nil {
		t.Fatal("homepage shell was accepted as an article")
	}
}
