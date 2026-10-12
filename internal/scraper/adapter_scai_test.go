package scraper

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestScaiAdapterList(t *testing.T) {
	data, err := os.ReadFile("testdata/scai.html")
	if err != nil {
		t.Fatal(err)
	}
	site := AllSites["scai"]
	if site == nil || site.ParseFunc == nil {
		t.Fatal("scai adapter was not registered")
	}

	items, err := site.ParseFunc(data, site.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 47 {
		t.Fatalf("got %d items, want 47 article entries from the snapshot", len(items))
	}

	if items[0].URL != "https://scai.swjtu.edu.cn/web/page-newsDetail.html?nid=0b9fffc8-b75c-4632-affb-24c45f3438e0" {
		t.Errorf("first URL = %q", items[0].URL)
	}
	if items[0].Title != "百卅校庆系列学术论坛（四）： 学院成功举办2026年科研报奖报告会暨第一期集中辅导" {
		t.Errorf("first title = %q", items[0].Title)
	}
	if items[0].Date != "2026-06-12" {
		t.Errorf("first date = %q", items[0].Date)
	}
	if items[0].PublishedAt != "2026-06-12 00:00:00" {
		t.Errorf("first published_at = %q", items[0].PublishedAt)
	}

	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate URL %q", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, "https://scai.swjtu.edu.cn/web/page-newsDetail.html?nid=") {
			t.Errorf("non-article URL %q", item.URL)
		}
		if item.Title == "" || strings.HasPrefix(item.Title, "了解详情") {
			t.Errorf("bad title %q", item.Title)
		}
		if _, err := time.Parse("2006-01-02", item.Date); err != nil {
			t.Errorf("item %q has non-ISO date %q", item.Title, item.Date)
		}
	}
}

func TestScaiAdapterArticleDate(t *testing.T) {
	const articleHTML = "<!doctype html><html><body>" +
		"<header><a href='/index.html'>首页</a></header>" +
		"<main><h1 class='news-title'>学院新闻标题</h1>" +
		"<div class='article-meta'>发布日期：Tue Jul 07 00:00:00 CST 2026</div>" +
		"<div class='content-main'><p>这是正文内容。</p>" +
		"<p>活动时间：2026年9月16日，正文中的日期不能覆盖发布日期。</p></div>" +
		"</main></body></html>"

	site := AllSites["scai"]
	if site == nil || site.ParseArticleFunc == nil {
		t.Fatal("scai article adapter was not registered")
	}
	article, err := site.ParseArticleFunc([]byte(articleHTML), site.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "学院新闻标题" {
		t.Errorf("title = %q", article.Title)
	}
	if article.Date != "2026-07-07" {
		t.Errorf("date = %q, want 2026-07-07", article.Date)
	}
	if article.PublishedAt != "2026-07-07 00:00:00" {
		t.Errorf("published_at = %q, want 2026-07-07 00:00:00", article.PublishedAt)
	}
	if !strings.Contains(article.Content, "这是正文内容") {
		t.Errorf("content = %q", article.Content)
	}
}

func TestScaiHomepageTypesFollowTheirSections(t *testing.T) {
	data, err := os.ReadFile("testdata/scai.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseScaiAdapterList(data, "https://scai.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"2026年度ACM成都分会":            "通知公告",
		"中国人工智能学会走进":               "科学研究",
		"2026届本科毕业设计（论文）相关模板":      "本科生教育",
		"2026年博士研究生招生材料评议":         "研究生教育",
		"【离校工作】计算机与人工智能学院2026届研究生": "学生工作",
	}
	found := make(map[string]bool, len(want))
	for _, item := range items {
		for marker, typ := range want {
			if strings.Contains(item.Title, marker) {
				found[marker] = true
				if item.Type != typ {
					t.Errorf("%q type = %q, want %q", item.Title, item.Type, typ)
				}
			}
		}
	}
	for marker := range want {
		if !found[marker] {
			t.Errorf("homepage fixture did not contain section marker %q", marker)
		}
	}
}

func TestScaiListPreservesPublicationMinutes(t *testing.T) {
	const html = `<div class="container"><h1 class="list-big-title">通知公告</h1>
<div class="list-item"><a href="/web/page-newsDetail.html?nid=precise-time" title="带发布时间的文章标题">带发布时间的文章标题</a>
<span class="date-label">Tue Sep 02 14:35:00 CST 2026</span></div></div>`
	items, err := parseScaiAdapterList([]byte(html), "https://scai.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want one", len(items))
	}
	if items[0].Date != "2026-09-02" || items[0].PublishedAt != "2026-09-02 14:35:00" {
		t.Fatalf("date/published_at = %q/%q", items[0].Date, items[0].PublishedAt)
	}
	if items[0].Type != "通知公告" {
		t.Fatalf("type = %q, want 通知公告", items[0].Type)
	}
}

