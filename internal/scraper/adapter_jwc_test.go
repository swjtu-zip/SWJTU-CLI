package scraper

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

func jwcFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestJWCAdapterRegistersAllArticleFeeds(t *testing.T) {
	site := AllSites["jwc"]
	if site == nil || site.BaseURL != jwcCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.ListPageFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("jwc adapter is incomplete: %#v", site)
	}
	if !site.NoIndexFeed {
		t.Fatal("jwc homepage is a portal shell and must not publish an implicit index feed")
	}
	if len(jwcSubPages) != 55 {
		t.Fatalf("got %d jwc feeds, want 55", len(jwcSubPages))
	}
	if !reflect.DeepEqual(site.SubPages, jwcSubPages) {
		t.Fatalf("site sub-pages differ from jwc adapter routes")
	}
	for _, slug := range jwcSubPages {
		if _, ok := jwcFindRoute(slug); !ok {
			t.Errorf("sub-page %q has no route definition", slug)
		}
	}
	for _, excluded := range []string{"rules", "files", "service/login", "CourseAction", "PublicInfoQueryAction", "jiaowu.swjtu.edu.cn"} {
		if containsJWCFeed(jwcSubPages, excluded) {
			t.Errorf("non-feed %q was registered as a jwc feed", excluded)
		}
	}
}

func containsJWCFeed(feeds []string, want string) bool {
	for _, feed := range feeds {
		if feed == want || strings.Contains(feed, want) {
			return true
		}
	}
	return false
}

func TestJWCIndexFeedIsRefusedAsUnregisteredRoute(t *testing.T) {
	if _, err := jwcListURL(jwcCanonicalBaseURL, "index", 1, ""); err == nil {
		t.Fatal("index slug unexpectedly has a VATUU list URL")
	}
	fetcher := func(rawURL string) ([]byte, error) {
		t.Fatalf("index route must be refused before any fetch, got %s", rawURL)
		return nil, fmt.Errorf("unexpected fetch %s", rawURL)
	}
	if items, _, err := jwcListPageWithFetcher(jwcCanonicalBaseURL, "index", 1, fetcher); err == nil || items != nil {
		t.Fatalf("index list page = items=%#v err=%v; want refusal", items, err)
	}
	// The refreshed live homepage fixture is still a portal shell: category
	// links and system functions, but no article rows of its own.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(jwcFixture(t, "jwc_live_home.html"))))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Find("a[href*='setAction=newsDetail']").Length() != 0 {
		t.Fatal("live homepage exposes article rows; NoIndexFeed needs re-evaluation")
	}
	if doc.Find("a[href*='setAction=newsList']").Length() == 0 {
		t.Fatal("live homepage lost its category navigation links")
	}
}

