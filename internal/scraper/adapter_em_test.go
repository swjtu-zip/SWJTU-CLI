package scraper

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

const emFixtureBaseURL = "https://em.swjtu.edu.cn"

func TestEmAdapterRegisteredWithAllArticleFeeds(t *testing.T) {
	site := AllSites["em"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("em adapter hooks are not registered")
	}
	if site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("em adapter pagination hooks are not registered")
	}

	want := []string{
		"xwdt",
		"dqgz",
		"dqgz/djgz",
		"dqgz/jwgz",
		"dqgz/ghgz",
		"dqgz/ltgz",
		"rcpy",
		"rcpy/xwdt",
		"rcpy/zszl",
		"rcpy/tzgg",
		"rcpy/glgd",
		"rcpy/wdxz",
		"rcpy/zyjj",
		"jypx3/pxfw",
		"jypx3/pxdt",
		"tzgg",
		"hqbz",
		"hqbz/hqfw",
		"hqbz/hqfw/bmfc",
		"hqbz/hqfw/tzgg",
		"hqbz/bwks",
		"gzdt",
		"xqfg",
		"xyyd",
		"xyyd/xyhd",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("em feeds = %#v, want %#v", site.SubPages, want)
	}
}

func TestEmPageURLUsesReverseGeneratedNumbers(t *testing.T) {
	tests := []struct {
		name       string
		slug       string
		page       int
		totalPages int
		extension  string
		want       string
	}{
		{"first", "xwdt", 1, 71, ".htm", emFixtureBaseURL + "/xwdt.htm"},
		{"second", "xwdt", 2, 71, ".htm", emFixtureBaseURL + "/xwdt/70.htm"},
		{"middle", "xwdt", 36, 71, ".htm", emFixtureBaseURL + "/xwdt/36.htm"},
		{"last", "xwdt", 71, 71, ".htm", emFixtureBaseURL + "/xwdt/1.htm"},
		{"nested", "rcpy/tzgg", 7, 7, ".html", emFixtureBaseURL + "/rcpy/tzgg/1.html"},
		{"unknown total cannot guess", "xwdt", 2, 0, ".htm", ""},
		{"past last page cannot clamp", "xwdt", 72, 71, ".htm", ""},
		{"past one-page feed cannot repeat first", "rcpy/zyjj", 2, 1, ".htm", ""},
		{"non-positive page cannot repeat first", "xwdt", 0, 71, ".htm", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := emPageURL(emFixtureBaseURL, tt.slug, tt.page, tt.totalPages, tt.extension); got != tt.want {
				t.Fatalf("emPageURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmPageCountReadsDisplayedLogicalNumbers(t *testing.T) {
	html := []byte(`<div class="p_t">共421条</div><div class="p_pages">
<span class="p_no_d">1</span><span class="p_no"><a href="xwdt/70.htm">2</a></span>
<span class="p_no"><a href="xwdt/1.htm">71</a></span><span class="p_last"><a href="xwdt/1.htm">尾页</a></span>
</div>`)
	if got := emPageCountFromHTML(html); got != 71 {
		t.Fatalf("emPageCountFromHTML() = %d, want 71", got)
	}

	empty := []byte(`<div class="p_t">共0条</div><div class="p_pages"><span class="p_no_d">1</span></div>`)
	if got := emPageCountFromHTML(empty); got != 1 {
		t.Fatalf("empty em page count = %d, want 1", got)
	}
}

func TestEmLiveHomeParsesEveryModuleAndExcludesExternalLinks(t *testing.T) {
	items, err := parseAdapter_emList(mustEmFixture(t, "em_live_home.html"), emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	seen := make(map[string]bool)
	for _, item := range items {
		counts[item.Type]++
		if seen[item.URL] {
			t.Errorf("duplicate live home article URL: %q", item.URL)
		}
		seen[item.URL] = true
		if strings.Contains(item.URL, "bksy.swjtu.edu.cn") || strings.Contains(item.URL, "mp.weixin.qq.com") {
			t.Errorf("external live home link leaked into feed: %#v", item)
		}
	}

	wantCounts := map[string]int{
		"校区要闻": 4,
		"校区公告": 10,
		"党政办公": 10,
		"人才培养": 9,
		"教育培训": 10,
		"后勤保障": 10,
		"工作动态": 4,
		"校区风光": 5,
	}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("live home type counts = %#v, want %#v (items=%d)", counts, wantCounts, len(items))
	}
	if len(items) != 62 {
		t.Fatalf("live home items = %d, want 62", len(items))
	}

	var found bool
	for _, item := range items {
		if item.URL == emFixtureBaseURL+"/info/1023/21306.htm" {
			found = item.Type == "校区要闻" && item.Date == "2026-09-30" && item.Title == "峨眉校区召开2026年第六次安全生产工作会议"
		}
	}
	if !found {
		t.Fatal("live home 校区要闻 spot check missing")
	}
}

func TestEmIndexFeedCountsAsSingleHomepage(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	defaultClient = &http.Client{Transport: emTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/index.htm" {
			return emTestResponse(req, http.StatusOK, "text/html; charset=utf-8", mustEmFixture(t, "em_live_home.html")), nil
		}
		return emTestResponse(req, http.StatusNotFound, "text/html", []byte("not found")), nil
	})}
	NoCache = true
	t.Cleanup(func() { defaultClient, NoCache = oldClient, oldNoCache; ClearCache() })

	total, err := emTotalPages("index")
	if err != nil || total != 1 {
		t.Fatalf("em index total = %d, err=%v; want a single homepage page", total, err)
	}
}

func TestEmIndexFeedRejectsHomeShapedShells(t *testing.T) {
	shell := []byte(`<html><body><div class="newsBox"></div><div id="tabs-container1"><div class="swiper-slide"><ul><li><a href="/xwdt.htm">新闻动态</a></li></ul></div></div></body></html>`)
	oldClient, oldNoCache := defaultClient, NoCache
	defaultClient = &http.Client{Transport: emTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/index.htm" {
			return emTestResponse(req, http.StatusOK, "text/html; charset=utf-8", shell), nil
		}
		return emTestResponse(req, http.StatusNotFound, "text/html", []byte("not found")), nil
	})}
	NoCache = true
	t.Cleanup(func() { defaultClient, NoCache = oldClient, oldNoCache; ClearCache() })

	if total, err := emTotalPages("index"); err == nil || total != 0 {
		t.Fatalf("em index total = %d, err=%v; want refusal for a home-shaped shell", total, err)
	}
}

