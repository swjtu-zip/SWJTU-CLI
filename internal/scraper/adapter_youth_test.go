package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const youthTestBaseURL = "https://youth.swjtu.edu.cn"

func readYouthFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestYouthAdapterRegistered(t *testing.T) {
	site := AllSites["youth"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("youth adapter hooks are not registered")
	}
	if site.Name != "校团委" || site.BaseURL != youthTestBaseURL || site.Category != "party" {
		t.Fatalf("youth registry entry = %#v", site)
	}
	want := []string{"tqkx1", "tzgg", "twfw", "jctj", "xszz", "qncm", "tgjyz", "xbjh"}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("youth subpages = %#v, want %#v", site.SubPages, want)
	}
	if site.PageURL == nil {
		t.Fatal("youth pagination hook is not registered")
	}
	for _, tc := range []struct {
		slug        string
		page, total int
		want        string
	}{
		{"tzgg", 1, 132, youthTestBaseURL + "/tzgg.htm"},
		{"tzgg", 2, 132, youthTestBaseURL + "/tzgg/131.htm"},
		{"tzgg", 132, 132, youthTestBaseURL + "/tzgg/1.htm"},
		{"index", 1, 0, youthTestBaseURL + "/index.htm"},
		{"tzgg", 0, 132, ""},
		{"tzgg", 133, 132, ""},
		{"tzgg", 2, 0, ""},
	} {
		if got := site.PageURL(site.BaseURL, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("youth page %s %d/%d = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestYouthListParsesRowsDatesAndPagination(t *testing.T) {
	body := readYouthFixture(t, "youth_list.html")
	items, err := parseAdapter_youthList(body, youthTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 10 {
		t.Fatalf("items = %d, want 10", len(items))
	}
	first := items[0]
	if first.Title != "关于四川省青年马克思主义者培养工程第九届高校班学员拟推荐名单的公示" {
		t.Fatalf("first title = %q", first.Title)
	}
	if first.URL != youthTestBaseURL+"/info/1191/179491.htm" {
		t.Fatalf("first URL = %q", first.URL)
	}
	if first.Date != "2026-09-04" || first.PublishedAt != "2026-09-04" {
		t.Fatalf("first dates = (%q, %q)", first.Date, first.PublishedAt)
	}
	if first.Type != "通知公告" {
		t.Fatalf("first type = %q", first.Type)
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.URL] {
			t.Fatalf("duplicate item URL %q", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, youthTestBaseURL+"/info/") {
			t.Fatalf("non-article URL %q", item.URL)
		}
		if item.Date == "" || item.Type != "通知公告" {
			t.Fatalf("item missing date or type: %#v", item)
		}
	}
	if got := parseTotalPages(body); got != 132 {
		t.Fatalf("total pages = %d, want 132", got)
	}
}

func TestYouthSinglePageListReportsOnePage(t *testing.T) {
	body := readYouthFixture(t, "youth_list_single_page.html")
	items, err := parseAdapter_youthList(body, youthTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("items = %d, want 6", len(items))
	}
	if items[0].Type != "青年传媒" {
		t.Fatalf("first type = %q", items[0].Type)
	}
	if got := parseTotalPages(body); got != 1 {
		t.Fatalf("total pages = %d, want 1", got)
	}
}

func TestYouthHomepageParsesModules(t *testing.T) {
	body := readYouthFixture(t, "youth_home.html")
	items, err := parseAdapter_youthList(body, youthTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 40 {
		t.Fatalf("homepage items = %d, want at least 40", len(items))
	}
	byURL := make(map[string]string, len(items))
	for _, item := range items {
		if !strings.HasPrefix(item.URL, youthTestBaseURL+"/info/") {
			t.Fatalf("external or non-article URL accepted: %q", item.URL)
		}
		byURL[item.URL] = item.Type
	}
	for url, typ := range map[string]string{
		youthTestBaseURL + "/info/1281/179421.htm": "团情快讯",
		youthTestBaseURL + "/info/1191/179491.htm": "通知公告",
		youthTestBaseURL + "/info/1211/179471.htm": "基层团建",
		youthTestBaseURL + "/info/1231/179141.htm": "学生组织",
		youthTestBaseURL + "/info/1291/172381.htm": "团干加油站",
		youthTestBaseURL + "/info/1241/41731.htm":  "青年传媒",
		youthTestBaseURL + "/info/1381/170741.htm": "西部计划",
	} {
		if got, ok := byURL[url]; !ok || got != typ {
			t.Errorf("homepage item %s type = %q (present=%v), want %q", url, got, ok, typ)
		}
	}
}

func TestYouthArticleParsesMetadataAndBody(t *testing.T) {
	body := readYouthFixture(t, "youth_article.html")
	article, err := parseAdapter_youthArticle(body, youthTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于四川省青年马克思主义者培养工程第九届高校班学员拟推荐名单的公示" {
		t.Fatalf("article title = %q", article.Title)
	}
	if article.Type != "通知公告" {
		t.Fatalf("article type = %q", article.Type)
	}
	if article.Date != "2026-09-04" || article.PublishedAt != "2026-09-04 11:39:00" {
		t.Fatalf("article dates = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Source != "团委" {
		t.Fatalf("article source = %q", article.Source)
	}
	if article.Author != "团委组织工作办公室" {
		t.Fatalf("article author = %q", article.Author)
	}
	if !strings.Contains(article.Content, "屈姝含") || !strings.Contains(article.Content, "公示期为2026年9月4日") {
		t.Fatalf("article content is incomplete: %q", article.Content)
	}
	if strings.Contains(article.Content, "点击数") {
		t.Fatalf("metadata leaked into content: %q", article.Content)
	}
	if article.ContentHTML == "" {
		t.Fatal("article content HTML is empty")
	}
}

func TestYouthRejectsErrorShells(t *testing.T) {
	body := readYouthFixture(t, "youth_404.html")
	if _, err := parseAdapter_youthList(body, youthTestBaseURL); err == nil {
		t.Fatal("live 404 response was accepted as a list")
	}
	if _, err := parseAdapter_youthArticle(body, youthTestBaseURL); err == nil {
		t.Fatal("live 404 response was accepted as an article")
	}
}
