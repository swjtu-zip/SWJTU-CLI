package scraper

import (
	"reflect"
	"strings"
	"testing"
)

func TestLeedsAdapterRegistered(t *testing.T) {
	site := AllSites["leeds"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("leeds adapter hooks are not registered")
	}
	want := []string{"index/xyxw", "xytz/dzbg", "xytz/jwjx", "xytz/xszc", "xxgs/dzbg", "xxgs/jwjx", "xxgs/xszc", "index/hdyg", "index/zsxx", "index/zyxz", "xszc/xshd", "xszc/xsfc", "xszc/gjjl", "ztjy/xjpxsdzgtsshzysxztjy/xxdt"}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("leeds feeds = %#v, want %#v", site.SubPages, want)
	}
	if got := site.PageURL(site.BaseURL, "index/xyxw", 2, 162, ".htm"); got != site.BaseURL+"/index/xyxw/161.htm" {
		t.Fatalf("leeds reverse page URL = %q", got)
	}
	if got := site.PageURL(site.BaseURL, "index/xyxw", 163, 162, ".htm"); got != "" {
		t.Fatalf("leeds out-of-range page URL = %q, want empty", got)
	}
}

func TestLeedsCurrentListTemplatesAndHomepageModules(t *testing.T) {
	nyNews := []byte(`<html><head><meta name="pageType" content="2"><meta name="pageTitle" content="学院新闻"></head><body><div class="ny_news"><ul><li><div class="txt"><h5>学院新闻完整标题</h5><span>2026.07.31</span></div><a href="../info/1032/1.htm">详情</a></li></ul><div class="pb_sys_common"><span class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="xyxw/1.htm">2</a></span></span></div></div></body></html>`)
	items, err := parseLeedsNews(nyNews, "http://leeds.swjtu.edu.cn/index/")
	if err != nil || len(items) != 1 || items[0].Title != "学院新闻完整标题" || items[0].Type != "学院新闻" || items[0].Date != "2026-07-31" {
		t.Fatalf("leeds ny_news = %#v, err=%v", items, err)
	}
	nywp := []byte(`<html><head><meta name="pageType" content="2"><meta name="pageTitle" content="活动预告"></head><body><div class="nywp"><ul class="ul-list-d3"><li><a class="con" href="../info/1044/2.htm"><div class="g-info-d"><em>20</em>2026.06</div><div class="text"><h3 class="g-tit-d">活动预告完整标题</h3></div></a></li></ul></div></body></html>`)
	items, err = parseLeedsNews(nywp, "http://leeds.swjtu.edu.cn/index/")
	if err != nil || len(items) != 1 || items[0].Date != "2026-06-20" || items[0].Type != "活动预告" {
		t.Fatalf("leeds nywp = %#v, err=%v", items, err)
	}
	home := []byte(`<html><head><meta name="pageType" content="1"></head><body><div class="news_bg"><div class="news_box1"><ul><li><div class="txt"><i>2026.07.31</i><a href="info/1032/3.htm" title="新闻完整标题">截断</a></div></li></ul></div></div><div class="noitce_bg"><div id="NoitceCenterTobox"><ul class="swiper-wrapper"><li class="swiper-slide"><dl><a href="info/1034/4.htm">通知标题</a><span>2026.09.02</span></dl></li><li class="swiper-slide"><dl><a href="info/1039/5.htm">公示标题</a><span>2026.09.01</span></dl></li></ul></div></div><div class="Events"><div class="EventsBoxLTop"><a href="info/1044/6.htm">活动标题</a></div></div></body></html>`)
	items, err = parseLeedsNews(home, "http://leeds.swjtu.edu.cn")
	if err != nil || len(items) != 4 {
		t.Fatalf("leeds homepage = %#v, err=%v", items, err)
	}
	if items[0].Title != "新闻完整标题" || items[0].Type != "学院新闻" || items[1].Type != "学院通知" || items[2].Type != "信息公示" || items[3].Type != "活动预告" {
		t.Fatalf("leeds homepage types/titles = %#v", items)
	}
}

func TestLeedsPaginationCountRejectsHomepageAndKeepsSinglePage(t *testing.T) {
	if got, err := leedsTotalPagesFromHTML([]byte(`<div class="nywp"><ul class="ul-list-d3"><li><a href="info/1/1.htm">标题</a></li></ul><div class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="x/9.htm">10</a></span></div></div>`), "index/hdyg"); err != nil || got != 10 {
		t.Fatalf("leeds total pages = %d, %v; want 10", got, err)
	}
	if _, err := leedsTotalPagesFromHTML([]byte(`<meta name="pageType" content="1"><div class="news_bg">home</div>`), "index/hdyg"); err == nil {
		t.Fatal("leeds homepage accepted as paginated list")
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

func TestLeedsArticleDoesNotUseBodyDeadlineAsPublicationDate(t *testing.T) {
	const html = `<html><body><div class="m-textq1">
<h1>迎新志愿者拟选人员名单公示</h1>
<div class="info"><span class="date">2026-09-02</span></div>
<div class="txt"><div class="v_news_content">
<p>名单公示正文。</p><p>如有异议，请于2026年9月4日17:30前反馈。</p>
</div></div></div></body></html>`
	article, err := parseLeedsArticle([]byte(html), "http://leeds.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-09-02" || article.PublishedAt != "2026-09-02" {
		t.Fatalf("body deadline replaced publication date: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
}
