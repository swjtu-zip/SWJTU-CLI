package scraper

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func readLibFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read lib fixture %q: %v", name, err)
	}
	return body
}

func TestLibAdapterRegistrationAndCanonicalPagination(t *testing.T) {
	site := AllSites["lib"]
	if site == nil || site.BaseURL != libCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("lib adapter is incomplete: %#v", site)
	}
	wantFeeds := []string{
		"resources", "news", "image-news", "latest", "notices", "publicity",
		"lectures", "activities", "exhibitions",
	}
	if !reflect.DeepEqual(site.SubPages, wantFeeds) {
		t.Fatalf("lib feeds = %#v, want %#v", site.SubPages, wantFeeds)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "news", 1, 5, ".htm"); got != libCanonicalBaseURL+"/news/web_newsList?cid=Ds8XD6N32k1BF9%24F&pageIndex=1" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "news", 3, 5, ".html"); got != libCanonicalBaseURL+"/news/web_newsList?cid=Ds8XD6N32k1BF9%24F&pageIndex=3" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "news", 5, 5, ".htm"); got != libCanonicalBaseURL+"/news/web_newsList?cid=Ds8XD6N32k1BF9%24F&pageIndex=5" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "index", 1, 1, ".htm"); got != libCanonicalBaseURL+"/" {
		t.Fatalf("homepage URL = %q", got)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "news", 6, 5, ".htm"); got != "" {
		t.Fatalf("out-of-range page URL = %q, want empty", got)
	}
	if got := libPageURL("http://www.lib.swjtu.edu.cn", "news", 2, 0, ".htm"); !strings.Contains(got, "pageIndex=2") {
		t.Fatalf("unknown-total page URL = %q", got)
	}
}

func TestLibHomepageParsesEveryArticleModuleAndDropsResources(t *testing.T) {
	items, err := parseAdapter_libList(readLibFixture(t, "lib_home.html"), "http://www.lib.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse lib homepage: %v", err)
	}
	if len(items) != 7 {
		t.Fatalf("homepage items = %d, want 7 (2 lecture JSON + exhibition + activity + 2 news + carousel)", len(items))
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Type]++
		if !strings.HasPrefix(item.URL, libCanonicalBaseURL+"/") {
			t.Errorf("homepage item escaped canonical host: %#v", item)
		}
		if strings.Contains(item.URL, "/pagelinklog/") || strings.Contains(item.URL, "/link/") {
			t.Errorf("homepage retained resource link: %#v", item)
		}
	}
	if counts["讲座"] != 2 || counts["展览"] != 1 || counts["活动"] != 1 || counts["最新动态"] != 1 || counts["公示"] != 1 || counts["图片新闻"] != 1 {
		t.Fatalf("homepage type counts = %#v", counts)
	}
	for _, item := range items {
		if item.Title == "带精确时间的讲座" {
			if item.Date != "2026-08-31" || item.PublishedAt != "2026-08-31 14:03:05" {
				t.Fatalf("precise homepage metadata = %#v", item)
			}
			return
		}
	}
	t.Fatal("homepage did not retain the full lecture JSON record")
}

func TestLibKeepsLegitimateLectureHeadlineEndingInBookTitleMark(t *testing.T) {
	if !libIsArticleTitle("高价值专利培育系列讲座之《面向研发活动的专利布局及案例分享》") {
		t.Fatal("legitimate lib headline ending in 》 was rejected")
	}
}

func TestLibListParsesAllRowsAndSourcePageCount(t *testing.T) {
	items, err := parseAdapter_libList(readLibFixture(t, "lib_list.html"), libCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse lib list: %v", err)
	}
	if len(items) != 10 {
		t.Fatalf("list items = %d, want 10 local rows", len(items))
	}
	if items[0].URL != libCanonicalBaseURL+"/Home/ServiceDetail/9001" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31" || items[0].Type != "最新动态" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if pages, known := libPageCountFromFixture(readLibFixture(t, "lib_list.html")); !known || pages != 132 {
		t.Fatalf("page count = %d/%v, want 132/true", pages, known)
	}
}

func TestLibLegacyTabsRemainSeparateArticleLists(t *testing.T) {
	items, err := parseAdapter_libList(readLibFixture(t, "lib_legacy_list.html"), libCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse lib exhibition list: %v", err)
	}
	if len(items) != 2 || items[0].Type != "展览" || items[0].URL != libCanonicalBaseURL+"/news/newsdetail/5887" {
		t.Fatalf("legacy tab items = %#v", items)
	}
	if pages, known := libPageCountFromFixture(readLibFixture(t, "lib_legacy_list.html")); !known || pages != 3 {
		t.Fatalf("legacy tab page count = %d/%v, want 3/true", pages, known)
	}
}

func TestLibArticleParsesMetadataBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_libArticle(readLibFixture(t, "lib_article.html"), "http://www.lib.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse lib article: %v", err)
	}
	if article.Title != "图书馆详情测试标题" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:03:05" || article.Type != "最新动态" || article.Source != "图书馆" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "完整正文段落") || !strings.Contains(article.ContentHTML, `src="https://www.lib.swjtu.edu.cn/images/body.jpg"`) {
		t.Fatalf("article body/resources not retained: content=%q html=%q", article.Content, article.ContentHTML)
	}
	if len(article.Images) != 1 || article.Images[0] != libCanonicalBaseURL+"/images/body.jpg" {
		t.Fatalf("article images = %#v", article.Images)
	}
	want := map[string]string{
		"附件1 PDF": libCanonicalBaseURL + "/files/notice.pdf",
		"附件2":     libCanonicalBaseURL + "/download?id=1",
	}
	if len(article.Attachments) != len(want) {
		t.Fatalf("article attachments = %#v, want %#v", article.Attachments, want)
	}
	for _, attachment := range article.Attachments {
		if want[attachment.Name] != attachment.URL {
			t.Errorf("unexpected attachment = %#v", attachment)
		}
	}
}

func TestLibArticleAcceptsLazyBodyImages(t *testing.T) {
	html := []byte(`<html><body><div class="news-detail"><h1>懒加载图片测试标题</h1><em><span>发布时间：2026-08-31</span></em><div class="news-content"><p>正文内容</p><img data-original="/images/lazy.jpg"><img src="data:image/gif;base64,placeholder"><a href="/files/test.pdf">附件</a></div></div></body></html>`)
	article, err := parseAdapter_libArticle(html, libCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(article.Images) != 1 || article.Images[0] != libCanonicalBaseURL+"/images/lazy.jpg" {
		t.Fatalf("lazy images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="`+libCanonicalBaseURL+`/images/lazy.jpg"`) {
		t.Fatalf("lazy image was not written to ContentHTML: %q", article.ContentHTML)
	}
}

func TestLibEmptyAndNonArticleResponsesAreStrict(t *testing.T) {
	items, err := parseAdapter_libList(readLibFixture(t, "lib_empty_list.html"), libCanonicalBaseURL)
	if err != nil {
		t.Fatalf("legal empty lib list returned error: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("legal empty lib list = %#v, want non-nil empty slice", items)
	}
	for _, name := range []string{"lib_static.html", "lib_captcha.html", "lib.html"} {
		if items, err := parseAdapter_libList(readLibFixture(t, name), libCanonicalBaseURL); err == nil || items != nil {
			t.Fatalf("%s was accepted as a list: items=%#v err=%v", name, items, err)
		}
		if article, err := parseAdapter_libArticle(readLibFixture(t, name), libCanonicalBaseURL); err == nil || article != nil {
			t.Fatalf("%s was accepted as an article: article=%#v err=%v", name, article, err)
		}
	}
}

func TestLibLiveAPIListFixtureUsesSourceColumnsAndDateOnly(t *testing.T) {
	feed, ok := libFeedForSlug("news")
	if !ok {
		t.Fatal("news feed is not registered")
	}
	items, pages, err := parseLibAPIList(readLibFixture(t, "lib_api_list_live.json"), feed)
	if err != nil {
		t.Fatalf("parse live API list: %v", err)
	}
	if len(items) != 2 || pages != 5 {
		t.Fatalf("live API list = %d items/%d pages, want 2/5", len(items), pages)
	}
	wantURL := libCanonicalBaseURL + "/news/web_newsDetails?cid=Ds8XD6N32k1BF9%24F&id=09922304-25f3-4114-acb5-9c7df478860d"
	if items[0].URL != wantURL || items[0].Type != "最新动态" || items[0].Date != "2026-09-04" || items[0].PublishedAt != "2026-09-04" {
		t.Fatalf("live API item = %#v, want URL/type/date from source fields", items[0])
	}
}

func TestLibLiveAPIArticleFixtureUsesDetailMetadataAndBodyScope(t *testing.T) {
	article, err := parseLibAPIArticle(readLibFixture(t, "lib_api_article_live.json"), libCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse live API article: %v", err)
	}
	if article.Title != "图书馆召开树立和践行正确政绩观学习教育总结会" || article.Source != "图书馆" || article.Type != "新闻动态" {
		t.Fatalf("live API article metadata = %#v", article)
	}
	if article.Date != "2026-09-04" || article.PublishedAt != "2026-09-04" {
		t.Fatalf("live API article date = %q/%q, body activity date leaked", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "活动时间：2026年12月31日") || strings.Contains(article.ContentHTML, "editor-style") {
		t.Fatalf("API body scope/style cleanup failed: content=%q html=%q", article.Content, article.ContentHTML)
	}
	if len(article.Images) != 2 || len(article.Attachments) != 2 || article.Attachments[0].Name != "会议材料.pdf" {
		t.Fatalf("API body resources = images %#v attachments %#v", article.Images, article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "external.pdf") {
		t.Fatal("external attachment was promoted into article resources")
	}
}

type libTestRoundTripFunc func(*http.Request) (*http.Response, error)

func (f libTestRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func libTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}
}

func TestLibAPIAndResourceFetchChecksPaginationAndBadAssets(t *testing.T) {
	oldClient := defaultClient
	defer func() { defaultClient = oldClient }()

	listFixture := readLibFixture(t, "lib_api_list_live.json")
	detailFixture := readLibFixture(t, "lib_api_article_live.json")
	var requestedPage int
	defaultClient = &http.Client{Transport: libTestRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/jwt/api/Auth/AccessToken":
			return libTestResponse(req, http.StatusOK, "application/json", []byte(`{"statusCode":"200","data":{"token":"fixture-token"}}`)), nil
		case "/gateway/news/api/news/pront-news-list-data-get":
			var payload struct {
				PageIndex int `json:"pageIndex"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			requestedPage = payload.PageIndex
			return libTestResponse(req, http.StatusOK, "application/json", listFixture), nil
		case "/gateway/news/api/news/pront-news-content-get":
			return libTestResponse(req, http.StatusOK, "application/json", detailFixture), nil
		case "/file/good.png":
			return libTestResponse(req, http.StatusOK, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}), nil
		case "/file/bad.png":
			return libTestResponse(req, http.StatusOK, "image/png", []byte("<html>captcha</html>")), nil
		case "/files/good.pdf":
			return libTestResponse(req, http.StatusOK, "application/pdf", []byte("%PDF-1.7\n")), nil
		case "/files/bad.pdf":
			return libTestResponse(req, http.StatusNotFound, "text/html", []byte("not found")), nil
		default:
			return libTestResponse(req, http.StatusNotFound, "text/html", []byte("not found")), nil
		}
	})}

	if _, pages, err := libListPage("news", 3); err != nil || pages != 5 || requestedPage != 3 {
		t.Fatalf("API page fetch = pages %d/requested %d/err %v", pages, requestedPage, err)
	}
	articleURL := libNewsDetailURL("Ds8XD6N32k1BF9$F", "09922304-25f3-4114-acb5-9c7df478860d")
	article, _, err := libFetchArticle(articleURL)
	if err != nil {
		t.Fatalf("fetch API article: %v", err)
	}
	if len(article.Images) != 1 || !strings.HasSuffix(article.Images[0], "/file/good.png") {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || !strings.HasSuffix(article.Attachments[0].URL, "/files/good.pdf") || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	for _, rejected := range []string{"bad.png", "bad.pdf", "external.pdf", "captcha"} {
		if strings.Contains(strings.ToLower(article.ContentHTML), strings.ToLower(rejected)) {
			t.Fatalf("rejected resource %q remained in ContentHTML: %s", rejected, article.ContentHTML)
		}
	}
}

func TestLibResourceValidationRequiresTypeMagicAndRejectsShells(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        []byte
		resource    string
		image       bool
		want        bool
	}{
		{"png", "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "good.png", true, true},
		{"missing type", "", []byte("anything"), "good.png", true, false},
		{"wrong image magic", "image/png", []byte("<html>captcha</html>"), "bad.png", true, false},
		{"pdf", "application/pdf", []byte("%PDF-1.7"), "good.pdf", false, true},
		{"wrong pdf magic", "application/pdf", []byte("plain text"), "bad.pdf", false, false},
		{"html shell", "application/octet-stream", []byte("<!doctype html><title>验证码</title>"), "download", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := libResourceBodyIsUsable(tc.contentType, tc.body, tc.resource, tc.image); got != tc.want {
				t.Fatalf("usable = %v, want %v", got, tc.want)
			}
		})
	}
}

func libPageCountFromFixture(raw []byte) (int, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return 0, false
	}
	return libPageCount(doc)
}
