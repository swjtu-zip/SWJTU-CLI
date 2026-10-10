package scraper

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

type rvlTestRoundTripper func(*http.Request) (*http.Response, error)

func (f rvlTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func rvlTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func rvlFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

func rvlHasFeed(slug string) bool {
	for _, known := range rvlSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func TestRvlAdapterRegistrationAndAuditedFeeds(t *testing.T) {
	site := AllSites["rvl"]
	if site == nil {
		t.Fatal("rvl site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("rvl adapter did not install all strict parsing hooks")
	}
	want := []string{
		"xwdt", "tzgg", "xshd", "djgz/dwgzgg", "djgz/xwdt", "djgz/jwgz",
		"kxyj/kygg", "kxyj/kyxm/gjjxm", "kxyj/kylw", "kxyj/kyzl", "kxyj/ryjl",
		"yjsjy/yjspygg", "yjsjy/zlxz", "xsgz/tzgg", "xsgz/gszl", "xsgz/xszx",
		"xsgz/xgwjxz", "xsgz/xsjy", "rsgz/tzgg", "rsgz/g_s", "sysaq", "cpyc",
	}
	if len(site.SubPages) != len(want) {
		t.Fatalf("rvl has %d feeds, want %d: %v", len(site.SubPages), len(want), site.SubPages)
	}
	for i, slug := range want {
		if site.SubPages[i] != slug {
			t.Errorf("feed %d = %q, want %q", i, site.SubPages[i], slug)
		}
	}
	for _, excluded := range []string{
		"sysgk/sysjj", "szdw/jsml", "yjsjy/yjsds", "yjsjy/kt_zq_dbhb",
		"kxyj/zzkt", "kxyj/kfkt", "rsgz/zlxz", "kxyj/kyxm/sbjxm", "jlhz/jlhzgs",
	} {
		if rvlHasFeed(excluded) {
			t.Errorf("static, empty, or unverified page %q must not be registered", excluded)
		}
	}
}

func TestRvlAdapterParsesLiveRowsAndReversePagination(t *testing.T) {
	items, err := parseAdapter_rvlList(rvlFixture(t, "rvl_list_live.html"), "http://rvl.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("parsed %d local rows, want 2", len(items))
	}
	if items[0].Title != "车辆工程与测控研究所2024级硕士研究生党支部组织开展“轨辙所向，青春以行”校级示范性学生党支部特色活动" {
		t.Errorf("full title was not taken from title attribute: %q", items[0].Title)
	}
	if items[0].Date != "2026-06-25" || items[0].PublishedAt != "2026-06-25" || items[0].Type != "新闻动态" {
		t.Errorf("first row metadata = %#v", items[0])
	}
	if items[1].Date != "2026-06-24" || items[1].PublishedAt != "2026-06-24 09:08:07" {
		t.Errorf("timestamp precision was not retained: %#v", items[1])
	}
	for _, item := range items {
		if strings.Contains(item.URL, "news.swjtu.edu.cn") {
			t.Fatal("external article escaped the local feed")
		}
	}
	if got := rvlTotalPagesFromHTML(rvlFixture(t, "rvl_list_live.html")); got != 16 {
		t.Fatalf("total pages = %d, want 16", got)
	}
	base := "http://rvl.swjtu.edu.cn"
	checks := map[string]string{
		"first":  base + "/djgz/xwdt.htm",
		"second": base + "/djgz/xwdt/15.htm",
		"last":   base + "/djgz/xwdt/1.htm",
	}
	if got := rvlPageURL(base, "djgz/xwdt", 1, 16, ".htm"); got != checks["first"] {
		t.Errorf("first page URL = %q", got)
	}
	if got := rvlPageURL(base, "djgz/xwdt", 2, 16, ".htm"); got != checks["second"] {
		t.Errorf("second page URL = %q", got)
	}
	if got := rvlPageURL(base, "djgz/xwdt", 16, 16, ".htm"); got != checks["last"] {
		t.Errorf("last page URL = %q", got)
	}
	if got := rvlPageURL(base, "djgz/xwdt", 17, 16, ".htm"); got != "" {
		t.Errorf("out-of-range page URL = %q", got)
	}
	if got := rvlPageURL(base, "djgz/xwdt", 2, 16, "html"); got != base+"/djgz/xwdt/15.html" {
		t.Errorf("extension normalization URL = %q", got)
	}

	// Counts are the page-foot totals observed during the 2026-09-02/03
	// audit. The matrix keeps every registered feed covered by the same
	// first/middle/last/out-of-range reverse-page contract.
	feedTotals := map[string]int{
		"xwdt": 447, "tzgg": 333, "xshd": 202,
		"djgz/dwgzgg": 177, "djgz/xwdt": 152, "djgz/jwgz": 21,
		"kxyj/kygg": 33, "kxyj/kyxm/gjjxm": 5, "kxyj/kylw": 4, "kxyj/kyzl": 4, "kxyj/ryjl": 3,
		"yjsjy/yjspygg": 606, "yjsjy/zlxz": 18,
		"xsgz/tzgg": 386, "xsgz/gszl": 8, "xsgz/xszx": 398, "xsgz/xgwjxz": 20, "xsgz/xsjy": 14,
		"rsgz/tzgg": 24, "rsgz/g_s": 24, "sysaq": 6, "cpyc": 2,
	}
	if len(feedTotals) != len(rvlSubPages) {
		t.Fatalf("pagination matrix has %d feeds, registry has %d", len(feedTotals), len(rvlSubPages))
	}
	for _, slug := range rvlSubPages {
		total := feedTotals[slug]
		if total == 0 {
			t.Fatalf("missing audited page count for %q", slug)
		}
		first := rvlPageURL(base, slug, 1, total, ".htm")
		if want := base + "/" + slug + ".htm"; first != want {
			t.Errorf("%s first page = %q, want %q", slug, first, want)
		}
		if total > 1 {
			last := rvlPageURL(base, slug, total, total, ".htm")
			if want := base + "/" + slug + "/1.htm"; last != want {
				t.Errorf("%s last page = %q, want %q", slug, last, want)
			}
			middle := (total + 2) / 2
			got := rvlPageURL(base, slug, middle, total, ".htm")
			want := base + "/" + slug + "/" + strconv.Itoa(total-middle+1) + ".htm"
			if got != want {
				t.Errorf("%s middle page %d = %q, want %q", slug, middle, got, want)
			}
		}
		if got := rvlPageURL(base, slug, 0, total, ".htm"); got != "" {
			t.Errorf("%s page zero = %q", slug, got)
		}
		if got := rvlPageURL(base, slug, total+1, total, ".htm"); got != "" {
			t.Errorf("%s out-of-range page = %q", slug, got)
		}
	}
}

func TestRvlAdapterParsesEveryHomepageModule(t *testing.T) {
	items, err := parseAdapter_rvlList(rvlFixture(t, "rvl_home_live.html"), "http://rvl.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse homepage: %v", err)
	}
	// 2026-10-09 capture: five rows in each of the seven notice tabs plus
	// three academic-event rows.
	if len(items) != 38 {
		t.Fatalf("parsed %d homepage items, want 38 local rows", len(items))
	}
	byURL := make(map[string]model.NewsItem, len(items))
	for _, item := range items {
		byURL[item.URL] = item
	}
	if byURL["http://rvl.swjtu.edu.cn/info/1071/20958.htm"].Type != "行政公告" || byURL["http://rvl.swjtu.edu.cn/info/1071/20958.htm"].Date != "2026-10-02" {
		t.Fatalf("administrative tab row = %#v", byURL["http://rvl.swjtu.edu.cn/info/1071/20958.htm"])
	}
	if byURL["http://rvl.swjtu.edu.cn/info/1046/20998.htm"].Type != "研究生教育" {
		t.Fatalf("hidden graduate tab type = %#v", byURL["http://rvl.swjtu.edu.cn/info/1046/20998.htm"])
	}
	event := byURL["http://rvl.swjtu.edu.cn/info/1066/20568.htm"]
	if event.Type != "学术活动" || event.Date != "" || event.PublishedAt != "" {
		t.Fatalf("event row metadata = %#v; event schedule must not become publication date", event)
	}
	news := byURL["http://rvl.swjtu.edu.cn/info/1073/20448.htm"]
	if news.Type != "新闻动态" || news.Date != "07-08" {
		t.Fatalf("news carousel row = %#v; partial homepage dates are expected", news)
	}
	for _, item := range items {
		if strings.Contains(item.URL, "news.swjtu.edu.cn") || strings.Contains(item.URL, "9999") {
			t.Errorf("out-of-scope homepage link accepted: %#v", item)
		}
	}
}

func TestRvlAcademicListIgnoresReportAndEventDates(t *testing.T) {
	items, err := parseAdapter_rvlList(rvlFixture(t, "rvl_list_academic_live.html"), "http://rvl.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse academic list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("parsed %d academic rows, want 2", len(items))
	}
	for _, item := range items {
		if item.Date != "" || item.PublishedAt != "" {
			t.Errorf("academic schedule leaked into publication metadata: %#v", item)
		}
	}
}

func TestRvlAdapterRejectsShellsAndAcceptsExplicitEmptyList(t *testing.T) {
	items, err := parseAdapter_rvlList(rvlFixture(t, "rvl_empty_list.html"), "http://rvl.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("explicit empty list should be valid: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list returned %d rows", len(items))
	}
	for _, name := range []string{"rvl_waf.html", "rvl_spa.html"} {
		if _, err := parseAdapter_rvlList(rvlFixture(t, name), "http://rvl.swjtu.edu.cn"); err == nil {
			t.Errorf("%s was accepted as a list", name)
		}
		if _, err := parseAdapter_rvlArticle(rvlFixture(t, name), "http://rvl.swjtu.edu.cn"); err == nil {
			t.Errorf("%s was accepted as an article", name)
		}
	}
	if _, err := parseAdapter_rvlList([]byte(`<meta name="pageType" content="3"><form name="_newscontent_fromname"></form>`), "http://rvl.swjtu.edu.cn"); err == nil {
		t.Fatal("detail response was accepted as a list")
	}
}

func TestRvlArticleUsesFixedMetadataAndContentRoot(t *testing.T) {
	article, err := parseAdapter_rvlArticle(rvlFixture(t, "rvl_article_live.html"), "http://rvl.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse article: %v", err)
	}
	if article.Title != "翟婉明院士主持获得我国轨道交通领域首个国家自然科学奖" || article.Type != "新闻动态" {
		t.Fatalf("article title/type = %q/%q", article.Title, article.Type)
	}
	if article.Source != "实验室" || article.Date != "2026-07-08" || article.PublishedAt != "2026-07-08 17:10:07" {
		t.Fatalf("fixed source metadata = %#v", article)
	}
	if strings.Contains(article.Date, "2026-09-07") || strings.Contains(article.PublishedAt, "2026-09-07") {
		t.Fatal("body event date polluted publication date")
	}
	if !strings.Contains(article.Content, "活动时间：2026年9月7日") || strings.Contains(article.Content, "页脚伪文章链接") {
		t.Fatalf("content root was not limited to article body: %q", article.Content)
	}
	if !strings.Contains(article.ContentHTML, "/__local/rvl/good.png") || !strings.Contains(article.ContentHTML, "https://example.com/ordinary") {
		t.Fatalf("article ContentHTML lost body/image/external link: %q", article.ContentHTML)
	}
	if len(article.Images) != 1 || article.Images[0] != "http://rvl.swjtu.edu.cn/__local/rvl/good.png" {
		t.Fatalf("candidate images = %#v", article.Images)
	}
}

func TestRvlArticleDateOnlyDoesNotInventClock(t *testing.T) {
	article, err := parseAdapter_rvlArticle(rvlFixture(t, "rvl_article_date_only.html"), "http://rvl.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse date-only article: %v", err)
	}
	if article.Date != "2026-01-02" || article.PublishedAt != "2026-01-02" {
		t.Fatalf("date-only metadata = %q/%q", article.Date, article.PublishedAt)
	}
}

