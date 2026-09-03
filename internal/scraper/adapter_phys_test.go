package scraper

import (
	"strings"
	"testing"
)

func TestPhysAdapterRegistersAuditedArticleFeeds(t *testing.T) {
	site := AllSites["phys"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("phys adapter is not registered")
	}
	want := map[string]bool{
		"xwgg/xydt": true, "kxyj/xsdt": true,
		"rcpy/bkspy": true, "rcpy/yjspy": true,
		"xsfz/gqtgz": true, "xsfz/xgdt": true, "xsfz/xgtz": true,
		"xygz/xyjy": true, "dqgz/jjgz": true, "dqgz/pyfz": true, "dqgz/xxjy": true,
	}
	got := make(map[string]bool, len(site.SubPages))
	for _, slug := range site.SubPages {
		got[slug] = true
	}
	if len(got) != len(want) {
		t.Fatalf("phys feed count = %d, want %d (%v)", len(got), len(want), site.SubPages)
	}
	for slug := range want {
		if !got[slug] {
			t.Errorf("phys audited feed %q is missing", slug)
		}
	}
}

func TestPhysHomepageParsesAllRenderedNewsModules(t *testing.T) {
	html := []byte(`<html><head><meta name="pageType" content="1"><meta name="pageTitle" content="物理科学与技术学院"></head><body>
<div class="home1"><div class="title"><div class="con"><h3>学院动态</h3></div></div><ul><li><a href="info/1022/1.htm"><h3>学院动态首页文章</h3><p>2026.07.01</p></a></li></ul></div>
<div class="home3"><div class="left"><div class="title"><div class="con"><h3>本科生培养</h3></div></div><ul><li><a href="info/1031/2.htm"><h3>本科生培养文章</h3><span>2026.07.02</span></a></li></ul></div><div class="right"><div class="title"><div class="con"><h3>研究生培养</h3></div></div><ul><li><a href="info/1034/3.htm"><h3>研究生培养文章</h3><span>2026.07.03</span></a></li></ul></div></div>
<div class="home4"><div class="left"><div class="title"><div class="con"><h3>学工动态</h3></div></div><ul><li><a href="info/1040/4.htm"><h3>学工动态文章</h3><p>2026.07.04</p></a></li></ul></div></div></body></html>`)
	items, err := parseAdapter_physList(html, "http://phys.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("phys homepage items = %#v, want all 4 module entries", items)
	}
	if items[1].Type != "本科生培养" || items[1].Date != "2026-07-02" || items[2].Type != "研究生培养" || items[3].Type != "学工动态" {
		t.Fatalf("phys homepage metadata = %#v", items)
	}
}

func TestPhysTotalPagesFromHTMLRejectsNonList(t *testing.T) {
	if got, err := physTotalPagesFromHTML([]byte(`<html><div class="newlist1"><ul class="list"><li><a href="info/1/1.htm">x</a></li></ul><div class="p_pages"><a href="x/4.htm">4</a></div></div></html>`), "xwgg/xydt"); err != nil || got != 4 {
		t.Fatalf("phys page count = %d, %v; want 4", got, err)
	}
	if _, err := physTotalPagesFromHTML([]byte(`<html><div class="home1">首页</div></html>`), "xwgg/xydt"); err == nil {
		t.Fatal("phys homepage was accepted as an archive list")
	}
}

func TestPhysListKeepsHeadingTypeAndDatePrecision(t *testing.T) {
	html := []byte(`<html><body>
<div class="loaction"><a>首页</a><a>新闻公告</a><a>学院动态</a></div>
<ul class="list_zx"><li><a href="info/1024/9272.htm">学院动态文章标题</a><span class="time">2026-07-01 09:05</span></li></ul>
</body></html>`)
	items, err := parseAdapter_physList(html, "http://phys.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != "学院动态" || items[0].Date != "2026-07-01" || items[0].PublishedAt != "2026-07-01 09:05:00" {
		t.Fatalf("phys list item = %#v", items)
	}
}

func TestPhysArticleKeepsBodyImageAndAttachment(t *testing.T) {
	html := []byte(`<html><body><div class="loaction"><a>首页</a><a>学院动态</a></div><h1>物理学院文章标题</h1><div class="news_source"><span class="date">2026-07-01</span></div><div class="v_news_content"><p>物理学院正文内容。</p><img data-src="/images/body.jpg"><a download="附件.pdf" href="/download?id=1">下载附件</a></div></body></html>`)
	article, err := parseAdapter_physArticle(html, "http://phys.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "学院动态" || !strings.Contains(article.Content, "正文内容") || len(article.Images) != 1 || len(article.Attachments) != 1 {
		t.Fatalf("phys article = %#v", article)
	}
}
