package scraper

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
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
	if site == nil || site.BaseURL != dbBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.ListPageFunc == nil {
		t.Fatalf("db adapter hooks are incomplete: %#v", site)
	}
	want := []string{
		"xwdt", "tzgg", "bmgz", "bmgz/bmdt", "bmgz/jzzm", "bmgz/bmgzbslc",
		"bmgz/1796", "bmgz/1806", "bmgz/1816", "fwzn", "gzzd",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("db feeds = %#v, want %#v", site.SubPages, want)
	}
	for _, static := range []string{
		"bmgk/bmjs", "bmgk/bmld", "bmgk/gzry", "bmgk/kssz/msk", "bmgk/kssz/zhk",
		"bmgk/kssz/jyk", "bmgk/kssz/bmk", "bmgz/bmgzbslc/static",
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
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		var body []byte
		switch r.URL.Path {
		case "/xwdt.htm", "/xwdt/1.htm":
			body = listFixture
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
}
