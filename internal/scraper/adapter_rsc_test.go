package scraper

import (
	"os"
	"strings"
	"testing"
)

func readRSCFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read rsc fixture %q: %v", name, err)
	}
	return body
}

func TestRSCAdapterRegistersRecruitmentTabsAndReversePagination(t *testing.T) {
	site := AllSites["rsc"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatal("rsc adapter is incomplete")
	}
	want := []string{
		"xwtz/xwNews", "xwtz/tzNotice", "rszp/gljqtzjryzphgs", "rszp/shpyryzphgs",
		"rszp/jsjbshzphgs", "rszp/zpxx", "rszp/gsxx", "rszp/ggxx",
		"xgxz", "xgxz/zcps", "bszn", "bszn/rszc", "bszn/shbx", "bszn/szrc", "cjwt",
	}
	if strings.Join(site.SubPages, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rsc feeds = %#v, want %#v", site.SubPages, want)
	}
	if got := site.PageURL(site.BaseURL, "xwtz/xwNews", 2, 4, ".htm"); got != site.BaseURL+"/xwtz/xwNews/3.htm" {
		t.Fatalf("rsc reverse page URL = %q", got)
	}
	if got := site.PageURL(site.BaseURL, "xwtz/xwNews", 5, 4, ".htm"); got != "" {
		t.Fatalf("rsc out-of-range page URL = %q, want empty", got)
	}
}

func TestRSCHomepageKeepsLocalModulesAndTypes(t *testing.T) {
	items, err := parseAdapter_rscList(readRSCFixture(t, "rsc.html"), "https://rsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 22 {
		t.Fatalf("rsc homepage items = %d, want 22 local article cards", len(items))
	}
	types := map[string]int{}
	for _, item := range items {
		types[item.Type]++
		if strings.Contains(item.URL, "news.swjtu.edu.cn") || strings.Contains(item.URL, "jdpta.com") || strings.Contains(item.URL, "moe.gov.cn") {
			t.Fatalf("external homepage link leaked: %#v", item)
		}
	}
	for _, typ := range []string{"人事新闻", "通知公告", "社会聘用人员招聘和公示", "师资建设", "薪酬社保", "常见问题", "常见下载"} {
		if types[typ] == 0 {
			t.Fatalf("homepage type %q was not retained: %#v", typ, types)
		}
	}
}

func TestRSCListUsesArticleAnchorNotTabAnchor(t *testing.T) {
	html := []byte(`<html><body>
<div class="loaction"><a>首页</a><a>新闻通知</a><a>新闻News</a></div>
<ul class="list"><li><a href="xwtz/xwNews.htm">[新闻News]</a><a href="info/1025/9672.htm">列表文章准确标题</a><span>时间：2026-07-16 14:03</span></li><li><a href="xwtz/xwNews.htm">[新闻News]</a><a href="https://news.swjtu.edu.cn/info/1/2.htm">外部文章</a></li></ul>
</body></html>`)
	items, err := parseAdapter_rscList(html, "https://rsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "列表文章准确标题" || items[0].Type != "新闻News" || items[0].Date != "2026-07-16" || items[0].PublishedAt != "2026-07-16 14:03:00" {
		t.Fatalf("rsc list item = %#v", items)
	}
}

func TestRSCArticleKeepsSectionBodyImageAndAttachment(t *testing.T) {
	html := []byte(`<html><body><div class="loaction"><a>首页</a><a>新闻通知</a><a>新闻News</a></div><h1>人事新闻详情标题</h1><div class="news_source"><span class="date">2026-07-16</span></div><div class="v_news_content"><p>人事新闻正文内容。</p><img data-original="/images/body.jpg"><a download="通知附件.pdf" href="/download?id=1">下载附件</a></div></body></html>`)
	article, err := parseAdapter_rscArticle(html, "https://rsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "新闻News" || !strings.Contains(article.Content, "正文内容") || len(article.Images) != 1 || len(article.Attachments) != 1 {
		t.Fatalf("rsc article = %#v", article)
	}
}
