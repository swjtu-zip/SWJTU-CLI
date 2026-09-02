package scraper

import "testing"

func TestRwxyListUsesHeadlineAndCommentDate(t *testing.T) {
	html := []byte(`<div class="sidebar"><div class="title"><span>学院新闻</span></div></div>
<div class="loaction"><a href="index.htm">首页</a><a href="xyxw.htm">学院新闻</a></div>
<div class="list-news"><ul><li><a href="info/1142/28674.htm"><div class="intr"><div class="time"><!-- 2026年7月14日--> 北京交通大学外国语与新闻传播学院与西南交通大学人文学院共话新闻传播学科建设</div><div class="more"><a href="info/1142/28674.htm">More →</a></div></div></a></li></ul></div>`)
	items, err := parseAdapter_rwxyList(html, "https://rwxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Title != "北京交通大学外国语与新闻传播学院与西南交通大学人文学院共话新闻传播学科建设" {
		t.Fatalf("title = %q", items[0].Title)
	}
	if items[0].Date != "2026-07-14" || items[0].PublishedAt != "2026-07-14" {
		t.Fatalf("date fields = %q/%q", items[0].Date, items[0].PublishedAt)
	}
	if items[0].Type != "学院新闻" {
		t.Fatalf("type = %q", items[0].Type)
	}
}

func TestRwxyArticleParsesPreciseDate(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="人文学院新闻"></head><body>
<div class="sidebar"><div class="title"><span>学院新闻</span></div></div>
<form name="_newscontent_fromname"><div class="bd"><div class="title"><h3>人文学院新闻</h3><div class="info"><span>作者：杜欣瑞</span><span>日期：2026-07-14 09:56</span></div></div><div class="detail"><div id="vsb_content_501"><div class="v_news_content"><p>这是完整的详情正文。</p></div></div></div></div></form>
</body></html>`)
	article, err := parseAdapter_rwxyArticle(html, "https://rwxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-07-14" || article.PublishedAt != "2026-07-14 09:56:00" {
		t.Fatalf("date fields = %q/%q", article.Date, article.PublishedAt)
	}
	if article.Content != "这是完整的详情正文。" {
		t.Fatalf("content = %q", article.Content)
	}
}
