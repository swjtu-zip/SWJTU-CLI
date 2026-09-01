package scraper

import (
	"os"
	"strings"
	"testing"
)

func TestWkjscAdapterRegistered(t *testing.T) {
	site := AllSites["wkjsc"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("wkjsc adapter hooks are not registered")
	}
}

func TestWkjscListUsesDatedNewsRows(t *testing.T) {
	body, err := os.ReadFile("testdata/wkjsc.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_wkjscList(body, "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("got %d items, want 20 dated news rows", len(items))
	}
	for _, item := range items {
		if item.Title == "详细" {
			t.Errorf("slideshow summary leaked as an article: %+v", item)
		}
		if item.Date == "" {
			t.Errorf("article %q has no date", item.Title)
		}
		if !strings.Contains(item.URL, "/info/") {
			t.Errorf("non-article URL leaked: %q", item.URL)
		}
	}
}

func TestWkjscArticleReadsLegacyMetaLine(t *testing.T) {
	body := []byte(`<html><head><title>测试文章-文科建设处</title></head><body>
<div id="div_title"><span id="lb_title">测试文章</span><br>
<span id="lb_hits">[2026-05-18 来源 新华社 编辑: 浏览 3次]</span></div>
<div id="div_wenzi"><div id="vsb_content"><div class="v_news_content"><p>这是一段足够长的正文，用于验证文科建设处旧模板的详情元数据解析。</p></div></div></div>
</body></html>`)
	article, err := parseAdapter_wkjscArticle(body, "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "测试文章" || article.Date != "2026-05-18" || article.Source != "新华社" {
		t.Fatalf("unexpected article: %+v", article)
	}
}

func TestWkjscArticleUsesDetailTitleInsteadOfSiteName(t *testing.T) {
	body, err := os.ReadFile("testdata/wkjsc_article.html")
	if err != nil {
		t.Fatal(err)
	}

	generic, err := parseBodaArticle(body, "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if generic.Title != wkjscSiteName {
		t.Fatalf("fixture no longer reproduces generic site-name title: %q", generic.Title)
	}

	article, err := parseAdapter_wkjscArticle(body, "https://wkjsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}

	want := "学校获四川省社会科学优秀成果特等奖"
	if article.Title != want {
		t.Fatalf("article title = %q, want %q", article.Title, want)
	}
	if article.Title == wkjscSiteName {
		t.Fatalf("site name leaked into article title: %q", article.Title)
	}
	if article.Date != "2026-05-20" {
		t.Fatalf("article date = %q, want 2026-05-20", article.Date)
	}
	if article.Source != "新华社" {
		t.Fatalf("article source = %q, want 新华社", article.Source)
	}
	if !strings.Contains(article.Content, "四川省社会科学评奖委员会") {
		t.Fatalf("article content is missing: %q", article.Content)
	}
}
