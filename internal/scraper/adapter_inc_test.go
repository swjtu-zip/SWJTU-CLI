package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func readINCFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read INC fixture %q: %v", name, err)
	}
	return body
}

func TestINCAdapterRegistrationAndFeeds(t *testing.T) {
	site := AllSites["inc"]
	if site == nil || site.BaseURL != incBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("INC adapter is incomplete: %#v", site)
	}
	want := []string{
		"xwdt", "tzgg", "wlaq", "zbrj", "djgz", "zcfg", "xsfw", "jsfw", "gdwfw", "wxwl", "english/News_Event",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("INC feeds = %#v, want %#v", site.SubPages, want)
	}
}

func TestINCReversePaginationAndTotalPages(t *testing.T) {
	if got := incPageURL(incBaseURL, "xwdt", 1, 12, ".htm"); got != incBaseURL+"/xwdt.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := incPageURL(incBaseURL, "xwdt", 2, 12, ".htm"); got != incBaseURL+"/xwdt/11.htm" {
		t.Fatalf("second page URL = %q", got)
	}
	if got := incPageURL(incBaseURL, "xwdt", 12, 12, ".htm"); got != incBaseURL+"/xwdt/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got, err := incTotalPagesFromHTML(readINCFixture(t, "inc_list.html"), "xwdt"); err != nil || got != 12 {
		t.Fatalf("total pages = %d, err=%v; want 12", got, err)
	}
}

func TestINCHomepageScopesArticleModulesAndFiltersServices(t *testing.T) {
	items, err := parseAdapter_incList(readINCFixture(t, "inc_home.html"), incBaseURL)
	if err != nil {
		t.Fatalf("parse INC homepage: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("homepage item count = %d, want 4", len(items))
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Type]++
		if !strings.HasPrefix(item.URL, incBaseURL+"/info/") {
			t.Errorf("homepage leaked non-local article URL: %q", item.URL)
		}
		if item.Title == "" || item.Date == "" || item.Type == "" {
			t.Errorf("homepage item has incomplete metadata: %+v", item)
		}
	}
	if counts["新闻动态"] != 2 || counts["通知公告"] != 2 {
		t.Fatalf("homepage type counts = %#v, want news=2 notices=2", counts)
	}
}

func TestINCListParsesRowsMetadataAndExcludesExternal(t *testing.T) {
	items, err := parseAdapter_incList(readINCFixture(t, "inc_list.html"), incBaseURL)
	if err != nil {
		t.Fatalf("parse INC list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("local list item count = %d, want 2", len(items))
	}
	if items[0].Title != "列表中的第一条通知" || items[0].Date != "2026-08-25" || items[0].PublishedAt != "2026-08-25" || items[0].Type != "通知公告" {
		t.Fatalf("first list item metadata = %+v", items[0])
	}
	if items[1].URL != incBaseURL+"/info/1022/4809.htm" {
		t.Fatalf("second list URL = %q", items[1].URL)
	}
}

func TestINCEnglishNewsEventList(t *testing.T) {
	items, err := parseAdapter_incList(readINCFixture(t, "inc_english_list.html"), incBaseURL)
	if err != nil {
		t.Fatalf("parse INC English list: %v", err)
	}
	if len(items) != 2 || items[0].Type != "News Event" || items[0].Date != "2018-11-07" {
		t.Fatalf("English list items = %#v", items)
	}
}

func TestINCEmptyResourceAndBlockedShells(t *testing.T) {
	items, err := parseAdapter_incList(readINCFixture(t, "inc_empty.html"), incBaseURL)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("explicit empty list = %#v, err=%v", items, err)
	}
	for _, name := range []string{"inc_static.html", "inc_resource.html", "inc_captcha.html"} {
		if _, err := parseAdapter_incList(readINCFixture(t, name), incBaseURL); err == nil {
			t.Errorf("%s was accepted as an article list", name)
		}
	}
}

func TestINCArticleExtractsMetadataBodyImagesAndOutsideAttachment(t *testing.T) {
	article, err := parseAdapter_incArticle(readINCFixture(t, "inc_article.html"), incBaseURL)
	if err != nil {
		t.Fatalf("parse INC article: %v", err)
	}
	if article.Title != "正文通知标题" || article.Type != "通知公告" {
		t.Fatalf("article title/type = %q/%q", article.Title, article.Type)
	}
	if article.Date != "2026-08-25" || article.PublishedAt != "2026-08-25 14:23:04" {
		t.Fatalf("article date/published_at = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "正文第一段") || strings.Contains(article.Content, "下一篇") {
		t.Fatalf("article body is inaccurate: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != incBaseURL+"/__local/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://inc.swjtu.edu.cn/__local/body.png"`) {
		t.Fatalf("article HTML did not resolve body image: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, `href="https://inc.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&amp;owner=1339963777&amp;wbfileid=INC-FIXTURE"`) {
		t.Fatalf("article HTML did not resolve body attachment: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v, want body + outside", article.Attachments)
	}
	if article.Attachments[0].Name != "正文附件.pdf" || !strings.Contains(article.Attachments[0].URL, "wbfileid=INC-FIXTURE") {
		t.Fatalf("body attachment = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "公告方案.zip" || !strings.Contains(article.Attachments[1].URL, "wbfileid=INC-OUTSIDE") {
		t.Fatalf("outside attachment = %#v", article.Attachments[1])
	}
}

func TestINCArticleRejectsStaticAndCaptchaShells(t *testing.T) {
	for _, name := range []string{"inc_home.html", "inc_static.html", "inc_captcha.html"} {
		if _, err := parseAdapter_incArticle(readINCFixture(t, name), incBaseURL); err == nil {
			t.Errorf("%s was accepted as an article", name)
		}
	}
}
