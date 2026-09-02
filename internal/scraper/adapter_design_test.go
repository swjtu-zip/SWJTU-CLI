package scraper

import (
	"os"
	"strings"
	"testing"
)

const designFixtureBase = "https://design.swjtu.edu.cn"

func TestDesignAdapterRegistrationAndPageURL(t *testing.T) {
	site, ok := AllSites["design"]
	if !ok {
		t.Fatal("design site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("design adapter hooks are incomplete: %+v", site)
	}

	if len(site.SubPages) != len(designSubPages) {
		t.Fatalf("design sub-pages = %d, want %d", len(site.SubPages), len(designSubPages))
	}
	for i, want := range designSubPages {
		if site.SubPages[i] != want {
			t.Errorf("design sub-page[%d] = %q, want %q", i, site.SubPages[i], want)
		}
	}
	for _, excluded := range []string{"xygk", "jyjx/bkzy", "jyjx/ssxwd", "jyjx/cybgxz", "kysj", "cpyc", "xszt"} {
		for _, slug := range site.SubPages {
			if slug == excluded {
				t.Errorf("static/resource/empty route %q was registered as a news feed", excluded)
			}
		}
	}

	cases := []struct {
		slug        string
		page, total int
		want        string
	}{
		{"xydt", 1, 25, designFixtureBase + "/xydt.htm"},
		{"xydt", 2, 25, designFixtureBase + "/xydt/24.htm"},
		{"xydt", 13, 25, designFixtureBase + "/xydt/13.htm"},
		{"xydt", 25, 25, designFixtureBase + "/xydt/1.htm"},
		{"xshd/jz", 2, 8, designFixtureBase + "/xshd/jz/7.htm"},
		{"xshd/jz", 8, 8, designFixtureBase + "/xshd/jz/1.htm"},
	}
	for _, tc := range cases {
		if got := designPageURL(designFixtureBase+"/", tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("designPageURL(%q, %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestDesignHomepageScopesVisibleAndHiddenArticleModules(t *testing.T) {
	html := readDesignFixture(t, "design_adapter_home.html")
	items, err := parseAdapter_designList(html, designFixtureBase)
	if err != nil {
		t.Fatalf("parse design homepage: %v", err)
	}
	if len(items) != 8 {
		t.Fatalf("got %d homepage articles, want 8: %#v", len(items), items)
	}

	if items[0].Title != "学院动态真实文章" || items[0].Type != "学院动态" || items[0].Date != "2026-09-01" {
		t.Fatalf("unexpected dynamic item: %#v", items[0])
	}
	if items[1].Type != "讲座" || items[1].Date != "2026-07-10" || items[1].PublishedAt != "2026-07-10 14:30:00" {
		t.Fatalf("event timestamp was not preserved: %#v", items[1])
	}
	if items[3].Type != "论坛" || items[4].Type != "活动" {
		t.Fatalf("hidden academic tab types were not retained: %#v", items[2:5])
	}
	if items[5].Type != "通知公告" || items[6].Type != "本科生通知" || items[7].Type != "研究生通知" {
		t.Fatalf("hidden notice tab types were not retained: %#v", items[5:])
	}
	for _, item := range items {
		if strings.Contains(item.Title, "导航") || strings.Contains(item.Title, "轮播") || strings.Contains(item.URL, "mp.weixin") {
			t.Fatalf("non-module link leaked into homepage items: %#v", item)
		}
	}
}

func TestDesignListRejectsNavigationAndExternalLinks(t *testing.T) {
	html := readDesignFixture(t, "design_adapter_list.html")
	items, err := parseAdapter_designList(html, designFixtureBase)
	if err != nil {
		t.Fatalf("parse design list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list articles, want 2: %#v", len(items), items)
	}
	if items[0].Title != "学院动态列表文章一" || items[0].Date != "2026-08-01" || items[0].Type != "学院动态" {
		t.Fatalf("unexpected first list item: %#v", items[0])
	}
	if items[1].Title != "学院动态列表文章二" || items[1].URL != designFixtureBase+"/info/1016/2.htm" {
		t.Fatalf("unexpected second list item: %#v", items[1])
	}
}

func TestDesignAdapterDistinguishesEmptyColumnFromShell(t *testing.T) {
	empty := []byte(`<html><body><div class="file"><div class="title">科研实践</div><div class="list"><ul></ul></div></div></body></html>`)
	items, err := parseAdapter_designList(empty, designFixtureBase)
	if err != nil {
		t.Fatalf("reachable empty column should parse: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty column produced articles: %#v", items)
	}
	if _, err := parseAdapter_designList([]byte(`<html><body><div id="app"></div></body></html>`), designFixtureBase); err == nil {
		t.Fatal("static shell was accepted as a design list")
	}
}

func TestDesignArticleFieldsImagesAndAttachments(t *testing.T) {
	html := readDesignFixture(t, "design_adapter_article.html")
	article, err := parseAdapter_designArticle(html, designFixtureBase)
	if err != nil {
		t.Fatalf("parse design article: %v", err)
	}
	if article.Title != "设计艺术学院详情测试通知" || article.Date != "2026-09-01" || article.PublishedAt != "2026-09-01 12:34:00" {
		t.Fatalf("unexpected article title/time: %+v", article)
	}
	if article.Type != "学院动态" {
		t.Fatalf("unexpected article type: %+v", article)
	}
	if !strings.Contains(article.Content, "正文段落") || strings.Contains(article.Content, "侧栏推荐") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != designFixtureBase+"/__local/article.jpg" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("got %d attachments, want 2: %#v", len(article.Attachments), article.Attachments)
	}
	if article.Attachments[0].Name != "正文附件.pdf" || article.Attachments[0].URL != designFixtureBase+"/files/body.pdf" {
		t.Fatalf("unexpected body attachment: %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "外部附件.xlsx" || article.Attachments[1].URL != designFixtureBase+"/system/_content/download.jsp?id=fixture" {
		t.Fatalf("unexpected outside attachment: %#v", article.Attachments[1])
	}
}

func TestDesignAcademicArticleUsesEventHeader(t *testing.T) {
	html := readDesignFixture(t, "design_adapter_event_article.html")
	article, err := parseAdapter_designArticle(html, designFixtureBase)
	if err != nil {
		t.Fatalf("parse design event article: %v", err)
	}
	if article.Title != "人因与智能交互驱动创新设计的四种模式" || article.Type != "讲座" {
		t.Fatalf("unexpected event header: %+v", article)
	}
	if article.Date != "2026-07-10" || article.PublishedAt != "2026-07-10 14:30:00" {
		t.Fatalf("event date/time was not retained: %+v", article)
	}
	if !strings.Contains(article.Content, "学术活动正文") || len(article.Images) != 1 {
		t.Fatalf("event body/image was not parsed: %+v", article)
	}
}

func TestDesignArticleRejectsNonArticleResponse(t *testing.T) {
	if _, err := parseAdapter_designArticle([]byte(`<html><head><title>404错误提示</title></head><body>404</body></html>`), designFixtureBase); err == nil {
		t.Fatal("error page was accepted as an article")
	}
	if _, err := parseAdapter_designArticle([]byte(`<div class="news-title">首页壳</div><div id="app"></div>`), designFixtureBase); err == nil {
		t.Fatal("homepage shell was accepted as an article")
	}
}

func readDesignFixture(t *testing.T, name string) []byte {
	t.Helper()
	html, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return html
}
