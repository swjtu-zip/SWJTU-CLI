package scraper

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func hzcFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return body
}

func TestHzcRegistersAuditedFeedsAndReversePagination(t *testing.T) {
	site, ok := AllSites["hzc"]
	if !ok {
		t.Fatal("hzc site is not registered")
	}
	want := []string{
		"xwdt/zx", "xwdt/dw", "xwdt/zh", "tzgs", "xzzq/xzzq",
		"dwhz/hzqk", "dwhz/zdwj", "jxjy/xxgs", "jxjy/zdwj",
		"ddbf/rwfc", "zbjs/djyd",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("hzc feeds = %#v, want %#v", site.SubPages, want)
	}
	if site.BaseURL != hzcCanonicalBaseURL || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("hzc registration lost canonical URL or pagination hooks: %#v", site)
	}
	if got := hzcPageURL(hzcCanonicalBaseURL, "xwdt/zx", 1, 38, ".htm"); got != "https://hzc.swjtu.edu.cn/xwdt/zx.htm" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := hzcPageURL(hzcCanonicalBaseURL, "xwdt/zx", 2, 38, ".htm"); got != "https://hzc.swjtu.edu.cn/xwdt/zx/37.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
	if got := hzcPageURL(hzcCanonicalBaseURL, "xwdt/zx", 38, 38, ".htm"); got != "https://hzc.swjtu.edu.cn/xwdt/zx/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := hzcPaginationTotal(parseHzcDocument(t, "hzc_list.html")); got != 38 {
		t.Fatalf("pagination total = %d, want 38", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 38},
		{39, 38},
		{2, 0},
	} {
		if got := hzcPageURL(hzcCanonicalBaseURL, "xwdt/zx", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
}

func parseHzcDocument(t *testing.T, name string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(hzcFixture(t, name))))
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return doc
}

func TestHzcHomepageParsesVisibleAndHiddenModules(t *testing.T) {
	items, err := parseAdapter_hzcList(hzcFixture(t, "hzc_home.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("homepage items = %d, want 4 local article entries", len(items))
	}
	wantTypes := map[string]string{
		"https://hzc.swjtu.edu.cn/info/1039/19817.htm": "新闻动态",
		"https://hzc.swjtu.edu.cn/info/1039/19807.htm": "新闻动态",
		"https://hzc.swjtu.edu.cn/info/1052/19637.htm": "通知公告",
		"https://hzc.swjtu.edu.cn/info/1031/19707.htm": "下载专区",
	}
	for _, item := range items {
		if got := wantTypes[item.URL]; got == "" || item.Type != got {
			t.Errorf("homepage item %q type = %q, want %q", item.URL, item.Type, got)
		}
		if item.URL != "https://hzc.swjtu.edu.cn/info/1031/19707.htm" && (item.Date == "" || item.PublishedAt == "") {
			t.Errorf("homepage item %q lost date precision: %#v", item.URL, item)
		}
		if strings.Contains(item.URL, "news.swjtu") || strings.HasSuffix(item.URL, ".doc") {
			t.Errorf("homepage retained an external/direct resource: %#v", item)
		}
	}
}

func TestHzcListParsesDlRowsMetadataAndFiltersNonArticles(t *testing.T) {
	items, err := parseAdapter_hzcList(hzcFixture(t, "hzc_list.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("list items = %d, want 3 local article rows", len(items))
	}
	if items[0].Title != "完整的列表标题一" || items[0].Date != "2026-07-15" || items[0].PublishedAt != "2026-07-15 12:34:56" || items[0].Type != "最新" {
		t.Fatalf("first item metadata = %#v", items[0])
	}
	if items[1].Date != "2026-06-10" || items[1].Type != "对外" || items[2].Type != "综合" {
		t.Fatalf("row metadata = %#v", items)
	}
}

func TestHzcEmptyListIsAllowedButStaticPageIsRejected(t *testing.T) {
	items, err := parseAdapter_hzcList(hzcFixture(t, "hzc_empty.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatalf("legal empty list rejected: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list returned items: %#v", items)
	}
	if _, err := parseAdapter_hzcList(hzcFixture(t, "hzc_static.html"), hzcCanonicalBaseURL); err == nil {
		t.Fatal("static introduction page was accepted as article list")
	}
}

func TestHzcArticleExtractsMetadataBodyImagesAndOutsideAttachments(t *testing.T) {
	article, err := parseAdapter_hzcArticle(hzcFixture(t, "hzc_article.html"), hzcCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "测试文章" || article.Date != "2026-07-15" || article.PublishedAt != "2026-07-15 12:34:56" || article.Type != "最新" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || strings.Contains(article.Content, "2026年9月16日，不是发布日期") == false {
		t.Fatalf("article body was not retained: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://hzc.swjtu.edu.cn/images/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://hzc.swjtu.edu.cn/images/body.png"`) {
		t.Fatalf("content HTML did not resolve body image: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if article.Attachments[0].URL != "https://hzc.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=2054591004&wbfileid=ABC" || article.Attachments[1].URL != "https://hzc.swjtu.edu.cn/files/guide.pdf" {
		t.Fatalf("attachment URLs were not resolved: %#v", article.Attachments)
	}
}

func TestHzcRejectsCaptchaAndErrorResponsesAsArticles(t *testing.T) {
	for _, fixture := range []string{"hzc_captcha.html", "hzc_error.html", "hzc_static.html"} {
		if _, err := parseAdapter_hzcArticle(hzcFixture(t, fixture), hzcCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as an article", fixture)
		}
	}
}
