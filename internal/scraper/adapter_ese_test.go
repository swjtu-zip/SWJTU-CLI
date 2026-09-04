package scraper

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

const eseTestBaseURL = "https://ese.swjtu.edu.cn"

type eseRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper eseRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func loadESEFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestESEAdapterRegistrationAndReversePagination(t *testing.T) {
	site := AllSites["ese"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("ESE adapter hooks are incomplete")
	}
	wantSubPages := []string{
		"index/xwgg/zyxw",
		"index/xwgg/hjyw",
		"index/xwgg/tzgg1/qb",
		"index/xwgg/tzgg1/dzbg",
		"index/xwgg/tzgg1/bks",
		"index/xwgg/tzgg1/yjs",
		"index/xwgg/tzgg1/kygz",
		"index/xwgg/tzgg1/xsgz",
		"index/xwgg/tzgg1/qt",
		"djgz/djdt",
		"djgz/jwgz",
		"djgz/ghgz",
		"kxyj/xsdt",
		"xsgz/tzjgg",
		"xsgz/txdt",
		"xsgz/xsfc",
		"xywh/hjsg",
		"xyyd/xyqy",
		"index/cyxz",
		"jyjx/jxcg/jxcgj",
		"jyjx/rcpy/yjspy",
		"jyjx/jypt/syjx/bksyjxxm",
		"jyjx/jypt/syjx/yjssjjd",
		"jyjx/jypt/syjx/syjxcyxz",
		"jyjx/jypt/syjx/cxsyxm",
		"jyjx/jypt/sysaq/sysaqgd",
		"jyjx/jypt/sysaq/aqjsal",
		"jyjx/rcpy/bkspy",
		"kxyj/kypt",
		"kxyj/kytd",
		"szdw/wjxz",
		"xsgz/wjxz",
		"xywh/whhd",
		"xywh/whly",
		"xyyd/xyfc",
		"xyyd/xyhbqysy",
		"cpyc/szzp",
		"cpyc/zzbshzp",
	}
	if len(site.SubPages) != len(wantSubPages) {
		t.Fatalf("ESE sub-pages = %#v, want %#v", site.SubPages, wantSubPages)
	}
	for index, want := range wantSubPages {
		if site.SubPages[index] != want {
			t.Errorf("ESE sub-page %d = %q, want %q", index, site.SubPages[index], want)
		}
	}

	cases := []struct {
		name  string
		slug  string
		page  int
		total int
		want  string
	}{
		{name: "first", slug: "index/xwgg/hjyw", page: 1, total: 38, want: eseTestBaseURL + "/index/xwgg/hjyw.htm"},
		{name: "second", slug: "index/xwgg/hjyw", page: 2, total: 38, want: eseTestBaseURL + "/index/xwgg/hjyw/37.htm"},
		{name: "middle", slug: "index/xwgg/hjyw", page: 19, total: 38, want: eseTestBaseURL + "/index/xwgg/hjyw/20.htm"},
		{name: "last", slug: "index/xwgg/hjyw", page: 38, total: 38, want: eseTestBaseURL + "/index/xwgg/hjyw/1.htm"},
		{name: "important-middle", slug: "index/xwgg/zyxw", page: 2, total: 3, want: eseTestBaseURL + "/index/xwgg/zyxw/2.htm"},
		{name: "important-last", slug: "index/xwgg/zyxw", page: 3, total: 3, want: eseTestBaseURL + "/index/xwgg/zyxw/1.htm"},
		{name: "graduate-first", slug: "jyjx/rcpy/yjspy", page: 1, total: 1, want: eseTestBaseURL + "/jyjx/rcpy/yjspy.htm"},
		{name: "graduate-out-of-range", slug: "jyjx/rcpy/yjspy", page: 2, total: 1, want: ""},
		{name: "safety-first", slug: "jyjx/jypt/sysaq/aqjsal", page: 1, total: 1, want: eseTestBaseURL + "/jyjx/jypt/sysaq/aqjsal.htm"},
		{name: "safety-out-of-range", slug: "jyjx/jypt/sysaq/aqjsal", page: 2, total: 1, want: ""},
		{name: "unknown-total", slug: "index/xwgg/hjyw", page: 2, total: 0, want: ""},
		{name: "one-page-out-of-range", slug: "djgz/ghgz", page: 2, total: 1, want: ""},
		{name: "past-last-page", slug: "index/xwgg/hjyw", page: 39, total: 38, want: ""},
		{name: "non-positive-page", slug: "index/xwgg/hjyw", page: 0, total: 38, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := esePageURL(eseTestBaseURL, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
				t.Fatalf("esePageURL(%q, %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
			}
		})
	}
}

