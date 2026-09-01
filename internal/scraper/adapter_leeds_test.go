package scraper

import (
	"strings"
	"testing"
)

func TestLeedsAdapterRegistered(t *testing.T) {
	site := AllSites["leeds"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("leeds adapter hooks are not registered")
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
