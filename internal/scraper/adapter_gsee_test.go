package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const gseeFixtureBaseURL = "https://gsee.example.edu"

func TestGseeAdapterRegistrationAndRoutes(t *testing.T) {
	site := AllSites["gsee"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("gsee adapter hooks are incomplete: %#v", site)
	}
	if site.BaseURL != gseeCanonicalBaseURL {
		t.Fatalf("gsee base URL = %q, want %q", site.BaseURL, gseeCanonicalBaseURL)
	}
	if !reflect.DeepEqual(site.SubPages, gseeSubPages) {
		t.Fatalf("gsee sub-pages = %#v, want %#v", site.SubPages, gseeSubPages)
	}
	for _, resource := range gseeResourcePages {
		for _, feed := range site.SubPages {
			if feed == resource {
				t.Fatalf("resource route %q was registered as an ordinary feed", resource)
			}
		}
	}
}

func TestGseePageURLUsesReverseGeneratedPages(t *testing.T) {
	tests := []struct {
		name       string
		slug       string
		page       int
		totalPages int
		want       string
	}{
		{name: "first page", slug: "xwzx/xwdt", page: 1, totalPages: 131, want: gseeFixtureBaseURL + "/xwzx/xwdt.htm"},
		{name: "second page", slug: "xwzx/xwdt", page: 2, totalPages: 131, want: gseeFixtureBaseURL + "/xwzx/xwdt/130.htm"},
		{name: "middle page", slug: "xwzx/xwdt", page: 66, totalPages: 131, want: gseeFixtureBaseURL + "/xwzx/xwdt/66.htm"},
		{name: "last page", slug: "xwzx/xwdt", page: 131, totalPages: 131, want: gseeFixtureBaseURL + "/xwzx/xwdt/1.htm"},
		{name: "short alias", slug: "xwdt", page: 2, totalPages: 131, want: gseeFixtureBaseURL + "/xwzx/xwdt/130.htm"},
		{name: "one page", slug: "xygz/tzgg", page: 1, totalPages: 1, want: gseeFixtureBaseURL + "/xygz/tzgg.htm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gseePageURL(gseeFixtureBaseURL+"/", tt.slug, tt.page, tt.totalPages, "htm"); got != tt.want {
				t.Fatalf("gseePageURL(%q, %d/%d) = %q, want %q", tt.slug, tt.page, tt.totalPages, got, tt.want)
			}
		})
	}
}

func TestGseeHomepageIncludesVisibleHiddenAndAcademicModules(t *testing.T) {
	items, err := parseGseeNews(readGseeFixture(t, "gsee_home.html"), gseeFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse gsee homepage: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d homepage articles, want four unique local articles: %#v", len(items), items)
	}

	want := map[string]struct {
		typ         string
		date        string
		publishedAt string
	}{
		"轮播文章标题":   {typ: "新闻动态"},
		"公告文章完整标题": {typ: "本科教学", date: "2026-08-01"},
		"党务文章完整标题": {typ: "党政办公", date: "07-31"},
		"学术活动完整标题": {typ: "学术信息", date: "07-16"},
	}
	for _, item := range items {
		expected, ok := want[item.Title]
		if !ok {
			t.Errorf("unexpected homepage item: %#v", item)
			continue
		}
		if item.Type != expected.typ || item.Date != expected.date || item.PublishedAt != expected.publishedAt {
			t.Errorf("homepage item %q = (type=%q,date=%q,published=%q), want %#v", item.Title, item.Type, item.Date, item.PublishedAt, expected)
		}
		if !strings.HasPrefix(item.URL, gseeFixtureBaseURL+"/info/") {
			t.Errorf("non-local article leaked into homepage items: %#v", item)
		}
	}
}

func TestGseeListScopesRowsAndPreservesPreciseTimestamp(t *testing.T) {
	items, err := parseGseeNews(readGseeFixture(t, "gsee_list.html"), gseeFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse gsee list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want two local articles: %#v", len(items), items)
	}
	if items[0].Title != "带精确时间的列表文章" || items[0].Type != "新闻动态" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("unexpected precise list item: %#v", items[0])
	}
	if items[1].Title != "列表通知文章" || items[1].Type != "通知公告" || items[1].Date != "2026-08-30" {
		t.Fatalf("unexpected second list item: %#v", items[1])
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, gseeFixtureBaseURL+"/info/") {
			t.Errorf("navigation/external URL leaked into list: %#v", item)
		}
	}
}

func TestGseeArticleParsesMetadataBodyImagesAndAttachments(t *testing.T) {
	article, err := parseGseeArticle(readGseeFixture(t, "gsee_article.html"), gseeFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse gsee article: %v", err)
	}
	if article.Title != "地学学院详情测试通知" || article.Type != "新闻动态" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("unexpected article metadata: %+v", article)
	}
	if !strings.Contains(article.Content, "正文段落") || strings.Contains(article.Content, "侧栏推荐") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != gseeFixtureBaseURL+"/__local/article.jpg" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "测试文件.pdf" || article.Attachments[0].URL != gseeFixtureBaseURL+"/system/_content/download.jsp?id=fixture" {
		t.Fatalf("unexpected article attachments: %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, gseeFixtureBaseURL+"/__local/article.jpg") {
		t.Fatalf("content HTML did not retain resolved image: %q", article.ContentHTML)
	}
}

func TestGseeAcademicArticleAllowsImageOnlyContent(t *testing.T) {
	article, err := parseGseeArticle(readGseeFixture(t, "gsee_academic_article.html"), gseeFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse gsee academic article: %v", err)
	}
	if article.Title != "地震滑坡级联灾害学术报告" || article.Type != "学术信息" || article.Date != "" {
		t.Fatalf("unexpected academic article metadata: %+v", article)
	}
	if article.PublishedAt != "" || len(article.Images) != 1 || article.Images[0] != gseeFixtureBaseURL+"/__local/event.jpg" {
		t.Fatalf("unexpected academic date/image fields: %+v", article)
	}
}

func TestGseeDistinguishesEmptyStaticResourceAndVerificationPages(t *testing.T) {
	empty, err := parseGseeNews(readGseeFixture(t, "gsee_empty.html"), gseeFixtureBaseURL)
	if err != nil {
		t.Fatalf("reachable empty list should parse: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty list returned %d items", len(empty))
	}

	static := readGseeFixture(t, "gsee_static.html")
	if _, err := parseGseeNews(static, gseeFixtureBaseURL); err == nil || !strings.Contains(err.Error(), "static/resource") {
		t.Fatalf("static list error = %v", err)
	}
	if _, err := parseGseeArticle(static, gseeFixtureBaseURL); err == nil || !strings.Contains(err.Error(), "pageType") {
		t.Fatalf("static article error = %v", err)
	}

	resource := readGseeFixture(t, "gsee_resource.html")
	if _, err := parseGseeArticle(resource, gseeFixtureBaseURL); err == nil || !strings.Contains(err.Error(), "resource page") {
		t.Fatalf("resource article error = %v", err)
	}

	verification := readGseeFixture(t, "gsee_captcha.html")
	if _, err := parseGseeNews(verification, gseeFixtureBaseURL); err == nil {
		t.Fatal("verification page was accepted as a list")
	}
	if _, err := parseGseeArticle(verification, gseeFixtureBaseURL); err == nil {
		t.Fatal("verification page was accepted as an article")
	}
}

func readGseeFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
