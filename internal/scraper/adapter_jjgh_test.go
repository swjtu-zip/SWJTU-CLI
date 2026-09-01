package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func readJJGHFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read jjgh fixture %q: %v", name, err)
	}
	return body
}

func TestJJGHAdapterRegistrationAndFeeds(t *testing.T) {
	site := AllSites["jjgh"]
	if site == nil || site.BaseURL != jjghCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("jjgh adapter is incomplete: %#v", site)
	}
	want := []string{
		"index/xwdt", "zbjs", "gggs", "zcfg/gjxgfgzd", "zcfg/xxxggzzd", "gzcx/gcxm", "tszs",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("jjgh feeds = %#v, want %#v", site.SubPages, want)
	}
	if got := jjghPageURL(site.BaseURL, "index/xwdt", 1, 7, ".htm"); got != site.BaseURL+"/index/xwdt.htm" {
		t.Fatalf("jjgh first page URL = %q", got)
	}
	if got := jjghPageURL(site.BaseURL, "index/xwdt", 4, 7, ".htm"); got != site.BaseURL+"/index/xwdt/4.htm" {
		t.Fatalf("jjgh middle page URL = %q", got)
	}
	if got := jjghPageURL(site.BaseURL, "index/xwdt", 7, 7, ".htm"); got != site.BaseURL+"/index/xwdt/1.htm" {
		t.Fatalf("jjgh last page URL = %q", got)
	}
}

func TestJJGHHomepageIncludesHiddenTabsCarouselAndAllModules(t *testing.T) {
	items, err := parseAdapter_jjghList(readJJGHFixture(t, "jjgh.html"), jjghCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse jjgh homepage: %v", err)
	}
	if len(items) != 23 {
		t.Fatalf("homepage item count = %d, want 23 unique local articles", len(items))
	}
	types := map[string]int{}
	for _, item := range items {
		types[item.Type]++
		if !strings.HasPrefix(item.URL, jjghCanonicalBaseURL+"/info/") {
			t.Errorf("homepage retained non-article URL: %q", item.URL)
		}
		if item.ParsedAt.IsZero() {
			t.Errorf("homepage item has no ParsedAt: %#v", item)
		}
	}
	wantTypes := map[string]int{
		"基建动态": 5,
		"基建视野": 1,
		"公告公示": 6,
		"支部建设": 6,
		"政策法规": 5,
	}
	if !reflect.DeepEqual(types, wantTypes) {
		t.Fatalf("homepage module types = %#v, want %#v", types, wantTypes)
	}
	for _, want := range []struct {
		url, date string
	}{
		{jjghCanonicalBaseURL + "/info/1025/4761.htm", "2026-06-10"},
		{jjghCanonicalBaseURL + "/info/1057/1181.htm", "2016-04-29"},
		{jjghCanonicalBaseURL + "/info/1005/4711.htm", "2026-05-20"},
	} {
		found := false
		for _, item := range items {
			if item.URL == want.url {
				found = true
				if item.Date != want.date {
					t.Errorf("homepage date for %s = %q, want %q", want.url, item.Date, want.date)
				}
			}
		}
		if !found {
			t.Errorf("homepage omitted %s", want.url)
		}
	}
}

func TestJJGHListParsesRowsMetadataAndFiltersExternalLinks(t *testing.T) {
	items, err := parseAdapter_jjghList(readJJGHFixture(t, "jjgh_list.html"), jjghCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse jjgh list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("local list item count = %d, want 2", len(items))
	}
	if items[0].Title != "第一页最新基建动态" || items[0].URL != jjghCanonicalBaseURL+"/info/1025/9001.htm" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31 14:34:56" || items[0].Type != "基建动态" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[1].Date != "2026-08-30" || items[1].URL != jjghCanonicalBaseURL+"/info/1025/9002.htm" {
		t.Fatalf("second list item = %#v", items[1])
	}
}

func TestJJGHKeepsRealHeadlinesEndingInClosingBookMark(t *testing.T) {
	title := "赓续红色基因 厚植爱国情怀——建管处组织主题党日活动观看红色影片《志愿军：存亡之战》"
	if !jjghValidTitle(title) {
		t.Fatalf("real jjgh headline was rejected: %q", title)
	}
	if jjghValidTitle("更多》") {
		t.Fatal("navigation label was accepted as an article headline")
	}
}

