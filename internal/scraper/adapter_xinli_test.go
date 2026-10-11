package scraper

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

func TestXinliAdapterRegistered(t *testing.T) {
	site := AllSites["xinli"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("xinli adapter hooks are not registered")
	}
	want := []string{
		"zxyw", "tzgg/qb", "tzgg/xljkjy", "tzgg/dzbg", "tzgg/bks",
		"tzgg/yjs", "tzgg/kygz", "tzgg/xsgz", "tzgg/sygz", "tzgg/gsgg",
		"xljkjy/xljkkc", "xljkjy/xljkcp", "xljkjy/xlzxfw", "xljkjy/xlwhhd", "xljkjy/shxlfw",
		"rcpy/bksjy", "rcpy/yjsjy", "kxsj/kydt", "kxsj/sysx",
		"dqgz1/djgz/djdt", "dqgz1/djhd", "dqgz1/jwgz", "dqgz1/ghgz",
		"xsgz/xgdw", "xsgz/xsfc", "xsgz/xshd", "xsgz/jyxx", "hzjl",
		"stfc/dxsxlxh", "stfc/czxh", "stfc/yjsxlxh", "stfc/xcm", "kpzy/spkt",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("xinli feeds = %#v, want %#v", site.SubPages, want)
	}
	if site.PageURL == nil {
		t.Fatal("xinli reverse pagination hook is missing")
	}
	if site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("xinli adapter is missing its page-count or article-fetch hook")
	}
	for _, excluded := range []string{"zxgk/zxjj", "zxgk/zzjg", "zxgk/zxld", "dqgz1/djgz/dwjj", "szdw"} {
		for _, feed := range site.SubPages {
			if feed == excluded {
				t.Fatalf("static Xinli page %q was registered as a news feed", excluded)
			}
		}
	}
}

func TestXinliHomepageCollectsEveryVisibleNewsTab(t *testing.T) {
	html, err := os.ReadFile("testdata/xinli.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_xinliList(html, "http://xinli.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 70 {
		t.Fatalf("homepage items = %d, want all centre and notice tabs", len(items))
	}

	types := make(map[string]int)
	for _, item := range items {
		types[item.Type]++
		if !strings.HasPrefix(item.URL, "http://xinli.swjtu.edu.cn/info/") {
			t.Errorf("non-local item URL = %q", item.URL)
		}
	}
	for _, typ := range []string{"中心要闻", "心理健康教育", "党政办公", "本科生", "研究生", "科研工作", "学生工作", "实验工作", "公示公告"} {
		if types[typ] == 0 {
			t.Errorf("homepage has no items for type %q; types=%#v", typ, types)
		}
	}
	for _, item := range items {
		if item.Title == "心理咨询值班" || strings.Contains(item.URL, "/info/1020/") {
			t.Errorf("service link was treated as a news item: %#v", item)
		}
	}
}

func loadXinliFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestXinliLiveListFixturesCoverReversePagesAndResourceTabs(t *testing.T) {
	const baseURL = "https://xinli.swjtu.edu.cn"
	cases := []struct {
		name, fixture, firstURL, firstType string
		total, count                       int
	}{
		{name: "centre first", fixture: "xinli_list_zxyw_first_live.html", total: 150, count: 20, firstURL: baseURL + "/info/1022/48066.htm", firstType: "中心要闻"},
		{name: "centre middle", fixture: "xinli_list_zxyw_middle_live.html", total: 150, count: 20, firstURL: baseURL + "/info/1022/2187.htm", firstType: "中心要闻"},
		{name: "centre last", fixture: "xinli_list_zxyw_last_live.html", total: 150, count: 12, firstURL: baseURL + "/info/1022/3428.htm", firstType: "中心要闻"},
		{name: "notice first", fixture: "xinli_list_tzgg_qb_first_live.html", total: 52, count: 20, firstURL: baseURL + "/info/1532/48076.htm", firstType: "研究生"},
		{name: "notice middle", fixture: "xinli_list_tzgg_qb_middle_live.html", total: 52, count: 20, firstURL: baseURL + "/info/1522/23546.htm", firstType: "本科生"},
		{name: "notice last", fixture: "xinli_list_tzgg_qb_last_live.html", total: 52, count: 16, firstURL: baseURL + "/info/1502/36856.htm", firstType: "心理健康教育"},
		{name: "undergraduate education", fixture: "xinli_list_rcpy_bksjy_live.html", total: 1, count: 8, firstURL: baseURL + "/info/1702/13586.htm", firstType: "国家级与省级一流课程"},
		{name: "party activity", fixture: "xinli_list_dqgz1_djhd_live.html", total: 12, count: 20, firstURL: baseURL + "/info/1042/47946.htm", firstType: "党建活动"},
		{name: "student activity", fixture: "xinli_list_xsgz_xshd_live.html", total: 36, count: 20, firstURL: baseURL + "/info/1047/48336.htm", firstType: "学生活动"},
		{name: "video classroom", fixture: "xinli_list_kpzy_spkt_live.html", total: 1, count: 5, firstURL: baseURL + "/info/1622/12736.htm", firstType: "视频课堂"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := loadXinliFixture(t, tc.fixture)
			if got := xinliTotalPagesFromHTML(raw); got != tc.total {
				t.Fatalf("total pages = %d, want %d", got, tc.total)
			}
			items, err := parseAdapter_xinliList(raw, baseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.count {
				t.Fatalf("items = %d, want %d: %#v", len(items), tc.count, items)
			}
			if items[0].URL != tc.firstURL || items[0].Type != tc.firstType {
				t.Fatalf("first item = %#v, want URL=%q type=%q", items[0], tc.firstURL, tc.firstType)
			}
		})
	}

	for _, tc := range []struct {
		name, fixture string
		wantTotal     int
	}{
		{name: "external-only culture resources", fixture: "xinli_list_kpzy_xywhhd_live.html", wantTotal: 1},
		{name: "empty psychology resources", fixture: "xinli_list_kpzy_xlkp_live.html", wantTotal: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := loadXinliFixture(t, tc.fixture)
			if got := xinliTotalPagesFromHTML(raw); got != tc.wantTotal {
				t.Fatalf("total pages = %d, want %d", got, tc.wantTotal)
			}
			items, err := parseAdapter_xinliList(raw, baseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 0 {
				t.Fatalf("resource links produced false news items: %#v", items)
			}
		})
	}
}

func TestXinliLiveHomepageFixtureStillContainsAllNoticePanels(t *testing.T) {
	items, err := parseAdapter_xinliList(loadXinliFixture(t, "xinli_home_live.html"), "https://xinli.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 70 {
		t.Fatalf("live homepage items = %d, want all rendered notice panels", len(items))
	}
	types := make(map[string]int)
	for _, item := range items {
		types[item.Type]++
		if !strings.HasPrefix(item.URL, "https://xinli.swjtu.edu.cn/info/") {
			t.Errorf("non-local live homepage item URL = %q", item.URL)
		}
	}
	for _, typ := range []string{"中心要闻", "心理健康教育", "党政办公", "本科生", "研究生", "科研工作", "学生工作", "实验工作", "公示公告"} {
		if types[typ] == 0 {
			t.Errorf("live homepage has no items for type %q; types=%#v", typ, types)
		}
	}
}

func TestXinliListUsesRealRowsAndColumnTypes(t *testing.T) {
	html := []byte(`<!doctype html><html><head>
<meta name="pageType" content="2"><meta name="pageTitle" content="公示公告">
</head><body><section class="ej_main_left"><h2 class="lmmc_yj">公示公告</h2>
<div class="ej_list ej_list_ss"><div><UL>
<li><a href="info/1572/47856.htm" title="真实公告"><div class="box"><p class="bt">真实公告</p><p class="sj">2026-08-24</p></div></a></li>
<li><a href="https://example.com/not-an-article">外部链接</a></li>
<li><a href="info/1522/48086.htm"><div class="box"><p class="bt">本科通知</p><p class="sj">2026-08-23</p></div></a></li>
</UL></div></div></section></body></html>`)
	items, err := parseAdapter_xinliList(html, "http://xinli.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v, want two local article rows", items)
	}
	if items[0].Type != "公示公告" || items[0].Date != "2026-08-24" || items[0].PublishedAt != "2026-08-24" {
		t.Fatalf("first item metadata = %#v", items[0])
	}
	if items[1].Type != "本科生" {
		t.Fatalf("column type = %q, want 本科生", items[1].Type)
	}
}

