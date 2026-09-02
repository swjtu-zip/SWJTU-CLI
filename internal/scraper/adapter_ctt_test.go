package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCttAdapterRegistration(t *testing.T) {
	site, ok := AllSites["ctt"]
	if !ok {
		t.Fatal("ctt site is not registered")
	}
	if site.BaseURL != cttBaseURL {
		t.Fatalf("CTT base URL = %q, want %q", site.BaseURL, cttBaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("CTT adapter hooks are incomplete: %+v", site)
	}
	if !reflect.DeepEqual(site.SubPages, cttSubPages) {
		t.Fatalf("CTT sub-pages = %#v, want %#v", site.SubPages, cttSubPages)
	}
}

func TestCttListParserExtractsCardsAndDates(t *testing.T) {
	body, err := os.ReadFile("testdata/ctt.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseCttNews(body, "http://ctt.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d CTT articles, want 3: %#v", len(items), items)
	}
	wantDates := []string{"2025-11-24", "2025-11-05", "2025-10-23"}
	for i, item := range items {
		if item.Date != wantDates[i] {
			t.Errorf("item %d date = %q, want %q", i, item.Date, wantDates[i])
		}
		if !strings.HasPrefix(item.URL, "http://ctt.swjtu.edu.cn/info/") {
			t.Errorf("item %d has unexpected URL: %q", i, item.URL)
		}
	}
	for _, item := range items {
		if item.URL == "http://ctt.swjtu.edu.cn/info/3511/70731.htm" || item.URL == "http://ctt.swjtu.edu.cn/info/3511/99999.htm" {
			t.Errorf("navigation/banner link leaked: %#v", item)
		}
	}
}

func TestCttHomeParserUsesLatestNewsContainer(t *testing.T) {
	body, err := os.ReadFile("testdata/ctt_home.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseCttNews(body, "http://ctt.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d CTT home articles, want 2: %#v", len(items), items)
	}
	if items[0].URL != "http://ctt.swjtu.edu.cn/228032.htm" || items[1].URL != "http://ctt.swjtu.edu.cn/228062.htm" {
		t.Fatalf("unexpected home URLs: %#v", items)
	}
	if items[0].Date != "06-18" || items[1].Date != "06-18" {
		t.Fatalf("unexpected home dates: %#v", items)
	}
}

func TestCttHomeParserUsesCurrentHomeModules(t *testing.T) {
	html := []byte(`<html><body>
<div class="news-list"></div>
<ul class="innews">
  <li><div class="pic"><a href="info/3421/228032.htm"><img src="cover.jpg"></a></div><div class="info">
    <a class="line2" href="info/3421/228032.htm" title="交运要闻">交运要闻</a>
    <time>2026-06-18</time>
  </div></li>
</ul>
<div class="rowb"><div class="list"><ul>
  <li><a class="texto" href="info/3521/228452.htm">院务公告</a><span>2026-07-17</span></li>
</ul></div></div>
</body></html>`)
	items, err := parseCttNews(html, "http://ctt.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d current home articles, want 2: %#v", len(items), items)
	}
	if items[0].Date != "2026-06-18" || items[1].Date != "2026-07-17" {
		t.Fatalf("unexpected current home dates: %#v", items)
	}
	if items[0].Type != "交运要闻" {
		t.Fatalf("unexpected current home type: %#v", items[0])
	}
}

