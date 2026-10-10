package scraper

import (
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
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
	if site == nil || site.BaseURL != jzxyCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("jzxy adapter hooks are incomplete: %+v", site)
	}
	want := []string{
		"xxzx/xyxw", "xxzx/xygg", "xxzx/bkstz", "xxzx/yjstz", "xxzx/jyxx", "xxzx/xskj", "xxzx/xshdyg",
		"xxzx/xzzx", "djgz/djtz", "djgz/djxw", "djgz/jwgz", "djgz/xxyd", "hzjl/hzxm", "hzjl/jldt",
		"xsgz/xsgzzd", "xsgz/bgxz", "xygz", "xygz/xyhd", "xygz/byly", "xygz/jcxy/tysqxy",
		"xygz/jcxy/cdbxhxy", "zskj",
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
	if got := jzxyPageURL(jzxyCanonicalBaseURL, "index", 2, 1, ".htm"); got != "" {
		t.Errorf("home page 2/1 returned %q, want empty", got)
	}
}

func TestJzxyIndexFeedHasSingleScopedHomePage(t *testing.T) {
	site := AllSites["jzxy"]
	if site == nil || site.TotalPagesFunc == nil {
		t.Fatal("jzxy total-pages hook is missing")
	}
	// The home page is a single-page module digest; the total must be
	// reported without touching the network (ListNewsRange validates the
	// home shell while parsing page 1 before asking for the total).
	pages, err := site.TotalPagesFunc("index")
	if err != nil || pages != 1 {
		t.Fatalf("index total pages = (%d, %v), want (1, nil)", pages, err)
	}

	items, err := parseAdapter_jzxyList(readJzxyFixture(t, "jzxy_live_home.html"), jzxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 28 {
		t.Fatalf("parsed %d live home items, want 28: %#v", len(items), items)
	}
	byURL := make(map[string]model.NewsItem, len(items))
	for _, item := range items {
		if !strings.HasPrefix(item.URL, jzxyCanonicalBaseURL+"/info/") {
			t.Errorf("non-canonical live home URL: %#v", item)
		}
		byURL[item.URL] = item
	}
	// Hero carousel slides carry no invented metadata but must be kept.
	hero := byURL[jzxyCanonicalBaseURL+"/info/1241/17951.htm"]
	if hero.Title != "欢迎海内外英才依托建筑学院申报国家优秀青年科学基金项目（海外）" || hero.Type != "" || hero.Date != "" {
		t.Errorf("hero slide item = %#v", hero)
	}
	// Tab-scoped modules keep their column names and dates.
	news := byURL[jzxyCanonicalBaseURL+"/info/1032/24701.htm"]
	if news.Type != "学院新闻" || news.Date != "2026-07-10" {
		t.Errorf("news module item = %#v", news)
	}
	notice := byURL[jzxyCanonicalBaseURL+"/info/1030/25421.htm"]
	if notice.Type != "本科生通知" || notice.Date != "2026-10-09" {
		t.Errorf("notice tab item = %#v", notice)
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

func TestJzxyHomeParserIncludesInternalHeroSlidesOnly(t *testing.T) {
	items, err := parseAdapter_jzxyList(readJzxyFixture(t, "jzxy_home_banner.html"), jzxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("parsed %d hero items, want 2: %#v", len(items), items)
	}
	for _, item := range items {
		if item.Type != "" || item.Date != "" || item.PublishedAt != "" {
			t.Errorf("hero item has invented metadata: %#v", item)
		}
		if strings.Contains(item.URL, "mp.weixin.qq.com") {
			t.Errorf("external hero slide leaked: %#v", item)
		}
	}
	if items[0].URL != jzxyCanonicalBaseURL+"/info/1032/20001.htm" || items[0].Title != "桌面焦点文章完整标题" {
		t.Errorf("desktop hero = %#v", items[0])
	}
	if items[1].URL != jzxyCanonicalBaseURL+"/info/1032/20002.htm" || items[1].Title != "移动端焦点文章" {
		t.Errorf("mobile hero = %#v", items[1])
	}
}

func TestJzxyAdditionalArticleFeedsAndGallery(t *testing.T) {
	items, err := parseAdapter_jzxyList(readJzxyFixture(t, "jzxy_list_extra.html"), jzxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "下载中心真实文章" || items[0].Type != "下载中心" || items[0].Date != "2024-10-11" {
		t.Fatalf("download-center items = %#v", items)
	}
	doc, err := jzxyDocument(readJzxyFixture(t, "jzxy_list_extra.html"))
	if err != nil {
		t.Fatal(err)
	}
	if got := jzxyPageCount(doc); got != 2 {
		t.Fatalf("download-center page count = %d, want 2", got)
	}

	gallery, err := parseAdapter_jzxyList(readJzxyFixture(t, "jzxy_gallery.html"), jzxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(gallery) != 2 || gallery[0].Type != "毕业留影" || gallery[0].Date != "2025-06-27" || gallery[1].Date != "2024-07-01" {
		t.Fatalf("gallery items = %#v", gallery)
	}
	galleryDoc, err := jzxyDocument(readJzxyFixture(t, "jzxy_gallery.html"))
	if err != nil {
		t.Fatal(err)
	}
	if got := jzxyPageCount(galleryDoc); got != 1 {
		t.Fatalf("gallery page count = %d, want 1", got)
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

func TestJzxyArticleUsesOnlyFixedPublicationMetadata(t *testing.T) {
	article, err := parseAdapter_jzxyArticle(readJzxyFixture(t, "jzxy_article_body_date.html"), jzxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("body date polluted publication metadata: (%q, %q)", article.Date, article.PublishedAt)
	}
}

func TestJzxyLiveArticleWithoutBreadcrumbColumnUsesSectionHeader(t *testing.T) {
	article, err := parseAdapter_jzxyArticle(readJzxyFixture(t, "jzxy_live_article.html"), jzxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "欢迎海内外英才依托建筑学院申报国家优秀青年科学基金项目（海外）" {
		t.Fatalf("article title = %q", article.Title)
	}
	// The 首页高清大图 column renders no column link in the breadcrumb
	// ("首页 > 正文" only); the type must fall back to the section header.
	if article.Type != "首页高清大图" {
		t.Fatalf("article type = %q, want 首页高清大图", article.Type)
	}
	if article.Date != "2025-01-14" || article.PublishedAt != "2025-01-14" {
		t.Fatalf("publication time = (%q, %q)", article.Date, article.PublishedAt)
	}
}

func TestJzxyLiveArticleBreadcrumbColumnStillTakesPrecedence(t *testing.T) {
	article, err := parseAdapter_jzxyArticle(readJzxyFixture(t, "jzxy_live_article_notice.html"), jzxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "本科生通知" {
		t.Fatalf("article type = %q, want 本科生通知 from breadcrumb", article.Type)
	}
	if article.Date != "2026-10-09" || article.PublishedAt != "2026-10-09" {
		t.Fatalf("publication time = (%q, %q)", article.Date, article.PublishedAt)
	}
}

func TestJzxyResourceValidationUsesContentTypeAndMagic(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        []byte
		nameOrURL   string
		image       bool
		want        bool
	}{
		{"jpeg", "image/jpeg", []byte{0xff, 0xd8, 0xff, 0xd9}, "photo.jpg", true, true},
		{"png", "image/png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, "photo.png", true, true},
		{"bad image magic", "image/jpeg", []byte("not-an-image"), "photo.jpg", true, false},
		{"pdf", "application/octet-stream", []byte("%PDF-1.7\n"), "附件.pdf", false, true},
		{"ole document", "application/octet-stream", []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}, "附件.doc", false, true},
		{"captcha html", "text/html; charset=UTF-8", []byte("<!doctype html><h1>请输入验证码下载附件</h1>"), "附件.pdf", false, false},
	}
	for _, tc := range cases {
		if got := jzxyResourceBodyIsUsable(tc.contentType, tc.body, tc.nameOrURL, tc.image); got != tc.want {
			t.Errorf("%s usable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type jzxyTestRoundTripper func(*http.Request) (*http.Response, error)

func (f jzxyTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jzxyTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}
}

func TestJzxyFetchArticleVerifiesResourcesWithDetailReferer(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	defer func() {
		defaultClient = oldClient
		NoCache = oldNoCache
		ClearCache()
	}()
	ClearCache()
	NoCache = true
	articleURL := jzxyCanonicalBaseURL + "/info/1000/10009.htm"
	fixture := readJzxyFixture(t, "jzxy_article.html")
	resourceRequests := 0
	defaultClient = &http.Client{Transport: jzxyTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/info/1000/10009.htm":
			return jzxyTestResponse(req, http.StatusOK, "text/html; charset=utf-8", fixture), nil
		case "/__local/body.jpg":
			resourceRequests++
			if req.Header.Get("Referer") != articleURL {
				t.Errorf("resource Referer = %q, want %q", req.Header.Get("Referer"), articleURL)
			}
			return jzxyTestResponse(req, http.StatusOK, "image/jpeg", []byte{0xff, 0xd8, 0xff, 0xd9}), nil
		case "/system/_content/download.jsp":
			resourceRequests++
			if req.Header.Get("Referer") != articleURL {
				t.Errorf("attachment Referer = %q, want %q", req.Header.Get("Referer"), articleURL)
			}
			return jzxyTestResponse(req, http.StatusOK, "application/octet-stream", []byte("%PDF-1.7\nfixture")), nil
		default:
			return jzxyTestResponse(req, http.StatusNotFound, "text/html", []byte("not found")), nil
		}
	})}

	article, raw, err := jzxyFetchArticle(articleURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || resourceRequests != 2 || len(article.Images) != 1 || len(article.Attachments) != 1 || article.Attachments[0].Size == "" {
		t.Fatalf("verified article = raw=%d requests=%d images=%#v attachments=%#v", len(raw), resourceRequests, article.Images, article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, jzxyCanonicalBaseURL+"/__local/body.jpg") {
		t.Fatalf("verified image was removed from ContentHTML: %s", article.ContentHTML)
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
	if _, err := parseAdapter_jzxyList([]byte(`<html><head><meta name="pageType" content="2"></head><body><div class="news">module-only shell</div></body></html>`), jzxyCanonicalBaseURL); err == nil {
		t.Error("pageType=2 module-only shell was accepted as a home page")
	}
	if _, err := parseAdapter_jzxyList([]byte(`<html><head><title>404错误提示</title></head><body>页面不存在</body></html>`), jzxyCanonicalBaseURL); err == nil {
		t.Error("404 error shell was accepted as a list")
	}
}
