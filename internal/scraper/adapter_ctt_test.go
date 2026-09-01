package scraper

import (
	"os"
	"strings"
	"testing"
)

func TestCttAdapterRegistration(t *testing.T) {
	site, ok := AllSites["ctt"]
	if !ok {
		t.Fatal("ctt site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("CTT adapter hooks are incomplete: %+v", site)
	}
}

func TestCttListParserExtractsCardsAndDates(t *testing.T) {
	body, err := os.ReadFile("testdata/ctt.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseCttNews(body, "http://ctt.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d CTT articles, want 3: %#v", len(items), items)
	}
	wantDates := []string{"2025-11-24", "2025-11-05", "2025-10-23"}
	for i, item := range items {
		if item.Date != wantDates[i] {
			t.Errorf("item %d date = %q, want %q", i, item.Date, wantDates[i])
		}
		if !strings.HasPrefix(item.URL, "http://ctt.swjtu.edu.cn/info/") {
			t.Errorf("item %d has unexpected URL: %q", i, item.URL)
		}
	}
	for _, item := range items {
		if item.URL == "http://ctt.swjtu.edu.cn/info/3511/70731.htm" || item.URL == "http://ctt.swjtu.edu.cn/info/3511/99999.htm" {
			t.Errorf("navigation/banner link leaked: %#v", item)
		}
	}
}

func TestCttHomeParserUsesLatestNewsContainer(t *testing.T) {
	body, err := os.ReadFile("testdata/ctt_home.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseCttNews(body, "http://ctt.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d CTT home articles, want 2: %#v", len(items), items)
	}
	if items[0].URL != "http://ctt.swjtu.edu.cn/228032.htm" || items[1].URL != "http://ctt.swjtu.edu.cn/228062.htm" {
		t.Fatalf("unexpected home URLs: %#v", items)
	}
	if items[0].Date != "06-18" || items[1].Date != "06-18" {
		t.Fatalf("unexpected home dates: %#v", items)
	}
}

func TestCttHomeParserUsesCurrentHomeModules(t *testing.T) {
	html := []byte(`<html><body>
<div class="news-list"></div>
<ul class="innews">
  <li><div class="pic"><a href="info/3421/228032.htm"><img src="cover.jpg"></a></div><div class="info">
    <a class="line2" href="info/3421/228032.htm" title="交运要闻">交运要闻</a>
    <time>2026-06-18</time>
  </div></li>
</ul>
<div class="rowb"><div class="list"><ul>
  <li><a class="texto" href="info/3521/228452.htm">院务公告</a><span>2026-07-17</span></li>
</ul></div></div>
</body></html>`)
	items, err := parseCttNews(html, "http://ctt.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d current home articles, want 2: %#v", len(items), items)
	}
	if items[0].Date != "2026-06-18" || items[1].Date != "2026-07-17" {
		t.Fatalf("unexpected current home dates: %#v", items)
	}
}

func TestCttPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	const baseURL = "http://ctt.swjtu.edu.cn"
	if got := cttPageURL(baseURL, "tzgg/xytz", 1, 20, ".htm"); got != baseURL+"/tzgg/xytz.htm" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := cttPageURL(baseURL, "tzgg/xytz", 2, 20, ".htm"); got != baseURL+"/tzgg/xytz/19.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
	if got := cttPageURL(baseURL, "tzgg/xytz", 20, 20, ".htm"); got != baseURL+"/tzgg/xytz/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
}

func TestCttArticleParserReadsPublishDate(t *testing.T) {
	html := []byte(`<html><body>
<div class="left-top"><h3 class="news-title">CTT 测试文章</h3>
<div class="top-info"><span>来源：交通运输与物流学院</span><span>发布日期：2026-05-21</span></div></div>
<div class="left-main"><div class="v_news_content"><p>这是一段足够长的测试正文，用于验证文章解析仍然沿用公共内容提取逻辑。</p></div></div>
</body></html>`)

	article, err := parseCttArticle(html, "http://ctt.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-05-21" {
		t.Fatalf("article date = %q, want 2026-05-21", article.Date)
	}
}
