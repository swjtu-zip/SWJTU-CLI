package scraper

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

func TestIscitAdapterRegistered(t *testing.T) {
	site := AllSites["iscit"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("iscit adapter hooks are not registered")
	}
	want := []string{
		"xw/zxxw", "xw/xsdt", "dqgz/dqxw", "xsgz/txxw",
		"tz/yw", "tz/dq", "tz/jx", "tz/xg", "tz/sz", "tz/ky",
		"zt/dsxxjy", "zt/b_s_bqgzs", "zt/cptxyc",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("iscit feeds = %#v, want %#v", site.SubPages, want)
	}
	if got := iscitPageURL("https://iscit.swjtu.edu.cn", "dqgz/dqxw", 2, 13, ".htm"); got != "https://iscit.swjtu.edu.cn/dqgz/dqxw/12.htm" {
		t.Fatalf("reverse page URL = %q", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 13},
		{14, 13},
		{2, 0},
	} {
		if got := iscitPageURL("https://iscit.swjtu.edu.cn", "dqgz/dqxw", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
	if _, _, err := iscitListPage("dqgz/dqxw", 0); err == nil {
		t.Fatal("ISCit custom list accepted page zero")
	}
}

func readIscitFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read iscit fixture %s: %v", name, err)
	}
	return body
}

func TestIscitAuditedReversePageSamples(t *testing.T) {
	cases := []struct {
		slug  string
		total int
	}{
		{"xw/zxxw", 50}, {"xw/xsdt", 10}, {"dqgz/dqxw", 13}, {"xsgz/txxw", 24},
		{"tz/yw", 3}, {"tz/dq", 4}, {"tz/jx", 13}, {"tz/xg", 43},
		{"tz/sz", 1}, {"tz/ky", 1}, {"zt/dsxxjy", 4}, {"zt/b_s_bqgzs", 3}, {"zt/cptxyc", 1},
	}
	for _, tc := range cases {
		first := iscitPageURL(iscitCanonicalBaseURL, tc.slug, 1, tc.total, ".htm")
		if want := iscitCanonicalBaseURL + "/" + tc.slug + ".htm"; first != want {
			t.Errorf("%s first page = %q, want %q", tc.slug, first, want)
		}
		middle := (tc.total + 1) / 2
		if middle == 1 && tc.total > 1 {
			middle = 2
		}
		middleWant := first
		if middle > 1 {
			middleWant = iscitCanonicalBaseURL + "/" + tc.slug + "/" + strconv.Itoa(tc.total-middle+1) + ".htm"
		}
		if got := iscitPageURL(iscitCanonicalBaseURL, tc.slug, middle, tc.total, ".htm"); got != middleWant {
			t.Errorf("%s middle page %d = %q, want %q", tc.slug, middle, got, middleWant)
		}
		lastWant := iscitCanonicalBaseURL + "/" + tc.slug + "/1.htm"
		if tc.total == 1 {
			lastWant = first
		}
		if got := iscitPageURL(iscitCanonicalBaseURL, tc.slug, tc.total, tc.total, ".htm"); got != lastWant {
			t.Errorf("%s last page = %q, want %q", tc.slug, got, lastWant)
		}
		if got := iscitPageURL(iscitCanonicalBaseURL, tc.slug, tc.total+1, tc.total, ".htm"); got != "" {
			t.Errorf("%s out-of-range page = %q", tc.slug, got)
		}
	}
}

func TestIscitListRequiresPaginationEvidence(t *testing.T) {
	raw := []byte(strings.TrimSpace(`
<!doctype html>
<html><head><meta name="pageType" content="2"></head><body>
<div class="new-list"><div class="newlb-box"><ul>
  <li><a href="/info/1102/8490.htm">一条新闻</a></li>
</ul></div></div>
</body></html>`))
	if _, err := iscitTotalPagesFromHTML(raw, "dqgz/dqxw"); err == nil {
		t.Fatal("ISCit short list without pagination was treated as complete")
	}
}

