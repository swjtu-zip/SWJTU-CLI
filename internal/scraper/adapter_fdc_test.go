package scraper

import (
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

const fdcFixtureBaseURL = "https://fdc.swjtu.edu.cn"

func readFdcFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fdc fixture %s: %v", name, err)
	}
	return data
}

func TestFdcAdapterRegistrationAndRoutes(t *testing.T) {
	site := AllSites["fdc"]
	if site == nil || site.BaseURL != fdcFixtureBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("fdc adapter hooks are incomplete: %#v", site)
	}
	want := []string{
		"tzgg", "xwdt", "llxx", "sdby", "sdby/rybz", "sdby/yrgs",
		"gzzd", "gzzd/sjwj", "gzzd/xxwj", "sdjd", "sdjd/dxal",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("fdc routes = %#v, want %#v", site.SubPages, want)
	}
	for _, excluded := range []string{"bmjj", "bmjj/gzzz", "bmjj/jgsz", "bmjj/gzry", "sdjd/jbxz"} {
		for _, route := range site.SubPages {
			if route == excluded {
				t.Errorf("static route registered as feed: %q", excluded)
			}
		}
	}
}

func TestFdcPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	tests := []struct {
		slug, want  string
		page, total int
	}{
		{slug: "tzgg", page: 1, total: 3, want: fdcFixtureBaseURL + "/tzgg.htm"},
		{slug: "tzgg", page: 2, total: 3, want: fdcFixtureBaseURL + "/tzgg/2.htm"},
		{slug: "tzgg", page: 3, total: 3, want: fdcFixtureBaseURL + "/tzgg/1.htm"},
		{slug: "xwdt", page: 4, total: 7, want: fdcFixtureBaseURL + "/xwdt/4.htm"},
		{slug: "gzzd/sjwj", page: 2, total: 2, want: fdcFixtureBaseURL + "/gzzd/sjwj/1.htm"},
		{slug: "index", page: 1, total: 1, want: fdcFixtureBaseURL + "/index.htm"},
	}
	for _, tt := range tests {
		if got := fdcPageURL(fdcFixtureBaseURL, tt.slug, tt.page, tt.total, ".htm"); got != tt.want {
			t.Errorf("fdcPageURL(%q, %d/%d) = %q, want %q", tt.slug, tt.page, tt.total, got, tt.want)
		}
	}
}

func TestFdcHomepageScopesAllNewsPanels(t *testing.T) {
	items, err := parseAdapter_fdcList(readFdcFixture(t, "fdc_home.html"), fdcFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("homepage items = %d, want five internal entries: %#v", len(items), items)
	}
	wantTypes := map[string]int{"通知公告": 2, "新闻动态": 1, "理论学习": 2}
	gotTypes := make(map[string]int)
	for _, item := range items {
		gotTypes[item.Type]++
		if strings.Contains(item.URL, "news.swjtu.edu.cn") || strings.Contains(item.URL, "www.swjtu.edu.cn") {
			t.Errorf("carousel/friend link leaked into homepage feed: %#v", item)
		}
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("homepage types = %#v, want %#v", gotTypes, wantTypes)
	}
	if items[2].Title != "我校参加教师队伍建设重点工作推进会" {
		t.Fatalf("internal news item was not retained: %#v", items[2])
	}
	if items[3].Title != "习近平给四所交通大学全体师生的回信" {
		t.Fatalf("hidden theory tab was not parsed: %#v", items[3])
	}
}

