package scraper

import (
	"os"
	"strings"
	"testing"
)

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
