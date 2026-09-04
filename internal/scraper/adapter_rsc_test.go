package scraper

import (
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

func readRSCFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read rsc fixture %q: %v", name, err)
	}
	return body
}

func TestRSCAdapterRegistersRecruitmentTabsAndReversePagination(t *testing.T) {
	site := AllSites["rsc"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatal("rsc adapter is incomplete")
	}
	want := []string{
		"xwtz/xwNews", "xwtz/tzNotice", "rszp/shpyryzphgs",
		"xgxz", "xgxz/szrc", "xgxz/rszc", "xgxz/zcps", "xgxz/bsh", "xgxz/ldgz", "xgxz/shbx", "xgxz/zzkyjkyzl",
		"bszn", "bszn/szrc", "bszn/rszc", "bszn/bsh", "bszn/shbx", "cjwt",
	}
	if strings.Join(site.SubPages, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rsc feeds = %#v, want %#v", site.SubPages, want)
	}
	if got := site.PageURL(site.BaseURL, "xwtz/xwNews", 2, 4, ".htm"); got != site.BaseURL+"/xwtz/xwNews/3.htm" {
		t.Fatalf("rsc reverse page URL = %q", got)
	}
	if got := site.PageURL(site.BaseURL, "xwtz/xwNews", 5, 4, ".htm"); got != "" {
		t.Fatalf("rsc out-of-range page URL = %q, want empty", got)
	}
}

func TestRSCHomepageKeepsLocalModulesAndTypes(t *testing.T) {
	items, err := parseAdapter_rscList(readRSCFixture(t, "rsc.html"), "https://rsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 22 {
		t.Fatalf("rsc homepage items = %d, want 22 local article cards", len(items))
	}
	types := map[string]int{}
	for _, item := range items {
		types[item.Type]++
		if strings.Contains(item.URL, "news.swjtu.edu.cn") || strings.Contains(item.URL, "jdpta.com") || strings.Contains(item.URL, "moe.gov.cn") {
			t.Fatalf("external homepage link leaked: %#v", item)
		}
	}
	for _, typ := range []string{"人事新闻", "通知公告", "社会聘用人员招聘和公示", "师资建设", "薪酬社保", "常见问题", "常见下载"} {
		if types[typ] == 0 {
			t.Fatalf("homepage type %q was not retained: %#v", typ, types)
		}
	}
}

func TestRSCListUsesArticleAnchorNotTabAnchor(t *testing.T) {
	html := []byte(`<html><body>
<div class="loaction"><a>首页</a><a>新闻通知</a><a>新闻News</a></div>
<div class="conlistComm"><dl><dt><span><a href="xwtz/xwNews.htm">[新闻News]</a></span><a href="info/1025/9672.htm">列表文章准确标题</a></dt><dd>时间：2026-07-16 14:03</dd></dl><dl><dt><span><a href="xwtz/xwNews.htm">[新闻News]</a></span><a href="https://news.swjtu.edu.cn/info/1/2.htm">外部文章</a></dt><dd>时间：2026-07-16</dd></dl></div>
</body></html>`)
	items, err := parseAdapter_rscList(html, "https://rsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "列表文章准确标题" || items[0].Type != "新闻News" || items[0].Date != "2026-07-16" || items[0].PublishedAt != "2026-07-16 14:03:00" {
		t.Fatalf("rsc list item = %#v", items)
	}
}

func TestRSCArticleKeepsSectionBodyImageAndAttachment(t *testing.T) {
	html := []byte(`<html><body><div class="loaction"><a>首页</a><a>新闻通知</a><a>新闻News</a></div><h1>人事新闻详情标题</h1><div class="news_source"><span class="date">2026-07-16</span></div><div class="v_news_content"><p>人事新闻正文内容。</p><img data-original="/images/body.jpg"><a download="通知附件.pdf" href="/download?id=1">下载附件</a></div></body></html>`)
	article, err := parseAdapter_rscArticle(html, "https://rsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "新闻News" || !strings.Contains(article.Content, "正文内容") || len(article.Images) != 1 || len(article.Attachments) != 1 {
		t.Fatalf("rsc article = %#v", article)
	}
}

