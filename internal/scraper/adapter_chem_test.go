package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const chemFixtureBaseURL = "https://chem.swjtu.edu.cn"

func TestChemAdapterRegistrationAndPageURL(t *testing.T) {
	site, ok := AllSites["chem"]
	if !ok {
		t.Fatal("chem site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("chem adapter hooks are incomplete: %+v", site)
	}
	wantSubPages := []string{
		"index/xyxw",
		"index/xytz",
		"index/jwgg",
		"index/jwgg/yjsgg",
		"index/ttxw",
		"index/kxyj",
		"rcpy",
		"rcpy/yjsds",
		"rcpy/yjspygg",
		"rcpy/wjzlxz",
		"kxyj",
		"kxyj/kytz",
		"kxyj/kytd",
		"kxyj/kydt",
		"kxyj/xshd",
		"kxyj/sypt",
		"dqgz",
		"dqgz/djc",
		"dqgz/djdt",
		"dqgz/zzjsydyfz",
		"dqgz/jwgz",
		"dqgz/ghgz",
		"xsgz",
		"xsgz/xgtz",
		"xsgz/xgdt",
		"xsgz/gsgg",
		"aqgz",
		"aqgz/aqxz",
		"aqgz/gzzd",
		"aqgz/xgzy",
		"rczp",
		"xygz",
		"xygz/xyfc",
		"xygz/bysfc",
		"hzjl",
		"hzjl/gjhhz",
		"hzjl/xqhz",
		"zsjy",
		"zsjy/zsgz",
		"zsjy/jygz",
	}
	if !reflect.DeepEqual(site.SubPages, wantSubPages) {
		t.Fatalf("chem sub-pages = %#v, want %#v", site.SubPages, wantSubPages)
	}

	cases := []struct {
		slug  string
		page  int
		total int
		want  string
	}{
		{"index/xyxw", 1, 31, chemFixtureBaseURL + "/index/xyxw.htm"},
		{"index/xyxw", 2, 31, chemFixtureBaseURL + "/index/xyxw/30.htm"},
		{"index/xyxw", 16, 31, chemFixtureBaseURL + "/index/xyxw/16.htm"},
		{"index/xyxw", 30, 31, chemFixtureBaseURL + "/index/xyxw/2.htm"},
		{"index/xyxw", 31, 31, chemFixtureBaseURL + "/index/xyxw/1.htm"},
		{"index/jwgg/yjsgg", 2, 11, chemFixtureBaseURL + "/index/jwgg/yjsgg/10.htm"},
		{"index/jwgg/yjsgg", 11, 11, chemFixtureBaseURL + "/index/jwgg/yjsgg/1.htm"},
		{"rcpy", 2, 2, chemFixtureBaseURL + "/rcpy/1.htm"},
	}
	for _, tc := range cases {
		if got := chemPageURL(chemFixtureBaseURL, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("chemPageURL(%q, page %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestChemPageURLRejectsOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 31},
		{name: "past last", page: 32, totalPages: 31},
		{name: "past one-page feed", page: 2, totalPages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chemPageURL(chemFixtureBaseURL, "index/xyxw", tc.page, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("chemPageURL() = %q for page %d/%d, want empty URL", got, tc.page, tc.totalPages)
			}
		})
	}
}

func TestChemHomeParserScopesRealModulesAndTabs(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_home.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_chemList(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 7 {
		t.Fatalf("got %d chem home articles, want 7: %#v", len(items), items)
	}
	want := []struct {
		url   string
		title string
		date  string
		typ   string
	}{
		{chemFixtureBaseURL + "/info/1086/1001.htm", "真实学院新闻", "2026-07-16", "学院新闻"},
		{chemFixtureBaseURL + "/info/1086/1002.htm", "另一条学院新闻", "2026-07-15", "学院新闻"},
		{chemFixtureBaseURL + "/info/1098/2001.htm", "真实通知公告", "2026-06-06", "通知公告"},
		{chemFixtureBaseURL + "/info/1119/3001.htm", "本科生真实公告", "2026-07-01", "教务公告"},
		{chemFixtureBaseURL + "/info/1120/3002.htm", "研究生真实公告", "2026-06-16", "教务公告"},
		{chemFixtureBaseURL + "/info/1133/4001.htm", "学术报告真实文章", "2026-07-12", "科研动态"},
		{chemFixtureBaseURL + "/info/1132/4002.htm", "科研进展真实文章", "2026-07-15", "科研动态"},
	}
	seen := make(map[string]bool)
	for i, expected := range want {
		item := items[i]
		if item.URL != expected.url || item.Title != expected.title || item.Date != expected.date || item.Type != expected.typ {
			t.Errorf("home item %d = (%q, %q, %q, %q), want (%q, %q, %q, %q)", i, item.URL, item.Title, item.Date, item.Type, expected.url, expected.title, expected.date, expected.typ)
		}
		if seen[item.URL] {
			t.Errorf("duplicate home article URL: %q", item.URL)
		}
		seen[item.URL] = true
	}
	for _, item := range items {
		if strings.Contains(item.Title, "伪文章") || strings.Contains(item.Title, "导航") || strings.Contains(item.Title, "轮播") || strings.Contains(item.Title, "侧栏") || strings.Contains(item.Title, "页脚") {
			t.Errorf("non-article page chrome leaked into home results: %#v", item)
		}
	}
}

func TestChemListParserExtractsCategoryDateAndFiltersExternal(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_list.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_chemList(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d chem list articles, want 2: %#v", len(items), items)
	}
	if items[0].URL != chemFixtureBaseURL+"/info/1098/2001.htm" || items[0].Title != "关于开展真实通知" || items[0].Date != "2026-06-16" || items[0].Type != "通知公告" {
		t.Errorf("unexpected first list item: %#v", items[0])
	}
	if items[1].URL != chemFixtureBaseURL+"/info/1086/2002.htm" || items[1].Date != "2026-07-15" || items[1].Type != "学院新闻" {
		t.Errorf("unexpected second list item: %#v", items[1])
	}
	for _, item := range items {
		if strings.Contains(item.URL, "example.com") || strings.Contains(item.Title, "伪文章") {
			t.Errorf("external or fake list entry leaked: %#v", item)
		}
	}
}

func TestChemLegacyListAcceptsSourceContentURL(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_legacy_list.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_chemList(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d legacy chem articles, want 1: %#v", len(items), items)
	}
	wantURL := chemFixtureBaseURL + "/content.jsp?urltype=news.NewsContentUrl&wbtreeid=1117&wbnewsid=14809"
	if items[0].URL != wantURL || items[0].Type != "头条新闻" || items[0].Date != "2026-06-08" {
		t.Errorf("unexpected legacy list item: %#v, want URL %q", items[0], wantURL)
	}
}

func TestChemArticleParserExtractsMetadataBodyOutsideImageAndAttachment(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_article_attachment.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_chemArticle(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于真实通知" || article.Date != "2026-06-16" || article.PublishedAt != "2026-06-16 09:27:00" {
		t.Fatalf("unexpected article publication fields: title=%q date=%q published_at=%q", article.Title, article.Date, article.PublishedAt)
	}
	if article.Type != "学院通知" || article.Source != "化学学院" || article.Author != "张三" {
		t.Fatalf("unexpected article metadata: type=%q source=%q author=%q", article.Type, article.Source, article.Author)
	}
	if article.Content != "这是正文内容。" {
		t.Fatalf("article content = %q, want 正文内容", article.Content)
	}
	wantImages := []string{
		chemFixtureBaseURL + "/__local/body.png",
		chemFixtureBaseURL + "/__local/extra.png",
	}
	if !reflect.DeepEqual(article.Images, wantImages) {
		t.Fatalf("article images = %#v, want %#v", article.Images, wantImages)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("got %d attachments, want 1: %#v", len(article.Attachments), article.Attachments)
	}
	wantAttachmentURL := chemFixtureBaseURL + "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1897809613&wbfileid=ABC123"
	if article.Attachments[0].Name != "测试材料.pdf" || article.Attachments[0].URL != wantAttachmentURL {
		t.Fatalf("unexpected attachment: %#v, want URL %q", article.Attachments[0], wantAttachmentURL)
	}
}

func TestChemParserRejectsShellAndErrorPages(t *testing.T) {
	if _, err := parseAdapter_chemList([]byte(`<html><body><div id="app"></div></body></html>`), chemFixtureBaseURL); err == nil {
		t.Fatal("SPA shell was accepted as a chem list")
	}
	if _, err := parseAdapter_chemList([]byte(`<main><div class="profile"><h1>学院概况</h1></div></main>`), chemFixtureBaseURL); err == nil {
		t.Fatal("static profile page was accepted as a chem list")
	}
	if _, err := parseAdapter_chemArticle([]byte(`<html><head><title>系统提示</title></head><body>您访问的页面未找到</body></html>`), chemFixtureBaseURL); err == nil {
		t.Fatal("source error page was accepted as a chem article")
	}
}
