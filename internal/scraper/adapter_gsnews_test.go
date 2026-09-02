package scraper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

const gsnewsFixtureBaseURL = "https://gsnews.example.edu.cn"

func readGsnewsFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read gsnews fixture %s: %v", name, err)
	}
	return body
}

func TestGsnewsRegistrationCoversAuditedNavigationLeaves(t *testing.T) {
	want := []string{
		"xwdtnew/rdxw", "xwdtnew/xsdt", "xwdtnew/mtbd",
		"tzggnew/qb", "tzggnew/pygz", "tzggnew/xwgz", "tzggnew/sxsj", "tzggnew/zhxx", "gzlc/pygz",
		"gzzdnew/yzgz", "gzzdnew/pygz", "gzzdnew/xwgz", "gzzdnew/sxsj", "gzzdnew/gjjl", "gzzdnew/zhsw",
		"pygznew/pyfa", "pygznew/xjxl", "pygznew/jxgl", "pygznew/kcsz", "pygznew/sxsj", "pygznew/gjjl", "pygznew/jxgg", "pygznew/jcjs", "pygznew/zldd", "pygznew/jsfz", "pygznew/qt",
		"xwgznew/xwsy", "xwgznew/xwsqd", "xwgznew/dsdw", "xwgznew/pycy", "xwgznew/tdxl",
		"xzzx/pygz", "xzzx/xwgz", "xzzx/sxsj", "xzzx/gjjl", "xzzx/lxsxg",
		"zhxxnew/xkjs", "zhxxnew/cydjt", "zsgz/sszs", "zsgz/bszs", "zsgz/zsxc",
	}
	if len(gsnewsSubPages) != len(want) {
		t.Fatalf("subpage count = %d, want %d", len(gsnewsSubPages), len(want))
	}
	for i, slug := range want {
		if gsnewsSubPages[i] != slug {
			t.Errorf("subpage %d = %q, want %q", i, gsnewsSubPages[i], slug)
		}
		if _, ok := gsnewsPageTotals[slug]; !ok {
			t.Errorf("subpage %q has no audited page total", slug)
		}
	}
	if len(gsnewsPageTotals) != len(gsnewsSubPages) {
		t.Errorf("page total count = %d, want one entry per feed (%d)", len(gsnewsPageTotals), len(gsnewsSubPages))
	}
	for _, excluded := range []string{"jgjjnew/yjsyjj", "zsgz", "pygznew", "xzzx/yzgz", "xzzx/zhsw", "https://yz.swjtu.edu.cn/", "https://ygb.swjtu.edu.cn/index.htm"} {
		if gsnewsKnownFeed(excluded) {
			t.Errorf("static/external route was registered as feed: %q", excluded)
		}
	}
}

func TestGsnewsPageURLUsesReverseGeneratedFiles(t *testing.T) {
	tests := []struct {
		name       string
		slug       string
		page       int
		total      int
		wantSuffix string
	}{
		{name: "first", slug: "tzggnew/qb", page: 1, total: 60, wantSuffix: "/tzggnew/qb.htm"},
		{name: "middle", slug: "tzggnew/qb", page: 30, total: 60, wantSuffix: "/tzggnew/qb/31.htm"},
		{name: "last", slug: "tzggnew/qb", page: 60, total: 60, wantSuffix: "/tzggnew/qb/1.htm"},
		{name: "two-page", slug: "xzzx/pygz", page: 2, total: 2, wantSuffix: "/xzzx/pygz/1.htm"},
		{name: "known-fallback", slug: "zsgz/sszs", page: 2, total: 0, wantSuffix: "/zsgz/sszs/11.htm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gsnewsPageURL(gsnewsFixtureBaseURL, tt.slug, tt.page, tt.total, ".htm")
			if !strings.HasSuffix(got, tt.wantSuffix) {
				t.Fatalf("page URL = %q, want suffix %q", got, tt.wantSuffix)
			}
		})
	}
}

