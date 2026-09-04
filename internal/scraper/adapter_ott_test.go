package scraper

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

func readOTTFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read OTT fixture %q: %v", name, err)
	}
	return data
}

func TestOTTAdapterRegistersAuditedFeedsAndReversePages(t *testing.T) {
	site := AllSites["ott"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatal("ott adapter is not registered")
	}
	if len(site.SubPages) != 12 {
		t.Fatalf("ott feeds = %d, want 12 (%#v)", len(site.SubPages), site.SubPages)
	}
	wantFeeds := []string{"xwdt1/rdxw", "xwdt1/gzdt", "tzgong/tzgg", "kjhz/hzdt", "kjhz/xqfb", "cgzh/gsgg", "jszyzx/zxdt", "jszyzx/jsjlr", "zcfg/dfzc", "zcfg/gjzc", "zcfg/xxzc", "bslc__/xmsb"}
	for _, feed := range wantFeeds {
		found := false
		for _, got := range site.SubPages {
			if got == feed {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing OTT live feed %q", feed)
		}
	}
	if got := ottPageURL(site.BaseURL, "xwdt1/gzdt", 2, 16, ".htm"); got != site.BaseURL+"/xwdt1/gzdt/15.htm" {
		t.Fatalf("ott reverse page URL = %q", got)
	}
	if got := ottPageURL(site.BaseURL, "xwdt1/gzdt", 16, 16, ".htm"); got != site.BaseURL+"/xwdt1/gzdt/1.htm" {
		t.Fatalf("ott last page URL = %q", got)
	}
	if got := ottPageURL(site.BaseURL, "xwdt1/gzdt", 17, 16, ".htm"); got != "" {
		t.Fatalf("ott out-of-range page URL = %q, want empty", got)
	}
	if got := ottPageURL(site.BaseURL, "xwdt1/gzdt", 2, 0, ".htm"); got != "" {
		t.Fatalf("ott unknown-total page URL = %q, want empty", got)
	}
	if got := ottPageURL(site.BaseURL, "xwdt1/rdxw", 7, 14, ".htm"); got != site.BaseURL+"/xwdt1/rdxw/8.htm" {
		t.Fatalf("ott middle page URL = %q", got)
	}
}

func TestOTTListFixturesUseRealRowsAndReversePagination(t *testing.T) {
	first := readOTTFixture(t, "ott_list_first.html")
	if got := parseTotalPages(first); got != 14 {
		t.Fatalf("ott first-page total = %d, want 14", got)
	}
	items, err := parseAdapter_ottList(first, ottCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("ott first-page local rows = %#v", items)
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, ottCanonicalBaseURL+"/info/") || item.Type != "热点新闻" || item.Date == "" {
			t.Fatalf("ott first-page item leaked invalid metadata: %#v", item)
		}
	}

	middle, err := parseAdapter_ottList(readOTTFixture(t, "ott_list_middle.html"), ottCanonicalBaseURL)
	if err != nil || len(middle) != 1 {
		t.Fatalf("ott middle-page rows = %#v, err=%v", middle, err)
	}
	last, err := parseAdapter_ottList(readOTTFixture(t, "ott_list_last.html"), ottCanonicalBaseURL)
	if err != nil || len(last) != 1 || last[0].Date != "2021-04-12" {
		t.Fatalf("ott last-page rows = %#v, err=%v", last, err)
	}
	if _, err := parseAdapter_ottList(readOTTFixture(t, "ott_list_oob.html"), ottCanonicalBaseURL); err == nil {
		t.Fatal("OTT 404 page was accepted as an empty article list")
	}
}

func TestOTTListParsesScopedMetadata(t *testing.T) {
	items := parseOTTListRows([]byte(`<html><div class="list-left-tt">工作动态</div><ul class="lb-list">
<li><a href="../info/1064/3432.htm"><div class="lb-list-date">25<br><span>2025.04</span></div><div class="lb-list-right"><div class="lb-list-tt">完整的技术转移文章标题</div></div></a></li>
<li><a href="https://news.swjtu.edu.cn/info/1011/1.htm"><div class="lb-list-tt">外部文章</div></a></li>
</ul></html>`), "http://ott.swjtu.edu.cn")
	if len(items) != 1 {
		t.Fatalf("ott items = %#v", items)
	}
	if items[0].Title != "完整的技术转移文章标题" || items[0].Date != "2025-04-25" || items[0].PublishedAt != "2025-04-25" || items[0].Type != "工作动态" {
		t.Fatalf("ott metadata = %#v", items[0])
	}
}

func TestOTTArticleAddsSectionType(t *testing.T) {
	article, err := parseAdapter_ottArticle([]byte(`<html><title>技术转移文章-西南交通大学技术转移研究院</title><meta name="pageType" content="3"><h1 class="article-tt">技术转移文章</h1><div class="list-banner-tt">工作动态</div><div id="vsb_content"><div class="v_news_content"><p>正文内容足够长且准确。</p><img data-src="/images/body.jpg"><a download="通知附件.pdf" href="/download?id=1">下载附件</a></div></div><div class="article-sm">发布日期：2025-04-25</div></html>`), "http://ott.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "工作动态" || !strings.Contains(article.Content, "正文内容") || article.Date != "2025-04-25" {
		t.Fatalf("ott article = %#v", article)
	}
	if len(article.Images) != 1 || article.Images[0] != "http://ott.swjtu.edu.cn/images/body.jpg" || len(article.Attachments) != 1 || article.Attachments[0].Name != "通知附件.pdf" {
		t.Fatalf("ott article resources = images=%#v attachments=%#v", article.Images, article.Attachments)
	}
}

