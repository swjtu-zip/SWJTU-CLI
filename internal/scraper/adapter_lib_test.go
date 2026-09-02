package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func readLibFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read lib fixture %q: %v", name, err)
	}
	return body
}

func TestLibAdapterRegistrationAndCanonicalPagination(t *testing.T) {
	site := AllSites["lib"]
	if site == nil || site.BaseURL != libCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("lib adapter is incomplete: %#v", site)
	}
	wantFeeds := []string{
		"home/service/1005/0", "home/service/0/2564", "home/service/0/1165", "home/service/0/2561",
		"home/service/0/2563", "home/service/0/1006", "lecture/1007", "news/newslist/1007",
		"news/newslist/1020", "news/newslist/1021",
	}
	if !reflect.DeepEqual(site.SubPages, wantFeeds) {
		t.Fatalf("lib feeds = %#v, want %#v", site.SubPages, wantFeeds)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "home/service/1005/0", 1, 132, ".htm"); got != libCanonicalBaseURL+"/home/service/1005/0?page=1" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "home/service/1005/0", 66, 132, ".html"); got != libCanonicalBaseURL+"/home/service/1005/0?page=66" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "home/service/1005/0", 132, 132, ".htm"); got != libCanonicalBaseURL+"/home/service/1005/0?page=132" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "index", 1, 1, ".htm"); got != libCanonicalBaseURL+"/" {
		t.Fatalf("homepage URL = %q", got)
	}
}

func TestLibHomepageParsesEveryArticleModuleAndDropsResources(t *testing.T) {
	items, err := parseAdapter_libList(readLibFixture(t, "lib_home.html"), "http://www.lib.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse lib homepage: %v", err)
	}
	if len(items) != 7 {
		t.Fatalf("homepage items = %d, want 7 (2 lecture JSON + exhibition + activity + 2 news + carousel)", len(items))
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Type]++
		if !strings.HasPrefix(item.URL, libCanonicalBaseURL+"/") {
			t.Errorf("homepage item escaped canonical host: %#v", item)
		}
		if strings.Contains(item.URL, "/pagelinklog/") || strings.Contains(item.URL, "/link/") {
			t.Errorf("homepage retained resource link: %#v", item)
		}
	}
	if counts["讲座"] != 2 || counts["展览"] != 1 || counts["活动"] != 1 || counts["最新动态"] != 1 || counts["公示"] != 1 || counts["图片新闻"] != 1 {
		t.Fatalf("homepage type counts = %#v", counts)
	}
	for _, item := range items {
		if item.Title == "带精确时间的讲座" {
			if item.Date != "2026-08-31" || item.PublishedAt != "2026-08-31 14:03:05" {
				t.Fatalf("precise homepage metadata = %#v", item)
			}
			return
		}
	}
	t.Fatal("homepage did not retain the full lecture JSON record")
}

func TestLibKeepsLegitimateLectureHeadlineEndingInBookTitleMark(t *testing.T) {
	if !libIsArticleTitle("高价值专利培育系列讲座之《面向研发活动的专利布局及案例分享》") {
		t.Fatal("legitimate lib headline ending in 》 was rejected")
	}
}

func TestLibListParsesAllRowsAndSourcePageCount(t *testing.T) {
	items, err := parseAdapter_libList(readLibFixture(t, "lib_list.html"), libCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse lib list: %v", err)
	}
	if len(items) != 10 {
		t.Fatalf("list items = %d, want 10 local rows", len(items))
	}
	if items[0].URL != libCanonicalBaseURL+"/Home/ServiceDetail/9001" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31" || items[0].Type != "最新动态" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if pages, known := libPageCountFromFixture(readLibFixture(t, "lib_list.html")); !known || pages != 132 {
		t.Fatalf("page count = %d/%v, want 132/true", pages, known)
	}
}

func TestLibLegacyTabsRemainSeparateArticleLists(t *testing.T) {
	items, err := parseAdapter_libList(readLibFixture(t, "lib_legacy_list.html"), libCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse lib exhibition list: %v", err)
	}
	if len(items) != 2 || items[0].Type != "展览" || items[0].URL != libCanonicalBaseURL+"/news/newsdetail/5887" {
		t.Fatalf("legacy tab items = %#v", items)
	}
	if pages, known := libPageCountFromFixture(readLibFixture(t, "lib_legacy_list.html")); !known || pages != 3 {
		t.Fatalf("legacy tab page count = %d/%v, want 3/true", pages, known)
	}
}

func TestLibArticleParsesMetadataBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_libArticle(readLibFixture(t, "lib_article.html"), "http://www.lib.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse lib article: %v", err)
	}
	if article.Title != "图书馆详情测试标题" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:03:05" || article.Type != "最新动态" || article.Source != "图书馆" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "完整正文段落") || !strings.Contains(article.ContentHTML, `src="https://www.lib.swjtu.edu.cn/images/body.jpg"`) {
		t.Fatalf("article body/resources not retained: content=%q html=%q", article.Content, article.ContentHTML)
	}
	if len(article.Images) != 1 || article.Images[0] != libCanonicalBaseURL+"/images/body.jpg" {
		t.Fatalf("article images = %#v", article.Images)
	}
	want := map[string]string{
		"附件1 PDF": libCanonicalBaseURL + "/files/notice.pdf",
		"附件2":     libCanonicalBaseURL + "/download?id=1",
	}
	if len(article.Attachments) != len(want) {
		t.Fatalf("article attachments = %#v, want %#v", article.Attachments, want)
	}
	for _, attachment := range article.Attachments {
		if want[attachment.Name] != attachment.URL {
			t.Errorf("unexpected attachment = %#v", attachment)
		}
	}
}

func TestLibEmptyAndNonArticleResponsesAreStrict(t *testing.T) {
	items, err := parseAdapter_libList(readLibFixture(t, "lib_empty_list.html"), libCanonicalBaseURL)
	if err != nil {
		t.Fatalf("legal empty lib list returned error: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("legal empty lib list = %#v, want non-nil empty slice", items)
	}
	for _, name := range []string{"lib_static.html", "lib_captcha.html"} {
		if items, err := parseAdapter_libList(readLibFixture(t, name), libCanonicalBaseURL); err == nil || items != nil {
			t.Fatalf("%s was accepted as a list: items=%#v err=%v", name, items, err)
		}
		if article, err := parseAdapter_libArticle(readLibFixture(t, name), libCanonicalBaseURL); err == nil || article != nil {
			t.Fatalf("%s was accepted as an article: article=%#v err=%v", name, article, err)
		}
	}
}

func libPageCountFromFixture(raw []byte) (int, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0, false
	}
	return libPageCount(doc)
}
