package scraper

import (
	"reflect"
	"strings"
	"testing"
)

func TestLeedsAdapterRegistered(t *testing.T) {
	site := AllSites["leeds"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatal("leeds adapter hooks are not registered")
	}
	want := []string{"index/xyxw", "xytz/dzbg", "xytz/jwjx", "xytz/xszc", "xxgs/dzbg", "xxgs/jwjx", "xxgs/xszc", "index/hdyg", "djysz/djyd", "djysz/jszj"}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("leeds feeds = %#v, want %#v", site.SubPages, want)
	}
	if got := site.PageURL(site.BaseURL, "index/xyxw", 2, 162, ".htm"); got != site.BaseURL+"/index/xyxw/161.htm" {
		t.Fatalf("leeds reverse page URL = %q", got)
	}
}

func TestLeedsListNormalizesDateAndKeepsSectionType(t *testing.T) {
	html := []byte(`<html><body>
<div class="loaction"><a>首页</a><a>学院通知</a><a>教务教学</a></div>
<ul class="list_zx"><li><a href="info/1037/22074.htm"><span class="xw-ul-tt">利兹学院关于教学工作的通知</span></a><span class="time">2026.07.08 14:03</span></li></ul>
</body></html>`)
	items, err := parseLeedsNews(html, "http://leeds.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "利兹学院关于教学工作的通知" || items[0].Type != "教务教学" || items[0].Date != "2026-07-08" || items[0].PublishedAt != "2026-07-08 14:03:00" {
		t.Fatalf("leeds list item = %#v", items)
	}
}

func TestLeedsArticleDateUsesArticleHeader(t *testing.T) {
	const html = `<html><body>
<div class="m-textq1">
  <h1>高分子材料与工程专业开展第二批新授课教师试讲</h1>
  <div class="info">
    <div class="item"><span class="date">2026-07-08</span></div>
    <div class="item"><span>来源：教学教师事务中心</span></div>
  </div>
  <div class="txt"><div id="vsb_content_2"><div class="v_news_content">
    <p>本次活动为授课教师搭建了互学互鉴、打磨课堂的交流平台。</p>
  </div></div></div>
</div>
<aside class="col-r"><ul class="ul-listl3">
  <li><a href="23064.htm"><div class="date sptime">2026.07.31</div><h3>最新阅读</h3></a></li>
</ul></aside>
</body></html>`

	article, err := parseLeedsArticle([]byte(html), "http://leeds.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "高分子材料与工程专业开展第二批新授课教师试讲" {
		t.Fatalf("unexpected title: %q", article.Title)
	}
	if article.Date != "2026-07-08" {
		t.Fatalf("article date = %q, want 2026-07-08", article.Date)
	}
	if !strings.Contains(article.Content, "互学互鉴") {
		t.Fatalf("article content did not parse: %q", article.Content)
	}
}

func TestLeedsArticleDateNormalizesDotFormat(t *testing.T) {
	const html = `<html><body><div class="m-textq1">
<h1>诚聘英才</h1>
<div class="info"><span class="date">2026.07.08</span></div>
<div class="txt"><div class="v_news_content"><p>欢迎加入利兹学院。</p></div></div>
</div></body></html>`

	article, err := parseLeedsArticle([]byte(html), "http://leeds.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-07-08" {
		t.Fatalf("article date = %q, want 2026-07-08", article.Date)
	}
}
