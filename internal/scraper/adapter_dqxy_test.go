package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDqxyAdapterRegistration(t *testing.T) {
	site, ok := AllSites["dqxy"]
	if !ok {
		t.Fatal("dqxy site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil || site.TotalPagesFunc == nil {
		t.Fatal("dqxy adapter hooks are incomplete")
	}
	if len(site.SubPages) != len(dqxySubPages) {
		t.Fatalf("registered sub-pages = %d, want %d", len(site.SubPages), len(dqxySubPages))
	}
	for i, want := range dqxySubPages {
		if site.SubPages[i] != want {
			t.Errorf("sub-page[%d] = %q, want %q", i, site.SubPages[i], want)
		}
	}
	for _, want := range []string{
		"dqgz1/djgz/djdt",
		"dqgz1/jwgz/gzdt",
		"dqgz1/ghgz",
		"dqgz1/ltgz",
		"dqgz1/tzgz",
		"xsgz1/xgdw/xgdt",
		"zsjy/jygz",
		"xyyd/xyhd",
		"cyxz/zlwj/djgz",
	} {
		found := false
		for _, slug := range site.SubPages {
			if slug == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("confirmed live list route %q is not registered", want)
		}
	}

	const base = "http://dqxy.swjtu.edu.cn"
	if got := site.PageURL(base, "xwdt/qb", 1, 21, ".htm"); got != base+"/xwdt/qb.htm" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 2, 21, ".htm"); got != base+"/xwdt/qb/20.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 21, 21, ".htm"); got != base+"/xwdt/qb/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := site.PageURL(base, "xsgz1/xsfc1", 2, 53, ".htm"); got != base+"/xsgz1/xsfc1/52.htm" {
		t.Fatalf("xsgz1 page 2 URL = %q", got)
	}
	for _, tc := range []struct {
		slug  string
		page  int
		total int
		want  string
	}{
		{"dqgz1/djgz/djdt", 2, 11, base + "/dqgz1/djgz/djdt/10.htm"},
		{"dqgz1/jwgz/gzdt", 2, 2, base + "/dqgz1/jwgz/gzdt/1.htm"},
		{"xsgz1/xgdw/xgdt", 2, 14, base + "/xsgz1/xgdw/xgdt/13.htm"},
		{"zsjy/jygz", 2, 53, base + "/zsjy/jygz/52.htm"},
	} {
		if got := site.PageURL(base, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("%s page %d URL = %q, want %q", tc.slug, tc.page, got, tc.want)
		}
	}
	if got := site.PageURL(base, "xyyd/xyhd", 2, 1, ".htm"); got != "" {
		t.Errorf("single-page alumni feed page 2 URL = %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 2, 0, ".htm"); got != "" {
		t.Fatalf("unknown total must not repeat page 1: %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 22, 21, ".htm"); got != "" {
		t.Fatalf("out-of-range page must not clamp to an old page: %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 0, 21, ".htm"); got != "" {
		t.Fatalf("non-positive page must be rejected: %q", got)
	}
	if got, err := site.TotalPagesFunc("index"); err != nil || got != 1 {
		t.Fatalf("homepage total = %d, err %v; want one page", got, err)
	}
}

func TestDqxyPageCountAcceptsOnlyRecognizedListPagination(t *testing.T) {
	paged := []byte(`<main class="tzgg_page"><div class="tzgg_page_base">
<div class="item"><a href="/info/2926/200001.htm">真实文章</a></div>
</div><div class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="qb/20.htm">2</a></span><span class="p_no"><a href="qb/1.htm">21</a></span></div></main>`)
	if got := dqxyTotalPagesFromHTML(paged); got != 21 {
		t.Fatalf("paged dqxy total = %d, want 21", got)
	}

	empty := []byte(`<main class="tzgg_page"><div class="tzgg_page_base"><div class="item"></div></div>
<div class="p_pages"><span class="p_no_d">1</span></div></main>`)
	if got := dqxyTotalPagesFromHTML(empty); got != 1 {
		t.Fatalf("empty dqxy total = %d, want 1", got)
	}
	emptyWithoutControls := []byte(`<main class="tzgg_page"><div class="tzgg_page_base"></div></main>`)
	if got := dqxyTotalPagesFromHTML(emptyWithoutControls); got != 1 {
		t.Fatalf("empty single-page dqxy total = %d, want 1", got)
	}

	contactPaged := []byte(`<main class="contact_us_page"><div class="campusally_area">
<div class="item"><a href="/info/1576/200001.htm">就业文章</a></div>
</div><div class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="jygz/1.htm">53</a></span></div></main>`)
	if got := dqxyTotalPagesFromHTML(contactPaged); got != 53 {
		t.Fatalf("contact-list total = %d, want 53", got)
	}

	contactSingle := []byte(`<main class="contact_us_page"><div class="campusally_area">
<div class="item"><a href="/info/1876/200002.htm">校友活动</a></div>
</div></main>`)
	if got := dqxyTotalPagesFromHTML(contactSingle); got != 1 {
		t.Fatalf("single contact-list total = %d, want 1", got)
	}

	if got := dqxyTotalPagesFromHTML([]byte(`<main><div class="content">静态页面</div></main>`)); got != 0 {
		t.Fatalf("static dqxy page total = %d, want 0", got)
	}
	if got := dqxyTotalPagesFromHTML([]byte(`<html><title>请完成验证</title><body>验证码</body></html>`)); got != 0 {
		t.Fatalf("WAF shell dqxy total = %d, want 0", got)
	}
}

func TestDqxyAdapterParsesContactListDates(t *testing.T) {
	const fixture = `<main class="contact_us_page"><div class="campusally_area">
<div class="item"><div class="date"><span class="month">10-13</span><span class="year">2025</span></div><a href="/info/1576/229639.htm" class="a_block line1">国家电网校园宣讲公告</a></div>
<div class="item"><div class="date"><span class="month">01-06</span><span class="year">2026</span></div><a href="/info/1876/231699.htm" class="a_block line1">校友活动</a></div>
</div></main>`

	items, err := parseDqxyNews([]byte(fixture), "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Title != "国家电网校园宣讲公告" || items[0].Date != "2025-10-13" {
		t.Errorf("first contact item = %#v", items[0])
	}
	if items[1].Title != "校友活动" || items[1].Date != "2026-01-06" {
		t.Errorf("second contact item = %#v", items[1])
	}
}

func TestDqxyListPageHydratesShortDatesBeforeRangePagination(t *testing.T) {
	article, err := os.ReadFile("testdata/dqxy_article.html")
	if err != nil {
		t.Fatal(err)
	}
	const list = `<main class="tzgg_page"><div class="tzgg_page_base">
<div class="item"><a href="/info/2926/234499.htm"><p class="a_block line1"><span class="lanmu">【学院新闻】</span>最新文章</p><div class="date">07-19</div></a></div>
<div class="item"><a href="/info/2926/234489.htm"><p class="a_block line1"><span class="lanmu">【学院新闻】</span>较早文章…</p><div class="date">05-17</div></a></div>
</div><div class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="qb/1.htm">2</a></span></div></main>`

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/xwdt/qb.htm":
			_, _ = w.Write([]byte(list))
		case "/info/2926/234499.htm", "/info/2926/234489.htm":
			_, _ = w.Write(article)
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}))
	defer server.Close()

	site := AllSites["dqxy"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	oldListPage := site.ListPageFunc
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL = oldBaseURL
		site.ListPageFunc = oldListPage
		NoCache = oldNoCache
		ClearCache()
	})
	site.BaseURL = server.URL
	NoCache = true

	items, total, err := dqxyListPage("xwdt/qb", 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("hydrated list = %d items/%d pages, want 2/2", len(items), total)
	}
	if items[0].Date != "2026-07-19" || items[1].Date != "2026-05-17" {
		t.Fatalf("hydrated dates = %q, %q", items[0].Date, items[1].Date)
	}
	if items[1].Title == "较早文章…" || items[1].Title == "" {
		t.Fatalf("truncated title was not repaired: %q", items[1].Title)
	}

	requests = nil
	items, err = site.ListNewsRange("xwdt/qb", "2026-07-01", "2026-09-04", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Date != "2026-07-19" {
		t.Fatalf("recent range = %#v, want the one recent article", items)
	}
	for _, requestPath := range requests {
		if requestPath == "/xwdt/1.htm" {
			t.Fatalf("range pagination visited an older page after date hydration: %#v", requests)
		}
	}
}

