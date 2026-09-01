package scraper

import (
	"os"
	"testing"
)

func TestJgdwAdapterRegistered(t *testing.T) {
	site := AllSites["jgdw"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("jgdw adapter hooks are not registered")
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
	if article.Content == "" {
		t.Fatal("article content is empty")
	}
}
