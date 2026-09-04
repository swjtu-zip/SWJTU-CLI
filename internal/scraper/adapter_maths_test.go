package scraper

import (
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

func readMathsFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read maths fixture %s: %v", name, err)
	}
	return body
}

func TestMathsAdapterRegistrationAndReversePagination(t *testing.T) {
	site := AllSites["maths"]
	if site == nil || site.BaseURL != mathsCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("maths adapter hooks are incomplete: %#v", site)
	}
	if !reflect.DeepEqual(site.SubPages, mathsSubPages) {
		t.Fatalf("maths feeds = %#v, want %#v", site.SubPages, mathsSubPages)
	}
	cases := []struct {
		slug        string
		page, total int
		want        string
	}{
		{"xyxw", 1, 62, mathsCanonicalBaseURL + "/xyxw.htm"},
		{"xyxw", 2, 62, mathsCanonicalBaseURL + "/xyxw/61.htm"},
		{"xyxw", 31, 62, mathsCanonicalBaseURL + "/xyxw/32.htm"},
		{"xyxw", 62, 62, mathsCanonicalBaseURL + "/xyxw/1.htm"},
		{"jyjx1", 32, 63, mathsCanonicalBaseURL + "/jyjx1/32.htm"},
		{"index", 1, 1, mathsCanonicalBaseURL + "/index.htm"},
	}
	for _, tc := range cases {
		if got := mathsPageURL("http://maths.swjtu.edu.cn", tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("mathsPageURL(%q, %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 62}, {63, 62}, {2, 0}, {2, 1},
	} {
		if got := mathsPageURL(mathsCanonicalBaseURL, "xyxw", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("invalid page %d/%d produced %q", tc.page, tc.total, got)
		}
	}
	if got := mathsPageURL(mathsCanonicalBaseURL, "index", 2, 1, ".htm"); got != "" {
		t.Fatalf("homepage out-of-range URL = %q", got)
	}
	if got := mathsPageCount(nil); got != 0 {
		t.Fatalf("nil page count = %d, want 0", got)
	}
	if _, err := mathsTotalPages("not-a-live-feed"); err == nil {
		t.Fatal("un-audited route was accepted")
	}
}

func TestMathsAuditedSubPagesAreExplicit(t *testing.T) {
	want := []string{
		"jyjx1", "jyjx1/bkjx1", "jyjx1/yjsjx1", "jyjx1/zyjsjjyq", "jyjx1/sxsyzx", "jyjx1/sxjd", "jyjx1/hzq",
		"xsky/kytz", "xsky/xsjl", "xsky/sjycx", "xsky/kycg", "xsky/scsxctjxh",
		"dwjw", "dwjw/djdt", "dwjw/djtz", "dwjw/llxx", "dwjw/rdzn",
		"xsgz", "xsgz/xgtz", "xsgz/xshd", "xsgz/jyxx",
		"ghgz", "ghgz/ghhd", "xygz", "xygz/xyhd", "xyxw", "xygg",
	}
	if !reflect.DeepEqual(mathsSubPages, want) {
		t.Fatalf("maths audited feeds changed: %#v, want %#v", mathsSubPages, want)
	}
	for _, excluded := range []string{
		"xygk/xyjj", "xygk/xrld", "xygk/zzjg", "xygk/jswyh", "sztd/szjj", "sbds", "sxx", "tjx", "sjkxx", "ggsxjxyjb",
		"jyjx1/sxxxzx", "xsky/kypt/xtkxxzdyzgj_", "dwjw/dwjj", "xsgz/xsgzz", "xsgz/xytw", "xsgz/xszz", "xsgz/xsjzfw",
		"ghgz/ghgk", "xygz/xyxx", "xygz/xyfw", "xygz/sjxx", "xygz/yzxx", "xygz/xmtptspcm",
	} {
		for _, slug := range mathsSubPages {
			if slug == excluded {
				t.Errorf("static/service route %q was registered as an article feed", excluded)
			}
		}
	}
}

func TestMathsHomepageIncludesHiddenModulesAndFiltersExternalLinks(t *testing.T) {
	items, err := parseAdapter_mathsList(readMathsFixture(t, "maths_home_tabs.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse maths homepage: %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("homepage unique article count = %d, want 6", len(items))
	}
	want := map[string]struct {
		title string
		date  string
		typ   string
	}{
		"/info/1296/1.htm": {"轮播文章", "", "首页焦点"},
		"/info/1149/2.htm": {"新闻动态文章", "2026-08-20", "新闻动态"},
		"/info/1150/3.htm": {"公告文章", "2026-08-19", "通知公告"},
		"/info/1150/4.htm": {"隐藏研究生公告", "2026-08-18", "研究生"},
		"/info/1041/5.htm": {"学术活动文章", "", "学术活动"},
		"/info/1052/6.htm": {"党建文章", "05-22", "党建工作"},
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, mathsCanonicalBaseURL+"/info/") {
			t.Fatalf("non-local homepage URL leaked: %#v", item)
		}
		path := strings.TrimPrefix(item.URL, mathsCanonicalBaseURL)
		wantItem, ok := want[path]
		if !ok {
			t.Fatalf("unexpected homepage item: %#v", item)
		}
		if item.Title != wantItem.title || item.Date != wantItem.date || item.Type != wantItem.typ {
			t.Errorf("homepage item %s = %#v, want title/date/type %q/%q/%q", path, item, wantItem.title, wantItem.date, wantItem.typ)
		}
	}
}

func TestMathsOfficialHomepageFixtureCoversAllArticleModules(t *testing.T) {
	items, err := parseAdapter_mathsList(readMathsFixture(t, "maths_home_live.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse official maths homepage: %v", err)
	}
	wantTypes := map[string]int{
		"首页焦点": 5,
		"新闻动态": 5,
		"通知公告": 6,
		"学术活动": 4,
		"党建工作": 7,
		"教学工作": 7,
		"学生工作": 7,
	}
	gotTypes := make(map[string]int)
	for _, item := range items {
		gotTypes[item.Type]++
	}
	if len(items) != 41 {
		t.Fatalf("official maths homepage returned %d items, want 41: %#v", len(items), gotTypes)
	}
	for typ, want := range wantTypes {
		if gotTypes[typ] != want {
			t.Errorf("official homepage type %q count = %d, want %d", typ, gotTypes[typ], want)
		}
	}
	for _, want := range []struct {
		url  string
		typ  string
		date string
	}{
		{mathsCanonicalBaseURL + "/info/1149/26964.htm", "新闻动态", "2026-09-03"},
		{mathsCanonicalBaseURL + "/info/1150/26684.htm", "通知公告", "2026-07-06"},
		{mathsCanonicalBaseURL + "/info/1041/26914.htm", "学术活动", ""},
		{mathsCanonicalBaseURL + "/info/1052/26884.htm", "党建工作", "08-25"},
		{mathsCanonicalBaseURL + "/info/1027/26244.htm", "教学工作", "06-09"},
		{mathsCanonicalBaseURL + "/info/1064/26944.htm", "学生工作", "09-02"},
	} {
		found := false
		for _, item := range items {
			if item.URL == want.url {
				found = item.Type == want.typ && item.Date == want.date
				break
			}
		}
		if !found {
			t.Errorf("official homepage item %s missing expected type/date %s/%s", want.url, want.typ, want.date)
		}
	}
}

func TestMathsListScopesRowsAndPreservesTypeDate(t *testing.T) {
	items, err := parseAdapter_mathsList(readMathsFixture(t, "maths_list.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse maths list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("list items = %d, want 2", len(items))
	}
	if items[0].Title != "列表截断标题" || items[0].URL != mathsCanonicalBaseURL+"/info/1150/9001.htm" || items[0].Date != "2026-08-25" || items[0].Type != "学院公告" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[1].Title != "带精确时间的公告" || items[1].PublishedAt != "2026-08-24 09:08:07" || items[1].Type != "学院公告" {
		t.Fatalf("second list item = %#v", items[1])
	}
	if got := mathsPageCount(parseMathsDocument(t, "maths_list.html")); got != 62 {
		t.Fatalf("list total pages = %d, want 62", got)
	}
	if _, ok := mathsArticleURL(mathsCanonicalBaseURL, "https://example.com/info/1/2.htm"); ok {
		t.Fatal("external article URL was accepted")
	}
	malformed := []byte(`<meta name="pageType" content="2"><div class="lists"><h5>学院公告</h5><ul><li><a href="/info/1150/9001.htm"></a></li></ul></div>`)
	if _, err := parseAdapter_mathsList(malformed, mathsCanonicalBaseURL); err == nil {
		t.Fatal("malformed local maths row was silently discarded")
	}
}

func TestMathsOfficialListFixturesPreservePaginationAndMetadata(t *testing.T) {
	cases := []struct {
		fixture      string
		total        int
		firstTitle   string
		firstDate    string
		firstType    string
		firstArticle string
	}{
		{"maths_list_xyxw_first_live.html", 62, "我院向多惠老师研究成果于Journal de Mathématiques Pures et Appliquées发表", "2026-09-03", "学院新闻", "/info/1149/26964.htm"},
		{"maths_list_xyxw_middle_live.html", 62, "数学学院召开实验工作研讨会", "2022-03-12", "学院新闻", "/info/1149/9459.htm"},
		{"maths_list_xyxw_last_live.html", 62, "关于学院网络信息安全和信息发布操作培训会通知", "2018-05-21", "学院新闻", "/info/1149/4985.htm"},
		{"maths_list_xsgz_jyxx_first_live.html", 15, "西南交通大学数学学院2026届毕业生选聘指南", "2025-09-09", "就业信息", "/info/1074/21794.htm"},
	}
	for _, tc := range cases {
		doc := parseMathsDocument(t, tc.fixture)
		if got := mathsPageCount(doc); got != tc.total {
			t.Errorf("%s page total = %d, want %d", tc.fixture, got, tc.total)
		}
		items, err := parseAdapter_mathsList(readMathsFixture(t, tc.fixture), mathsCanonicalBaseURL)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.fixture, err)
		}
		if len(items) == 0 || items[0].Title != tc.firstTitle || items[0].Date != tc.firstDate || items[0].Type != tc.firstType || !strings.HasSuffix(items[0].URL, tc.firstArticle) {
			t.Errorf("%s first item = %#v", tc.fixture, items)
		}
	}
	if _, err := parseAdapter_mathsList(readMathsFixture(t, "maths_list_xyxw_out_of_range_live.html"), mathsCanonicalBaseURL); err == nil {
		t.Fatal("official out-of-range 404 fixture was accepted")
	}
	if _, err := parseAdapter_mathsList(readMathsFixture(t, "maths_static_sxxxzx_live.html"), mathsCanonicalBaseURL); err == nil {
		t.Fatal("official static service fixture was accepted as a list")
	}
}