func TestScaiListTypeUsesActiveSubTab(t *testing.T) {
	const html = `<div class="container"><h1 class="list-big-title">本科生教育
<span><a class="active" href="/web/page-module.html?mid=B730BEB095B31840&amp;tid=350">通知公告</a></span>
<span><a href="/web/page-module.html?mid=B730BEB095B31840&amp;tid=351">培养方案</a></span></h1>
<div class="list-item"><a href="/web/page-newsDetail.html?nid=active-tab" title="当前子栏目文章标题">当前子栏目文章标题</a>
<span class="date-label">2026-09-02</span></div></div>`
	items, err := parseScaiAdapterList([]byte(html), "https://scai.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != "通知公告" {
		t.Fatalf("active sub-tab type = %#v, want 通知公告", items)
	}
}

func TestScaiArticlePreservesPublicationMinutes(t *testing.T) {
	const html = `<html><body><div class="news-content-wrapper"><div class="content">
<h1 class="news-title">带发布时间的详情标题</h1>
<div class="news-info"><span>来源：计算机与人工智能学院</span><span>发布日期：Tue Sep 02 14:35:00 CST 2026</span></div>
<div class="content-main"><p>正文内容。</p><p>活动时间：2026年9月16日，不能覆盖发布日期。</p></div>
</div></div></body></html>`
	article, err := parseScaiArticle([]byte(html), "https://scai.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-09-02" || article.PublishedAt != "2026-09-02 14:35:00" {
		t.Fatalf("date/published_at = %q/%q", article.Date, article.PublishedAt)
	}
}

func TestScaiAdapterArticleDoesNotUseBodyDeadlineAsPublicationDate(t *testing.T) {
	const articleHTML = `<html><body><div class="news-content-wrapper"><div class="content">
<h1 class="news-title">推免排名成绩计算通知</h1>
<div class="news-info"><span>来源：计算机与人工智能学院</span><span>发布日期：2026-09-02</span></div>
<div class="content-main"><p>请于2026年9月4日17:00前提交材料。</p></div>
</div></div></body></html>`
	article, err := parseScaiArticle([]byte(articleHTML), "https://scai.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-09-02" {
		t.Fatalf("body deadline replaced publication date: %q", article.Date)
	}
}

func TestScaiAdapterArticleTitleUsesContentHeadline(t *testing.T) {
	data, err := os.ReadFile("testdata/scai_article.html")
	if err != nil {
		t.Fatal(err)
	}

	site := AllSites["scai"]
	if site == nil || site.ParseArticleFunc == nil {
		t.Fatal("scai article adapter was not registered")
	}
	article, err := site.ParseArticleFunc(data, site.BaseURL)
	if err != nil {
		t.Fatal(err)
	}

	const want = "计算机与人工智能学院党委理论学习中心组召开专题学习会"
	if article.Title != want {
		t.Fatalf("title = %q, want %q", article.Title, want)
	}
	if article.Title == "计算机与人工智能学院" {
		t.Fatal("article title fell back to the school name")
	}
	if article.Date != "2026-07-07" {
		t.Errorf("date = %q, want 2026-07-07", article.Date)
	}
	if article.Type != "学院新闻" {
		t.Errorf("type = %q, want 学院新闻", article.Type)
	}
	if !strings.Contains(article.Content, "学院召开党委理论学习中心组") {
		t.Errorf("content = %q", article.Content)
	}
}

func TestScaiAdapterListUsesModuleRowsAndRejectsWAFPage(t *testing.T) {
	data, err := os.ReadFile("testdata/scai_module.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseScaiAdapterList(data, "https://scai.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 real module rows", len(items))
	}
	if items[0].Title != "置顶真实文章标题" || items[0].Date != "2026-08-03" {
		t.Errorf("top item = %#v", items[0])
	}
	if items[1].Title != "普通列表真实文章标题" || items[1].Date != "2026-07-07" {
		t.Errorf("ordinary item = %#v", items[1])
	}
	if items[0].Type != "学院新闻" || items[1].Type != "学院新闻" {
		t.Errorf("types = %q/%q", items[0].Type, items[1].Type)
	}

	if _, err := parseScaiAdapterList([]byte(`<html><head><title>您的访问请求可能对网站造成安全威胁，请求已被阻断。</title></head></html>`), "https://scai.swjtu.edu.cn"); err == nil {
		t.Fatal("WAF block page was accepted as an empty feed")
	}
}

func TestScaiAdapterParsesSplitLiveCalendarDate(t *testing.T) {
	html := []byte(`<div class="container"><h1 class="list-big-title">学院新闻</h1>
<div class="list-top-item"><div class="calendar"><div class="day">03</div><div class="date">2026/08</div></div>
<a href="/web/page-newsDetail.html?nid=split-date">分栏日期文章标题</a></div></div>`)
	items, err := parseScaiAdapterList(html, "https://scai.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Date != "2026-08-03" {
		t.Fatalf("split calendar item = %#v, want date 2026-08-03", items)
	}
}

func TestScaiPageURLPreservesSubTabAndUsesIndexHTML(t *testing.T) {
	base := "https://scai.swjtu.edu.cn"
	if got := scaiPageURL(base, "index", 1, 0, ".htm"); got != base+"/index.html" {
		t.Fatalf("index page URL = %q", got)
	}
	if got := scaiPageURL(base, "undergraduate/notice", 2, 31, ".htm"); got != base+"/web/page-module.html?mid=B730BEB095B31840&page=2&swjtu_adapter_page=2&tid=350" {
		t.Fatalf("sub-tab page URL = %q", got)
	}
	if got := scaiPageURL(base, "notice/files", 7, 7, ".html"); got != base+"/web/page-module.html?mid=F4DFBF47DF89091A&page=7&swjtu_adapter_page=7&tid=360" {
		t.Fatalf("notice file-tab page URL = %q", got)
	}
}

func TestScaiTotalPagesFromModuleFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/scai_pagination.html")
	if err != nil {
		t.Fatal(err)
	}
	if got := scaiTotalPagesFromHTML(data); got != 15 {
		t.Fatalf("total pages = %d, want 15", got)
	}
}

func TestScaiListNewsRangeUsesTotalPagesHook(t *testing.T) {
	data, err := os.ReadFile("testdata/scai_pagination.html")
	if err != nil {
		t.Fatal(err)
	}

	site := AllSites["scai"]
	if site == nil || site.TotalPagesFunc == nil {
		t.Fatal("scai total-pages hook was not registered")
	}
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
		ClearCache()
	})

	var requestedPages []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if value := r.URL.Query().Get("page"); value != "" {
			page, _ = strconv.Atoi(value)
		}
		requestedPages = append(requestedPages, page)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		pageHTML := strings.Replace(string(data), "当前第【1】页", "当前第【"+strconv.Itoa(page)+"】页", 1)
		_, _ = w.Write([]byte(pageHTML))
	}))
	defer server.Close()

	site.BaseURL = server.URL
	NoCache = true
	if _, err := site.ListNewsRange("news", "2026-01-01", "2026-12-31", 0); err != nil {
		t.Fatal(err)
	}

	maxPage := 0
	for _, page := range requestedPages {
		if page > maxPage {
			maxPage = page
		}
	}
	if maxPage != 15 {
		t.Fatalf("ListNewsRange reached page %d (requests=%v), want page 15", maxPage, requestedPages)
	}
}

