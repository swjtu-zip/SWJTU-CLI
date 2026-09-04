package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSmeAdapterRegistration(t *testing.T) {
	site, ok := AllSites["sme"]
	if !ok {
		t.Fatal("sme site is not registered")
	}
	if site.BaseURL != smeCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("SME adapter hooks are incomplete: %+v", site)
	}
	wantSubPages := smeSubPages
	if !reflect.DeepEqual(site.SubPages, wantSubPages) {
		t.Fatalf("unexpected SME sub-pages: got %#v, want %#v", site.SubPages, wantSubPages)
	}
}

func TestSmeHomeParserKeepsArticleCarouselsAndTabDates(t *testing.T) {
	html, err := os.ReadFile("testdata/sme_home_tabs.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseSmeNews(html, "https://sme.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d SME home articles, want 3: %#v", len(items), items)
	}
	want := []struct {
		title string
		url   string
		date  string
		typ   string
	}{
		{"学院新闻真实文章", "https://sme.swjtu.edu.cn/info/1094/65651.htm", "", "学院新闻"},
		{"学术活动真实文章", "https://sme.swjtu.edu.cn/info/1941/65711.htm", "2026-08-25", "学术活动"},
		{"党政办公真实通知", "https://sme.swjtu.edu.cn/info/1621/65471.htm", "2026-07-09", "党政办公"},
	}
	for i, expected := range want {
		if items[i].Title != expected.title || items[i].URL != expected.url || items[i].Date != expected.date || items[i].Type != expected.typ {
			t.Errorf("item %d = (%q, %q, %q, %q), want (%q, %q, %q, %q)", i, items[i].Title, items[i].URL, items[i].Date, items[i].Type, expected.title, expected.url, expected.date, expected.typ)
		}
	}
}

func TestSmeListParserExtractsArticlesAndDates(t *testing.T) {
	html, err := os.ReadFile("testdata/sme.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseSmeNews(html, "https://sme.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d SME articles, want 3: %#v", len(items), items)
	}

	want := []struct {
		title string
		url   string
		date  string
	}{
		{
			title: "机械工程学院召开第四届教代会暨第五届工代会第一次会议",
			url:   "https://sme.swjtu.edu.cn/info/1094/65561.htm",
			date:  "2026-07-13",
		},
		{
			title: "机械工程学院党委书记袁艳平为全体教职工讲授专题党课",
			url:   "https://sme.swjtu.edu.cn/info/1094/65551.htm",
			date:  "2026-07-11",
		},
		{
			title: "关于开展2026年7月主题党日的通知",
			url:   "https://sme.swjtu.edu.cn/info/1066/65431.htm",
			date:  "2026-07-08",
		},
	}
	for i, expected := range want {
		if items[i].Title != expected.title || items[i].URL != expected.url || items[i].Date != expected.date {
			t.Errorf("item %d = (%q, %q, %q), want (%q, %q, %q)", i, items[i].Title, items[i].URL, items[i].Date, expected.title, expected.url, expected.date)
		}
	}
	for _, item := range items {
		if item.Title == "学院简介" || item.Title == "首页" || item.Title == "横幅文章不应进入列表" {
			t.Errorf("navigation/banner item leaked: %#v", item)
		}
	}
}

func TestSmeArticleParserNormalizesPublishDate(t *testing.T) {
	html, err := os.ReadFile("testdata/sme_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseSmeArticle(html, "https://sme.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "机械工程学院召开第四届教代会暨第五届工代会第一次会议" {
		t.Fatalf("unexpected article title: %q", article.Title)
	}
	if article.Date != "2026-07-13" {
		t.Fatalf("article date = %q, want 2026-07-13", article.Date)
	}
}

