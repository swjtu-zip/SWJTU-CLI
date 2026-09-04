package scraper

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

func TestEesAdapterRegisteredWithAuditedRoutes(t *testing.T) {
	site := AllSites["ees"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("ees adapter hooks are not registered")
	}
	if site.BaseURL != "https://ees.swjtu.edu.cn" {
		t.Fatalf("ees base URL = %q", site.BaseURL)
	}
	want := []string{
		"xwdt1/xwdt", "xwdt1/mtbd", "djgz/djtz", "djgz/djdt",
		"tzgg/qb", "tzgg/zs", "tzgg/py", "tzgg/sj", "tzgg/xg", "tzgg/zh",
		"zsxx/zsdt", "zsxx/zlxz", "rcpy/kcjx", "rcpy/gcal", "rcpy/rqsj", "rcpy/xwgz",
		"xmpt/xmjs", "xmpt/xqpt", "xmpt/gjjlpt", "szdw/xnds", "szdw/qyds",
		"xsgz/xgtz", "xsgz/xshd", "gfwj/zcwj", "gfwj/gzzd", "gfwj/cybg",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("ees sub-pages = %#v, want %#v", site.SubPages, want)
	}
}

func TestEesPageURLUsesReverseGeneratedPages(t *testing.T) {
	base := "https://ees.swjtu.edu.cn"
	tests := []struct {
		name       string
		slug       string
		page       int
		totalPages int
		want       string
	}{
		{name: "first", slug: "xwdt1/xwdt", page: 1, totalPages: 5, want: base + "/xwdt1/xwdt.htm"},
		{name: "second", slug: "xwdt1/xwdt", page: 2, totalPages: 5, want: base + "/xwdt1/xwdt/4.htm"},
		{name: "middle", slug: "xwdt1/xwdt", page: 3, totalPages: 5, want: base + "/xwdt1/xwdt/3.htm"},
		{name: "last", slug: "xwdt1/xwdt", page: 5, totalPages: 5, want: base + "/xwdt1/xwdt/1.htm"},
		{name: "single page", slug: "tzgg/qb", page: 1, totalPages: 1, want: base + "/tzgg/qb.htm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eesPageURL(base, tt.slug, tt.page, tt.totalPages, ".htm"); got != tt.want {
				t.Fatalf("eesPageURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEesMeasuredFeedsCoverEveryRegisteredArticleTab(t *testing.T) {
	measuredPages := map[string]int{
		"xwdt1/xwdt": 5, "xwdt1/mtbd": 5,
		"djgz/djtz": 1, "djgz/djdt": 1,
		"tzgg/qb": 2, "tzgg/zs": 1, "tzgg/py": 1, "tzgg/sj": 1, "tzgg/xg": 1, "tzgg/zh": 1,
		"zsxx/zsdt": 1, "zsxx/zlxz": 1,
		"rcpy/kcjx": 1, "rcpy/gcal": 1, "rcpy/rqsj": 1, "rcpy/xwgz": 1,
		"xmpt/xmjs": 1, "xmpt/xqpt": 1, "xmpt/gjjlpt": 1,
		"szdw/xnds": 1, "szdw/qyds": 1,
		"xsgz/xgtz": 1, "xsgz/xshd": 1,
		"gfwj/zcwj": 1, "gfwj/gzzd": 1, "gfwj/cybg": 1,
	}
	if !reflect.DeepEqual(eesSubPages, AllSites["ees"].SubPages) {
		t.Fatalf("registered EES routes diverged from adapter routes: %#v", AllSites["ees"].SubPages)
	}
	if len(measuredPages) != len(eesSubPages) {
		t.Fatalf("measured route count = %d, registered route count = %d", len(measuredPages), len(eesSubPages))
	}
	for _, slug := range eesSubPages {
		total, ok := measuredPages[slug]
		if !ok || total < 1 {
			t.Fatalf("route %q has no positive measured page count", slug)
		}
		if got := eesPageURL("https://ees.swjtu.edu.cn", slug, total+1, total, ".htm"); got != "" {
			t.Errorf("route %q out-of-range URL = %q", slug, got)
		}
		if total > 1 && !strings.HasSuffix(eesPageURL("https://ees.swjtu.edu.cn", slug, total, total, ".htm"), "/1.htm") {
			t.Errorf("route %q last logical page was not mapped to physical /1.htm", slug)
		}
	}
}

func TestEesLiveHomepageIncludesHiddenTechnicalCenterAndOnlyRealModules(t *testing.T) {
	items, err := parseAdapter_eesList(loadTestHTML("ees_home_live.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 18 {
		t.Fatalf("got %d live homepage items, want 8 news + 7 notices + 3 hidden-panel articles", len(items))
	}
	counts := make(map[string]int)
	for _, item := range items {
		counts[item.Type]++
		if strings.Contains(item.URL, "/info/1871/") || strings.Contains(item.URL, "xwdt1/jszx") || strings.Contains(item.URL, "beian") {
			t.Fatalf("homepage shell/banner/footer leaked into feed: %+v", item)
		}
	}
	if counts["新闻动态"] != 8 || counts["培养"] != 5 || counts["综合"] != 2 || counts["技术中心"] != 1 || counts["校企导师"] != 1 || counts["卓工学生"] != 1 {
		t.Fatalf("unexpected live homepage module counts: %#v", counts)
	}
	var technical model.NewsItem
	for _, item := range items {
		if item.Type == "技术中心" {
			technical = item
		}
	}
	if technical.Title != "实干树政绩，匠心育卓越——国家卓越工程师学院开展“七一”专题党课学习" || technical.Date != "2026-07-02" || technical.PublishedAt != "" {
		t.Fatalf("technical-center date/title precision = %+v", technical)
	}
}

func TestEesLiveListFixturesCoverFirstMiddleLastAndPhysicalPageCount(t *testing.T) {
	first, err := parseAdapter_eesList(loadTestHTML("ees_list_live_first.html"), "https://ees.swjtu.edu.cn")
	if err != nil || len(first) != 3 {
		t.Fatalf("first live page = items=%#v err=%v", first, err)
	}
	middle, err := parseAdapter_eesList(loadTestHTML("ees_list_live_middle.html"), "https://ees.swjtu.edu.cn")
	if err != nil || len(middle) != 3 {
		t.Fatalf("middle live page = items=%#v err=%v", middle, err)
	}
	last, err := parseAdapter_eesList(loadTestHTML("ees_list_live_last.html"), "https://ees.swjtu.edu.cn")
	if err != nil || len(last) != 2 {
		t.Fatalf("last live page = items=%#v err=%v", last, err)
	}
	if eesPageCount(parseDocumentForEesTest(t, "ees_list_live_first.html")) != 5 {
		t.Fatal("reverse pagination count for xwdt1/xwdt was not derived as 5")
	}
	if eesPageCount(parseDocumentForEesTest(t, "ees_list_notice_last_live.html")) != 2 {
		t.Fatal("reverse pagination count for tzgg/qb was not derived as 2")
	}
	if first[0].URL != "https://ees.swjtu.edu.cn/info/1311/4731.htm" || first[0].Date != "2026-09-02" || first[0].Type != "新闻动态" {
		t.Fatalf("first live row = %+v", first[0])
	}
	if middle[1].URL != "https://ees.swjtu.edu.cn/info/1311/2771.htm" || middle[1].Date != "2025-11-20" {
		t.Fatalf("middle live row = %+v", middle[1])
	}
	if last[1].Date != "2024-05-15" {
		t.Fatalf("last live row = %+v", last[1])
	}
}

func TestEesTotalPagesFetchesSourceCountAndMapsLogicalPages(t *testing.T) {
	first := loadTestHTML("ees_list_live_first.html")
	middle := loadTestHTML("ees_list_live_middle.html")
	last := loadTestHTML("ees_list_live_last.html")
	baseURL := "https://ees.test"
	paths := make(map[string]int)
	site := AllSites["ees"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL = baseURL
	defaultClient = &http.Client{Transport: eesTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		paths[request.URL.Path]++
		switch request.URL.Path {
		case "/xwdt1/xwdt.htm":
			return eesTestResponse(request, http.StatusOK, "text/html; charset=utf-8", first), nil
		case "/xwdt1/xwdt/4.htm", "/xwdt1/xwdt/3.htm":
			return eesTestResponse(request, http.StatusOK, "text/html; charset=utf-8", middle), nil
		case "/xwdt1/xwdt/1.htm":
			return eesTestResponse(request, http.StatusOK, "text/html; charset=utf-8", last), nil
		default:
			return eesTestResponse(request, http.StatusNotFound, "text/plain", []byte("not found")), nil
		}
	})}
	NoCache = true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL = oldBase
		defaultClient = oldClient
		NoCache = oldNoCache
		ClearCache()
	})

	total, err := eesTotalPages("xwdt1/xwdt")
	if err != nil || total != 5 {
		t.Fatalf("EES source page count = %d, err=%v; want 5", total, err)
	}
	if _, err := site.ListNewsPage("xwdt1/xwdt", 2); err != nil {
		t.Fatalf("logical page 2: %v", err)
	}
	if _, err := site.ListNewsPage("xwdt1/xwdt", 5); err != nil {
		t.Fatalf("logical page 5: %v", err)
	}
	if paths["/xwdt1/xwdt/4.htm"] == 0 || paths["/xwdt1/xwdt/1.htm"] == 0 {
		t.Fatalf("logical-to-physical page requests = %#v", paths)
	}
	if _, err := eesTotalPages("xwdt1/jszx"); err == nil {
		t.Fatal("hidden homepage-shell route was accepted as a registered feed")
	}
}

func TestEesMixedListPreservesExplicitForwardedExternalArticle(t *testing.T) {
	items, err := parseAdapter_eesList(loadTestHTML("ees_list_external_live.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].URL != "https://www.sceea.cn/Html/202509/Newsdetail_4463.html" || items[0].Date != "2025-10-01" {
		t.Fatalf("forwarded external row = %#v", items)
	}
}

func TestEesRejectsHomepageShellAndStaticListFallback(t *testing.T) {
	if _, err := parseAdapter_eesList(loadTestHTML("ees_shell_live.html"), "https://ees.swjtu.edu.cn"); err == nil {
		t.Fatal("hidden more-route homepage shell was accepted as a complete list")
	}
	if _, err := parseAdapter_eesList([]byte(`<html><head><meta name="pageType" content="2"></head><body><div class="profile">学院概况</div></body></html>`), "https://ees.swjtu.edu.cn"); err == nil {
		t.Fatal("static profile page was accepted as an article list")
	}
}

func TestEesHomepageParsesVisibleAndHiddenArticleModules(t *testing.T) {
	items, err := parseAdapter_eesList(loadTestHTML("ees.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 17 {
		t.Fatalf("got %d homepage items, want 8 news + 7 notices + 2 hidden-panel articles", len(items))
	}
	types := make(map[string]int)
	seen := make(map[string]bool)
	for _, item := range items {
		if seen[item.URL] {
			t.Fatalf("duplicate homepage URL: %s", item.URL)
		}
		seen[item.URL] = true
		types[item.Type]++
		if !strings.HasPrefix(item.URL, "https://ees.swjtu.edu.cn/info/") {
			t.Fatalf("non-EES homepage URL leaked: %+v", item)
		}
		if strings.Contains(item.URL, "/info/1871/") {
			t.Fatalf("banner carousel leaked into homepage feed: %+v", item)
		}
	}
	if types["新闻动态"] != 8 || types["培养"] != 4 || types["综合"] != 3 || types["校企导师"] != 1 || types["卓工学生"] != 1 {
		t.Fatalf("unexpected homepage module types: %#v", types)
	}
	if items[0].Title != "竢实耀百卅 扬华铸未来——庆祝西南交通大学建校130周年大会隆重举行" || items[0].Date != "2026-05-18" {
		t.Fatalf("unexpected first homepage item: %+v", items[0])
	}
	if items[len(items)-1].Title != "国家卓越工程师学院迎来2025级新生" || items[len(items)-1].Type != "卓工学生" {
		t.Fatalf("hidden panel item was not retained: %+v", items[len(items)-1])
	}
}

func TestEesListParsesCanonicalRowsDatesAndType(t *testing.T) {
	items, err := parseAdapter_eesList(loadTestHTML("ees_list.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d list items, want three canonical rows", len(items))
	}
	if items[0].Title != "第一条真实新闻标题" || items[0].Date != "2026-07-26" || items[0].Type != "新闻动态" {
		t.Fatalf("unexpected first row: %+v", items[0])
	}
	if items[1].Date != "2026-07-25" || items[1].PublishedAt != "2026-07-25 14:34:00" {
		t.Fatalf("unexpected minute-precision row: %+v", items[1])
	}
	if items[2].Date != "2026-07-14" {
		t.Fatalf("unexpected final row date: %+v", items[2])
	}
	for _, item := range items {
		if strings.Contains(item.Title, "轮播") || strings.Contains(item.URL, "example.org") {
			t.Fatalf("non-list content leaked: %+v", item)
		}
	}
}

func TestEesPresentEmptyListIsAValidEmptyResult(t *testing.T) {
	items, err := parseAdapter_eesList(loadTestHTML("ees_empty_list.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty list result = %#v, want a non-nil empty slice", items)
	}
}

func TestEesArticleParsesMetadataBodyImageAndAttachment(t *testing.T) {
	article, err := parseAdapter_eesArticle(loadTestHTML("ees_article.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于工程实践的通知" || article.Source != "综合" || article.Type != "通知公告" {
		t.Fatalf("unexpected article metadata: %+v", article)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("unexpected article date: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	if article.Content != "第一段正文。\n\n第二段正文，含一个外部说明链接。说明" {
		t.Fatalf("unexpected article content: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://ees.swjtu.edu.cn/assets/article.jpg" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "工程实践申请表.docx" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("unexpected article attachments: %#v", article.Attachments)
	}
	if strings.Contains(article.Content, "工程实践申请表.docx") || strings.Contains(article.ContentHTML, "<style>") {
		t.Fatal("attachment or style leaked into cleaned article body")
	}
	if !strings.Contains(article.ContentHTML, `src="https://ees.swjtu.edu.cn/assets/article.jpg"`) || !strings.Contains(article.RawHTML, "关于工程实践的通知") {
		t.Fatal("article archive HTML did not retain the source/resource evidence")
	}
}

func TestEesArticleKeepsEmbeddedPDFAsVerifiableAttachment(t *testing.T) {
	article, err := parseAdapter_eesArticle(loadTestHTML("ees_article_pdf.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "正文内嵌PDF" || !strings.HasSuffix(article.Attachments[0].URL, "/document.pdf") {
		t.Fatalf("embedded PDF attachment = %#v", article.Attachments)
	}
}

func TestEesMeasuredArticlesUseFixedMetadataAndBoundary(t *testing.T) {
	article, err := parseAdapter_eesArticle(loadTestHTML("ees_article_live.html"), "https://ees.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "国家卓越工程师学院关于公开招聘研究生助管的通知" || article.Type != "通知公告" || article.Source != "综合" || article.Date != "2026-09-03" || article.PublishedAt != "" {
		t.Fatalf("fixed live metadata = %+v", article)
	}
	if !strings.Contains(article.Content, "报名时间：2026年9月3日-2026年9月10日") || strings.Contains(article.Content, "来源：综合") || strings.Contains(article.Content, "通知公告") {
		t.Fatalf("live content boundary = %q", article.Content)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "《西南交通大学研究生助管岗位申请表》20260903.doc" {
		t.Fatalf("live outside-body attachment = %#v", article.Attachments)
	}
	resourceArticle, err := parseAdapter_eesArticle(loadTestHTML("ees_article_attachment_live.html"), "https://ees.swjtu.edu.cn")
	if err != nil || resourceArticle.Type != "规范文件" || resourceArticle.Date != "2026-04-02" || len(resourceArticle.Attachments) != 1 {
		t.Fatalf("resource detail = article=%#v err=%v", resourceArticle, err)
	}
}

func TestEesResourceValidationRequiresStatusTypeAndMagic(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !eesResourceBodyIsUsable("image/png", png, "good.png", true) {
		t.Fatal("valid PNG was rejected")
	}
	if eesResourceBodyIsUsable("image/png", loadTestHTML("ees_attachment_captcha_live.html"), "bad.png", true) {
		t.Fatal("HTTP 200 CAPTCHA HTML was accepted as an image")
	}
	if !eesResourceBodyIsUsable("application/pdf", []byte("%PDF-1.7 fixture"), "good.pdf", false) {
		t.Fatal("valid PDF was rejected")
	}
	if eesResourceBodyIsUsable("application/pdf", []byte("not a PDF"), "bad.pdf", false) {
		t.Fatal("mismatched PDF magic was accepted")
	}
}

func TestEesFetchArticleFiltersCaptchaResourcesAndKeepsExternalLinks(t *testing.T) {
	page, err := os.ReadFile("testdata/ees_article_resources.html")
	if err != nil {
		t.Fatal(err)
	}
	captcha, err := os.ReadFile("testdata/ees_attachment_captcha_live.html")
	if err != nil {
		t.Fatal(err)
	}
	requests := make(map[string]int)
	baseURL := "https://ees.test"
	site := AllSites["ees"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	oldClient := defaultClient
	site.BaseURL = baseURL
	defaultClient = &http.Client{Transport: eesTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/info/1201/4751.htm":
			return eesTestResponse(r, http.StatusOK, "text/html; charset=utf-8", page), nil
		case "/__local/good.png":
			return eesTestResponse(r, http.StatusOK, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}), nil
		case "/__local/bad.png":
			return eesTestResponse(r, http.StatusOK, "image/png", captcha), nil
		case "/files/good.pdf":
			return eesTestResponse(r, http.StatusOK, "application/pdf", []byte("%PDF-1.7 fixture")), nil
		case "/files/bad.pdf":
			return eesTestResponse(r, http.StatusOK, "application/pdf", []byte("not a PDF")), nil
		case "/system/_content/download.jsp":
			return eesTestResponse(r, http.StatusOK, "text/html; charset=UTF-8", captcha), nil
		default:
			return eesTestResponse(r, http.StatusNotFound, "text/plain", []byte("not found")), nil
		}
	})}
	NoCache = true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL = oldBase
		defaultClient = oldClient
		NoCache = oldNoCache
		ClearCache()
	})

	article, raw, err := eesFetchArticle(baseURL + "/info/1201/4751.htm")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(page) || article.Date != "2026-09-03" || article.PublishedAt != "" {
		t.Fatalf("fetched EES article metadata/archive = article=%+v raw=%d", article, len(raw))
	}
	if len(article.Images) != 1 || article.Images[0] != baseURL+"/__local/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != baseURL+"/files/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if requests["/__local/bad.png"] == 0 || requests["/files/bad.pdf"] == 0 || requests["/system/_content/download.jsp"] == 0 {
		t.Fatalf("invalid resources were not checked: %#v", requests)
	}
	if strings.Contains(article.ContentHTML, "bad.png") || strings.Contains(article.ContentHTML, "bad.pdf") || strings.Contains(article.ContentHTML, "download.jsp") {
		t.Fatalf("invalid resources leaked into ContentHTML: %q", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, "https://external.example/guide") || !strings.Contains(article.ContentHTML, baseURL+"/files/good.pdf") {
		t.Fatalf("verified resource or external ordinary link was lost: %q", article.ContentHTML)
	}
}

type eesTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper eesTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func eesTestResponse(request *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}

func TestEesArticleRejectsStaticAndVerificationPages(t *testing.T) {
	staticPage := []byte(`<html><head><meta name="pageType" content="2"></head><body><div class="text-heading">静态页面</div><div class="v_news_content"><p>不是文章。</p></div></body></html>`)
	if _, err := parseAdapter_eesArticle(staticPage, "https://ees.swjtu.edu.cn"); err == nil {
		t.Fatal("static page was accepted as an article")
	}
	verificationPage := []byte(`<html><head><title>附件下载</title></head><body><h1>请输入验证码下载附件</h1></body></html>`)
	if _, err := parseAdapter_eesArticle(verificationPage, "https://ees.swjtu.edu.cn"); err == nil {
		t.Fatal("verification page was accepted as an article")
	}
}

func parseDocumentForEesTest(t *testing.T, name string) *goquery.Document {
	t.Helper()
	raw := loadTestHTML(name)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
