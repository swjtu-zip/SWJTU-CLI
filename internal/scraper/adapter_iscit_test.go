package scraper

import (
	"os"
	"testing"
)

func TestIscitAdapterRegistered(t *testing.T) {
	site := AllSites["iscit"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("iscit adapter hooks are not registered")
	}
}

func TestIscitListFiltersNonArticlesAndMergesDuplicates(t *testing.T) {
	html, err := os.ReadFile("testdata/iscit.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseIscitNews(html, "https://iscit.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].URL != "https://iscit.swjtu.edu.cn/info/1102/8490.htm" {
		t.Fatalf("first URL = %q", items[0].URL)
	}
	if items[0].Title != "2025届推免专题" || items[0].Date != "2024-10-16" {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[1].URL != "https://iscit.swjtu.edu.cn/info/1091/1131.htm" || items[1].Date != "2024-06-07" {
		t.Fatalf("second item = %+v", items[1])
	}
}

func TestIscitArticleDate(t *testing.T) {
	html, err := os.ReadFile("testdata/iscit_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseIscitArticle(html, "https://iscit.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "【讲座预告】探寻智能出行与交通安全研究的结合点" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2025-02-26" {
		t.Fatalf("date = %q, want 2025-02-26", article.Date)
	}
}

func TestNormalizeIscitDateDoesNotInventYear(t *testing.T) {
	if got := normalizeIscitDate("07-08"); got != "" {
		t.Fatalf("date = %q, want empty", got)
	}
}