func TestSmeArticleParserExtractsMetaImageAndExternalAttachment(t *testing.T) {
	html, err := os.ReadFile("testdata/sme_article_attachment.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseSmeArticle(html, "https://sme.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "SME 附件与精确时间测试文章" {
		t.Fatalf("unexpected article title: %q", article.Title)
	}
	if article.Date != "2026-08-06" || article.PublishedAt != "2026-08-06 12:34:56" {
		t.Fatalf("unexpected publication time: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	if article.Type != "学院新闻" {
		t.Fatalf("article type = %q, want 学院新闻", article.Type)
	}
	if article.Editor != "测试员" {
		t.Fatalf("article editor = %q, want 测试员", article.Editor)
	}
	if article.Content != "这是正文内容。" {
		t.Fatalf("article content = %q, want 正文内容", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://sme.swjtu.edu.cn/__local/body.png" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("got %d attachments, want 1: %#v", len(article.Attachments), article.Attachments)
	}
	attachment := article.Attachments[0]
	wantAttachmentURL := "https://sme.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1444057924&wbfileid=ABC123"
	if attachment.Name != "附件：测试材料.pdf" || attachment.URL != wantAttachmentURL {
		t.Fatalf("unexpected attachment: %#v", attachment)
	}
	if !strings.Contains(article.ContentHTML, "https://sme.swjtu.edu.cn/__local/body.png") {
		t.Fatalf("content HTML lost canonical image URL: %q", article.ContentHTML)
	}
}

func TestSmeListParserPreservesMinutePrecision(t *testing.T) {
	html, err := os.ReadFile("testdata/sme_list_precise.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseSmeNews(html, "http://sme.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want one: %#v", len(items), items)
	}
	item := items[0]
	if item.Date != "2026-08-06" || item.PublishedAt != "2026-08-06 12:34:00" {
		t.Fatalf("publication time = date %q published_at %q", item.Date, item.PublishedAt)
	}
	if item.Type != "学院新闻" {
		t.Fatalf("item type = %q, want 学院新闻", item.Type)
	}
}

func TestSmeArticleParserDoesNotUseBodyEventDate(t *testing.T) {
	html, err := os.ReadFile("testdata/sme_article_event_date.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseSmeArticle(html, "http://sme.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-08-06" || article.PublishedAt != "2026-08-06 12:34:00" {
		t.Fatalf("body event date replaced publication time: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
}

func TestSmeParsersRejectUnrecognizedResponsesAndKeepRealEmptyLists(t *testing.T) {
	if _, err := parseSmeNews([]byte(`<html><head><title>请登录</title></head><body>请登录后访问</body></html>`), smeCanonicalBaseURL); err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("login shell was accepted as a list: %v", err)
	}
	if _, err := parseSmeNews([]byte(`<main class="list_main_box"><div class="p_pages"><span class="p_no_d">1</span></div></main>`), smeCanonicalBaseURL); err != nil {
		t.Fatalf("real empty one-page list rejected: %v", err)
	}
	if _, err := parseSmeNews([]byte(`<main class="list_main_box"><div class="list_li_box"><a href="/download/a.docx">文件</a></div></main>`), smeCanonicalBaseURL); err == nil || !strings.Contains(err.Error(), "resource catalog") {
		t.Fatalf("resource-only list was accepted as empty: %v", err)
	}
	if _, err := parseSmeArticle([]byte(`<html><head><title>验证码</title></head><body>请输入验证码</body></html>`), smeCanonicalBaseURL); err == nil || !strings.Contains(err.Error(), "verification") {
		t.Fatalf("verification shell was accepted as an article: %v", err)
	}
}

func TestSmeHomepageHiddenTabAndColumnTypes(t *testing.T) {
	html := []byte(`<html><body>
		<div id="Tab10"><a href="/info/1541/64041.htm" title="公示文章">公示文章</a></div>
		<div class="colorful"><a href="/info/1551/63671.htm" title="多彩机械文章">多彩机械文章</a></div>
	</body></html>`)
	items, err := parseSmeNews(html, smeCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Type != "公示专栏" || items[1].Type != "多彩机械" {
		t.Fatalf("hidden/channel types = %#v", items)
	}
}

func TestSmeArticleColumnTypeMapping(t *testing.T) {
	for rawURL, want := range map[string]string{
		"https://sme.swjtu.edu.cn/info/1055/65741.htm": "学生工作",
		"https://sme.swjtu.edu.cn/info/1098/65551.htm": "研究生教学",
		"https://sme.swjtu.edu.cn/info/1541/64041.htm": "公示专栏",
		"https://sme.swjtu.edu.cn/info/1551/63671.htm": "多彩机械",
	} {
		if got := smeArticleTypeForURL(rawURL); got != want {
			t.Errorf("%s type = %q, want %q", rawURL, got, want)
		}
	}
	if got := smeArticleTypeForURL("https://example.com/info/1094/1.htm"); got != "" {
		t.Fatalf("external article got SME type %q", got)
	}
}

func TestSmeTotalPagesFromHTMLValidatesReverseAndSinglePageLists(t *testing.T) {
	reverse := []byte(`<main class="list_main_box"><div class="p_pages">
		<a class="p_no" href="xyxw/176.htm">2</a><a class="p_no" href="xyxw/1.htm">177</a>
	</div></main>`)
	if got, err := smeTotalPagesFromHTML(reverse, "xwtz/xyxw"); err != nil || got != 177 {
		t.Fatalf("reverse pagination = %d, %v; want 177", got, err)
	}
	single, err := os.ReadFile("testdata/sme_list_precise.html")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := smeTotalPagesFromHTML(single, "xwtz/xyxw"); err != nil || got != 1 {
		t.Fatalf("single-page pagination = %d, %v; want 1", got, err)
	}
	if got, err := smeTotalPagesFromHTML([]byte(`<main class="static-page">静态内容</main>`), "xwtz/xyxw"); err == nil || got != 0 {
		t.Fatalf("static shell was accepted: pages=%d err=%v", got, err)
	}
}

func TestSmePageURLUsesReverseGeneratedNumbering(t *testing.T) {
	const baseURL = "https://sme.swjtu.edu.cn"
	cases := []struct {
		slug  string
		page  int
		total int
		want  string
	}{
		{"xwtz/xyxw", 1, 177, baseURL + "/xwtz/xyxw.htm"},
		{"xwtz/xyxw", 2, 177, baseURL + "/xwtz/xyxw/176.htm"},
		{"xwtz/xyxw", 177, 177, baseURL + "/xwtz/xyxw/1.htm"},
		{"xwtz/xshd", 2, 16, baseURL + "/xwtz/xshd/15.htm"},
		{"xwtz/xshd", 16, 16, baseURL + "/xwtz/xshd/1.htm"},
		{"dqgz/p/tzgg", 2, 11, baseURL + "/dqgz/p/tzgg/10.htm"},
		{"rcpy/bks/tzgg", 2, 128, baseURL + "/rcpy/bks/tzgg/127.htm"},
		{"rcpy/yjs/tzgg", 2, 152, baseURL + "/rcpy/yjs/tzgg/151.htm"},
		{"kxyj/tzgg", 2, 62, baseURL + "/kxyj/tzgg/61.htm"},
		{"xsgz/tzgg", 2, 43, baseURL + "/xsgz/tzgg/42.htm"},
		{"xwtz/tmzl", 1, 1, baseURL + "/xwtz/tmzl.htm"},
		{"xwtz/gszl", 1, 1, baseURL + "/xwtz/gszl.htm"},
		{"dqgz/j/jwgz", 2, 4, baseURL + "/dqgz/j/jwgz/3.htm"},
		{"dqgz/g/ghdt", 2, 8, baseURL + "/dqgz/g/ghdt/7.htm"},
		{"dqgz/tx/gzdt", 2, 2, baseURL + "/dqgz/tx/gzdt/1.htm"},
		{"xsgz/t/txdt", 2, 39, baseURL + "/xsgz/t/txdt/38.htm"},
		{"xsgz/bjjs", 2, 16, baseURL + "/xsgz/bjjs/15.htm"},
		{"xsgz/gzzd", 2, 4, baseURL + "/xsgz/gzzd/3.htm"},
		{"xygz/byxc", 2, 4, baseURL + "/xygz/byxc/3.htm"},
		{"xygz/xydt", 2, 5, baseURL + "/xygz/xydt/4.htm"},
		{"zsjy/jyzd", 2, 27, baseURL + "/zsjy/jyzd/26.htm"},
		{"g/gnhz", 1, 1, baseURL + "/g/gnhz.htm"},
	}
	for _, tc := range cases {
		if got := smePageURL(baseURL, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("%s page %d/%d URL = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}
