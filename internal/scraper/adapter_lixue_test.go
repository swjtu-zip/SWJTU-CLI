package scraper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

func lixueFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return body
}

func lixueHasSubPage(slug string) bool {
	for _, known := range lixueSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func TestLixueAdapterRegistration(t *testing.T) {
	site := AllSites["lixue"]
	if site == nil {
		t.Fatal("lixue site is not registered")
	}
	if site.BaseURL != lixueCanonicalBaseURL {
		t.Fatalf("lixue base URL = %q, want %q", site.BaseURL, lixueCanonicalBaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("lixue adapter did not install all strict parsing hooks")
	}
	for _, required := range []string{
		"xwgg/xytz", "xwgg/xytz/dj", "xwgg/xytz/jx", "xwgg/xytz/ky", "xwgg/xytz/xg", "xwgg/xytz/zj", "xwgg/xytz/my",
		"xwgg/xyxw", "xwgg/ttxw", "xshd", "djgz/djdt", "jyjx/jxxw", "xsky/xkxw", "xsgz/xgtz", "xsgz/xgdt",
		"jwgz/jwdt", "ghgz/ghdt", "xygk/lxcgz", "jlhz/gjzmzjlf", "zsjy/jygz", "xzzq", "yxjd", "sycc",
	} {
		if !lixueHasSubPage(required) {
			t.Errorf("audited feed %q is not registered", required)
		}
	}
	for _, excluded := range []string{"xygk/xyjj", "lxxq", "jwgz/jwgk", "jlhz"} {
		if lixueHasSubPage(excluded) {
			t.Errorf("static/duplicate page %q must not be registered as a feed", excluded)
		}
	}
}

func TestLixuePageURLAndCount(t *testing.T) {
	body := lixueFixture(t, "lixue_list.html")
	if got := lixuePageCountDocument(t, body); got != 3 {
		t.Fatalf("lixue page count = %d, want 3", got)
	}
	base := lixueCanonicalBaseURL
	if got := lixuePageURL(base, "xwgg/xyxw", 1, 3, ".htm"); got != base+"/xwgg/xyxw.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := lixuePageURL(base, "xwgg/xyxw", 2, 3, ".htm"); got != base+"/xwgg/xyxw/2.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := lixuePageURL(base, "xwgg/xyxw", 3, 3, ".htm"); got != base+"/xwgg/xyxw/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := lixuePageURL(base, "xwgg/xyxw", 2, 0, ".htm"); got != "" {
		t.Fatalf("page URL without source total = %q, want refusal", got)
	}
}

func lixuePageCountDocument(t *testing.T, body []byte) int {
	t.Helper()
	doc, err := lixueDocument(body)
	if err != nil {
		t.Fatal(err)
	}
	return lixuePageCount(doc)
}

func lixueDocument(body []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func TestLixueListParsingAndStrictShell(t *testing.T) {
	items, err := parseAdapter_lixueList(lixueFixture(t, "lixue_list.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("parsed %d list items, want 2 local article rows", len(items))
	}
	if items[0].Title != "首条学院新闻" || items[0].Date != "2026-08-21" || items[0].Type != "学院新闻" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[0].URL != lixueCanonicalBaseURL+"/info/1059/10101.htm" {
		t.Fatalf("first list URL = %q", items[0].URL)
	}
	if _, err := parseAdapter_lixueList(lixueFixture(t, "lixue_unrecognized.html"), lixueCanonicalBaseURL); err == nil {
		t.Fatal("unrecognized pageType=2 shell was accepted")
	}
}

func TestLixuePartyListAndEmptyFeed(t *testing.T) {
	items, err := parseAdapter_lixueList(lixueFixture(t, "lixue_party_list.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse party list: %v", err)
	}
	if len(items) != 2 || items[0].Type != "党建动态" || items[0].Date != "2026-06-08" {
		t.Fatalf("party list items = %#v", items)
	}
	items, err = parseAdapter_lixueList(lixueFixture(t, "lixue_empty.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse valid empty feed: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty feed returned %d items", len(items))
	}
}

func TestLixueHomepageModulesAndDeduplication(t *testing.T) {
	items, err := parseAdapter_lixueList(lixueFixture(t, "lixue_home.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse homepage: %v", err)
	}
	if len(items) != 8 {
		t.Fatalf("parsed %d homepage items, want 8 scoped local articles", len(items))
	}
	byURL := make(map[string]model.NewsItem, len(items))
	for _, item := range items {
		byURL[item.URL] = item
	}
	news := byURL[lixueCanonicalBaseURL+"/info/1059/10001.htm"]
	if news.Title != "学院在重大工程中的完整标题" || news.Date != "2026-08-18" {
		t.Fatalf("deduplicated news = %#v", news)
	}
	if byURL[lixueCanonicalBaseURL+"/info/1226/10004.htm"].Type != "党建" {
		t.Fatalf("hidden notice tab type = %#v", byURL[lixueCanonicalBaseURL+"/info/1226/10004.htm"])
	}
	if byURL[lixueCanonicalBaseURL+"/info/1336/10006.htm"].Date != "" {
		t.Fatal("event time was incorrectly treated as publication date")
	}
	if _, exists := byURL[lixueCanonicalBaseURL+"/info/1003/3202.htm"]; exists {
		t.Fatal("footer navigation link was accepted as homepage news")
	}
}

func TestLixueArticleContentMetadataAndAttachments(t *testing.T) {
	article, err := parseAdapter_lixueArticle(lixueFixture(t, "lixue_article.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse article: %v", err)
	}
	if article.Title != "共产党手机报学习链接" || article.Date != "2026-06-08" || article.PublishedAt != "2026-06-08 09:43:00" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Type != "党建动态" {
		t.Fatalf("article type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "请通过线上线下等途径学习使用共产党员手机报") || !strings.Contains(article.Content, "12371.people.com.cn") {
		t.Fatalf("article body was not extracted: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != lixueCanonicalBaseURL+"/__local/1/A2/test.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	if article.Attachments[0].Name != "附件1 考核表.docx" || !strings.Contains(article.Attachments[0].URL, "wbfileid=ABC123") {
		t.Fatalf("first attachment = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "实施细则.pdf" || !strings.Contains(article.Attachments[1].URL, "wbfileid=DEF456") {
		t.Fatalf("second attachment = %#v", article.Attachments[1])
	}
}

func TestLixueURLAndColumnTypeGuards(t *testing.T) {
	if _, ok := lixueArticleURL(lixueCanonicalBaseURL, "https://other.example/info/1059/1.htm"); ok {
		t.Fatal("external article URL was accepted")
	}
	if _, ok := lixueArticleURL(lixueCanonicalBaseURL, "info/1059/1.htm#fragment"); !ok {
		t.Fatal("valid local article URL was rejected")
	}
	if got := lixueTypeForURL("http://lixue.swjtu.edu.cn/info/1261/17233.htm"); got != "党建动态" {
		t.Fatalf("party column type = %q", got)
	}
	if got := lixueTypeForURL("https://lixue.swjtu.edu.cn/info/9999/1.htm"); got != "" {
		t.Fatalf("unknown column type = %q, want empty", got)
	}
}
