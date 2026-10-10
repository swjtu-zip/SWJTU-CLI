package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLeedsAdapterRegistered(t *testing.T) {
	site := AllSites["leeds"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("leeds adapter hooks are not registered")
	}
	want := []string{"index/xyxw", "xytz/dzbg", "xytz/jwjx", "xytz/xszc", "xxgs/dzbg", "xxgs/jwjx", "xxgs/xszc", "index/hdyg", "index/zsxx", "index/zyxz", "xszc/xshd", "xszc/xsfc", "xszc/gjjl", "djysz/djyd", "djysz/jszj", "ztjy/xjpxsdzgtsshzysxztjy/xxdt", "yyzj"}
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

func readLeedsFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read leeds fixture %q: %v", name, err)
	}
	return body
}

func TestLeedsMeasuredListFixturesCoverFirstMiddleLastAndNewFeeds(t *testing.T) {
	cases := []struct {
		fixture string
		slug    string
		total   int
		title   string
		date    string
	}{
		{"leeds_list_xyxw_first.html", "index/xyxw", 162, "凝心聚力 蓄力奋进——利兹学院全体教职工暑期集中攻坚两周 推进重点工作", "2026-07-31"},
		{"leeds_list_xyxw_middle.html", "index/xyxw", 162, "利兹动态｜百卅相聚展新篇 利兹大学校长韦希如一行到访利兹学院", "2026-05-18"},
		{"leeds_list_xyxw_last.html", "index/xyxw", 162, "西南交大—利兹学院正式成立", "2015-02-28"},
		{"leeds_list_djyd_first.html", "djysz/djyd", 34, "深化中外交流 共话青年成长——利兹大学校长韦希如一行与利兹学院师生开展座谈交流活动", "2026-05-16"},
		{"leeds_list_jszj_first.html", "djysz/jszj", 3, "利兹学院召开教师座谈会", "2023-06-09"},
		{"leeds_list_yyzj_first.html", "yyzj", 2, "利兹校友返校交流", "2025-05-10"},
	}
	for _, tc := range cases {
		body := readLeedsFixture(t, tc.fixture)
		items, err := parseLeedsNews(body, "https://leeds.swjtu.edu.cn")
		if err != nil {
			t.Fatalf("%s parse error: %v", tc.fixture, err)
		}
		if len(items) != 1 || items[0].Title != tc.title || items[0].Date != tc.date {
			t.Fatalf("%s items = %#v", tc.fixture, items)
		}
		if got, err := leedsTotalPagesFromHTML(body, tc.slug); err != nil || got != tc.total {
			t.Fatalf("%s pages = %d/%v, want %d", tc.fixture, got, err, tc.total)
		}
	}
}

func TestLeedsHomeEmptyWAFAndStaticResponsesAreDistinguished(t *testing.T) {
	home, err := parseLeedsNews(readLeedsFixture(t, "leeds_home_live.html"), "https://leeds.swjtu.edu.cn")
	if err != nil || len(home) != 4 {
		t.Fatalf("leeds measured home = %#v, err=%v", home, err)
	}
	if home[1].Type != "学院通知" || home[2].Type != "信息公示" || home[3].Date != "2026-03-31" {
		t.Fatalf("leeds measured home metadata = %#v", home)
	}
	empty := readLeedsFixture(t, "leeds_empty_list.html")
	if items, err := parseLeedsNews(empty, "https://leeds.swjtu.edu.cn"); err != nil || len(items) != 0 {
		t.Fatalf("leeds empty list = %#v, err=%v", items, err)
	}
	if pages, err := leedsTotalPagesFromHTML(empty, "xszc/xsfc"); err != nil || pages != 1 {
		t.Fatalf("leeds empty pages = %d/%v, want 1", pages, err)
	}
	for _, fixture := range []string{"leeds_waf.html", "leeds_static.html"} {
		if _, err := parseLeedsNews(readLeedsFixture(t, fixture), "https://leeds.swjtu.edu.cn"); err == nil {
			t.Fatalf("%s was accepted as a list", fixture)
		}
	}
}