func TestOTTArticleFixtureUsesFixedMetadataBodyAndOutsideAttachments(t *testing.T) {
	article, err := parseAdapter_ottArticle(readOTTFixture(t, "ott_article_live.html"), ottCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于转发技术成果需求征集工作的通知" {
		t.Fatalf("ott title = %q", article.Title)
	}
	if article.Type != "通知公告" || article.Source != "技转院" {
		t.Fatalf("ott type/source = %q/%q", article.Type, article.Source)
	}
	if article.Date != "2026-07-28" || article.PublishedAt != "2026-07-28" {
		t.Fatalf("ott publication = %q/%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || !strings.Contains(article.Content, "截止时间") {
		t.Fatalf("ott body was not confined to .v_news_content: %q", article.Content)
	}
	if len(article.Images) != 2 || article.Images[0] != ottCanonicalBaseURL+"/__local/valid.png" {
		t.Fatalf("ott images = %#v", article.Images)
	}
	if len(article.Attachments) != 4 {
		t.Fatalf("ott attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.Attachments[2].Name, "外置附件") || strings.Contains(strings.Join([]string{article.Attachments[0].URL, article.Attachments[1].URL, article.Attachments[2].URL, article.Attachments[3].URL}, "\n"), "other.example") {
		t.Fatalf("ott outside/external attachment handling = %#v", article.Attachments)
	}
}

func TestOTTResourceVerificationChecksStatusContentTypeAndMagic(t *testing.T) {
	validPNG := []byte("\x89PNG\r\n\x1a\nvalid")
	validPDF := []byte("%PDF-1.7\nvalid")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(validPNG)
		case "/bad-status.png":
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(validPNG)
		case "/bad-type.png":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>captcha</html>"))
		case "/bad-magic.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("not a png"))
		case "/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(validPDF)
		case "/bad.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("not a pdf"))
		case "/system/_content/download.jsp":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("<html>请输入验证码下载附件</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	referer := server.URL + "/info/1063/1.htm"

	images := ottVerifyImages([]string{server.URL + "/good.png", server.URL + "/bad-status.png", server.URL + "/bad-type.png", server.URL + "/bad-magic.png"}, referer)
	if len(images) != 1 || images[0] != server.URL+"/good.png" {
		t.Fatalf("verified OTT images = %#v", images)
	}
	attachments := ottVerifyAttachments([]model.Attachment{
		{Name: "有效.pdf", URL: server.URL + "/good.pdf"},
		{Name: "伪 PDF.pdf", URL: server.URL + "/bad.pdf"},
		{Name: "验证码.pdf", URL: server.URL + "/system/_content/download.jsp?wbfileid=CAPTCHA"},
	}, referer)
	if len(attachments) != 1 || attachments[0].Name != "有效.pdf" || attachments[0].Size != strconv.Itoa(len(validPDF)) {
		t.Fatalf("verified OTT attachments = %#v", attachments)
	}
	if ottResourceBodyIsUsable("image/png", validPNG, "good.png", true) == false || ottResourceBodyIsUsable("image/png", []byte("bad"), "bad.png", true) {
		t.Fatal("OTT image Content-Type/magic checks are not strict")
	}
}

type ottTestRoundTripper func(*http.Request) (*http.Response, error)

func (f ottTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func ottTestResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func TestOTTFetchArticleFiltersInvalidResourcesAndSanitizesHTML(t *testing.T) {
	oldClient := defaultClient
	oldNoCache := NoCache
	NoCache = true
	defer func() {
		defaultClient = oldClient
		NoCache = oldNoCache
	}()
	defaultClient = &http.Client{Transport: ottTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/info/1063/4612.htm":
			return ottTestResponse(req, http.StatusOK, "text/html; charset=utf-8", readOTTFixture(t, "ott_article_live.html")), nil
		case "/__local/valid.png":
			return ottTestResponse(req, http.StatusOK, "image/png", []byte("\x89PNG\r\n\x1a\nvalid")), nil
		case "/__local/bad.png":
			return ottTestResponse(req, http.StatusOK, "image/png", []byte("not a png")), nil
		case "/files/in-body.pdf":
			return ottTestResponse(req, http.StatusOK, "application/pdf", []byte("%PDF-1.7\nvalid")), nil
		case "/files/bad.pdf":
			return ottTestResponse(req, http.StatusOK, "application/pdf", []byte("not a pdf")), nil
		case "/system/_content/download.jsp":
			if req.URL.Query().Get("wbfileid") == "OUTSIDE" {
				return ottTestResponse(req, http.StatusOK, "application/pdf", []byte("%PDF-1.7\nvalid")), nil
			}
			return ottTestResponse(req, http.StatusOK, "application/octet-stream", []byte("<html>请输入验证码下载附件</html>")), nil
		default:
			return ottTestResponse(req, http.StatusNotFound, "text/html", []byte("404")), nil
		}
	})}
	article, raw, err := ottFetchArticle(ottCanonicalBaseURL + "/info/1063/4612.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || len(article.Images) != 1 || article.Images[0] != ottCanonicalBaseURL+"/__local/valid.png" {
		t.Fatalf("ott fetched resources = raw=%d images=%#v", len(raw), article.Images)
	}
	if len(article.Attachments) != 2 || !strings.Contains(article.Attachments[0].Name, "正文附件") || !strings.Contains(article.Attachments[1].Name, "外置附件") {
		t.Fatalf("ott fetched attachments = %#v", article.Attachments)
	}
	if strings.Contains(article.ContentHTML, "bad.png") || strings.Contains(article.ContentHTML, "CAPTCHA") || strings.Contains(article.ContentHTML, "bad.pdf") {
		t.Fatalf("invalid OTT resources leaked into content HTML: %s", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, "valid.png") || !strings.Contains(article.ContentHTML, "in-body.pdf") {
		t.Fatalf("valid OTT resources were removed from content HTML: %s", article.ContentHTML)
	}
}
