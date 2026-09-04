package scraper

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

func lixueFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return body
}

func lixueHasSubPage(slug string) bool {
	for _, known := range lixueSubPages {
		if known == slug {
			return true
		}
	}
	return false
}

func TestLixueAdapterRegistration(t *testing.T) {
	site := AllSites["lixue"]
	if site == nil {
		t.Fatal("lixue site is not registered")
	}
	if site.BaseURL != lixueCanonicalBaseURL {
		t.Fatalf("lixue base URL = %q, want %q", site.BaseURL, lixueCanonicalBaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("lixue adapter did not install all strict parsing hooks")
	}
	for _, required := range []string{
		"xwgg/xytz", "xwgg/xytz/dj", "xwgg/xytz/jx", "xwgg/xytz/ky", "xwgg/xytz/xg", "xwgg/xytz/zj", "xwgg/xytz/my",
		"xwgg/xyxw", "xwgg/ttxw", "xshd",
		"djgz/djdt", "djgz/djtz", "djgz/jgdj", "djgz/xsdj", "djgz/llxx",
		"jyjx", "jyjx/jxxw", "jyjx/kcjs", "jyjx/jcjs", "jyjx/jxjy", "jyjx/jxtd", "jyjx/yjsjy",
		"xsky", "xsky/xkxw", "xsky/xkgk", "xsky/xkpt", "xsky/xktz", "xsky/yjfx", "xsky/yjtd",
		"xsgz/xgtz", "xsgz/xgdt", "xsgz/txhd", "xsgz/bjfc1",
		"jwgz", "jwgz/jwtz", "jwgz/jwdt", "ghgz", "ghgz/ghtz", "ghgz/ghdt",
		"xygz", "xygz/xyfc", "xygz/xyhd",
		"jlhz", "jlhz/gjxshy", "jlhz/gjzmzjlf", "jlhz/gjhytybg", "jlhz/gjhkc", "jlhz/lxtd", "jlhz/gjxsjgrz",
		"zsjy", "zsjy/zsgz", "zsjy/yjsds", "zsjy/jygz", "xzzq", "sycc",
	} {
		if !lixueHasSubPage(required) {
			t.Errorf("audited feed %q is not registered", required)
		}
	}
	for _, excluded := range []string{
		"xygk", "xygk/xyjj", "xygk/jgsz", "xygk/xyld", "xygk/lxwm", "xygk/lxcgz",
		"szll/jsdw", "szll/szjj", "szll/dsml1", "djgz", "djgz/dwgk",
		"jyjx/zyjs", "jyjx/bkjw1", "xsgz", "xsgz/xggk1", "jwgz/jwgk", "ghgz/ghgk", "yxjd", "lxxq",
	} {
		if lixueHasSubPage(excluded) {
			t.Errorf("static/duplicate page %q must not be registered as a feed", excluded)
		}
	}
}

func TestLixueAuditedFeedPaginationMatrix(t *testing.T) {
	tests := []struct {
		slug  string
		total int
	}{
		{"xwgg/xytz", 55}, {"xwgg/xytz/dj", 2}, {"xwgg/xytz/jx", 6}, {"xwgg/xytz/ky", 1},
		{"xwgg/xytz/xg", 6}, {"xwgg/xytz/zj", 1}, {"xwgg/xytz/my", 3}, {"xwgg/xyxw", 163},
		{"xwgg/ttxw", 2}, {"xshd", 6},
		{"djgz/djdt", 10}, {"djgz/djtz", 1}, {"djgz/jgdj", 2}, {"djgz/xsdj", 8}, {"djgz/llxx", 12},
		{"jyjx", 1}, {"jyjx/jxxw", 8}, {"jyjx/kcjs", 1}, {"jyjx/jcjs", 1}, {"jyjx/jxjy", 1},
		{"jyjx/jxtd", 1}, {"jyjx/yjsjy", 1},
		{"xsky", 2}, {"xsky/xkxw", 2}, {"xsky/xkgk", 1}, {"xsky/xkpt", 1}, {"xsky/xktz", 1},
		{"xsky/yjfx", 1}, {"xsky/yjtd", 1},
		{"xsgz/xgtz", 29}, {"xsgz/xgdt", 10}, {"xsgz/txhd", 17}, {"xsgz/bjfc1", 1},
		{"jwgz", 2}, {"jwgz/jwtz", 1}, {"jwgz/jwdt", 2}, {"ghgz", 6}, {"ghgz/ghtz", 1}, {"ghgz/ghdt", 6},
		{"xygz", 2}, {"xygz/xyfc", 1}, {"xygz/xyhd", 1},
		{"jlhz", 2}, {"jlhz/gjxshy", 1}, {"jlhz/gjzmzjlf", 1}, {"jlhz/gjhytybg", 1}, {"jlhz/gjhkc", 1},
		{"jlhz/lxtd", 1}, {"jlhz/gjxsjgrz", 1},
		{"zsjy", 2}, {"zsjy/zsgz", 1}, {"zsjy/yjsds", 1}, {"zsjy/jygz", 2}, {"xzzq", 1}, {"sycc", 2},
	}
	if len(tests) != len(lixueSubPages) {
		t.Fatalf("pagination matrix has %d feeds, registry has %d", len(tests), len(lixueSubPages))
	}
	for _, tc := range tests {
		if !lixueHasSubPage(tc.slug) {
			t.Errorf("pagination matrix feed %q is not registered", tc.slug)
			continue
		}
		first := lixuePageURL(lixueCanonicalBaseURL, tc.slug, 1, tc.total, ".htm")
		if want := lixueCanonicalBaseURL + "/" + tc.slug + ".htm"; first != want {
			t.Errorf("%s first page = %q, want %q", tc.slug, first, want)
		}
		last := lixuePageURL(lixueCanonicalBaseURL, tc.slug, tc.total, tc.total, ".htm")
		if tc.total == 1 {
			if last != first {
				t.Errorf("%s single page last = %q, want %q", tc.slug, last, first)
			}
		} else if want := lixueCanonicalBaseURL + "/" + tc.slug + "/1.htm"; last != want {
			t.Errorf("%s last page = %q, want %q", tc.slug, last, want)
		}
		// Page one is the unnumbered newest page; choose a numbered middle
		// page for even-sized feeds so the reverse mapping is exercised too.
		middle := (tc.total + 2) / 2
		middleURL := lixuePageURL(lixueCanonicalBaseURL, tc.slug, middle, tc.total, ".htm")
		if tc.total > 1 {
			want := lixueCanonicalBaseURL + "/" + tc.slug + "/" + fmt.Sprint(tc.total-middle+1) + ".htm"
			if middleURL != want {
				t.Errorf("%s logical page %d = %q, want %q", tc.slug, middle, middleURL, want)
			}
		}
		if got := lixuePageURL(lixueCanonicalBaseURL, tc.slug, 0, tc.total, ".htm"); got != "" {
			t.Errorf("%s page zero = %q, want refusal", tc.slug, got)
		}
		if got := lixuePageURL(lixueCanonicalBaseURL, tc.slug, tc.total+1, tc.total, ".htm"); got != "" {
			t.Errorf("%s out-of-range page = %q, want refusal", tc.slug, got)
		}
	}
}

func TestLixueTotalPagesRejectsUncountedShell(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	t.Cleanup(func() {
		defaultClient, NoCache = oldClient, oldNoCache
		ClearCache()
	})
	ClearCache()
	NoCache = true
	defaultClient = &http.Client{Transport: lixueTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		body := []byte(`<meta name="pageType" content="2"><div class="page-news"><div class="hots"><ul></ul></div><div class="p_pages"><span>下一页</span></div></div>`)
		return lixueTestResponse(req, http.StatusOK, "text/html; charset=utf-8", body), nil
	})}
	if _, err := lixueTotalPages("xwgg/xytz"); err == nil {
		t.Fatal("uncounted list shell was accepted as a one-page feed")
	}
}

