package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const sflTestBaseURL = "http://sfl.swjtu.edu.cn"

func readSflFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read SFL fixture %q: %v", name, err)
	}
	return body
}

func resetSflTestState() {
	sflState.Lock()
	sflState.dates = make(map[string]string)
	sflState.publishedAt = make(map[string]string)
	sflState.pageCounts = make(map[string]int)
	sflState.Unlock()
}

func TestSflAdapterRegistrationCoversEveryLiveArticleTab(t *testing.T) {
	site := AllSites["sfl"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("SFL adapter hooks are incomplete")
	}
	if !reflect.DeepEqual(site.SubPages, sflSubPages) {
		t.Fatalf("registered SFL feeds differ from audited feeds: got %d, want %d", len(site.SubPages), len(sflSubPages))
	}
	seen := make(map[string]bool, len(site.SubPages))
	for _, slug := range site.SubPages {
		if seen[slug] {
			t.Fatalf("duplicate SFL feed %q", slug)
		}
		seen[slug] = true
		if typ := sflFeedTypes[slug]; typ == "" {
			t.Errorf("feed %q has no audited type", slug)
		}
	}
	for _, static := range []string{"jyjx/dxyy", "xsky/yjtd", "xygz/xyxw", "zsjy/bkzs"} {
		if seen[static] {
			t.Errorf("static page %q was incorrectly registered as a feed", static)
		}
	}
	if got := len(seen); got != 63 {
		t.Fatalf("audited SFL feed count = %d, want 63", got)
	}
}

func TestSflListUsesDetailSafeTitlesPreciseDatesAndRealType(t *testing.T) {
	resetSflTestState()
	items, err := parseSflNews(readSflFixture(t, "sfl_list.html"), sflTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d local items, want 2: %#v", len(items), items)
	}
	if items[0].Title != "外国语学院举办2026年第9期“创源大讲堂”：陆小飞教授主讲外语教学与研究视域下的功能性与批判性AI素养" {
		t.Fatalf("first title was not taken from title attribute: %q", items[0].Title)
	}
	if items[0].Type != "学术科研" || items[1].Type != "学术科研" {
		t.Fatalf("list type = %q/%q, want 学术科研", items[0].Type, items[1].Type)
	}
	if items[0].Date != "2026-06-12" || items[0].PublishedAt != "2026-06-12 14:03:07" {
		t.Fatalf("precise list publication = %q/%q", items[0].Date, items[0].PublishedAt)
	}
	if items[1].Date != "2026-06-11" || items[1].PublishedAt != "2026-06-11" {
		t.Fatalf("second list publication = %q/%q", items[1].Date, items[1].PublishedAt)
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, sflTestBaseURL+"/info/") {
			t.Errorf("non-local article leaked: %q", item.URL)
		}
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(readSflFixture(t, "sfl_list.html"))))
	if err != nil {
		t.Fatal(err)
	}
	_, current, pages, ok := sflPageInfoFromDocument(doc)
	if !ok || current != 1 || pages != 1 {
		t.Fatalf("pagination marker = current %d/pages %d/ok %v", current, pages, ok)
	}
}

func TestSflHomeParserScopesSevenNewsAndNoticeTabs(t *testing.T) {
	resetSflTestState()
	items, err := parseSflNews(readSflFixture(t, "sfl_home.html"), sflTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d home items, want 4: %#v", len(items), items)
	}
	want := map[string]struct {
		typ  string
		date string
	}{
		"39005": {typ: "学院新闻"},
		"38275": {typ: "本科生教育"},
		"38325": {typ: "通知公告", date: "2026-06-24"},
		"38975": {typ: "本科生教育", date: "2026-07-28"},
	}
	for _, item := range items {
		id := item.URL[strings.LastIndex(item.URL, "/")+1:]
		id = strings.TrimSuffix(id, ".htm")
		wantItem, ok := want[id]
		if !ok {
			t.Errorf("unexpected home item: %+v", item)
			continue
		}
		if item.Type != wantItem.typ || item.Date != wantItem.date {
			t.Errorf("home item %s type/date = %q/%q, want %q/%q", id, item.Type, item.Date, wantItem.typ, wantItem.date)
		}
		if strings.Contains(item.URL, "30001") {
			t.Errorf("image-card article leaked into home list: %+v", item)
		}
	}
}

