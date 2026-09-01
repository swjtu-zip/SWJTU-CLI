package scraper

import (
	"os"
	"strings"
	"testing"
)

func TestDqxyAdapterRegistration(t *testing.T) {
	site, ok := AllSites["dqxy"]
	if !ok {
		t.Fatal("dqxy site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatal("dqxy adapter hooks are incomplete")
	}

	const base = "http://dqxy.swjtu.edu.cn"
	if got := site.PageURL(base, "xwdt/qb", 1, 21, ".htm"); got != base+"/xwdt/qb.htm" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 2, 21, ".htm"); got != base+"/xwdt/qb/20.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 21, 21, ".htm"); got != base+"/xwdt/qb/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
}

func TestDqxyAdapterList(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseDqxyNews(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("got %d list items, want 20", len(items))
	}

	wantFirstTitle := "聚焦前沿交叉，激发创新动能——西南交通大学2026年“智驭交通·能动未来”创新领军工程硕博士论坛顺利举行"
	if items[0].URL != "http://dqxy.swjtu.edu.cn/info/2926/234499.htm" {
		t.Errorf("first URL = %q", items[0].URL)
	}
	if items[0].Title != wantFirstTitle {
		t.Errorf("first title = %q", items[0].Title)
	}
	if items[0].Date != "2026-07-19" {
		t.Errorf("first date = %q", items[0].Date)
	}
	if items[len(items)-1].URL != "http://dqxy.swjtu.edu.cn/info/2926/233409.htm" {
		t.Errorf("last URL = %q", items[len(items)-1].URL)
	}
	if items[len(items)-1].Date != "2026-05-17" {
		t.Errorf("last date = %q", items[len(items)-1].Date)
	}

	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate URL %q", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, "http://dqxy.swjtu.edu.cn/info/") {
			t.Errorf("non-article URL %q", item.URL)
		}
		if len(item.Date) != 10 || item.Date[4] != '-' || item.Date[7] != '-' {
			t.Errorf("item %q has non-ISO date %q", item.Title, item.Date)
		}
		if item.Title == "全部" || item.Title == "学院新闻" || item.Title == "党建动态" {
			t.Errorf("navigation title leaked: %q", item.Title)
		}
	}
}

func TestDqxyAdapterDoesNotInventYearOrKeepNavigation(t *testing.T) {
	const fixture = `<main class="tzgg_page">
  <div class="team_base tzgg_page_base">
    <div class="item"><a href="../info/2926/200001.htm"><p class="a_block line1"><span class="lanmu">【学院新闻】</span>一条没有完整年份的新闻</p><div class="date">12-31</div></a></div>
  </div>
</main>
<nav><div class="item"><a href="../info/2926/200002.htm">学院新闻</a></div></nav>`

	items, err := parseDqxyNews([]byte(fixture), "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want one article", len(items))
	}
	if items[0].Title != "一条没有完整年份的新闻" {
		t.Fatalf("title = %q", items[0].Title)
	}
	if items[0].Date != "" {
		t.Fatalf("date = %q, want empty because source has only MM-DD", items[0].Date)
	}
}

func TestDqxyAdapterArticle(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_article.html")
	if err != nil {
		t.Fatal(err)
	}
	art, err := parseDqxyArticle(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if art.Title != "聚焦前沿交叉，激发创新动能——西南交通大学2026年“智驭交通·能动未来”创新领军工程硕博士论坛顺利举行" {
		t.Errorf("title = %q", art.Title)
	}
	if art.Date != "2026-07-19" {
		t.Errorf("date = %q, want 2026-07-19", art.Date)
	}
	if len(art.Content) < 100 {
		t.Errorf("content too short: %d", len(art.Content))
	}
}