func TestIscitListFiltersNonArticlesAndMergesDuplicates(t *testing.T) {
	html, err := os.ReadFile("testdata/iscit.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseIscitNews(html, "https://iscit.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].URL != "https://iscit.swjtu.edu.cn/info/1102/8490.htm" {
		t.Fatalf("first URL = %q", items[0].URL)
	}
	if items[0].Title != "2025届推免专题" || items[0].Date != "2024-10-16" {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[1].URL != "https://iscit.swjtu.edu.cn/info/1091/1131.htm" || items[1].Date != "2024-06-07" {
		t.Fatalf("second item = %+v", items[1])
	}
}

func TestIscitLiveHomepageCoversHiddenTabsAndScopedDates(t *testing.T) {
	items, err := parseIscitNews(readIscitFixture(t, "iscit_home_live.html"), iscitCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse iscit homepage: %v", err)
	}
	if len(items) != 10 {
		t.Fatalf("homepage unique article count = %d, want 10", len(items))
	}
	want := map[string]struct {
		typ  string
		date string
	}{
		"/info/1024/15001.htm": {"最新新闻", "2026-07-20"},
		"/info/1024/15002.htm": {"最新新闻", "2025-03-01"},
		"/info/1024/15003.htm": {"最新新闻", "2026-07-17"},
		"/info/1025/15004.htm": {"学术动态", "2025-02-26"},
		"/info/1025/15005.htm": {"学术动态", ""},
		"/info/1102/15006.htm": {"院务", ""},
		"/info/1102/15007.htm": {"院务", "2026-09-03"},
		"/info/1075/15008.htm": {"党群", "2026-08-01"},
		"/info/1102/15009.htm": {"教学", "2026-07-10"},
		"/info/1103/15010.htm": {"学工", "2026-06-10"},
	}
	for _, item := range items {
		path := strings.TrimPrefix(item.URL, iscitCanonicalBaseURL)
		expected, ok := want[path]
		if !ok {
			t.Fatalf("unexpected homepage item: %#v", item)
		}
		if item.Type != expected.typ || item.Date != expected.date {
			t.Errorf("homepage item %s = type/date %q/%q, want %q/%q", path, item.Type, item.Date, expected.typ, expected.date)
		}
	}
	if got := itemsForIscitURL(items, iscitCanonicalBaseURL+"/info/1025/15005.htm"); got == nil {
		t.Fatal("academic card disappeared")
	} else if got.Date != "" {
		t.Fatalf("event date polluted academic publication date: %#v", got)
	}
	if strings.Contains(fmt.Sprint(items), "external") {
		t.Fatal("external homepage article leaked")
	}
}

func itemsForIscitURL(items []model.NewsItem, target string) *model.NewsItem {
	for index := range items {
		if items[index].URL == target {
			return &items[index]
		}
	}
	return nil
}

func TestIscitLiveListsKeepMainColumnAndReportSourceTotals(t *testing.T) {
	items, err := parseIscitNews(readIscitFixture(t, "iscit_list_live.html"), iscitCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse iscit news list: %v", err)
	}
	if len(items) != 2 || items[0].Type != "最新新闻" || items[0].Date != "2026-07-17" {
		t.Fatalf("news list items = %#v", items)
	}
	for _, item := range items {
		if strings.Contains(item.URL, "/9999/") {
			t.Fatalf("sidebar article leaked: %#v", item)
		}
	}
	if total, err := iscitTotalPagesFromHTML(readIscitFixture(t, "iscit_list_live.html"), "xw/zxxw"); err != nil || total != 13 {
		t.Fatalf("news source total = %d, err=%v; want 13", total, err)
	}

	items, err = parseIscitNews(readIscitFixture(t, "iscit_list_notice_live.html"), iscitCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse iscit notice list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("notice list local items = %d, want 2: %#v", len(items), items)
	}
	for _, item := range items {
		if item.Type != "团学新闻" || item.Date != "2026-06-18" && item.Date != "2026-06-01" {
			t.Errorf("notice item = %#v", item)
		}
	}
}

func TestIscitRejectsStaticShellsWAFAndUnverifiedShortLists(t *testing.T) {
	static := readIscitFixture(t, "iscit_static_shell.html")
	if _, err := iscitTotalPagesFromHTML(static, "zt/zhcsyjtxy"); err == nil {
		t.Fatal("static topic shell was accepted as a list")
	}
	if _, err := parseIscitNews(static, iscitCanonicalBaseURL); err == nil {
		t.Fatal("static topic shell was accepted by list parser")
	}
	empty := readIscitFixture(t, "iscit_list_empty_live.html")
	if total, err := iscitTotalPagesFromHTML(empty, "tz/ky"); err != nil || total != 1 {
		t.Fatalf("audited empty feed = total %d, err=%v", total, err)
	}
	if items, err := parseIscitNews(empty, iscitCanonicalBaseURL); err != nil || items == nil || len(items) != 0 {
		t.Fatalf("audited empty feed parse = %#v, err=%v", items, err)
	}
	waf := readIscitFixture(t, "iscit_waf.html")
	if _, err := parseIscitNews(waf, iscitCanonicalBaseURL); err == nil {
		t.Fatal("WAF response was accepted as a list")
	}
	if _, err := parseIscitArticle(waf, iscitCanonicalBaseURL); err == nil {
		t.Fatal("WAF response was accepted as an article")
	}
}

func TestIscitIndexFeedIsAuditedHomepage(t *testing.T) {
	if !iscitKnownFeed("index") {
		t.Fatal("iscit index feed is not registered as audited")
	}
	if got := iscitPageURL(iscitCanonicalBaseURL, "index", 1, 0, ".htm"); got != iscitCanonicalBaseURL+"/index.htm" {
		t.Fatalf("index page URL = %q", got)
	}
	if got := iscitPageURL(iscitCanonicalBaseURL, "index", 2, 1, ".htm"); got != "" {
		t.Fatalf("index page 2 URL = %q, want empty", got)
	}

	home := readIscitFixture(t, "iscit_live_index.html")
	if total, err := iscitTotalPagesFromHTML(home, "index"); err != nil || total != 1 {
		t.Fatalf("index total = %d, err=%v; want 1", total, err)
	}
	items, err := parseIscitNews(home, iscitCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse live homepage: %v", err)
	}
	if len(items) < 10 {
		t.Fatalf("live homepage yielded %d items, want at least 10", len(items))
	}
	for _, item := range items {
		if _, ok := iscitArticleURL(iscitCanonicalBaseURL, item.URL); !ok {
			t.Fatalf("homepage item is not a local article: %#v", item)
		}
		if item.Title == "" || item.Type == "" {
			t.Fatalf("homepage item lost title/type: %#v", item)
		}
	}

	// A real list slug must keep refusing the homepage shell, and unknown
	// slugs stay unaudited (defense in depth around the index allowance).
	if _, err := iscitTotalPagesFromHTML(home, "xw/zxxw"); err == nil {
		t.Fatal("homepage shell accepted for a non-index list slug")
	}
	if iscitKnownFeed("index/nope") {
		t.Fatal("unexpected slug treated as the index feed")
	}
	if _, err := iscitTotalPages("not-a-feed"); err == nil {
		t.Fatal("unknown slug accepted by total-pages lookup")
	}
}

func TestIscitLiveZxxwKeepsLocalRowsAmongExternalOnes(t *testing.T) {
	raw := readIscitFixture(t, "iscit_live_zxxw.html")
	items, err := parseIscitNews(raw, iscitCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse live zxxw list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("live zxxw local items = %#v, want exactly the one local row", items)
	}
	if items[0].URL != iscitCanonicalBaseURL+"/info/1024/2916.htm" || items[0].Title != "诚聘海内外英才" || items[0].Date != "2025-03-01" || items[0].Type != "最新新闻" {
		t.Fatalf("live zxxw local item = %#v", items[0])
	}
	if total, err := iscitTotalPagesFromHTML(raw, "xw/zxxw"); err != nil || total != 50 {
		t.Fatalf("live zxxw source total = %d, err=%v; want 50", total, err)
	}
}

func TestIscitAllExternalRowsAreVerifiedEmptyNotAnError(t *testing.T) {
	const externalList = `<!doctype html>
<html><head><meta name="pageType" content="2"><meta name="pageTitle" content="最新新闻"><title>最新新闻</title></head>
<body>
<div class="new-list"><div class="newlb-box"><ul>
<li><div class="newlb-left"><strong>22</strong><p>2026.09</p></div>
<div class="newlb-rig"><h6><a href="https://mp.weixin.qq.com/s/kQg1HdPhSBF_5qngdrpikg" title="智慧城市与交通学院2026级学生开学典礼圆满举行">智慧城市与交通学院2026级学生开学典礼圆满举行</a></h6><p></p></div></li>
<li><div class="newlb-left"><strong>22</strong><p>2026.09</p></div>
<div class="newlb-rig"><h6><a href="http://mp-weixin-qq-com-s.vpn.swjtu.edu.cn:8118/s/gZ8VVJTVbW0lkeqzfmZZQw" title="东部（国际）校区2027届毕业生首场专场双选会成功举办">东部（国际）校区2027届毕业生首场专场双选会成功举办</a></h6><p></p></div></li>
<li><div class="newlb-left"><strong>13</strong><p>2026.09</p></div>
<div class="newlb-rig"><h6><a href="https://mp.weixin.qq.com/s/aCNyVSNFJZsLvVrvxjITwg" title="学院召开本科2026级新生家校沟通会">学院召开本科2026级新生家校沟通会</a></h6><p></p></div></li>
</ul></div></div>
<span class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="zxxw/49.htm">2</a></span></span>
</body></html>`
	items, err := parseIscitNews([]byte(externalList), iscitCanonicalBaseURL)
	if err != nil {
		t.Fatalf("all-external list must be a verified empty feed, not an error: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("all-external list items = %#v, want non-nil empty", items)
	}

	// A same-host non-article anchor means the template changed: keep the
	// hard error instead of swallowing the rows.
	const sameHostStatic = `<!doctype html>
<html><head><meta name="pageType" content="2"><meta name="pageTitle" content="最新新闻"><title>最新新闻</title></head>
<body>
<div class="new-list"><div class="newlb-box"><ul>
<li><div class="newlb-rig"><h6><a href="../xygk/xyjj.htm" title="学院简介">学院简介</a></h6></div></li>
</ul></div></div>
<span class="p_pages"><span class="p_no_d">1</span></span>
</body></html>`
	if _, err := parseIscitNews([]byte(sameHostStatic), iscitCanonicalBaseURL); err == nil {
		t.Fatal("same-host static rows were silently swallowed as an external list")
	}

	// Rows without any resolvable content anchor are a structural change too.
	const noAnchors = `<!doctype html>
<html><head><meta name="pageType" content="2"><meta name="pageTitle" content="最新新闻"><title>最新新闻</title></head>
<body>
<div class="new-list"><div class="newlb-box"><ul>
<li><div class="newlb-rig"><h6>只有标题没有链接</h6></div></li>
</ul></div></div>
<span class="p_pages"><span class="p_no_d">1</span></span>
</body></html>`
	if _, err := parseIscitNews([]byte(noAnchors), iscitCanonicalBaseURL); err == nil {
		t.Fatal("anchor-less rows were silently swallowed as an external list")
	}
}

func TestIscitArticleDate(t *testing.T) {
	html, err := os.ReadFile("testdata/iscit_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseIscitArticle(html, "https://iscit.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "【讲座预告】探寻智能出行与交通安全研究的结合点" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2025-02-26" {
		t.Fatalf("date = %q, want 2025-02-26", article.Date)
	}
}

func TestIscitLiveArticleUsesFixedMetadataAndBodyScope(t *testing.T) {
	article, err := parseIscitArticle(readIscitFixture(t, "iscit_article_live.html"), iscitCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse iscit article: %v", err)
	}
	if article.Title != "资源校验回归文章" || article.Date != "2026-09-02" || article.PublishedAt != "2026-09-02" || article.Type != "教学" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "活动时间：2026年9月6日14:00") || strings.Contains(article.Content, "阅读量") {
		t.Fatalf("article body scope = %q", article.Content)
	}
	if strings.Contains(article.ContentHTML, "newxq-boxy") || strings.Contains(article.ContentHTML, "有效附件.xlsx") || !strings.Contains(article.ContentHTML, "https://example.com/ordinary") {
		t.Fatalf("article ContentHTML scope/ordinary link = %q", article.ContentHTML)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachment candidates = %#v", article.Attachments)
	}

	timed, err := parseIscitArticle(readIscitFixture(t, "iscit_article_time.html"), iscitCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse timed iscit article: %v", err)
	}
	if timed.Date != "2026-09-02" || timed.PublishedAt != "2026-09-02 14:30:45" {
		t.Fatalf("timed publication metadata = date=%q published_at=%q", timed.Date, timed.PublishedAt)
	}
}

type iscitTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper iscitTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func iscitTestResponse(request *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}
}

