package scraper

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCmAdapterRegistration(t *testing.T) {
	site, ok := AllSites["cm"]
	if !ok {
		t.Fatal("cm site is not registered")
	}
	if site.BaseURL != cmCanonicalBaseURL {
		t.Fatalf("CM base URL = %q, want canonical HTTPS URL %q", site.BaseURL, cmCanonicalBaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("CM adapter hooks are incomplete: %+v", site)
	}
	if !reflect.DeepEqual(site.SubPages, cmSubPages) {
		t.Fatalf("CM sub-pages = %#v, want %#v", site.SubPages, cmSubPages)
	}
}

func TestCmPageCountUsesDisplayedLogicalNumbers(t *testing.T) {
	body := []byte(`<div class="p_t">共537条</div><span class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="xyyw/35.htm">2</a></span><span class="p_no"><a href="xyyw/34.htm">3</a></span><span class="p_no"><a href="xyyw/1.htm">36</a></span></span>`)
	if got := cmPageCountFromHTML(body); got != 36 {
		t.Fatalf("cmPageCountFromHTML() = %d, want 36 from displayed logical page numbers", got)
	}
}

func TestCmPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	base := "https://cm.swjtu.edu.cn"
	cases := []struct {
		name  string
		slug  string
		page  int
		total int
		want  string
	}{
		{name: "first", slug: "index/xyyw", page: 1, total: 36, want: base + "/index/xyyw.htm"},
		{name: "second", slug: "index/xyyw", page: 2, total: 36, want: base + "/index/xyyw/35.htm"},
		{name: "middle", slug: "index/xyyw", page: 18, total: 36, want: base + "/index/xyyw/19.htm"},
		{name: "last", slug: "index/xyyw", page: 36, total: 36, want: base + "/index/xyyw/1.htm"},
		{name: "nested last", slug: "index/ssfc/xsfc", page: 2, total: 2, want: base + "/index/ssfc/xsfc/1.htm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmPageURL(base, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
				t.Fatalf("cmPageURL(%q, %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
			}
		})
	}
}

func TestCmPageURLRejectsOutOfRange(t *testing.T) {
	const base = "https://cm.swjtu.edu.cn"
	for _, tc := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 36},
		{name: "past last", page: 37, totalPages: 36},
		{name: "past one-page feed", page: 2, totalPages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmPageURL(base, "index/xyyw", tc.page, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("cmPageURL() = %q for page %d/%d, want empty URL", got, tc.page, tc.totalPages)
			}
		})
	}
}

func TestCmHomepageParsesAllNewsModulesAndExcludesChrome(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_home.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_cmList(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("got %d homepage items, want all home modules: %#v", len(items), items)
	}

	want := map[string]struct {
		title string
		date  string
		typ   string
	}{
		"/info/1031/1.htm": {title: "学院要闻中的完整标题", date: "2026-07-29", typ: "学院要闻"},
		"/info/1032/2.htm": {title: "通知公告中的完整标题", date: "2026-07-08", typ: "通知公告"},
		"/info/1033/3.htm": {title: "科研团队中的完整标题", typ: "科研团队"},
		"/info/1034/4.htm": {title: "科研动态中的完整标题", date: "01-12", typ: "科研动态"},
		"/info/1035/5.htm": {title: "招生就业中的完整标题", date: "06-18", typ: "招生就业"},
		"/info/1036/6.htm": {title: "校园生活中的完整标题", date: "06-22", typ: "校园生活"},
		"/info/1512/7.htm": {title: "学生风采中的完整标题", typ: "学生风采"},
		"/info/1522/8.htm": {title: "教师风采中的完整标题", typ: "教师风采"},
	}
	for _, item := range items {
		path := strings.TrimPrefix(item.URL, "https://cm.swjtu.edu.cn")
		expected, ok := want[path]
		if !ok {
			t.Errorf("unexpected homepage item: %+v", item)
			continue
		}
		if item.Title != expected.title || item.Date != expected.date || item.Type != expected.typ {
			t.Errorf("homepage item = %+v, want title=%q date=%q type=%q", item, expected.title, expected.date, expected.typ)
		}
		delete(want, path)
	}
	if len(want) != 0 {
		t.Fatalf("homepage modules missing from parsed output: %#v", want)
	}
	for _, item := range items {
		if strings.Contains(item.Title, "导航") || strings.Contains(item.Title, "轮播") || strings.Contains(item.Title, "静态") {
			t.Errorf("page chrome leaked into homepage feed: %+v", item)
		}
	}
}