func TestDqxyAdapterList(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseDqxyNews(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("got %d list items, want 20", len(items))
	}

	wantFirstTitle := "聚焦前沿交叉，激发创新动能——西南交通大学2026年“智驭交通·能动未来”创新领军工程硕博士论坛顺利举行"
	if items[0].URL != "http://dqxy.swjtu.edu.cn/info/2926/234499.htm" {
		t.Errorf("first URL = %q", items[0].URL)
	}
	if items[0].Title != wantFirstTitle {
		t.Errorf("first title = %q", items[0].Title)
	}
	if items[0].Date != "2026-07-19" {
		t.Errorf("first date = %q", items[0].Date)
	}
	if items[0].Type != "学院新闻" {
		t.Errorf("first type = %q", items[0].Type)
	}
	if items[len(items)-1].URL != "http://dqxy.swjtu.edu.cn/info/2926/233409.htm" {
		t.Errorf("last URL = %q", items[len(items)-1].URL)
	}
	if items[len(items)-1].Date != "2026-05-17" {
		t.Errorf("last date = %q", items[len(items)-1].Date)
	}

	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate URL %q", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, "http://dqxy.swjtu.edu.cn/info/") {
			t.Errorf("non-article URL %q", item.URL)
		}
		if len(item.Date) != 10 || item.Date[4] != '-' || item.Date[7] != '-' {
			t.Errorf("item %q has non-ISO date %q", item.Title, item.Date)
		}
		if item.Title == "全部" || item.Title == "学院新闻" || item.Title == "党建动态" {
			t.Errorf("navigation title leaked: %q", item.Title)
		}
	}
}