func TestFdcMeasuredHomepageFixtureBindsAllVisibleRoutes(t *testing.T) {
	data := readFdcFixture(t, "fdc_live_home.html")
	for _, route := range []string{"index.htm", "bmjj.htm", "bmjj/gzzz.htm", "bmjj/jgsz.htm", "bmjj/gzry.htm"} {
		if !strings.Contains(string(data), `href="`+route+`"`) {
			t.Errorf("measured homepage fixture is missing route %q", route)
		}
	}
	for _, route := range fdcSubPages {
		if !strings.Contains(string(data), `href="`+route+`.htm"`) {
			t.Errorf("measured homepage fixture is missing route %q", route)
		}
	}
	items, err := parseAdapter_fdcList(data, fdcFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 13 {
		t.Fatalf("measured homepage items = %d, want 13 local articles: %#v", len(items), items)
	}
	wantTypes := map[string]int{"通知公告": 6, "新闻动态": 1, "理论学习": 6}
	gotTypes := make(map[string]int)
	for _, item := range items {
		gotTypes[item.Type]++
		if strings.Contains(item.URL, "news.swjtu.edu.cn") || strings.Contains(item.URL, "www.swjtu.edu.cn") {
			t.Errorf("external homepage link leaked into feed: %#v", item)
		}
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("measured homepage types = %#v, want %#v", gotTypes, wantTypes)
	}
}

func TestFdcMeasuredTzggFixtureBindsFirstMiddleAndLastPages(t *testing.T) {
	cases := []struct {
		fixture string
		page    int
		count   int
		wantURL string
	}{
		{fixture: "fdc_live_tzgg_first.html", page: 1, count: 10, wantURL: fdcFixtureBaseURL + "/info/1118/14863.htm"},
		{fixture: "fdc_live_tzgg_middle.html", page: 2, count: 10, wantURL: fdcFixtureBaseURL + "/info/1118/10493.htm"},
		{fixture: "fdc_live_tzgg_last.html", page: 3, count: 7, wantURL: fdcFixtureBaseURL + "/info/1118/10723.htm"},
	}
	for _, tc := range cases {
		data := readFdcFixture(t, tc.fixture)
		items, err := parseAdapter_fdcList(data, fdcFixtureBaseURL)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.fixture, err)
		}
		if len(items) != tc.count || items[0].URL != tc.wantURL || items[0].Type != "通知公告" {
			t.Fatalf("%s page %d = %#v", tc.fixture, tc.page, items)
		}
		totalItems, currentPage, totalPages, ok := fdcPaginationFromHTML(data)
		if !ok || totalItems != 27 || currentPage != tc.page || totalPages != 3 {
			t.Fatalf("%s pagination = %d/%d/%d/%v", tc.fixture, totalItems, currentPage, totalPages, ok)
		}
	}
}

func TestFdcListParsesFirstMiddleLastAndExternalEntries(t *testing.T) {
	cases := []struct {
		fixture string
		page    int
		count   int
		wantURL string
	}{
		{fixture: "fdc_tzgg_page1.html", page: 1, count: 2, wantURL: fdcFixtureBaseURL + "/info/1118/14863.htm"},
		{fixture: "fdc_tzgg_page2.html", page: 2, count: 1, wantURL: fdcFixtureBaseURL + "/info/1118/10493.htm"},
		{fixture: "fdc_tzgg_page3.html", page: 3, count: 1, wantURL: fdcFixtureBaseURL + "/info/1118/11023.htm"},
	}
	for _, tc := range cases {
		data := readFdcFixture(t, tc.fixture)
		items, err := parseAdapter_fdcList(data, fdcFixtureBaseURL)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.fixture, err)
		}
		if len(items) != tc.count {
			t.Fatalf("page %d items = %#v", tc.page, items)
		}
		if items[0].URL != tc.wantURL || items[0].Type != "通知公告" || items[0].Date == "" {
			t.Fatalf("page %d item = %#v", tc.page, items[0])
		}
		totalItems, currentPage, totalPages, ok := fdcPaginationFromHTML(data)
		if !ok || totalItems != 27 || currentPage != tc.page || totalPages != 3 {
			t.Fatalf("page %d pagination = %d/%d/%d/%v", tc.page, totalItems, currentPage, totalPages, ok)
		}
	}
}

func TestFdcEmptyListIsNotStaticContent(t *testing.T) {
	items, err := parseAdapter_fdcList(readFdcFixture(t, "fdc_empty.html"), fdcFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty list = %#v, want non-nil empty slice", items)
	}
	if got := fdcTotalPagesFromHTML(readFdcFixture(t, "fdc_empty.html")); got != 0 {
		t.Fatalf("empty list total pages = %d, want 0", got)
	}
	if _, err := parseAdapter_fdcList(readFdcFixture(t, "fdc_static.html"), fdcFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as an article list")
	}
}

func TestFdcArticleParsesMetadataBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_fdcArticle(readFdcFixture(t, "fdc_article.html"), fdcFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于测试正文、图片和附件的通知" || article.Type != "通知公告" || article.Source != "党委教师工作部" || article.Author != "张三" || article.Editor != "李四" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("article date = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "这是正文内容") || len(article.Images) != 1 || article.Images[0] != fdcFixtureBaseURL+"/__local/body.png" {
		t.Fatalf("article body/images = %#v", article)
	}
	if len(article.Attachments) != 2 || article.Attachments[0].Name != "附件：测试表.pdf" || !strings.Contains(article.Attachments[1].URL, "download.jsp") {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, fdcFixtureBaseURL+"/__local/body.png") {
		t.Fatalf("content HTML did not resolve image URL: %s", article.ContentHTML)
	}
}

