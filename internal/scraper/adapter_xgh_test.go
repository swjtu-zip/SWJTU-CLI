package scraper

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func xghReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read xgh fixture %s: %v", name, err)
	}
	return body
}

func TestXghAuditedRoutesAndHooks(t *testing.T) {
	site := AllSites["xgh"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("xgh adapter hooks are incomplete")
	}
	wantTopLevel := []string{"xwzx1", "tzgg", "jcdt", "jgfc", "szyl", "ghdj", "gzzd", "hdyx1", "bszn", "xzzx"}
	if !reflect.DeepEqual(site.SubPages, wantTopLevel) {
		t.Fatalf("top-level xgh feeds = %#v, want %#v", site.SubPages, wantTopLevel)
	}
	wantAll := []string{
		"xwzx1", "tzgg", "jcdt", "jgfc", "jgfc/gwjg", "szyl", "ghdj", "ghdj/djdt", "ghdj/xxyd",
		"gzzd", "gzzd/gjfg", "gzzd/dffg", "gzzd/xxgz", "gzzd/xghzd", "hdyx1", "bszn", "xzzx",
	}
	if !reflect.DeepEqual(xghFeedPages, wantAll) {
		t.Fatalf("audited xgh feeds = %#v, want %#v", xghFeedPages, wantAll)
	}
	for _, excluded := range []string{"gh2", "jdh1", "jyxc", "ghdj/zbgk", "jgfc/jsjy", "jgfc/xjmf", "bszn/zzjs"} {
		if xghKnownFeed(excluded) {
			t.Errorf("static or empty xgh route %q was registered as a feed", excluded)
		}
	}
}

func TestXghHomepageParsesAllArticleModules(t *testing.T) {
	items, err := parseAdapter_xghList(xghReadFixture(t, "xgh_home_live.html"), xghCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("homepage items = %d, want 8: %#v", len(items), items)
	}
	wantTypes := map[string]int{"新闻中心": 2, "通知公告": 2, "基层动态": 2, "活动影像": 2}
	gotTypes := make(map[string]int)
	for _, item := range items {
		gotTypes[item.Type]++
		if strings.Contains(item.Title, "banner") || strings.Contains(item.Title, "工会之家") {
			t.Errorf("homepage chrome leaked into item: %#v", item)
		}
		if item.Type == "活动影像" && item.Date != "" {
			t.Errorf("activity carousel invented date: %#v", item)
		}
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("homepage item types = %#v, want %#v", gotTypes, wantTypes)
	}
	for _, item := range items {
		if item.Title == "“交小苗”科学探索营第六期圆满收官" {
			if item.Date != "2026-07-10" || item.PublishedAt != "" {
				t.Fatalf("summary date polluted publication metadata: %#v", item)
			}
		}
	}
}

func TestXghListUsesScopedContainerAndDateOnly(t *testing.T) {
	cases := []struct {
		fixture string
		count   int
		total   int
		typ     string
	}{
		{fixture: "xgh_list_first_live.html", count: 3, total: 284, typ: "新闻中心"},
		{fixture: "xgh_list_middle_live.html", count: 2, total: 48, typ: "基层动态"},
		{fixture: "xgh_list_last_live.html", count: 2, total: 8, typ: "校工会制度"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			body := xghReadFixture(t, tc.fixture)
			items, err := parseAdapter_xghList(body, xghCanonicalBaseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.count {
				t.Fatalf("items = %d, want %d: %#v", len(items), tc.count, items)
			}
			for _, item := range items {
				if item.Type != tc.typ && tc.fixture != "xgh_list_last_live.html" {
					t.Errorf("item type = %q, want %q: %#v", item.Type, tc.typ, item)
				}
				if item.Date == "" || strings.Contains(item.PublishedAt, ":") {
					t.Errorf("date-only list timestamp was not preserved conservatively: %#v", item)
				}
			}
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
			if err != nil {
				t.Fatal(err)
			}
			if got := xghPageCount(doc); got != tc.total {
				t.Fatalf("page count = %d, want %d", got, tc.total)
			}
		})
	}
}