func TestESEPageCountAcceptsRecognizedEmptyAndXYHBLists(t *testing.T) {
	xyhb := []byte(`<html><body><div class="ny-right"><div class="xyhb"><ul>
<li><a href="/info/1771/10601.htm"><p>安徽合肥</p></a></li>
</ul><div class="pagination"><span class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="xyhbqysy/1.htm">2</a></span></span></div></div></div></body></html>`)
	if got := eseTotalPagesFromHTML(xyhb); got != 2 {
		t.Fatalf("xyhb ESE total = %d, want 2", got)
	}

	empty := []byte(`<html><body><div class="ny-right"><div class="list"><ul></ul></div>
<div class="p_pages"><span class="p_no_d">1</span></div></div></body></html>`)
	if got := eseTotalPagesFromHTML(empty); got != 1 {
		t.Fatalf("empty ESE total = %d, want 1", got)
	}

	if got := eseTotalPagesFromHTML([]byte(`<html><body><div class="banner">静态页面</div></body></html>`)); got != 0 {
		t.Fatalf("static ESE total = %d, want 0", got)
	}
}

func TestESEListParserSupportsXYHBRegion(t *testing.T) {
	html := []byte(`<html><head><meta name="pageType" content="2"></head><body>
<div class="ny-right">
  <div class="position"><h3>校友环保企业索引</h3></div>
  <div class="xyhb"><ul>
    <li><a href="/info/1771/10601.htm" target="_blank"><p>安徽合肥</p></a></li>
    <li><a href="/info/1771/10591.htm" target="_blank"><p>北京</p></a></li>
  </ul><div class="pagination"><span class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="xyhbqysy/1.htm">2</a></span></span></div></div>
</div></body></html>`)
	items, err := parseAdapter_eseList(html, eseTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d xyhb items, want 2: %#v", len(items), items)
	}
	if items[0].Title != "安徽合肥" || items[0].URL != eseTestBaseURL+"/info/1771/10601.htm" || items[0].Type != "校友环保企业索引" {
		t.Fatalf("unexpected first xyhb item: %#v", items[0])
	}
	if items[1].Title != "北京" || items[1].URL != eseTestBaseURL+"/info/1771/10591.htm" {
		t.Fatalf("unexpected second xyhb item: %#v", items[1])
	}
}

