package scraper

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSistAdapterRegistered(t *testing.T) {
	site, ok := AllSites["sist"]
	if !ok {
		t.Fatal("sist site is not registered")
	}
	if site.BaseURL != "http://sist.swjtu.edu.cn" {
		t.Fatalf("unexpected base URL: %q", site.BaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("sist adapter hooks are incomplete: %+v", site)
	}
	if len(site.SubPages) != 3 || site.SubPages[0] != "notice" || site.SubPages[1] != "undergraduate" || site.SubPages[2] != "postgraduate" {
		t.Fatalf("unexpected sist sub-pages: %#v", site.SubPages)
	}
}

func TestSistPageURL(t *testing.T) {
	const base = "http://sist.swjtu.edu.cn"
	if got := sistPageURL(base, "index", 1, 0, ".htm"); got != base+"/home?lang=cn" {
		t.Fatalf("unexpected index URL: %q", got)
	}
	if got := sistPageURL(base, "undergraduate", 1, 0, ".htm"); got != base+"/pc/9JpfFAQClhKDd-As8MXBYS8/KW_T7dJ2lt5sC-RJZSnPtk0?type=2&lang=cn" {
		t.Fatalf("unexpected undergraduate URL: %q", got)
	}
}

func TestSistAPIListHasRealItemsAndNormalizedDates(t *testing.T) {
	body, err := os.ReadFile("testdata/sist_api_list.json")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseSistNews(body, "https://sist.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("expected 4 usable articles, got %d: %#v", len(items), items)
	}
	if items[0].Title != "我院学子在第19届中国大学生计算机设计大赛中再获佳绩" {
		t.Fatalf("unexpected first title: %q", items[0].Title)
	}
	if items[0].Date != "2026-08-29" {
		t.Fatalf("unexpected first date: %q", items[0].Date)
	}
	wantURL := "https://sist.swjtu.edu.cn/pc/" + sistNewsNavID + "?id=uzbA5Cgk2sZdIHrZYyeuCs38&type=2&lang=cn"
	if items[0].URL != wantURL {
		t.Fatalf("unexpected first URL: %q", items[0].URL)
	}
	seen := make(map[string]bool)
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate article URL: %q", item.URL)
		}
		seen[item.URL] = true
		if strings.Contains(item.Title, "新闻中心") || strings.Contains(item.Title, "通知公告") {
			t.Errorf("navigation title leaked into result: %q", item.Title)
		}
		if !strings.HasPrefix(item.Date, "2026-") {
			t.Errorf("date was not normalized: %q", item.Date)
		}
	}
}

func TestSistRenderedListKeepsOnlyArticleLinks(t *testing.T) {
	body := []byte(`<html><body>
<nav><a href="/pc/9JpfFAQClhKDd-As8MXBYS8?type=2">本科教育</a></nav>
<main class="article_list">
  <a href="/pc/DpHktHeXJp3MX-FZoZdjz3w?id=one&type=2&lang=cn"><span class="article-title">第一条真实学院新闻</span><time>2026年8月29日</time></a>
  <a href="/pc/DpHktHeXJp3MX-FZoZdjz3w?id=two&type=2&lang=cn"><span class="article-title">第二条真实学院新闻</span><time>2026/08/23 00:00:00</time></a>
  <a href="https://example.invalid/pc/news?id=three">外部导航项</a>
</main>
</body></html>`)
	items, err := parseSistNews(body, "https://sist.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 article links, got %d: %#v", len(items), items)
	}
	if items[0].Date != "2026-08-29" || items[1].Date != "2026-08-23" {
		t.Fatalf("unexpected dates: %#v", items)
	}
}

func TestSistArticleAPIFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/sist_api_article.json")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseSistArticle(body, "https://sist.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "我院学子在第19届中国大学生计算机设计大赛中再获佳绩" {
		t.Fatalf("unexpected title: %q", article.Title)
	}
	if article.Date != "2026-08-29" {
		t.Fatalf("unexpected date: %q", article.Date)
	}
	if article.Source != "信息科学与技术学院" || article.Author != "吴卫伟" {
		t.Fatalf("unexpected metadata: source=%q author=%q", article.Source, article.Author)
	}
	if !strings.Contains(article.Content, "第19届中国大学生计算机设计大赛") {
		t.Fatalf("article content was not extracted: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://sist.swjtu.edu.cn/attached/2026/08/29/f78c0101-e286-413f-b1af-05a3f45cf438.png" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
}

func TestSistShellIsNotASuccessfulEmptyFeed(t *testing.T) {
	body, err := os.ReadFile("testdata/sist.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseSistNews(body, "http://sist.swjtu.edu.cn")
	if len(items) != 0 {
		t.Fatalf("SPA shell unexpectedly produced items: %#v", items)
	}
	if !errors.Is(err, errSistDynamicPage) {
		t.Fatalf("expected explicit SPA/API error, got %v", err)
	}
	if !strings.Contains(err.Error(), "/dev-api/web/articlelist") {
		t.Fatalf("error did not identify the required API: %v", err)
	}
}

func TestSistShellArticleIsNotParsedAsArticle(t *testing.T) {
	body, err := os.ReadFile("testdata/sist.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseSistArticle(body, "http://sist.swjtu.edu.cn")
	if article != nil {
		t.Fatalf("SPA shell unexpectedly produced article: %#v", article)
	}
	if !errors.Is(err, errSistDynamicPage) {
		t.Fatalf("expected explicit SPA/API error, got %v", err)
	}
	if !strings.Contains(err.Error(), "/dev-api/web/article") {
		t.Fatalf("error did not identify the required API: %v", err)
	}
}