func TestXinliReversePageURL(t *testing.T) {
	cases := []struct {
		name        string
		page, total int
		want        string
	}{
		{name: "first", page: 1, total: 150, want: "http://xinli.swjtu.edu.cn/zxyw.htm"},
		{name: "middle", page: 75, total: 150, want: "http://xinli.swjtu.edu.cn/zxyw/76.htm"},
		{name: "last", page: 150, total: 150, want: "http://xinli.swjtu.edu.cn/zxyw/1.htm"},
		{name: "past last", page: 151, total: 150, want: ""},
		{name: "zero", page: 0, total: 150, want: ""},
		{name: "unknown total", page: 2, total: 0, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reverseBodaPageURL("http://xinli.swjtu.edu.cn", "zxyw", tc.page, tc.total, ".htm"); got != tc.want {
				t.Fatalf("page URL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestXinliColumnTypeMapCoversAuditedArticleColumns(t *testing.T) {
	want := map[string]string{
		"1022": "中心要闻", "1042": "党建活动", "1047": "学生活动",
		"1142": "心理健康课程", "1143": "心理健康测评", "1144": "心理咨询服务",
		"1147": "心理文化活动", "1151": "社会心理服务", "1212": "师资队伍",
		"1302": "科研动态", "1322": "实验实训", "1352": "党建动态", "1372": "工会工作",
		"1402": "学工队伍", "1412": "学生风采", "1452": "合作交流",
		"1502": "心理健康教育", "1512": "党政办公", "1522": "本科生", "1532": "研究生",
		"1542": "科研工作", "1552": "学生工作", "1562": "实验工作", "1572": "公示公告",
		"1622": "视频课堂", "1662": "研究生教育", "1672": "应用心理学专业",
		"1682": "应用心理学辅修专业", "1692": "应用心理学微专业", "1702": "国家级与省级一流课程",
		"1712": "教学管理规定", "1822": "大学生心理学会", "1832": "创造协会",
		"1842": "研究生心理学会", "1852": "心传媒",
	}
	for code, expected := range want {
		if got := xinliTypeForURL("https://xinli.swjtu.edu.cn/info/" + code + "/1.htm"); got != expected {
			t.Errorf("column %s type = %q, want %q", code, got, expected)
		}
	}
}

func TestXinliTotalPagesHandlesSingleAndReverseNumberedLists(t *testing.T) {
	singlePage := []byte(`<!doctype html><html><head><meta name="pageType" content="2"></head><body>
<div class="ej_list"><ul></ul></div>
<span class="p_t">共20条</span><span class="p_pages"><span class="p_first_d">首页</span><span class="p_last_d">尾页</span></span>
</body></html>`)
	if got := xinliTotalPagesFromHTML(singlePage); got != 1 {
		t.Fatalf("single-page total = %d, want 1", got)
	}

	reversePages := []byte(`<!doctype html><html><body><div class="ej_list"></div>
<span class="p_pages"><span class="p_no"><a href="zxyw/149.htm">2</a></span><span class="p_no"><a href="zxyw/1.htm">150</a></span></span>
</body></html>`)
	if got := xinliTotalPagesFromHTML(reversePages); got != 150 {
		t.Fatalf("reverse-list total = %d, want logical total 150", got)
	}
}

func TestXinliDateValuesPreserveAvailablePrecision(t *testing.T) {
	tests := []struct {
		name, raw, date, publishedAt string
	}{
		{name: "minute", raw: "发布时间：2026-09-03 14:05", date: "2026-09-03", publishedAt: "2026-09-03 14:05:00"},
		{name: "chinese minute", raw: "发布时间：2026年9月3日 14时05分", date: "2026-09-03", publishedAt: "2026-09-03 14:05:00"},
		{name: "short list date", raw: "06-08", date: "06-08", publishedAt: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			date, publishedAt := xinliDateValues(tt.raw)
			if date != tt.date || publishedAt != tt.publishedAt {
				t.Fatalf("xinliDateValues(%q) = (%q, %q), want (%q, %q)", tt.raw, date, publishedAt, tt.date, tt.publishedAt)
			}
		})
	}
}

func TestXinliLiveDetailsUseFixedMetadataAndBodyContainers(t *testing.T) {
	const baseURL = "https://xinli.swjtu.edu.cn"
	cases := []struct {
		name, fixture, title, typ, date, body string
	}{
		{
			name:    "centre news",
			fixture: "xinli_detail_centre_live.html",
			title:   "中心主任入选教育部高等学校健康教学指导委员会委员",
			typ:     "中心要闻",
			date:    "2026-08-24",
			body:    "教育部高等学校健康教学指导委员会",
		},
		{
			name:    "undergraduate notice",
			fixture: "xinli_detail_undergrad_live.html",
			title:   "【推免】关于组织2027届特殊学术专长学生参加中心专家审核小组审核认定公开答辩的通知",
			typ:     "本科生",
			date:    "2026-09-04",
			body:    "答辩时间：9月7日（周一）10:00",
		},
		{
			name:    "student activity",
			fixture: "xinli_detail_activity_live.html",
			title:   "凝心聚力明方向 精准赋能促升学——心理中心召开2027届本科生升学与就业座谈会",
			typ:     "学生活动",
			date:    "2026-07-02",
			body:    "升学与就业",
		},
		{
			name:    "assessment article",
			fixture: "xinli_detail_notice_live.html",
			title:   "2025-2026第二学期心理咨询值班表",
			typ:     "公示公告",
			date:    "2026-05-26",
			body:    "",
		},
		{
			name:    "video classroom article",
			fixture: "xinli_detail_video_live.html",
			title:   "《设计心理学：体验与创意》",
			typ:     "视频课堂",
			date:    "2021-10-18",
			body:    "设计心理学",
		},
		{
			name:    "undergraduate course article",
			fixture: "xinli_detail_course_live.html",
			title:   "国家级与省级一流课程",
			typ:     "国家级与省级一流课程",
			date:    "2021-09-29",
			body:    "课程链接",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			article, err := parseAdapter_xinliArticle(loadXinliFixture(t, tc.fixture), baseURL)
			if err != nil {
				t.Fatal(err)
			}
			if article.Title != tc.title || article.Type != tc.typ || article.Date != tc.date || article.PublishedAt != tc.date {
				t.Fatalf("metadata = title=%q type=%q date=%q published_at=%q", article.Title, article.Type, article.Date, article.PublishedAt)
			}
			if tc.body != "" && !strings.Contains(article.Content, tc.body) {
				t.Fatalf("body lost %q: %q", tc.body, article.Content)
			}
		})
	}

	notice, err := parseAdapter_xinliArticle(loadXinliFixture(t, "xinli_detail_notice_live.html"), baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(notice.Date, "2025-10-28") || strings.Contains(notice.PublishedAt, "2025-10-28") {
		t.Fatalf("body meeting date polluted publication metadata: %+v", notice)
	}
	if len(notice.Images) != 2 || !strings.HasSuffix(notice.Images[0], "/__local/5/39/7A/DDE6301345260FDB9FA524B4C4C_5028594A_4DFF8.png") || !strings.HasSuffix(notice.Images[1], "/__local/9/02/2E/10C67471EC7801C40161F1292A0_25A9DE11_5ACA5.png") {
		t.Fatalf("real notice image was not retained: %#v", notice.Images)
	}
	undergrad, err := parseAdapter_xinliArticle(loadXinliFixture(t, "xinli_detail_undergrad_live.html"), baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(undergrad.Attachments) != 1 || undergrad.Attachments[0].Name != "附件：参加特殊学术专长公开答辩声明书.docx" {
		t.Fatalf("outside-body attachment was not captured: %#v", undergrad.Attachments)
	}
}

func TestXinliParsersRejectErrorShellsAndKeepLegalEmptyResources(t *testing.T) {
	if _, err := parseAdapter_xinliList(loadXinliFixture(t, "xinli_list_404_live.html"), "https://xinli.swjtu.edu.cn"); err == nil {
		t.Fatal("Xinli 404 response was accepted as a list")
	}
	if got := xinliTotalPagesFromHTML(loadXinliFixture(t, "xinli_list_404_live.html")); got != 0 {
		t.Fatalf("Xinli 404 total pages = %d, want 0", got)
	}
	waf := []byte(`<!doctype html><html><head><title>安全验证</title></head><body><h1>请求被拦截</h1></body></html>`)
	if _, err := parseAdapter_xinliList(waf, "https://xinli.swjtu.edu.cn"); err == nil {
		t.Fatal("Xinli WAF response was accepted as a list")
	}
	if _, err := parseAdapter_xinliArticle(waf, "https://xinli.swjtu.edu.cn"); err == nil {
		t.Fatal("Xinli WAF response was accepted as an article")
	}
	if _, err := parseAdapter_xinliArticle(loadXinliFixture(t, "xinli_list_404_live.html"), "https://xinli.swjtu.edu.cn"); err == nil {
		t.Fatal("Xinli 404 response was accepted as an article")
	}
	for _, fixture := range []string{"xinli_list_kpzy_xywhhd_live.html", "xinli_list_kpzy_xlkp_live.html"} {
		items, err := parseAdapter_xinliList(loadXinliFixture(t, fixture), "https://xinli.swjtu.edu.cn")
		if err != nil || len(items) != 0 {
			t.Fatalf("legal non-news resource page %s = %#v, err=%v", fixture, items, err)
		}
	}
}

func TestXinliResourceVerificationChecksHTTPStatusTypeAndMagic(t *testing.T) {
	if xinliResourceBodyIsUsable("text/html; charset=UTF-8", loadXinliFixture(t, "xinli_attachment_captcha_live.html"), "download.jsp", false) {
		t.Fatal("real Xinli captcha HTML was accepted as an attachment")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})
		case "/fake.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<!doctype html><h1>验证码下载附件</h1>"))
		case "/valid.pdf":
			w.Header().Set("Content-Type", "application/pdf; charset=binary")
			_, _ = w.Write([]byte("%PDF-1.7\n"))
		case "/fake.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("<html><body>登录页面</body></html>"))
		case "/valid.docx":
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
			_, _ = w.Write([]byte{'P', 'K', 0x03, 0x04, 'X', 'I', 'N'})
		case "/captcha-download":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<h1>请输入验证码下载附件</h1>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body, contentType, err := xinliFetchResource(server.URL+"/valid.png", server.URL, true)
	if err != nil || contentType != "image/png" || len(body) == 0 {
		t.Fatalf("valid image fetch = len=%d type=%q err=%v", len(body), contentType, err)
	}
	for _, resource := range []string{"/fake.png", "/missing.png", "/captcha-download"} {
		if _, _, err := xinliFetchResource(server.URL+resource, server.URL, resource == "/fake.png"); err == nil {
			t.Fatalf("invalid resource %s was accepted", resource)
		}
	}

	images := xinliVerifyImages([]string{
		server.URL + "/valid.png", server.URL + "/fake.png", server.URL + "/missing.png",
		"javascript:void(0)", "https://example.com/external.png",
	}, server.URL)
	if len(images) != 1 || images[0] != server.URL+"/valid.png" {
		t.Fatalf("verified images = %#v", images)
	}
	attachments := xinliVerifyAttachments([]model.Attachment{
		{Name: "valid.pdf", URL: server.URL + "/valid.pdf"},
		{Name: "fake.pdf", URL: server.URL + "/fake.pdf"},
		{Name: "valid.docx", URL: server.URL + "/valid.docx"},
		{Name: "external.pdf", URL: "https://example.com/external.pdf"},
	}, server.URL)
	if len(attachments) != 2 {
		t.Fatalf("verified attachments = %#v", attachments)
	}
	if attachments[0].Name != "valid.pdf" || attachments[0].Size != "9" || attachments[1].Name != "valid.docx" || attachments[1].Size != "7" {
		t.Fatalf("verified attachment metadata = %#v", attachments)
	}
}

func TestXinliForwardedArticleUsesOuterDateAndKeepsBodyResources(t *testing.T) {
	fixture, err := os.ReadFile("testdata/xinli_forwarded_article.html")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1522/48476.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(fixture)
		case "/images/forwarded.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})
		case "/system/_content/download.jsp":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	site := AllSites["xinli"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	ClearCache()
	site.BaseURL = server.URL
	NoCache = true
	t.Cleanup(func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
		ClearCache()
	})

	article, raw, err := site.GetArticleSnapshot(server.URL + "/info/1522/48476.htm")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(fixture) {
		t.Fatal("article snapshot did not retain the exact source response")
	}
	if article.Title != "推免丨关于开展2027年推免排名成绩计算工作的通知（转发自本科生院）" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Type != "本科生" {
		t.Fatalf("type = %q, want 本科生", article.Type)
	}
	if article.Date != "2026-09-03" || article.PublishedAt != "2026-09-03" {
		t.Fatalf("outer publication = (%q, %q), want 2026-09-03 without copied body time", article.Date, article.PublishedAt)
	}
	if strings.Contains(article.Content, "2026-09-01 20:53:28") {
		t.Fatalf("copied publication metadata leaked into article body: %q", article.Content)
	}
	if !strings.Contains(article.Content, "学生确认课程成绩时间") || !strings.Contains(article.Content, "2026年9月1日至2026年9月6日") {
		t.Fatalf("article body was not preserved: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/images/forwarded.png" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "2027_应用心理学_推免课程.pdf" || !strings.HasPrefix(article.Attachments[0].URL, server.URL+"/system/_content/download.jsp?") {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, server.URL+"/images/forwarded.png") {
		t.Fatalf("content_html did not resolve the image URL: %q", article.ContentHTML)
	}
}

func TestXinliArticleUsesDetailTitleInsteadOfSectionHeading(t *testing.T) {
	html, err := os.ReadFile("testdata/xinli_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_xinliArticle(html, "http://xinli.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	wantTitle := "第九届“演说中国”全国青少年演讲与朗诵艺术展示全国总展演隆重启幕"
	if article.Title != wantTitle {
		t.Fatalf("title = %q, want detail title %q", article.Title, wantTitle)
	}
	if article.Title == "中心要闻" {
		t.Fatal("section heading was incorrectly used as article title")
	}
	if article.Date != "2026-08-13" {
		t.Fatalf("date = %q, want 2026-08-13", article.Date)
	}
	if article.Type != "中心要闻" {
		t.Fatalf("type = %q, want 中心要闻", article.Type)
	}
	if !strings.Contains(article.Content, "全国总展演开幕式") {
		t.Fatalf("article content was not extracted: %q", article.Content)
	}
}

func TestXinliIndexFeedListsHomepage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.htm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(loadXinliFixture(t, "xinli_home_live.html"))
	}))
	defer server.Close()

	site := AllSites["xinli"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	})

	items, err := site.ListNewsPage("index", 1)
	if err != nil || len(items) == 0 {
		t.Fatalf("index items = %d, err=%v", len(items), err)
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, server.URL+"/info/") {
			t.Fatalf("non-local index item: %#v", item)
		}
	}
	if total, err := site.GetTotalPages("index"); err != nil || total != 1 {
		t.Fatalf("index total = %d, err=%v", total, err)
	}
	if _, err := site.ListNewsPage("index", 2); err == nil {
		t.Fatal("index page 2 was accepted")
	}
}