func TestLixuePageURLAndCount(t *testing.T) {
	body := lixueFixture(t, "lixue_list.html")
	if got := lixuePageCountDocument(t, body); got != 3 {
		t.Fatalf("lixue page count = %d, want 3", got)
	}
	base := lixueCanonicalBaseURL
	if got := lixuePageURL(base, "xwgg/xyxw", 1, 3, ".htm"); got != base+"/xwgg/xyxw.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := lixuePageURL(base, "xwgg/xyxw", 2, 3, ".htm"); got != base+"/xwgg/xyxw/2.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := lixuePageURL(base, "xwgg/xyxw", 3, 3, ".htm"); got != base+"/xwgg/xyxw/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := lixuePageURL(base, "xwgg/xyxw", 2, 0, ".htm"); got != "" {
		t.Fatalf("page URL without source total = %q, want refusal", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 3}, {4, 3}, {2, 1},
	} {
		if got := lixuePageURL(base, "xwgg/xyxw", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("invalid page %d/%d produced %q", tc.page, tc.total, got)
		}
	}
}

func lixuePageCountDocument(t *testing.T, body []byte) int {
	t.Helper()
	doc, err := lixueDocument(body)
	if err != nil {
		t.Fatal(err)
	}
	return lixuePageCount(doc)
}

