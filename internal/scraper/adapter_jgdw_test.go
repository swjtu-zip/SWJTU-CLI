package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestJgdwAdapterRegistered(t *testing.T) {
	site := AllSites["jgdw"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("jgdw adapter hooks are not registered")
	}
	want := []string{"djyw", "jcdj", "zgzj", "qngz", "index/tzgg", "index/tjxw"}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("jgdw subpages = %#v, want %#v", site.SubPages, want)
	}
	if site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("jgdw pagination hooks are not registered")
	}
	if got := site.PageURL(site.BaseURL, "jcdj", 1, 38, ".htm"); got != "https://jgdw.swjtu.edu.cn/jcdj.htm" {
		t.Fatalf("first jgdw page = %q", got)
	}
	if got := site.PageURL(site.BaseURL, "jcdj", 2, 38, ".htm"); got != "https://jgdw.swjtu.edu.cn/jcdj/37.htm" {
		t.Fatalf("second jgdw page = %q", got)
	}
	if got := site.PageURL(site.BaseURL, "jcdj", 38, 38, ".htm"); got != "https://jgdw.swjtu.edu.cn/jcdj/1.htm" {
		t.Fatalf("last jgdw page = %q", got)
	}
}

func TestJgdwListParsesTypeDatesAndAbsoluteURLs(t *testing.T) {
	html, err := os.ReadFile("testdata/jgdw_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_jgdwList(html, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].URL != "https://jgdw.swjtu.edu.cn/info/1012/16350.htm" || items[0].Date != "2026-07-07" || items[0].PublishedAt != "2026-07-07" {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[0].Type != "党建要闻" {
		t.Fatalf("first item type = %q", items[0].Type)
	}
	if items[1].Date != "2026-06-05" || items[1].PublishedAt != "2026-06-05 14:03:05" {
		t.Fatalf("precise item dates = (%q, %q)", items[1].Date, items[1].PublishedAt)
	}
}

func TestJgdwHomepageIncludesAllVisibleModules(t *testing.T) {
	html, err := os.ReadFile("testdata/jgdw.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_jgdwList(html, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 20 {
		t.Fatalf("homepage items = %d, want all three news panels and notice module", len(items))
	}
	types := map[string]int{}
	for _, item := range items {
		types[item.Type]++
	}
	for _, typ := range []string{"党建要闻", "基层党建", "职工之家", "通知公告"} {
		if types[typ] == 0 {
			t.Fatalf("homepage has no %q module: %#v", typ, types)
		}
	}
}

func TestJgdwEmptyListIsAcceptedButStaticAndErrorAreRejected(t *testing.T) {
	empty, err := os.ReadFile("testdata/jgdw_empty.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_jgdwList(empty, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("legal empty list rejected: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty list = %#v, want non-nil empty slice", items)
	}
	for _, name := range []string{"jgdw_static.html", "jgdw_error.html"} {
		body, readErr := os.ReadFile("testdata/" + name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, parseErr := parseAdapter_jgdwList(body, "https://jgdw.swjtu.edu.cn"); parseErr == nil {
			t.Fatalf("%s was accepted as a list", name)
		}
	}
}

func TestJgdwArticleUsesDetailHeadingInsteadOfChannelName(t *testing.T) {
	html, err := os.ReadFile("testdata/jgdw_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_jgdwArticle(html, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}

	want := "机关与直属单位党委2026年党支部委员党务工作专题培训顺利举办"
	if article.Title != want {
		t.Fatalf("article title = %q, want %q", article.Title, want)
	}
	if article.Title == "职工之家" || article.Title == "基层党建" || article.Title == "党建要闻" {
		t.Fatalf("channel name leaked into article title: %q", article.Title)
	}
	if article.Date != "2026-06-05" {
		t.Fatalf("article date = %q, want 2026-06-05", article.Date)
	}
	if article.Type != "党建要闻" {
		t.Fatalf("article type = %q, want 党建要闻", article.Type)
	}
	if article.Content == "" {
		t.Fatal("article content is empty")
	}
}

func TestJgdwArticlePreservesPreciseMetadataAndOutsideAttachments(t *testing.T) {
	html, err := os.ReadFile("testdata/jgdw_article_attachments.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_jgdwArticle(html, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "机关与直属单位党委专题培训通知" || article.Type != "通知公告" {
		t.Fatalf("article identity = (%q, %q)", article.Title, article.Type)
	}
	if article.Date != "2026-06-05" || article.PublishedAt != "2026-06-05 14:03:05" {
		t.Fatalf("article dates = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Source != "组织部" {
		t.Fatalf("article source = %q", article.Source)
	}
	if article.Content == "" || article.ContentHTML == "" {
		t.Fatal("article body is empty")
	}
	if !containsString(article.ContentHTML, "https://jgdw.swjtu.edu.cn/images/training.png") {
		t.Fatalf("article HTML did not resolve image URL: %s", article.ContentHTML)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://jgdw.swjtu.edu.cn/images/training.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "培训通知.pdf" || article.Attachments[0].URL != "https://jgdw.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1746847036&wbfileid=ABC123" {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
}

func TestJgdwArticleRejectsEmptyShell(t *testing.T) {
	body, err := os.ReadFile("testdata/jgdw_article_empty.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdapter_jgdwArticle(body, "https://jgdw.swjtu.edu.cn"); err == nil {
		t.Fatal("empty article shell was accepted")
	}
}

func containsString(value, want string) bool {
	return strings.Contains(value, want)
}
