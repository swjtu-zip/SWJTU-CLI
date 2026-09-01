package scraper

import (
	"os"
	"strings"
	"testing"
)

func TestXinliAdapterRegistered(t *testing.T) {
	site := AllSites["xinli"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("xinli adapter hooks are not registered")
	}
}

func TestXinliArticleUsesDetailTitleInsteadOfSectionHeading(t *testing.T) {
	html, err := os.ReadFile("testdata/xinli_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_xinliArticle(html, "http://xinli.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	wantTitle := "第九届“演说中国”全国青少年演讲与朗诵艺术展示全国总展演隆重启幕"
	if article.Title != wantTitle {
		t.Fatalf("title = %q, want detail title %q", article.Title, wantTitle)
	}
	if article.Title == "中心要闻" {
		t.Fatal("section heading was incorrectly used as article title")
	}
	if article.Date != "2026-08-13" {
		t.Fatalf("date = %q, want 2026-08-13", article.Date)
	}
	if !strings.Contains(article.Content, "全国总展演开幕式") {
		t.Fatalf("article content was not extracted: %q", article.Content)
	}
}
