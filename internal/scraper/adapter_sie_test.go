package scraper

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func readSieFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read SIE fixture %s: %v", name, err)
	}
	return body
}

func TestSieAdapterRegistrationAndAuditedRoutes(t *testing.T) {
	site := AllSites["sie"]
	if site == nil || site.BaseURL != sieCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("SIE adapter hooks are incomplete: %#v", site)
	}
	if len(site.SubPages) != 21 {
		t.Fatalf("SIE routes = %d, want 21: %#v", len(site.SubPages), site.SubPages)
	}
	for _, route := range sieSubPages {
		if !sieIsKnownSubPage(route) || siePageTotals[route] < 1 {
			t.Errorf("route %q is not backed by a positive audited page total", route)
		}
	}
	for _, excluded := range []string{"index/hdrl", "jyjx/yys", "English1/Home"} {
		if sieIsKnownSubPage(excluded) {
			t.Errorf("static, empty, or external route was registered: %q", excluded)
		}
	}
}

func TestSieListUsesBothHomepageNewsBlocks(t *testing.T) {
	items, err := parseAdapter_sieList(readSieFixture(t, "sie.html"), sieCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 7 {
		t.Fatalf("got %d items, want 7: %#v", len(items), items)
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Type]++
		if item.Date == "" {
			t.Errorf("item %q has no date", item.Title)
		}
		if !strings.HasPrefix(item.URL, sieCanonicalBaseURL+"/info/") {
			t.Errorf("non-local item URL: %#v", item)
		}
	}
	if counts["学院新闻"] != 3 || counts["通知公告"] != 4 {
		t.Fatalf("homepage types = %#v, want 3/4: %#v", counts, items)
	}
}

func TestSieListFixturesAndReversePagination(t *testing.T) {
	cases := []struct {
		fixture string
		rows    int
		first   string
		date    string
	}{
		{"sie_xyxw_first_live.html", 2, "学院新闻首条真实列表记录", "2026-08-01"},
		{"sie_xyxw_middle_live.html", 1, "学院新闻中页真实列表记录", "2026-05-01"},
		{"sie_xyxw_last_live.html", 1, "学院新闻末页真实列表记录", "2025-01-08"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			items, err := parseAdapter_sieList(readSieFixture(t, tc.fixture), sieCanonicalBaseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.rows || items[0].Title != tc.first || items[0].Date != tc.date || items[0].Type != "学院新闻" {
				t.Fatalf("parsed rows = %#v", items)
			}
		})
	}
	if got := parseTotalPages(readSieFixture(t, "sie_xyxw_first_live.html")); got != 14 {
		t.Fatalf("source total pages = %d, want 14", got)
	}
	if got := siePageURL(sieCanonicalBaseURL, "xyxw", 1, 14, ".htm"); got != sieCanonicalBaseURL+"/xyxw.htm" {
		t.Fatalf("first logical page URL = %q", got)
	}
	if got := siePageURL(sieCanonicalBaseURL, "xyxw", 2, 14, ".htm"); got != sieCanonicalBaseURL+"/xyxw/13.htm" {
		t.Fatalf("middle logical page URL = %q", got)
	}
	if got := siePageURL(sieCanonicalBaseURL, "xyxw", 14, 14, ".htm"); got != sieCanonicalBaseURL+"/xyxw/1.htm" {
		t.Fatalf("last logical page URL = %q", got)
	}
	if got := siePageURL(sieCanonicalBaseURL, "tzgg", 7, 7, ".htm"); got != sieCanonicalBaseURL+"/tzgg/1.htm" {
		t.Fatalf("notice last logical page URL = %q", got)
	}
	for _, tc := range []struct {
		slug        string
		page, total int
	}{
		{"xyxw", 0, 14}, {"xyxw", 15, 14}, {"xyxw", 2, 0}, {"cglx/pxxw", 2, 1}, {"not-a-feed", 2, 14},
	} {
		if got := siePageURL(sieCanonicalBaseURL, tc.slug, tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range/unknown page URL for %#v = %q", tc, got)
		}
	}
}

func TestSieRejectsShellAndEmptyList(t *testing.T) {
	if _, err := parseAdapter_sieList(readSieFixture(t, "sie_404_live.html"), sieCanonicalBaseURL); err == nil {
		t.Fatal("404 shell was accepted as a list")
	}
	if _, err := parseAdapter_sieList(readSieFixture(t, "sie_empty_list_live.html"), sieCanonicalBaseURL); err == nil {
		t.Fatal("empty list shell was accepted as a feed")
	}
	if _, err := parseAdapter_sieList([]byte(`<html><body><div id="app"></div></body></html>`), sieCanonicalBaseURL); err == nil {
		t.Fatal("SPA shell was accepted as a list")
	}
}

