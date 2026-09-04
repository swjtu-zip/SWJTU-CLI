package scraper

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

func readDBFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read db fixture %s: %v", name, err)
	}
	return body
}

func TestDBAdapterRegistrationAndAllArticleFeeds(t *testing.T) {
	site := AllSites["db"]
	if site == nil || site.BaseURL != dbBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.ListPageFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("db adapter hooks are incomplete: %#v", site)
	}
	want := []string{
		"xwdt", "tzgg", "bmgz/bmdt", "bmgz/jzzm", "bmgz/bmgzbslc",
		"bmgz/1796", "bmgz/1806", "bmgz/1816", "fwzn", "fwzn/gzcx", "gzzd", "llqy",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("db feeds = %#v, want %#v", site.SubPages, want)
	}
	for _, static := range []string{
		"bmgk/bmjs", "bmgk/bmld", "bmgk/gzry", "bmgk/kssz/msk", "bmgk/kssz/zhk",
		"bmgk/kssz/jyk", "bmgk/kssz/bmk", "bmgz", "bmgz/bmgzbslc/static",
	} {
		for _, feed := range site.SubPages {
			if feed == static {
				t.Errorf("static page registered as feed: %q", static)
			}
		}
	}
}

func TestDBHomepageScopesBothArticleModules(t *testing.T) {
	items, err := parseAdapter_dbList(readDBFixture(t, "db_home.html"), dbBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("homepage items = %d, want 4: %#v", len(items), items)
	}
	types := make(map[string]int)
	for _, item := range items {
		types[item.Type]++
		if strings.Contains(item.URL, "9999") || strings.Contains(item.URL, "gov.cn") {
			t.Errorf("homepage decoration leaked: %#v", item)
		}
		if item.Date == "" || item.PublishedAt != item.Date {
			t.Errorf("homepage date metadata = %#v", item)
		}
	}
	if !reflect.DeepEqual(types, map[string]int{"新闻动态": 2, "通知公告": 2}) {
		t.Fatalf("homepage types = %#v", types)
	}
}

