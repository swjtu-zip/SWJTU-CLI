package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func readMathsFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read maths fixture %s: %v", name, err)
	}
	return body
}

func TestMathsAdapterRegistrationAndReversePagination(t *testing.T) {
	site := AllSites["maths"]
	if site == nil || site.BaseURL != mathsCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("maths adapter hooks are incomplete: %#v", site)
	}
	if !reflect.DeepEqual(site.SubPages, mathsSubPages) {
		t.Fatalf("maths feeds = %#v, want %#v", site.SubPages, mathsSubPages)
	}
	cases := []struct {
		slug        string
		page, total int
		want        string
	}{
		{"xyxw", 1, 62, mathsCanonicalBaseURL + "/xyxw.htm"},
		{"xyxw", 2, 62, mathsCanonicalBaseURL + "/xyxw/61.htm"},
		{"xyxw", 31, 62, mathsCanonicalBaseURL + "/xyxw/32.htm"},
		{"xyxw", 62, 62, mathsCanonicalBaseURL + "/xyxw/1.htm"},
		{"jyjx1", 32, 63, mathsCanonicalBaseURL + "/jyjx1/32.htm"},
		{"index", 1, 1, mathsCanonicalBaseURL + "/index.htm"},
	}
	for _, tc := range cases {
		if got := mathsPageURL("http://maths.swjtu.edu.cn", tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("mathsPageURL(%q, %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
	if got := mathsPageCount(nil); got != 0 {
		t.Fatalf("nil page count = %d, want 0", got)
	}
	if _, err := mathsTotalPages("not-a-live-feed"); err == nil {
		t.Fatal("un-audited route was accepted")
	}
}

func TestMathsHomepageIncludesHiddenModulesAndFiltersExternalLinks(t *testing.T) {
	items, err := parseAdapter_mathsList(readMathsFixture(t, "maths_home_tabs.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse maths homepage: %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("homepage unique article count = %d, want 6", len(items))
	}
	want := map[string]struct {
		title string
		date  string
		typ   string
	}{
		"/info/1296/1.htm": {"轮播文章", "", "首页焦点"},
		"/info/1149/2.htm": {"新闻动态文章", "2026-08-20", "新闻动态"},
		"/info/1150/3.htm": {"公告文章", "2026-08-19", "通知公告"},
		"/info/1150/4.htm": {"隐藏研究生公告", "2026-08-18", "研究生"},
		"/info/1041/5.htm": {"学术活动文章", "2026-08-17", "学术活动"},
		"/info/1052/6.htm": {"党建文章", "05-22", "党建工作"},
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, mathsCanonicalBaseURL+"/info/") {
			t.Fatalf("non-local homepage URL leaked: %#v", item)
		}
		path := strings.TrimPrefix(item.URL, mathsCanonicalBaseURL)
		wantItem, ok := want[path]
		if !ok {
			t.Fatalf("unexpected homepage item: %#v", item)
		}
		if item.Title != wantItem.title || item.Date != wantItem.date || item.Type != wantItem.typ {
			t.Errorf("homepage item %s = %#v, want title/date/type %q/%q/%q", path, item, wantItem.title, wantItem.date, wantItem.typ)
		}
	}
}

func TestMathsListScopesRowsAndPreservesTypeDate(t *testing.T) {
	items, err := parseAdapter_mathsList(readMathsFixture(t, "maths_list.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse maths list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("list items = %d, want 2", len(items))
	}
	if items[0].Title != "列表截断标题" || items[0].URL != mathsCanonicalBaseURL+"/info/1150/9001.htm" || items[0].Date != "2026-08-25" || items[0].Type != "学院公告" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[1].Title != "带精确时间的公告" || items[1].PublishedAt != "2026-08-24 09:08:07" || items[1].Type != "学院公告" {
		t.Fatalf("second list item = %#v", items[1])
	}
	if got := mathsPageCount(parseMathsDocument(t, "maths_list.html")); got != 62 {
		t.Fatalf("list total pages = %d, want 62", got)
	}
	if _, ok := mathsArticleURL(mathsCanonicalBaseURL, "https://example.com/info/1/2.htm"); ok {
		t.Fatal("external article URL was accepted")
	}
}

func TestMathsArticleUsesDetailTitleTypeBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_mathsArticle(readMathsFixture(t, "maths_article.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse maths article: %v", err)
	}
	if article.Title != "详情页完整标题——不要使用列表截断" || article.Date != "2026-08-25" || article.PublishedAt != "2026-08-25 14:05:06" || article.Type != "学院公告" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "这是数学学院正文第一段") || strings.Contains(article.Content, "导航噪声") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != mathsCanonicalBaseURL+"/images/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="`+mathsCanonicalBaseURL+`/images/body.png"`) {
		t.Fatalf("resolved image missing from ContentHTML: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件.zip" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
}

func TestMathsDistinguishesEmptyStaticAndVerificationPages(t *testing.T) {
	items, err := parseAdapter_mathsList(readMathsFixture(t, "maths_empty.html"), mathsCanonicalBaseURL)
	if err != nil {
		t.Fatalf("legal empty list rejected: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list returned items: %#v", items)
	}
	for _, fixture := range []string{"maths_static.html", "maths_captcha.html"} {
		if _, err := parseAdapter_mathsList(readMathsFixture(t, fixture), mathsCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as a list", fixture)
		}
		if _, err := parseAdapter_mathsArticle(readMathsFixture(t, fixture), mathsCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as an article", fixture)
		}
	}
	if _, err := parseAdapter_mathsArticle(readMathsFixture(t, "maths_list.html"), mathsCanonicalBaseURL); err == nil {
		t.Fatal("list page was accepted as an article")
	}
}

func TestMathsLiveAuditedRoutes(t *testing.T) {
	if os.Getenv("MATHS_LIVE") != "1" {
		t.Skip("set MATHS_LIVE=1 to run the live maths audit")
	}
	site := AllSites["maths"]
	if site == nil {
		t.Fatal("maths site is not registered")
	}
	for _, slug := range mathsSubPages {
		total, err := site.GetTotalPages(slug)
		if err != nil {
			t.Fatalf("%s total pages: %v", slug, err)
		}
		pages := []int{1, total}
		if total > 1 {
			pages = append(pages, 2)
		}
		if total > 2 {
			pages = append(pages, (total+1)/2)
		}
		seen := make(map[int]bool)
		for _, page := range pages {
			if seen[page] {
				continue
			}
			seen[page] = true
			items, listErr := site.ListNewsPage(slug, page)
			if listErr != nil {
				t.Fatalf("%s page %d/%d: %v", slug, page, total, listErr)
			}
			if len(items) == 0 {
				t.Fatalf("%s page %d/%d returned no articles", slug, page, total)
			}
		}
	}
	article, err := site.GetArticle(mathsCanonicalBaseURL + "/info/1027/26244.htm")
	if err != nil {
		t.Fatalf("live detail: %v", err)
	}
	if article.Title == "" || article.Date == "" || article.Type == "" || article.Content == "" || len(article.Attachments) == 0 {
		t.Fatalf("live detail lost metadata/body/attachment: %#v", article)
	}
	imageArticle, err := site.GetArticle(mathsCanonicalBaseURL + "/info/1149/26874.htm")
	if err != nil {
		t.Fatalf("live image detail: %v", err)
	}
	if len(imageArticle.Images) == 0 || imageArticle.Content == "" {
		t.Fatalf("live image detail lost body/images: %#v", imageArticle)
	}
}

func parseMathsDocument(t *testing.T, name string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(readMathsFixture(t, name))))
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return doc
}
