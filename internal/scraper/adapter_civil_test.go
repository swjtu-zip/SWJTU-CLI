package scraper

import (
	"os"
	"regexp"
	"testing"

	"swjtu-cli/internal/model"
)

func TestCivilAdapterRegistered(t *testing.T) {
	site := AllSites["civil"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatal("civil adapter hooks are not registered")
	}
	if len(site.SubPages) < 2 || site.SubPages[0] != "tzgg" || site.SubPages[1] != "tmyw" {
		t.Fatalf("unexpected civil sub-pages: %#v", site.SubPages)
	}
}

func TestCivilAdapterListSnapshot(t *testing.T) {
	html, err := os.ReadFile("testdata/civil.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseCivilNews(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	legacyItems, err := parseBodaNewsList(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	legacyDates := 0
	for _, item := range legacyItems {
		if item.Date != "" {
			legacyDates++
		}
	}

	// The snapshot has six featured items that are not banner links and five
	// six-item category panels. Two featured entries are the site's known hero
	// announcements and are intentionally filtered by isValidNewsTitle.
	if got, want := len(items), 36; got != want {
		t.Fatalf("parsed %d items, want %d", got, want)
	}
	if len(items) <= len(legacyItems) {
		t.Fatalf("adapter did not improve coverage: legacy=%d adapter=%d", len(legacyItems), len(items))
	}
	t.Logf("legacy parser: %d items/%d dates; civil adapter: %d items", len(legacyItems), legacyDates, len(items))

	dateRe := regexp.MustCompile(`^20\d{2}-\d{2}-\d{2}$`)
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if !dateRe.MatchString(item.Date) {
			t.Errorf("%q has non-normalized date %q", item.Title, item.Date)
		}
		if seen[item.URL] {
			t.Errorf("duplicate URL %q", item.URL)
		}
		seen[item.URL] = true
		if item.URL == "http://civil.swjtu.edu.cn/info/1152/70532.htm" || item.URL == "http://civil.swjtu.edu.cn/info/1161/71392.htm" {
			t.Errorf("banner URL leaked into list: %q", item.URL)
		}
	}

	want := map[string]struct {
		title string
		date  string
	}{
		"http://civil.swjtu.edu.cn/info/1152/72042.htm": {"土木工程学院举办2026届毕业生党员大会", "2026-05-29"},
		"http://civil.swjtu.edu.cn/info/1048/72392.htm": {"2027年土木工程学院研究生导师资格遴选与上岗聘任办法公示", "2026-06-12"},
		"http://civil.swjtu.edu.cn/info/1057/71882.htm": {"土木工程学院2026-2028年（本科）教育教学研究与改革项目申报公示", "2026-05-25"},
	}
	got := make(map[string]model.NewsItem, len(items))
	for _, item := range items {
		got[item.URL] = item
	}
	for url, expected := range want {
		item, ok := got[url]
		if !ok {
			t.Fatalf("missing expected item %q", url)
		}
		if item.Title != expected.title || item.Date != expected.date {
			t.Errorf("item %q = (%q, %q), want (%q, %q)", url, item.Title, item.Date, expected.title, expected.date)
		}
	}
}

func TestCivilAdapterCategoryCardDateAndNavigationScope(t *testing.T) {
	html, err := os.ReadFile("testdata/civil_list_live.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseCivilNews(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("parsed %d items, want 2", len(items))
	}
	if items[0].Title != "关于开展真实通知" || items[0].Date != "2026-08-01" {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[1].Title != "真实科研消息" || items[1].Date != "2026-07-27" {
		t.Fatalf("second item = %#v", items[1])
	}
	for _, item := range items {
		if item.Title == "导航伪文章链接" {
			t.Fatalf("navigation link leaked into items: %#v", item)
		}
	}
}

func TestCivilAdapterArticleMeta(t *testing.T) {
	html := []byte(`<html><body>
<main>
  <h3>土木工程学院举办2026届毕业生党员大会</h3>
  <div class="top-info">2026-05-29 11:50:46 来源：土木工程学院 分类： 阅读量：</div>
  <div class="v_news_content"><p>正文内容。</p></div>
</main>
</body></html>`)
	article, err := parseCivilArticle(html, "https://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "土木工程学院举办2026届毕业生党员大会" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2026-05-29" {
		t.Fatalf("date = %q, want 2026-05-29", article.Date)
	}
	if article.Content != "正文内容。" {
		t.Fatalf("content = %q", article.Content)
	}
}

func TestCivilAdapterLiveDetailShape(t *testing.T) {
	html, err := os.ReadFile("testdata/civil_article_live.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseCivilArticle(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "土木工程学院2026年暑期实习安排" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2026-06-26" {
		t.Fatalf("date = %q, want 2026-06-26", article.Date)
	}
	if article.PublishedAt != "2026-06-26 10:03:47" {
		t.Fatalf("published_at = %q, want source seconds preserved", article.PublishedAt)
	}
	if article.Source != "" || article.Type != "" || article.Author != "" {
		t.Fatalf("empty civil metadata was not kept empty: source=%q type=%q author=%q", article.Source, article.Type, article.Author)
	}
	if article.Content != "各位老师、同学：\n\n2026年土木工程学院认识实习与生产实习的具体安排详见附件。" {
		t.Fatalf("content = %q", article.Content)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("attachments = %#v, want one CMS attachment", article.Attachments)
	}
	if article.Attachments[0].Name != "土木工程学院认识实习安排表.xlsx" || article.Attachments[0].URL != "http://civil.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1796209034&wbfileid=A875E791E31D455E18F3B471CC9C07BC" {
		t.Fatalf("attachment = %#v", article.Attachments[0])
	}
	if len(article.Images) != 1 || article.Images[0] != "http://civil.swjtu.edu.cn/__local/ABC/image.jpg" {
		t.Fatalf("images = %#v", article.Images)
	}
}

func TestCivilPageURL(t *testing.T) {
	base := "http://civil.swjtu.edu.cn"
	if got := civilPageURL(base, "tzgg", 1, 197, ".htm"); got != base+"/tzgg.htm" {
		t.Errorf("page 1 URL = %q", got)
	}
	if got := civilPageURL(base, "tzgg", 2, 197, ".htm"); got != base+"/tzgg/196.htm" {
		t.Errorf("page 2 URL = %q", got)
	}
	if got := civilPageURL(base, "tzgg", 197, 197, ".htm"); got != base+"/tzgg/1.htm" {
		t.Errorf("last page URL = %q", got)
	}
}
