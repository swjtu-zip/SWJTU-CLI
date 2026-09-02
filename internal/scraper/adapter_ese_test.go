package scraper

import (
	"os"
	"strings"
	"testing"
)

const eseTestBaseURL = "https://ese.swjtu.edu.cn"

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
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatal("ESE adapter hooks are incomplete")
	}
	wantSubPages := []string{
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
		{name: "stale-total", slug: "index/xwgg/hjyw", page: 2, total: 1, want: eseTestBaseURL + "/index/xwgg/hjyw/37.htm"},
		{name: "one-page-out-of-range", slug: "djgz/ghgz", page: 2, total: 1, want: eseTestBaseURL + "/djgz/ghgz/2.htm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := esePageURL(eseTestBaseURL, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
				t.Fatalf("esePageURL(%q, %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
			}
		})
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
}
