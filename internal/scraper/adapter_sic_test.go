package scraper

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestSicAdapterRegistered(t *testing.T) {
	site, ok := AllSites["sic"]
	if !ok {
		t.Fatal("sic site is not registered")
	}
	if site.BaseURL != "https://sic.swjtu.edu.cn" {
		t.Fatalf("unexpected base URL: %q", site.BaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("sic adapter hooks are incomplete: %+v", site)
	}
	wantSubPages := []string{
		"xydt", "dqgz/dqdt", "dqgz/ghzj", "dqgz/xytw", "dqgz/jwgz",
		"rcpy/bkspy/pyfa", "rcpy/bkspy/gzzd", "rcpy/yjspy/pyfa", "rcpy/yjspy/gzzd",
		"tzgg/qb", "tzgg/dzb", "tzgg/yjsjx", "tzgg/bkjx", "tzgg/ky",
		"tzgg/xsgz", "tzgg/sy", "tzgg/xshd", "tzgg/zsjy", "xsgz/xsfc",
		"xsjl", "jxhd", "cyzl",
	}
	if len(site.SubPages) != len(wantSubPages) {
		t.Fatalf("sic sub-pages = %#v, want exactly %#v", site.SubPages, wantSubPages)
	}
	for i, want := range wantSubPages {
		if site.SubPages[i] != want {
			t.Errorf("sic sub-page[%d] = %q, want %q", i, site.SubPages[i], want)
		}
	}
}

func TestSicSourceTotalPagesUsesArticleCountAndPageSize(t *testing.T) {
	html, err := os.ReadFile("testdata/sic_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseSicNews(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 10 {
		t.Fatalf("expected ten newest entries, got %d", len(items))
	}
	if got := items[0].Type; got != "学院动态" {
		t.Fatalf("list type = %q, want 学院动态", got)
	}
	if got := items[0].Date; got != "2026-08-28" {
		t.Fatalf("list date = %q, want 2026-08-28", got)
	}
	if got, err := sicTotalPagesFromHTML(html, "xydt"); err != nil || got != 17 {
		t.Fatalf("sicTotalPagesFromHTML = %d, %v; want 17", got, err)
	}
}

func TestSicZeroItemTabIsAValidSinglePage(t *testing.T) {
	html := []byte(`<html><body><h1 class="margin_auto_1">党政办</h1><span class="p_t">共0条</span></body></html>`)
	if got, err := sicTotalPagesFromHTML(html, "tzgg/dzb"); err != nil || got != 1 {
		t.Fatalf("zero-item tab pages = %d, %v; want 1", got, err)
	}
}

func TestSicPageURL(t *testing.T) {
	const base = "https://sic.swjtu.edu.cn"
	if got := sicPageURL(base, "xydt", 1, 17, ".htm"); got != base+"/xydt.htm" {
		t.Fatalf("unexpected first page URL: %q", got)
	}
	if got := sicPageURL(base, "xydt", 2, 17, ".htm"); got != base+"/xydt/16.htm" {
		t.Fatalf("unexpected second page URL: %q", got)
	}
	if got := sicPageURL(base, "xydt", 17, 17, ".htm"); got != base+"/xydt/1.htm" {
		t.Fatalf("unexpected last page URL: %q", got)
	}
}

func TestSicParseHomepageSnapshot(t *testing.T) {
	html, err := os.ReadFile("testdata/sic.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseSicNews(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) != 45 {
		t.Fatalf("expected 45 unique article links, got %d", len(items))
	}

	if got := items[0]; got.Title != "职引未来｜集成电路学院、利兹学院学子走进成都京东方研学" || got.URL != "https://sic.swjtu.edu.cn/info/1381/6641.htm" || got.Date != "2026-05-30" {
		t.Fatalf("unexpected first item: %+v", got)
	}

	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate article URL: %s", item.URL)
		}
		seen[item.URL] = true
		if !strings.Contains(item.URL, "/info/") || !strings.HasSuffix(item.URL, ".htm") {
			t.Errorf("unexpected article URL: %q", item.URL)
		}
		if item.Title == "首页" || item.Title == "查看更多+" || item.Title == "通知公告" {
			t.Errorf("navigation title leaked: %q", item.Title)
		}
		if item.Date == "" {
			t.Errorf("empty date for %q", item.Title)
			continue
		}
		if _, err := time.Parse("2006-01-02", item.Date); err != nil {
			t.Errorf("non-normalized date %q for %q", item.Date, item.Title)
		}
	}
}