func TestDqxyAdapterDoesNotInventYearOrKeepNavigation(t *testing.T) {
	const fixture = `<main class="tzgg_page">
  <div class="team_base tzgg_page_base">
    <div class="item"><a href="../info/2926/200001.htm"><p class="a_block line1"><span class="lanmu">【学院新闻】</span>一条没有完整年份的新闻</p><div class="date">12-31</div></a></div>
  </div>
</main>
<nav><div class="item"><a href="../info/2926/200002.htm">学院新闻</a></div></nav>`

	items, err := parseDqxyNews([]byte(fixture), "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want one article", len(items))
	}
	if items[0].Title != "一条没有完整年份的新闻" {
		t.Fatalf("title = %q", items[0].Title)
	}
	if items[0].Date != "" {
		t.Fatalf("date = %q, want empty because source has only MM-DD", items[0].Date)
	}
}

func TestDqxyAdapterArticle(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_article.html")
	if err != nil {
		t.Fatal(err)
	}
	art, err := parseDqxyArticle(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if art.Title != "聚焦前沿交叉，激发创新动能——西南交通大学2026年“智驭交通·能动未来”创新领军工程硕博士论坛顺利举行" {
		t.Errorf("title = %q", art.Title)
	}
	if art.Date != "2026-07-19" {
		t.Errorf("date = %q, want 2026-07-19", art.Date)
	}
	if art.Type != "学院新闻" {
		t.Errorf("type = %q, want 学院新闻", art.Type)
	}
	if art.PublishedAt != "2026-07-19 16:03:00" {
		t.Errorf("published_at = %q, want source time preserved", art.PublishedAt)
	}
	if len(art.Images) != 5 {
		t.Errorf("images = %d, want 5", len(art.Images))
	}
	if len(art.Content) < 100 {
		t.Errorf("content too short: %d", len(art.Content))
	}
}

func TestDqxyAdapterHomepageScope(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_home.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseDqxyNews(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d homepage items, want 2 bounded news cards", len(items))
	}
	if items[0].URL != "http://dqxy.swjtu.edu.cn/info/2926/234499.htm" {
		t.Errorf("first homepage URL = %q", items[0].URL)
	}
	if items[0].Type != "学院新闻" || items[1].Type != "学院新闻" {
		t.Errorf("homepage types = %q, %q", items[0].Type, items[1].Type)
	}
	for _, item := range items {
		if strings.Contains(item.Title, "导航伪文章") {
			t.Errorf("navigation item leaked into homepage results: %q", item.Title)
		}
	}
}

func TestDqxyAdapterReadsPlainSpanDateWithoutGuessingYear(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_home.html")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseDqxyNewsRows(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].MonthDay != 719 || rows[1].MonthDay != 717 {
		t.Fatalf("homepage month/day values = %#v, want 07-19 and 07-17", rows)
	}
	for i, row := range rows {
		if row.Date != "" {
			t.Errorf("homepage row %d invented a year: %q", i, row.Date)
		}
	}
}

func TestDqxyAdapterDoesNotTurnEmptyListIntoLatestUpdates(t *testing.T) {
	const fixture = `<main class="tzgg_page">
<div class="tzgg_page_base"></div>
<div class="latest-update"><ul><li><a href="/info/2926/234499.htm">不属于空列表的重复文章</a><time>2026年07月19日</time></li></ul></div>
</main>`
	items, err := parseDqxyNews([]byte(fixture), "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list produced %#v; latest-update must not be used as a fallback", items)
	}
}

