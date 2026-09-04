package scraper

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

const bwcFixtureBase = "https://bwc.swjtu.edu.cn"

func readBWCFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read BWC fixture %s: %v", name, err)
	}
	return data
}

func TestBwcAdapterRegistrationAndFeeds(t *testing.T) {
	site := AllSites["bwc"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil {
		t.Fatalf("BWC adapter hooks are incomplete: %#v", site)
	}

	want := []string{
		"tzgg", "gzdt", "fwzn", "fwzn/bslc", "aqjs", "aqjs/aqjy", "wzgz/zbgz",
		"zcfg/flfg", "zcfg/gzzd", "English/News", "English/Laws", "English/Rules",
		"English/Announcement", "English/Service_Guide", "English/Service_Guide/Work_Processes",
	}
	if got := strings.Join(site.SubPages, "\x00"); got != strings.Join(want, "\x00") {
		t.Fatalf("BWC article-list routes = %#v, want %#v", site.SubPages, want)
	}
	for _, slug := range []string{
		"jgsz/bmgk", "bszn/hzyw", "aqjs/jsal", "djzj/xxzj", "fwzn/xzzx", "zcfg",
		"English/About", "English/Party_s_Works", "English/Party_s_Works/Honest_Administration",
		"English/Party_s_Works/Party_Branch_Works", "English/Service_Guide/Download",
	} {
		for _, registered := range site.SubPages {
			if registered == slug {
				t.Errorf("non-feed/static/empty route %q was registered", slug)
			}
		}
	}
}

func TestBwcPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	tests := []struct {
		slug, want  string
		page, total int
	}{
		{slug: "tzgg", page: 1, total: 87, want: bwcFixtureBase + "/tzgg.htm"},
		{slug: "tzgg", page: 2, total: 87, want: bwcFixtureBase + "/tzgg/86.htm"},
		{slug: "tzgg", page: 44, total: 87, want: bwcFixtureBase + "/tzgg/44.htm"},
		{slug: "tzgg", page: 86, total: 87, want: bwcFixtureBase + "/tzgg/2.htm"},
		{slug: "tzgg", page: 87, total: 87, want: bwcFixtureBase + "/tzgg/1.htm"},
		{slug: "wzgz/zbgz", page: 2, total: 4, want: bwcFixtureBase + "/wzgz/zbgz/3.htm"},
		{slug: "English/Announcement", page: 2, total: 4, want: bwcFixtureBase + "/English/Announcement/3.htm"},
	}
	for _, tt := range tests {
		if got := bwcPageURL(bwcFixtureBase, tt.slug, tt.page, tt.total, ".htm"); got != tt.want {
			t.Errorf("bwcPageURL(%q, %d/%d) = %q, want %q", tt.slug, tt.page, tt.total, got, tt.want)
		}
	}
}

func TestBwcLegacyWzgzAliasUsesAuditedCanonicalList(t *testing.T) {
	if got := bwcCanonicalSlug("/wzgz/dxszbgz/"); got != "wzgz/zbgz" {
		t.Fatalf("legacy 武装工作 route canonicalized to %q", got)
	}
	if !bwcIsKnownListRoute("wzgz/dxszbgz") {
		t.Fatal("legacy 武装工作 route is not accepted as an audited alias")
	}
}

func TestBwcPageURLRejectsOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 87},
		{name: "past last", page: 88, totalPages: 87},
		{name: "past one-page feed", page: 2, totalPages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bwcPageURL(bwcFixtureBase, "tzgg", tc.page, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("bwcPageURL() = %q for page %d/%d, want empty URL", got, tc.page, tc.totalPages)
			}
		})
	}
}

