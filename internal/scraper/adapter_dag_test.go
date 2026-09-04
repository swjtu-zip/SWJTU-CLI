package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestDagAdapterRegistered(t *testing.T) {
	site := AllSites["dag"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("dag adapter hooks are not registered")
	}
	if site.BaseURL != dagCanonicalBaseURL {
		t.Fatalf("base URL = %q, want %q", site.BaseURL, dagCanonicalBaseURL)
	}
	if site.ListPageFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("dag custom list/article fetchers are not registered")
	}
	if len(site.SubPages) != len(dagSubPages) {
		t.Fatalf("got %d dag sub-pages, want %d", len(site.SubPages), len(dagSubPages))
	}
	if _, ok := dagRouteForSlug("links"); ok {
		t.Fatal("友情链接 must not be registered as an article feed")
	}
	if _, ok := dagRouteForSlug("lecture-appointment"); !ok {
		t.Fatal("hidden lecture appointment article is not registered")
	}
}

func TestDagListSnapshotKeepsRealArticlesAndDates(t *testing.T) {
	html, err := os.ReadFile("testdata/dag.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseDagNews(html, "http://dag.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("got %d items, want 8 real archive articles: %#v", len(items), items)
	}
	if items[0].Title != "关于校史博物馆常态化开放的通知" {
		t.Fatalf("unexpected first title: %q", items[0].Title)
	}
	if items[0].URL != "http://dag.swjtu.edu.cn/info/1211/6901.htm" {
		t.Fatalf("unexpected first URL: %q", items[0].URL)
	}
	if items[0].Date != "2026-06-02" {
		t.Fatalf("unexpected first date: %q", items[0].Date)
	}
	for _, item := range items {
		if !strings.Contains(item.URL, "/info/") {
			t.Errorf("non-article URL leaked: %q", item.URL)
		}
		if item.Date == "" {
			t.Errorf("article %q has empty date", item.Title)
		}
	}
}