func TestJWCListURLUsesTheObservedVATUURouteKinds(t *testing.T) {
	tests := []struct {
		slug       string
		page       int
		pageParam  string
		wantBig    string
		wantSmall  string
		wantSelect string
	}{
		{slug: "all", page: 1},
		{slug: "news", page: 1, wantBig: "FB2D32BEAD066637", wantSelect: "bigType"},
		{slug: "quality/teacher-development", page: 2, pageParam: "pageNum", wantSmall: "B4F513430B5EBC91", wantSelect: "smallType"},
	}
	for _, tc := range tests {
		raw, err := jwcListURL(jwcCanonicalBaseURL, tc.slug, tc.page, tc.pageParam)
		if err != nil {
			t.Fatalf("%s: %v", tc.slug, err)
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if query.Get("setAction") != "newsList" || parsed.Path != "/vatuu/WebAction" {
			t.Errorf("%s URL = %s", tc.slug, raw)
		}
		if query.Get("bigTypeId") != tc.wantBig || query.Get("keyWord") != tc.wantSmall || query.Get("selectType") != tc.wantSelect {
			t.Errorf("%s query = %#v", tc.slug, query)
		}
		if tc.page == 1 && (query.Get("pageNum") != "" || query.Get("currentPage") != "") {
			t.Errorf("page one unexpectedly has a page parameter: %s", raw)
		}
		if tc.page > 1 && query.Get(tc.pageParam) != "2" {
			t.Errorf("page %d URL = %s", tc.page, raw)
		}
	}
	if _, err := jwcListURL(jwcCanonicalBaseURL, "management-rules", 1, ""); err == nil {
		t.Fatal("unknown/static route unexpectedly accepted")
	}
}

func TestJWCListParserKeepsCompleteRowsAndPrecision(t *testing.T) {
	items, err := parseAdapter_jwcList(jwcFixture(t, "jwc_list.html"), jwcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want 2: %#v", len(items), items)
	}
	if items[0].Title != "关于做好2026-2027学年第一学期选课工作的通知（完整列表标题）" || items[0].Date != "2026-07-02" || items[0].PublishedAt != "2026-07-02 08:45:10" || items[0].Type != "排课选课事务" {
		t.Errorf("first item = %#v", items[0])
	}
	if items[1].Title != "2025—2026学年第二学期期末考试情况通报（一）完整标题" || items[1].Date != "2026-06-15" || items[1].PublishedAt != "2026-06-15 17:35:18" || items[1].Type != "要闻及动态" {
		t.Errorf("second item = %#v", items[1])
	}
	for _, item := range items {
		if strings.Contains(item.URL, "news.swjtu.edu.cn") || strings.Contains(item.URL, "setAction=download") {
			t.Errorf("non-local article/resource leaked into list: %#v", item)
		}
	}
	if got := jwcPageParamFromHTML(jwcFixture(t, "jwc_list.html"), 2); got != "pageNum" {
		t.Fatalf("detected page parameter = %q, want pageNum", got)
	}
}

func TestJWCPaginationAndLegalEmptyList(t *testing.T) {
	info, err := jwcPaginationFromHTML(jwcFixture(t, "jwc_list.html"))
	if err != nil {
		t.Fatal(err)
	}
	if info.totalItems != 123 || info.current != 2 || info.perPage != 50 || info.totalPages != 3 {
		t.Fatalf("pagination = %#v", info)
	}
	empty, err := parseAdapter_jwcList(jwcFixture(t, "jwc_empty.html"), jwcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("legal empty list = %#v", empty)
	}
	emptyInfo, err := jwcPaginationFromHTML(jwcFixture(t, "jwc_empty.html"))
	if err != nil || emptyInfo.totalPages != 1 {
		t.Fatalf("empty pagination = %#v, err=%v", emptyInfo, err)
	}
}

func TestJWCRejectsHomepageStaticErrorAndLoginShells(t *testing.T) {
	for _, fixture := range []string{"jwc.html", "jwc_static.html", "jwc_error.html", "jwc_login.html"} {
		items, err := parseAdapter_jwcList(jwcFixture(t, fixture), jwcCanonicalBaseURL)
		if err == nil || items != nil {
			t.Errorf("%s: items=%#v err=%v; shell/error must not be success", fixture, items, err)
		}
	}
	if _, err := parseAdapter_jwcArticle(jwcFixture(t, "jwc_error.html"), jwcCanonicalBaseURL); err == nil {
		t.Fatal("error page accepted as article")
	}
	if _, _, err := jwcFetchArticle("https://news.swjtu.edu.cn/info/1011/108875.htm"); err == nil {
		t.Fatal("external article redirect/URL accepted")
	}
}

func TestJWCArticleExtractsMetadataBodyImagesAndOnlyFileLinks(t *testing.T) {
	article, err := parseAdapter_jwcArticle(jwcFixture(t, "jwc_article.html"), jwcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于做好2026-2027学年第一学期选课工作的通知" || article.Source != "本科生院" || article.Type != "排课选课事务" || article.Date != "2026-07-02" || article.PublishedAt != "2026-07-02 08:45:10" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "现将2026-2027学年第一学期选课工作的安排通知如下") || !strings.Contains(article.Content, "选课") {
		t.Fatalf("article body lost text/table content: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != jwcCanonicalBaseURL+"/images/1782952979192063301.jpg" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	want := map[string]string{
		"附件一：移动端选课使用操作手册.pdf": jwcCanonicalBaseURL + "/vatuu/WebAction?fileId=PDF-FIXTURE&fileType=file&setAction=download",
		"选课指南.docx": jwcCanonicalBaseURL + "/files/selection-guide.docx",
	}
	for _, attachment := range article.Attachments {
		if want[attachment.Name] != attachment.URL {
			t.Errorf("unexpected attachment = %#v", attachment)
		}
		delete(want, attachment.Name)
	}
	if len(want) != 0 {
		t.Errorf("missing attachments = %#v", want)
	}
	for _, attachment := range article.Attachments {
		if strings.Contains(attachment.URL, "yhxt.swjtu.edu.cn") || strings.Contains(attachment.URL, "error.html") {
			t.Errorf("non-file HTML/external body link was retained as an attachment: %#v", attachment)
		}
	}
	if !strings.Contains(article.ContentHTML, "fileId=PDF-FIXTURE") {
		t.Errorf("fileType=file download link was removed from content HTML: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, "yhxt.swjtu.edu.cn") || strings.Contains(article.ContentHTML, "href=\"https://jwc.swjtu.edu.cn/vatuu/old/error.html\"") {
		t.Errorf("external link was not preserved or local HTML shell was retained: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, `src="https://jwc.swjtu.edu.cn/images/1782952979192063301.jpg"`) {
		t.Errorf("image URL was not resolved in content HTML: %s", article.ContentHTML)
	}
}

func TestJWCAttachmentResponseRejectsHTMLAndVerificationShells(t *testing.T) {
	valid := []struct {
		contentType string
		body        []byte
	}{
		{contentType: "application/pdf", body: []byte("%PDF-1.7\nfixture")},
		{contentType: "application/octet-stream", body: []byte{0x50, 0x4b, 0x03, 0x04, 0x01}},
		{contentType: "text/plain", body: []byte("这是一个真实的文本附件")},
	}
	for _, tc := range valid {
		if !jwcAttachmentBodyIsUsable(tc.contentType, tc.body) {
			t.Errorf("valid attachment rejected: content_type=%q body=%q", tc.contentType, tc.body)
		}
	}
	invalid := []struct {
		contentType string
		body        []byte
	}{
		{contentType: "text/html; charset=utf-8", body: []byte("<html><body>403</body></html>")},
		{contentType: "application/octet-stream", body: []byte("<!doctype html><h1>请输入验证码下载附件</h1>")},
		{contentType: "application/json", body: []byte(`{"error":"not authorized"}`)},
		{contentType: "application/pdf", body: []byte("<html><title>Cloudflare</title></html>")},
		{contentType: "application/pdf", body: []byte("not a PDF")},
		{contentType: "application/octet-stream", body: []byte("arbitrary response")},
		{contentType: "image/png", body: []byte("not a PNG")},
		{contentType: "", body: []byte("unknown content")},
		{contentType: "application/pdf", body: nil},
	}
	for _, tc := range invalid {
		if jwcAttachmentBodyIsUsable(tc.contentType, tc.body) {
			t.Errorf("error shell accepted: content_type=%q body=%q", tc.contentType, tc.body)
		}
	}
}

func TestJWCLiveListFixturesParseRowsAndRealJumpPage(t *testing.T) {
	tests := []struct {
		fixture       string
		current       int
		wantItems     int
		wantTitle     string
		wantDate      string
		wantPublished string
	}{
		{fixture: "jwc_live_news_first.html", current: 1, wantItems: 50, wantTitle: "校领导巡视期末考试工作", wantDate: "2026-06-30", wantPublished: "2026-06-30 08:45:47"},
		{fixture: "jwc_live_news_middle.html", current: 14, wantItems: 50, wantTitle: "【科研训练营第一讲公告】再现港珠澳大桥建设幕后的故事", wantDate: "2018-11-22", wantPublished: "2018-11-22 18:12:06"},
		{fixture: "jwc_live_news_last.html", current: 28, wantItems: 43, wantTitle: "陈春阳校长等校领导巡视期末考试", wantDate: "2009-01-13", wantPublished: "2009-01-13 12:06:40"},
	}
	for _, tc := range tests {
		raw := jwcFixture(t, tc.fixture)
		info, err := jwcPaginationFromHTML(raw)
		if err != nil {
			t.Fatalf("%s pagination: %v", tc.fixture, err)
		}
		if info.current != tc.current || info.totalItems != 1393 || info.totalPages != 28 || info.perPage != 50 {
			t.Fatalf("%s pagination = %#v", tc.fixture, info)
		}
		items, err := parseAdapter_jwcList(raw, jwcCanonicalBaseURL)
		if err != nil {
			t.Fatalf("%s parse: %v", tc.fixture, err)
		}
		if len(items) != tc.wantItems {
			t.Fatalf("%s parsed %d items, want %d", tc.fixture, len(items), tc.wantItems)
		}
		if items[0].Title != tc.wantTitle || items[0].Date != tc.wantDate || items[0].PublishedAt != tc.wantPublished || items[0].Type == "" {
			t.Fatalf("%s first item = %#v", tc.fixture, items[0])
		}
	}
	if got := jwcPageParamFromHTML(jwcFixture(t, "jwc_live_news_first.html"), 2); got != "jumpPage" {
		t.Fatalf("live page parameter = %q, want jumpPage", got)
	}
}

func TestJWCLiveHomeAndEmptyFeedAreClassifiedCorrectly(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(jwcFixture(t, "jwc_live_home.html"))))
	if err != nil {
		t.Fatal(err)
	}
	homeText := jwcCleanText(doc.Text())
	for _, label := range []string{"网站首页", "机构设置", "教学动态", "教务管理", "教学运行", "教改研究", "实践教学", "质量保障", "管理规定", "表格文档", "网上办事", "新闻通知", "教学相关"} {
		if !strings.Contains(homeText, label) {
			t.Errorf("live homepage is missing audited navigation/module %q", label)
		}
	}
	for _, hrefPart := range []string{"CourseAction", "TestInfoAction", "PublicInfoQueryAction", "ruleList", "fileList"} {
		if doc.Find("a[href*='"+hrefPart+"']").Length() == 0 {
			t.Errorf("live homepage is missing expected non-article link %q", hrefPart)
		}
	}
	if _, err := jwcPaginationFromHTML(jwcFixture(t, "jwc_live_home.html")); err == nil {
		t.Fatal("homepage shell was accepted as an article list")
	}

	empty, err := parseAdapter_jwcList(jwcFixture(t, "jwc_live_empty_virtual.html"), jwcCanonicalBaseURL)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("live empty feed = items=%#v err=%v", empty, err)
	}
	info, err := jwcPaginationFromHTML(jwcFixture(t, "jwc_live_empty_virtual.html"))
	if err != nil || info.totalItems != 0 || info.totalPages != 1 {
		t.Fatalf("live empty feed pagination = %#v err=%v", info, err)
	}
}

func TestJWCListPageChecksFirstMiddleLastAndOutOfRange(t *testing.T) {
	pages := map[int][]byte{
		1:  jwcFixture(t, "jwc_live_news_first.html"),
		14: jwcFixture(t, "jwc_live_news_middle.html"),
		28: jwcFixture(t, "jwc_live_news_last.html"),
	}
	fetcher := func(rawURL string) ([]byte, error) {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return nil, err
		}
		page := 1
		if value := parsed.Query().Get("jumpPage"); value != "" {
			if _, scanErr := fmt.Sscanf(value, "%d", &page); scanErr != nil {
				return nil, scanErr
			}
		}
		if body := pages[page]; body != nil {
			return body, nil
		}
		return nil, fmt.Errorf("fixture page %d not found", page)
	}
	for _, page := range []int{1, 14, 28} {
		items, totalPages, err := jwcListPageWithFetcher("https://jwc.swjtu.edu.cn", "news", page, fetcher)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if totalPages != 28 || len(items) == 0 {
			t.Fatalf("page %d: items=%d totalPages=%d", page, len(items), totalPages)
		}
	}
	for _, page := range []int{0, 29} {
		items, totalPages, err := jwcListPageWithFetcher("https://jwc.swjtu.edu.cn", "news", page, fetcher)
		if err == nil || items != nil || totalPages != 28 {
			t.Fatalf("out-of-range page %d: items=%#v totalPages=%d err=%v", page, items, totalPages, err)
		}
	}
}