func TestDBListParsesRowsAndLogicalPageCount(t *testing.T) {
	items, err := parseAdapter_dbList(readDBFixture(t, "db_list.html"), dbBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Title != "列表第一条真实文章" || items[0].Date != "2026-07-13" || items[0].PublishedAt != "2026-07-13" || items[0].Type != "新闻动态" {
		t.Fatalf("list metadata = %#v", items)
	}
	if got := dbTotalPagesFromHTML(readDBFixture(t, "db_list.html")); got != 2 {
		t.Fatalf("logical total pages = %d, want 2", got)
	}
	if got := dbRouteURL(dbBaseURL, dbRoutes["xwdt"], 2, 2); got != dbBaseURL+"/xwdt/1.htm" {
		t.Fatalf("reverse page URL = %q", got)
	}
	if got := dbRouteURL(dbBaseURL, dbRoutes["tzgg"], 2, 18); got != dbBaseURL+"/tzgg/1.htm" {
		t.Fatalf("forward generated page URL = %q", got)
	}
	if got := dbRouteURL(dbBaseURL, dbRoutes["tzgg"], 18, 18); got != dbBaseURL+"/tzgg/17.htm" {
		t.Fatalf("forward last page URL = %q", got)
	}
	if got := dbRouteURL(dbBaseURL, dbRoutes["tzgg"], 19, 18); got != "" {
		t.Fatalf("out-of-range page URL = %q", got)
	}
	if got := dbRouteURL(dbBaseURL, dbRoutes["llqy"], 16, 31); got != dbBaseURL+"/llqy/16.htm" {
		t.Fatalf("theory middle page URL = %q", got)
	}
}

func TestDBHiddenAndSecondaryArticleFeeds(t *testing.T) {
	procedures, err := parseAdapter_dbList(readDBFixture(t, "db_fwzn_gzcx_live.html"), dbBaseURL)
	if err != nil {
		t.Fatalf("parse hidden procedure feed: %v", err)
	}
	if len(procedures) != 8 || procedures[0].Type != "工作程序" || procedures[0].Date != "2018-11-02" || procedures[7].Date != "2013-06-24" {
		t.Fatalf("procedure feed = %#v", procedures)
	}
	theory, err := parseAdapter_dbList(readDBFixture(t, "db_llqy_live.html"), dbBaseURL)
	if err != nil {
		t.Fatalf("parse hidden theory feed: %v", err)
	}
	if len(theory) != 10 || theory[0].Type != "理论前沿" || theory[0].Date != "2023-06-15" {
		t.Fatalf("theory feed = %#v", theory)
	}
	if got := dbTotalPagesFromHTML(readDBFixture(t, "db_llqy_live.html")); got != 31 {
		t.Fatalf("theory logical total = %d, want 31", got)
	}
	if _, err := parseAdapter_dbList(readDBFixture(t, "db_bmgz_landing_live.html"), dbBaseURL); err == nil {
		t.Fatal("bmgz category landing was accepted as an article list")
	}
}

func TestDBResourceListAcceptsContentJSPDetails(t *testing.T) {
	items, err := parseAdapter_dbList(readDBFixture(t, "db_resource_list.html"), dbBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("resource list items = %#v", items)
	}
	if !strings.HasPrefix(items[0].URL, dbBaseURL+"/content.jsp?") || items[0].Type != "涉密人员相关下载" || items[0].Date != "2025-05-14" {
		t.Fatalf("resource list first item = %#v", items[0])
	}
	query, err := url.Parse(items[0].URL)
	if err != nil || query.Query().Get("wbtreeid") != "1796" || query.Query().Get("wbnewsid") != "7193" {
		t.Fatalf("content.jsp URL = %q", items[0].URL)
	}
	if got := dbTotalPagesFromHTML(readDBFixture(t, "db_resource_list.html")); got != 2 {
		t.Fatalf("resource total pages = %d, want 2", got)
	}
}

func TestDBArticleExtractsMetadataBodyImageAndOuterAttachment(t *testing.T) {
	article, err := parseAdapter_dbArticle(readDBFixture(t, "db_article.html"), dbBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "测试正文、图片和附件" || article.Type != "新闻动态" || article.Source != "新闻中心" || article.Author != "张三" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:27" {
		t.Fatalf("article date = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || len(article.Images) != 1 || article.Images[0] != dbBaseURL+"/__local/body.png" {
		t.Fatalf("article body/images = %#v", article)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "测试附件.pdf" || !strings.HasPrefix(article.Attachments[0].URL, dbBaseURL+"/system/_content/download.jsp?") {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, `src="`+dbBaseURL+`/__local/body.png"`) {
		t.Fatalf("resolved image missing from ContentHTML: %s", article.ContentHTML)
	}
}

func TestDBSourceFieldHandlesInfoLabelEntitiesAndNBSP(t *testing.T) {
	article, err := parseAdapter_dbArticle(readDBFixture(t, "db_info_source_article.html"), dbBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Source != "党委办公室&保密工作" {
		t.Fatalf("source = %q, want entity-decoded source without label", article.Source)
	}
	if article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("published_at = %q, want minute precision", article.PublishedAt)
	}
	for _, source := range []string{"来源：新闻中心 作者：张三", "来源:新闻中心 作者：张三", "信息来源：党委办公室 作者：张三"} {
		got := dbField(source, "来源", "作者")
		if got == "" {
			got = dbField(source, "信息来源", "作者")
		}
		if got == "" || (got != "新闻中心" && got != "党委办公室") {
			t.Errorf("dbField(%q) = %q", source, got)
		}
	}
}

func TestDBArticleDateUsesOnlyFixedSourceAndPreservesDateOnly(t *testing.T) {
	article, err := parseAdapter_dbArticle(readDBFixture(t, "db_article_dates_live.html"), dbBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于会议安排的通知" || article.Type != "通知公告" || article.Source != "党委办公室" || article.Author != "李四" {
		t.Fatalf("fixed metadata = %#v", article)
	}
	if article.Date != "2024-09-04" || article.PublishedAt != "2024-09-04" {
		t.Fatalf("date-only metadata = %q/%q", article.Date, article.PublishedAt)
	}
	if strings.Contains(article.Content, "2099-12-31") || !strings.Contains(article.Content, "2026年9月16日") {
		t.Fatalf("content scope/date contamination = %q", article.Content)
	}
	for input, want := range map[string]string{
		"日期：2026-8-1 9:03:04": "2026-08-01 09:03:04",
		"日期：2026-08-01 09:03": "2026-08-01 09:03:00",
	} {
		_, got := dbArticleDate(input)
		if got != want {
			t.Errorf("dbArticleDate(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDBArticleTypeUsesSpecificBreadcrumbRoute(t *testing.T) {
	article, err := parseAdapter_dbArticle(readDBFixture(t, "db_bmgz_article_live.html"), dbBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "保密动态" || article.Date != "2025-04-10" || article.PublishedAt != "2025-04-10 08:13:43" {
		t.Fatalf("bmgz detail metadata = %#v", article)
	}
	if strings.Contains(article.Content, "2026年4月10日") == false {
		t.Fatalf("article body was not retained: %q", article.Content)
	}
}

func TestDBContentJSPArticleUsesTreeTypeAndAttachment(t *testing.T) {
	article, err := parseAdapter_dbArticle(readDBFixture(t, "db_content_article.html"), dbBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "涉密人员相关下载" || article.Date != "2025-05-14" || article.PublishedAt != "2025-05-14 09:32:04" {
		t.Fatalf("content article metadata = %#v", article)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "审批表.pdf" {
		t.Fatalf("content article attachment = %#v", article.Attachments)
	}
}

func TestDBRejectsStaticAndVerificationPages(t *testing.T) {
	if _, err := parseAdapter_dbList(readDBFixture(t, "db_static.html"), dbBaseURL); err == nil {
		t.Fatal("static page was accepted as an article list")
	}
	if _, err := parseAdapter_dbArticle(readDBFixture(t, "db_static.html"), dbBaseURL); err == nil {
		t.Fatal("static page was accepted as an article")
	}
	if _, err := parseAdapter_dbList(readDBFixture(t, "db_captcha.html"), dbBaseURL); err == nil {
		t.Fatal("captcha page was accepted as a list")
	}
	if _, err := parseAdapter_dbArticle(readDBFixture(t, "db_captcha.html"), dbBaseURL); err == nil {
		t.Fatal("captcha page was accepted as an article")
	}
}

func TestDBListPageUsesReverseAndQueryPagination(t *testing.T) {
	listFixture := readDBFixture(t, "db_list.html")
	resourceFixture := readDBFixture(t, "db_resource_list.html")
	forwardFixture := readDBFixture(t, "db_tzgg_live.html")
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		var body []byte
		switch r.URL.Path {
		case "/xwdt.htm", "/xwdt/1.htm":
			body = listFixture
		case "/tzgg.htm", "/tzgg/1.htm", "/tzgg/17.htm":
			body = forwardFixture
		case "/list.jsp":
			body = resourceFixture
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	site := AllSites["db"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	site.BaseURL = server.URL
	NoCache = true
	defer func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
	}()

	if _, total, err := dbListPage("xwdt", 2); err != nil || total != 2 {
		t.Fatalf("reverse list page = total %d, err %v", total, err)
	}
	if len(requests) != 2 || requests[1] != "/xwdt/1.htm" {
		t.Fatalf("reverse requests = %#v", requests)
	}
	requests = nil
	if _, total, err := dbListPage("bmgz/1796", 2); err != nil || total != 2 {
		t.Fatalf("query list page = total %d, err %v", total, err)
	}
	if len(requests) != 2 {
		t.Fatalf("query requests = %#v", requests)
	}
	query, err := url.Parse(requests[1])
	if err != nil || query.Query().Get("PAGENUM") != "2" || query.Query().Get("totalpage") != "2" || query.Query().Get("wbtreeid") != "1796" {
		t.Fatalf("query pagination request = %q", requests[1])
	}
	requests = nil
	if _, total, err := dbListPage("tzgg", 18); err != nil || total != 18 {
		t.Fatalf("forward last page = total %d, err %v", total, err)
	}
	if len(requests) != 2 || requests[1] != "/tzgg/17.htm" {
		t.Fatalf("forward last-page requests = %#v", requests)
	}
	requests = nil
	if _, total, err := dbListPage("tzgg", 19); err == nil || total != 18 {
		t.Fatalf("out-of-range forward page = total %d, err %v", total, err)
	}
	if len(requests) != 1 || requests[0] != "/tzgg.htm" {
		t.Fatalf("out-of-range requests = %#v", requests)
	}
}

type dbTestRoundTripper func(*http.Request) (*http.Response, error)

func (f dbTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func dbTestHTTPResponse(request *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}
}

func TestDBFetchArticleVerifiesStatusContentTypeMagicAndExternalResources(t *testing.T) {
	articleHTML := strings.ReplaceAll(string(readDBFixture(t, "db_article_resources_live.html")), "__DB_BASE__", dbBaseURL)
	oldClient, oldNoCache := defaultClient, NoCache
	defaultClient = &http.Client{Transport: dbTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		contentType := "application/octet-stream"
		status := http.StatusOK
		var body []byte
		switch request.URL.Hostname() + request.URL.Path {
		case "db.swjtu.edu.cn/info/1004/9002.htm":
			contentType, body = "text/html; charset=utf-8", []byte(articleHTML)
		case "db.swjtu.edu.cn/good.png":
			contentType, body = "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
		case "db.swjtu.edu.cn/bad.png":
			contentType, body = "image/png", []byte("<html><body>请输入验证码</body></html>")
		case "db.swjtu.edu.cn/bad-magic.png":
			contentType, body = "image/png", []byte("not an image")
		case "cdn.example.test/external.gif":
			contentType, body = "image/gif", []byte("GIF89a\x00\x00")
		case "db.swjtu.edu.cn/good.pdf":
			contentType, body = "application/pdf", []byte("%PDF-1.7\n")
		case "db.swjtu.edu.cn/bad.pdf":
			contentType, body = "application/pdf", []byte("not a PDF")
		case "db.swjtu.edu.cn/bad-status.pdf":
			contentType, status, body = "application/pdf", http.StatusNotFound, []byte("%PDF-1.7\n")
		case "db.swjtu.edu.cn/captcha.xlsx":
			contentType, body = "text/html; charset=utf-8", []byte("<!doctype html><html><body>验证码下载附件</body></html>")
		default:
			status, body = http.StatusNotFound, []byte("not found")
		}
		return dbTestHTTPResponse(request, status, contentType, body), nil
	})}
	NoCache = true
	t.Cleanup(func() { defaultClient, NoCache = oldClient, oldNoCache; ClearCache() })

	article, raw, err := dbFetchArticle(dbBaseURL + "/info/1004/9002.htm")
	if err != nil {
		t.Fatalf("fetch db article: %v", err)
	}
	if len(raw) == 0 || len(article.Images) != 2 || article.Images[0] != dbBaseURL+"/good.png" || article.Images[1] != "https://cdn.example.test/external.gif" {
		t.Fatalf("verified images = raw %d, %#v", len(raw), article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != dbBaseURL+"/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, dbBaseURL+"/good.png") || !strings.Contains(article.ContentHTML, dbBaseURL+"/good.pdf") || !strings.Contains(article.ContentHTML, "https://cdn.example.test/external.gif") {
		t.Fatalf("verified resources missing from ContentHTML: %q", article.ContentHTML)
	}
	for _, rejected := range []string{"bad.png", "bad-magic.png", "bad.pdf", "bad-status.pdf", "captcha.xlsx", "javascript:download"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource remained in ContentHTML: %q", rejected)
		}
	}
	if got := dbVerifyAttachments([]model.Attachment{
		{Name: "有效.pdf", URL: dbBaseURL + "/good.pdf"},
		{Name: "错误.pdf", URL: dbBaseURL + "/bad.pdf"},
	}, dbBaseURL); len(got) != 1 || got[0].URL != dbBaseURL+"/good.pdf" {
		t.Fatalf("direct attachment verification = %#v", got)
	}
}