func TestEmHomeParsesEveryModuleAndExcludesExternalLinks(t *testing.T) {
	html, err := os.ReadFile("testdata/em.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_emList(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	seen := make(map[string]bool)
	for _, item := range items {
		counts[item.Type]++
		if seen[item.URL] {
			t.Errorf("duplicate home article URL: %q", item.URL)
		}
		seen[item.URL] = true
		if strings.Contains(item.URL, "mp.weixin.qq.com") || strings.Contains(item.URL, "jwc.swjtu.edu.cn") {
			t.Errorf("external home link leaked into feed: %#v", item)
		}
		if item.Date == "" && item.Type != "校区风光" {
			t.Errorf("home item has no date: %#v", item)
		}
	}

	wantCounts := map[string]int{
		"校区要闻": 4,
		"校区公告": 10,
		"党政办公": 10,
		"人才培养": 9,
		"教育培训": 10,
		"后勤保障": 10,
		"工作动态": 4,
		"校区风光": 5,
	}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("home type counts = %#v, want %#v (items=%d)", counts, wantCounts, len(items))
	}
	if len(items) != 62 {
		t.Fatalf("home items = %d, want 62", len(items))
	}
}

func TestEmListParsesDatesTypesAndLegacyContentURLs(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="管理规定"></head><body>
<section class="TextList bgss"><div><ul>
<li><a href="../zzz_content.jsp?urltype=news.NewsContentUrl&amp;wbtreeid=1850&amp;wbnewsid=18316" title="西南交通大学本科生考试违规处理办法"><div class="data"><i>03-09</i><b>2026</b></div><p class="line1">西南交通大学本科生考试违规处理办法</p></a></li>
<li><a href="info/1025/20756.htm" title="社会用工拟聘人员公示"><div class="data"><i>08-31</i><b>2026</b></div><p class="line1">社会用工拟聘人员公示</p></a></li>
<li><a href="https://example.com/info/1/2.htm" title="不属于峨眉校区的文章"><div class="data"><i>08-30</i><b>2026</b></div><p class="line1">不属于峨眉校区的文章</p></a></li>
</ul><span class="p_t">共2条</span><span class="p_pages"><span class="p_no_d">1</span></span></div></section>
</body></html>`)

	items, err := parseAdapter_emList(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d em list items, want 2: %#v", len(items), items)
	}
	if items[0].URL != emFixtureBaseURL+"/zzz_content.jsp?urltype=news.NewsContentUrl&wbtreeid=1850&wbnewsid=18316" ||
		items[0].Date != "2026-03-09" || items[0].Type != "管理规定" {
		t.Errorf("unexpected legacy list item: %#v", items[0])
	}
	if items[1].URL != emFixtureBaseURL+"/info/1025/20756.htm" || items[1].Date != "2026-08-31" || items[1].Type != "管理规定" {
		t.Errorf("unexpected ordinary list item: %#v", items[1])
	}
}

func TestEmNestedListUsesLastBreadcrumbAsType(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="人才培养"></head><body>
<div class="mianbao"><a href="/">首页</a><a href="/rcpy.htm">人才培养</a><a href="/rcpy/tzgg.htm">通知公告</a></div>
<section class="TextList bgss"><div><ul>
<li><a href="/info/1230/20756.htm" title="关于开展考试工作的通知"><div class="data"><i>31</i><b>2026.08</b></div></a></li>
</ul><span class="p_t">共1条</span><span class="p_pages"><span class="p_no_d">1</span></span></div></section>
</body></html>`)

	items, err := parseAdapter_emList(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d em list items, want 1: %#v", len(items), items)
	}
	if items[0].Type != "通知公告" {
		t.Fatalf("nested em list type = %q, want 通知公告", items[0].Type)
	}
}