func TestCmListParsesRowsAndPreciseTime(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_cmList(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want 2: %#v", len(items), items)
	}
	if items[0].Title != "列表中的精确时间通知" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31 14:34:00" || items[0].Type != "通知公告" {
		t.Fatalf("unexpected precise list item: %+v", items[0])
	}
	if items[1].Title != "列表中的日期公告" || items[1].Date != "2026-08-30" || items[1].PublishedAt != "" {
		t.Fatalf("unexpected date-only list item: %+v", items[1])
	}
}

func TestCmListRejectsRecognizedEmptyShell(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_empty_list.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdapter_cmList(body, "https://cm.swjtu.edu.cn"); err == nil {
		t.Fatal("recognized empty/static list shell was accepted")
	}
}

func TestCmFengcaiListUsesNListTxt(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_fengcai.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_cmList(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Type != "教师风采" || items[1].Title != "第二位教师风采" {
		t.Fatalf("unexpected n_listTxt items: %#v", items)
	}
	for _, item := range items {
		if item.Date != "" || item.PublishedAt != "" {
			t.Fatalf("legacy n_listTxt placeholder date leaked: %+v", item)
		}
	}
}

func TestCmArticleParserPrefersDtlTitleOverSidebarHome(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_cmArticle(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "国家卫生健康委员会专家组到医学院开展生物安全飞行检查" {
		t.Fatalf("article title = %q, want the .dtl-tit h4 headline", article.Title)
	}
	if article.Title == "首页" {
		t.Fatal("sidebar title leaked into article title")
	}
	if article.Date != "2026-07-29" {
		t.Fatalf("article date = %q, want 2026-07-29", article.Date)
	}
	if article.Type != "学院要闻" || article.Author != "智伟、赵春花" {
		t.Fatalf("article metadata = type %q author %q", article.Type, article.Author)
	}
	if article.Content == "" {
		t.Fatal("expected article content")
	}
}

func TestCmArticleParsesImagesAttachmentsAndMinutePrecision(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_article_resources.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_cmArticle(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "带资源的医学院通知" || article.Type != "常用信息" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("unexpected article metadata: %+v", article)
	}
	if article.Content != "正文第一段。\n\n正文第二段。" {
		t.Fatalf("content = %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://cm.swjtu.edu.cn/__local/cm-body.png" {
		t.Fatalf("unexpected images: %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("unexpected attachments: %#v", article.Attachments)
	}
	if article.Attachments[0].Name != "附件一.docx" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("unexpected first attachment: %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "附件二.pdf" || article.Attachments[1].URL != "https://cm.swjtu.edu.cn/__local/cm-file.pdf" {
		t.Fatalf("unexpected second attachment: %#v", article.Attachments[1])
	}
	if !strings.Contains(article.ContentHTML, "https://cm.swjtu.edu.cn/__local/cm-body.png") {
		t.Fatal("content HTML did not retain a resolved body image")
	}
}

func TestCmArticleDoesNotUseBodyDatesAsPublicationDate(t *testing.T) {
	body, err := os.ReadFile("testdata/cm_article_body_dates.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_cmArticle(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("body event/deadline/forwarded dates leaked into publication metadata: %+v", article)
	}
}

func TestCmDateValuesKeepsChineseClockPrecision(t *testing.T) {
	date, publishedAt := cmDateValues("2026年8月31日14时34分56秒")
	if date != "2026-08-31" || publishedAt != "2026-08-31 14:34:56" {
		t.Fatalf("cmDateValues() = %q, %q; want date and second precision", date, publishedAt)
	}
}

func TestCmParsersRejectStaticShells(t *testing.T) {
	if _, err := parseAdapter_cmList([]byte(`<html><body><nav><a href="/info/1000/1.htm">导航文章标题</a></nav><section class="banner"><a href="/info/1000/2.htm">轮播文章标题</a></section></body></html>`), "https://cm.swjtu.edu.cn"); err == nil {
		t.Fatal("static page shell was accepted as a list")
	}
	if _, err := parseAdapter_cmArticle([]byte(`<html><body><h1>首页</h1><div class="v_news_content"><p>不是文章</p></div></body></html>`), "https://cm.swjtu.edu.cn"); err == nil {
		t.Fatal("static page shell was accepted as an article")
	}
}

func TestCmListPageRepairsLegacyDatesBeforeRangeFiltering(t *testing.T) {
	var requested []string
	articleHTML := func(title, date string) string {
		return `<html><body><div class="ny-tit"><h4>学生风采</h4></div><div class="detail"><div class="dtl-tit"><h4>` + title + `</h4><div class="dtl-ar"><span>发布时间：` + date + `</span></div></div><div class="dtl-txt"><div class="v_news_content"><p>详情正文</p></div></div></div></body></html>`
	}
	responses := map[string]string{
		"/index/ssfc/xsfc.htm":   `<html><head><meta name="pageTitle" content="学生风采"></head><body><div class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="1.htm">2</a></span></div><div class="n_listTxt"><ul><li><a href="../../../info/1512/1.htm" title="过期学生风采"><h2>过期学生风采</h2><time>2023-04-12</time></a></li><li><a href="../../../info/1512/2.htm" title="最近一年学生风采"><h2>最近一年学生风采</h2><time>2023-04-12</time></a></li></ul></div></body></html>`,
		"/index/ssfc/xsfc/1.htm": `<html><head><meta name="pageTitle" content="学生风采"></head><body><div class="p_pages"><span class="p_no_d">1</span></div><div class="n_listTxt"><ul><li><a href="../../../info/1512/3.htm" title="更早学生风采"><h2>更早学生风采</h2><time>2023-04-12</time></a></li></ul></div></body></html>`,
		"/info/1512/1.htm":       articleHTML("过期学生风采", "2024-05-07"),
		"/info/1512/2.htm":       articleHTML("最近一年学生风采", "2025-11-17"),
		"/info/1512/3.htm":       articleHTML("更早学生风采", "2024-01-01"),
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		if body, ok := responses[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	site := AllSites["cm"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	site.BaseURL = "http://" + listener.Addr().String()
	NoCache = true
	defer func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
	}()

	items, err := site.ListNewsRange("index/ssfc/xsfc", "2025-09-03", "2026-09-03", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want only the one detail dated in the last year: %#v", len(items), items)
	}
	if items[0].Title != "最近一年学生风采" || items[0].Date != "2025-11-17" || items[0].Type != "学生风采" {
		t.Fatalf("list metadata was not repaired from detail: %+v", items[0])
	}
	seenSecondPage := false
	for _, path := range requested {
		if path == "/index/ssfc/xsfc/1.htm" {
			seenSecondPage = true
			break
		}
	}
	if !seenSecondPage {
		t.Fatalf("range walk never reached reverse-numbered second page; requests=%v", requested)
	}
}

func TestCmArticleKeepsTableTextAndUsableResources(t *testing.T) {
	body := []byte(`<html><body><div class="ny-tit"><h4>通知公告</h4></div><div class="detail"><div class="dtl-tit"><h4>表格与资源通知</h4><div class="dtl-ar"><span>发布时间：2026-08-31 14:34</span></div></div><div class="dtl-txt"><div class="v_news_content"><p>正文段落</p><table><tr><th>字段</th><td>值</td></tr></table><img src="/blank.gif" data-original="/__local/real.png"><img src="javascript:void(0)" data-src="/__local/real-2.png"><img src="data:image/gif;base64,placeholder" data-lazy-src="/__local/real-3.png"></div></div><div class="attachment"><a href="javascript:download('bad.pdf')" download="伪造.pdf">伪造.pdf</a><a href="/__local/real.pdf">真实.pdf</a></div></div></body></html>`)
	article, err := parseAdapter_cmArticle(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(article.Content, "字段") || !strings.Contains(article.Content, "值") {
		t.Fatalf("table cells were lost from article content: %q", article.Content)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" || article.Type != "通知公告" {
		t.Fatalf("unexpected article metadata: %+v", article)
	}
	wantImages := map[string]bool{
		"https://cm.swjtu.edu.cn/__local/real.png":   true,
		"https://cm.swjtu.edu.cn/__local/real-2.png": true,
		"https://cm.swjtu.edu.cn/__local/real-3.png": true,
	}
	if len(article.Images) != len(wantImages) {
		t.Fatalf("got images %#v, want all lazy images and no placeholder", article.Images)
	}
	for _, image := range article.Images {
		if !wantImages[image] {
			t.Errorf("unexpected image resource %q", image)
		}
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "真实.pdf" || article.Attachments[0].URL != "https://cm.swjtu.edu.cn/__local/real.pdf" {
		t.Fatalf("pseudo attachment was not filtered: %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, `src="https://cm.swjtu.edu.cn/__local/real.png"`) {
		t.Fatal("content HTML did not retain a resolved image URL")
	}
}

func TestCmListSurfacesExternalOnlyRows(t *testing.T) {
	body := []byte(`<div class="n_list"><ul><li><a href="https://mp.weixin.qq.com/s/example" title="微信招聘信息">微信招聘信息</a></li></ul></div>`)
	if got := cmExternalListRowCount(body, "https://cm.swjtu.edu.cn"); got != 1 {
		t.Fatalf("cmExternalListRowCount() = %d, want 1", got)
	}
}

func TestCmListAllExternalRowsIsEmptyFeed(t *testing.T) {
	body := []byte(`<html><head><meta name="pageTitle" content="教师招聘"></head><body><div class="n_list"><ul><li><a href="https://mp.weixin.qq.com/s/example" target="_blank" title="微信招聘信息" class="eclip">微信招聘信息</a><time>2026-01-12</time></li></ul></div><div class="p_pages"><span class="p_no_d">1</span></div></body></html>`)
	items, err := parseAdapter_cmList(body, "https://cm.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("all-external list page should parse as an empty feed, got error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("all-external list page returned %d items, want empty feed: %#v", len(items), items)
	}
	if got := cmExternalListRowCount(body, "https://cm.swjtu.edu.cn"); got != 1 {
		t.Fatalf("cmExternalListRowCount() = %d, want 1", got)
	}
}

func TestCmLiveListsSkipExternalRowsAndKeepLocalOnes(t *testing.T) {
	cases := []struct {
		file           string
		wantCount      int
		wantFirstURL   string
		wantFirstTitle string
		wantFirstDate  string
	}{
		{
			file:           "testdata/cm_live_jszp.html",
			wantCount:      1,
			wantFirstURL:   "https://cm.swjtu.edu.cn/info/1064/14421.htm",
			wantFirstTitle: "西南交通大学医学院求贤榜： 心怀“国之大者”，逐梦“西南交大”",
			wantFirstDate:  "2023-01-13",
		},
		{
			file:           "testdata/cm_live_rczp.html",
			wantCount:      4,
			wantFirstURL:   "https://cm.swjtu.edu.cn/info/1063/24751.htm",
			wantFirstTitle: "@全球英才，西南交通大学医学院诚邀依托申报2025海外优青！",
			wantFirstDate:  "2025-02-25",
		},
		{
			file:           "testdata/cm_live_xyyw_page4.html",
			wantCount:      14,
			wantFirstURL:   "https://cm.swjtu.edu.cn/info/1031/30151.htm",
			wantFirstTitle: "医学院党委理论学习中心组开展树立和践行正确政绩观学习教育暨全国两会精神专题学习",
			wantFirstDate:  "2026-03-25",
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			body, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			if got := cmExternalListRowCount(body, "https://cm.swjtu.edu.cn"); got != 1 {
				t.Fatalf("cmExternalListRowCount() = %d, want 1 external row", got)
			}
			items, err := parseAdapter_cmList(body, "https://cm.swjtu.edu.cn")
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.wantCount {
				t.Fatalf("got %d items, want %d local rows kept: %#v", len(items), tc.wantCount, items)
			}
			first := items[0]
			if first.URL != tc.wantFirstURL || first.Title != tc.wantFirstTitle || first.Date != tc.wantFirstDate {
				t.Fatalf("first item = %+v, want url=%q title=%q date=%q", first, tc.wantFirstURL, tc.wantFirstTitle, tc.wantFirstDate)
			}
			for _, item := range items {
				if !strings.Contains(item.URL, "://cm.swjtu.edu.cn/") {
					t.Errorf("external row leaked into feed: %+v", item)
				}
			}
		})
	}
}

func TestCmListPageSkipsExternalRowsAndTreatsAllExternalPageAsEmpty(t *testing.T) {
	jszpBody, err := os.ReadFile("testdata/cm_live_jszp.html")
	if err != nil {
		t.Fatal(err)
	}
	externalOnly := []byte(`<html><head><meta name="pageTitle" content="人才招聘"></head><body><div class="n_list"><ul><li><a href="https://mp.weixin.qq.com/s/LMtoEylXdORvk8eGv392nA" target="_blank" title="@全球英才丨西南交通大学诚邀依托申报2026海外优青 ！" class="eclip">@全球英才丨西南交通大学诚邀依托申报2026海外优青 ！</a><time>2026-01-05</time></li></ul></div><div class="pb_sys_common"><span class="p_t">共1条</span><span class="p_pages"><span class="p_no_d">1</span></span></div></body></html>`)
	responses := map[string][]byte{
		"/cpyc/jszp.htm": jszpBody,
		"/cpyc/rczp.htm": externalOnly,
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := responses[r.URL.Path]; ok {
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	site := AllSites["cm"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	site.BaseURL = "http://" + listener.Addr().String()
	NoCache = true
	defer func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
	}()

	items, totalPages, err := site.ListPageFunc("cpyc/jszp", 1)
	if err != nil {
		t.Fatalf("mixed list page failed: %v", err)
	}
	if totalPages != 1 {
		t.Fatalf("totalPages = %d, want 1", totalPages)
	}
	if len(items) != 1 || items[0].URL != "http://"+listener.Addr().String()+"/info/1064/14421.htm" {
		t.Fatalf("mixed list page items = %#v, want only the local row", items)
	}

	items, totalPages, err = site.ListPageFunc("cpyc/rczp", 1)
	if err != nil {
		t.Fatalf("all-external list page should be an empty feed, got error: %v", err)
	}
	if totalPages != 1 {
		t.Fatalf("totalPages = %d, want 1", totalPages)
	}
	if len(items) != 0 {
		t.Fatalf("all-external list page items = %#v, want empty feed", items)
	}
}

func TestCmLiveArticleFiltersHTMLResourcesByTypeAndMagic(t *testing.T) {
	articleHTML := `<html><body><div class="ny-tit"><h4>通知公告</h4></div><div class="detail"><div class="dtl-tit"><h4>资源验证通知</h4><div class="dtl-ar"><span>发布时间：2026-08-31</span></div></div><div class="dtl-txt"><div class="v_news_content"><p>正文内容</p><img src="/good.png"><img src="/bad.png"><a href="/good.pdf">有效附件.pdf</a><a href="/system/_content/download.jsp?urltype=news.DownloadAttachUrl&amp;wbfileid=BAD">验证码附件.rar</a></div></div></div></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/1000/1.htm":
			_, _ = w.Write([]byte(articleHTML))
		case "/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		case "/bad.png":
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body>请输入验证码</body></html>`))
		case "/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\n"))
		case "/system/_content/download.jsp":
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body>验证码下载附件</body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["cm"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	site.BaseURL = server.URL
	NoCache = true
	defer func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
	}()

	article, body, err := site.GetArticleSnapshot(server.URL + "/info/1000/1.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 || article.Title != "资源验证通知" || article.Date != "2026-08-31" {
		t.Fatalf("unexpected live article result: article=%+v body=%d", article, len(body))
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/good.png" {
		t.Fatalf("HTML image resource was not rejected: %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "有效附件.pdf" || article.Attachments[0].URL != server.URL+"/good.pdf" {
		t.Fatalf("HTML/CAPTCHA attachment was not rejected: %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "/bad.png") || strings.Contains(article.ContentHTML, "BAD") {
		t.Fatalf("rejected image or attachment leaked into ContentHTML: %q", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, server.URL+"/good.png") || !strings.Contains(article.ContentHTML, server.URL+"/good.pdf") {
		t.Fatalf("verified resources were not retained in ContentHTML: %q", article.ContentHTML)
	}
}