func TestSflEmptyAndBlockedResponsesAreDistinguished(t *testing.T) {
	resetSflTestState()
	empty, err := parseSflNews(readSflFixture(t, "sfl_empty_list.html"), sflTestBaseURL)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("source-reported empty list = %#v, err=%v", empty, err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(readSflFixture(t, "sfl_empty_list.html"))))
	if err != nil {
		t.Fatal(err)
	}
	_, current, pages, ok := sflPageInfoFromDocument(doc)
	if !ok || current != 0 || pages != 0 {
		t.Fatalf("empty source marker = current %d/pages %d/ok %v", current, pages, ok)
	}

	for name, html := range map[string]string{
		"blocked":  `<html><head><title>安全验证</title></head><body>请输入验证码</body></html>`,
		"static":   `<html><body><h1>研究团队</h1><p>静态介绍页</p></body></html>`,
		"external": `<ul class="list-group rightnew"><li><a href="https://example.com/info/1/2.htm" title="外部站点文章标题">外部</a></li></ul><div class="lit"><td id="fanye1">共1条 1/1</td></div>`,
	} {
		if _, err := parseSflNews([]byte(html), sflTestBaseURL); err == nil {
			t.Errorf("%s response was accepted as a valid SFL feed", name)
		}
	}
}

func TestSflReversePaginationChecksFirstMiddleLastAndBounds(t *testing.T) {
	resetSflTestState()
	base := sflTestBaseURL
	cases := []struct {
		page int
		want string
	}{
		{1, base + "/xsky/xwgg.htm"},
		{2, base + "/xsky/xwgg/13.htm"},
		{7, base + "/xsky/xwgg/8.htm"},
		{14, base + "/xsky/xwgg/1.htm"},
	}
	for _, tc := range cases {
		if got := sflPageURL(base, "xsky/xwgg", tc.page, 14, ".htm"); got != tc.want {
			t.Errorf("logical page %d URL = %q, want %q", tc.page, got, tc.want)
		}
	}
	for _, tc := range []struct {
		slug  string
		page  int
		total int
	}{
		{"xsky/xwgg", 15, 14},
		{"xsky/xwgg", 0, 14},
		{"unknown", 1, 4},
		{"zsjy/yjszs", 2, 0},
	} {
		if got := sflPageURL(base, tc.slug, tc.page, tc.total, "htm"); got != "" {
			t.Errorf("out-of-range/unknown page %s/%d produced %q", tc.slug, tc.page, got)
		}
	}
	if got := sflPageURL(base, "xsky/xwgg", 2, 14, "html"); got != base+"/xsky/xwgg/13.html" {
		t.Errorf("extension normalization = %q", got)
	}
}

