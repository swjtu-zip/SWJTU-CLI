package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const fdcFixtureBaseURL = "https://fdc.swjtu.edu.cn"

func readFdcFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fdc fixture %s: %v", name, err)
	}
	return data
}

func TestFdcAdapterRegistrationAndRoutes(t *testing.T) {
	site := AllSites["fdc"]
	if site == nil || site.BaseURL != fdcFixtureBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("fdc adapter hooks are incomplete: %#v", site)
	}
	want := []string{
		"tzgg", "xwdt", "llxx", "sdby", "sdby/rybz", "sdby/yrgs",
		"gzzd", "gzzd/sjwj", "gzzd/xxwj", "sdjd", "sdjd/dxal",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("fdc routes = %#v, want %#v", site.SubPages, want)
	}
	for _, excluded := range []string{"bmjj", "bmjj/gzzz", "bmjj/jgsz", "bmjj/gzry", "sdjd/jbxz"} {
		for _, route := range site.SubPages {
			if route == excluded {
				t.Errorf("static route registered as feed: %q", excluded)
			}
		}
	}
}

func TestFdcPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	tests := []struct {
		slug, want  string
		page, total int
	}{
		{slug: "tzgg", page: 1, total: 3, want: fdcFixtureBaseURL + "/tzgg.htm"},
		{slug: "tzgg", page: 2, total: 3, want: fdcFixtureBaseURL + "/tzgg/2.htm"},
		{slug: "tzgg", page: 3, total: 3, want: fdcFixtureBaseURL + "/tzgg/1.htm"},
		{slug: "xwdt", page: 4, total: 7, want: fdcFixtureBaseURL + "/xwdt/4.htm"},
		{slug: "gzzd/sjwj", page: 2, total: 2, want: fdcFixtureBaseURL + "/gzzd/sjwj/1.htm"},
		{slug: "index", page: 1, total: 1, want: fdcFixtureBaseURL + "/index.htm"},
	}
	for _, tt := range tests {
		if got := fdcPageURL(fdcFixtureBaseURL, tt.slug, tt.page, tt.total, ".htm"); got != tt.want {
			t.Errorf("fdcPageURL(%q, %d/%d) = %q, want %q", tt.slug, tt.page, tt.total, got, tt.want)
		}
	}
}

func TestFdcHomepageScopesAllNewsPanels(t *testing.T) {
	items, err := parseAdapter_fdcList(readFdcFixture(t, "fdc_home.html"), fdcFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("homepage items = %d, want five internal entries: %#v", len(items), items)
	}
	wantTypes := map[string]int{"通知公告": 2, "新闻动态": 1, "理论学习": 2}
	gotTypes := make(map[string]int)
	for _, item := range items {
		gotTypes[item.Type]++
		if strings.Contains(item.URL, "news.swjtu.edu.cn") || strings.Contains(item.URL, "www.swjtu.edu.cn") {
			t.Errorf("carousel/friend link leaked into homepage feed: %#v", item)
		}
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("homepage types = %#v, want %#v", gotTypes, wantTypes)
	}
	if items[2].Title != "我校参加教师队伍建设重点工作推进会" {
		t.Fatalf("internal news item was not retained: %#v", items[2])
	}
	if items[3].Title != "习近平给四所交通大学全体师生的回信" {
		t.Fatalf("hidden theory tab was not parsed: %#v", items[3])
	}
}

func TestFdcListParsesFirstMiddleLastAndExternalEntries(t *testing.T) {
	cases := []struct {
		fixture string
		page    int
		count   int
		wantURL string
	}{
		{fixture: "fdc_tzgg_page1.html", page: 1, count: 2, wantURL: fdcFixtureBaseURL + "/info/1118/14863.htm"},
		{fixture: "fdc_tzgg_page2.html", page: 2, count: 1, wantURL: fdcFixtureBaseURL + "/info/1118/10493.htm"},
		{fixture: "fdc_tzgg_page3.html", page: 3, count: 1, wantURL: fdcFixtureBaseURL + "/info/1118/11023.htm"},
	}
	for _, tc := range cases {
		data := readFdcFixture(t, tc.fixture)
		items, err := parseAdapter_fdcList(data, fdcFixtureBaseURL)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.fixture, err)
		}
		if len(items) != tc.count {
			t.Fatalf("page %d items = %#v", tc.page, items)
		}
		if items[0].URL != tc.wantURL || items[0].Type != "通知公告" || items[0].Date == "" {
			t.Fatalf("page %d item = %#v", tc.page, items[0])
		}
		totalItems, currentPage, totalPages, ok := fdcPaginationFromHTML(data)
		if !ok || totalItems != 27 || currentPage != tc.page || totalPages != 3 {
			t.Fatalf("page %d pagination = %d/%d/%d/%v", tc.page, totalItems, currentPage, totalPages, ok)
		}
	}
}

func TestFdcEmptyListIsNotStaticContent(t *testing.T) {
	items, err := parseAdapter_fdcList(readFdcFixture(t, "fdc_empty.html"), fdcFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty list = %#v, want non-nil empty slice", items)
	}
	if got := fdcTotalPagesFromHTML(readFdcFixture(t, "fdc_empty.html")); got != 0 {
		t.Fatalf("empty list total pages = %d, want 0", got)
	}
	if _, err := parseAdapter_fdcList(readFdcFixture(t, "fdc_static.html"), fdcFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as an article list")
	}
}

func TestFdcArticleParsesMetadataBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_fdcArticle(readFdcFixture(t, "fdc_article.html"), fdcFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于测试正文、图片和附件的通知" || article.Type != "通知公告" || article.Source != "党委教师工作部" || article.Author != "张三" || article.Editor != "李四" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("article date = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "这是正文内容") || len(article.Images) != 1 || article.Images[0] != fdcFixtureBaseURL+"/__local/body.png" {
		t.Fatalf("article body/images = %#v", article)
	}
	if len(article.Attachments) != 2 || article.Attachments[0].Name != "附件：测试表.pdf" || !strings.Contains(article.Attachments[1].URL, "download.jsp") {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, fdcFixtureBaseURL+"/__local/body.png") {
		t.Fatalf("content HTML did not resolve image URL: %s", article.ContentHTML)
	}
}

func TestFdcRejectsNonArticleAndVerificationShells(t *testing.T) {
	if _, err := parseAdapter_fdcArticle(readFdcFixture(t, "fdc_static.html"), fdcFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as an article")
	}
	if _, err := parseAdapter_fdcArticle(readFdcFixture(t, "fdc_captcha.html"), fdcFixtureBaseURL); err == nil {
		t.Fatal("captcha HTML was accepted as an article")
	}
	if _, err := parseAdapter_fdcList(readFdcFixture(t, "fdc_captcha.html"), fdcFixtureBaseURL); err == nil {
		t.Fatal("captcha HTML was accepted as a list")
	}
}

func TestFdcDateValuesPreserveMinutePrecision(t *testing.T) {
	if date, publishedAt := fdcDateValues("日期：2026-08-31"); date != "2026-08-31" || publishedAt != "" {
		t.Fatalf("date-only value = %q/%q", date, publishedAt)
	}
	if date, publishedAt := fdcDateValues("日期：2026-08-31 14:34"); date != "2026-08-31" || publishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("precise value = %q/%q", date, publishedAt)
	}
}