func TestXghReversePaginationAndOutOfRange(t *testing.T) {
	const base = "https://xgh.swjtu.edu.cn"
	cases := []struct {
		slug  string
		page  int
		total int
		want  string
	}{
		{slug: "xwzx1", page: 1, total: 284, want: base + "/xwzx1.htm"},
		{slug: "xwzx1", page: 2, total: 284, want: base + "/xwzx1/283.htm"},
		{slug: "xwzx1", page: 143, total: 284, want: base + "/xwzx1/142.htm"},
		{slug: "xwzx1", page: 284, total: 284, want: base + "/xwzx1/1.htm"},
		{slug: "jcdt", page: 2, total: 48, want: base + "/jcdt/47.htm"},
		{slug: "jcdt", page: 48, total: 48, want: base + "/jcdt/1.htm"},
	}
	for _, tc := range cases {
		if got := xghPageURL(base, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("xghPageURL(%q,%d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
	for _, tc := range []struct {
		slug  string
		page  int
		total int
	}{
		{slug: "xwzx1", page: 0, total: 284},
		{slug: "xwzx1", page: 285, total: 284},
		{slug: "jcdt", page: 49, total: 48},
		{slug: "jdh1", page: 1, total: 1},
	} {
		if got := xghPageURL(base, tc.slug, tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range/static xgh page returned %q for %#v", got, tc)
		}
	}
}

func TestXghRejectsStaticEmptyAndWAFResponses(t *testing.T) {
	for _, fixture := range []string{"xgh_empty_list.html", "xgh_static_shell.html", "xgh_waf.html"} {
		t.Run(fixture, func(t *testing.T) {
			if _, err := parseAdapter_xghList(xghReadFixture(t, fixture), xghCanonicalBaseURL); err == nil {
				t.Fatalf("%s was accepted as an article list", fixture)
			}
		})
	}
	if _, err := parseAdapter_xghArticle(xghReadFixture(t, "xgh_waf.html"), xghCanonicalBaseURL); err == nil {
		t.Fatal("WAF response was accepted as an article")
	}
}

func TestXghArticleUsesFixedMetadataAndScopedBody(t *testing.T) {
	article, err := parseAdapter_xghArticle(xghReadFixture(t, "xgh_article_live.html"), xghCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于开展主题线上知识竞答活动的通知" || article.Type != "通知公告" || article.Date != "2026-06-29" || article.PublishedAt != "2026-06-29 14:57:00" {
		t.Fatalf("fixed metadata = title=%q type=%q date=%q published=%q", article.Title, article.Type, article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "活动时间：2026年6月29日-2026年7月31日") {
		t.Fatalf("article body missing: %q", article.Content)
	}
	if strings.Contains(article.Content, "首页壳链接") || strings.Contains(article.ContentHTML, "通知公告</h3>") || strings.Contains(article.ContentHTML, "正文外附件") {
		t.Fatalf("article chrome leaked into body: content=%q html=%q", article.Content, article.ContentHTML)
	}
	if len(article.Images) != 2 || len(article.Attachments) != 4 {
		t.Fatalf("unverified fixture resources = images %#v attachments %#v", article.Images, article.Attachments)
	}
}

func TestXghDateOnlyDetailDoesNotInventClock(t *testing.T) {
	article, err := parseAdapter_xghArticle(xghReadFixture(t, "xgh_article_date_only.html"), xghCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2025-09-12" || article.PublishedAt != "" {
		t.Fatalf("date-only detail = date=%q published=%q", article.Date, article.PublishedAt)
	}
}

type xghTestRoundTripper func(*http.Request) (*http.Response, error)

func (f xghTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func xghTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func TestXghFetchFiltersHTTPTypeMagicAndCaptchaResources(t *testing.T) {
	oldBase := AllSites["xgh"].BaseURL
	oldClient := defaultClient
	oldNoCache := NoCache
	t.Cleanup(func() {
		AllSites["xgh"].BaseURL = oldBase
		defaultClient = oldClient
		NoCache = oldNoCache
	})

	captcha := xghReadFixture(t, "xgh_attachment_captcha.html")
	serverBase := "https://xgh.test"
	AllSites["xgh"].BaseURL = serverBase
	defaultClient = &http.Client{Transport: xghTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/info/1005/9005.htm":
			return xghTestResponse(req, http.StatusOK, "text/html; charset=utf-8", xghReadFixture(t, "xgh_article_live.html")), nil
		case "/img/good.png":
			return xghTestResponse(req, http.StatusOK, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}), nil
		case "/img/bad.png":
			return xghTestResponse(req, http.StatusOK, "image/png", []byte("<html>not an image</html>")), nil
		case "/files/ok.pdf", "/files/outside.pdf":
			return xghTestResponse(req, http.StatusOK, "application/pdf", []byte("%PDF-1.7\n")), nil
		case "/files/wrong.pdf":
			return xghTestResponse(req, http.StatusOK, "application/pdf", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}), nil
		case "/files/captcha.pdf":
			return xghTestResponse(req, http.StatusOK, "text/html; charset=utf-8", captcha), nil
		case "/files/footer.pdf":
			return xghTestResponse(req, http.StatusOK, "application/pdf", []byte("%PDF-1.7\n")), nil
		default:
			return xghTestResponse(req, http.StatusNotFound, "text/html", []byte("not found")), nil
		}
	})}
	NoCache = true

	article, body, err := xghFetchArticle(serverBase + "/info/1005/9005.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 || article.Title == "" {
		t.Fatalf("fetch result lost raw article: article=%#v body=%d", article, len(body))
	}
	if len(article.Images) != 1 || article.Images[0] != serverBase+"/img/good.png" {
		t.Fatalf("HTTP/content-type/magic image filtering = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("HTTP/content-type/magic/CAPTCHA attachment filtering = %#v", article.Attachments)
	}
	for _, attachment := range article.Attachments {
		if attachment.URL != serverBase+"/files/ok.pdf" && attachment.URL != serverBase+"/files/outside.pdf" {
			t.Fatalf("invalid attachment survived: %#v", article.Attachments)
		}
	}
	if strings.Contains(article.ContentHTML, "bad.png") || strings.Contains(article.ContentHTML, "href=\"https://xgh.test/files/wrong.pdf\"") || strings.Contains(article.ContentHTML, "href=\"https://xgh.test/files/captcha.pdf\"") || strings.Contains(article.ContentHTML, "javascript:") {
		t.Fatalf("invalid resources survived ContentHTML: %q", article.ContentHTML)
	}
}

func TestXghArticleFormWrappedTitleBoxParsesFixedMetadata(t *testing.T) {
	// The current live template nests .title inside
	// <form name="_newscontent_fromname">, which the old direct-child lookup
	// missed (every fresh article fetch failed with incomplete metadata).
	article, err := parseAdapter_xghArticle(xghReadFixture(t, "xgh_article_form_wrapped_live.html"), xghCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于开展2026年新入职教职工团队素质拓展活动的通知" || article.Type != "通知公告" || article.Date != "2026-09-30" || article.PublishedAt != "2026-09-30 13:00:00" {
		t.Fatalf("form-wrapped metadata = title=%q type=%q date=%q published=%q", article.Title, article.Type, article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "各部门工会") {
		t.Fatalf("form-wrapped article body missing: %q", article.Content)
	}
}

func TestXghIndexFeedListsHomepage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.htm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(xghReadFixture(t, "xgh_home_live.html"))
	}))
	defer server.Close()

	site := AllSites["xgh"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	})

	items, err := site.ListNewsPage("index", 1)
	if err != nil || len(items) != 8 {
		t.Fatalf("index items = %d, err=%v", len(items), err)
	}
	if total, err := site.GetTotalPages("index"); err != nil || total != 1 {
		t.Fatalf("index total = %d, err=%v", total, err)
	}
	if _, err := site.ListNewsPage("index", 2); err == nil {
		t.Fatal("index page 2 was accepted")
	}
}
