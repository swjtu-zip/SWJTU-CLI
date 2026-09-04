package scraper

import (
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const mysFixtureBaseURL = "https://mys.swjtu.edu.cn"

func readMysFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read mys fixture %q: %v", name, err)
	}
	return body
}

func TestMysAdapterRegistrationAndReversePagination(t *testing.T) {
	site := AllSites["mys"]
	if site == nil || site.BaseURL != mysCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("mys adapter is incomplete: %#v", site)
	}
	wantFeeds := []string{
		"djgz", "djgz/djfc", "djgz/tszb", "djgz/ljsy", "djgz/wjjb",
		"xsgz", "xsgz/tzgg", "xsgz/tcsy", "xsgz/xsdj",
		"xsgz/xytw/tjzx", "xsgz/xytw/xsh", "xsgz/xytw/qnzyzxh",
		"xsgz/xytw/tcr_xcm", "xsgz/xytw/xxfzzdzx", "xsgz/xytw/syglwyh",
		"xsgz/jqzd", "xsgz/cxsj", "xsgz/jdyd", "xsgz/xswhhd",
		"jxjw/jxgg", "jxjw/pyfa/a2014jbkspyfa", "jxjw/pyfa/a2015jbkspyfa",
		"jxjw/pyfa/a2016jbkspyfa",
		"dwjl", "dwjl/ryzj", "dwjl/tzgg", "dwjl/jhxm/yxxm",
		"ryyy", "ryyy/ssyhjzhdz",
		"ryyy/lrxszx", "ryyy/czzjd",
		"tcjs", "tcsx", "tckc", "tcqn", "tcsh", "xyt", "tcyw",
	}
	if !reflect.DeepEqual(site.SubPages, wantFeeds) {
		t.Fatalf("mys feeds = %#v, want %#v", site.SubPages, wantFeeds)
	}

	cases := []struct {
		slug, want  string
		page, total int
	}{
		{slug: "xyt", page: 1, total: 58, want: mysFixtureBaseURL + "/xyt.htm"},
		{slug: "xyt", page: 2, total: 58, want: mysFixtureBaseURL + "/xyt/57.htm"},
		{slug: "xyt", page: 29, total: 58, want: mysFixtureBaseURL + "/xyt/30.htm"},
		{slug: "xyt", page: 58, total: 58, want: mysFixtureBaseURL + "/xyt/1.htm"},
		{slug: "xsgz/xytw/tcr_xcm", page: 6, total: 12, want: mysFixtureBaseURL + "/xsgz/xytw/tcr_xcm/7.htm"},
		{slug: "jxjw/jxgg", page: 4, total: 4, want: mysFixtureBaseURL + "/jxjw/jxgg/1.htm"},
		{slug: "tcjs", page: 1, total: 1, want: mysFixtureBaseURL + "/tcjs.htm"},
	}
	for _, tc := range cases {
		if got := mysPageURL("http://mys.swjtu.edu.cn", tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("mysPageURL(%q, page %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
	if got := mysPageURL("http://mys.swjtu.edu.cn", "xyt", 59, 58, ".htm"); got != "" {
		t.Fatalf("out-of-range mys page URL = %q, want empty", got)
	}
	if got := mysPageURL("http://mys.swjtu.edu.cn", "xyt", 2, 0, ".htm"); got != "" {
		t.Fatalf("unknown-total mys page URL = %q, want empty", got)
	}
	if got := mysPageCount(readMysFixture(t, "mys_list.html")); got != 58 {
		t.Fatalf("mys page count = %d, want 58", got)
	}
}

func TestMysAuditedFeedPaginationMatrix(t *testing.T) {
	// Counts observed from the source pagination controls on 2026-09-04.  The
	// test keeps the audit inventory next to the route registry so a later
	// navigation change cannot silently reintroduce an empty/static page.
	cases := []struct {
		slug  string
		total int
	}{
		{"djgz", 4}, {"djgz/djfc", 1}, {"djgz/tszb", 1}, {"djgz/ljsy", 1}, {"djgz/wjjb", 1},
		{"xsgz", 34}, {"xsgz/tzgg", 10}, {"xsgz/tcsy", 3}, {"xsgz/xsdj", 7},
		{"xsgz/xytw/tjzx", 1}, {"xsgz/xytw/xsh", 4}, {"xsgz/xytw/qnzyzxh", 1},
		{"xsgz/xytw/tcr_xcm", 12}, {"xsgz/xytw/xxfzzdzx", 4}, {"xsgz/xytw/syglwyh", 3},
		{"xsgz/jqzd", 3}, {"xsgz/cxsj", 1}, {"xsgz/jdyd", 4}, {"xsgz/xswhhd", 9},
		{"jxjw/jxgg", 4}, {"jxjw/pyfa/a2014jbkspyfa", 1}, {"jxjw/pyfa/a2015jbkspyfa", 2}, {"jxjw/pyfa/a2016jbkspyfa", 1},
		{"dwjl", 9}, {"dwjl/ryzj", 1}, {"dwjl/tzgg", 1}, {"dwjl/jhxm/yxxm", 1},
		{"ryyy", 1}, {"ryyy/ssyhjzhdz", 1}, {"ryyy/lrxszx", 1}, {"ryyy/czzjd", 1},
		{"tcjs", 1}, {"tcsx", 1}, {"tckc", 1}, {"tcqn", 1}, {"tcsh", 1},
		{"xyt", 58}, {"tcyw", 53},
	}
	if len(cases) != len(mysSubPages) {
		t.Fatalf("audit matrix has %d routes, mysSubPages has %d", len(cases), len(mysSubPages))
	}
	for _, tc := range cases {
		if !mysKnownFeed(tc.slug) {
			t.Errorf("audited route %q is not registered", tc.slug)
			continue
		}
		if got := mysPageURL(mysFixtureBaseURL, tc.slug, 1, tc.total, ".htm"); got != mysFixtureBaseURL+"/"+tc.slug+".htm" {
			t.Errorf("%s first page = %q", tc.slug, got)
		}
		if got := mysPageURL(mysFixtureBaseURL, tc.slug, tc.total+1, tc.total, ".htm"); got != "" {
			t.Errorf("%s out-of-range page = %q", tc.slug, got)
		}
		if tc.total > 1 {
			middle := (tc.total + 2) / 2
			physical := tc.total - middle + 1
			want := mysFixtureBaseURL + "/" + tc.slug + "/" + strconv.Itoa(physical) + ".htm"
			if got := mysPageURL(mysFixtureBaseURL, tc.slug, middle, tc.total, ".htm"); got != want {
				t.Errorf("%s middle logical page %d = %q, want %q", tc.slug, middle, got, want)
			}
			want = mysFixtureBaseURL + "/" + tc.slug + "/1.htm"
			if got := mysPageURL(mysFixtureBaseURL, tc.slug, tc.total, tc.total, ".htm"); got != want {
				t.Errorf("%s last page = %q, want %q", tc.slug, got, want)
			}
		}
	}
}

func TestMysHomepageIncludesCarouselAndHiddenModules(t *testing.T) {
	items, err := parseAdapter_mysList(readMysFixture(t, "mys_home.html"), "http://mys.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse mys homepage: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("homepage items = %d, want 5 unique articles: %#v", len(items), items)
	}

	want := map[string]struct {
		title, typ, date string
	}{
		mysFixtureBaseURL + "/info/1001/11013.htm": {"轮播唯一文章", "", ""},
		mysFixtureBaseURL + "/info/1001/10993.htm": {"重复文章完整标题", "唐臣要闻", ""},
		mysFixtureBaseURL + "/info/1059/10513.htm": {"首页通知文章", "学院通知", ""},
		mysFixtureBaseURL + "/info/1023/11513.htm": {"隐藏公示文章", "公示专栏", ""},
		mysFixtureBaseURL + "/info/1037/11553.htm": {"活动文章", "活动公告", ""},
	}
	for _, item := range items {
		expected, ok := want[item.URL]
		if !ok {
			t.Errorf("unexpected homepage item: %#v", item)
			continue
		}
		if item.Title != expected.title || item.Type != expected.typ || item.Date != expected.date || item.PublishedAt != "" {
			t.Errorf("homepage item = %#v, want title=%q type=%q date=%q", item, expected.title, expected.typ, expected.date)
		}
	}
	if len(want) != len(items) {
		t.Fatalf("homepage did not contain every expected module: %#v", items)
	}
}

func TestMysLiveHomepageAndReversePaginationFixtures(t *testing.T) {
	items, err := parseAdapter_mysList(readMysFixture(t, "mys_live_home.html"), mysFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse captured mys homepage: %v", err)
	}
	if len(items) != 9 {
		t.Fatalf("captured homepage items = %d, want carousel plus four modules: %#v", len(items), items)
	}
	want := map[string]struct{ title, typ string }{
		mysFixtureBaseURL + "/info/1001/11013.htm": {"唐臣筑梦·扬华致远——茅以升学院毕业嘉年华圆满落幕", ""},
		mysFixtureBaseURL + "/info/1001/10873.htm": {"唐臣读书课第六讲 烽火弦歌：王尧教授人民文学奖长篇小说《桃花坞》读书分享会圆满...", ""},
		mysFixtureBaseURL + "/info/1059/10513.htm": {"2026年茅以升学院“唐臣奖章”评选通知", "学院通知"},
		mysFixtureBaseURL + "/info/1060/11623.htm": {"行走先贤足迹传承报国初心｜通识课《架桥者茅以升》师生赴杭州开展主题研学实践", "唐臣要闻"},
		mysFixtureBaseURL + "/info/1023/11513.htm": {"茅以升学院学生社团干部任职名单公示", "公示专栏"},
		mysFixtureBaseURL + "/info/1037/11553.htm": {"“2026行知中国的茅院人”（暑假版）征稿通知", "活动公告"},
	}
	for _, item := range items {
		if expected, ok := want[item.URL]; ok && (item.Title != expected.title || item.Type != expected.typ || item.Date != "" || item.PublishedAt != "") {
			t.Errorf("captured homepage item = %#v, want title=%q type=%q and no date", item, expected.title, expected.typ)
		}
	}
	for url := range want {
		found := false
		for _, item := range items {
			if item.URL == url {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("captured homepage omitted %s", url)
		}
	}

	listCases := []struct {
		fixture, title, date, url string
	}{
		{"mys_live_list_xyt_first.html", "2026年茅以升学院“唐臣奖章”评选通知", "2026-04-24", "/info/1059/10513.htm"},
		{"mys_live_list_xyt_middle.html", "茅以升学院2018-2019学年本科生国家奖学金和国家励志奖学金评选通知", "2019-09-04", "/info/1059/3213.htm"},
		{"mys_live_list_xyt_last.html", "学习伙伴倡议书", "2016-04-19", "/info/1059/1974.htm"},
	}
	for _, tc := range listCases {
		got, parseErr := parseAdapter_mysList(readMysFixture(t, tc.fixture), mysFixtureBaseURL)
		if parseErr != nil {
			t.Errorf("parse %s: %v", tc.fixture, parseErr)
			continue
		}
		if len(got) != 1 && tc.fixture != "mys_live_list_xyt_first.html" {
			t.Errorf("%s items = %#v, want one row", tc.fixture, got)
			continue
		}
		if len(got) == 0 || got[0].URL != mysFixtureBaseURL+tc.url || got[0].Title != tc.title || got[0].Date != tc.date || got[0].Type != "学院通知" {
			t.Errorf("%s first row = %#v", tc.fixture, got)
		}
		if got[0].PublishedAt != "" {
			t.Errorf("%s date-only row invented a time: %#v", tc.fixture, got[0])
		}
		if gotPages := mysPageCount(readMysFixture(t, tc.fixture)); gotPages != 58 {
			t.Errorf("%s page count = %d, want 58", tc.fixture, gotPages)
		}
	}
	if _, err := parseAdapter_mysList(readMysFixture(t, "mys_live_list_xyt_oob.html"), mysFixtureBaseURL); err == nil {
		t.Fatal("captured HTTP 404 page was accepted as a mys list")
	}
}

func TestMysListExtractsTypeTimeAndRejectsExternal(t *testing.T) {
	items, err := parseAdapter_mysList(readMysFixture(t, "mys_list.html"), mysFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse mys list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("list items = %d, want 2 internal articles: %#v", len(items), items)
	}
	if items[0].URL != mysFixtureBaseURL+"/info/1059/10513.htm" || items[0].Title != "真实通知标题" || items[0].Type != "学院通知" || items[0].Date != "2026-04-24" || items[0].PublishedAt != "2026-04-24 09:05:06" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[1].URL != mysFixtureBaseURL+"/info/1060/11623.htm" || items[1].Type != "唐臣要闻" || items[1].Date != "2026-07-22" {
		t.Fatalf("second list item = %#v", items[1])
	}
	for _, item := range items {
		if strings.Contains(item.URL, "evil.example") || strings.Contains(item.Title, "外链") {
			t.Fatalf("external list link leaked: %#v", item)
		}
	}
}

func TestMysPageTypeAcceptsDuplicateMetaTags(t *testing.T) {
	list := []byte(`<html><head><meta name="pageType" content="1"><meta name="pageType" content="2"></head><body><div class="news-list-box"><div class="title"><h4>重复标签列表</h4></div><ul class="list"><li><a href="/info/1059/10513.htm"><span class="desc">列表文章标题<span>重复标签列表</span></span><span class="state">2026-04-24</span></a></li></ul></div></body></html>`)
	items, err := parseAdapter_mysList(list, mysFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != "重复标签列表" {
		t.Fatalf("duplicate pageType list = %#v", items)
	}

	article := []byte(`<html><head><meta name="pageType" content="1"><meta name="pageType" content="3"><meta name="pageTitle" content="首页壳标题"><meta name="pageTitle" content="重复标签详情标题"></head><body><div class="detail-box"><div class="hd"><h3>详情 DOM 权威标题</h3></div><div class="v_news_content"><p>详情正文内容</p></div><div class="source"><span>发布日期：2026-04-24</span></div></div></body></html>`)
	parsed, err := parseAdapter_mysArticle(article, mysFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Title != "详情 DOM 权威标题" || !strings.Contains(parsed.Content, "详情正文内容") {
		t.Fatalf("duplicate pageType article = %#v", parsed)
	}
}

func TestMysArticleUsesDetailMetadataAndCollectsOutsideAttachments(t *testing.T) {
	article, err := parseAdapter_mysArticle(readMysFixture(t, "mys_article.html"), "http://mys.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse mys article: %v", err)
	}
	if article.Title != "详情页权威标题" || article.Date != "2026-04-24" || article.PublishedAt != "2026-04-24 09:05:06" || article.Type != "学院通知" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "正文第一段") || !strings.Contains(article.Content, "正文第二段") || strings.Contains(article.Content, "侧栏伪正文") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != mysFixtureBaseURL+"/__local/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="`+mysFixtureBaseURL+`/__local/body.png"`) || !strings.Contains(article.ContentHTML, mysFixtureBaseURL+"/files/body.pdf") {
		t.Fatalf("resolved body resources missing from ContentHTML: %q", article.ContentHTML)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v, want body and outside attachment", article.Attachments)
	}
	if article.Attachments[0].Name != "正文附件.pdf" || article.Attachments[0].URL != mysFixtureBaseURL+"/files/body.pdf" {
		t.Fatalf("body attachment = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "评选办法.docx" || !strings.Contains(article.Attachments[1].URL, "download.jsp") {
		t.Fatalf("outside attachment = %#v", article.Attachments[1])
	}
}

func TestMysEmptyAndNonArticleResponsesAreStrict(t *testing.T) {
	items, err := parseAdapter_mysList(readMysFixture(t, "mys_live_empty_xsczda.html"), mysFixtureBaseURL)
	if err != nil {
		t.Fatalf("legal empty mys list returned error: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("legal empty mys list = %#v, want non-nil empty slice", items)
	}
	for _, excluded := range []string{
		"xsgz/xsczda", "jxjw/rykc", "dwjl/jhxm/zqjhxm", "dwjl/jhxm/dqjhxm",
		"ryyy/tcjzhdz", "ryyy/xzzhw", "ryyy/yybtx", "tcxg",
		"hzxy", "zsgz/xbxz", "zsgz/sqbm", "xsgz/xytw/twjj",
		"jxjw/zyjs/gklyjxbjcxb", "jxjw/zyjs/kxkjcfhxjyb",
	} {
		for _, feed := range mysSubPages {
			if feed == excluded {
				t.Fatalf("known empty mys column %q was registered as a feed", excluded)
			}
		}
	}
	if _, err := parseAdapter_mysList(readMysFixture(t, "mys_live_static_profile.html"), mysFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as a mys list")
	}
	if _, err := parseAdapter_mysArticle(readMysFixture(t, "mys_live_static_profile.html"), mysFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as a mys article")
	}
	if _, err := parseAdapter_mysArticle(readMysFixture(t, "mys_captcha.html"), mysFixtureBaseURL); err == nil {
		t.Fatal("captcha/error page was accepted as a mys article")
	}
	if _, err := parseAdapter_mysList([]byte(`<html><body><div id="app"></div></body></html>`), mysFixtureBaseURL); err == nil {
		t.Fatal("SPA shell was accepted as a mys list")
	}
}

func TestMysLiveArticleUsesFixedMetadataAndContentScope(t *testing.T) {
	article, err := parseAdapter_mysArticle(readMysFixture(t, "mys_live_article.html"), mysFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse captured mys article: %v", err)
	}
	if article.Title != "2026年茅以升学院“唐臣奖章”评选通知" || article.Type != "学院通知" || article.Date != "2026-04-24" || article.PublishedAt != "2026-04-24" {
		t.Fatalf("captured article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "2026年4月30日17:00前") || strings.Contains(article.Content, "showVsbVideo") {
		t.Fatalf("captured article body lost prose or retained script: %q", article.Content)
	}
	if strings.Contains(article.ContentHTML, "showVsbVideo") || !strings.Contains(article.ContentHTML, "outside.example/guide") {
		t.Fatalf("captured article ContentHTML scope/link handling is wrong: %q", article.ContentHTML)
	}
	if len(article.Attachments) != 4 {
		t.Fatalf("captured article attachment candidates = %#v, want four candidates before HTTP verification", article.Attachments)
	}

	carousel, err := parseAdapter_mysArticle(readMysFixture(t, "mys_live_carousel_article.html"), mysFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse captured carousel article: %v", err)
	}
	if carousel.Title != "唐臣筑梦·扬华致远——茅以升学院毕业嘉年华圆满落幕" || carousel.Type != "" || carousel.Date != "2026-05-29" || carousel.PublishedAt != "2026-05-29" {
		t.Fatalf("carousel detail metadata = %#v", carousel)
	}
	if strings.Contains(carousel.Content, "showVsbVideo") || strings.Contains(carousel.ContentHTML, "_videourl") {
		t.Fatalf("carousel video script leaked into body: content=%q html=%q", carousel.Content, carousel.ContentHTML)
	}
}

func TestMysFetchArticleVerifiesLiveResources(t *testing.T) {
	articleHTML := readMysFixture(t, "mys_live_article.html")
	captchaHTML := readMysFixture(t, "mys_live_attachment_captcha.html")
	requests := make(map[string]int)
	oldClient, oldNoCache := defaultClient, NoCache
	defaultClient = &http.Client{Transport: mysTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests[request.URL.Path]++
		contentType := "application/octet-stream"
		status := http.StatusOK
		var body []byte
		switch request.URL.Path {
		case "/info/1059/10513.htm":
			contentType, body = "text/html; charset=utf-8", articleHTML
		case "/__local/real.png":
			contentType, body = "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
		case "/__local/bad.png":
			contentType, body = "image/png", captchaHTML
		case "/files/good.pdf":
			contentType, body = "application/pdf", []byte("%PDF-1.7\n")
		case "/files/wrong.pdf":
			contentType, body = "application/pdf", []byte("not a PDF")
		case "/system/_content/download.jsp":
			contentType, body = "text/html; charset=UTF-8", captchaHTML
		default:
			status, body = http.StatusNotFound, []byte("not found")
		}
		return mysTestHTTPResponse(request, status, contentType, body), nil
	})}
	NoCache = true
	defer func() { defaultClient, NoCache = oldClient, oldNoCache }()

	site := AllSites["mys"]
	oldBase := site.BaseURL
	site.BaseURL = mysFixtureBaseURL
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL = oldBase
		ClearCache()
	})

	article, raw, err := site.GetArticleSnapshot(mysFixtureBaseURL + "/info/1059/10513.htm")
	if err != nil {
		t.Fatalf("fetch captured mys article: %v", err)
	}
	if string(raw) != string(articleHTML) || article.RawHTML != string(articleHTML) || article.Date != "2026-04-24" || article.PublishedAt != "2026-04-24" {
		t.Fatalf("snapshot/fixed date mismatch: raw=%d article=%#v", len(raw), article)
	}
	if len(article.Images) != 1 || article.Images[0] != mysFixtureBaseURL+"/__local/real.png" {
		t.Fatalf("verified image candidates = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != mysFixtureBaseURL+"/files/good.pdf" || article.Attachments[0].Size != "9" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	for _, rejected := range []string{"bad.png", "wrong.pdf", "missing.pdf", "download.jsp", "cdn.example/foreign.png", "showVsbVideo"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource/script remained in ContentHTML: %q -> %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, mysFixtureBaseURL+"/__local/real.png") || !strings.Contains(article.ContentHTML, mysFixtureBaseURL+"/files/good.pdf") || !strings.Contains(article.ContentHTML, "https://outside.example/guide") {
		t.Fatalf("verified resources or external ordinary link missing: %q", article.ContentHTML)
	}
	for _, path := range []string{"/__local/bad.png", "/files/wrong.pdf", "/files/missing.pdf", "/system/_content/download.jsp"} {
		if requests[path] == 0 {
			t.Errorf("invalid resource %s was not checked", path)
		}
	}
}

type mysTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper mysTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func mysTestHTTPResponse(request *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}
}

func TestMysResourceValidationRequiresContentTypeAndMagic(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !mysResourceBodyIsUsable("image/png", png, "good.png", true) {
		t.Fatal("valid PNG was rejected")
	}
	if mysResourceBodyIsUsable("image/png", []byte("<html>请输入验证码下载附件</html>"), "bad.png", true) {
		t.Fatal("HTML CAPTCHA was accepted as an image")
	}
	if !mysResourceBodyIsUsable("application/pdf", []byte("%PDF-1.7"), "good.pdf", false) {
		t.Fatal("valid PDF was rejected")
	}
	if mysResourceBodyIsUsable("application/pdf", []byte("not a PDF"), "bad.pdf", false) {
		t.Fatal("mismatched PDF magic was accepted")
	}
	if !mysResourceBodyIsUsable("application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte{'P', 'K', 0x03, 0x04}, "good.docx", false) {
		t.Fatal("valid docx ZIP magic was rejected")
	}
	if !mysResourceBodyIsUsable("application/msword", []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}, "good.doc", false) {
		t.Fatal("valid legacy Office OLE magic was rejected")
	}
}
