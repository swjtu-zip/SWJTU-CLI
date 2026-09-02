package scraper

import "testing"

func TestZzxyListParsesTimeAndSection(t *testing.T) {
	html := []byte(`<div class="right"><ul class="list">
<li><div class="tit"><span class="tag">[学院要闻]</span><a href="info/1065/23431.htm">我院铸牢中华民族共同体意识研究基地两项成果获得省级荣誉</a></div><div class="time">时间：2026-08-26 阅读次数：1</div></li>
</ul></div>`)
	items, err := parseAdapter_zzxyList(html, "https://zzxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Date != "2026-08-26" || items[0].PublishedAt != "2026-08-26" {
		t.Fatalf("date fields = %q/%q", items[0].Date, items[0].PublishedAt)
	}
	if items[0].Type != "学院要闻" {
		t.Fatalf("type = %q", items[0].Type)
	}
}

func TestZzxyArticleParsesAuthorDate(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="一篇学院新闻"></head><body>
<div class="hd"><h3>一篇学院新闻</h3><div class="author">发布者：赵子涵日期：2026-01-13 11:35 &nbsp;点击数：1</div></div>
<div class="right"><div class="title"><h3>学院要闻</h3></div></div>
<div id="vsb_content"><div class="v_news_content"><p>这是用于验证正文提取的内容。</p></div></div>
</body></html>`)
	article, err := parseAdapter_zzxyArticle(html, "https://zzxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-01-13" || article.PublishedAt != "2026-01-13 11:35:00" {
		t.Fatalf("date fields = %q/%q", article.Date, article.PublishedAt)
	}
	if article.Type != "学院要闻" {
		t.Fatalf("type = %q", article.Type)
	}
}