func TestScaiListPageRetriesAStalePageResponse(t *testing.T) {
	data, err := os.ReadFile("testdata/scai_pagination.html")
	if err != nil {
		t.Fatal(err)
	}
	site := AllSites["scai"]
	if site == nil {
		t.Fatal("scai site was not registered")
	}
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
		ClearCache()
	})

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		page := 1
		if r.URL.Query().Get("scai_adapter_retry") != "" {
			page = 2
		}
		pageHTML := strings.Replace(string(data), "当前第【1】页", "当前第【"+strconv.Itoa(page)+"】页", 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pageHTML))
	}))
	defer server.Close()

	site.BaseURL = server.URL
	NoCache = true
	items, total, err := scaiListPage("news", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || total != 15 || requests != 2 {
		t.Fatalf("items=%d total=%d requests=%d, want 2/15/2", len(items), total, requests)
	}
}

func TestScaiListPageRejectsPersistentPageRepeat(t *testing.T) {
	data, err := os.ReadFile("testdata/scai_pagination.html")
	if err != nil {
		t.Fatal(err)
	}
	site := AllSites["scai"]
	if site == nil {
		t.Fatal("scai site was not registered")
	}
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
		ClearCache()
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	}))
	defer server.Close()

	site.BaseURL = server.URL
	NoCache = true
	if _, _, err := scaiListPage("news", 2); err == nil || !strings.Contains(err.Error(), "requested page 2") {
		t.Fatalf("persistent stale page error = %v", err)
	}
}