func lixueDocument(body []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func TestLixueListParsingAndStrictShell(t *testing.T) {
	items, err := parseAdapter_lixueList(lixueFixture(t, "lixue_list.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("parsed %d list items, want 2 local article rows", len(items))
	}
	if items[0].Title != "首条学院新闻" || items[0].Date != "2026-08-21" || items[0].Type != "学院新闻" {
		t.Fatalf("first list item = %#v", items[0])
	}
	if items[0].URL != lixueCanonicalBaseURL+"/info/1059/10101.htm" {
		t.Fatalf("first list URL = %q", items[0].URL)
	}
	if _, err := parseAdapter_lixueList(lixueFixture(t, "lixue_unrecognized.html"), lixueCanonicalBaseURL); err == nil {
		t.Fatal("unrecognized pageType=2 shell was accepted")
	}
	malformed := []byte(`<meta name="pageType" content="2"><div class="page-news"><div class="hots"><ul><li><a href="/info/1059/10101.htm"></a></li></ul></div></div>`)
	if _, err := parseAdapter_lixueList(malformed, lixueCanonicalBaseURL); err == nil {
		t.Fatal("malformed local lixue row was silently discarded")
	}
}

func TestLixuePartyListAndEmptyFeed(t *testing.T) {
	items, err := parseAdapter_lixueList(lixueFixture(t, "lixue_party_list.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse party list: %v", err)
	}
	if len(items) != 2 || items[0].Type != "党建动态" || items[0].Date != "2026-06-08" {
		t.Fatalf("party list items = %#v", items)
	}
	items, err = parseAdapter_lixueList(lixueFixture(t, "lixue_empty.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse valid empty feed: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty feed returned %d items", len(items))
	}
}

func TestLixueHomepageModulesAndDeduplication(t *testing.T) {
	items, err := parseAdapter_lixueList(lixueFixture(t, "lixue_home.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse homepage: %v", err)
	}
	if len(items) != 8 {
		t.Fatalf("parsed %d homepage items, want 8 scoped local articles", len(items))
	}
	byURL := make(map[string]model.NewsItem, len(items))
	for _, item := range items {
		byURL[item.URL] = item
	}
	news := byURL[lixueCanonicalBaseURL+"/info/1059/10001.htm"]
	if news.Title != "学院在重大工程中的完整标题" || news.Date != "2026-08-18" {
		t.Fatalf("deduplicated news = %#v", news)
	}
	if byURL[lixueCanonicalBaseURL+"/info/1226/10004.htm"].Type != "党建" {
		t.Fatalf("hidden notice tab type = %#v", byURL[lixueCanonicalBaseURL+"/info/1226/10004.htm"])
	}
	if byURL[lixueCanonicalBaseURL+"/info/1336/10006.htm"].Date != "" {
		t.Fatal("event time was incorrectly treated as publication date")
	}
	if _, exists := byURL[lixueCanonicalBaseURL+"/info/1003/3202.htm"]; exists {
		t.Fatal("footer navigation link was accepted as homepage news")
	}
}

func TestLixueArticleContentMetadataAndAttachments(t *testing.T) {
	article, err := parseAdapter_lixueArticle(lixueFixture(t, "lixue_article.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse article: %v", err)
	}
	if article.Title != "共产党手机报学习链接" || article.Date != "2026-06-08" || article.PublishedAt != "2026-06-08 09:43:00" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Type != "党建动态" {
		t.Fatalf("article type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "请通过线上线下等途径学习使用共产党员手机报") || !strings.Contains(article.Content, "12371.people.com.cn") {
		t.Fatalf("article body was not extracted: %q", article.Content)
	}
	if !strings.Contains(article.ContentHTML, `src="`+lixueCanonicalBaseURL+`/__local/1/A2/test.png"`) || !strings.Contains(article.ContentHTML, "https://12371.people.com.cn/") {
		t.Fatalf("article content HTML was not preserved: %q", article.ContentHTML)
	}
	if len(article.Images) != 1 || article.Images[0] != lixueCanonicalBaseURL+"/__local/1/A2/test.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
	if article.Attachments[0].Name != "附件1 考核表.docx" || !strings.Contains(article.Attachments[0].URL, "wbfileid=ABC123") {
		t.Fatalf("first attachment = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "实施细则.pdf" || !strings.Contains(article.Attachments[1].URL, "wbfileid=DEF456") {
		t.Fatalf("second attachment = %#v", article.Attachments[1])
	}
}

func TestLixueFixedPublicationDateIgnoresBodyDates(t *testing.T) {
	article, err := parseAdapter_lixueArticle(lixueFixture(t, "lixue_article_body_dates.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse body-date article: %v", err)
	}
	if article.Title != "只从固定节点读取发布日期" || article.Date != "2026-01-02" || article.PublishedAt != "2026-01-02" {
		t.Fatalf("body-date article metadata = %#v", article)
	}

	article, err = parseAdapter_lixueArticle(lixueFixture(t, "lixue_no_publication.html"), lixueCanonicalBaseURL)
	if err != nil {
		t.Fatalf("parse article without publication node: %v", err)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("body-only dates were accepted: %#v", article)
	}
}

func TestLixueRejectsErrorAndVerificationPages(t *testing.T) {
	for _, name := range []string{"lixue_waf.html", "lixue_login.html", "lixue_captcha.html"} {
		if _, err := parseAdapter_lixueList(lixueFixture(t, name), lixueCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as a list", name)
		}
		if _, err := parseAdapter_lixueArticle(lixueFixture(t, name), lixueCanonicalBaseURL); err == nil {
			t.Errorf("%s was accepted as an article", name)
		}
	}
}

func TestLixueFetchArticleVerifiesImagesAndAttachments(t *testing.T) {
	oldClient, oldNoCache := defaultClient, NoCache
	t.Cleanup(func() {
		defaultClient, NoCache = oldClient, oldNoCache
		ClearCache()
	})
	ClearCache()
	NoCache = true
	resourceRequests := 0
	badRefererRequests := 0
	defaultClient = &http.Client{Transport: lixueTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/info/1261/10201.htm" {
			return lixueTestResponse(req, http.StatusOK, "text/html; charset=utf-8", lixueFixture(t, "lixue_article_resources.html")), nil
		}
		resourceRequests++
		if req.Header.Get("Referer") != lixueCanonicalBaseURL+"/info/1261/10201.htm" {
			badRefererRequests++
		}
		switch req.URL.Path {
		case "/__local/lixue/valid.png":
			return lixueTestResponse(req, http.StatusOK, "image/png", []byte("\x89PNG\r\n\x1a\nvalid")), nil
		case "/__local/lixue/invalid.png":
			return lixueTestResponse(req, http.StatusOK, "image/png", []byte("<html>请输入验证码</html>")), nil
		case "/system/_content/download.jsp":
			switch req.URL.Query().Get("wbfileid") {
			case "ABC123":
				return lixueTestResponse(req, http.StatusOK, "application/octet-stream", []byte("PK\x03\x04fixture")), nil
			case "CAPTCHA":
				return lixueTestResponse(req, http.StatusOK, "text/html; charset=UTF-8", []byte("<!DOCTYPE html><html>请输入验证码下载附件</html>")), nil
			}
		}
		return lixueTestResponse(req, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
	})}

	article, raw, err := lixueFetchArticle(lixueCanonicalBaseURL + "/info/1261/10201.htm")
	if err != nil {
		t.Fatalf("fetch resource article: %v", err)
	}
	if len(raw) == 0 || article.Date != "2026-08-21" || article.PublishedAt != "2026-08-21 10:11:12" {
		t.Fatalf("fetched article archive/metadata = raw=%d article=%#v", len(raw), article)
	}
	if len(article.Images) != 1 || !strings.HasSuffix(article.Images[0], "/__local/lixue/valid.png") {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "有效材料.docx" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, "/__local/lixue/valid.png") || !strings.Contains(article.ContentHTML, "wbfileid=ABC123") {
		t.Fatalf("verified resources were not retained in content HTML: %q", article.ContentHTML)
	}
	if strings.Contains(article.ContentHTML, "invalid.png") || strings.Contains(article.ContentHTML, "wbfileid=CAPTCHA") || strings.Contains(article.ContentHTML, "javascript:") {
		t.Fatalf("invalid resources survived content sanitization: %q", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, "https://example.com/ordinary") {
		t.Fatalf("ordinary external link was incorrectly removed: %q", article.ContentHTML)
	}
	if resourceRequests != 4 || badRefererRequests != 0 {
		t.Fatalf("resource validation requests = %d, bad referers = %d, want 4/0", resourceRequests, badRefererRequests)
	}
}

func TestLixueURLAndColumnTypeGuards(t *testing.T) {
	if _, ok := lixueArticleURL(lixueCanonicalBaseURL, "https://other.example/info/1059/1.htm"); ok {
		t.Fatal("external article URL was accepted")
	}
	if _, ok := lixueArticleURL(lixueCanonicalBaseURL, "info/1059/1.htm#fragment"); !ok {
		t.Fatal("valid local article URL was rejected")
	}
	if got := lixueTypeForURL("http://lixue.swjtu.edu.cn/info/1261/17233.htm"); got != "党建动态" {
		t.Fatalf("party column type = %q", got)
	}
	if got := lixueTypeForURL("https://lixue.swjtu.edu.cn/info/1028/17453.htm"); got != "理论学习" {
		t.Fatalf("theory column type = %q", got)
	}
	if got := lixueTypeForURL("https://lixue.swjtu.edu.cn/info/1035/17453.htm"); got != "班级风采" {
		t.Fatalf("class showcase column type = %q", got)
	}
	if got := lixueTypeForURL("https://lixue.swjtu.edu.cn/info/9999/1.htm"); got != "" {
		t.Fatalf("unknown column type = %q, want empty", got)
	}
}

type lixueTestRoundTripper func(*http.Request) (*http.Response, error)

func (f lixueTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func lixueTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d", status),
		Header:        http.Header{"Content-Type": []string{contentType}},
		Body:          io.NopCloser(strings.NewReader(string(body))),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