func TestBwcHomepageScopesOnlyRealTabs(t *testing.T) {
	items, err := parseAdapter_bwcList(readBWCFixture(t, "bwc_home.html"), bwcFixtureBase)
	if err != nil {
		t.Fatalf("parse BWC homepage: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("homepage items = %d, want five scoped module items: %#v", len(items), items)
	}

	wantTypes := map[string]string{
		"工作动态文章": "工作动态",
		"通知公告文章": "通知公告",
		"安全警示文章": "安全警示",
		"政策法规文章": "政策法规",
		"下载专区文章": "下载专区",
	}
	for _, item := range items {
		if want, ok := wantTypes[item.Title]; !ok || item.Type != want {
			t.Errorf("homepage item type mismatch: %#v", item)
		}
		if strings.Contains(item.URL, "/1080/") || strings.Contains(item.URL, "/1079/") {
			t.Errorf("carousel/recommendation URL leaked into homepage feed: %#v", item)
		}
	}
	if got := bwcTotalPages(readBWCFixture(t, "bwc_home.html")); got != 1 {
		t.Fatalf("homepage total pages = %d, want 1", got)
	}
}

func TestBwcChineseListAndPagination(t *testing.T) {
	data := readBWCFixture(t, "bwc_list.html")
	items, err := parseAdapter_bwcList(data, bwcFixtureBase)
	if err != nil {
		t.Fatalf("parse BWC Chinese list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Chinese list items = %d, want 2: %#v", len(items), items)
	}
	if items[0].Title != "第一条通知公告" || items[0].Date != "2026-08-31" || items[0].Type != "通知公告" {
		t.Errorf("first Chinese list item = %#v", items[0])
	}
	if items[1].URL != bwcFixtureBase+"/info/1013/8845.htm" {
		t.Errorf("second Chinese list URL = %q", items[1].URL)
	}
	if got := bwcTotalPages(data); got != 87 {
		t.Fatalf("Chinese total pages = %d, want 87", got)
	}
}

func TestBwcChineseListSamplesFirstMiddleLastAndRejectsOOBShell(t *testing.T) {
	cases := []struct {
		fixture string
		urlPart string
		title   string
	}{
		{fixture: "bwc_list.html", urlPart: "/info/1013/8846.htm", title: "第一条通知公告"},
		{fixture: "bwc_list_middle.html", urlPart: "/info/1013/7800.htm", title: "中间页通知公告"},
		{fixture: "bwc_list_last.html", urlPart: "/info/1013/1001.htm", title: "最末页通知公告"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			items, err := parseAdapter_bwcList(readBWCFixture(t, tc.fixture), bwcFixtureBase)
			if err != nil || len(items) != 2 {
				t.Fatalf("list sample = items=%#v err=%v", items, err)
			}
			if items[0].Title != tc.title || items[0].URL != bwcFixtureBase+tc.urlPart || items[0].Type != "通知公告" {
				t.Fatalf("first sample item = %#v", items[0])
			}
			if items[0].PublishedAt != "" {
				t.Fatalf("date-only list item invented a clock: %#v", items[0])
			}
		})
	}
	if _, err := parseAdapter_bwcList(readBWCFixture(t, "bwc_out_of_range.html"), bwcFixtureBase); err == nil {
		t.Fatal("out-of-range/WAF fixture was accepted as a list")
	}
}

func TestBwcListPageFetchesReverseFirstMiddleLastAndNeverOOB(t *testing.T) {
	requested := make([]string, 0, 6)
	site := AllSites["bwc"]
	oldClient, oldBase, oldNoCache := defaultClient, site.BaseURL, NoCache
	defaultClient = &http.Client{Transport: bwcTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.Path)
		switch req.URL.Path {
		case "/tzgg.htm":
			return bwcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readBWCFixture(t, "bwc_list.html")), nil
		case "/tzgg/44.htm":
			return bwcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readBWCFixture(t, "bwc_list_middle.html")), nil
		case "/tzgg/1.htm":
			return bwcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readBWCFixture(t, "bwc_list_last.html")), nil
		default:
			return bwcTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", readBWCFixture(t, "bwc_out_of_range.html")), nil
		}
	})}
	site.BaseURL, NoCache = "https://bwc.test", true
	ClearCache()
	t.Cleanup(func() {
		defaultClient, site.BaseURL, NoCache = oldClient, oldBase, oldNoCache
		ClearCache()
	})

	for _, tc := range []struct {
		name  string
		page  int
		want  string
		count int
	}{
		{name: "middle", page: 44, want: "中间页通知公告", count: 2},
		{name: "last", page: 87, want: "最末页通知公告", count: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := bwcListPage("tzgg", tc.page)
			if err != nil || total != 87 || len(items) != tc.count || items[0].Title != tc.want {
				t.Fatalf("logical page %d = items=%#v total=%d err=%v", tc.page, items, total, err)
			}
		})
	}
	if _, _, err := bwcListPage("tzgg", 88); err == nil {
		t.Fatal("logical page 88 was accepted")
	}
	for _, path := range requested {
		if path == "/tzgg/0.htm" || path == "/tzgg/88.htm" {
			t.Fatalf("out-of-range physical page was requested: %#v", requested)
		}
	}
}