func TestScaiEmptyFeedIsNotReportedAsPaginationFailure(t *testing.T) {
	const emptyHTML = `<html><body><div class="container"><h1 class="list-big-title">资料下载</h1>
<!-- 共【0】条，20条/页，共【0】页，当前第【0】页 --></div></body></html>`
	site := AllSites["scai"]
	if site == nil {
		t.Fatal("scai site was not registered")
	}
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
		ClearCache()
	})

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(emptyHTML))
	}))
	defer server.Close()

	site.BaseURL = server.URL
	NoCache = true
	items, err := site.ListNewsRange("undergraduate/downloads", "2026-01-01", "2026-12-31", 0)
	if err != nil {
		t.Fatalf("valid zero-item feed returned an error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("got %d items from an explicitly empty feed", len(items))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want page 1 parse plus page-count fetch", requests)
	}
}

func TestScaiRejectsUnavailableAndTemplatePages(t *testing.T) {
	if _, err := parseScaiAdapterList([]byte(`<html><body><div class="header">统一身份认证登录</div></body></html>`), "https://scai.swjtu.edu.cn"); !errors.Is(err, errScaiUnavailablePage) {
		t.Fatalf("login page error = %v, want %v", err, errScaiUnavailablePage)
	}
	if _, err := parseScaiAdapterList([]byte(`<html><body><div id="app"></div></body></html>`), "https://scai.swjtu.edu.cn"); !errors.Is(err, errScaiUnavailablePage) {
		t.Fatalf("SPA shell error = %v, want %v", err, errScaiUnavailablePage)
	}
	if _, err := parseScaiArticle([]byte(`<html><body><header>首页</header><div class="footer">版权所有</div></body></html>`), "https://scai.swjtu.edu.cn"); !errors.Is(err, errScaiNoArticleDetail) {
		t.Fatalf("template shell error = %v, want %v", err, errScaiNoArticleDetail)
	}
}