func TestJJGHReversePaginationFetchesFirstMiddleAndLast(t *testing.T) {
	fixtures := map[string]string{
		"/index/xwdt.htm":   string(readJJGHFixture(t, "jjgh_list.html")),
		"/index/xwdt/2.htm": string(readJJGHFixture(t, "jjgh_list_page2.html")),
		"/index/xwdt/1.htm": string(readJJGHFixture(t, "jjgh_list_page3.html")),
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		body, ok := fixtures[r.URL.Path]
		if !ok {
			http.Error(w, "unexpected jjgh path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	site := AllSites["jjgh"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	site.BaseURL = server.URL
	NoCache = true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
		ClearCache()
	})

	total, err := site.GetTotalPages("index/xwdt")
	if err != nil {
		t.Fatalf("get jjgh page count: %v", err)
	}
	if total != 3 {
		t.Fatalf("jjgh total pages = %d, want 3", total)
	}
	wantURLs := []string{
		server.URL + "/info/1025/9001.htm",
		server.URL + "/info/1025/9002.htm",
		server.URL + "/info/1025/9004.htm",
		server.URL + "/info/1025/9005.htm",
	}
	var gotURLs []string
	for page := 1; page <= total; page++ {
		items, pageErr := site.ListNewsPage("index/xwdt", page)
		if pageErr != nil {
			t.Fatalf("jjgh page %d: %v", page, pageErr)
		}
		for _, item := range items {
			gotURLs = append(gotURLs, item.URL)
		}
	}
	if !reflect.DeepEqual(gotURLs, wantURLs) {
		t.Fatalf("jjgh page order/items = %#v, want %#v", gotURLs, wantURLs)
	}
	first, middle, last := -1, -1, -1
	for i, path := range requests {
		switch path {
		case "/index/xwdt.htm":
			if first < 0 {
				first = i
			}
		case "/index/xwdt/2.htm":
			if middle < 0 {
				middle = i
			}
		case "/index/xwdt/1.htm":
			if last < 0 {
				last = i
			}
		}
	}
	if first < 0 || middle < 0 || last < 0 || !(first < middle && middle < last) {
		t.Fatalf("jjgh pagination requests = %#v, want unsuffixed, /2.htm, /1.htm", requests)
	}
}

func TestJJGHLegalEmptyListAndStrictShellRejection(t *testing.T) {
	items, err := parseAdapter_jjghList(readJJGHFixture(t, "jjgh_empty.html"), jjghCanonicalBaseURL)
	if err != nil {
		t.Fatalf("legal empty jjgh list returned error: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("legal empty jjgh list = %#v", items)
	}
	for _, name := range []string{"jjgh_static.html", "jjgh_error.html"} {
		if _, err := parseAdapter_jjghList(readJJGHFixture(t, name), jjghCanonicalBaseURL); err == nil {
			t.Fatalf("%s was accepted as a jjgh list", name)
		}
	}
}

func TestJJGHArticleExtractsPreciseMetadataBodyImageAndAttachments(t *testing.T) {
	article, err := parseAdapter_jjghArticle(readJJGHFixture(t, "jjgh_article.html"), jjghCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse jjgh article: %v", err)
	}
	if article.Title != "公告标题" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:56" || article.Type != "公告公示" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "这是正文第一段") {
		t.Fatalf("article body was not retained: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != jjghCanonicalBaseURL+"/images/notice.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://jjgh.swjtu.edu.cn/images/notice.png"`) || !strings.Contains(article.ContentHTML, `href="https://jjgh.swjtu.edu.cn/files/inline.pdf"`) {
		t.Fatalf("article HTML did not resolve body resources: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v, want inline and outside attachments", article.Attachments)
	}
	attachments := map[string]string{}
	for _, attachment := range article.Attachments {
		attachments[attachment.Name] = attachment.URL
	}
	if !strings.Contains(attachments["正文附件.pdf"], "/files/inline.pdf") || !strings.Contains(attachments["外置附件.zip"], "wbfileid=ABC123") {
		t.Fatalf("article attachment URLs = %#v", attachments)
	}
}

func TestJJGHArticleRejectsErrorAndNonArticleShells(t *testing.T) {
	if _, err := parseAdapter_jjghArticle(readJJGHFixture(t, "jjgh_error.html"), jjghCanonicalBaseURL); err == nil {
		t.Fatal("error shell was accepted as a jjgh article")
	}
	if _, err := parseAdapter_jjghArticle(readJJGHFixture(t, "jjgh_list.html"), jjghCanonicalBaseURL); err == nil {
		t.Fatal("list shell was accepted as a jjgh article")
	}
}
