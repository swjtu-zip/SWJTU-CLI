package scraper

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestSicAdapterRegistered(t *testing.T) {
	site, ok := AllSites["sic"]
	if !ok {
		t.Fatal("sic site is not registered")
	}
	if site.BaseURL != "https://sic.swjtu.edu.cn" {
		t.Fatalf("unexpected base URL: %q", site.BaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("sic adapter hooks are incomplete: %+v", site)
	}
	for _, want := range []string{"xydt", "tzgg/qb", "tzgg/xshd"} {
		found := false
		for _, got := range site.SubPages {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing sic sub-page %q in %#v", want, site.SubPages)
		}
	}
}

func TestSicPageURL(t *testing.T) {
	const base = "https://sic.swjtu.edu.cn"
	if got := sicPageURL(base, "xydt", 1, 17, ".htm"); got != base+"/xydt.htm" {
		t.Fatalf("unexpected first page URL: %q", got)
	}
	if got := sicPageURL(base, "xydt", 2, 17, ".htm"); got != base+"/xydt/16.htm" {
		t.Fatalf("unexpected second page URL: %q", got)
	}
	if got := sicPageURL(base, "xydt", 17, 17, ".htm"); got != base+"/xydt/1.htm" {
		t.Fatalf("unexpected last page URL: %q", got)
	}
}

func TestSicParseHomepageSnapshot(t *testing.T) {
	html, err := os.ReadFile("testdata/sic.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseSicNews(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) != 45 {
		t.Fatalf("expected 45 unique article links, got %d", len(items))
	}

	if got := items[0]; got.Title != "职引未来｜集成电路学院、利兹学院学子走进成都京东方研学" || got.URL != "https://sic.swjtu.edu.cn/info/1381/6641.htm" || got.Date != "2026-05-30" {
		t.Fatalf("unexpected first item: %+v", got)
	}

	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate article URL: %s", item.URL)
		}
		seen[item.URL] = true
		if !strings.Contains(item.URL, "/info/") || !strings.HasSuffix(item.URL, ".htm") {
			t.Errorf("unexpected article URL: %q", item.URL)
		}
		if item.Title == "首页" || item.Title == "查看更多+" || item.Title == "通知公告" {
			t.Errorf("navigation title leaked: %q", item.Title)
		}
		if item.Date == "" {
			t.Errorf("empty date for %q", item.Title)
			continue
		}
		if _, err := time.Parse("2006-01-02", item.Date); err != nil {
			t.Errorf("non-normalized date %q for %q", item.Date, item.Title)
		}
	}
}

func TestSicParseListFiltersBannerAndMergesDuplicate(t *testing.T) {
	html := []byte(`<html><body>
<header><a href="info/9999/1.htm" title="首页导航">首页导航</a></header>
<div class="bannerBox"><a href="info/9999/2.htm" title="横幅链接">横幅链接</a></div>
<main><ul>
  <li><a href="info/1381/1001.htm"><div class="date"><i>07</i><span>2026.08</span></div><h5>第一条学院新闻</h5></a></li>
  <li><a href="info/1381/1001.htm" title="第一条学院新闻"><i>2026.08.07</i></a></li>
  <li><a href="info/1381/1002.htm" title="第二条学院新闻"><time>2026/08/06 12:00</time></a></li>
</ul></main>
</body></html>`)

	items, err := parseSicNews(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two content articles, got %d: %#v", len(items), items)
	}
	if items[0].URL != "https://sic.swjtu.edu.cn/info/1381/1001.htm" || items[0].Title != "第一条学院新闻" || items[0].Date != "2026-08-07" {
		t.Fatalf("unexpected merged item: %+v", items[0])
	}
	if items[1].URL != "https://sic.swjtu.edu.cn/info/1381/1002.htm" || items[1].Date != "2026-08-06" {
		t.Fatalf("unexpected second item: %+v", items[1])
	}
}

func TestSicParseArticleNormalizesTimestamp(t *testing.T) {
	html := []byte(`<html><body>
<h1 class="news_tit">职引未来｜集成电路学院、利兹学院学子走进成都京东方研学</h1>
<div class="news_source"><span>来源：</span><span>日期：2026/05/30 16:30:21</span><span>点击数：</span></div>
<div class="v_news_content"><p>正文内容。</p></div>
</body></html>`)

	article, err := parseSicArticle(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "职引未来｜集成电路学院、利兹学院学子走进成都京东方研学" {
		t.Fatalf("unexpected article title: %q", article.Title)
	}
	if article.Date != "2026-05-30" {
		t.Fatalf("unexpected article date: %q", article.Date)
	}
	if article.Content != "正文内容。" {
		t.Fatalf("unexpected article content: %q", article.Content)
	}
}

func TestSicParseArticlePrefersDetailHeadline(t *testing.T) {
	html := []byte(`<html><body>
<h1 class="margin_auto_1">学院动态</h1>
<div class="show01"><h5>真实的集成电路学院文章标题</h5><p><em>来源：</em><em>日期：2026/08/28 17:03:55</em></p></div>
<div class="show02"><div class="v_news_content"><p>这是一段足够长的正文，用于验证详情页标题不会被侧栏栏目名覆盖。</p></div></div>
</body></html>`)
	article, err := parseSicArticle(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "真实的集成电路学院文章标题" {
		t.Fatalf("article title = %q", article.Title)
	}
}
