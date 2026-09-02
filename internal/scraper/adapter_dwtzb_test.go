package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const dwtzbFixtureBaseURL = "https://dwtzb.swjtu.edu.cn"

func readDwtzbFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read dwtzb fixture %s: %v", name, err)
	}
	return data
}

func TestDwtzbAdapterRegistrationAndRoutes(t *testing.T) {
	site := AllSites["dwtzb"]
	if site == nil || site.BaseURL != dwtzbFixtureBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("dwtzb adapter hooks are incomplete: %#v", site)
	}
	want := dwtzbSubPages
	if !reflect.DeepEqual(site.SubPages, want) || len(site.SubPages) != 28 {
		t.Fatalf("dwtzb routes = %#v, want 28 real list routes", site.SubPages)
	}
	for _, excluded := range []string{"bmgk/gzzz", "bmgk/ryzc", "tztt"} {
		for _, route := range site.SubPages {
			if route == excluded {
				t.Errorf("static route registered as feed: %q", excluded)
			}
		}
	}
}

func TestDwtzbPageURLAndTotalPagesUseReverseNumbering(t *testing.T) {
	tests := []struct {
		slug        string
		page, total int
		want        string
	}{
		{slug: "zhyw/xnxw", page: 1, total: 124, want: dwtzbFixtureBaseURL + "/zhyw/xnxw.htm"},
		{slug: "zhyw/xnxw", page: 2, total: 124, want: dwtzbFixtureBaseURL + "/zhyw/xnxw/123.htm"},
		{slug: "zhyw/xnxw", page: 62, total: 124, want: dwtzbFixtureBaseURL + "/zhyw/xnxw/63.htm"},
		{slug: "zhyw/xnxw", page: 124, total: 124, want: dwtzbFixtureBaseURL + "/zhyw/xnxw/1.htm"},
		{slug: "zcws/ldls", page: 2, total: 8, want: dwtzbFixtureBaseURL + "/zcws/ldls/7.htm"},
	}
	for _, tt := range tests {
		if got := dwtzbPageURL(dwtzbFixtureBaseURL, tt.slug, tt.page, tt.total, ".htm"); got != tt.want {
			t.Errorf("dwtzbPageURL(%q, %d/%d) = %q, want %q", tt.slug, tt.page, tt.total, got, tt.want)
		}
	}
	if got := dwtzbTotalPagesFromHTML(readDwtzbFixture(t, "dwtzb_list.html")); got != 3 {
		t.Fatalf("total pages = %d, want 3", got)
	}
}

func TestDwtzbHomepageScopesAllArticleModules(t *testing.T) {
	items, err := parseAdapter_dwtzbList(readDwtzbFixture(t, "dwtzb_home.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("homepage items = %d, want six center and two sidebar items: %#v", len(items), items)
	}
	wantTypes := map[string]bool{"校内新闻": true, "政策务实": true, "民主党派": true, "无党派": true, "参政议政": true, "互联快递": true, "通知公告": true, "人物风采": true}
	for _, item := range items {
		if !wantTypes[item.Type] {
			t.Errorf("unexpected homepage type: %#v", item)
		}
		if strings.Contains(item.URL, "/9514.htm") || strings.Contains(item.URL, "example.com") {
			t.Errorf("carousel/external link leaked into homepage: %#v", item)
		}
	}
}

func TestDwtzbListAndArticleParsers(t *testing.T) {
	items, err := parseAdapter_dwtzbList(readDwtzbFixture(t, "dwtzb_list.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Type != "校内新闻" || items[0].Date != "2026-08-08" || items[0].PublishedAt != "" {
		t.Fatalf("list items = %#v", items)
	}
	if _, err := parseAdapter_dwtzbList([]byte(`<html><body><div id="content" class="pages"><p>工作职责</p></div></body></html>`), dwtzbFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as an article list")
	}

	article, err := parseAdapter_dwtzbArticle(readDwtzbFixture(t, "dwtzb_article.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于测试附件的通知" || article.Type != "通知公告" || article.Source != "统战新语" || article.Author != "张三" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Date != "2026-09-02" || article.PublishedAt != "2026-09-02 14:34:00" {
		t.Fatalf("article date = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "这是正文内容") || len(article.Images) != 1 || article.Images[0] != dwtzbFixtureBaseURL+"/__local/body.png" {
		t.Fatalf("article body/resources = %#v", article)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件：测试表.xlsx" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	if _, err := parseAdapter_dwtzbArticle([]byte(`<html><head><meta name="pageType" content="1"></head><body><h1>页面</h1></body></html>`), dwtzbFixtureBaseURL); err == nil {
		t.Fatal("non-article page was accepted as an article")
	}
}

func TestDwtzbDateValuesDoNotInventClock(t *testing.T) {
	if date, publishedAt := dwtzbDateValues("日期：2026-08-31"); date != "2026-08-31" || publishedAt != "" {
		t.Fatalf("date-only value = %q/%q", date, publishedAt)
	}
	if date, publishedAt := dwtzbDateValues("日期：2026-08-31 14:34"); date != "2026-08-31" || publishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("precise value = %q/%q", date, publishedAt)
	}
}