func TestBwcEnglishListPreservesSecondPrecision(t *testing.T) {
	data := readBWCFixture(t, "bwc_english_list.html")
	items, err := parseAdapter_bwcList(data, bwcFixtureBase)
	if err != nil {
		t.Fatalf("parse BWC English list: %v", err)
	}
	if len(items) != 2 || items[0].Type != "Announcement" {
		t.Fatalf("English list items = %#v", items)
	}
	if items[0].Date != "2018-01-18" || items[0].PublishedAt != "2018-01-18 10:31:38" {
		t.Errorf("English timestamp not preserved: %#v", items[0])
	}
	if got := bwcTotalPages(data); got != 4 {
		t.Fatalf("English total pages = %d, want 4", got)
	}
}

func TestBwcArticleExtractsMetadataContentImagesAndExternalAttachments(t *testing.T) {
	article, err := parseAdapter_bwcArticle(readBWCFixture(t, "bwc_article.html"), bwcFixtureBase)
	if err != nil {
		t.Fatalf("parse BWC article: %v", err)
	}
	if article.Title != "关于开展安全工作的通知" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 09:27:00" {
		t.Errorf("article metadata = %#v", article)
	}
	if article.Type != "通知公告" || article.Author != "admin" {
		t.Errorf("article type/author = %q/%q", article.Type, article.Author)
	}
	if !strings.Contains(article.Content, "正文第一段") || !strings.Contains(article.Content, "校外车辆及人员来访申请表.docx") {
		t.Errorf("article body is incomplete: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != bwcFixtureBase+"/images/body.jpg" {
		t.Errorf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v, want body and outside-body files", article.Attachments)
	}
	if article.Attachments[0].Name != "校外车辆及人员来访申请表.docx" || article.Attachments[0].URL != bwcFixtureBase+"/XW0001.docx" {
		t.Errorf("body attachment = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "外部附件.pdf" || article.Attachments[1].URL != bwcFixtureBase+"/files/outside.pdf" {
		t.Errorf("outside attachment = %#v", article.Attachments[1])
	}
	if !strings.Contains(article.ContentHTML, "https://bwc.swjtu.edu.cn/images/body.jpg") || !strings.Contains(article.ContentHTML, "https://bwc.swjtu.edu.cn/XW0001.docx") {
		t.Errorf("resolved resources missing from ContentHTML: %s", article.ContentHTML)
	}
}

func TestBwcArticleUsesFixedMetadataAndDateOnlyDoesNotBorrowBodyTimes(t *testing.T) {
	article, err := parseAdapter_bwcArticle(readBWCFixture(t, "bwc_article_date_only.html"), bwcFixtureBase)
	if err != nil {
		t.Fatalf("parse date-only BWC article: %v", err)
	}
	if article.Title != "固定位置标题" || article.Type != "工作动态" || article.Date != "2026-09-02" {
		t.Fatalf("fixed article metadata = %#v", article)
	}
	if article.PublishedAt != "" {
		t.Fatalf("date-only article invented a publication time: %#v", article)
	}
	if strings.Contains(article.Title, "页面 h1 假标题") || strings.Contains(article.Content, "侧栏") {
		t.Fatalf("unscoped shell content leaked into article: %#v", article)
	}
	if !strings.Contains(article.Content, "会议时间 2026年9月16日 14:30") || !strings.Contains(article.Content, "报名截止 2026-09-20 23:59") {
		t.Fatalf("article body lost activity/deadline text: %q", article.Content)
	}
}

func TestBwcRejectsShellsAndUnscopedPages(t *testing.T) {
	if _, err := parseAdapter_bwcArticle(readBWCFixture(t, "bwc_home.html"), bwcFixtureBase); err == nil {
		t.Fatal("homepage shell was accepted as an article")
	}
	if _, err := parseAdapter_bwcList(readBWCFixture(t, "bwc_article.html"), bwcFixtureBase); err == nil {
		t.Fatal("article detail was accepted as a list")
	}
	if _, err := parseAdapter_bwcList([]byte(`<html><body><div id="app"></div></body></html>`), bwcFixtureBase); err == nil {
		t.Fatal("empty SPA shell was accepted as an empty list")
	}
}

func TestBwcFetchArticleVerifiesHTTPContentTypeMagicAndCAPTCHA(t *testing.T) {
	const baseURL = "https://bwc.test"
	fixture := readBWCFixture(t, "bwc_article_resources.html")
	requested := make([]string, 0, 8)
	oldClient, oldNoCache, oldBase := defaultClient, NoCache, AllSites["bwc"].BaseURL
	defaultClient = &http.Client{Transport: bwcTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "bwc.test" {
			return bwcTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
		if req.URL.Path != "/info/1000/9001.htm" {
			requested = append(requested, req.URL.Path)
		}
		switch req.URL.Path {
		case "/info/1000/9001.htm":
			return bwcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", fixture), nil
		case "/good.png":
			if req.Header.Get("Referer") != baseURL+"/info/1000/9001.htm" {
				t.Errorf("image Referer = %q", req.Header.Get("Referer"))
			}
			return bwcTestResponse(req, http.StatusOK, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}), nil
		case "/bad.png":
			return bwcTestResponse(req, http.StatusOK, "image/png", []byte("<html>请输入验证码</html>")), nil
		case "/missing.png":
			return bwcTestResponse(req, http.StatusNotFound, "image/png", []byte("missing")), nil
		case "/good.pdf":
			if req.Header.Get("Referer") != baseURL+"/info/1000/9001.htm" {
				t.Errorf("attachment Referer = %q", req.Header.Get("Referer"))
			}
			return bwcTestResponse(req, http.StatusOK, "application/pdf", []byte("%PDF-1.7 fixture")), nil
		case "/bad.pdf":
			return bwcTestResponse(req, http.StatusOK, "application/pdf", []byte("not a PDF")), nil
		case "/wrong.pdf":
			return bwcTestResponse(req, http.StatusOK, "text/plain", []byte("%PDF-1.7 but wrong Content-Type")), nil
		case "/missing.pdf":
			return bwcTestResponse(req, http.StatusNotFound, "application/pdf", []byte("missing")), nil
		case "/system/_content/download.jsp":
			return bwcTestResponse(req, http.StatusOK, "text/html; charset=utf-8", []byte("<html><body>请输入验证码下载附件</body></html>")), nil
		default:
			return bwcTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
	})}
	AllSites["bwc"].BaseURL, NoCache = baseURL, true
	ClearCache()
	t.Cleanup(func() {
		defaultClient, NoCache, AllSites["bwc"].BaseURL = oldClient, oldNoCache, oldBase
		ClearCache()
	})

	article, raw, err := bwcFetchArticle(baseURL + "/info/1000/9001.htm")
	if err != nil {
		t.Fatalf("fetch BWC article: %v", err)
	}
	if len(raw) == 0 || article.Date != "2026-09-02" || article.PublishedAt != "" {
		t.Fatalf("date-only fetched article = raw=%d article=%#v", len(raw), article)
	}
	if len(article.Images) != 1 || article.Images[0] != baseURL+"/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != baseURL+"/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	for _, rejected := range []string{"bad.png", "missing.png", "captcha.png", "bad.pdf", "wrong.pdf", "missing.pdf", "wbfileid=bad", "请输入验证码", "javascript:"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource survived ContentHTML: %q in %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, `src="`+baseURL+`/good.png"`) || !strings.Contains(article.ContentHTML, `href="`+baseURL+`/good.pdf"`) {
		t.Fatalf("verified resources missing from ContentHTML: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, "https://external.example/reference") || !strings.Contains(article.ContentHTML, "https://external.example/file.pdf") {
		t.Fatalf("ordinary external links were not preserved: %s", article.ContentHTML)
	}
	if len(requested) != 8 {
		t.Fatalf("resource requests = %#v, want image/status/type/magic checks only", requested)
	}
	for _, path := range requested {
		if path == "/captcha.png" {
			t.Fatalf("CAPTCHA image was requested: %#v", requested)
		}
	}
}

func TestBwcResourceURLAndMagicGuards(t *testing.T) {
	if got := bwcResourceURL(bwcFixtureBase, "javascript:download('/x.pdf')"); got != "" {
		t.Fatalf("pseudo resource URL = %q", got)
	}
	if got := bwcResourceURL(bwcFixtureBase, "/captcha/image.png"); got != "" {
		t.Fatalf("CAPTCHA resource URL = %q", got)
	}
	if got := bwcResourceURL(bwcFixtureBase, "https://cdn.example/image.png#fragment"); got != "https://cdn.example/image.png" {
		t.Fatalf("external resource URL = %q", got)
	}
	if !bwcResourceBodyIsUsable("image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "good.png", true) {
		t.Fatal("valid PNG was rejected")
	}
	if bwcResourceBodyIsUsable("image/png", []byte("not a PNG"), "bad.png", true) {
		t.Fatal("wrong image magic was accepted")
	}
	if bwcResourceBodyIsUsable("application/pdf", []byte("<html>请输入验证码下载附件</html>"), "bad.pdf", false) {
		t.Fatal("CAPTCHA HTML was accepted as PDF")
	}
	if bwcResourceBodyIsUsable("text/plain", []byte("%PDF-1.7"), "wrong.pdf", false) {
		t.Fatal("PDF magic with wrong Content-Type was accepted")
	}
}

type bwcTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper bwcTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return roundTripper(req)
}

func bwcTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}
