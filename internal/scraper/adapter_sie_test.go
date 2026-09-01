package scraper

import (
	"os"
	"testing"
)

func TestSieListUsesBothHomepageNewsBlocks(t *testing.T) {
	body, err := os.ReadFile("testdata/sie.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_sieList(body, "https://sie.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 7 {
		t.Fatalf("got %d items, want 7: %#v", len(items), items)
	}
	for _, item := range items {
		if item.Date == "" {
			t.Errorf("item %q has no date", item.Title)
		}
	}
}

func TestSieArticleUsesSiteTitle(t *testing.T) {
	body := []byte(`<html><body><div class="detail-tit">国际学生新闻</div><div class="detail-meta">发布日期：2026年06月10日</div><div class="detail-main"><div class="v_news_content"><p>这是足够长的详情正文，用于验证国际教育学院详情页的站点专属解析。</p></div></div></body></html>`)
	article, err := parseAdapter_sieArticle(body, "https://sie.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "国际学生新闻" || article.Date != "2026-06-10" {
		t.Fatalf("unexpected article: %+v", article)
	}
}