func TestSieArticleUsesFixedFieldsAndIgnoresBodyDates(t *testing.T) {
	article, err := parseAdapter_sieArticle(readSieFixture(t, "sie_article_live.html"), sieCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "我校国际学生代表参加2026世界交通运输大会" || article.Type != "学院新闻" {
		t.Fatalf("fixed title/type = %#v", article)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("body date was treated as publication metadata: %#v", article)
	}
	if !strings.Contains(article.Content, "活动时间：2026年6月2日-3日") || strings.Contains(article.Content, "侧栏不应进入正文") {
		t.Fatalf("article body range = %q", article.Content)
	}
	if len(article.Images) != 3 || len(article.Attachments) != 4 {
		t.Fatalf("article resource candidates = images %#v attachments %#v", article.Images, article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, "https://external.example/reference") {
		t.Fatalf("ordinary external link missing from content HTML: %s", article.ContentHTML)
	}
}

func TestSieArticleReadsOnlyExplicitMetadata(t *testing.T) {
	article, err := parseAdapter_sieArticle(readSieFixture(t, "sie_article_meta_live.html"), sieCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Source != "国际教育学院" || article.Date != "2026-06-10" || article.PublishedAt != "" {
		t.Fatalf("strict date/source fields = %#v", article)
	}
}

func TestSieResourceMagicGuards(t *testing.T) {
	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !sieResourceBodyIsUsable("image/png", validPNG, "good.png", true) {
		t.Fatal("valid PNG was rejected")
	}
	if sieResourceBodyIsUsable("image/png", []byte("<html>请输入验证码</html>"), "bad.png", true) {
		t.Fatal("CAPTCHA HTML was accepted as an image")
	}
	if sieResourceBodyIsUsable("image/png", []byte("not a PNG"), "wrong.png", true) {
		t.Fatal("wrong image magic was accepted")
	}
	if !sieResourceBodyIsUsable("application/pdf", []byte("%PDF-1.7"), "good.pdf", false) {
		t.Fatal("valid PDF was rejected")
	}
	if sieResourceBodyIsUsable("text/plain", []byte("%PDF-1.7"), "wrong.pdf", false) {
		t.Fatal("PDF with text/plain type was accepted")
	}
	if !sieResourceBodyIsUsable("application/octet-stream", []byte{'P', 'K', 3, 4}, "good.docx", false) {
		t.Fatal("valid OOXML magic was rejected")
	}
	if sieResourceBodyIsUsable("", []byte("file"), "file.bin", false) {
		t.Fatal("missing Content-Type was accepted")
	}
}

func TestSieFetchArticleVerifiesResourcesAndRedirects(t *testing.T) {
	articleFixture := readSieFixture(t, "sie_article_live.html")
	const baseURL = "https://sie.test"
	oldBase, oldNoCache, oldClient := AllSites["sie"].BaseURL, NoCache, defaultClient
	AllSites["sie"].BaseURL = baseURL
	NoCache = true
	defaultClient = &http.Client{Transport: sieTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/info/1013/9001.htm":
			return sieTestResponse(request, http.StatusOK, "text/html; charset=utf-8", articleFixture), nil
		case "/good.png":
			return sieTestResponse(request, http.StatusOK, "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}), nil
		case "/bad.png":
			return sieTestResponse(request, http.StatusOK, "image/png", []byte("<html>请输入验证码</html>")), nil
		case "/wrong.png":
			return sieTestResponse(request, http.StatusOK, "image/png", []byte("not a PNG")), nil
		case "/good.pdf", "/outside.pdf":
			return sieTestResponse(request, http.StatusOK, "application/pdf", []byte("%PDF-1.7 fixture")), nil
		case "/bad.pdf":
			return sieTestResponse(request, http.StatusOK, "application/pdf", []byte("not a PDF")), nil
		case "/missing.pdf":
			return sieTestResponse(request, http.StatusNotFound, "application/pdf", []byte("missing")), nil
		case "/redirect.pdf":
			response := sieTestResponse(request, http.StatusFound, "text/plain", nil)
			response.Header.Set("Location", "https://external.example/file.pdf")
			return response, nil
		default:
			return sieTestResponse(request, http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
	})}
	ClearCache()
	t.Cleanup(func() {
		AllSites["sie"].BaseURL, NoCache, defaultClient = oldBase, oldNoCache, oldClient
		ClearCache()
	})

	article, raw, err := sieFetchArticle(baseURL + "/info/1013/9001.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || len(article.Images) != 1 || article.Images[0] != baseURL+"/good.png" {
		t.Fatalf("verified images/raw = %d %#v", len(raw), article.Images)
	}
	if len(article.Attachments) != 2 || article.Attachments[0].URL != baseURL+"/good.pdf" || article.Attachments[1].URL != baseURL+"/outside.pdf" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	for _, attachment := range article.Attachments {
		if attachment.Size == "" {
			t.Errorf("verified attachment has no size: %#v", attachment)
		}
	}
	if !strings.Contains(article.ContentHTML, baseURL+"/good.png") || !strings.Contains(article.ContentHTML, baseURL+"/good.pdf") || !strings.Contains(article.ContentHTML, "https://external.example/reference") {
		t.Fatalf("valid resources/external link missing: %s", article.ContentHTML)
	}
	for _, rejected := range []string{"bad.png", "wrong.png", "bad.pdf", "missing.pdf", "请输入验证码", "javascript:"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource survived: %q in %s", rejected, article.ContentHTML)
		}
	}
	if _, _, err := sieFetchResource(baseURL+"/redirect.pdf", baseURL, baseURL+"/info/1013/9001.htm", "redirect.pdf", false); err == nil {
		t.Fatal("cross-origin resource redirect was accepted")
	}
}

type sieTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper sieTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func sieTestResponse(request *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}
