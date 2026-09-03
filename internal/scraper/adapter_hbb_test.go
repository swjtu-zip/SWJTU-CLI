package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func readHBBFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read HBB fixture %q: %v", name, err)
	}
	return body
}

func TestHBBAdapterRegistrationAndFeeds(t *testing.T) {
	site := AllSites["hbb"]
	if site == nil || site.BaseURL != hbbBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("HBB adapter is incomplete: %#v", site)
	}
	want := []string{
		"hjxw/hqxw", "tzgg/tzgg", "cggl", "cggl/cggg", "cggl/jggg",
		"zpxx", "zpxx/zpxx", "zpxx/npgs", "djgz1", "djgz1/xxzl", "djgz1/xxdt",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("HBB feeds = %#v, want %#v", site.SubPages, want)
	}
}

func TestHBBHomepageParsesAllLocalArticleModules(t *testing.T) {
	items, err := parseAdapter_hbbList(readHBBFixture(t, "hbb_home.html"), hbbBaseURL)
	if err != nil {
		t.Fatalf("parse HBB homepage: %v", err)
	}
	if len(items) != 15 {
		t.Fatalf("homepage item count = %d, want 15 (7 news + 8 notices)", len(items))
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Type]++
		if !strings.HasPrefix(item.URL, hbbBaseURL+"/info/") {
			t.Errorf("homepage item %q escaped local article scope: %q", item.Title, item.URL)
		}
		if strings.Contains(item.URL, "mp.weixin.qq.com") || item.URL == hbbBaseURL+"/#" {
			t.Errorf("homepage retained external/placeholder URL: %q", item.URL)
		}
	}
	if counts["新闻动态"] != 7 || counts["通知公告"] != 8 {
		t.Fatalf("homepage type counts = %#v, want 新闻动态=7 通知公告=8", counts)
	}
	var found bool
	for _, item := range items {
		if item.Title == "国网成都供电公司金牛供电中心到访我校九里校区开展供电保障交流座谈" {
			found = true
			if item.Date != "2026-08-05" || item.Type != "新闻动态" {
				t.Errorf("homepage news metadata = %#v, want date/type 2026-08-05/新闻动态", item)
			}
		}
	}
	if !found {
		t.Fatal("homepage did not retain a real news module article")
	}
}