func TestDqxyAdapterRejectsUnrecognizedResponse(t *testing.T) {
	_, err := parseDqxyNews([]byte(`<html><head><title>请登录</title></head><body>登录后继续</body></html>`), "http://dqxy.swjtu.edu.cn")
	if err == nil || !strings.Contains(err.Error(), "WAF/login/SPA/template shell") {
		t.Fatalf("unrecognized response error = %v", err)
	}
}

func TestDqxyAdapterPreservesListPublicationTime(t *testing.T) {
	const fixture = `<main class="tzgg_page"><div class="tzgg_page_base">
<div class="item"><a href="/info/2926/234499.htm"><p class="a_block line1">带发布时间的新闻</p><time>2026年07月19日 16:03</time></a></div>
</div></main>`
	items, err := parseDqxyNews([]byte(fixture), "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Date != "2026-07-19" || items[0].PublishedAt != "2026-07-19 16:03:00" {
		t.Fatalf("list date/time = %#v", items)
	}
}

func TestDqxyAdapterUsesExplicitDetailCategoryForTopLevelBreadcrumb(t *testing.T) {
	const fixture = `<main class="news_detail_page"><div class="content-left">
<div class="top-nav"><a href="/">首页</a><a href="/zsjy/zsgz.htm">招生就业</a><span>正文</span></div>
<div class="writing"><h4 class="else-title">【专场招聘会】校园宣讲公告</h4>
<div class="little-tips"><span>2025年10月13日 11:16</span></div>
<div class="v_news_content"><p>招聘公告正文。</p></div></div></div></main>`
	article, err := parseDqxyArticle([]byte(fixture), "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "专场招聘会" || article.Date != "2025-10-13" || article.PublishedAt != "2025-10-13 11:16:00" {
		t.Fatalf("detail category/date/time = %#v", article)
	}
}

func TestDqxyAdapterExternalAttachments(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_article_attachments.html")
	if err != nil {
		t.Fatal(err)
	}
	art, err := parseDqxyArticle(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if art.Title != "关于组织院内创新大赛选拔赛的通知" {
		t.Errorf("title = %q", art.Title)
	}
	if art.Date != "2024-05-10" || art.PublishedAt != "2024-05-10 15:33:00" {
		t.Errorf("date/published_at = %q/%q", art.Date, art.PublishedAt)
	}
	if art.Type != "通知公告" {
		t.Errorf("type = %q, want 通知公告", art.Type)
	}
	if len(art.Content) < 20 {
		t.Errorf("content too short: %d", len(art.Content))
	}
	if len(art.Images) != 1 || art.Images[0] != "http://dqxy.swjtu.edu.cn/__local/test.png" {
		t.Errorf("images = %#v", art.Images)
	}
	if len(art.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(art.Attachments))
	}
	if art.Attachments[0].Name != "附件1：报名表.docx" || !strings.Contains(art.Attachments[0].URL, "wbfileid=DOC1") {
		t.Errorf("first attachment = %#v", art.Attachments[0])
	}
	if art.Attachments[1].Name != "附件2：汇总表.xlsx" || !strings.Contains(art.Attachments[1].URL, "wbfileid=XLS2") {
		t.Errorf("second attachment = %#v", art.Attachments[1])
	}
}

func TestDqxyAdapterFiltersPresentationImages(t *testing.T) {
	images := filterDqxyImages([]string{
		"https://dqxy.swjtu.edu.cn/__local/real.png",
		"https://dqxy.swjtu.edu.cn/_mediafile/%7Bownername%7D/ueditor/upload/image/20170101/broken.png",
		"https://dqxy.swjtu.edu.cn/system/resource/images/fileTypeImages/icon_pdf.gif",
		"https://dqxy.swjtu.edu.cn/doc.gif",
		"https://dqxy.swjtu.edu.cn/spacer.gif",
		"https://dqxy.swjtu.edu.cn/KopPONA92JcsjYP6auanFNnx.files/file0003NULL",
		"https://dqxy.swjtu.edu.cn/png;base64,R0lGODlhTQIDAHcA",
	})
	if len(images) != 1 || images[0] != "https://dqxy.swjtu.edu.cn/__local/real.png" {
		t.Fatalf("filtered images = %#v", images)
	}
}