func TestGsnewsHomeParsesNewsAndAllHiddenNoticeTabs(t *testing.T) {
	items, err := parseAdapter_gsnewsList(readGsnewsFixture(t, "gsnews_home.html"), gsnewsFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("home item count = %d, want 5", len(items))
	}
	got := make(map[string]model.NewsItem, len(items))
	for _, item := range items {
		got[item.URL] = item
	}
	checks := []struct {
		url         string
		title       string
		date        string
		publishedAt string
		typ         string
	}{
		{gsnewsFixtureBaseURL + "/info/1587/1001.htm", "首页新闻一", "2026-08-27", "2026-08-27", "热点新闻"},
		{gsnewsFixtureBaseURL + "/info/1637/1002.htm", "培养通知一", "2026-08-26", "2026-08-26", "培养工作"},
		{gsnewsFixtureBaseURL + "/info/1647/1003.htm", "学位通知一", "2026-08-25", "2026-08-25", "学位工作"},
		{gsnewsFixtureBaseURL + "/info/1657/1004.htm", "实践通知一", "2026-08-24", "2026-08-24", "实习实践"},
		{gsnewsFixtureBaseURL + "/info/1677/1005.htm", "综合信息一", "2026-08-23", "2026-08-23", "综合信息"},
	}
	for _, want := range checks {
		item, ok := got[want.url]
		if !ok {
			t.Fatalf("home item %q was not parsed", want.url)
		}
		if item.Title != want.title || item.Date != want.date || item.PublishedAt != want.publishedAt || item.Type != want.typ {
			t.Errorf("item %s = (%q, %q, %q, %q), want (%q, %q, %q, %q)", want.url, item.Title, item.Date, item.PublishedAt, item.Type, want.title, want.date, want.publishedAt, want.typ)
		}
	}
}

func TestGsnewsListRejectsResourcesAndPreservesTypeAndDate(t *testing.T) {
	items, err := parseAdapter_gsnewsList(readGsnewsFixture(t, "gsnews_list.html"), gsnewsFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("list item count = %d, want 3", len(items))
	}
	if items[0].URL != gsnewsFixtureBaseURL+"/info/1637/2001.htm" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31" || items[0].Type != "通知公告" {
		t.Errorf("first item = %#v", items[0])
	}
	if items[1].URL != gsnewsFixtureBaseURL+"/info/1637/2002.htm" || items[1].Date != "2026-08-30" {
		t.Errorf("second item = %#v", items[1])
	}
	if items[2].URL != gsnewsFixtureBaseURL+"/info/1637/2003.htm" || items[2].Date != "2026-08-29" {
		t.Errorf("quoted title item = %#v", items[2])
	}
}

func TestGsnewsEmptyListIsAcceptedAndErrorShellIsRejected(t *testing.T) {
	items, err := parseAdapter_gsnewsList(readGsnewsFixture(t, "gsnews_empty.html"), gsnewsFixtureBaseURL)
	if err != nil {
		t.Fatalf("empty list returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list returned %d items", len(items))
	}
	if _, err := parseAdapter_gsnewsList(readGsnewsFixture(t, "gsnews_error.html"), gsnewsFixtureBaseURL); err == nil {
		t.Fatal("error shell was accepted as a list")
	}
}

func TestGsnewsArticleParsesMetadataContentImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_gsnewsArticle(readGsnewsFixture(t, "gsnews_article.html"), gsnewsFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于研究生管理信息系统升级的通知" {
		t.Errorf("title = %q", article.Title)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Errorf("date metadata = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Type != "培养工作" {
		t.Errorf("type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "正文第一段") || !strings.Contains(article.Content, "正文第二段") {
		t.Errorf("content = %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != gsnewsFixtureBaseURL+"/images/body.png" {
		t.Errorf("images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="`+gsnewsFixtureBaseURL+`/images/body.png"`) {
		t.Errorf("content HTML did not resolve image: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 3 {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	attachments := make(map[string]string, len(article.Attachments))
	for _, attachment := range article.Attachments {
		attachments[attachment.Name] = attachment.URL
	}
	if attachments["正文附件.pdf"] != gsnewsFixtureBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&wbfileid=PDF1" {
		t.Errorf("PDF attachment = %#v", attachments)
	}
	if attachments["正文中的表格.xlsx"] != gsnewsFixtureBaseURL+"/__local/table.xlsx?e=.xlsx" {
		t.Errorf("XLSX attachment = %#v", attachments)
	}
	if attachments["正文外部附件.docx"] != gsnewsFixtureBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&wbfileid=DOC1" {
		t.Errorf("section attachment = %#v", attachments)
	}
}

func TestGsnewsArticleRejectsErrorShell(t *testing.T) {
	if _, err := parseAdapter_gsnewsArticle(readGsnewsFixture(t, "gsnews_error.html"), gsnewsFixtureBaseURL); err == nil {
		t.Fatal("error shell was accepted as an article")
	}
}
