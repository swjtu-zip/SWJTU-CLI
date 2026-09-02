package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const fadFixtureBaseURL = "https://fad.swjtu.edu.cn"

func readFadFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestFadAdapterRegistrationAndRoutes(t *testing.T) {
	site := AllSites["fad"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("fad adapter hooks are not registered")
	}
	if site.BaseURL != fadCanonicalBaseURL {
		t.Fatalf("base URL = %q, want %q", site.BaseURL, fadCanonicalBaseURL)
	}
	if site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("fad pagination hooks are not registered")
	}

	wantFeeds := map[string]bool{
		"xwsd/tzgg":    true,
		"xwsd/xwsd":    true,
		"xwsd/xxgs":    true,
		"hwxx/gjgpxm":  true,
		"hwxx/sxwxm":   true,
		"hwxx/dqjlxm":  true,
		"gjxz/yzxmsb":  true,
		"gjxz/wjjsjlb": true,
		"gjxz/swrytx":  true,
		"djyd/hdxw":    true,
	}
	if len(site.SubPages) != len(wantFeeds) {
		t.Fatalf("fad feeds = %#v, want %d actual list routes", site.SubPages, len(wantFeeds))
	}
	for _, feed := range site.SubPages {
		if !wantFeeds[feed] {
			t.Errorf("unexpected fad feed %q", feed)
		}
	}
}

