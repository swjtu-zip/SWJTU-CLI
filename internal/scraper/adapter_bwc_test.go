package scraper

import (
	"os"
	"strings"
	"testing"
)

const bwcFixtureBase = "https://bwc.swjtu.edu.cn"

func readBWCFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read BWC fixture %s: %v", name, err)
	}
	return data
}

func TestBwcAdapterRegistrationAndFeeds(t *testing.T) {
	site := AllSites["bwc"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil {
		t.Fatalf("BWC adapter hooks are incomplete: %#v", site)
	}

	want := []string{
		"tzgg", "gzdt", "aqjs", "aqjs/aqjy", "aqjs/jsal", "djzj/xxzj",
		"zcfg/flfg", "zcfg/gzzd", "fwzn/xzzx", "English/About",
		"English/Party_s_Works", "English/Party_s_Works/Honest_Administration",
		"English/Laws", "English/Rules", "English/Announcement",
		"English/Service_Guide", "English/Service_Guide/Work_Processes",
	}
	got := make(map[string]bool, len(site.SubPages))
	for _, slug := range site.SubPages {
		got[slug] = true
	}
	for _, slug := range want {
		if !got[slug] {
			t.Errorf("BWC feed %q is not registered; feeds=%#v", slug, site.SubPages)
		}
	}
	for _, slug := range []string{"jgsz/bmgk", "bszn/hzyw", "zcfg", "English/News", "English/Party_s_Works/Party_Branch_Works"} {
		if got[slug] {
			t.Errorf("non-feed/static or empty page %q was registered as a feed", slug)
		}
	}
}

func TestBwcPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	tests := []struct {
		slug, want  string
		page, total int
	}{
		{slug: "tzgg", page: 1, total: 87, want: bwcFixtureBase + "/tzgg.htm"},
		{slug: "tzgg", page: 2, total: 87, want: bwcFixtureBase + "/tzgg/86.htm"},
		{slug: "tzgg", page: 44, total: 87, want: bwcFixtureBase + "/tzgg/44.htm"},
		{slug: "tzgg", page: 86, total: 87, want: bwcFixtureBase + "/tzgg/2.htm"},
		{slug: "tzgg", page: 87, total: 87, want: bwcFixtureBase + "/tzgg/1.htm"},
		{slug: "English/Announcement", page: 2, total: 4, want: bwcFixtureBase + "/English/Announcement/3.htm"},
	}
	for _, tt := range tests {
		if got := bwcPageURL(bwcFixtureBase, tt.slug, tt.page, tt.total, ".htm"); got != tt.want {
			t.Errorf("bwcPageURL(%q, %d/%d) = %q, want %q", tt.slug, tt.page, tt.total, got, tt.want)
		}
	}
}

