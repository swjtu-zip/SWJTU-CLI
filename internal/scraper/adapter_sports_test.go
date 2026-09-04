package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const sportsFixtureBaseURL = "https://sports.swjtu.edu.cn"

func readSportsFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read sports fixture %q: %v", name, err)
	}
	return body
}

func TestSportsAdapterRegistersOnlyAuditedArticleFeeds(t *testing.T) {
	want := []string{
		"xwdt",
		"tzggnew/qb", "tzggnew/dzbg", "tzggnew/jyjx", "tzggnew/qtjs",
		"tzggnew/xsgz", "tzggnew/jyzp", "tzggnew/gsgg",
		"dqgzNew/tzgz", "dqgzNew/ghgz", "dqgzNew/ltgz",
		"rcpyNew/ggtyjy/jkcs", "rcpyNew/ggtyjy/tzcs",
		"xsgzNew/txhd", "xsgzNew/zdwj",
		"xytyNew/qtjs", "xytyNew/tyst/sthd", "ydxy",
		"zsjyNew/zsgz", "zsjyNew/jygz",
	}
	site := AllSites["sports"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("sports adapter hooks are not fully registered")
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("sports feeds = %#v, want %#v", site.SubPages, want)
	}
	if site.BaseURL != sportsCanonicalBaseURL {
		t.Fatalf("sports base URL = %q, want %q", site.BaseURL, sportsCanonicalBaseURL)
	}
}

func TestSportsPageURLUsesReverseNumberingAndRejectsOutOfRange(t *testing.T) {
	tests := []struct {
		slug       string
		page       int
		totalPages int
		want       string
	}{
		{"xwdt", 1, 22, sportsFixtureBaseURL + "/xwdt.htm"},
		{"xwdt", 2, 22, sportsFixtureBaseURL + "/xwdt/21.htm"},
		{"xwdt", 11, 22, sportsFixtureBaseURL + "/xwdt/12.htm"},
		{"xwdt", 22, 22, sportsFixtureBaseURL + "/xwdt/1.htm"},
		{"tzggnew/qb", 2, 46, sportsFixtureBaseURL + "/tzggnew/qb/45.htm"},
		{"tzggnew/jyzp", 2, 1, ""},
		{"xwdt", 23, 22, ""},
		{"xwdt", 2, 0, ""},
		{"xwdt", 0, 22, ""},
		{"not-a-feed", 1, 1, ""},
		{"index", 1, 1, sportsFixtureBaseURL + "/"},
		{"index", 2, 1, ""},
	}
	for _, test := range tests {
		if got := sportsPageURL(sportsFixtureBaseURL, test.slug, test.page, test.totalPages, ".htm"); got != test.want {
			t.Errorf("sports page URL %s page %d/%d = %q, want %q", test.slug, test.page, test.totalPages, got, test.want)
		}
	}
}

func TestSportsLiveListFixturesStayInsideTheSelectedFeed(t *testing.T) {
	for _, test := range []struct {
		fixture  string
		wantURL  string
		wantDate string
	}{
		{"sports_list_first_live.html", sportsFixtureBaseURL + "/info/1872/26342.htm", "2026-09-03"},
		{"sports_list_middle_live.html", sportsFixtureBaseURL + "/info/1872/16692.htm", "2020-06-18"},
		{"sports_list_last_live.html", sportsFixtureBaseURL + "/info/1872/14622.htm", "2018-04-12"},
	} {
		items, err := parseAdapter_sportsList(readSportsFixture(t, test.fixture), sportsFixtureBaseURL)
		if err != nil {
			t.Fatalf("%s: %v", test.fixture, err)
		}
		if len(items) != 1 {
			t.Fatalf("%s returned %d items, want only the main-list row: %#v", test.fixture, len(items), items)
		}
		if items[0].URL != test.wantURL || items[0].Date != test.wantDate || items[0].PublishedAt != test.wantDate || items[0].Type != "新闻动态" {
			t.Fatalf("%s item = %#v", test.fixture, items[0])
		}
		if strings.Contains(items[0].URL, "mp.weixin") || strings.Contains(items[0].Title, "侧栏") {
			t.Fatalf("%s retained a sidebar/external row: %#v", test.fixture, items[0])
		}
	}
}

