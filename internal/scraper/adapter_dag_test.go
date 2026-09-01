package scraper

import (
	"os"
	"strings"
	"testing"
)

func TestDagAdapterRegistered(t *testing.T) {
	site := AllSites["dag"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("dag adapter hooks are not registered")
	}
}

func TestDagListSnapshotKeepsRealArticlesAndDates(t *testing.T) {
	html, err := os.ReadFile("testdata/dag.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseDagNews(html, "http://dag.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("got %d items, want 8 real archive articles: %#v", len(items), items)
	}
	if items[0].Title != "关于校史博物馆常态化开放的通知" {
		t.Fatalf("unexpected first title: %q", items[0].Title)
	}
	if items[0].URL != "http://dag.swjtu.edu.cn/info/1211/6901.htm" {
		t.Fatalf("unexpected first URL: %q", items[0].URL)
	}
	if items[0].Date != "2026-06-02" {
		t.Fatalf("unexpected first date: %q", items[0].Date)
	}
	for _, item := range items {
		if !strings.Contains(item.URL, "/info/") {
			t.Errorf("non-article URL leaked: %q", item.URL)
		}
		if item.Date == "" {
			t.Errorf("article %q has empty date", item.Title)
		}
	}
}

func TestDagArticleUsesDateInArchiveHeadline(t *testing.T) {
	html := []byte(`<html><head>
<title>档案馆（校史馆）2026年暑假值班表（7月20日-9月2日）-档案馆（校史馆）</title>
<meta name="pageTitle" content="档案馆（校史馆）2026年暑假值班表（7月20日-9月2日）">
</head><body><div id="detailCn"><div>
<h3 class="article_title">档案馆（校史馆）2026年暑假值班表（7月20日-9月2日）</h3>
<div id="vsb_content"><div class="v_news_content"><p>档案馆暑假值班安排及服务事项，请按公告时间办理相关业务。</p></div></div>
</div></div></body></html>`)

	article, err := parseDagArticle(html, "http://dag.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "档案馆（校史馆）2026年暑假值班表（7月20日-9月2日）" {
		t.Fatalf("unexpected title: %q", article.Title)
	}
	if article.Date != "2026-07-20" {
		t.Fatalf("date = %q, want 2026-07-20", article.Date)
	}
	if !strings.Contains(article.Content, "档案馆暑假值班安排") {
		t.Fatalf("article content was not extracted: %q", article.Content)
	}
}
