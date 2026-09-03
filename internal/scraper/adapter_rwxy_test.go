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

func TestRwxyLegacyMigrationDateIsNotPublishedDate(t *testing.T) {
	if date, publishedAt := rwxyDateValues("日期：2013-01-01 00:00"); date != "" || publishedAt != "" {
		t.Fatalf("legacy migration date leaked: %q/%q", date, publishedAt)
	}

	html := []byte(`<html><body><form name="_newscontent_fromname"><div class="title"><h3>旧文章</h3><div class="info">日期：2013-01-01 00:00</div></div><div class="detail"><div class="v_news_content"><p>这是一段完整的旧文章正文，日期来自迁移模板。</p></div></div></form></body></html>`)
	article, err := parseAdapter_rwxyArticle(html, "https://rwxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("legacy migration date leaked from article: %q/%q", article.Date, article.PublishedAt)
	}
}

func TestRwxyHomeParsesLiveDatesAndTypes(t *testing.T) {
	html := []byte(`<html><body>
<div class="news"><div class="conts"><div class="title"><b>学院新闻</b></div>
  <div class="item"><div class="imgs"><div class="labs"><em>07-14</em><b>2026</b></div></div><div class="text"><div class="tits"><a href="info/1142/28674.htm">学院新闻标题</a></div></div></div>
</div></div>
<div class="activity"><div class="title"><b>学术活动</b></div><div class="conts"><div class="swiper-slide">
  <div class="inform"><div class="time"><span>2026/06/02</span></div></div><div class="tits"><a href="info/1143/27964.htm">学术活动标题</a></div>
</div></div></div>
</body></html>`)

	items, err := parseAdapter_rwxyList(html, "https://rwxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Date != "2026-07-14" || items[0].Type != "学院新闻" {
		t.Errorf("home news item = %#v", items[0])
	}
	if items[1].Date != "2026-06-02" || items[1].Type != "学术活动" {
		t.Errorf("home activity item = %#v", items[1])
	}
}

func TestRwxyAdapterOwnsDisplayedReversePagination(t *testing.T) {
	html := []byte(`<div class="list-news"><ul><li><a href="info/1142/28674.htm"><div class="time"><!-- 2026年7月14日-->标题</div></a></li></ul></div>
<div class="p_pages"><span class="p_no_d">1</span><a href="xyxw/215.htm">2</a><a href="xyxw/1.htm">216</a></div>`)
	if got := rwxyTotalPagesFromHTML(html); got != 216 {
		t.Fatalf("total pages = %d, want 216", got)
	}
	base := "http://rwxy.swjtu.edu.cn"
	if got := rwxyPageURL(base, "xyxw", 2, 216, ".htm"); got != base+"/xyxw/215.htm" {
		t.Fatalf("logical page 2 URL = %q", got)
	}
	if got := rwxyPageURL(base, "xyxw", 217, 216, ".htm"); got != "" {
		t.Fatalf("out-of-range URL = %q", got)
	}
}