func TestRvlFetchArticleVerifiesResourcesAndKeepsExternalLinks(t *testing.T) {
	oldClient, oldNoCache, oldBase := defaultClient, NoCache, AllSites["rvl"].BaseURL
	t.Cleanup(func() {
		defaultClient, NoCache, AllSites["rvl"].BaseURL = oldClient, oldNoCache, oldBase
		ClearCache()
	})
	ClearCache()
	NoCache = true
	const baseURL = "http://rvl-test.local"
	var badReferer []string
	defaultClient = &http.Client{Transport: rvlTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		articleURL := baseURL + "/info/1046/20468.htm"
		if r.URL.Path == "/info/1046/20468.htm" {
			return rvlTestResponse(r, http.StatusOK, "text/html; charset=utf-8", rvlFixture(t, "rvl_article_resources.html")), nil
		}
		if r.Header.Get("Referer") != articleURL {
			badReferer = append(badReferer, r.URL.Path)
		}
		switch r.URL.Path {
		case "/images/good.png":
			return rvlTestResponse(r, http.StatusOK, "image/png", []byte("\x89PNG\r\n\x1a\nvalid")), nil
		case "/images/bad.png":
			return rvlTestResponse(r, http.StatusOK, "image/png", []byte("<html>请输入验证码</html>")), nil
		case "/files/good.pdf":
			return rvlTestResponse(r, http.StatusOK, "application/pdf", []byte("%PDF-1.7\nfixture")), nil
		case "/files/bad.pdf":
			return rvlTestResponse(r, http.StatusOK, "application/pdf", []byte("not a PDF")), nil
		case "/files/missing.pdf":
			return rvlTestResponse(r, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		case "/system/_content/download.jsp":
			return rvlTestResponse(r, http.StatusOK, "text/html; charset=utf-8", []byte("<!doctype html><html>请输入验证码下载附件</html>")), nil
		default:
			return rvlTestResponse(r, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
	})}
	AllSites["rvl"].BaseURL = baseURL
	article, raw, err := rvlFetchArticle(baseURL + "/info/1046/20468.htm")
	if err != nil {
		t.Fatalf("fetch article: %v", err)
	}
	if len(raw) == 0 || article.Date != "2026-07-13" || article.PublishedAt != "2026-07-13 11:47:30" {
		t.Fatalf("archive/metadata = raw=%d article=%#v", len(raw), article)
	}
	if len(article.Images) != 1 || !strings.HasSuffix(article.Images[0], "/images/good.png") {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件1 有效材料.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, "/images/good.png") || !strings.Contains(article.ContentHTML, "/files/good.pdf") || !strings.Contains(article.ContentHTML, "https://example.com/external.pdf") {
		t.Fatalf("valid resources/external link missing from ContentHTML: %q", article.ContentHTML)
	}
	for _, invalid := range []string{"bad.png", "bad.pdf", "missing.pdf", "CAPTCHA", "javascript:"} {
		if strings.Contains(article.ContentHTML, invalid) {
			t.Errorf("invalid resource %q survived sanitization: %q", invalid, article.ContentHTML)
		}
	}
	if len(badReferer) != 0 {
		t.Errorf("resource requests without detail Referer: %v", badReferer)
	}
}

func TestRvlResourceValidationRequiresTypeAndMagic(t *testing.T) {
	tests := []struct {
		name, contentType string
		body              []byte
		image, want       bool
	}{
		{"png", "image/png", []byte("\x89PNG\r\n\x1a\n"), true, true},
		{"html-as-png", "image/png", []byte("<html>请输入验证码</html>"), true, false},
		{"pdf", "application/pdf", []byte("%PDF-1.7"), false, true},
		{"pdf-wrong-magic", "application/pdf", []byte("PK\x03\x04"), false, false},
		{"missing-type", "", []byte("%PDF-1.7"), false, false},
		{"captcha-html", "text/html", []byte("<!doctype html><html>captcha</html>"), false, false},
	}
	for _, tc := range tests {
		if got := rvlResourceBodyIsUsable(tc.contentType, tc.body, tc.name, tc.image); got != tc.want {
			t.Errorf("%s usable = %v, want %v", tc.name, got, tc.want)
		}
	}
	if _, ok := rvlArticleURL("http://rvl.swjtu.edu.cn", "https://news.swjtu.edu.cn/info/1012/110885.htm"); ok {
		t.Fatal("external detail URL was accepted")
	}
}

func TestRvlIndexFeedHasSingleVerifiedPage(t *testing.T) {
	base := "http://rvl.swjtu.edu.cn"
	if got := rvlPageURL(base, "index", 1, 0, ".htm"); got != base+"/index.htm" {
		t.Fatalf("index first page = %q, want %s/index.htm", got, base)
	}
	if got := rvlPageURL(base, "index", 2, 1, ".htm"); got != "" {
		t.Fatalf("index page 2 = %q, want empty (homepage is a single preview page)", got)
	}

	oldClient, oldNoCache, oldBase := defaultClient, NoCache, AllSites["rvl"].BaseURL
	NoCache = true
	AllSites["rvl"].BaseURL = base
	defer func() {
		defaultClient, NoCache, AllSites["rvl"].BaseURL = oldClient, oldNoCache, oldBase
		ClearCache()
	}()

	serve := func(fixture string) {
		defaultClient = &http.Client{Transport: rvlTestRoundTripper(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/index.htm" {
				return rvlTestResponse(r, http.StatusOK, "text/html; charset=utf-8", rvlFixture(t, fixture)), nil
			}
			return rvlTestResponse(r, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		})}
	}

	serve("rvl_home_live.html")
	ClearCache()
	if total, err := rvlTotalPages("index"); err != nil || total != 1 {
		t.Fatalf("index total = %d, %v; want 1 confirmed page", total, err)
	}

	// A portal that lost its article modules, an SPA/WAF shell, or a plain
	// column list must not be reported as a healthy single-page feed.
	for _, fixture := range []string{"rvl_spa.html", "rvl_waf.html", "rvl_list_live.html"} {
		serve(fixture)
		ClearCache()
		if total, err := rvlTotalPages("index"); err == nil || total != 0 {
			t.Errorf("index over %s = %d, %v; want refusal", fixture, total, err)
		}
	}
}