func TestRSCMeasuredListFixturesKeepLocalRowsAndReversePages(t *testing.T) {
	cases := []struct {
		fixture string
		count   int
		first   string
	}{
		{"rsc_live_list_first.html", 2, "https://rsc.swjtu.edu.cn/info/1025/10442.htm"},
		{"rsc_live_list_middle.html", 1, "https://rsc.swjtu.edu.cn/info/1025/10142.htm"},
		{"rsc_live_list_last.html", 1, "https://rsc.swjtu.edu.cn/info/1025/9582.htm"},
	}
	for _, tc := range cases {
		items, err := parseAdapter_rscList(readRSCFixture(t, tc.fixture), "https://rsc.swjtu.edu.cn")
		if err != nil {
			t.Fatalf("parse %s: %v", tc.fixture, err)
		}
		if len(items) != tc.count || items[0].URL != tc.first || items[0].Type != "新闻News" || items[0].Date == "" {
			t.Fatalf("%s items = %#v", tc.fixture, items)
		}
		for _, item := range items {
			if strings.Contains(item.URL, "news.swjtu.edu.cn") {
				t.Fatalf("external article leaked from %s: %#v", tc.fixture, item)
			}
		}
	}
	if got := AllSites["rsc"].PageURL("https://rsc.swjtu.edu.cn", "xwtz/xwNews", 1, 3, ".htm"); got != "https://rsc.swjtu.edu.cn/xwtz/xwNews.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := AllSites["rsc"].PageURL("https://rsc.swjtu.edu.cn", "xwtz/xwNews", 2, 3, ".htm"); got != "https://rsc.swjtu.edu.cn/xwtz/xwNews/2.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := AllSites["rsc"].PageURL("https://rsc.swjtu.edu.cn", "xwtz/xwNews", 3, 3, ".htm"); got != "https://rsc.swjtu.edu.cn/xwtz/xwNews/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := AllSites["rsc"].PageURL("https://rsc.swjtu.edu.cn", "xwtz/xwNews", 4, 3, ".htm"); got != "" {
		t.Fatalf("out-of-range page URL = %q", got)
	}
}

func TestRSCEmptyStaticAndBlockedResponsesAreNotSuccessfulLists(t *testing.T) {
	items, err := parseAdapter_rscList(readRSCFixture(t, "rsc_live_empty.html"), "https://rsc.swjtu.edu.cn")
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty list = %#v, err=%v", items, err)
	}
	for _, fixture := range []string{"rsc_live_static.html", "rsc_live_captcha.html"} {
		if _, err := parseAdapter_rscList(readRSCFixture(t, fixture), "https://rsc.swjtu.edu.cn"); err == nil {
			t.Fatalf("%s was accepted as a list", fixture)
		}
	}
}

func TestRSCMeasuredArticleUsesFixedMetadataAndBodyBoundary(t *testing.T) {
	article, err := parseAdapter_rscArticle(readRSCFixture(t, "rsc_live_article.html"), "https://rsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于教职工职称评聘工作的通知" || article.Type != "通知Notice" || article.Source != "人力资源部" || article.Date != "2026-07-16" || article.PublishedAt != "2026-07-16 14:03:00" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "正文里的活动时间为2026年9月16日") || strings.Contains(article.Content, "正文边界外的导航") || len(article.Images) != 1 || len(article.Attachments) != 1 {
		t.Fatalf("article body/resources = %#v", article)
	}
	if strings.Contains(article.ContentHTML, "正文边界外的导航") || !strings.Contains(article.ContentHTML, "https://rsc.swjtu.edu.cn/__local/body.png") {
		t.Fatalf("fixed body boundary or image URL failed: %s", article.ContentHTML)
	}

	dateOnly, err := parseAdapter_rscArticle(readRSCFixture(t, "rsc_live_article_date_only.html"), "https://rsc.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if dateOnly.Date != "2026-07-16" || dateOnly.PublishedAt != "" {
		t.Fatalf("date-only metadata fabricated a clock: %#v", dateOnly)
	}
}

