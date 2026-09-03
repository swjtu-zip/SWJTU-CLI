package scraper

import (
	"os"
	"strings"
	"testing"
)

func TestDqxyAdapterRegistration(t *testing.T) {
	site, ok := AllSites["dqxy"]
	if !ok {
		t.Fatal("dqxy site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("dqxy adapter hooks are incomplete")
	}
	if len(site.SubPages) != len(dqxySubPages) {
		t.Fatalf("registered sub-pages = %d, want %d", len(site.SubPages), len(dqxySubPages))
	}
	for i, want := range dqxySubPages {
		if site.SubPages[i] != want {
			t.Errorf("sub-page[%d] = %q, want %q", i, site.SubPages[i], want)
		}
	}
	for _, want := range []string{
		"dqgz1/ghgz",
		"dqgz1/ltgz",
		"dqgz1/tzgz",
		"cyxz/zlwj/djgz",
	} {
		found := false
		for _, slug := range site.SubPages {
			if slug == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("confirmed live list route %q is not registered", want)
		}
	}

	const base = "http://dqxy.swjtu.edu.cn"
	if got := site.PageURL(base, "xwdt/qb", 1, 21, ".htm"); got != base+"/xwdt/qb.htm" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 2, 21, ".htm"); got != base+"/xwdt/qb/20.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 21, 21, ".htm"); got != base+"/xwdt/qb/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := site.PageURL(base, "xsgz1/xsfc1", 2, 53, ".htm"); got != base+"/xsgz1/xsfc1/52.htm" {
		t.Fatalf("xsgz1 page 2 URL = %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 2, 0, ".htm"); got != "" {
		t.Fatalf("unknown total must not repeat page 1: %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 22, 21, ".htm"); got != "" {
		t.Fatalf("out-of-range page must not clamp to an old page: %q", got)
	}
	if got := site.PageURL(base, "xwdt/qb", 0, 21, ".htm"); got != "" {
		t.Fatalf("non-positive page must be rejected: %q", got)
	}
}

func TestDqxyPageCountAcceptsOnlyRecognizedListPagination(t *testing.T) {
	paged := []byte(`<main class="tzgg_page"><div class="tzgg_page_base">
<div class="item"><a href="/info/2926/200001.htm">真实文章</a></div>
</div><div class="p_pages"><span class="p_no_d">1</span><span class="p_no"><a href="qb/20.htm">2</a></span><span class="p_no"><a href="qb/1.htm">21</a></span></div></main>`)
	if got := dqxyTotalPagesFromHTML(paged); got != 21 {
		t.Fatalf("paged dqxy total = %d, want 21", got)
	}

	empty := []byte(`<main class="tzgg_page"><div class="tzgg_page_base"><div class="item"></div></div>
<div class="p_pages"><span class="p_no_d">1</span></div></main>`)
	if got := dqxyTotalPagesFromHTML(empty); got != 1 {
		t.Fatalf("empty dqxy total = %d, want 1", got)
	}

	if got := dqxyTotalPagesFromHTML([]byte(`<main><div class="content">静态页面</div></main>`)); got != 0 {
		t.Fatalf("static dqxy page total = %d, want 0", got)
	}
}

func TestDqxyAdapterList(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseDqxyNews(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("got %d list items, want 20", len(items))
	}

	wantFirstTitle := "聚焦前沿交叉，激发创新动能——西南交通大学2026年“智驭交通·能动未来”创新领军工程硕博士论坛顺利举行"
	if items[0].URL != "http://dqxy.swjtu.edu.cn/info/2926/234499.htm" {
		t.Errorf("first URL = %q", items[0].URL)
	}
	if items[0].Title != wantFirstTitle {
		t.Errorf("first title = %q", items[0].Title)
	}
	if items[0].Date != "2026-07-19" {
		t.Errorf("first date = %q", items[0].Date)
	}
	if items[0].Type != "学院新闻" {
		t.Errorf("first type = %q", items[0].Type)
	}
	if items[len(items)-1].URL != "http://dqxy.swjtu.edu.cn/info/2926/233409.htm" {
		t.Errorf("last URL = %q", items[len(items)-1].URL)
	}
	if items[len(items)-1].Date != "2026-05-17" {
		t.Errorf("last date = %q", items[len(items)-1].Date)
	}

	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate URL %q", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, "http://dqxy.swjtu.edu.cn/info/") {
			t.Errorf("non-article URL %q", item.URL)
		}
		if len(item.Date) != 10 || item.Date[4] != '-' || item.Date[7] != '-' {
			t.Errorf("item %q has non-ISO date %q", item.Title, item.Date)
		}
		if item.Title == "全部" || item.Title == "学院新闻" || item.Title == "党建动态" {
			t.Errorf("navigation title leaked: %q", item.Title)
		}
	}
}