func TestSicParseListFiltersBannerAndMergesDuplicate(t *testing.T) {
	html := []byte(`<html><body>
<header><a href="info/9999/1.htm" title="首页导航">首页导航</a></header>
<div class="bannerBox"><a href="info/9999/2.htm" title="横幅链接">横幅链接</a></div>
<main><ul>
	<li><a href="info/1381/1001.htm"><div class="date"><i>07</i><span>2026.08</span></div><h5>第一条学院新闻</h5></a></li>
	<li><a href="info/1381/1001.htm" title="第一条学院新闻"><i>2026.08.07</i></a></li>
	<li><a href="info/1381/1002.htm" title="第二条学院新闻"><time>2026/08/06 12:00</time></a></li>
	<li><a href="info/1381/1003.htm" title="学院纪委参与拍摄制作学校首部廉洁微短剧《守》"><time>2026/08/05</time></a></li>
</ul></main>
</body></html>`)

	items, err := parseSicNews(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected three content articles, got %d: %#v", len(items), items)
	}
	if items[0].URL != "https://sic.swjtu.edu.cn/info/1381/1001.htm" || items[0].Title != "第一条学院新闻" || items[0].Date != "2026-08-07" {
		t.Fatalf("unexpected merged item: %+v", items[0])
	}
	if items[1].URL != "https://sic.swjtu.edu.cn/info/1381/1002.htm" || items[1].Date != "2026-08-06" {
		t.Fatalf("unexpected second item: %+v", items[1])
	}
	if items[2].URL != "https://sic.swjtu.edu.cn/info/1381/1003.htm" || items[2].Date != "2026-08-05" {
		t.Fatalf("unexpected quoted-headline item: %+v", items[2])
	}
}

func TestSicParseListKeepsVerifiedForwardedArticleEntries(t *testing.T) {
	html := []byte(`<html><head><meta name="pageType" content="2"></head><body>
<div id="nav_slide"><div class="n_title"><h1 class="margin_auto_1">研究生教学</h1></div></div>
<ul>
  <li><a href="https://gsnews.swjtu.edu.cn/info/1637/31564.htm" title="【转发研究生院通知】2025-2026-1学期选课通知"><div class="time"><div class="day">05</div><div class="year">2025.09</div></div></a></li>
  <li><a href="http://jwc.swjtu.edu.cn/vatuu/WebAction?setAction=newsDetail&amp;viewType=web&amp;newsId=DC6209AAB1E99721" title="【转发教务处通知】关于高等教育自学考试全国统考期间封楼的通知"><div class="time"><div class="day">02</div><div class="year">2025.04</div></div></a></li>
  <li><a href="https://evil.example/info/1/2.htm" title="不应被采集的外域链接"><div class="time"><div class="day">01</div><div class="year">2025.01</div></div></a></li>
</ul><span class="p_t">共2条</span>
</body></html>`)

	items, err := parseSicNews(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two source entries, got %d: %#v", len(items), items)
	}
	if items[0].URL != "https://gsnews.swjtu.edu.cn/info/1637/31564.htm" || items[0].Date != "2025-09-05" || items[0].Type != "研究生教学" {
		t.Fatalf("unexpected graduate-school forwarding entry: %+v", items[0])
	}
	wantJWC := "http://jwc.swjtu.edu.cn/vatuu/WebAction?setAction=newsDetail&viewType=web&newsId=DC6209AAB1E99721"
	if items[1].URL != wantJWC || items[1].Date != "2025-04-02" || items[1].Type != "研究生教学" {
		t.Fatalf("unexpected academic-affairs forwarding entry: %+v", items[1])
	}
}

