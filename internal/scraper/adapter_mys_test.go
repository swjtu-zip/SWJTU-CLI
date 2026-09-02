package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const mysFixtureBaseURL = "https://mys.swjtu.edu.cn"

func readMysFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read mys fixture %q: %v", name, err)
	}
	return body
}

func TestMysAdapterRegistrationAndReversePagination(t *testing.T) {
	site := AllSites["mys"]
	if site == nil || site.BaseURL != mysCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("mys adapter is incomplete: %#v", site)
	}
	wantFeeds := []string{
		"djgz", "djgz/djfc", "djgz/tszb", "djgz/ljsy", "djgz/wjjb",
		"xsgz", "xsgz/tzgg", "xsgz/tcsy", "xsgz/xsdj",
		"xsgz/xytw/tjzx", "xsgz/xytw/xsh", "xsgz/xytw/qnzyzxh",
		"xsgz/xytw/tcr_xcm", "xsgz/xytw/xxfzzdzx", "xsgz/xytw/syglwyh",
		"xsgz/jqzd", "xsgz/cxsj", "xsgz/jdyd", "xsgz/xswhhd", "xsgz/xsczda",
		"jxjw/jxgg", "jxjw/pyfa/a2014jbkspyfa", "jxjw/pyfa/a2015jbkspyfa",
		"jxjw/pyfa/a2016jbkspyfa", "jxjw/rykc",
		"dwjl", "dwjl/ryzj", "dwjl/tzgg", "dwjl/jhxm/zqjhxm",
		"dwjl/jhxm/dqjhxm", "dwjl/jhxm/yxxm",
		"ryyy", "ryyy/ssyhjzhdz", "ryyy/tcjzhdz", "ryyy/xzzhw", "ryyy/yybtx",
		"ryyy/lrxszx", "ryyy/czzjd",
		"tcjs", "tcsx", "tckc", "tcqn", "tcxg", "tcsh", "xyt", "tcyw",
	}
	if !reflect.DeepEqual(site.SubPages, wantFeeds) {
		t.Fatalf("mys feeds = %#v, want %#v", site.SubPages, wantFeeds)
	}

	cases := []struct {
		slug, want  string
		page, total int
	}{
		{slug: "xyt", page: 1, total: 58, want: mysFixtureBaseURL + "/xyt.htm"},
		{slug: "xyt", page: 2, total: 58, want: mysFixtureBaseURL + "/xyt/57.htm"},
		{slug: "xyt", page: 29, total: 58, want: mysFixtureBaseURL + "/xyt/30.htm"},
		{slug: "xyt", page: 58, total: 58, want: mysFixtureBaseURL + "/xyt/1.htm"},
		{slug: "xsgz/xytw/tcr_xcm", page: 6, total: 12, want: mysFixtureBaseURL + "/xsgz/xytw/tcr_xcm/7.htm"},
		{slug: "jxjw/jxgg", page: 4, total: 4, want: mysFixtureBaseURL + "/jxjw/jxgg/1.htm"},
		{slug: "tcjs", page: 1, total: 1, want: mysFixtureBaseURL + "/tcjs.htm"},
	}
	for _, tc := range cases {
		if got := mysPageURL("http://mys.swjtu.edu.cn", tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("mysPageURL(%q, page %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
	if got := mysPageCount(readMysFixture(t, "mys_list.html")); got != 58 {
		t.Fatalf("mys page count = %d, want 58", got)
	}
}

func TestMysHomepageIncludesCarouselAndHiddenModules(t *testing.T) {
	items, err := parseAdapter_mysList(readMysFixture(t, "mys_home.html"), "http://mys.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse mys homepage: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("homepage items = %d, want 5 unique articles: %#v", len(items), items)
	}

	want := map[string]struct {
		title, typ, date string
	}{
		mysFixtureBaseURL + "/info/1001/11013.htm": {"轮播唯一文章", "", ""},
		mysFixtureBaseURL + "/info/1001/10993.htm": {"重复文章完整标题", "唐臣要闻", ""},
		mysFixtureBaseURL + "/info/1059/10513.htm": {"首页通知文章", "学院通知", ""},
		mysFixtureBaseURL + "/info/1023/11513.htm": {"隐藏公示文章", "公示专栏", ""},
		mysFixtureBaseURL + "/info/1037/11553.htm": {"活动文章", "活动公告", ""},
	}
	for _, item := range items {
		expected, ok := want[item.URL]
		if !ok {
			t.Errorf("unexpected homepage item: %#v", item)
			continue
		}
		if item.Title != expected.title || item.Type != expected.typ || item.Date != expected.date || item.PublishedAt != "" {
			t.Errorf("homepage item = %#v, want title=%q type=%q date=%q", item, expected.title, expected.typ, expected.date)
		}
	}
	if len(want) != len(items) {
		t.Fatalf("homepage did not contain every expected module: %#v", items)
	}
}

func TestMysListExtractsTypeTimeAndRejectsExternal(t *testing.T) {
	items, err := parseAdapter_mysList(readMysFixture(t, "mys_list.html"), mysFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse mys list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("list items = %d, want 2 internal articles: %#v", len(items), items)
	}
	if items[0].URL != mysFixtureBaseURL+"/info/1059/10513.htm" || items[0].Title != "真实通知标题" || items[0].Type != "学院通知" || items[0].Date != "2026-04-24" || items[0].PublishedAt != "2026-04-24 09:05:06" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[1].URL != mysFixtureBaseURL+"/info/1060/11623.htm" || items[1].Type != "唐臣要闻" || items[1].Date != "2026-07-22" {
		t.Fatalf("second list item = %#v", items[1])
	}
	for _, item := range items {
		if strings.Contains(item.URL, "evil.example") || strings.Contains(item.Title, "外链") {
			t.Fatalf("external list link leaked: %#v", item)
		}
	}
}

func TestMysArticleUsesDetailMetadataAndCollectsOutsideAttachments(t *testing.T) {
	article, err := parseAdapter_mysArticle(readMysFixture(t, "mys_article.html"), "http://mys.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse mys article: %v", err)
	}
	if article.Title != "详情页权威标题" || article.Date != "2026-04-24" || article.PublishedAt != "2026-04-24 09:05:06" || article.Type != "学院通知" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "正文第一段") || !strings.Contains(article.Content, "正文第二段") || strings.Contains(article.Content, "侧栏伪正文") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != mysFixtureBaseURL+"/__local/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="`+mysFixtureBaseURL+`/__local/body.png"`) || !strings.Contains(article.ContentHTML, mysFixtureBaseURL+"/files/body.pdf") {
		t.Fatalf("resolved body resources missing from ContentHTML: %q", article.ContentHTML)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v, want body and outside attachment", article.Attachments)
	}
	if article.Attachments[0].Name != "正文附件.pdf" || article.Attachments[0].URL != mysFixtureBaseURL+"/files/body.pdf" {
		t.Fatalf("body attachment = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "评选办法.docx" || !strings.Contains(article.Attachments[1].URL, "download.jsp") {
		t.Fatalf("outside attachment = %#v", article.Attachments[1])
	}
}

func TestMysEmptyAndNonArticleResponsesAreStrict(t *testing.T) {
	items, err := parseAdapter_mysList(readMysFixture(t, "mys_empty.html"), mysFixtureBaseURL)
	if err != nil {
		t.Fatalf("legal empty mys list returned error: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("legal empty mys list = %#v, want non-nil empty slice", items)
	}
	if _, err := parseAdapter_mysList(readMysFixture(t, "mys_static.html"), mysFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as a mys list")
	}
	if _, err := parseAdapter_mysArticle(readMysFixture(t, "mys_static.html"), mysFixtureBaseURL); err == nil {
		t.Fatal("static page was accepted as a mys article")
	}
	if _, err := parseAdapter_mysArticle(readMysFixture(t, "mys_captcha.html"), mysFixtureBaseURL); err == nil {
		t.Fatal("captcha/error page was accepted as a mys article")
	}
}