func TestSportsHomepageParsesEveryRenderedNotificationTab(t *testing.T) {
	items, err := parseAdapter_sportsList(readSportsFixture(t, "sports.html"), sportsFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("sports homepage returned no local items")
	}
	seenTypes := make(map[string]bool)
	for _, item := range items {
		seenTypes[item.Type] = true
		if strings.Contains(item.URL, "mp.weixin.qq.com") || strings.Contains(item.URL, "faculty.swjtu.edu.cn") {
			t.Fatalf("homepage retained an external card: %#v", item)
		}
		if strings.Contains(item.Title, "习近平给四所交通大学全体师生") {
			t.Fatalf("homepage retained the static banner card: %#v", item)
		}
	}
	for _, typ := range []string{"新闻动态", "全部", "党政办公", "教育教学", "学生工作", "群体竞赛", "就业招聘", "公示公告"} {
		if !seenTypes[typ] {
			t.Errorf("homepage did not parse notification type %q; types=%v", typ, seenTypes)
		}
	}
	if seenTypes["体育学院"] {
		t.Fatal("homepage site title was copied as every item's type")
	}
}

func TestSportsEmptyListIsValidButStaticAndExternalShellsAreNot(t *testing.T) {
	empty, err := parseAdapter_sportsList(readSportsFixture(t, "sports_empty_list.html"), sportsFixtureBaseURL)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty real list = %#v, %v; want empty success", empty, err)
	}
	if got, err := sportsTotalPagesFromHTML(readSportsFixture(t, "sports_empty_list.html"), "tzggnew/jyzp"); err != nil || got != 1 {
		t.Fatalf("empty real list pages = %d/%v, want one page", got, err)
	}
	for _, fixture := range []string{"sports_static_shell.html", "sports_external_only.html"} {
		if items, err := parseAdapter_sportsList(readSportsFixture(t, fixture), sportsFixtureBaseURL); err == nil || items != nil {
			t.Errorf("%s accepted as a local feed: items=%#v err=%v", fixture, items, err)
		}
	}
}

func TestSportsPaginationFixtureReportsSourceTotal(t *testing.T) {
	first := readSportsFixture(t, "sports_list_first_live.html")
	if got, err := sportsTotalPagesFromHTML(first, "xwdt"); err != nil || got != 22 {
		t.Fatalf("first page total = %d/%v, want 22", got, err)
	}
	if got, err := sportsTotalPagesFromHTML(readSportsFixture(t, "sports_list_middle_live.html"), "xwdt"); err != nil || got != 22 {
		t.Fatalf("middle page total = %d/%v, want 22", got, err)
	}
	if got, err := sportsTotalPagesFromHTML(readSportsFixture(t, "sports_list_last_live.html"), "xwdt"); err != nil || got != 22 {
		t.Fatalf("last page total = %d/%v, want 22", got, err)
	}
}

func TestSportsArticleUsesFixedMetadataBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_sportsArticle(readSportsFixture(t, "sports_article_live.html"), sportsFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于2026-2027学年体育学院研究生会换届结果公示" || article.Type != "全部" || article.Source != "体育学院" || article.Date != "2026-07-13" || article.PublishedAt != "2026-07-13" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "正文首段") || strings.Contains(article.Content, "侧栏推荐") {
		t.Fatalf("article body was not limited to the content container: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != sportsFixtureBaseURL+"/__local/body.jpg" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	wantAttachments := map[string]string{
		"外部附件.pdf":        "https://external.example/files/external.pdf",
		"体育学院本科生体测细则.pdf": sportsFixtureBaseURL + "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1788604684&wbfileid=SPORTS-FIXTURE",
	}
	for _, attachment := range article.Attachments {
		if wantAttachments[attachment.Name] != attachment.URL {
			t.Errorf("unexpected attachment = %#v", attachment)
		}
		delete(wantAttachments, attachment.Name)
	}
	if len(wantAttachments) != 0 {
		t.Errorf("missing attachments = %#v", wantAttachments)
	}
	if strings.Contains(article.ContentHTML, "bad.pdf") || strings.Contains(article.ContentHTML, "captcha") || strings.Contains(article.ContentHTML, "data:image") {
		t.Fatalf("pseudo attachment/image leaked into body HTML: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, "external.example/article.html") {
		t.Fatal("ordinary external body link was incorrectly removed")
	}
}

func TestSportsArticleKeepsClockPrecisionAndRejectsErrorShells(t *testing.T) {
	article, err := parseAdapter_sportsArticle(readSportsFixture(t, "sports_article_precise.html"), sportsFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-09-03" || article.PublishedAt != "2026-09-03 14:05:07" {
		t.Fatalf("precise article date = %q/%q", article.Date, article.PublishedAt)
	}
	if article.Type != "新闻动态" {
		t.Fatalf("precise article type = %q", article.Type)
	}
	for _, fixture := range []string{"sports_waf.html", "sports_spa.html", "sports_static_shell.html"} {
		if article, err := parseAdapter_sportsArticle(readSportsFixture(t, fixture), sportsFixtureBaseURL); err == nil || article != nil {
			t.Errorf("%s accepted as an article: article=%#v err=%v", fixture, article, err)
		}
	}
}
