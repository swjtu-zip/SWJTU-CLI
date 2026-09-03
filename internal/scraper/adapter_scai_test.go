package scraper

import (
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
	if !strings.Contains(article.Content, "这是正文内容") {
		t.Errorf("content = %q", article.Content)
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
		_, _ = w.Write(data)
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