func TestIscitFetchArticleVerifiesStatusContentTypeMagicAndKeepsExternalLinks(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	NoCache = true
	requests := make([]string, 0, 4)
	defaultClient = &http.Client{Transport: iscitTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.Path)
		switch request.URL.Path {
		case "/info/1024/9001.htm":
			return iscitTestResponse(request, http.StatusOK, "text/html; charset=utf-8", readIscitFixture(t, "iscit_article_live.html")), nil
		case "/bad.png":
			return iscitTestResponse(request, http.StatusOK, "image/png", []byte("<html>请输入验证码</html>")), nil
		case "/good.png":
			return iscitTestResponse(request, http.StatusOK, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}), nil
		case "/missing.png":
			return iscitTestResponse(request, http.StatusNotFound, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}), nil
		case "/bad.xlsx":
			return iscitTestResponse(request, http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("<html>验证码下载附件</html>")), nil
		case "/system/_content/download.jsp":
			return iscitTestResponse(request, http.StatusOK, "application/octet-stream", []byte{'P', 'K', 0x03, 0x04, 0x00, 0x01}), nil
		default:
			return iscitTestResponse(request, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
	})}
	t.Cleanup(func() {
		defaultClient, NoCache = oldClient, oldNoCache
		ClearCache()
	})

	article, raw, err := iscitFetchArticle(iscitCanonicalBaseURL + "/info/1024/9001.htm")
	if err != nil {
		t.Fatalf("fetch iscit article: %v", err)
	}
	if len(raw) == 0 || article.Date != "2026-09-02" || article.PublishedAt != "2026-09-02" {
		t.Fatalf("fetched archive/metadata = raw=%d article=%#v", len(raw), article)
	}
	if len(article.Images) != 1 || article.Images[0] != iscitCanonicalBaseURL+"/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "有效附件.xlsx" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, iscitCanonicalBaseURL+"/good.png") || !strings.Contains(article.ContentHTML, "https://example.com/ordinary") {
		t.Fatalf("verified body resources/external link = %q", article.ContentHTML)
	}
	for _, rejected := range []string{"bad.png", "bad.xlsx", "验证码", "wbfileid=GOOD"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource survived ContentHTML: %q in %q", rejected, article.ContentHTML)
		}
	}
	if len(requests) != 6 {
		t.Fatalf("resource verification requests = %#v, want article + 5 resources", requests)
	}
}

