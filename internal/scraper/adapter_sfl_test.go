package scraper

import (
	"strings"
	"testing"
	"time"
)

func TestSflListUsesCanonicalRowsAndDates(t *testing.T) {
	const listHTML = `<html><body>
<nav><a href="info/9999/1.htm">菜单入口</a></nav>
<div class="xnewlist"><ul class="list-group rightnew">
  <li><a href="info/1263/38335.htm" title="美国内布拉斯加州教育工作者代表团访问西南交通大学外国语学院">截断标题</a><span class="tim">[2026-06-26]</span></li>
  <li><a href="info/1264/38275.htm" title="我校学子在第31届“21世纪杯”全国英语演讲比赛川渝地区决赛中荣获佳绩">截断标题</a><span class="tim">[2026-06-23]</span></li>
</ul><div class="lit">共1149条 1/58</div></div>
<div class="news-img"><a href="info/1263/1.htm"><img src="cover.jpg"></a></div>
</body></html>`

	items, err := parseSflNews([]byte(listHTML), "http://sfl.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	wantDates := []string{"2026-06-26", "2026-06-23"}
	for i, item := range items {
		if item.Date != wantDates[i] {
			t.Errorf("item %d date = %q, want %q", i, item.Date, wantDates[i])
		}
		if !strings.Contains(item.URL, "/info/") || !strings.HasSuffix(item.URL, ".htm") {
			t.Errorf("item %d has non-article URL %q", i, item.URL)
		}
	}
	if got := sflPageURL("http://sfl.swjtu.edu.cn", "index", 2, 58, ".htm"); got != "http://sfl.swjtu.edu.cn/xyxw/57.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
}

func TestSflHomeParserDoesNotUseImageCardsOrNavigation(t *testing.T) {
	const homeHTML = `<html><body>
<nav><a href="info/9999/1.htm">导航入口</a></nav>
<div class="product-wrap"><div class="product">
  <div class="news-text fl"><ul>
    <li><a href="info/1263/38335.htm" title="美国内布拉斯加州教育工作者代表团访问西南交通大学外国语学院">美国内布拉斯加州教育工作者代表团访问西南交通大...</a></li>
    <li><a href="info/1264/38275.htm" title="我校学子在第31届“21世纪杯”全国英语演讲比赛川渝地区决赛中荣获佳绩">我校学子在第31届“21世纪杯”全国英语演讲比赛川...</a></li>
  </ul></div>
  <div class="news-img"><a href="info/1263/30001.htm"><img src="cover.jpg"></a></div>
</div></div></body></html>`

	items, err := parseSflNews([]byte(homeHTML), "http://sfl.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d home items, want 2: %+v", len(items), items)
	}
	for _, item := range items {
		if strings.Contains(item.URL, "30001") {
			t.Fatalf("image card leaked into list: %+v", item)
		}
	}
}

func TestSflArticleUsesListDateInsteadOfBodyEventDate(t *testing.T) {
	const listHTML = `<ul class="list-group rightnew"><li>
<a href="info/1264/38275.htm" title="我校学子在第31届“21世纪杯”全国英语演讲比赛川渝地区决赛中荣获佳绩">文章</a>
<span class="tim">[2026-06-23]</span></li></ul>`
	if _, err := parseSflNews([]byte(listHTML), "http://sfl.swjtu.edu.cn"); err != nil {
		t.Fatal(err)
	}

	const articleHTML = `<html><head><script>_jsq_(1264,'/nry.jsp',38275,1447715275)</script></head><body>
<div class="xnewlist newtext ntext"><h1 class="newtitle2">我校学子在第31届“21世纪杯”全国英语演讲比赛川渝地区决赛中荣获佳绩</h1>
<div id="vsb_content" class="newinfo2"><div class="v_news_content"><p>2026年6月21日，比赛在重庆圆满落幕。</p><p>正文内容足够长，用于验证外国语学院文章正文解析。</p></div></div></div>
</body></html>`
	article, err := parseSflArticle([]byte(articleHTML), "http://sfl.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-06-23" {
		t.Fatalf("date = %q, want list date 2026-06-23", article.Date)
	}
	if strings.Contains(article.Date, "06-21") {
		t.Fatalf("body event date was mistaken for publication date: %q", article.Date)
	}
	if article.Title == "" || !strings.Contains(article.Content, "比赛在重庆") {
		t.Fatalf("article fields not parsed: %+v", article)
	}

	// The cache is process-local and must not affect explicit metadata parsing.
	metaHTML := `<html><body><h1 class="newtitle2">另一篇文章标题</h1><div class="article-meta">发布时间：2026/07/01</div><div class="v_news_content"><p>正文内容。</p></div></body></html>`
	metaArticle, err := parseSflArticle([]byte(metaHTML), "http://sfl.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if metaArticle.Date != "2026-07-01" {
		t.Fatalf("explicit metadata date = %q", metaArticle.Date)
	}
	if _, err := time.Parse("2006-01-02", metaArticle.Date); err != nil {
		t.Fatalf("date is not ISO normalized: %q", metaArticle.Date)
	}
}