func TestSflArticleUsesDetailTitleFixedDatesAndResourceFilters(t *testing.T) {
	resetSflTestState()
	article, err := parseSflArticle(readSflFixture(t, "sfl_article.html"), sflTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	wantTitle := "外国语学院关于组织参加第十五届“挑战杯”东北振兴产业升级专项赛作品的通知"
	if article.Title != wantTitle {
		t.Fatalf("detail title = %q, want %q", article.Title, wantTitle)
	}
	if article.Type != "学工动态" {
		t.Fatalf("detail type = %q, want 学工动态", article.Type)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("body event date was mistaken for publication date: %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "正文第一段必须完整保留") || !strings.Contains(article.Content, "正文第二段也必须完整保留") {
		t.Fatalf("article body was not preserved: %q", article.Content)
	}
	if len(article.Images) != 1 || !strings.Contains(article.Images[0], "real-image.png") {
		t.Fatalf("filtered article images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "可下载方案.pdf" || !strings.Contains(article.Attachments[0].URL, "/files/valid.pdf") {
		t.Fatalf("filtered article attachments = %#v", article.Attachments)
	}

	articleURL := sflTestBaseURL + "/info/1272/38905.htm"
	sflRememberDate(articleURL, "[2026-08-01 09:08:07]")
	sflApplyCachedPublication(article, articleURL)
	if article.Date != "2026-08-01" || article.PublishedAt != "2026-08-01 09:08:07" {
		t.Fatalf("cached list publication = %q/%q", article.Date, article.PublishedAt)
	}

	const explicitHTML = `<html><body><div class="righttop"><span>通知公告</span></div><h1 class="newtitle2">固定发布日期测试标题</h1><div class="article-meta">发布时间：2026/07/01 09:08:07</div><div class="v_news_content"><p>正文</p></div></body></html>`
	explicit, err := parseSflArticle([]byte(explicitHTML), sflTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Date != "2026-07-01" || explicit.PublishedAt != "2026-07-01 09:08:07" {
		t.Fatalf("fixed publication metadata = %q/%q", explicit.Date, explicit.PublishedAt)
	}
	if _, err := parseSflArticle([]byte(`<html><head><title>404错误提示</title></head><body>404错误提示</body></html>`), sflTestBaseURL); err == nil {
		t.Fatal("404 response was accepted as an SFL article")
	}
}

func TestSflResourceVerificationChecksTypeMagicAndCaptcha(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	for _, tc := range []struct {
		name        string
		contentType string
		body        []byte
		resource    bool
		want        bool
	}{
		{"real png", "image/png", png, true, true},
		{"html posing as image", "image/png", []byte("<!doctype html><title>登录</title>"), true, false},
		{"real pdf", "application/pdf", []byte("%PDF-1.7\n"), false, true},
		{"html posing as pdf", "application/pdf", []byte("<!doctype html><title>附件下载</title>"), false, false},
		{"zip magic", "application/octet-stream", []byte("PK\x03\x04archive"), false, true},
	} {
		if got := sflResourceBodyIsUsable(tc.contentType, tc.body, tc.name, tc.resource); got != tc.want {
			t.Errorf("%s: usable = %v, want %v", tc.name, got, tc.want)
		}
	}
	if !sflIsCaptchaDownloadURL(sflTestBaseURL + "/system/_content/download.jsp?wbfileid=CAPTCHA") {
		t.Fatal("SFL download endpoint was not classified as CAPTCHA-protected")
	}
	if sflIsCaptchaDownloadURL(sflTestBaseURL + "/files/valid.pdf") {
		t.Fatal("ordinary file URL was classified as CAPTCHA-protected")
	}
}

func TestSflLiveResourceFetchChecksHTTPAndMagic(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		case "/valid.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\n"))
		case "/fake.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<!doctype html><title>附件下载</title>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if body, contentType, err := sflFetchResource(server.URL+"/valid.png", server.URL, true); err != nil || contentType != "image/png" || len(body) != len(png) {
		t.Fatalf("valid image fetch = %d bytes, %q, %v", len(body), contentType, err)
	}
	if _, _, err := sflFetchResource(server.URL+"/fake.png", server.URL, true); err == nil {
		t.Fatal("HTML response posing as an image was accepted")
	}
	if _, _, err := sflFetchResource(server.URL+"/missing.pdf", server.URL, false); err == nil {
		t.Fatal("404 resource was accepted")
	}
	attachments := sflVerifyAttachments([]model.Attachment{
		{Name: "valid.pdf", URL: server.URL + "/valid.pdf"},
		{Name: "missing.pdf", URL: server.URL + "/missing.pdf"},
	}, server.URL)
	if len(attachments) != 1 || attachments[0].Size != "9" {
		t.Fatalf("verified attachments = %#v", attachments)
	}
}
