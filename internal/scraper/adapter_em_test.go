package scraper

import (
	"os"
	"testing"
)

func TestEmAdapterRegistered(t *testing.T) {
	site := AllSites["em"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("em adapter hooks are not registered")
	}
}

func TestEmArticleUsesShow01Headline(t *testing.T) {
	html, err := os.ReadFile("testdata/em_article.html")
	if err != nil {
		t.Fatal(err)
	}

	legacy, err := parseBodaArticle(html, "https://em.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Title != "新闻动态" {
		t.Fatalf("fixture no longer reproduces the generic title: %q", legacy.Title)
	}

	article, err := parseAdapter_emArticle(html, "https://em.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	want := "校区纪委组织开展廉洁主题系列教育活动 ——陈诗伟讲党风廉政教育党课"
	if article.Title != want {
		t.Fatalf("article title = %q, want %q", article.Title, want)
	}
	if article.Content == "" {
		t.Fatal("article content is empty")
	}
}