func TestFdcMeasuredArticleUsesFixedMetadataAndExactBodyBoundary(t *testing.T) {
	article, err := parseAdapter_fdcArticle(readFdcFixture(t, "fdc_live_article_notice.html"), fdcFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于组织参加第六届全国防范非法金融活动知识答题赛的通知" || article.Type != "通知公告" || article.Date != "2026-06-11" || article.PublishedAt != "" {
		t.Fatalf("measured article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "活动时间为即日起至6月18日") || strings.Contains(article.ContentHTML, "参赛指引.pdf") || strings.Contains(article.ContentHTML, "download.jsp") {
		t.Fatalf("measured article body escaped fixed content boundary: content=%q html=%q", article.Content, article.ContentHTML)
	}
	if len(article.Attachments) != 1 || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("measured article attachment = %#v", article.Attachments)
	}
}

func TestFdcRejectsNonArticleAndVerificationShells(t *testing.T) {
	if _, err := parseAdapter_fdcArticle(readFdcFixture(t, "fdc_static.html"), fdcFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as an article")
	}
	if _, err := parseAdapter_fdcArticle(readFdcFixture(t, "fdc_captcha.html"), fdcFixtureBaseURL); err == nil {
		t.Fatal("captcha HTML was accepted as an article")
	}
	if _, err := parseAdapter_fdcList(readFdcFixture(t, "fdc_captcha.html"), fdcFixtureBaseURL); err == nil {
		t.Fatal("captcha HTML was accepted as a list")
	}
}

func TestFdcDateValuesPreserveMinutePrecision(t *testing.T) {
	if date, publishedAt := fdcDateValues("日期：2026-08-31"); date != "2026-08-31" || publishedAt != "" {
		t.Fatalf("date-only value = %q/%q", date, publishedAt)
	}
	if date, publishedAt := fdcDateValues("日期：2026-08-31 14:34"); date != "2026-08-31" || publishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("precise value = %q/%q", date, publishedAt)
	}
}

func TestFdcExplicitEmptyListAcceptsAnyPageAndStopsRangeWalk(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	oldBase := AllSites["fdc"].BaseURL
	requested := make([]string, 0, 8)
	defaultClient = &http.Client{Transport: fdcTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.Path)
		switch req.URL.Path {
		case "/sdby.htm", "/sdby/yrgs.htm":
			return fdcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readFdcFixture(t, "fdc_live_sdby.html")), nil
		default:
			return fdcTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
	})}
	AllSites["fdc"].BaseURL = fdcFixtureBaseURL
	NoCache = true
	ClearCache()
	t.Cleanup(func() {
		defaultClient, NoCache = oldClient, oldNoCache
		AllSites["fdc"].BaseURL = oldBase
		ClearCache()
	})

	for _, slug := range []string{"sdby", "sdby/yrgs"} {
		items, err := AllSites["fdc"].ListNewsPage(slug, 2)
		if err != nil || len(items) != 0 {
			t.Fatalf("%s page 2 = items=%#v err=%v; want empty/nil", slug, items, err)
		}
		total, err := AllSites["fdc"].GetTotalPages(slug)
		if err != nil || total != 0 {
			t.Fatalf("%s total pages = %d, err = %v; want 0/nil", slug, total, err)
		}
		walked, err := AllSites["fdc"].ListNewsRange(slug, "2026-01-01", "2026-12-31", 0)
		if err != nil {
			t.Fatalf("%s ListNewsRange: %v", slug, err)
		}
		if len(walked) != 0 {
			t.Fatalf("%s range walk returned items for an empty list: %#v", slug, walked)
		}
	}
	for _, path := range requested {
		// A generated archive page is numeric (/sdby/2.htm); the slug first
		// page /sdby/yrgs.htm is expected traffic and must not trip the guard.
		if strings.HasPrefix(path, "/sdby/") && trailingPageNumber(path) > 0 {
			t.Fatalf("generated archive page was requested for an empty list: %#v", requested)
		}
	}
}

func TestFdcListPageUsesReverseMappingAndRejectsOutOfRange(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	oldBase := AllSites["fdc"].BaseURL
	requested := make([]string, 0, 5)
	defaultClient = &http.Client{Transport: fdcTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.Path)
		switch req.URL.Path {
		case "/tzgg.htm":
			return fdcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readFdcFixture(t, "fdc_live_tzgg_first.html")), nil
		case "/tzgg/2.htm":
			return fdcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readFdcFixture(t, "fdc_live_tzgg_middle.html")), nil
		case "/tzgg/1.htm":
			return fdcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readFdcFixture(t, "fdc_live_tzgg_last.html")), nil
		default:
			return fdcTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
	})}
	AllSites["fdc"].BaseURL = fdcFixtureBaseURL
	NoCache = true
	ClearCache()
	t.Cleanup(func() {
		defaultClient, NoCache = oldClient, oldNoCache
		AllSites["fdc"].BaseURL = oldBase
		ClearCache()
	})

	items, err := AllSites["fdc"].ListNewsPage("tzgg", 2)
	if err != nil || len(items) != 10 || items[0].URL != fdcFixtureBaseURL+"/info/1118/10493.htm" {
		t.Fatalf("logical page 2 = items=%#v err=%v", items, err)
	}
	if _, err := AllSites["fdc"].ListNewsPage("tzgg", 4); err == nil {
		t.Fatal("out-of-range page was accepted")
	}
	if _, err := AllSites["fdc"].GetTotalPages("tzgg"); err != nil {
		t.Fatalf("total pages after reverse lookup: %v", err)
	}
	for _, path := range requested {
		if path == "/tzgg/0.htm" || path == "/tzgg/4.htm" {
			t.Fatalf("out-of-range physical page was requested: %#v", requested)
		}
	}
}

