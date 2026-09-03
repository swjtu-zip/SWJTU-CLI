package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func readLTCFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read LTC fixture %q: %v", name, err)
	}
	return body
}

func TestLTCAdapterRegistrationAndFeeds(t *testing.T) {
	site := AllSites["ltc"]
	if site == nil || site.BaseURL != ltcBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("LTC adapter hooks are incomplete: %#v", site)
	}
	if !reflect.DeepEqual(site.SubPages, ltcSubPages) {
		t.Fatalf("LTC feeds = %#v, want %#v", site.SubPages, ltcSubPages)
	}
}

func TestLTCPageURLUsesReverseGeneratedPages(t *testing.T) {
	if got := ltcPageURL("http://ltc.swjtu.edu.cn", "wqdt", 1, 66, ".htm"); got != "http://ltc.swjtu.edu.cn/wqdt.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := ltcPageURL(ltcBaseURL, "wqdt", 2, 66, ".htm"); got != ltcBaseURL+"/wqdt/65.htm" {
		t.Fatalf("second page URL = %q", got)
	}
	if got := ltcPageURL(ltcBaseURL, "wqdt", 33, 66, ".htm"); got != ltcBaseURL+"/wqdt/34.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := ltcPageURL(ltcBaseURL, "wqdt", 66, 66, ".htm"); got != ltcBaseURL+"/wqdt/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := ltcPageURL(ltcBaseURL, "index", 1, 1, ""); got != ltcBaseURL+"/index.htm" {
		t.Fatalf("homepage URL = %q", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 66}, {67, 66}, {2, 0}, {2, 1},
	} {
		if got := ltcPageURL(ltcBaseURL, "wqdt", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("invalid page %d/%d produced %q", tc.page, tc.total, got)
		}
	}
	if got := ltcPageURL(ltcBaseURL, "index", 2, 1, ".htm"); got != "" {
		t.Fatalf("homepage out-of-range URL = %q", got)
	}
}

func TestLTCHomepageIncludesAllArticleModulesAndFiltersExternalLinks(t *testing.T) {
	items, err := parseAdapter_ltcList(readLTCFixture(t, "ltc.html"), ltcBaseURL)
	if err != nil {
		t.Fatalf("parse LTC homepage: %v", err)
	}
	if len(items) < 20 {
		t.Fatalf("homepage item count = %d, want all article modules: %#v", len(items), items)
	}
	types := make(map[string]int)
	for _, item := range items {
		types[item.Type]++
		if !strings.HasPrefix(item.URL, ltcBaseURL+"/info/") {
			t.Errorf("homepage leaked non-local URL: %#v", item)
		}
	}
	for _, typ := range []string{"头条新闻", "晚晴动态", "晚晴通知", "基层风采", "关心下一代", "乐龄天地"} {
		if types[typ] == 0 {
			t.Fatalf("homepage has no %q module: %#v", typ, types)
		}
	}
	for _, item := range items {
		if item.Title == "深入学习贯彻习近平总书记给四所交通大学全体师生重要回信精神" && item.Type != "头条新闻" {
			t.Fatalf("headline module type = %q", item.Type)
		}
	}
}

func TestLTCListParsesDetailMetadataAndPagination(t *testing.T) {
	items, err := parseAdapter_ltcList(readLTCFixture(t, "ltc_list.html"), ltcBaseURL)
	if err != nil {
		t.Fatalf("parse LTC list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("list item count = %d, want 2: %#v", len(items), items)
	}
	if items[0].Title != "列表详情标题" || items[0].URL != ltcBaseURL+"/info/1042/11526.htm" || items[0].Date != "2026-07-03" || items[0].PublishedAt != "2026-07-03 14:28:00" || items[0].Type != "晚晴动态" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[1].Date != "2026-07-02" || items[1].PublishedAt != "2026-07-02" || items[1].Type != "晚晴动态" {
		t.Fatalf("date-only list item = %#v", items[1])
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(readLTCFixture(t, "ltc_list.html"))))
	if err != nil {
		t.Fatal(err)
	}
	if got := ltcPaginationTotal(doc); got != 66 {
		t.Fatalf("pagination total = %d, want 66", got)
	}
}

func TestLTCArticleUsesDetailHeadlineTypeAndPreciseMetadata(t *testing.T) {
	article, err := parseAdapter_ltcArticle(readLTCFixture(t, "ltc_article.html"), ltcBaseURL)
	if err != nil {
		t.Fatalf("parse LTC article: %v", err)
	}
	if article.Title != "详情页完整标题" || article.Type != "晚晴通知" || article.Date != "2026-07-17" || article.PublishedAt != "2026-07-17 09:12:05" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Source != "离退休工作处" {
		t.Fatalf("article source = %q", article.Source)
	}
	if !strings.Contains(article.Content, "正文第一段") || !strings.Contains(article.Content, "正文第二段") || strings.Count(article.Content, "正文第二段") != 1 || strings.Contains(article.Content, "页脚噪声") {
		t.Fatalf("article body scope/blocks are wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != ltcBaseURL+"/images/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://ltc.swjtu.edu.cn/images/body.png"`) {
		t.Fatalf("article HTML did not resolve image URL: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件：活动安排.docx" || article.Attachments[0].URL != ltcBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1440185272&wbfileid=LTC123" {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, "wbfileid=LTC123") {
		t.Fatalf("article HTML did not preserve attachment URL: %s", article.ContentHTML)
	}
}

func TestLTCRejectsStaticVerificationAndExternalArticleShells(t *testing.T) {
	for _, name := range []string{"ltc_static.html", "ltc_captcha.html"} {
		if _, err := parseAdapter_ltcList(readLTCFixture(t, name), ltcBaseURL); err == nil {
			t.Fatalf("%s was accepted as an LTC list", name)
		}
		if _, err := parseAdapter_ltcArticle(readLTCFixture(t, name), ltcBaseURL); err == nil {
			t.Fatalf("%s was accepted as an LTC article", name)
		}
	}
	malformed := []byte(`<meta name="pageType" content="2"><div class="main_right"><div class="current"><h3>晚晴动态</h3></div><div class="conlistComm"><dl><dt><a href="/info/1042/11526.htm"></a></dt><dd>时间：2026年07月03日</dd></dl></div></div>`)
	if _, err := parseAdapter_ltcList(malformed, ltcBaseURL); err == nil {
		t.Fatal("malformed local LTC row was silently discarded")
	}
}