func TestScaiAdapterArticleSeparatesBodyAndAttachments(t *testing.T) {
	data, err := os.ReadFile("testdata/scai_detail_resources.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseScaiArticle(data, "https://scai.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "带图片和附件的真实详情标题" {
		t.Errorf("title = %q", article.Title)
	}
	if article.Source != "计算机与人工智能学院" {
		t.Errorf("source = %q", article.Source)
	}
	if article.Date != "2025-11-05" || article.PublishedAt != "2025-11-05 00:00:00" {
		t.Errorf("date/published_at = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || !strings.Contains(article.Content, "这是正文第二段") {
		t.Errorf("content = %q", article.Content)
	}
	if strings.Contains(article.Content, "相关文章标题") || strings.Contains(article.Content, "正文模板.docx") {
		t.Errorf("non-body text leaked into content = %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://scai.swjtu.edu.cn/attached/newsImage/2025/11/body.png" {
		t.Errorf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if article.Attachments[0].Name != "正文模板.docx" || article.Attachments[0].URL != "https://scai.swjtu.edu.cn/web/download?m=downLoad&fid=fixture-file-id" {
		t.Errorf("attachment = %#v", article.Attachments[0])
	}
	if !strings.Contains(article.ContentHTML, `src="https://scai.swjtu.edu.cn/attached/newsImage/2025/11/body.png"`) || !strings.Contains(article.ContentHTML, `href="https://scai.swjtu.edu.cn/web/download?m=downLoad&amp;fid=fixture-file-id"`) {
		t.Errorf("content HTML did not retain resolved resources = %s", article.ContentHTML)
	}
}

func TestScaiDelegatedArticleSiteResolvesOwners(t *testing.T) {
	scaiBase := "https://scai.swjtu.edu.cn"
	if site, delegated, err := scaiDelegatedArticleSite("https://scai.swjtu.edu.cn/web/page-newsDetail.html?nid=abc", scaiBase); err != nil || delegated || site != nil {
		t.Fatalf("same-host URL delegated: site=%v delegated=%v err=%v", site, delegated, err)
	}
	site, delegated, err := scaiDelegatedArticleSite("https://news.swjtu.edu.cn/info/1019/103815.htm", scaiBase)
	if err != nil || !delegated || site != NewsSite {
		t.Fatalf("news redirect resolved to %v delegated=%v err=%v", site, delegated, err)
	}
	site, delegated, err = scaiDelegatedArticleSite("http://jwc.swjtu.edu.cn/vatuu/WebAction?setAction=newsDetail&newsId=0F2CEABCE895D0BA", scaiBase)
	if err != nil || !delegated || site != AllSites["jwc"] {
		t.Fatalf("jwc redirect resolved to %v delegated=%v err=%v", site, delegated, err)
	}
	if _, _, err := scaiDelegatedArticleSite("https://example.com/info/1/2.htm", scaiBase); err == nil {
		t.Fatal("unrecognized redirect host was not reported")
	}
}

// scaiRedirectFixtureServer serves a scai detail endpoint that 302s to the
// same test server under a different loopback hostname (127.0.0.1 vs
// localhost), so hostname-based parser delegation can be exercised locally.
func scaiRedirectFixtureServer(t *testing.T, detailPath, redirectPath string, fixture []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case detailPath:
			location := "http://" + strings.Replace(r.Host, "127.0.0.1", "localhost", 1) + redirectPath
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusFound)
		case redirectPath:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(fixture)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestScaiFetchArticleFollowsRedirectToNewsSite(t *testing.T) {
	fixture, err := os.ReadFile("testdata/scai_redirect_news.html")
	if err != nil {
		t.Fatal(err)
	}
	server := scaiRedirectFixtureServer(t, "/web/page-newsDetail.html", "/info/1019/103815.htm", fixture)

	site := AllSites["scai"]
	oldScaiBase, oldNewsBase := site.BaseURL, NewsSite.BaseURL
	defer func() {
		site.BaseURL, NewsSite.BaseURL = oldScaiBase, oldNewsBase
	}()
	site.BaseURL = server.URL
	NewsSite.BaseURL = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)

	article, raw, err := site.GetArticleSnapshot(server.URL + "/web/page-newsDetail.html?nid=26244774-deb8-40f2-a9b5-a68ae6a71d41")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("redirected article raw body was not retained")
	}
	if article.Title != "【创源大讲堂】释放城市数据要素价值、驱动时空智能" {
		t.Errorf("title = %q", article.Title)
	}
	if article.Date != "2026-05-12" {
		t.Errorf("date = %q", article.Date)
	}
	if !strings.Contains(article.Content, "讲座时间") {
		t.Errorf("content = %q", article.Content)
	}
}

func TestScaiFetchArticleFollowsRedirectToJwc(t *testing.T) {
	fixture, err := os.ReadFile("testdata/scai_redirect_jwc.html")
	if err != nil {
		t.Fatal(err)
	}
	server := scaiRedirectFixtureServer(t, "/web/page-newsDetail.html", "/vatuu/WebAction", fixture)

	site := AllSites["scai"]
	jwc := AllSites["jwc"]
	oldScaiBase, oldJwcBase := site.BaseURL, jwc.BaseURL
	defer func() {
		site.BaseURL, jwc.BaseURL = oldScaiBase, oldJwcBase
	}()
	site.BaseURL = server.URL
	jwc.BaseURL = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)

	article, _, err := site.GetArticleSnapshot(server.URL + "/web/page-newsDetail.html?nid=d727f61b-4eaf-464e-826c-42ffac5efc4b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(article.Title, "市场调查与分析大赛") {
		t.Errorf("title = %q", article.Title)
	}
	if article.Date == "" || !strings.Contains(article.Content, "全国大学生市场调查") {
		t.Errorf("date/content = %q / %q", article.Date, article.Content)
	}
}

func TestScaiFetchArticleKeepsSameHostDetail(t *testing.T) {
	fixture, err := os.ReadFile("testdata/scai_article.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	site := AllSites["scai"]
	oldBase := site.BaseURL
	defer func() { site.BaseURL = oldBase }()
	site.BaseURL = server.URL

	article, _, err := site.GetArticleSnapshot(server.URL + "/web/page-newsDetail.html?nid=0b9fffc8-b75c-4632-affb-24c45f3438e0")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "计算机与人工智能学院党委理论学习中心组召开专题学习会" {
		t.Errorf("title = %q", article.Title)
	}
}

func TestScaiFetchArticleRejectsDeadDetailShell(t *testing.T) {
	fixture, err := os.ReadFile("testdata/scai_dead_shell.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	site := AllSites["scai"]
	oldBase := site.BaseURL
	defer func() { site.BaseURL = oldBase }()
	site.BaseURL = server.URL

	if _, _, err := site.GetArticleSnapshot(server.URL + "/web/page-newsDetail.html?nid=38584b19-9e4-49f2-9a58-39818a9a61f2"); !errors.Is(err, errScaiNoArticleDetail) {
		t.Fatalf("dead shell error = %v, want %v", err, errScaiNoArticleDetail)
	}
}

func TestScaiFetchArticleArchivesExternalRedirectNote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/web/page-newsDetail.html":
			w.Header().Set("Location", "https://tdxl.neea.edu.cn/some/exam/page")
			w.WriteHeader(http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	site := AllSites["scai"]
	oldBase := site.BaseURL
	defer func() { site.BaseURL = oldBase }()
	site.BaseURL = server.URL

	article, raw, err := site.GetArticleSnapshot(server.URL + "/web/page-newsDetail.html?nid=7b562bc1-2765-4d27-96b9-f4dee437726e")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(article.Title, "网页重定向到") || !strings.Contains(article.Title, "tdxl.neea.edu.cn") {
		t.Errorf("title = %q", article.Title)
	}
	if !strings.Contains(article.Content, "https://tdxl.neea.edu.cn/some/exam/page") {
		t.Errorf("content = %q", article.Content)
	}
	if len(raw) != 0 {
		t.Errorf("raw = %d bytes, want none for an external redirect", len(raw))
	}
}