func TestDagArticleDoesNotInferPublicationDateFromHeadline(t *testing.T) {
	html := []byte(`<html><head>
<title>档案馆（校史馆）2026年暑假值班表（7月20日-9月2日）-档案馆（校史馆）</title>
<meta name="pageTitle" content="档案馆（校史馆）2026年暑假值班表（7月20日-9月2日）">
</head><body><div id="detailCn"><div>
<h3 class="article_title">档案馆（校史馆）2026年暑假值班表（7月20日-9月2日）</h3>
<div id="vsb_content"><div class="v_news_content"><p>档案馆暑假值班安排及服务事项，请按公告时间办理相关业务。</p></div></div>
</div></div></body></html>`)

	article, err := parseDagArticle(html, "http://dag.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "档案馆（校史馆）2026年暑假值班表（7月20日-9月2日）" {
		t.Fatalf("unexpected title: %q", article.Title)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("headline/event dates must not become publication metadata: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "档案馆暑假值班安排") {
		t.Fatalf("article content was not extracted: %q", article.Content)
	}
}

func TestDagListParsesJSPRowsAndLegalEmptyList(t *testing.T) {
	listHTML, err := os.ReadFile("testdata/dag-list-page-1.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseDagNews(listHTML, "https://dag.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want 2: %#v", len(items), items)
	}
	if items[0].Type != "新闻动态" || items[0].Date != "2026-09-02" {
		t.Fatalf("unexpected list metadata: %#v", items[0])
	}
	if items[0].URL != "https://dag.swjtu.edu.cn/info/1221/9001.htm" {
		t.Fatalf("unexpected article URL: %q", items[0].URL)
	}

	emptyHTML, err := os.ReadFile("testdata/dag-empty-list.html")
	if err != nil {
		t.Fatal(err)
	}
	empty, err := parseDagNews(emptyHTML, "https://dag.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("legal empty list returned %d items", len(empty))
	}
}

func TestDagPaginationUsesJSPPageDirection(t *testing.T) {
	fixtures := map[string]string{}
	for page, name := range map[string]string{
		"1": "dag-list-page-1.html",
		"2": "dag-list-page-2.html",
		"3": "dag-list-page-3.html",
	} {
		body, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		fixtures[page] = string(body)
	}

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/listcolumn.jsp" {
			http.Error(w, fmt.Sprintf("unexpected path %s", r.URL.Path), http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("wbtreeid") != "1221" {
			http.Error(w, "unexpected column", http.StatusBadRequest)
			return
		}
		page := r.URL.Query().Get("a1088771p")
		if page == "" {
			page = "1"
		}
		requests = append(requests, page)
		body, ok := fixtures[page]
		if !ok {
			http.Error(w, "unexpected page", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html;charset=UTF-8")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	site := AllSites["dag"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	site.BaseURL = server.URL
	NoCache = true
	defer func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
	}()

	total, err := site.GetTotalPages("news")
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total pages = %d, want 3", total)
	}

	var all []string
	for page := 1; page <= total; page++ {
		items, err := site.ListNewsPage("news", page)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, item := range items {
			all = append(all, item.URL)
		}
	}
	want := []string{
		server.URL + "/info/1221/9001.htm",
		server.URL + "/info/1221/9002.htm",
		server.URL + "/info/1221/9003.htm",
		server.URL + "/info/1221/9004.htm",
		server.URL + "/info/1221/9005.htm",
	}
	if strings.Join(all, "\n") != strings.Join(want, "\n") {
		t.Fatalf("page order/items = %#v, want %#v", all, want)
	}
	if len(requests) < 5 || requests[len(requests)-1] != "3" {
		t.Fatalf("pagination requests = %#v, want page 3 to be fetched", requests)
	}
	if _, err := site.ListNewsPage("news", 4); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("out-of-range page error = %v", err)
	}
}

func TestDagArticleExtractsTypePreciseDateBodyImageAndAttachment(t *testing.T) {
	html, err := os.ReadFile("testdata/dag-article.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseDagArticle(html, "https://dag.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "带精确时间的档案通知" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("date fields = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Type != "通知公告" {
		t.Fatalf("type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "这是正文第一段") {
		t.Fatalf("content was not extracted: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://dag.swjtu.edu.cn/images/notice.png" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "档案通知附件.xlsx" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, "https://dag.swjtu.edu.cn/images/notice.png") || !strings.Contains(article.ContentHTML, "https://dag.swjtu.edu.cn/system/_content/download.jsp") {
		t.Fatalf("resource URLs were not resolved in ContentHTML: %q", article.ContentHTML)
	}
}

func TestDagRejectsCaptchaAndNonArticlePages(t *testing.T) {
	captcha, err := os.ReadFile("testdata/dag-captcha.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseDagNews(captcha, "https://dag.swjtu.edu.cn"); err == nil || !strings.Contains(err.Error(), "captcha") {
		t.Fatalf("captcha list error = %v, want captcha classification", err)
	}
	if _, err := parseDagArticle(captcha, "https://dag.swjtu.edu.cn"); err == nil || !strings.Contains(err.Error(), "captcha") {
		t.Fatalf("captcha article error = %v, want captcha classification", err)
	}
	staticPage := []byte(`<html><head><title>档案馆介绍-档案馆（校史馆）</title></head><body><h1>档案馆介绍</h1><p>静态页面</p></body></html>`)
	if _, err := parseDagArticle(staticPage, "https://dag.swjtu.edu.cn"); err == nil || !strings.Contains(err.Error(), "not an article") {
		t.Fatalf("static page error = %v, want non-article classification", err)
	}
	spa, err := os.ReadFile("testdata/dag-spa.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseDagNews(spa, "https://dag.swjtu.edu.cn"); err == nil || !strings.Contains(err.Error(), "SPA") {
		t.Fatalf("SPA list error = %v, want SPA classification", err)
	}
	waf, err := os.ReadFile("testdata/dag-waf.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseDagNews(waf, "https://dag.swjtu.edu.cn"); err == nil || !strings.Contains(err.Error(), "waf") {
		t.Fatalf("WAF list error = %v, want WAF classification", err)
	}
}

func TestDagHiddenDirectArticleRoute(t *testing.T) {
	items, total, err := dagListPage("lecture-appointment", 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("hidden route result = total %d items %#v, want one item", total, items)
	}
	if items[0].URL != dagCanonicalBaseURL+"/info/1431/4811.htm" || items[0].Type != "校史讲座预约" {
		t.Fatalf("hidden route item = %#v", items[0])
	}
	if _, _, err := dagListPage("lecture-appointment", 2); err == nil || !strings.Contains(err.Error(), "direct article route") {
		t.Fatalf("hidden route page 2 error = %v", err)
	}
}

func TestDagFetchArticleVerifiesResourcesAndSanitizesHTML(t *testing.T) {
	articleHTML, err := os.ReadFile("testdata/dag-resource-article.html")
	if err != nil {
		t.Fatal(err)
	}
	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	validXLSX := []byte{'P', 'K', 0x03, 0x04}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1211/9001.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(articleHTML)
		case "/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(validPNG)
		case "/bad-magic.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("not an image"))
		case "/missing.png":
			http.NotFound(w, r)
		case "/good.xlsx":
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			_, _ = w.Write(validXLSX)
		case "/bad.xlsx":
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			_, _ = w.Write([]byte("not a zip"))
		case "/captcha.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<html><body>请输入验证码下载附件</body></html>"))
		case "/large.bin":
			w.Header().Set("Content-Length", strconv.FormatInt(dagMaxResourceBytes+1, 10))
			_, _ = w.Write([]byte("too large"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["dag"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	site.BaseURL = server.URL
	NoCache = true
	defer func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
	}()

	article, raw, err := fetchDagArticle(server.URL + "/info/1211/9001.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("article raw/date fields = %d, %#v", len(raw), article)
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != server.URL+"/good.xlsx" || article.Attachments[0].Size != "4" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	for _, rejected := range []string{"bad-magic.png", "missing.png", "bad.xlsx", "captcha.pdf"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource %q remained in ContentHTML: %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, server.URL+"/good.png") || !strings.Contains(article.ContentHTML, server.URL+"/good.xlsx") || !strings.Contains(article.ContentHTML, "https://example.com/reference") {
		t.Fatalf("valid/external links were not preserved: %s", article.ContentHTML)
	}
	if _, _, err := dagFetchResource(server.URL+"/large.bin", server.URL, server.URL+"/info/1211/9001.htm", false); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized resource error = %v", err)
	}
}

func TestDagResourceBodyValidationChecksContentTypeAndMagic(t *testing.T) {
	if !dagResourceBodyIsUsable("image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "good.png", true) {
		t.Fatal("valid PNG was rejected")
	}
	if dagResourceBodyIsUsable("image/png", []byte("not an image"), "bad.png", true) {
		t.Fatal("bad image magic was accepted")
	}
	if !dagResourceBodyIsUsable("application/pdf", []byte("%PDF-1.7"), "good.pdf", false) {
		t.Fatal("valid PDF was rejected")
	}
	if dagResourceBodyIsUsable("application/pdf", []byte("<html>请输入验证码下载附件</html>"), "bad.pdf", false) {
		t.Fatal("HTML CAPTCHA was accepted as a PDF")
	}
	if !dagResourceBodyIsUsable("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte{'P', 'K', 0x03, 0x04}, "good.xlsx", false) {
		t.Fatal("valid OOXML magic was rejected")
	}
}
