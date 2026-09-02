package scraper

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

func loadTestHTML(filename string) []byte {
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		return nil
	}
	return data
}

func TestListNewsRangePassesLogicalPagesToCustomListPage(t *testing.T) {
	var pages []int
	site := &BodaSite{
		ListPageFunc: func(_ string, page int) ([]model.NewsItem, int, error) {
			pages = append(pages, page)
			items := map[int][]model.NewsItem{
				1: {{Title: "最新文章", URL: "https://example.com/info/1/1.htm", Date: "2026-08-01"}},
				2: {{Title: "次新文章", URL: "https://example.com/info/1/2.htm", Date: "2026-07-01"}},
				3: {{Title: "历史文章", URL: "https://example.com/info/1/3.htm", Date: "2025-01-01"}},
			}
			return items[page], 3, nil
		},
	}
	items, err := site.ListNewsRange("news", "2025-09-01", "2026-09-01", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d range items, want 2", len(items))
	}
	if strings.Join(intsToStrings(pages), ",") != "1,1,2,3" {
		t.Fatalf("custom page calls = %v, want [1 1 2 3]", pages)
	}
}

func TestParseDateTreatsMonthDayAsUnknown(t *testing.T) {
	if !parseDate("06-14").IsZero() || !parseDate("09.01").IsZero() {
		t.Fatal("partial month-day values must not be compared as year zero")
	}
}

func intsToStrings(values []int) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = fmt.Sprint(value)
	}
	return result
}

