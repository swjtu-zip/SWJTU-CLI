package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

const kfjtFixtureBaseURL = "https://kfjt.swjtu.edu.cn"

func readKFJTFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read KFJT fixture %q: %v", name, err)
	}
	return body
}

func TestKFJTAdapterRegistrationAndFeeds(t *testing.T) {
	site := AllSites["kfjt"]
	if site == nil || site.BaseURL != kfjtCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("KFJT adapter is incomplete: %#v", site)
	}
	want := []string{
		"xwzx/xwdt", "xwzx/tzgg",
		"djgz/djdt", "djgz/zbdj", "djgz/lxjc",
		"jjgz/jjdt", "jjgz/djfg", "jjgz/jsjy",
		"qywh/qydt", "qywh/whjs", "zcfg", "tzqy/qyjs",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("KFJT feeds = %#v, want %#v", site.SubPages, want)
	}
}

func TestKFJTPageURLUsesReverseGeneratedPages(t *testing.T) {
	if got := kfjtPageURL("http://kfjt.swjtu.edu.cn", "xwzx/xwdt", 1, 14, ".htm"); got != kfjtFixtureBaseURL+"/xwzx/xwdt.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := kfjtPageURL(kfjtFixtureBaseURL, "xwzx/xwdt", 2, 14, ".htm"); got != kfjtFixtureBaseURL+"/xwzx/xwdt/13.htm" {
		t.Fatalf("second page URL = %q", got)
	}
	if got := kfjtPageURL(kfjtFixtureBaseURL, "xwzx/xwdt", 7, 14, ".htm"); got != kfjtFixtureBaseURL+"/xwzx/xwdt/8.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := kfjtPageURL(kfjtFixtureBaseURL, "xwzx/xwdt", 14, 14, ".htm"); got != kfjtFixtureBaseURL+"/xwzx/xwdt/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 14}, {15, 14}, {2, 0}, {2, 1},
	} {
		if got := kfjtPageURL(kfjtFixtureBaseURL, "xwzx/xwdt", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("invalid page %d/%d produced %q", tc.page, tc.total, got)
		}
	}
	if got := kfjtPageURL(kfjtFixtureBaseURL, "index", 2, 1, ".htm"); got != "" {
		t.Fatalf("homepage out-of-range URL = %q", got)
	}
}

func TestKFJTHomepageIncludesHiddenTabsAndFiltersExternalRows(t *testing.T) {
	items, err := parseAdapter_kfjtList(readKFJTFixture(t, "kfjt_home.html"), "http://kfjt.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse KFJT homepage: %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("homepage item count = %d, want six unique local articles: %#v", len(items), items)
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Type]++
		if !strings.HasPrefix(item.URL, kfjtFixtureBaseURL+"/info/") {
			t.Errorf("homepage leaked non-local URL: %#v", item)
		}
	}
	if counts["新闻动态"] != 2 || counts["通知公告"] != 1 || counts["党建动态"] != 1 || counts["纪检动态"] != 1 || counts["企业动态"] != 1 {
		t.Fatalf("homepage type counts = %#v", counts)
	}
	for _, item := range items {
		if item.Title == "首页通知完整标题" && (item.Date != "2026-07-17" || item.PublishedAt != "2026-07-17 09:43:00") {
			t.Fatalf("homepage notification metadata = %#v", item)
		}
		if item.Title == "首页新闻精确时间" && item.PublishedAt != "2026-07-08 16:42:37" {
			t.Fatalf("homepage precise timestamp = %#v", item)
		}
	}
}

func TestKFJTListParsesMetadataAndPagination(t *testing.T) {
	items, err := parseAdapter_kfjtList(readKFJTFixture(t, "kfjt_list.html"), kfjtFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse KFJT list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("local list item count = %d, want 2: %#v", len(items), items)
	}
	if items[0].Title != "列表新闻完整标题" || items[0].Date != "2026-07-17" || items[0].PublishedAt != "2026-07-17 11:28:15" || items[0].Type != "新闻动态" {
		t.Fatalf("first list item metadata = %#v", items[0])
	}
	if items[1].Title != "列表通知标题" || items[1].Date != "2026-07-16" || items[1].Type != "新闻动态" {
		t.Fatalf("second list item metadata = %#v", items[1])
	}
	noticeItems, err := parseAdapter_kfjtList(readKFJTFixture(t, "kfjt_notice_list.html"), kfjtFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse KFJT notice list: %v", err)
	}
	if len(noticeItems) != 1 || noticeItems[0].Type != "通知公告" {
		t.Fatalf("notice list type = %#v, want 通知公告", noticeItems)
	}
	if got := kfjtPaginationTotal(mustKFJTDocument(t, "kfjt_list.html")); got != 14 {
		t.Fatalf("pagination total = %d, want 14", got)
	}
}

func mustKFJTDocument(t *testing.T, name string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(readKFJTFixture(t, name))))
	if err != nil {
		t.Fatalf("parse KFJT fixture document: %v", err)
	}
	return doc
}

func TestKFJTLegalEmptyAndStrictShellRejection(t *testing.T) {
	items, err := parseAdapter_kfjtList(readKFJTFixture(t, "kfjt_empty.html"), kfjtFixtureBaseURL)
	if err != nil {
		t.Fatalf("legal empty KFJT list returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("legal empty KFJT list returned %d items", len(items))
	}
	if _, err := parseAdapter_kfjtList(readKFJTFixture(t, "kfjt_static.html"), kfjtFixtureBaseURL); err == nil {
		t.Fatal("static/service page was accepted as an empty KFJT list")
	}
	if _, err := parseAdapter_kfjtList(readKFJTFixture(t, "kfjt_captcha.html"), kfjtFixtureBaseURL); err == nil {
		t.Fatal("captcha page was accepted as a KFJT list")
	}
	malformed := []byte(`<meta name="pageType" content="2"><div class="main"><div class="right"><div class="list"><ul><li><a href="/info/100/200.htm"></a></li></ul></div></div></div>`)
	if _, err := parseAdapter_kfjtList(malformed, kfjtFixtureBaseURL); err == nil {
		t.Fatal("malformed local KFJT row was silently discarded")
	}
}

func TestKFJTArticleExtractsMetadataBodyImageAndOutsideAttachment(t *testing.T) {
	article, err := parseAdapter_kfjtArticle(readKFJTFixture(t, "kfjt_article.html"), kfjtFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse KFJT article: %v", err)
	}
	if article.Title != "关于科技发展集团工作的完整通知标题" || article.Type != "通知公告" || article.Date != "2026-07-17" || article.PublishedAt != "2026-07-17 09:43:00" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Source != "科发集团" || article.Author != "测试作者" {
		t.Fatalf("article source/author = %q/%q", article.Source, article.Author)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || strings.Contains(article.Content, "页脚噪声") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != kfjtFixtureBaseURL+"/images/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://kfjt.swjtu.edu.cn/images/body.png"`) {
		t.Fatalf("article HTML did not resolve body image: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件：测试文件.docx" || !strings.Contains(article.Attachments[0].URL, "wbfileid=KFJT123") {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
}

func TestKFJTArticleRejectsNonArticleAndCaptchaShells(t *testing.T) {
	if _, err := parseAdapter_kfjtArticle(readKFJTFixture(t, "kfjt_static.html"), kfjtFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as a KFJT article")
	}
	if _, err := parseAdapter_kfjtArticle(readKFJTFixture(t, "kfjt_captcha.html"), kfjtFixtureBaseURL); err == nil {
		t.Fatal("captcha page was accepted as a KFJT article")
	}
}
