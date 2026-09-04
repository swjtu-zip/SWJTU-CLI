package scraper

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

const dwtzbFixtureBaseURL = "https://dwtzb.swjtu.edu.cn"

func readDwtzbFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read dwtzb fixture %s: %v", name, err)
	}
	return data
}

func TestDwtzbAdapterRegistrationAndRoutes(t *testing.T) {
	site := AllSites["dwtzb"]
	if site == nil || site.BaseURL != dwtzbFixtureBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("dwtzb adapter hooks are incomplete: %#v", site)
	}
	want := dwtzbSubPages
	if !reflect.DeepEqual(site.SubPages, want) || len(site.SubPages) != 28 {
		t.Fatalf("dwtzb routes = %#v, want 28 real list routes", site.SubPages)
	}
	for _, excluded := range []string{"bmgk/gzzz", "bmgk/ryzc", "tztt"} {
		for _, route := range site.SubPages {
			if route == excluded {
				t.Errorf("static route registered as feed: %q", excluded)
			}
		}
	}
}

func TestDwtzbPageURLAndTotalPagesUseReverseNumbering(t *testing.T) {
	tests := []struct {
		slug        string
		page, total int
		want        string
	}{
		{slug: "zhyw/xnxw", page: 1, total: 124, want: dwtzbFixtureBaseURL + "/zhyw/xnxw.htm"},
		{slug: "zhyw/xnxw", page: 2, total: 124, want: dwtzbFixtureBaseURL + "/zhyw/xnxw/123.htm"},
		{slug: "zhyw/xnxw", page: 62, total: 124, want: dwtzbFixtureBaseURL + "/zhyw/xnxw/63.htm"},
		{slug: "zhyw/xnxw", page: 124, total: 124, want: dwtzbFixtureBaseURL + "/zhyw/xnxw/1.htm"},
		{slug: "zcws/ldls", page: 2, total: 8, want: dwtzbFixtureBaseURL + "/zcws/ldls/7.htm"},
	}
	for _, tt := range tests {
		if got := dwtzbPageURL(dwtzbFixtureBaseURL, tt.slug, tt.page, tt.total, ".htm"); got != tt.want {
			t.Errorf("dwtzbPageURL(%q, %d/%d) = %q, want %q", tt.slug, tt.page, tt.total, got, tt.want)
		}
	}
	if got := dwtzbTotalPagesFromHTML(readDwtzbFixture(t, "dwtzb_list.html")); got != 3 {
		t.Fatalf("total pages = %d, want 3", got)
	}
	for _, tt := range []struct {
		page, total int
	}{
		{0, 124}, {125, 124}, {2, 0},
	} {
		if got := dwtzbPageURL(dwtzbFixtureBaseURL, "zhyw/xnxw", tt.page, tt.total, ".htm"); got != "" {
			t.Errorf("out-of-range dwtzbPageURL(%d/%d) = %q, want empty", tt.page, tt.total, got)
		}
	}
}

func TestDwtzbAuditedRoutesHaveRealPaginationTotals(t *testing.T) {
	want := map[string]int{
		"zhyw/xnxw": 124, "zhyw/hlkd": 22, "mzdp1": 44,
		"mzdp1/mg/dpgz": 3, "mzdp1/mm/dpgz": 8, "mzdp1/mj/dpgz": 2,
		"mzdp1/mj1/dpgz": 3, "mzdp1/ngd/dpgz": 2, "mzdp1/zgd/dpgz": 5,
		"mzdp1/jsxs/dpgz": 7, "tztt/ql_xq_": 2, "tztt/omtxh_lxrylyh_": 4,
		"tztt/dwzsfzlyh": 2, "wdp": 6, "mzgz": 5, "czyz1": 16,
		"czyz1/rddb": 7, "czyz1/zxwy": 9, "czyz1/qtdwdbrs": 1,
		"zcws": 20, "zcws/ldls": 8, "zcws/fzzcjd": 5, "zcws/tzllyj": 8,
		"qzj": 2, "shzyxy": 6, "bszn": 1, "tzgg": 7, "rwfc": 9,
	}
	if len(want) != len(dwtzbSubPages) {
		t.Fatalf("audited route total table has %d entries, want %d", len(want), len(dwtzbSubPages))
	}
	pages := 0
	for _, route := range dwtzbSubPages {
		total, ok := want[route]
		if !ok || !dwtzbIsKnownListRoute(route) || total < 1 {
			t.Errorf("route %q is missing a positive audited total", route)
			continue
		}
		pages += total
	}
	if pages != 338 {
		t.Fatalf("audited logical pages = %d, want 338", pages)
	}
}

