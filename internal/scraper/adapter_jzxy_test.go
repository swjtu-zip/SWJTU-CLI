package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func readJzxyFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestJzxyAdapterRegistrationAndCanonicalURL(t *testing.T) {
	site := AllSites["jzxy"]
	if site == nil || site.BaseURL != jzxyCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatalf("jzxy adapter hooks are incomplete: %+v", site)
	}
	want := []string{
		"xxzx/xyxw", "xxzx/xygg", "xxzx/bkstz", "xxzx/yjstz", "xxzx/jyxx", "xxzx/xskj", "xxzx/xshdyg",
		"djgz/djtz", "djgz/djxw", "djgz/jwgz", "djgz/xxyd", "hzjl/jldt", "xsgz/xsgzzd", "xygz",
		"xygz/jcxy/tysqxy", "xygz/jcxy/cdbxhxy",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("jzxy sub-pages = %#v, want %#v", site.SubPages, want)
	}
	if got := jzxyCanonicalBase("http://jzxy.swjtu.edu.cn/"); got != jzxyCanonicalBaseURL {
		t.Fatalf("canonical base = %q, want %q", got, jzxyCanonicalBaseURL)
	}
}

func TestJzxyPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	cases := []struct {
		slug, want  string
		page, total int
	}{
		{"index", jzxyCanonicalBaseURL + "/index.htm", 1, 1},
		{"xxzx/xyxw", jzxyCanonicalBaseURL + "/xxzx/xyxw.htm", 1, 80},
		{"xxzx/xyxw", jzxyCanonicalBaseURL + "/xxzx/xyxw/79.htm", 2, 80},
		{"xxzx/xyxw", jzxyCanonicalBaseURL + "/xxzx/xyxw/41.htm", 40, 80},
		{"xxzx/xyxw", jzxyCanonicalBaseURL + "/xxzx/xyxw/1.htm", 80, 80},
	}
	for _, tc := range cases {
		if got := jzxyPageURL("http://jzxy.swjtu.edu.cn", tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("%s page %d/%d URL = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 80},
		{81, 80},
		{2, 0},
	} {
		if got := jzxyPageURL(jzxyCanonicalBaseURL, "xxzx/xyxw", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
}
func TestJzxyListParserScopesRealListAndPreservesType(t *testing.T) {
	raw := readJzxyFixture(t, "jzxy_list.html")
	items, err := parseAdapter_jzxyList(raw, "http://jzxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("parsed %d jzxy list items, want 3: %#v", len(items), items)
	}
	want := []struct {
		title, url, date string
	}{
		{"建筑学院真实新闻完整标题", jzxyCanonicalBaseURL + "/info/1032/24701.htm", "2026-07-10"},
		{"本科生真实通知", jzxyCanonicalBaseURL + "/info/1030/24691.htm", "2026-07-09"},
		{"校友姓名", jzxyCanonicalBaseURL + "/info/1070/19371.htm", "2025-06-03"},
	}
	for i, expected := range want {
		if items[i].Title != expected.title || items[i].URL != expected.url || items[i].Date != expected.date || items[i].PublishedAt != expected.date || items[i].Type != "学院新闻" {
			t.Errorf("item %d = (%q, %q, %q, %q, %q), want (%q, %q, %q, %q, 学院新闻)", i, items[i].Title, items[i].URL, items[i].Date, items[i].PublishedAt, items[i].Type, expected.title, expected.url, expected.date, expected.date)
		}
	}
	for _, item := range items {
		if strings.Contains(item.URL, "example.com") || item.Title == "组织简介" {
			t.Errorf("non-article leaked from jzxy list: %#v", item)
		}
	}
	doc, err := jzxyDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := jzxyPageCount(doc); got != 80 {
		t.Fatalf("jzxy page count = %d, want 80", got)
	}
}

func TestJzxyHomeParserTraversesHiddenTabsAndActivity(t *testing.T) {
	items, err := parseAdapter_jzxyList(readJzxyFixture(t, "jzxy_home.html"), "http://jzxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("parsed %d home items, want 8: %#v", len(items), items)
	}
	byURL := make(map[string]struct {
		title, date, publishedAt, typ string
	}, len(items))
	for _, item := range items {
		byURL[item.URL] = struct {
			title, date, publishedAt, typ string
		}{item.Title, item.Date, item.PublishedAt, item.Type}
		if !strings.HasPrefix(item.URL, jzxyCanonicalBaseURL+"/info/") {
			t.Errorf("non-canonical home URL: %#v", item)
		}
	}
	want := map[string]struct {
		title, date, publishedAt, typ string
	}{
		jzxyCanonicalBaseURL + "/info/1032/10001.htm": {"首页新闻焦点", "", "", "学院新闻"},
		jzxyCanonicalBaseURL + "/info/1032/10002.htm": {"首页新闻侧栏", "2026-07-10", "2026-07-10", "学院新闻"},
		jzxyCanonicalBaseURL + "/info/1032/10003.htm": {"首页新闻列表一", "2026-07-02", "2026-07-02", "学院新闻"},
		jzxyCanonicalBaseURL + "/info/1032/10004.htm": {"首页新闻列表二", "2026-07-01", "2026-07-01", "学院新闻"},
		jzxyCanonicalBaseURL + "/info/1029/10005.htm": {"学院公告文章", "2025-11-19", "2025-11-19", "学院公告"},
		jzxyCanonicalBaseURL + "/info/1030/10006.htm": {"本科生通知文章", "2026-07-09", "2026-07-09", "本科生通知"},
		jzxyCanonicalBaseURL + "/info/1031/10007.htm": {"研究生通知文章", "2026-07-06", "2026-07-06", "研究生通知"},
		jzxyCanonicalBaseURL + "/info/1036/10008.htm": {"学术活动文章", "", "", "学术活动"},
	}
	for url, expected := range want {
		if got, ok := byURL[url]; !ok {
			t.Errorf("missing home item %q", url)
		} else if got != expected {
			t.Errorf("home item %q = %#v, want %#v", url, got, expected)
		}
	}
}

func TestJzxyArticleParserExtractsCompleteMetadataBodyImagesAndOutsideAttachment(t *testing.T) {
	article, err := parseAdapter_jzxyArticle(readJzxyFixture(t, "jzxy_article.html"), "http://jzxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "建筑学院完整文章标题（不使用列表截断文本）" {
		t.Fatalf("article title = %q", article.Title)
	}
	if article.Date != "2026-07-09" || article.PublishedAt != "2026-07-09 14:03:05" {
		t.Fatalf("publication time = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Type != "本科生通知" {
		t.Fatalf("article type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "这是建筑学院文章正文。") || len(article.Images) != 1 || article.Images[0] != jzxyCanonicalBaseURL+"/__local/body.jpg" {
		t.Fatalf("body/images = (%q, %#v)", article.Content, article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="`+jzxyCanonicalBaseURL+`/__local/body.jpg"`) {
		t.Fatalf("content HTML did not canonicalize image: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("attachments = %#v, want one internal attachment", article.Attachments)
	}
	if article.Attachments[0].Name != "建筑学院测试附件.pdf" || article.Attachments[0].URL != jzxyCanonicalBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1&wbfileid=JZXYFIX" {
		t.Fatalf("attachment = %#v", article.Attachments[0])
	}
}

func TestJzxyStrictShellErrorAndEmptyListRegression(t *testing.T) {
	if items, err := parseAdapter_jzxyList(readJzxyFixture(t, "jzxy_empty.html"), jzxyCanonicalBaseURL); err != nil || len(items) != 0 {
		t.Fatalf("valid empty list = (%#v, %v), want empty nil result", items, err)
	}
	for _, name := range []string{"jzxy_static.html", "jzxy_error.html"} {
		if _, err := parseAdapter_jzxyList(readJzxyFixture(t, name), jzxyCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as a list", name)
		}
		if _, err := parseAdapter_jzxyArticle(readJzxyFixture(t, name), jzxyCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as an article", name)
		}
	}
	if _, err := parseAdapter_jzxyArticle(readJzxyFixture(t, "jzxy_empty.html"), jzxyCanonicalBaseURL); err == nil {
		t.Error("empty list shell was accepted as an article")
	}
}