func TestEmTrainingServiceListParsesItsSixItemFeedShape(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="教育培训"></head><body>
<div class="mianbao"><a href="/">首页</a><a href="/jypx3.htm">教育培训</a><a href="/jypx3/pxfw.htm">培训服务</a></div>
<section class="TextList bgss"><div><ul>
<li><a href="/info/1770/16116.htm" title="培训保障"><div class="data"><i>17</i><b>2025.07</b></div></a></li>
<li><a href="/info/1770/16106.htm" title="就餐配餐"><div class="data"><i>17</i><b>2025.07</b></div></a></li>
</ul><span class="p_t">共6条</span><span class="p_pages"><span class="p_no_d">1</span></span></div></section>
</body></html>`)

	items, err := parseAdapter_emList(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d training-service items, want 2: %#v", len(items), items)
	}
	for _, item := range items {
		if item.Type != "培训服务" || item.Date != "2025-07-17" {
			t.Errorf("training-service item metadata = (type=%q,date=%q), want (培训服务,2025-07-17): %#v", item.Type, item.Date, item)
		}
	}
}

func TestEmEmptyListIsLegalButStaticPageIsNotAFeed(t *testing.T) {
	empty := []byte(`<html><head><meta name="pageTitle" content="其他"></head><body>
<section class="TextList bgss"><div><ul></ul><span class="p_t">共0条</span><span class="p_pages"><span class="p_no_d">1</span></span></div></section>
</body></html>`)
	items, err := parseAdapter_emList(empty, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("empty em list returned %d items", len(items))
	}

	static := []byte(`<html><body><section id="nav_slide"><div class="n_title"><h1>教育培训</h1></div></section><div class="show01"><h5>教育培训</h5></div></body></html>`)
	if _, err := parseAdapter_emList(static, emFixtureBaseURL); err == nil {
		t.Fatal("static em page was accepted as a list feed")
	}
}

func TestEmArticleParsesHeadlineMetadataBodyImagesAndAttachments(t *testing.T) {
	html, err := os.ReadFile("testdata/em_article.html")
	if err != nil {
		t.Fatal(err)
	}

	legacy, err := parseBodaArticle(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Title != "新闻动态" {
		t.Fatalf("fixture no longer reproduces the generic title: %q", legacy.Title)
	}

	article, err := parseAdapter_emArticle(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	wantTitle := "校区纪委组织开展廉洁主题系列教育活动 ——陈诗伟讲党风廉政教育党课"
	if article.Title != wantTitle || article.Date != "2026-07-17" || article.PublishedAt != "2026-07-17 09:27:00" {
		t.Fatalf("unexpected article publication fields: title=%q date=%q published_at=%q", article.Title, article.Date, article.PublishedAt)
	}
	if article.Type != "新闻动态" {
		t.Fatalf("article type = %q, want 新闻动态", article.Type)
	}
	if article.Content != "为深入推进廉洁文化建设，校区纪委组织开展廉洁主题系列教育活动。" {
		t.Fatalf("article content = %q", article.Content)
	}
	wantImage := emFixtureBaseURL + "/__local/body.jpg"
	if !reflect.DeepEqual(article.Images, []string{wantImage}) {
		t.Fatalf("article images = %#v, want %#v", article.Images, []string{wantImage})
	}
	wantAttachments := []model.Attachment{
		{Name: "附件1：廉洁教育材料.pdf", URL: emFixtureBaseURL + "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1455160693&wbfileid=ABC123"},
		{Name: "附件2：活动照片.zip", URL: emFixtureBaseURL + "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1455160693&wbfileid=DEF456"},
	}
	if !reflect.DeepEqual(article.Attachments, wantAttachments) {
		t.Fatalf("article attachments = %#v, want %#v", article.Attachments, wantAttachments)
	}
	if article.PrevURL != "" || article.NextURL != "" {
		t.Fatalf("bare sibling links must not be resolved against the site root: prev=%q next=%q", article.PrevURL, article.NextURL)
	}
}

func TestEmArticleRejectsStaticAndVerificationPages(t *testing.T) {
	static := []byte(`<html><head><meta name="pageType" content="2"></head><body><div class="show01"><h5>教育培训</h5></div><div class="v_news_content"><p>静态内容</p></div></body></html>`)
	if _, err := parseAdapter_emArticle(static, emFixtureBaseURL); err == nil {
		t.Fatal("static em page was accepted as an article")
	}
	verification := []byte(`<html><head><title>验证码验证</title></head><body><div id="captcha">请完成验证码</div></body></html>`)
	if _, err := parseAdapter_emArticle(verification, emFixtureBaseURL); err == nil {
		t.Fatal("verification page was accepted as an article")
	}
}

func TestEmAuditedRoutesHaveTheObservedPageBoundaries(t *testing.T) {
	wantPages := map[string]int{
		"xwdt": 71, "dqgz": 13, "dqgz/djgz": 11, "dqgz/jwgz": 1,
		"dqgz/ghgz": 2, "dqgz/ltgz": 1, "rcpy": 9, "rcpy/xwdt": 3,
		"rcpy/zszl": 1, "rcpy/tzgg": 7, "rcpy/glgd": 1, "rcpy/wdxz": 1,
		"rcpy/zyjj": 1, "jypx3/pxfw": 2, "jypx3/pxdt": 2, "tzgg": 56,
		"hqbz": 1, "hqbz/hqfw": 1, "hqbz/hqfw/bmfc": 1,
		"hqbz/hqfw/tzgg": 1, "hqbz/bwks": 1, "gzdt": 4, "xqfg": 1,
		"xyyd": 1, "xyyd/xyhd": 1,
	}
	if len(emSubPages) != len(wantPages) {
		t.Fatalf("em route count = %d, want %d", len(emSubPages), len(wantPages))
	}
	for _, route := range emSubPages {
		if wantPages[route] == 0 {
			t.Errorf("route %q has no audited page boundary", route)
		}
	}
	for _, excluded := range []string{"qt", "jypx3/tzgg", "hqbz/bwks/gzzd", "hqbz/bwks/tzgg", "xyyd/xyjy"} {
		for _, route := range emSubPages {
			if route == excluded {
				t.Errorf("confirmed empty route %q was registered", excluded)
			}
		}
	}
}

func TestEmDateOnlyDetailNeverUsesBodyEventDate(t *testing.T) {
	html, err := os.ReadFile("testdata/em_article_date_only.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_emArticle(html, emFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-08-02" || article.PublishedAt != "2026-08-02" {
		t.Fatalf("date-only metadata = date=%q published=%q", article.Date, article.PublishedAt)
	}
	if strings.Contains(article.Date, ":") || strings.Contains(article.PublishedAt, ":") {
		t.Fatalf("date-only publication was given a clock: %#v", article)
	}
}

type emTestRoundTripper func(*http.Request) (*http.Response, error)

func (f emTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func emTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func TestEmFetchArticleVerifiesAndSanitizesResources(t *testing.T) {
	site := AllSites["em"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	t.Cleanup(func() {
		site.BaseURL = oldBase
		defaultClient = oldClient
		NoCache = oldNoCache
		ClearCache()
	})

	serverBase := "https://em.test"
	site.BaseURL = serverBase
	NoCache = true
	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	validPDF := []byte("%PDF-1.7\nvalid")
	captcha := []byte("<!doctype html><html><body>请输入验证码下载附件</body></html>")
	defaultClient = &http.Client{Transport: emTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/info/1025/9001.htm":
			return emTestResponse(req, http.StatusOK, "text/html; charset=utf-8", mustEmFixture(t, "em_article_resources.html")), nil
		case "/img/good.png":
			return emTestResponse(req, http.StatusOK, "image/png", validPNG), nil
		case "/img/bad.png":
			return emTestResponse(req, http.StatusOK, "image/png", captcha), nil
		case "/img/wrong.png":
			return emTestResponse(req, http.StatusOK, "image/png", []byte("not a png")), nil
		case "/files/good.pdf":
			return emTestResponse(req, http.StatusOK, "application/pdf", validPDF), nil
		case "/files/wrong.pdf":
			return emTestResponse(req, http.StatusOK, "application/pdf", validPNG), nil
		case "/files/captcha.pdf":
			return emTestResponse(req, http.StatusOK, "text/html; charset=utf-8", captcha), nil
		case "/files/missing.pdf":
			return emTestResponse(req, http.StatusNotFound, "text/html", []byte("not found")), nil
		case "/files/too-large.pdf":
			body := make([]byte, emMaxResourceBytes+1)
			copy(body, validPDF)
			return emTestResponse(req, http.StatusOK, "application/pdf", body), nil
		case "/system/_content/download.jsp":
			if req.URL.Query().Get("wbfileid") == "GOOD" {
				return emTestResponse(req, http.StatusOK, "application/octet-stream", validPDF), nil
			}
			return emTestResponse(req, http.StatusOK, "text/html; charset=utf-8", captcha), nil
		default:
			return emTestResponse(req, http.StatusNotFound, "text/html", []byte("not found")), nil
		}
	})}

	article, body, err := emFetchArticle(serverBase + "/info/1025/9001.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 || article.Title != "资源校验详情标题" || article.Type != "通知公告" {
		t.Fatalf("article fetch lost fixed metadata: article=%#v body=%d", article, len(body))
	}
	if article.Date != "2026-08-01" || article.PublishedAt != "2026-08-01" {
		t.Fatalf("body event date replaced source date: %#v", article)
	}
	if len(article.Images) != 1 || article.Images[0] != serverBase+"/img/good.png" {
		t.Fatalf("resource image filtering = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("resource attachment filtering = %#v", article.Attachments)
	}
	for _, attachment := range article.Attachments {
		if attachment.URL != serverBase+"/files/good.pdf" && !strings.Contains(attachment.URL, "wbfileid=GOOD") {
			t.Fatalf("invalid attachment survived: %#v", article.Attachments)
		}
		if attachment.Size == "" {
			t.Fatalf("verified attachment has no size: %#v", attachment)
		}
	}
	for _, invalid := range []string{"bad.png", "wrong.png", "wrong.pdf", "captcha.pdf", "missing.pdf", "too-large.pdf", "wbfileid=BAD"} {
		if strings.Contains(article.ContentHTML, invalid) {
			t.Fatalf("invalid resource survived ContentHTML (%s): %q", invalid, article.ContentHTML)
		}
	}
	if strings.Contains(article.ContentHTML, "javascript:") {
		t.Fatalf("pseudo resource survived ContentHTML: %q", article.ContentHTML)
	}
}

func TestEmResourceBodyRequiresContentTypeAndMagic(t *testing.T) {
	if emResourceBodyIsUsable("", []byte("%PDF-1.7"), "a.pdf", false) {
		t.Fatal("missing Content-Type was accepted")
	}
	if emResourceBodyIsUsable("application/pdf", []byte("plain text"), "a.pdf", false) {
		t.Fatal("wrong PDF magic was accepted")
	}
	if emResourceBodyIsUsable("image/png", []byte("<html>验证码</html>"), "a.png", true) {
		t.Fatal("CAPTCHA HTML was accepted as an image")
	}
}

func mustEmFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