func TestESEHomeParserKeepsHiddenNoticeTabsAndFiltersPlaceholders(t *testing.T) {
	items, err := parseAdapter_eseList(loadESEFixture(t, "ese_adapter_home.html"), eseTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 13 {
		t.Fatalf("got %d home items, want 13 unique articles: %#v", len(items), items)
	}

	want := map[string]struct {
		title string
		date  string
		typ   string
	}{
		"/info/1991/19591.htm": {title: "习近平给四所交通大学全体师生回信", date: "2026-04-07", typ: "环境要闻"},
		"/info/1991/21591.htm": {title: "这是一个超过一百字节但仍然是有效中文环境要闻标题用于回归长度边界解析", date: "2026-07-02", typ: "环境要闻"},
		"/info/1961/21781.htm": {title: "全部 tab 的真实通知", date: "2026-08-08", typ: "全部"},
		"/info/1921/18091.htm": {title: "党政办公 tab 的真实通知", date: "2026-01-19", typ: "党政办公"},
		"/info/1931/21671.htm": {title: "本科生 tab 的真实通知", date: "2026-07-10", typ: "本科生"},
		"/info/1941/21681.htm": {title: "研究生 tab 的真实通知", date: "2026-07-10", typ: "研究生"},
		"/info/1951/20601.htm": {title: "科研工作 tab 的真实通知", date: "2026-05-15", typ: "科研工作"},
		"/info/1961/21501.htm": {title: "学生工作 tab 的真实通知", date: "2026-06-26", typ: "学生工作"},
		"/info/1971/10781.htm": {title: "其他 tab 的真实通知", date: "2025-11-17", typ: "其他"},
		"/info/1077/20601.htm": {title: "首页学术动态文章", date: "2026-05-15", typ: "学术动态"},
		"/info/1077/20461.htm": {title: "首页第二条学术动态", date: "2026-05-08", typ: "学术动态"},
		"/info/1080/21891.htm": {title: "首页团学动态文章", date: "2026-08-12", typ: "团学动态"},
		"/info/1089/20661.htm": {title: "环境拾光-第2期", date: "", typ: "学院文化"},
	}
	for _, item := range items {
		key := strings.TrimPrefix(item.URL, eseTestBaseURL)
		expected, ok := want[key]
		if !ok {
			t.Errorf("unexpected home article: %+v", item)
			continue
		}
		if item.Title != expected.title || item.Date != expected.date || item.Type != expected.typ {
			t.Errorf("home item = %+v, want title=%q date=%q type=%q", item, expected.title, expected.date, expected.typ)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("home articles missing: %#v", want)
	}
}

func TestESEListParserExtractsAllRowsAndPreciseDate(t *testing.T) {
	items, err := parseAdapter_eseList(loadESEFixture(t, "ese_adapter_list.html"), eseTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want 2: %#v", len(items), items)
	}
	if items[0].Title != "列表第一条真实文章" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31 14:34:27" || items[0].Type != "通知及公告" {
		t.Fatalf("unexpected first list item: %+v", items[0])
	}
	if items[1].Title != "列表第二条真实文章" || items[1].Date != "2026-08-30" || items[1].PublishedAt != "" {
		t.Fatalf("unexpected second list item: %+v", items[1])
	}
}

func TestESENewlyAuditedListFixtures(t *testing.T) {
	cases := []struct {
		name          string
		fixture       string
		wantTotal     int
		wantCount     int
		wantType      string
		wantFirstDate string
		wantPublished string
	}{
		{
			name:          "important news",
			fixture:       "ese_adapter_important.html",
			wantTotal:     3,
			wantCount:     8,
			wantType:      "重要新闻",
			wantFirstDate: "2026-04-07",
			wantPublished: "2026-04-07 19:30:29",
		},
		{
			name:          "graduate training",
			fixture:       "ese_adapter_graduate.html",
			wantTotal:     1,
			wantCount:     5,
			wantType:      "研究生培养",
			wantFirstDate: "2026-05-19",
			wantPublished: "2026-05-19 11:15:34",
		},
		{
			name:          "empty graduate practice base",
			fixture:       "ese_adapter_empty_practice.html",
			wantTotal:     1,
			wantCount:     0,
			wantType:      "",
			wantFirstDate: "",
			wantPublished: "",
		},
		{
			name:          "laboratory downloads",
			fixture:       "ese_adapter_lab_downloads.html",
			wantTotal:     1,
			wantCount:     3,
			wantType:      "实验教学常用下载",
			wantFirstDate: "2025-12-17",
			wantPublished: "",
		},
		{
			name:          "innovative projects",
			fixture:       "ese_adapter_innovative_projects.html",
			wantTotal:     1,
			wantCount:     4,
			wantType:      "创新实验项目",
			wantFirstDate: "2025-12-22",
			wantPublished: "",
		},
		{
			name:          "safety cases",
			fixture:       "ese_adapter_safety_cases.html",
			wantTotal:     1,
			wantCount:     3,
			wantType:      "安全警示案例",
			wantFirstDate: "2025-12-22",
			wantPublished: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := loadESEFixture(t, tc.fixture)
			if got := eseTotalPagesFromHTML(raw); got != tc.wantTotal {
				t.Fatalf("fixture total pages = %d, want %d", got, tc.wantTotal)
			}
			items, err := parseAdapter_eseList(raw, eseTestBaseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.wantCount {
				t.Fatalf("got %d items, want %d: %#v", len(items), tc.wantCount, items)
			}
			if tc.wantCount == 0 {
				return
			}
			if items[0].Type != tc.wantType || items[0].Date != tc.wantFirstDate || items[0].PublishedAt != tc.wantPublished {
				t.Fatalf("first item metadata = %+v, want type=%q date=%q published_at=%q", items[0], tc.wantType, tc.wantFirstDate, tc.wantPublished)
			}
		})
	}
}

func TestESEArticleParserExtractsMetadataBodyImagesAndAttachment(t *testing.T) {
	article, err := parseAdapter_eseArticle(loadESEFixture(t, "ese_adapter_article.html"), eseTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "【2027届推免2】关于发布环境科学与工程学院2027届推免工作实施细则的通知" || article.Type != "本科生" {
		t.Fatalf("unexpected article title/type: %q / %q", article.Title, article.Type)
	}
	if article.Author != "余嘉希" || article.Date != "2026-07-10" || article.PublishedAt != "2026-07-10 09:36:33" {
		t.Fatalf("unexpected article metadata: author=%q date=%q published_at=%q", article.Author, article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "第一段正文") || !strings.Contains(article.Content, "第二段正文") || strings.Contains(article.Content, "附件材料.pdf") {
		t.Fatalf("article body is incomplete or attachment leaked into body: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != eseTestBaseURL+"/__local/article.png" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件材料.pdf" || article.Attachments[0].URL != eseTestBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1&wbfileid=ESEFIX" {
		t.Fatalf("unexpected article attachments: %#v", article.Attachments)
	}
	for _, attachment := range article.Attachments {
		if strings.Contains(attachment.URL, "mp.weixin.qq.com") {
			t.Fatal("external body link was incorrectly classified as an attachment")
		}
	}
	if !strings.Contains(article.ContentHTML, "https://mp.weixin.qq.com") {
		t.Fatal("external body link was lost from article content")
	}
	if !strings.Contains(article.ContentHTML, `src="https://ese.swjtu.edu.cn/__local/article.png"`) {
		t.Fatalf("content HTML did not retain resolved image: %s", article.ContentHTML)
	}
}

func TestESEPDFArticleParserExtractsViewerResources(t *testing.T) {
	article, err := parseAdapter_eseArticle(loadESEFixture(t, "ese_adapter_pdf_article.html"), eseTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "学术动态" || article.Date != "2026-05-15" || article.PublishedAt != "2026-05-15 10:17:15" {
		t.Fatalf("unexpected PDF article metadata: %+v", article)
	}
	if len(article.Images) != 2 || article.Images[0] != eseTestBaseURL+"/__local/page-1.jpg" || article.Images[1] != eseTestBaseURL+"/__local/page-2.png" {
		t.Fatalf("PDF page images were not extracted: %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "report.pdf" || article.Attachments[0].URL != eseTestBaseURL+"/__local/report.pdf" {
		t.Fatalf("PDF viewer attachment was not extracted: %#v", article.Attachments)
	}
}

func TestESEArticleUsesFixedPublicationMetadataAndCompleteBody(t *testing.T) {
	article, err := parseAdapter_eseArticle(loadESEFixture(t, "ese_adapter_audit_detail.html"), eseTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于开展环境学院年度工作安排的通知" || article.Type != "通知公告" {
		t.Fatalf("unexpected audit article title/type: %q / %q", article.Title, article.Type)
	}
	if article.Source != "环境学院" || article.Author != "审计测试" || article.Date != "2025-12-30" || article.PublishedAt != "2025-12-30 16:50:06" {
		t.Fatalf("unexpected audit article metadata: %+v", article)
	}
	for _, fragment := range []string{"本文正文说明年度工作安排和办理要求", "活动时间：2026年1月13日", "第一项办理事项", "第二项办理事项", "材料提交", "2026-01-08"} {
		if !strings.Contains(article.Content, fragment) {
			t.Fatalf("article body lost %q: %q", fragment, article.Content)
		}
	}
	if strings.Contains(article.Date, "2026") || strings.Contains(article.PublishedAt, "2026") {
		t.Fatalf("body activity date polluted publication metadata: %+v", article)
	}
	if len(article.Images) != 1 || article.Images[0] != eseTestBaseURL+"/__local/audit-valid.png" {
		t.Fatalf("unexpected audit article images: %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "年度安排.pdf" || article.Attachments[0].URL != eseTestBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1&wbfileid=ESEAUDIT" {
		t.Fatalf("unexpected audit article attachments: %#v", article.Attachments)
	}
	for _, fragment := range []string{"<table", "材料提交", "https://mp.weixin.qq.com/s/audit", `src="https://ese.swjtu.edu.cn/__local/audit-valid.png"`} {
		if !strings.Contains(article.ContentHTML, fragment) {
			t.Fatalf("content HTML lost %q: %s", fragment, article.ContentHTML)
		}
	}
	for _, fragment := range []string{"blank.gif", "javascript:void", "伪图片"} {
		if strings.Contains(article.ContentHTML, fragment) {
			t.Fatalf("invalid resource leaked into content HTML: %q in %s", fragment, article.ContentHTML)
		}
	}
}

func TestESEResourceVerificationChecksHTTPStatusTypeAndMagic(t *testing.T) {
	serverURL := "http://ese.test"
	originalTransport := defaultClient.Transport
	defaultClient.Transport = eseRoundTripper(func(request *http.Request) (*http.Response, error) {
		var statusCode = http.StatusOK
		contentType := "application/octet-stream"
		var body []byte
		switch request.URL.Path {
		case "/valid.png":
			contentType = "image/png"
			body = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
		case "/fake.png":
			contentType = "image/png"
			body = []byte("<!doctype html><h1>验证码下载附件</h1>")
		case "/valid.pdf":
			contentType = "application/pdf; charset=binary"
			body = []byte("%PDF-1.7\n")
		case "/fake.pdf":
			contentType = "application/pdf"
			body = []byte("<html><body>登录页面</body></html>")
		case "/valid.docx":
			contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
			body = []byte{'P', 'K', 0x03, 0x04, 'E', 'S', 'E'}
		default:
			statusCode = http.StatusNotFound
			contentType = "text/html"
			body = []byte("not found")
		}
		return &http.Response{
			StatusCode: statusCode,
			Status:     http.StatusText(statusCode),
			Header:     http.Header{"Content-Type": []string{contentType}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { defaultClient.Transport = originalTransport })

	body, contentType, err := eseFetchResource(serverURL+"/valid.png", serverURL, true)
	if err != nil || contentType != "image/png" || len(body) == 0 {
		t.Fatalf("valid image fetch = len=%d type=%q err=%v", len(body), contentType, err)
	}
	if _, _, err := eseFetchResource(serverURL+"/fake.png", serverURL, true); err == nil {
		t.Fatal("HTML captcha shell with image content type was accepted")
	}
	if _, _, err := eseFetchResource(serverURL+"/missing.pdf", serverURL, false); err == nil {
		t.Fatal("HTTP 404 resource was accepted")
	}

	images := eseVerifyImages([]string{
		serverURL + "/valid.png",
		serverURL + "/fake.png",
		serverURL + "/missing.png",
		"javascript:void(0)",
	}, serverURL)
	if len(images) != 1 || images[0] != serverURL+"/valid.png" {
		t.Fatalf("verified images = %#v", images)
	}

	attachments := eseVerifyAttachments([]model.Attachment{
		{Name: "valid.pdf", URL: serverURL + "/valid.pdf"},
		{Name: "fake.pdf", URL: serverURL + "/fake.pdf"},
		{Name: "valid.docx", URL: serverURL + "/valid.docx"},
	}, serverURL)
	if len(attachments) != 2 {
		t.Fatalf("verified attachments = %#v", attachments)
	}
	if attachments[0].Name != "valid.pdf" || attachments[0].Size != "9" || attachments[1].Name != "valid.docx" || attachments[1].Size != "7" {
		t.Fatalf("verified attachment metadata = %#v", attachments)
	}
}

func TestESEParsersRejectShellsAndKeepLegalEmptyLists(t *testing.T) {
	empty := []byte(`<html><body><div class="ny-right"><div class="position"><h3>文化活动</h3></div><div class="list"><ul><li><a href="#">尚未发布</a></li></ul></div></div></body></html>`)
	items, err := parseAdapter_eseList(empty, eseTestBaseURL)
	if err != nil || len(items) != 0 {
		t.Fatalf("legal empty list = %#v, err=%v", items, err)
	}

	shell := []byte(`<html><head><title>环境科学与工程学院</title></head><body><nav><a href="/info/1000/1.htm">导航伪文章</a></nav><div class="banner"><a href="/info/1000/2.htm">轮播伪文章</a></div></body></html>`)
	if _, err := parseAdapter_eseList(shell, eseTestBaseURL); err == nil {
		t.Fatal("ESE homepage shell was accepted as a list")
	}
	if _, err := parseAdapter_eseArticle(shell, eseTestBaseURL); err == nil {
		t.Fatal("ESE homepage shell was accepted as an article")
	}

	verification := []byte(`<html><head><title>附件下载</title></head><body><h1>请输入验证码下载附件</h1></body></html>`)
	if _, err := parseAdapter_eseArticle(verification, eseTestBaseURL); err == nil {
		t.Fatal("ESE verification page was accepted as an article")
	}

	waf := loadESEFixture(t, "ese_adapter_waf.html")
	if _, err := parseAdapter_eseList(waf, eseTestBaseURL); err == nil {
		t.Fatal("ESE WAF page was accepted as a list")
	}
	if _, err := parseAdapter_eseArticle(waf, eseTestBaseURL); err == nil {
		t.Fatal("ESE WAF page was accepted as an article")
	}
}