func TestDqxyAdapterDoesNotInventYearOrKeepNavigation(t *testing.T) {
	const fixture = `<main class="tzgg_page">
  <div class="team_base tzgg_page_base">
    <div class="item"><a href="../info/2926/200001.htm"><p class="a_block line1"><span class="lanmu">【学院新闻】</span>一条没有完整年份的新闻</p><div class="date">12-31</div></a></div>
  </div>
</main>
<nav><div class="item"><a href="../info/2926/200002.htm">学院新闻</a></div></nav>`

	items, err := parseDqxyNews([]byte(fixture), "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want one article", len(items))
	}
	if items[0].Title != "一条没有完整年份的新闻" {
		t.Fatalf("title = %q", items[0].Title)
	}
	if items[0].Date != "" {
		t.Fatalf("date = %q, want empty because source has only MM-DD", items[0].Date)
	}
}

func TestDqxyAdapterArticle(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_article.html")
	if err != nil {
		t.Fatal(err)
	}
	art, err := parseDqxyArticle(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if art.Title != "聚焦前沿交叉，激发创新动能——西南交通大学2026年“智驭交通·能动未来”创新领军工程硕博士论坛顺利举行" {
		t.Errorf("title = %q", art.Title)
	}
	if art.Date != "2026-07-19" {
		t.Errorf("date = %q, want 2026-07-19", art.Date)
	}
	if art.Type != "学院新闻" {
		t.Errorf("type = %q, want 学院新闻", art.Type)
	}
	if art.PublishedAt != "2026-07-19 16:03:00" {
		t.Errorf("published_at = %q, want source time preserved", art.PublishedAt)
	}
	if len(art.Images) != 5 {
		t.Errorf("images = %d, want 5", len(art.Images))
	}
	if len(art.Content) < 100 {
		t.Errorf("content too short: %d", len(art.Content))
	}
}

func TestDqxyAdapterHomepageScope(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_home.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseDqxyNews(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d homepage items, want 2 bounded news cards", len(items))
	}
	if items[0].URL != "http://dqxy.swjtu.edu.cn/info/2926/234499.htm" {
		t.Errorf("first homepage URL = %q", items[0].URL)
	}
	if items[0].Type != "学院新闻" || items[1].Type != "学院新闻" {
		t.Errorf("homepage types = %q, %q", items[0].Type, items[1].Type)
	}
	for _, item := range items {
		if strings.Contains(item.Title, "导航伪文章") {
			t.Errorf("navigation item leaked into homepage results: %q", item.Title)
		}
	}
}

func TestDqxyAdapterExternalAttachments(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_article_attachments.html")
	if err != nil {
		t.Fatal(err)
	}
	art, err := parseDqxyArticle(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if art.Title != "关于组织院内创新大赛选拔赛的通知" {
		t.Errorf("title = %q", art.Title)
	}
	if art.Date != "2024-05-10" || art.PublishedAt != "2024-05-10 15:33:00" {
		t.Errorf("date/published_at = %q/%q", art.Date, art.PublishedAt)
	}
	if art.Type != "通知公告" {
		t.Errorf("type = %q, want 通知公告", art.Type)
	}
	if len(art.Content) < 20 {
		t.Errorf("content too short: %d", len(art.Content))
	}
	if len(art.Images) != 1 || art.Images[0] != "http://dqxy.swjtu.edu.cn/__local/test.png" {
		t.Errorf("images = %#v", art.Images)
	}
	if len(art.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(art.Attachments))
	}
	if art.Attachments[0].Name != "附件1：报名表.docx" || !strings.Contains(art.Attachments[0].URL, "wbfileid=DOC1") {
		t.Errorf("first attachment = %#v", art.Attachments[0])
	}
	if art.Attachments[1].Name != "附件2：汇总表.xlsx" || !strings.Contains(art.Attachments[1].URL, "wbfileid=XLS2") {
		t.Errorf("second attachment = %#v", art.Attachments[1])
	}
}

func TestDqxyAdapterFiltersPresentationImages(t *testing.T) {
	images := filterDqxyImages([]string{
		"https://dqxy.swjtu.edu.cn/__local/real.png",
		"https://dqxy.swjtu.edu.cn/system/resource/images/fileTypeImages/icon_pdf.gif",
		"https://dqxy.swjtu.edu.cn/doc.gif",
		"https://dqxy.swjtu.edu.cn/spacer.gif",
		"https://dqxy.swjtu.edu.cn/KopPONA92JcsjYP6auanFNnx.files/file0003NULL",
	})
	if len(images) != 1 || images[0] != "https://dqxy.swjtu.edu.cn/__local/real.png" {
		t.Fatalf("filtered images = %#v", images)
	}
}