func TestRvlLegacyTemplateArticleUsesContent14Body(t *testing.T) {
	// info/1066/2298.htm (2018) follows the pre-2019 template: the body
	// paragraphs sit directly in .content14 and .v_news_content is empty.
	// The fallback must recover the announcement instead of syncing an empty
	// article, and must not leak the empty vsb/vote containers into the HTML.
	article, err := parseAdapter_rvlArticle(rvlFixture(t, "rvl_live_article_2298.html"), "http://rvl.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse legacy article: %v", err)
	}
	if article.Title != "大国速度——中国高铁崛起之路" || article.Type != "学术活动" {
		t.Fatalf("legacy article title/type = %q/%q", article.Title, article.Type)
	}
	if article.Date != "2018-02-01" || article.PublishedAt != "2018-02-01 09:11:50" {
		t.Fatalf("legacy article date = %q/%q", article.Date, article.PublishedAt)
	}
	for _, want := range []string{"时间：2018-02-01 16:30--2018-02-01 17:30", "地点：牵引动力国家重点实验室红楼218会议室", "报告人：徐厚广（高铁见闻）"} {
		if !strings.Contains(article.Content, want) {
			t.Fatalf("legacy content lost %q: %q", want, article.Content)
		}
	}
	if len(article.Images) != 0 || len(article.Attachments) != 0 {
		t.Fatalf("legacy article resources = %#v/%#v, want none", article.Images, article.Attachments)
	}
	for _, junk := range []string{"vsb_content", "div_vote_id"} {
		if strings.Contains(article.ContentHTML, junk) {
			t.Fatalf("legacy ContentHTML leaked %s: %q", junk, article.ContentHTML)
		}
	}

	// A genuinely empty body on the old template must still parse (the
	// syncer skips empties); the template fallback must not resurrect the
	// empty vsb_content node as content.
	empty := []byte(`<html><head><title>空旧文</title></head><body><form name="_newscontent_fromname"><div class="article"><h1>空旧文</h1><div class="source">日期：2018/02/01 09:11:50</div><div class="art_con"><div class="content14"><div id="vsb_content"><div class="v_news_content"></div></div><div id="div_vote_id"></div></div></div></div></form></body></html>`)
	parsed, err := parseAdapter_rvlArticle(empty, "http://rvl.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("empty legacy article returned an error: %v", err)
	}
	if strings.TrimSpace(parsed.Content) != "" || len(parsed.Images) != 0 || strings.Contains(parsed.ContentHTML, "vsb_content") {
		t.Fatalf("empty legacy article = %#v", parsed)
	}
}