func TestJWCLiveDetailUsesFixedMetadataAndBodyContainer(t *testing.T) {
	article, err := parseAdapter_jwcArticle(jwcFixture(t, "jwc_live_article_maintenance.html"), jwcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于系统维护期间访问受限的通知" || article.Source != "本科生院" || article.Type != "最新信息发布" || article.Date != "2026-08-10" || article.PublishedAt != "2026-08-10 16:16:26" {
		t.Fatalf("live article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "系统将于2026年8月10日16点至14日18点进行维护") || strings.Contains(article.Content, "最新更新") {
		t.Fatalf("live article body was not limited to .content-main: %q", article.Content)
	}
	if !strings.Contains(article.ContentHTML, "数据只能查询") {
		t.Fatalf("live article HTML lost body: %q", article.ContentHTML)
	}

	minor, err := parseAdapter_jwcArticle(jwcFixture(t, "jwc_live_article_minor.html"), jwcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if minor.Date != "2026-04-08" || minor.PublishedAt != "2026-04-08 09:03:09" || minor.Type != "最新信息发布" || len(minor.Attachments) != 3 {
		t.Fatalf("minor article metadata/resources = %#v", minor)
	}
	if !strings.Contains(minor.ContentHTML, "jiaowu.swjtu.edu.cn") {
		t.Fatalf("ordinary external teaching-service link was removed: %s", minor.ContentHTML)
	}
}

func TestJWCDateOnlyAndPermissionFixtures(t *testing.T) {
	article, err := parseAdapter_jwcArticle(jwcFixture(t, "jwc_date_only.html"), jwcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-06-29" || article.PublishedAt != "" {
		t.Fatalf("date-only metadata invented a time: %#v", article)
	}
	if _, err := parseAdapter_jwcArticle(jwcFixture(t, "jwc_live_permission.html"), jwcCanonicalBaseURL); err == nil {
		t.Fatal("permission shell accepted as article")
	}
}

func TestJWCResourceVerificationChecksStatusContentTypeAndMagic(t *testing.T) {
	oldClient := defaultClient
	defaultClient = &http.Client{Transport: jwcResourceRoundTripper(func(r *http.Request) (*http.Response, error) {
		response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}
		switch r.URL.Path {
		case "/good.pdf":
			response.Header.Set("Content-Type", "application/pdf")
			response.Body = io.NopCloser(strings.NewReader("%PDF-1.7 fixture"))
		case "/good.png":
			response.Header.Set("Content-Type", "image/png")
			response.Body = io.NopCloser(strings.NewReader(string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})))
		case "/html.pdf":
			response.Header.Set("Content-Type", "application/pdf")
			response.Body = io.NopCloser(strings.NewReader("<html>captcha</html>"))
		case "/bad.png":
			response.Header.Set("Content-Type", "image/png")
			response.Body = io.NopCloser(strings.NewReader("not an image"))
		case "/wrong-type.pdf":
			response.Header.Set("Content-Type", "text/html")
			response.Body = io.NopCloser(strings.NewReader("%PDF-1.7"))
		case "/missing.pdf":
			response.StatusCode = http.StatusNotFound
		default:
			response.StatusCode = http.StatusNotFound
		}
		return response, nil
	})}
	defer func() { defaultClient = oldClient }()
	baseURL := jwcCanonicalBaseURL

	if body, contentType, err := jwcFetchResource(baseURL+"/good.png", baseURL, true); err != nil || contentType != "image/png" || len(body) == 0 {
		t.Fatalf("valid image fetch = len=%d type=%q err=%v", len(body), contentType, err)
	}
	for _, path := range []string{"/html.pdf", "/bad.png", "/wrong-type.pdf", "/missing.pdf"} {
		if body, _, err := jwcFetchResource(baseURL+path, baseURL, strings.HasSuffix(path, ".png")); err == nil || body != nil {
			t.Errorf("invalid resource %s accepted: len=%d err=%v", path, len(body), err)
		}
	}
	images := jwcVerifyImages([]string{baseURL + "/good.png", baseURL + "/bad.png"}, baseURL)
	if len(images) != 1 || images[0] != baseURL+"/good.png" {
		t.Fatalf("verified images = %#v", images)
	}
	attachments := jwcVerifyAttachmentsAt([]model.Attachment{
		{Name: "good.pdf", URL: baseURL + "/good.pdf"},
		{Name: "html.pdf", URL: baseURL + "/html.pdf"},
	}, baseURL)
	if len(attachments) != 1 || attachments[0].Name != "good.pdf" || attachments[0].Size != "16" {
		t.Fatalf("verified attachments = %#v", attachments)
	}
}

type jwcResourceRoundTripper func(*http.Request) (*http.Response, error)

func (f jwcResourceRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