func TestDwtzbHomepageScopesAllArticleModules(t *testing.T) {
	items, err := parseAdapter_dwtzbList(readDwtzbFixture(t, "dwtzb_home.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("homepage items = %d, want six center and two sidebar items: %#v", len(items), items)
	}
	wantTypes := map[string]bool{"校内新闻": true, "政策务实": true, "民主党派": true, "无党派": true, "参政议政": true, "互联快递": true, "通知公告": true, "人物风采": true}
	for _, item := range items {
		if !wantTypes[item.Type] {
			t.Errorf("unexpected homepage type: %#v", item)
		}
		if strings.Contains(item.URL, "/9514.htm") || strings.Contains(item.URL, "example.com") {
			t.Errorf("carousel/external link leaked into homepage: %#v", item)
		}
	}
}

func TestDwtzbLiveHomepageAndReversePages(t *testing.T) {
	home, err := parseAdapter_dwtzbList(readDwtzbFixture(t, "dwtzb_home_live.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(home) != 9 {
		t.Fatalf("live homepage items = %d, want six center plus two notice and one people item: %#v", len(home), home)
	}
	wantTypes := map[string]bool{"校内新闻": true, "政策务实": true, "民主党派": true, "无党派": true, "参政议政": true, "互联快递": true, "通知公告": true, "人物风采": true}
	for _, item := range home {
		if !wantTypes[item.Type] {
			t.Errorf("unexpected live homepage type: %#v", item)
		}
		if strings.Contains(item.URL, "/9514.htm") || strings.Contains(item.URL, "example.com") {
			t.Errorf("carousel/friend link leaked from live homepage: %#v", item)
		}
	}

	cases := []struct {
		fixture, firstTitle, lastTitle, date string
	}{
		{"dwtzb_list_live_first.html", "西南交通大学两项铸牢中华民族共同体意识教育实践案例成功入选省级典型案例", "“锚定‘十五五’ 聚力新征程”西南民族大学 2026 年党外代表人士培训班在我校社会主义学院举办", "2026-08-08"},
		{"dwtzb_list_live_middle.html", "《四川统战工作》专期全文介绍我校统战工作争创一流的经验做法", "学校无党派代表人士周仲荣在全省党外知识分子统战工作座谈会上发言", "2017-09-30"},
		{"dwtzb_list_live_last.html", "孙林夫教授为省政协新委员培训班授课", "我校民主党派概况", "2003-10-09"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			items, parseErr := parseAdapter_dwtzbList(readDwtzbFixture(t, tc.fixture), dwtzbFixtureBaseURL)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if len(items) != 3 && tc.fixture != "dwtzb_list_live_last.html" {
				t.Fatalf("%s rows = %d, want 3", tc.fixture, len(items))
			}
			if len(items) != 2 && tc.fixture == "dwtzb_list_live_last.html" {
				t.Fatalf("last-page rows = %d, want 2", len(items))
			}
			if items[0].Title != tc.firstTitle || items[0].Date != tc.date || items[0].Type != "校内新闻" {
				t.Fatalf("first live row = %#v", items[0])
			}
			if items[len(items)-1].Title != tc.lastTitle {
				t.Fatalf("last live row title = %q, want %q", items[len(items)-1].Title, tc.lastTitle)
			}
		})
	}
	if got := dwtzbTotalPagesFromHTML(readDwtzbFixture(t, "dwtzb_list_live_first.html")); got != 124 {
		t.Fatalf("live first page total = %d, want 124", got)
	}
	if got := dwtzbPageURL(dwtzbFixtureBaseURL, "zhyw/xnxw", 62, 124, ".htm"); got != dwtzbFixtureBaseURL+"/zhyw/xnxw/63.htm" {
		t.Fatalf("middle logical page URL = %q", got)
	}
	if got := dwtzbPageURL(dwtzbFixtureBaseURL, "zhyw/xnxw", 124, 124, ".htm"); got != dwtzbFixtureBaseURL+"/zhyw/xnxw/1.htm" {
		t.Fatalf("last logical page URL = %q", got)
	}
}

func TestDwtzbListAndArticleParsers(t *testing.T) {
	items, err := parseAdapter_dwtzbList(readDwtzbFixture(t, "dwtzb_list.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Type != "校内新闻" || items[0].Date != "2026-08-08" || items[0].PublishedAt != "" {
		t.Fatalf("list items = %#v", items)
	}
	if _, err := parseAdapter_dwtzbList([]byte(`<html><body><div id="content" class="pages"><p>工作职责</p></div></body></html>`), dwtzbFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as an article list")
	}

	article, err := parseAdapter_dwtzbArticle(readDwtzbFixture(t, "dwtzb_article.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于测试附件的通知" || article.Type != "通知公告" || article.Source != "统战新语" || article.Author != "张三" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Date != "2026-09-02" || article.PublishedAt != "2026-09-02 14:34:00" {
		t.Fatalf("article date = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "这是正文内容") || len(article.Images) != 1 || article.Images[0] != dwtzbFixtureBaseURL+"/__local/body.png" {
		t.Fatalf("article body/resources = %#v", article)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件：测试表.xlsx" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	if _, err := parseAdapter_dwtzbArticle([]byte(`<html><head><meta name="pageType" content="1"></head><body><h1>页面</h1></body></html>`), dwtzbFixtureBaseURL); err == nil {
		t.Fatal("non-article page was accepted as an article")
	}
}

func TestDwtzbLiveArticlesUseFixedMetadataAndExactBody(t *testing.T) {
	article, err := parseAdapter_dwtzbArticle(readDwtzbFixture(t, "dwtzb_article_live.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "西南交通大学两项铸牢中华民族共同体意识教育实践案例成功入选省级典型案例" || article.Type != "校内新闻" || article.Date != "2026-08-08" || article.PublishedAt != "" {
		t.Fatalf("live article fixed metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "活动时间：2026年9月10日") || strings.Contains(article.Date, "2026-09-10") {
		t.Fatalf("body date contaminated publication date: date=%q content=%q", article.Date, article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != dwtzbFixtureBaseURL+"/__local/7/8D/7B/45CD769E8D442F5B48CDF2C52B3_2E17F7B2_1A55D.png" {
		t.Fatalf("live article images = %#v", article.Images)
	}

	notice, err := parseAdapter_dwtzbArticle(readDwtzbFixture(t, "dwtzb_article_live_notice.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if notice.Title != "关于统计学校统一战线成员2024-2025年相关工作开展情况的通知" || notice.Type != "通知公告" || notice.Date != "2025-11-20" || notice.PublishedAt != "" {
		t.Fatalf("notice fixed metadata = %#v", notice)
	}
	if len(notice.Attachments) != 1 || notice.Attachments[0].Name != "附件：学校统一战线成员2024-2025年开展相关工作情况统计表.xlsx" || !strings.Contains(notice.Attachments[0].URL, "wbfileid=BA22CF0B4B6A0FD57D79D28F28221942") {
		t.Fatalf("notice attachment candidate = %#v", notice.Attachments)
	}

	external, err := parseAdapter_dwtzbArticle(readDwtzbFixture(t, "dwtzb_article_live_external.html"), dwtzbFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if external.Type != "互联快递" || external.Date != "2015-09-07" || len(external.Attachments) != 0 {
		t.Fatalf("external-link article metadata/attachments = %#v", external)
	}
	if !strings.Contains(external.ContentHTML, "http://www.zytzb.gov.cn/") || !strings.Contains(external.ContentHTML, "https://external.example/guide") || !strings.Contains(external.ContentHTML, "https://external.example/paper.pdf") {
		t.Fatalf("ordinary external links were not retained: %s", external.ContentHTML)
	}
}

func TestDwtzbRejectsErrorAndSPAShells(t *testing.T) {
	if _, err := parseAdapter_dwtzbList(readDwtzbFixture(t, "dwtzb_error_live.html"), dwtzbFixtureBaseURL); err == nil {
		t.Fatal("live 404 shell was accepted as a list")
	}
	if _, err := parseAdapter_dwtzbList(readDwtzbFixture(t, "dwtzb_spa_live.html"), dwtzbFixtureBaseURL); err == nil {
		t.Fatal("SPA shell was accepted as a list")
	}
	if _, err := parseAdapter_dwtzbArticle(readDwtzbFixture(t, "dwtzb_error_live.html"), dwtzbFixtureBaseURL); err == nil {
		t.Fatal("live 404 shell was accepted as an article")
	}
	for _, staticRoute := range []string{"bmgk/gzzz", "bmgk/ryzc", "tztt"} {
		if _, err := dwtzbTotalPages(staticRoute); err == nil {
			t.Fatalf("static route %q was accepted for pagination", staticRoute)
		}
	}
}

type dwtzbTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper dwtzbTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func dwtzbTestResponse(request *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}

func TestDwtzbFetchArticleVerifiesResourcesAndKeepsExternalLinks(t *testing.T) {
	fixture := readDwtzbFixture(t, "dwtzb_resource_article.html")
	oldClient, oldNoCache, oldBase := defaultClient, NoCache, AllSites["dwtzb"].BaseURL
	NoCache = true
	AllSites["dwtzb"].BaseURL = "https://dwtzb.test"
	defaultClient = &http.Client{Transport: dwtzbTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "dwtzb.test":
			switch request.URL.Path {
			case "/info/1006/9001.htm":
				return dwtzbTestResponse(request, http.StatusOK, "text/html; charset=utf-8", fixture), nil
			case "/good.png":
				return dwtzbTestResponse(request, http.StatusOK, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}), nil
			case "/bad.png":
				return dwtzbTestResponse(request, http.StatusOK, "image/png", []byte("<html><body>请输入验证码</body></html>")), nil
			case "/wrong.png":
				return dwtzbTestResponse(request, http.StatusOK, "image/png", []byte("not a PNG")), nil
			case "/good.pdf":
				return dwtzbTestResponse(request, http.StatusOK, "application/pdf", []byte("%PDF-1.7 fixture")), nil
			case "/bad.pdf":
				return dwtzbTestResponse(request, http.StatusOK, "application/pdf", []byte("not a PDF")), nil
			case "/missing.pdf":
				return dwtzbTestResponse(request, http.StatusNotFound, "application/pdf", []byte("missing")), nil
			case "/system/_content/download.jsp":
				return dwtzbTestResponse(request, http.StatusOK, "text/html; charset=utf-8", []byte("<html><title>附件下载</title><body>请输入验证码下载附件</body></html>")), nil
			}
		case "cdn.example":
			if request.URL.Path == "/good.gif" {
				return dwtzbTestResponse(request, http.StatusOK, "image/gif", []byte("GIF89a fixture")), nil
			}
		}
		return dwtzbTestResponse(request, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
	})}
	t.Cleanup(func() {
		defaultClient, NoCache, AllSites["dwtzb"].BaseURL = oldClient, oldNoCache, oldBase
		ClearCache()
	})
	ClearCache()

	article, raw, err := dwtzbFetchArticle("https://dwtzb.test/info/1006/9001.htm")
	if err != nil {
		t.Fatalf("fetch dwtzb article: %v", err)
	}
	if len(raw) == 0 || article.Date != "2026-09-02" || article.PublishedAt != "" {
		t.Fatalf("fetched article/date = raw=%d article=%#v", len(raw), article)
	}
	if len(article.Images) != 2 || article.Images[0] != "https://dwtzb.test/good.png" || article.Images[1] != "https://cdn.example/good.gif" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != "https://dwtzb.test/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, "https://dwtzb.test/good.png") || !strings.Contains(article.ContentHTML, "https://cdn.example/good.gif") || !strings.Contains(article.ContentHTML, "https://external.example/ordinary") {
		t.Fatalf("valid resources/external link missing from ContentHTML: %s", article.ContentHTML)
	}
	for _, rejected := range []string{"bad.png", "wrong.png", "missing.pdf", "bad.pdf", "验证码", "wbfileid=BAD", "javascript:"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource survived ContentHTML: %q in %s", rejected, article.ContentHTML)
		}
	}
}

func TestDwtzbResourceURLAndMagicGuards(t *testing.T) {
	if got := dwtzbResourceURL(dwtzbFixtureBaseURL, "javascript:download('/x.pdf')"); got != "" {
		t.Fatalf("pseudo resource URL = %q", got)
	}
	if got := dwtzbResourceURL(dwtzbFixtureBaseURL, "https://cdn.example/image.png#fragment"); got != "https://cdn.example/image.png" {
		t.Fatalf("external resource URL = %q", got)
	}
	if !dwtzbResourceBodyIsUsable("image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "good.png", true) {
		t.Fatal("valid PNG was rejected")
	}
	if dwtzbResourceBodyIsUsable("image/png", []byte("not an image"), "bad.png", true) {
		t.Fatal("bad image magic was accepted")
	}
	if dwtzbResourceBodyIsUsable("application/octet-stream", []byte("<html>请输入验证码下载附件</html>"), "bad.xlsx", false) {
		t.Fatal("HTML CAPTCHA attachment was accepted")
	}
	if !dwtzbResourceBodyIsUsable("application/pdf", []byte("%PDF-1.7"), "good.pdf", false) {
		t.Fatal("valid PDF was rejected")
	}
	if dwtzbResourceBodyIsUsable("application/pdf", []byte("not a PDF"), "bad.pdf", false) {
		t.Fatal("bad PDF magic was accepted")
	}
}

func TestDwtzbDateValuesDoNotInventClock(t *testing.T) {
	if date, publishedAt := dwtzbDateValues("日期：2026-08-31"); date != "2026-08-31" || publishedAt != "" {
		t.Fatalf("date-only value = %q/%q", date, publishedAt)
	}
	if date, publishedAt := dwtzbDateValues("日期：2026-08-31 14:34"); date != "2026-08-31" || publishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("precise value = %q/%q", date, publishedAt)
	}
}