func TestCttHomeParserCollectsEveryHiddenTab(t *testing.T) {
	body := readCttFixture(t, "ctt_home_live.html")
	items, err := parseCttNews(body, cttBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 12 {
		t.Fatalf("got %d CTT home articles, want 12: %#v", len(items), items)
	}
	wantTypes := map[string]int{
		"交运要闻":  4,
		"院务公告":  2,
		"本科教学":  2,
		"研究生教育": 2,
		"学生工作":  2,
	}
	gotTypes := make(map[string]int)
	for _, item := range items {
		gotTypes[item.Type]++
		if !strings.HasPrefix(item.URL, cttBaseURL+"/info/") {
			t.Errorf("unexpected home URL: %q", item.URL)
		}
		if strings.Contains(item.URL, "/3001/") || strings.Contains(item.URL, "/5591/") {
			t.Errorf("banner/platform link leaked into home feed: %#v", item)
		}
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("home item types = %#v, want %#v", gotTypes, wantTypes)
	}
}

func TestCttListParserReadsClassificationAndTotalPages(t *testing.T) {
	body := readCttFixture(t, "ctt_list_live.html")
	items, err := parseCttNews(body, cttBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d CTT list articles, want 2: %#v", len(items), items)
	}
	for _, item := range items {
		if item.Type != "学院通知" {
			t.Errorf("item type = %q, want 学院通知", item.Type)
		}
		if item.PublishedAt != "" {
			t.Errorf("date-only list unexpectedly has a clock: %#v", item)
		}
	}
	if items[0].Date != "2026-07-17" || items[1].Date != "2026-04-13" {
		t.Fatalf("unexpected CTT list dates: %#v", items)
	}
	if got := cttTotalPagesFromHTML(body); got != 44 {
		t.Fatalf("CTT total pages = %d, want 44", got)
	}
}

func TestCttEmptyListIsAValidEmptyResult(t *testing.T) {
	body := readCttFixture(t, "ctt_empty.html")
	items, err := parseCttNews(body, cttBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("empty CTT list returned %#v", items)
	}
	if got := cttTotalPagesFromHTML(body); got != 1 {
		t.Fatalf("empty CTT list total pages = %d, want 1", got)
	}
}

func TestCttPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	const baseURL = "http://ctt.swjtu.edu.cn"
	if got := cttPageURL(baseURL, "tzgg/xytz", 1, 20, ".htm"); got != baseURL+"/tzgg/xytz.htm" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := cttPageURL(baseURL, "tzgg/xytz", 2, 20, ".htm"); got != baseURL+"/tzgg/xytz/19.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
	if got := cttPageURL(baseURL, "tzgg/xytz", 20, 20, ".htm"); got != baseURL+"/tzgg/xytz/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
}

func TestCttArticleParserReadsPublishDate(t *testing.T) {
	html := []byte(`<html><body>
<div class="left-top"><h3 class="news-title">CTT 测试文章</h3>
<div class="top-info"><span>来源：交通运输与物流学院</span><span>发布日期：2026-05-21</span></div></div>
<div class="left-main"><div class="v_news_content"><p>这是一段足够长的测试正文，用于验证文章解析仍然沿用公共内容提取逻辑。</p></div></div>
</body></html>`)

	article, err := parseCttArticle(html, "http://ctt.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-05-21" {
		t.Fatalf("article date = %q, want 2026-05-21", article.Date)
	}
}

func TestCttArticleParserExtractsResourcesAndPreciseMetadata(t *testing.T) {
	article, err := parseCttArticle(readCttFixture(t, "ctt_article_live.html"), cttBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "CTT 详情测试" || article.Source != "交通运输与物流学院" || article.Type != "学院通知" {
		t.Fatalf("unexpected CTT article metadata: %#v", article)
	}
	if article.Date != "2026-08-29" || article.PublishedAt != "2026-08-29 09:27:00" {
		t.Fatalf("unexpected CTT article date: %#v", article)
	}
	if !strings.Contains(article.Content, "正文第一段") {
		t.Errorf("article content = %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != cttBaseURL+"/images/body.jpg" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("article attachments = %#v, want one deduplicated attachment", article.Attachments)
	}
	if article.Attachments[0].Name != "通知附件.pdf" || !strings.HasPrefix(article.Attachments[0].URL, cttBaseURL+"/system/_content/download.jsp?") {
		t.Fatalf("article attachment = %#v", article.Attachments[0])
	}
	if !strings.Contains(article.ContentHTML, cttBaseURL+"/images/body.jpg") || !strings.Contains(article.ContentHTML, cttBaseURL+"/system/_content/download.jsp?") {
		t.Errorf("resolved resources missing from ContentHTML: %s", article.ContentHTML)
	}
}

func TestCttArticleParserRejectsShells(t *testing.T) {
	shell := []byte(`<html><head><meta name="pageType" content="1"><title>CTT 首页</title></head><body><div class="v_news_content"><p>壳页面</p></div></body></html>`)
	if _, err := parseCttArticle(shell, cttBaseURL); err == nil {
		t.Fatal("non-article pageType was accepted as an article")
	}
	captcha := []byte(`<html><head><title>附件下载</title></head><body><h1>请输入验证码下载附件</h1></body></html>`)
	if _, err := parseCttArticle(captcha, cttBaseURL); err == nil {
		t.Fatal("captcha HTML was accepted as an article")
	}
}

func readCttFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