func TestFadPageURLUsesReverseGeneratedPages(t *testing.T) {
	base := fadFixtureBaseURL
	tests := []struct {
		name       string
		slug       string
		page       int
		totalPages int
		want       string
	}{
		{name: "article landing", slug: "index", page: 1, want: base + "/xwsd.htm"},
		{name: "first page", slug: "xwsd/xwsd", page: 1, totalPages: 78, want: base + "/xwsd/xwsd.htm"},
		{name: "second page", slug: "xwsd/xwsd", page: 2, totalPages: 78, want: base + "/xwsd/xwsd/77.htm"},
		{name: "middle page", slug: "xwsd/xwsd", page: 39, totalPages: 78, want: base + "/xwsd/xwsd/40.htm"},
		{name: "last page", slug: "xwsd/xwsd", page: 78, totalPages: 78, want: base + "/xwsd/xwsd/1.htm"},
		{name: "two-page list", slug: "hwxx/dqjlxm", page: 2, totalPages: 2, want: base + "/hwxx/dqjlxm/1.htm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fadPageURL(base, tt.slug, tt.page, tt.totalPages, ".htm"); got != tt.want {
				t.Fatalf("URL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFadHomepageParsesAllArticleModules(t *testing.T) {
	items, err := parseAdapter_fadList(readFadFixture(t, "fad_home.html"), fadFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d home items, want four local rows from all three modules: %#v", len(items), items)
	}

	want := map[string]struct {
		date string
		typ  string
	}{
		"新闻模块文章一": {date: "2026-08-01", typ: "新闻动态"},
		"新闻模块文章二": {date: "2026-07-31", typ: "新闻动态"},
		"通知模块文章":  {date: "2026-07-30", typ: "通知公告"},
		"公示模块文章":  {date: "2026-07-29", typ: "信息公示"},
	}
	for _, item := range items {
		if strings.Contains(item.URL, "news.swjtu.edu.cn") {
			t.Errorf("external syndicated row leaked into fad feed: %#v", item)
		}
		expected, ok := want[item.Title]
		if !ok {
			t.Errorf("unexpected homepage item: %#v", item)
			continue
		}
		if item.Date != expected.date || item.Type != expected.typ {
			t.Errorf("homepage item %q metadata = (%q, %q), want (%q, %q)", item.Title, item.Date, item.Type, expected.date, expected.typ)
		}
	}
}

func TestFadListScopesRowsAndPreservesPreciseTimestamp(t *testing.T) {
	items, err := parseAdapter_fadList(readFadFixture(t, "fad_list.html"), fadFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want two local article rows: %#v", len(items), items)
	}
	if items[0].Title != "真实通知一" || items[0].Date != "2026-08-31" || items[0].Type != "通知公告" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[1].Title != "带精确时间的通知" || items[1].Date != "2026-08-30" || items[1].PublishedAt != "2026-08-30 14:34:00" {
		t.Fatalf("precise list item = %#v", items[1])
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, fadFixtureBaseURL+"/info/") {
			t.Errorf("non-article URL leaked: %#v", item)
		}
	}
}

func TestFadPaginationUsesDisplayedLogicalPageNumbers(t *testing.T) {
	fixture := readFadFixture(t, "fad_list.html")
	if got := fadTotalPagesFromHTML(fixture); got != 78 {
		t.Fatalf("total pages = %d, want displayed logical page 78", got)
	}

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	site := AllSites["fad"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	ClearCache()
	NoCache = true
	site.BaseURL = server.URL
	t.Cleanup(func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
		ClearCache()
	})

	total, err := site.GetTotalPages("xwsd/xwsd")
	if err != nil {
		t.Fatal(err)
	}
	if total != 78 {
		t.Fatalf("site total pages = %d, want 78", total)
	}
	for _, page := range []int{2, 39, 78} {
		if _, err := site.ListNewsPage("xwsd/xwsd", page); err != nil {
			t.Fatalf("logical page %d: %v", page, err)
		}
	}
	for _, want := range []string{"/xwsd/xwsd/77.htm", "/xwsd/xwsd/40.htm", "/xwsd/xwsd/1.htm"} {
		found := false
		for _, request := range requests {
			if request == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("requests = %#v, missing logical page URL %q", requests, want)
		}
	}
}

func TestFadArticleExtractsMetadataBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_fadArticle(readFadFixture(t, "fad_article.html"), fadFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "带精确时间的通知" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Source != "国际合作与交流处" || article.Author != "张三" {
		t.Fatalf("source/author = %q/%q", article.Source, article.Author)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("date/published_at = %q/%q", article.Date, article.PublishedAt)
	}
	if article.Type != "通知公告" {
		t.Fatalf("type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || strings.Contains(article.Content, "导航伪正文") {
		t.Fatalf("unexpected content = %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != fadFixtureBaseURL+"/images/notice.png" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 || article.Attachments[0].Name != "正文模板.docx" || article.Attachments[1].Name != "附件二.pdf" {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.Attachments[1].URL, "/system/_content/download.jsp?") {
		t.Fatalf("second attachment URL = %q", article.Attachments[1].URL)
	}
	if !strings.Contains(article.ContentHTML, `src="https://fad.swjtu.edu.cn/images/notice.png"`) || !strings.Contains(article.ContentHTML, `href="https://fad.swjtu.edu.cn/__local/template.docx?e=.docx"`) {
		t.Fatalf("content HTML did not retain resolved resources: %s", article.ContentHTML)
	}
}

func TestFadDistinguishesEmptyStaticRedirectAndVerificationPages(t *testing.T) {
	empty, err := parseAdapter_fadList(readFadFixture(t, "fad_empty.html"), fadFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("legal empty list returned %d items", len(empty))
	}

	redirect := readFadFixture(t, "fad.html")
	if _, err := parseAdapter_fadList(redirect, fadFixtureBaseURL); err == nil {
		t.Fatal("redirect/landing shell was accepted as a list")
	}
	if _, err := parseAdapter_fadArticle(redirect, fadFixtureBaseURL); err == nil {
		t.Fatal("redirect/landing shell was accepted as an article")
	}

	static := readFadFixture(t, "fad_static.html")
	if _, err := parseAdapter_fadList(static, fadFixtureBaseURL); err == nil || !strings.Contains(err.Error(), "static/resource") {
		t.Fatalf("static list error = %v", err)
	}
	if _, err := parseAdapter_fadArticle(static, fadFixtureBaseURL); err == nil || !strings.Contains(err.Error(), "not an article") {
		t.Fatalf("static article error = %v", err)
	}

	verification := readFadFixture(t, "fad_captcha.html")
	for name, parse := range map[string]func([]byte, string) error{
		"list": func(raw []byte, base string) error {
			_, err := parseAdapter_fadList(raw, base)
			return err
		},
		"article": func(raw []byte, base string) error {
			_, err := parseAdapter_fadArticle(raw, base)
			return err
		},
	} {
		if err := parse(verification, fadFixtureBaseURL); err == nil || !strings.Contains(err.Error(), "captcha") {
			t.Errorf("%s verification error = %v", name, err)
		}
	}
}

func TestFadUnknownRouteCannotBecomeAFeed(t *testing.T) {
	if _, err := fadTotalPages("gjxz/cyxz"); err == nil || !strings.Contains(err.Error(), "not an article-list route") {
		t.Fatalf("resource page route error = %v", err)
	}
	if fadKnownFeed("gjxz/cyxz") {
		t.Fatal("resource page was registered as a feed")
	}
	if got := fmt.Sprint(fadPageURL(fadFixtureBaseURL, "gjxz/cyxz", 1, 1, ".htm")); !strings.HasSuffix(got, "/gjxz/cyxz.htm") {
		t.Fatalf("unexpected fallback URL: %q", got)
	}
}