func TestIscitResourceURLAndMagicGuards(t *testing.T) {
	if _, ok := iscitArticleURL(iscitCanonicalBaseURL, "https://example.com/info/1/2.htm"); ok {
		t.Fatal("external article URL was accepted")
	}
	if got := iscitResourceURL(iscitCanonicalBaseURL, "https://cdn.example/image.png#fragment"); got != "https://cdn.example/image.png" {
		t.Fatalf("external resource URL = %q", got)
	}
	if got := iscitResourceURL(iscitCanonicalBaseURL, "javascript:download('/x.xlsx')"); got != "" {
		t.Fatalf("pseudo resource URL = %q", got)
	}
	if !iscitResourceBodyIsUsable("image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "good.png", true) {
		t.Fatal("valid PNG was rejected")
	}
	if iscitResourceBodyIsUsable("image/png", []byte("not an image"), "bad.png", true) {
		t.Fatal("bad image magic was accepted")
	}
	if iscitResourceBodyIsUsable("application/octet-stream", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "wrong-type.png", true) {
		t.Fatal("image with an unrelated content type was accepted")
	}
	if iscitResourceBodyIsUsable("application/octet-stream", []byte("<html>captcha</html>"), "bad.xlsx", false) {
		t.Fatal("HTML pseudo attachment was accepted")
	}
	if !iscitResourceBodyIsUsable("application/octet-stream", []byte{'P', 'K', 0x03, 0x04}, "good.xlsx", false) {
		t.Fatal("valid XLSX magic was rejected")
	}
}

func TestNormalizeIscitDateDoesNotInventYear(t *testing.T) {
	if got := normalizeIscitDate("07-08"); got != "" {
		t.Fatalf("date = %q, want empty", got)
	}
}