func TestLeedsMeasuredArticleUsesFixedMetadataAndContentContainer(t *testing.T) {
	article, err := parseLeedsArticle(readLeedsFixture(t, "leeds_article_live.html"), "https://leeds.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "利兹学院2026年“利兹科技月”报名通知" || article.Date != "2026-06-09" || article.PublishedAt != "2026-06-09" {
		t.Fatalf("leeds article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "报名截止日期") || strings.Contains(article.Content, "最新阅读") {
		t.Fatalf("leeds article content escaped its fixed container: %q", article.Content)
	}
	if len(article.Images) != 2 || len(article.Attachments) != 2 {
		t.Fatalf("leeds article resources before HTTP verification = %#v / %#v", article.Images, article.Attachments)
	}
}

func TestLeedsFetchArticleVerifiesContentTypeMagicAndKeepsExternalLinks(t *testing.T) {
	page := readLeedsFixture(t, "leeds_article_live.html")
	pageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1035/22504.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
		case "/images/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		case "/images/bad.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>captcha</html>"))
		case "/files/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7 fixture"))
		case "/files/bad.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<html>验证码下载附件</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer pageServer.Close()

	site := AllSites["leeds"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	defer func() {
		site.BaseURL = oldBase
		NoCache = oldNoCache
		ClearCache()
	}()
	site.BaseURL = pageServer.URL
	NoCache = true
	ClearCache()

	article, _, err := leedsFetchArticle(pageServer.URL + "/info/1035/22504.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(article.Images) != 1 || !strings.HasSuffix(article.Images[0], "/images/good.png") {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || !strings.HasSuffix(article.Attachments[0].URL, "/files/good.pdf") || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "bad.png") || strings.Contains(article.ContentHTML, "bad.pdf") || strings.Contains(article.ContentHTML, "downloadCaptcha") {
		t.Fatalf("invalid resources remained in content html: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, "https://external.example/guide") {
		t.Fatalf("external ordinary link was not preserved: %s", article.ContentHTML)
	}
	if article.Date != "2026-06-09" || article.PublishedAt != "2026-06-09" {
		t.Fatalf("verified article date = %q/%q", article.Date, article.PublishedAt)
	}
}

func TestLeedsResourceValidationRequiresHTTPContentTypeAndMagic(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !leedsResourceBodyIsUsable("image/png", png, "good.png", true) {
		t.Fatal("valid PNG was rejected")
	}
	if leedsResourceBodyIsUsable("image/png", []byte("<html>captcha</html>"), "bad.png", true) {
		t.Fatal("HTML CAPTCHA was accepted as an image")
	}
	if !leedsResourceBodyIsUsable("application/pdf", []byte("%PDF-1.7"), "good.pdf", false) {
		t.Fatal("valid PDF was rejected")
	}
	if leedsResourceBodyIsUsable("application/pdf", []byte("not a PDF"), "bad.pdf", false) {
		t.Fatal("mismatched PDF magic was accepted")
	}
}

func TestLeedsLiveHomepageSupportsIndexFeed(t *testing.T) {
	items, err := parseLeedsNews(readLeedsFixture(t, "leeds_live_home.html"), "https://leeds.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse Leeds live homepage: %v", err)
	}
	if len(items) != 21 {
		t.Fatalf("live homepage item count = %d, want 21: %#v", len(items), items)
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, item := range items {
		counts[item.Type]++
		if seen[item.URL] {
			t.Errorf("duplicate live homepage URL: %s", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, "https://leeds.swjtu.edu.cn/info/") {
			t.Errorf("live homepage leaked non-local URL: %#v", item)
		}
	}
	for _, module := range []string{"学院新闻", "学院通知", "信息公示", "活动预告"} {
		if counts[module] == 0 {
			t.Errorf("live homepage module %q has no items: %#v", module, counts)
		}
	}

	// The homepage is a single-page preview feed, not a reverse-numbered archive.
	for _, slug := range []string{"", "index"} {
		if pages, err := leedsTotalPages(slug); err != nil || pages != 1 {
			t.Fatalf("leedsTotalPages(%q) = %d, %v; want 1, nil", slug, pages, err)
		}
	}
	if pages, err := leedsTotalPagesFromHTML(readLeedsFixture(t, "leeds_live_home.html"), "index"); err != nil || pages != 1 {
		t.Fatalf("leedsTotalPagesFromHTML(index) = %d, %v; want 1, nil", pages, err)
	}
}
