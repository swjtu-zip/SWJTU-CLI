package scraper

import (
	"os"
	"testing"
)

func TestCmAdapterRegistration(t *testing.T) {
	site, ok := AllSites["cm"]
	if !ok {
		t.Fatal("cm site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatalf("CM adapter hooks are incomplete: %+v", site)
	}
}

func TestCmArticleParserPrefersDtlTitleOverSidebarHome(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_cmArticle(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "国家卫生健康委员会专家组到医学院开展生物安全飞行检查" {
		t.Fatalf("article title = %q, want the .dtl-tit h4 headline", article.Title)
	}
	if article.Title == "首页" {
		t.Fatal("sidebar title leaked into article title")
	}
	if article.Date != "2026-07-29" {
		t.Fatalf("article date = %q, want 2026-07-29", article.Date)
	}
	if article.Content == "" {
		t.Fatal("expected article content")
	}
}