func TestArticleParserKeepsArchiveHTMLAndResolvesResources(t *testing.T) {
	html := []byte(`<html><body>
<h1 class="news_tit">测试文章</h1>
<div class="news_source"><span class="date">2026-08-31</span></div>
<div class="v_news_content"><p>正文内容</p><img src="/images/photo.jpg"><a href="/upload/file.pdf">附件</a></div>
</body></html>`)
	article, err := parseBodaArticle(html, "https://news.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Content != "正文内容" {
		t.Fatalf("unexpected text content: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://news.swjtu.edu.cn/images/photo.jpg" {
		t.Fatalf("unexpected images: %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != "https://news.swjtu.edu.cn/upload/file.pdf" {
		t.Fatalf("unexpected attachments: %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, `src="https://news.swjtu.edu.cn/images/photo.jpg"`) || !strings.Contains(article.ContentHTML, `href="https://news.swjtu.edu.cn/upload/file.pdf"`) {
		t.Fatalf("archive HTML did not retain resolved resources: %s", article.ContentHTML)
	}
}

func TestArticleParserKeepsListTableAndLazyImageContent(t *testing.T) {
	html := []byte(`<html><body>
<h1 class="news_tit">列表和表格文章</h1>
<div class="v_news_content">
<div>第一段普通文本。</div>
<p>第二段正文。</p>
<ul><li>第三项正文。</li><li><p>第四段正文。</p></li></ul>
<table><tbody><tr><td>第五格</td><td>第六格</td></tr></tbody></table>
<img src="data:image/gif;base64,placeholder" data-src="/images/lazy.png">
<img data-original="/images/lazy.png">
</div></body></html>`)
	article, err := parseBodaArticle(html, "https://news.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	want := "第一段普通文本。\n\n第二段正文。\n\n第三项正文。\n\n第四段正文。\n\n第五格\n\n第六格"
	if article.Content != want {
		t.Fatalf("content = %q, want %q", article.Content, want)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://news.swjtu.edu.cn/images/lazy.png" {
		t.Fatalf("images = %#v", article.Images)
	}
}

func TestArticleParserPreservesPublishedTimeAndType(t *testing.T) {
	html := []byte(`<html><body>
<h1 class="news_tit">带发布时间的文章</h1>
<div class="news_source"><span>发布时间：2026-08-31 14:34</span><span>类型：通知公告</span></div>
<div class="v_news_content"><p>这是一段足够长的正文，用来验证发布时间和类型不会从元数据中丢失。</p></div>
</body></html>`)
	article, err := parseBodaArticle(html, "https://news.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" || article.Type != "通知公告" {
		t.Fatalf("unexpected precise metadata: date=%q published_at=%q type=%q", article.Date, article.PublishedAt, article.Type)
	}
}

func TestBodaFallbackCleansListTitlesAndFindsSectionTypes(t *testing.T) {
	if got := cleanTitle("09.302024 技术转移研究院2024年国庆节值班安排表"); got != "技术转移研究院2024年国庆节值班安排表" {
		t.Fatalf("cleaned title = %q", got)
	}
	if isValidNewsTitle("了解更多") {
		t.Fatal("homepage action link was accepted as an article title")
	}
	tests := []struct {
		name string
		html string
		want string
	}{
		{name: "breadcrumb", html: `<div class="loaction">首页 &gt; <a href="/xyxw.htm">学院新闻</a> &gt; 正文</div><h1>文章标题</h1>`, want: "学院新闻"},
		{name: "section heading", html: `<div class="news-list-wrap"><div class="hd"><h3>通知公告</h3></div></div><h1>文章标题</h1>`, want: "通知公告"},
		{name: "vue breadcrumb", html: `<div class="el-breadcrumb"><div class="container">首页 &gt; 新闻资讯 &gt; 新闻动态 &gt; <span>文章详情</span></div></div><h1>文章标题</h1>`, want: "新闻动态"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			if err != nil {
				t.Fatal(err)
			}
			if got := bodaArticleSectionType(doc, "文章标题"); got != tt.want {
				t.Fatalf("section type = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTotalPagesUsesExplicitCounterAndLastPage(t *testing.T) {
	if got := parseTotalPages([]byte(`<div class="p_t">第1/78页</div>`)); got != 78 {
		t.Fatalf("explicit counter = %d, want 78", got)
	}
	if got := parseTotalPages([]byte(`<div class="p_pages"><span class="p_last"><a href="1.htm">尾页</a></span><a href="7.htm">7</a></div>`)); got != 7 {
		t.Fatalf("link counter = %d, want 7", got)
	}
}

// TestJdywParse tests parsing of 交大要闻
func TestJdywParse(t *testing.T) {
	html := loadTestHTML("jdyw.html")
	if html == nil {
		t.Skip("testdata/jdyw.html not found")
	}
	items, err := parseBodaNewsList(html, "https://news.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 5 {
		n = 5
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
		if item.Title == "" {
			t.Errorf("item %d: empty title", i+1)
		}
		if item.URL == "" {
			t.Errorf("item %d: empty URL", i+1)
		}
	}
}

// TestCivilParse tests parsing of 土木学院
func TestCivilParse(t *testing.T) {
	html := loadTestHTML("civil.html")
	if html == nil {
		t.Skip("testdata/civil.html not found")
	}
	items, err := parseBodaNewsList(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 10 {
		n = 10
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check no banner content
	for i, item := range items {
		if containsAny(item.Title, "习近平给四所交通大学全体师生回信") {
			t.Errorf("item %d: banner content leaked: %s", i+1, item.Title)
		}
	}
}

// TestClxyParse tests parsing of 材料学院
func TestClxyParse(t *testing.T) {
	html := loadTestHTML("clxy.html")
	if html == nil {
		t.Skip("testdata/clxy.html not found")
	}
	items, err := parseBodaNewsList(html, "http://clxy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 10 {
		n = 10
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check no navigation links
	for i, item := range items {
		if item.Title == "院务公开" || item.Title == "联系方式" {
			t.Errorf("item %d: navigation link leaked: %s", i+1, item.Title)
		}
	}
	// Check no external links
	for i, item := range items {
		if containsAny(item.URL, "jiuye.swjtu.edu.cn") {
			t.Errorf("item %d: external link leaked: %s", i+1, item.URL)
		}
	}
}

// TestScyshjParse tests parsing of 营商环境研究院
func TestScyshjParse(t *testing.T) {
	html := loadTestHTML("scyshj.html")
	if html == nil {
		t.Skip("testdata/scyshj.html not found")
	}
	items, err := parseBodaNewsList(html, "https://scyshj.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 5 {
		n = 5
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check no date prefix in title
	for i, item := range items {
		if hasDatePrefix(item.Title) {
			t.Errorf("item %d: date prefix in title: %s", i+1, item.Title)
		}
	}
}

// TestYgbParse tests parsing of 党委研究生工作部
func TestYgbParse(t *testing.T) {
	html := loadTestHTML("ygb.html")
	if html == nil {
		t.Skip("testdata/ygb.html not found")
	}
	items, err := parseBodaNewsList(html, "https://ygb.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 5 {
		n = 5
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
		if item.Title == "" {
			t.Errorf("item %d: empty title", i+1)
		}
	}
}

// TestXczxParse tests parsing of 乡村振兴研究院
func TestXczxParse(t *testing.T) {
	html := loadTestHTML("xczx.html")
	if html == nil {
		t.Skip("testdata/xczx.html not found")
	}
	items, err := parseBodaNewsList(html, "https://xczx.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 5 {
		n = 5
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check no date prefix in title
	for i, item := range items {
		if hasDatePrefix(item.Title) {
			t.Errorf("item %d: date prefix in title: %s", i+1, item.Title)
		}
	}
}

// TestEseParse tests parsing of 环境学院 (dedicated parser)
func TestEseParse(t *testing.T) {
	html := loadTestHTML("ese.html")
	if html == nil {
		t.Skip("testdata/ese.html not found")
	}
	items, err := parseEseNews(html, "https://ese.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 5 {
		n = 5
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check dates are extracted
	hasDate := false
	for _, item := range items {
		if item.Date != "" {
			hasDate = true
		}
		// Check no date in title
		if hasDatePrefix(item.Title) {
			t.Errorf("date prefix in title: %s", item.Title)
		}
		// Check no full article content in title
		if len(item.Title) > 100 {
			t.Errorf("title too long (full article?): %s...", item.Title[:80])
		}
	}
	if !hasDate {
		t.Error("no dates extracted from ese")
	}
}

// TestSportsParse tests parsing of 体育学院 (dedicated parser)
func TestSportsParse(t *testing.T) {
	html := loadTestHTML("sports.html")
	if html == nil {
		t.Skip("testdata/sports.html not found")
	}
	items, err := parseSportsNews(html, "http://sports.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 5 {
		n = 5
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check no banner content
	for i, item := range items {
		if containsAny(item.Title, "习近平给四所交通大学全体师生") {
			t.Errorf("item %d: banner content: %s", i+1, item.Title)
		}
	}
	// Check dates are extracted
	hasDate := false
	for _, item := range items {
		if item.Date != "" {
			hasDate = true
		}
	}
	if !hasDate {
		t.Error("no dates extracted from sports")
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func hasDatePrefix(s string) bool {
	// Check for patterns like "07/16", "2025-08-12", "06-01"
	if len(s) < 5 {
		return false
	}
	// 07/16 pattern
	if s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9' && s[2] == '/' && s[3] >= '0' && s[3] <= '9' && s[4] >= '0' && s[4] <= '9' {
		return true
	}
	// 2025-08-12 pattern
	if len(s) >= 10 && s[0] == '2' && s[1] == '0' && s[4] == '-' && s[7] == '-' {
		return true
	}
	return false
}