func TestSicFetchArticleRejectsExternalForwardedDetail(t *testing.T) {
	for _, rawURL := range []string{
		"https://gsnews.swjtu.edu.cn/info/1637/31564.htm",
		"http://jwc.swjtu.edu.cn/vatuu/WebAction?setAction=newsDetail&viewType=web&newsId=DC6209AAB1E99721",
	} {
		_, _, err := sicFetchArticle(rawURL)
		if err == nil || !strings.Contains(err.Error(), "external forwarded detail URL") {
			t.Errorf("sicFetchArticle(%q) error = %v, want an explicit external-forwarding error", rawURL, err)
		}
	}
}

func TestSicParseArticleNormalizesTimestamp(t *testing.T) {
	html := []byte(`<html><body>
<h1 class="news_tit">职引未来｜集成电路学院、利兹学院学子走进成都京东方研学</h1>
<div class="news_source"><span>来源：</span><span>日期：2026/05/30 16:30:21</span><span>点击数：</span></div>
<div class="v_news_content"><p>正文内容。</p></div>
</body></html>`)

	article, err := parseSicArticle(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "职引未来｜集成电路学院、利兹学院学子走进成都京东方研学" {
		t.Fatalf("unexpected article title: %q", article.Title)
	}
	if article.Date != "2026-05-30" {
		t.Fatalf("unexpected article date: %q", article.Date)
	}
	if article.Content != "正文内容。" {
		t.Fatalf("unexpected article content: %q", article.Content)
	}
}

func TestSicParseArticlePrefersDetailHeadline(t *testing.T) {
	html := []byte(`<html><body>
<h1 class="margin_auto_1">学院动态</h1>
<div class="show01"><h5>真实的集成电路学院文章标题</h5><p><em>来源：</em><em>日期：2026/08/28 17:03:55</em></p></div>
<div class="show02"><div class="v_news_content"><p>这是一段足够长的正文，用于验证详情页标题不会被侧栏栏目名覆盖。</p></div></div>
</body></html>`)
	article, err := parseSicArticle(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "真实的集成电路学院文章标题" {
		t.Fatalf("article title = %q", article.Title)
	}
}

func TestSicParseLiveArticleFixturePreservesMetadataAndResources(t *testing.T) {
	html, err := os.ReadFile("testdata/sic_article.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseSicArticle(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if article.Title != "喜报！我院首届硕士研究生在中国研究生电子设计竞赛中斩获佳绩" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Type != "学院动态" {
		t.Fatalf("type = %q, want 学院动态", article.Type)
	}
	if article.Date != "2026-08-28" || article.PublishedAt != "2026-08-28 17:03:55" {
		t.Fatalf("date metadata = (%q, %q)", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "正文第一段") || strings.Contains(article.Content, "学院动态") {
		t.Fatalf("unexpected body content: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://sic.swjtu.edu.cn/__local/example.png" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "会议通知.pdf" || article.Attachments[0].URL != "https://sic.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&wbfileid=fixture" {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, `src="https://sic.swjtu.edu.cn/__local/example.png"`) || !strings.Contains(article.ContentHTML, `href="https://sic.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&amp;wbfileid=fixture"`) {
		t.Fatalf("content HTML did not resolve resources: %s", article.ContentHTML)
	}
}

func TestSicParseNestedArticlePreservesSourceAttachment(t *testing.T) {
	html := []byte(`<html><head><meta name="pageType" content="3"></head><body>
<section id="nav_slide"><div class="n_title"><h1 class="margin_auto_1">规章制度</h1></div></section>
<section class="show"><div class="show01"><h5>西南交通大学集成电路科学与工程学院专业学位硕士研究生行业导师选聘办法</h5>
<p><em>来源：</em><em>日期：2026/04/13 11:23:45</em></p></div>
<div id="vsb_content" class="show02"><div class="v_news_content"><p>行业导师选聘办法正文。</p><p><img src="/__local/guide.jpg"></p></div></div>
<p><ul><li>附件【<a href="/system/_content/download.jsp?urltype=news.DownloadAttachUrl&amp;wbfileid=fixture">附件：行业导师信息备案表.xlsx</a>】</li></ul></p>
</section></body></html>`)

	article, err := parseSicArticle(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "西南交通大学集成电路科学与工程学院专业学位硕士研究生行业导师选聘办法" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Type != "规章制度" {
		t.Fatalf("type = %q, want 规章制度", article.Type)
	}
	if article.Date != "2026-04-13" || article.PublishedAt != "2026-04-13 11:23:45" {
		t.Fatalf("date metadata = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Content != "行业导师选聘办法正文。" {
		t.Fatalf("content = %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://sic.swjtu.edu.cn/__local/guide.jpg" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件：行业导师信息备案表.xlsx" || article.Attachments[0].URL != "https://sic.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&wbfileid=fixture" {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
}

func TestSicParseNestedListEntryAndPageTotal(t *testing.T) {
	html := []byte(`<html><body><h1 class="margin_auto_1">规章制度</h1>
<ul><li><a href="../../info/1191/6211.htm"><div class="time"><div class="day">13</div><div class="year">2026.04</div></div><div class="con"><h5>西南交通大学集成电路科学与工程学院专业学位硕士研究生行业导师选聘办法</h5></div></a></li></ul>
<span class="p_t">共1条</span></body></html>`)

	items, err := parseSicNews(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].URL != "https://sic.swjtu.edu.cn/info/1191/6211.htm" || items[0].Title != "西南交通大学集成电路科学与工程学院专业学位硕士研究生行业导师选聘办法" || items[0].Date != "2026-04-13" || items[0].Type != "规章制度" {
		t.Fatalf("nested list item = %#v", items[0])
	}
	if got, err := sicTotalPagesFromHTML(html, "rcpy/yjspy/gzzd"); err != nil || got != 1 {
		t.Fatalf("nested list pages = %d, %v; want 1", got, err)
	}
}

func TestSicRejectsNonArticlePage(t *testing.T) {
	html, err := os.ReadFile("testdata/sic_static.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSicArticle(html, "https://sic.swjtu.edu.cn"); err == nil {
		t.Fatal("static page was accepted as an article")
	}
}

func TestSicRejectsUnavailableShellsButAcceptsExplicitEmptyList(t *testing.T) {
	cases := []struct {
		name string
		html string
	}{
		{
			name: "waf",
			html: `<html><head><title>Access Denied</title></head><body>访问受限</body></html>`,
		},
		{
			name: "login",
			html: `<html><head><title>统一身份认证登录</title></head><body><form>用户名 密码 请登录</form></body></html>`,
		},
		{
			name: "spa",
			html: `<html><head><title>集成电路科学与工程学院</title></head><body><div id="app"></div><script>window.enableJavaScript = true</script></body></html>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseSicNews([]byte(tc.html), "https://sic.swjtu.edu.cn"); err == nil {
				t.Fatal("unavailable shell was accepted as a successful list")
			}
		})
	}

	empty := []byte(`<html><body><h1 class="margin_auto_1">党政办</h1><span class="p_t">共0条</span></body></html>`)
	items, err := parseSicNews(empty, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("explicit empty tab returned an error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("explicit empty tab returned %d items", len(items))
	}
}

func TestSicParseImageOnlyArticlePreservesHTMLAndImage(t *testing.T) {
	html := []byte(`<html><head><meta name="pageType" content="3"></head><body>
<section id="nav_slide"><div class="n_title"><h1 class="margin_auto_1">常用资料</h1></div></section>
<section class="show"><div class="show01"><h5>集成电路科学与工程学院标识</h5>
<p><em>来源：</em><em>日期：2024/08/05 15:32:02</em></p></div>
<div class="show02"><div class="v_news_content"><p><img src="/__local/logo.jpg"></p></div></div></section>
</body></html>`)

	article, err := parseSicArticle(html, "https://sic.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "集成电路科学与工程学院标识" || article.Type != "常用资料" {
		t.Fatalf("metadata = (%q, %q)", article.Title, article.Type)
	}
	if article.Date != "2024-08-05" || article.PublishedAt != "2024-08-05 15:32:02" {
		t.Fatalf("date metadata = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Content != "" {
		t.Fatalf("image-only body acquired fabricated text: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://sic.swjtu.edu.cn/__local/logo.jpg" {
		t.Fatalf("images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://sic.swjtu.edu.cn/__local/logo.jpg"`) {
		t.Fatalf("resolved image missing from content HTML: %s", article.ContentHTML)
	}
}
