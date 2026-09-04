package scraper

import (
	"os"
	"strings"
	"testing"
)

func xunchaFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read xuncha fixture %s: %v", name, err)
	}
	return body
}

func TestXunchaHomepageLiveFixtureParsesScopedModules(t *testing.T) {
	items, err := parseAdapter_xunchaList(xunchaFixture(t, "xuncha_home_live.html"), xunchaCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 23 {
		t.Fatalf("homepage items = %d, want 23 real article links", len(items))
	}

	counts := make(map[string]int)
	for _, item := range items {
		counts[item.Type]++
		if item.Date != "" || item.PublishedAt != "" {
			t.Fatalf("homepage month-day value invented a year/time: %#v", item)
		}
		if strings.Contains(item.Title, "欢迎来到") {
			t.Fatalf("static site shell was emitted as an article: %#v", item)
		}
	}
	want := map[string]int{
		"工作动态":   8,
		"信息公开":   5,
		"上级精神":   5,
		"条例专题学习": 5,
	}
	for typ, expected := range want {
		if counts[typ] != expected {
			t.Errorf("%s items = %d, want %d (%#v)", typ, counts[typ], expected, counts)
		}
	}
}

func TestXunchaDetailLiveFixtureUsesFixedMetadataAndBody(t *testing.T) {
	article, err := parseAdapter_xunchaArticle(xunchaFixture(t, "xuncha_article_work_live.html"), xunchaCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "第十五届党委巡察工作领导小组召开第13次会议" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Type != "工作动态" || article.Date != "2026-07-13" || article.PublishedAt != "2026-07-13 14:58:00" {
		t.Fatalf("fixed metadata = type=%q date=%q published=%q", article.Type, article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "第十五届党委巡察工作领导小组第13次会议") {
		t.Fatalf("body text missing: %q", article.Content)
	}
	if strings.Contains(article.Content, "来源：") || strings.Contains(article.ContentHTML, "日期：2026年07月13日") {
		t.Fatalf("source metadata leaked into body: content=%q html=%q", article.Content, article.ContentHTML)
	}
	if len(article.Images) != 1 || !strings.HasSuffix(article.Images[0], ".jpg") {
		t.Fatalf("detail images = %#v", article.Images)
	}
}

func TestXunchaRejectsEmptyParentShell(t *testing.T) {
	if _, err := parseAdapter_xunchaList(xunchaFixture(t, "xuncha_empty_parent_live.html"), xunchaCanonicalBaseURL); err == nil {
		t.Fatal("empty jyjl parent shell was accepted as an article list")
	}
}