func TestFdcEmptyListPaginatesQuietly(t *testing.T) {
	data := readFdcFixture(t, "fdc_live_sdby.html")
	items, err := parseAdapter_fdcList(data, fdcFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("measured empty list = %#v, want non-nil empty slice", items)
	}
	if totalItems, currentPage, totalPages, ok := fdcPaginationFromHTML(data); !ok || totalItems != 0 || currentPage != 0 || totalPages != 0 {
		t.Fatalf("measured empty list pagination = %d/%d/%d/%v, want explicit 0/0", totalItems, currentPage, totalPages, ok)
	}
}

func TestFdcFetchArticleVerifiesStatusContentTypeMagicAndExternalLinks(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	oldBase := AllSites["fdc"].BaseURL
	articleURL := "https://fdc-test.invalid/info/1118/9001.htm"
	resourceRequests := make([]string, 0, 5)
	defaultClient = &http.Client{Transport: fdcTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/info/1118/9001.htm":
			return fdcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readFdcFixture(t, "fdc_article_resources.html")), nil
		case "/good.png":
			resourceRequests = append(resourceRequests, req.URL.Path)
			if req.Header.Get("Referer") != articleURL {
				t.Errorf("image Referer = %q, want %q", req.Header.Get("Referer"), articleURL)
			}
			return fdcTestResponse(req, http.StatusOK, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}), nil
		case "/bad.png":
			resourceRequests = append(resourceRequests, req.URL.Path)
			return fdcTestResponse(req, http.StatusOK, "image/png", []byte("<html>请输入验证码</html>")), nil
		case "/missing.png":
			resourceRequests = append(resourceRequests, req.URL.Path)
			return fdcTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		case "/good.pdf":
			resourceRequests = append(resourceRequests, req.URL.Path)
			if req.Header.Get("Referer") != articleURL {
				t.Errorf("attachment Referer = %q, want %q", req.Header.Get("Referer"), articleURL)
			}
			return fdcTestResponse(req, http.StatusOK, "application/pdf", []byte("%PDF-1.7\nfixture")), nil
		case "/bad.pdf":
			resourceRequests = append(resourceRequests, req.URL.Path)
			return fdcTestResponse(req, http.StatusOK, "application/pdf", []byte("<!DOCTYPE html><html>请输入验证码下载附件</html>")), nil
		default:
			return fdcTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
	})}
	AllSites["fdc"].BaseURL = "https://fdc-test.invalid"
	NoCache = true
	ClearCache()
	t.Cleanup(func() {
		defaultClient, NoCache = oldClient, oldNoCache
		AllSites["fdc"].BaseURL = oldBase
		ClearCache()
	})

	article, raw, err := fdcFetchArticle(articleURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || article.Date != "2026-08-31" || article.PublishedAt != "" {
		t.Fatalf("fetched article/date = raw=%d article=%#v", len(raw), article)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://fdc-test.invalid/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "有效附件.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "bad.png") || strings.Contains(article.ContentHTML, "missing.png") || strings.Contains(article.ContentHTML, "bad.pdf") || strings.Contains(article.ContentHTML, `href="vurl"`) {
		t.Fatalf("invalid/pseudo resources remained in ContentHTML: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, `src="https://fdc-test.invalid/good.png"`) || !strings.Contains(article.ContentHTML, `href="https://example.com/notice"`) {
		t.Fatalf("valid image or external link was lost: %s", article.ContentHTML)
	}
	if len(resourceRequests) != 5 {
		t.Fatalf("resource requests = %#v, want one status/type/magic check per candidate", resourceRequests)
	}
	if got := fdcResourceURL("https://fdc-test.invalid", "https://example.com/foreign.pdf"); got != "" {
		t.Fatalf("external file was admitted as downloadable resource: %q", got)
	}
}

type fdcTestRoundTripper func(*http.Request) (*http.Response, error)

func (f fdcTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func fdcTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}
}