func TestBwcHomepageScopesOnlyRealTabs(t *testing.T) {
	items, err := parseAdapter_bwcList(readBWCFixture(t, "bwc_home.html"), bwcFixtureBase)
	if err != nil {
		t.Fatalf("parse BWC homepage: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("homepage items = %d, want five scoped module items: %#v", len(items), items)
	}

	wantTypes := map[string]string{
		"工作动态文章": "工作动态",
		"通知公告文章": "通知公告",
		"安全警示文章": "安全警示",
		"政策法规文章": "政策法规",
		"下载专区文章": "下载专区",
	}
	for _, item := range items {
		if want, ok := wantTypes[item.Title]; !ok || item.Type != want {
			t.Errorf("homepage item type mismatch: %#v", item)
		}
		if strings.Contains(item.URL, "/1080/") || strings.Contains(item.URL, "/1079/") {
			t.Errorf("carousel/recommendation URL leaked into homepage feed: %#v", item)
		}
	}
	if got := bwcTotalPages(readBWCFixture(t, "bwc_home.html")); got != 1 {
		t.Fatalf("homepage total pages = %d, want 1", got)
	}
}

func TestBwcChineseListAndPagination(t *testing.T) {
	data := readBWCFixture(t, "bwc_list.html")
	items, err := parseAdapter_bwcList(data, bwcFixtureBase)
	if err != nil {
		t.Fatalf("parse BWC Chinese list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Chinese list items = %d, want 2: %#v", len(items), items)
	}
	if items[0].Title != "第一条通知公告" || items[0].Date != "2026-08-31" || items[0].Type != "通知公告" {
		t.Errorf("first Chinese list item = %#v", items[0])
	}
	if items[1].URL != bwcFixtureBase+"/info/1013/8845.htm" {
		t.Errorf("second Chinese list URL = %q", items[1].URL)
	}
	if got := bwcTotalPages(data); got != 87 {
		t.Fatalf("Chinese total pages = %d, want 87", got)
	}
}

func TestBwcEnglishListPreservesSecondPrecision(t *testing.T) {
	data := readBWCFixture(t, "bwc_english_list.html")
	items, err := parseAdapter_bwcList(data, bwcFixtureBase)
	if err != nil {
		t.Fatalf("parse BWC English list: %v", err)
	}
	if len(items) != 2 || items[0].Type != "Announcement" {
		t.Fatalf("English list items = %#v", items)
	}
	if items[0].Date != "2018-01-18" || items[0].PublishedAt != "2018-01-18 10:31:38" {
		t.Errorf("English timestamp not preserved: %#v", items[0])
	}
	if got := bwcTotalPages(data); got != 4 {
		t.Fatalf("English total pages = %d, want 4", got)
	}
}

func TestBwcArticleExtractsMetadataContentImagesAndExternalAttachments(t *testing.T) {
	article, err := parseAdapter_bwcArticle(readBWCFixture(t, "bwc_article.html"), bwcFixtureBase)
	if err != nil {
		t.Fatalf("parse BWC article: %v", err)
	}
	if article.Title != "关于开展安全工作的通知" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 09:27:00" {
		t.Errorf("article metadata = %#v", article)
	}
	if article.Type != "通知公告" || article.Author != "admin" {
		t.Errorf("article type/author = %q/%q", article.Type, article.Author)
	}
	if !strings.Contains(article.Content, "正文第一段") || !strings.Contains(article.Content, "校外车辆及人员来访申请表.docx") {
		t.Errorf("article body is incomplete: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != bwcFixtureBase+"/images/body.jpg" {
		t.Errorf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v, want body and outside-body files", article.Attachments)
	}
	if article.Attachments[0].Name != "校外车辆及人员来访申请表.docx" || article.Attachments[0].URL != bwcFixtureBase+"/XW0001.docx" {
		t.Errorf("body attachment = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "外部附件.pdf" || article.Attachments[1].URL != bwcFixtureBase+"/files/outside.pdf" {
		t.Errorf("outside attachment = %#v", article.Attachments[1])
	}
	if !strings.Contains(article.ContentHTML, "https://bwc.swjtu.edu.cn/images/body.jpg") || !strings.Contains(article.ContentHTML, "https://bwc.swjtu.edu.cn/XW0001.docx") {
		t.Errorf("resolved resources missing from ContentHTML: %s", article.ContentHTML)
	}
}

func TestBwcRejectsShellsAndUnscopedPages(t *testing.T) {
	if _, err := parseAdapter_bwcArticle(readBWCFixture(t, "bwc_home.html"), bwcFixtureBase); err == nil {
		t.Fatal("homepage shell was accepted as an article")
	}
	if _, err := parseAdapter_bwcList(readBWCFixture(t, "bwc_article.html"), bwcFixtureBase); err == nil {
		t.Fatal("article detail was accepted as a list")
	}
	if _, err := parseAdapter_bwcList([]byte(`<html><body><div id="app"></div></body></html>`), bwcFixtureBase); err == nil {
		t.Fatal("empty SPA shell was accepted as an empty list")
	}
}