func TestRSCFetchArticleVerifiesStatusContentTypeMagicAndCaptcha(t *testing.T) {
	oldClient, oldNoCache, oldBase := defaultClient, NoCache, AllSites["rsc"].BaseURL
	articleURL := "https://rsc-test.invalid/info/1026/9001.htm"
	requested := make([]string, 0, 5)
	defaultClient = &http.Client{Transport: rscTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/info/1026/9001.htm":
			return rscTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readRSCFixture(t, "rsc_live_article_resources.html")), nil
		case "/good.png":
			requested = append(requested, req.URL.Path)
			if req.Header.Get("Referer") != articleURL {
				t.Errorf("image Referer = %q, want %q", req.Header.Get("Referer"), articleURL)
			}
			return rscTestResponse(req, http.StatusOK, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0}), nil
		case "/bad.png":
			requested = append(requested, req.URL.Path)
			return rscTestResponse(req, http.StatusOK, "image/png", []byte("<!doctype html><html>请输入验证码</html>")), nil
		case "/missing.png":
			requested = append(requested, req.URL.Path)
			return rscTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		case "/good.pdf":
			requested = append(requested, req.URL.Path)
			if req.Header.Get("Referer") != articleURL {
				t.Errorf("attachment Referer = %q, want %q", req.Header.Get("Referer"), articleURL)
			}
			return rscTestResponse(req, http.StatusOK, "application/pdf", []byte("%PDF-1.7\nfixture")), nil
		case "/bad.pdf":
			requested = append(requested, req.URL.Path)
			return rscTestResponse(req, http.StatusOK, "application/pdf", []byte("<html>验证码下载附件</html>")), nil
		default:
			return rscTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
	})}
	AllSites["rsc"].BaseURL = "https://rsc-test.invalid"
	NoCache = true
	ClearCache()
	t.Cleanup(func() {
		defaultClient, NoCache, AllSites["rsc"].BaseURL = oldClient, oldNoCache, oldBase
		ClearCache()
	})

	article, raw, err := rscFetchArticle(articleURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || len(article.Images) != 1 || article.Images[0] != "https://rsc-test.invalid/good.png" {
		t.Fatalf("verified article/images = raw=%d article=%#v", len(raw), article)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "有效附件.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "bad.png") || strings.Contains(article.ContentHTML, "missing.png") || strings.Contains(article.ContentHTML, "bad.pdf") || strings.Contains(article.ContentHTML, "javascript:") {
		t.Fatalf("invalid resources remained in ContentHTML: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, `src="https://rsc-test.invalid/good.png"`) || !strings.Contains(article.ContentHTML, `href="https://example.com/notice.pdf"`) {
		t.Fatalf("valid image or external link was lost: %s", article.ContentHTML)
	}
	if !reflect.DeepEqual(requested, []string{"/good.png", "/bad.png", "/missing.png", "/good.pdf", "/bad.pdf"}) {
		t.Fatalf("resource requests = %#v", requested)
	}
	if got := rscResourceURL("https://rsc-test.invalid", "https://example.com/foreign.pdf"); got != "" {
		t.Fatalf("foreign resource admitted: %q", got)
	}
}

func TestRSCResourceBodyValidationRequiresContentTypeAndMagic(t *testing.T) {
	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	validPDF := []byte("%PDF-1.7")
	cases := []struct {
		name, contentType string
		body              []byte
		image, want       bool
	}{
		{"good image", "image/png", validPNG, true, true},
		{"wrong image magic", "image/png", []byte("not png"), true, false},
		{"captcha image", "image/png", []byte("<html>请输入验证码</html>"), true, false},
		{"good pdf", "application/pdf", validPDF, false, true},
		{"wrong pdf magic", "application/pdf", []byte("PK\x03\x04"), false, false},
		{"missing type", "", validPDF, false, false},
	}
	for _, tc := range cases {
		if got := rscResourceBodyIsUsable(tc.contentType, tc.body, tc.name, tc.image); got != tc.want {
			t.Errorf("%s accepted=%v, want %v", tc.name, got, tc.want)
		}
	}
}

type rscTestRoundTripper func(*http.Request) (*http.Response, error)

func (f rscTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func rscTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}
}