func TestHBBListParsesMetadataAndFiltersExternalRows(t *testing.T) {
	items, err := parseAdapter_hbbList(readHBBFixture(t, "hbb_list.html"), hbbBaseURL)
	if err != nil {
		t.Fatalf("parse HBB list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("local list item count = %d, want 1", len(items))
	}
	item := items[0]
	if item.Title != "西南交通大学补考交通车时刻表" || item.Date != "2026-09-01" || item.Type != "通知公告" {
		t.Fatalf("list item metadata = %#v", item)
	}
	if item.URL != hbbBaseURL+"/info/1018/82112.htm" {
		t.Fatalf("list item URL = %q", item.URL)
	}
}

func TestHBBKeepsRealLocalHeadlinesEndingInClosingQuote(t *testing.T) {
	for _, title := range []string{
		"责任再压实 落实再深入 —饮食服务中心组织学习省教育厅《校园食品安全工作提示函》",
		"饮食服务中心党支部专题学习《党政机关厉行节约反对浪费条例》",
	} {
		if !hbbIsArticleTitle(title) {
			t.Errorf("real HBB headline was rejected: %q", title)
		}
	}
	for _, title := range []string{"通知公告", "首页", "更多>>"} {
		if hbbIsArticleTitle(title) {
			t.Errorf("navigation title was accepted: %q", title)
		}
	}
}

func TestHBBReversePaginationAndPageCount(t *testing.T) {
	tests := []struct {
		slug  string
		pages int
		mid   int
	}{
		{slug: "hjxw/hqxw", pages: 39, mid: 20},
		{slug: "tzgg/tzgg", pages: 9, mid: 5},
		{slug: "cggl", pages: 68, mid: 34},
		{slug: "cggl/cggg", pages: 35, mid: 18},
		{slug: "cggl/jggg", pages: 33, mid: 17},
		{slug: "djgz1/xxdt", pages: 24, mid: 12},
	}
	for _, test := range tests {
		if got := hbbPageURL(hbbBaseURL, test.slug, 1, test.pages, ".htm"); got != hbbBaseURL+"/"+test.slug+".htm" {
			t.Errorf("%s page 1 URL = %q", test.slug, got)
		}
		if got := hbbPageURL(hbbBaseURL, test.slug, 2, test.pages, ".htm"); got != hbbBaseURL+"/"+test.slug+"/"+itoa(test.pages-1)+".htm" {
			t.Errorf("%s page 2 URL = %q", test.slug, got)
		}
		if got := hbbPageURL(hbbBaseURL, test.slug, test.mid, test.pages, ".htm"); got != hbbBaseURL+"/"+test.slug+"/"+itoa(test.pages-test.mid+1)+".htm" {
			t.Errorf("%s middle page URL = %q", test.slug, got)
		}
		if got := hbbPageURL(hbbBaseURL, test.slug, test.pages, test.pages, ".htm"); got != hbbBaseURL+"/"+test.slug+"/1.htm" {
			t.Errorf("%s last page URL = %q", test.slug, got)
		}
	}
	if got := hbbPageCountFromHTML(readHBBFixture(t, "hbb_list.html")); got != 9 {
		t.Fatalf("page count = %d, want 9", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 9},
		{10, 9},
		{2, 0},
	} {
		if got := hbbPageURL(hbbBaseURL, "tzgg/tzgg", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	pos := len(digits)
	for value > 0 {
		pos--
		digits[pos] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[pos:])
}

func TestHBBLegalEmptyListAndStrictShellRejection(t *testing.T) {
	items, err := parseAdapter_hbbList(readHBBFixture(t, "hbb_empty.html"), hbbBaseURL)
	if err != nil {
		t.Fatalf("legal empty HBB list returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("legal empty HBB list returned %d items", len(items))
	}
	if _, err := parseAdapter_hbbList(readHBBFixture(t, "hbb_static.html"), hbbBaseURL); err == nil {
		t.Fatal("static/resource page was accepted as an empty list")
	}
	if _, err := parseAdapter_hbbList(readHBBFixture(t, "hbb_captcha.html"), hbbBaseURL); err == nil {
		t.Fatal("captcha shell was accepted as a list")
	}
}

func TestHBBArticleExtractsPreciseMetadataBodyImagesAndOutsideAttachment(t *testing.T) {
	article, err := parseAdapter_hbbArticle(readHBBFixture(t, "hbb_article.html"), hbbBaseURL)
	if err != nil {
		t.Fatalf("parse HBB article: %v", err)
	}
	if article.Title != "通知标题" || article.Date != "2026-09-01" || article.PublishedAt != "2026-09-01 10:18:48" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Type != "通知公告" || article.Source != "能源与维修服务中心" {
		t.Fatalf("article type/source = %q/%q", article.Type, article.Source)
	}
	if !strings.Contains(article.Content, "这是正文第一段") {
		t.Fatalf("article body was not retained: %q", article.Content)
	}
	if !strings.Contains(article.ContentHTML, `src="https://hbb.swjtu.edu.cn/images/notice.png"`) {
		t.Fatalf("article HTML did not resolve body image: %s", article.ContentHTML)
	}
	if len(article.Images) != 1 || article.Images[0] != hbbBaseURL+"/images/notice.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	attachment := article.Attachments[0]
	if attachment.Name != "通知附件.pdf" || !strings.Contains(attachment.URL, "download.jsp") || !strings.Contains(attachment.URL, "wbfileid=ABC123") {
		t.Fatalf("outside attachment = %#v", attachment)
	}
}

func TestHBBArticleRejectsCaptchaAndNonArticleShells(t *testing.T) {
	if _, err := parseAdapter_hbbArticle(readHBBFixture(t, "hbb_captcha.html"), hbbBaseURL); err == nil {
		t.Fatal("captcha shell was accepted as an article")
	}
	if _, err := parseAdapter_hbbArticle(readHBBFixture(t, "hbb_static.html"), hbbBaseURL); err == nil {
		t.Fatal("static/resource shell was accepted as an article")
	}
}