func TestMathsArticleUsesDetailTitleTypeBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_mathsArticle(readMathsFixture(t, "maths_article.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse maths article: %v", err)
	}
	if article.Title != "详情页完整标题——不要使用列表截断" || article.Date != "2026-08-25" || article.PublishedAt != "2026-08-25 14:05:06" || article.Type != "学院公告" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "这是数学学院正文第一段") || strings.Contains(article.Content, "导航噪声") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != mathsCanonicalBaseURL+"/images/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="`+mathsCanonicalBaseURL+`/images/body.png"`) {
		t.Fatalf("resolved image missing from ContentHTML: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件.zip" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
}

func TestMathsArticleDoesNotUseEventTimeAsPublicationDate(t *testing.T) {
	const html = `<html><head><meta name="pageType" content="3"></head><body>
<div class="page-bann"><h5>学术交流</h5></div>
<div class="inform"><h1>学术讲座通知</h1><div>2026-09-02 点击：0</div>
<div class="v_news_content"><p>报告时间：2026年9月6日16:00-17:00。</p></div></div>
</body></html>`
	article, err := parseAdapter_mathsArticle([]byte(html), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-09-02" || article.PublishedAt != "2026-09-02" {
		t.Fatalf("event time replaced publication date: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	const noFixedDateHTML = `<html><head><meta name="pageType" content="3"></head><body>
<div class="page-bann"><h5>学术交流</h5></div>
<div class="inform"><h1>没有固定发布日期的页面</h1><div class="v_news_content"><p>活动时间：2026年9月6日16:00。</p></div></div>
</body></html>`
	withoutFixedDate, err := parseAdapter_mathsArticle([]byte(noFixedDateHTML), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if withoutFixedDate.Date != "" || withoutFixedDate.PublishedAt != "" {
		t.Fatalf("body event date was used without a fixed publication node: date=%q published_at=%q", withoutFixedDate.Date, withoutFixedDate.PublishedAt)
	}
}

func TestMathsOfficialArticleFixturesUseFixedMetadataAndCleanPlaceholders(t *testing.T) {
	cases := []struct {
		fixture string
		title   string
		date    string
		typ     string
	}{
		{"maths_article_live_news.html", "我院向多惠老师研究成果于Journal de Mathématiques Pures et Appliquées发表", "2026-09-03", "学院新闻"},
		{"maths_article_live_attachment.html", "数学学院 关于2026年暑期实习相关工作的通知", "2026-06-09", "教学通知"},
		{"maths_article_live_old.html", "数学学院关于提交2018年度国家自然科学基金项目结题/成果报告的通知", "2019-01-10", "科研通知"},
	}
	for _, tc := range cases {
		article, err := parseAdapter_mathsArticle(readMathsFixture(t, tc.fixture), mathsCanonicalBaseURL)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.fixture, err)
		}
		if article.Title != tc.title || article.Date != tc.date || article.Type != tc.typ || strings.TrimSpace(article.Content) == "" {
			t.Errorf("%s metadata/body = %#v", tc.fixture, article)
		}
	}
	recent, err := parseAdapter_mathsArticle(readMathsFixture(t, "maths_article_live_news.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent.Images) != 1 || !strings.Contains(recent.Images[0], "/__local/") || strings.Contains(recent.ContentHTML, "spacer.gif") {
		t.Fatalf("official image evidence was not preserved/filtered: images=%#v html=%q", recent.Images, recent.ContentHTML)
	}
	attachment, err := parseAdapter_mathsArticle(readMathsFixture(t, "maths_article_live_attachment.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(attachment.Attachments) != 1 || attachment.Attachments[0].Name != "附件.zip" || !strings.Contains(attachment.Attachments[0].URL, "download.jsp") {
		t.Fatalf("official attachment link was not extracted: %#v", attachment.Attachments)
	}
	old, err := parseAdapter_mathsArticle(readMathsFixture(t, "maths_article_live_old.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(old.Images) != 0 || strings.Contains(old.ContentHTML, "spacer.gif") || !strings.Contains(old.Content, "sro.swjtu.edu.cn") {
		t.Fatalf("old detail placeholder/body handling failed: images=%#v html=%q content=%q", old.Images, old.ContentHTML, old.Content)
	}
}

func TestMathsFetchArticleVerifiesResources(t *testing.T) {
	articleHTML := readMathsFixture(t, "maths_article_resources.html")
	captchaHTML := readMathsFixture(t, "maths_attachment_captcha_live.html")
	oldClient, oldNoCache := defaultClient, NoCache
	defaultClient = &http.Client{Transport: mathsTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		contentType := "application/octet-stream"
		status := http.StatusOK
		var body []byte
		switch request.URL.Path {
		case "/info/1296/9002.htm":
			contentType, body = "text/html; charset=utf-8", articleHTML
		case "/good.png":
			contentType, body = "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
		case "/bad.png":
			contentType, body = "image/png", []byte("<html><body>验证码</body></html>")
		case "/bad-magic.png":
			contentType, body = "image/png", []byte("not an image")
		case "/bad-status.png":
			contentType, status, body = "image/png", http.StatusNotFound, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
		case "/good.pdf":
			contentType, body = "application/pdf", []byte("%PDF-1.7\n")
		case "/good.xlsx":
			contentType, body = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte{'P', 'K', 0x03, 0x04, 0x00}
		case "/captcha.xlsx":
			contentType, body = "text/html; charset=utf-8", captchaHTML
		case "/bad.pdf":
			contentType, body = "application/pdf", []byte("not a PDF")
		default:
			status, body = http.StatusNotFound, []byte("not found")
		}
		return mathsTestHTTPResponse(request, status, contentType, body), nil
	})}
	NoCache = true
	t.Cleanup(func() { defaultClient, NoCache = oldClient, oldNoCache; ClearCache() })

	article, raw, err := mathsFetchArticle(mathsCanonicalBaseURL + "/info/1296/9002.htm")
	if err != nil {
		t.Fatalf("fetch maths article: %v", err)
	}
	if len(raw) == 0 || len(article.Images) != 1 || article.Images[0] != mathsCanonicalBaseURL+"/good.png" {
		t.Fatalf("resource image verification failed: raw=%d images=%#v", len(raw), article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != mathsCanonicalBaseURL+"/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("resource attachment verification failed: %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, mathsCanonicalBaseURL+"/good.png") || !strings.Contains(article.ContentHTML, mathsCanonicalBaseURL+"/good.pdf") {
		t.Fatalf("verified resources missing from ContentHTML: %q", article.ContentHTML)
	}
	for _, rejected := range []string{"spacer.gif", "bad.png", "bad-magic.png", "captcha.xlsx", "javascript:download"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource/link remained in ContentHTML: %q", rejected)
		}
	}

	images := mathsVerifyImages([]string{
		mathsCanonicalBaseURL + "/good.png",
		mathsCanonicalBaseURL + "/bad-status.png",
		mathsCanonicalBaseURL + "/bad-magic.png",
	}, mathsCanonicalBaseURL)
	if !reflect.DeepEqual(images, []string{mathsCanonicalBaseURL + "/good.png"}) {
		t.Fatalf("status/magic image filtering = %#v", images)
	}
	attachments := mathsVerifyAttachments([]model.Attachment{
		{Name: "附件.xlsx", URL: mathsCanonicalBaseURL + "/good.xlsx"},
		{Name: "附件.pdf", URL: mathsCanonicalBaseURL + "/bad.pdf"},
		{Name: "附件.xlsx", URL: mathsCanonicalBaseURL + "/captcha.xlsx"},
	}, mathsCanonicalBaseURL)
	if len(attachments) != 1 || attachments[0].URL != mathsCanonicalBaseURL+"/good.xlsx" {
		t.Fatalf("attachment status/type/magic filtering = %#v", attachments)
	}
}

func TestMathsDistinguishesEmptyStaticAndVerificationPages(t *testing.T) {
	items, err := parseAdapter_mathsList(readMathsFixture(t, "maths_empty.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatalf("legal empty list rejected: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list returned items: %#v", items)
	}
	for _, fixture := range []string{"maths_static.html", "maths_captcha.html"} {
		if _, err := parseAdapter_mathsList(readMathsFixture(t, fixture), mathsCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as a list", fixture)
		}
		if _, err := parseAdapter_mathsArticle(readMathsFixture(t, fixture), mathsCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as an article", fixture)
		}
	}
	if _, err := parseAdapter_mathsArticle(readMathsFixture(t, "maths_list.html"), mathsCanonicalBaseURL); err == nil {
		t.Fatal("list page was accepted as an article")
	}
}

func TestMathsLiveAuditedRoutes(t *testing.T) {
	if os.Getenv("MATHS_LIVE") != "1" {
		t.Skip("set MATHS_LIVE=1 to run the live maths audit")
	}
	site := AllSites["maths"]
	if site == nil {
		t.Fatal("maths site is not registered")
	}
	for _, slug := range mathsSubPages {
		total, err := site.GetTotalPages(slug)
		if err != nil {
			t.Fatalf("%s total pages: %v", slug, err)
		}
		pages := []int{1, total}
		if total > 1 {
			pages = append(pages, 2)
		}
		if total > 2 {
			pages = append(pages, (total+1)/2)
		}
		seen := make(map[int]bool)
		for _, page := range pages {
			if seen[page] {
				continue
			}
			seen[page] = true
			items, listErr := site.ListNewsPage(slug, page)
			if listErr != nil {
				t.Fatalf("%s page %d/%d: %v", slug, page, total, listErr)
			}
			if len(items) == 0 {
				t.Fatalf("%s page %d/%d returned no articles", slug, page, total)
			}
		}
	}
	article, err := site.GetArticle(mathsCanonicalBaseURL + "/info/1027/26244.htm")
	if err != nil {
		t.Fatalf("live detail: %v", err)
	}
	if article.Title == "" || article.Date == "" || article.Type == "" || article.Content == "" || len(article.Attachments) != 0 {
		t.Fatalf("live detail metadata/body or CAPTCHA attachment filtering failed: %#v", article)
	}
	imageArticle, err := site.GetArticle(mathsCanonicalBaseURL + "/info/1149/26874.htm")
	if err != nil {
		t.Fatalf("live image detail: %v", err)
	}
	if len(imageArticle.Images) == 0 || imageArticle.Content == "" {
		t.Fatalf("live image detail lost body/images: %#v", imageArticle)
	}
}

func parseMathsDocument(t *testing.T, name string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(readMathsFixture(t, name))))
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return doc
}

type mathsTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper mathsTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func mathsTestHTTPResponse(request *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}
}
