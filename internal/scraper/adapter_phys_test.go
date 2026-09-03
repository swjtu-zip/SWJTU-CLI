package scraper

import (
	"strings"
	"testing"
)

func TestPhysAdapterRegistersAuditedArticleFeeds(t *testing.T) {
	site := AllSites["phys"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
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
