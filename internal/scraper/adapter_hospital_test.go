package scraper

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

type hospitalTestRoundTripper func(*http.Request) (*http.Response, error)

func (f hospitalTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHospitalAdapterRegisteredWithAllArticleSurfaces(t *testing.T) {
	site := AllSites["hospital"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("hospital adapter hooks are not registered")
	}
	if site.BaseURL != hospitalCanonicalBaseURL {
		t.Fatalf("base URL = %q, want %q", site.BaseURL, hospitalCanonicalBaseURL)
	}
	if site.PageURL == nil || site.ListPageFunc == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("hospital reverse pagination hooks are not registered")
	}
	if len(site.SubPages) != len(hospitalSubPages) {
		t.Fatalf("sub-page count = %d, want %d", len(site.SubPages), len(hospitalSubPages))
	}
	for _, slug := range []string{
		"ddjs", "yydt", "tzgg", "jkjy", "jkjy/azbjkjyzl",
		"jkjy/jhbjkjyzl", "jkjy/jkshfszl", "jkjy/rccrbfk",
		"ylbx", "ylbx/xsyb", "ylbx/jgyb", "jkgl/jzgjkgl",
		"jkgl/xsjkgl", "hszh", "hszh/xhszhfc", "hszh/hszyd",
	} {
		if !hospitalKnownFeed(slug) {
			t.Errorf("missing audited feed %q", slug)
		}
	}
}

func TestHospitalHomeParsesAllTextualModulesAndFiltersVisualStaticLinks(t *testing.T) {
	raw, err := os.ReadFile("testdata/hospital.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_hospitalList(raw, hospitalCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 25 {
		t.Fatalf("home items = %d, want 25 textual articles: %#v", len(items), items)
	}

	byURL := make(map[string]model.NewsItem, len(items))
	for _, item := range items {
		byURL[item.URL] = item
		if strings.Contains(item.URL, "mp.weixin.qq.com") || strings.Contains(item.URL, "/1020/") || strings.Contains(item.URL, "/1019/") {
			t.Errorf("external/image-only/static link leaked into home articles: %#v", item)
		}
	}
	checks := []struct {
		url      string
		title    string
		date     string
		itemType string
	}{
		{hospitalCanonicalBaseURL + "/info/1006/6618.htm", "西南交通大学开展2026年食品安全突发事件应急演练", "04-24", "医院动态"},
		{hospitalCanonicalBaseURL + "/info/1012/6738.htm", "校医院党支部开展2026年5月主题党日活动", "06-01", "党的建设"},
		{hospitalCanonicalBaseURL + "/info/1009/6588.htm", "关于开展2026年度教职工健康体检工作的通知", "04-16", "通知公告"},
		{hospitalCanonicalBaseURL + "/info/1013/6728.htm", "关于及时报销2025年度大学生医保医疗票据的温馨提示", "06-03", "医疗保险"},
	}
	for _, check := range checks {
		item, ok := byURL[check.url]
		if !ok {
			t.Errorf("home article %s was not found", check.url)
			continue
		}
		if item.Title != check.title || item.Date != check.date || item.Type != check.itemType {
			t.Errorf("home metadata for %s = (%q, %q, %q), want (%q, %q, %q)", check.url, item.Title, item.Date, item.Type, check.title, check.date, check.itemType)
		}
		if hospitalTitleIsTruncated(item.Title) {
			t.Errorf("home title for %s remains truncated: %q", check.url, item.Title)
		}
	}
}

func TestHospitalListParsesPreciseMetadataAndSkipsExternalRows(t *testing.T) {
	raw, err := os.ReadFile("testdata/hospital_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_hospitalList(raw, hospitalCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("list items = %d, want 2 local articles: %#v", len(items), items)
	}
	if items[0].Title != "精确时间的校医院通知" || items[0].Type != "通知公告" || items[0].Date != "2026-09-01" || items[0].PublishedAt != "2026-09-01 14:20:00" {
		t.Fatalf("first item metadata = %#v", items[0])
	}
	if items[1].Title != "学生医保通知" || items[1].Type != "学生医保" || items[1].Date != "2026-08-30" {
		t.Fatalf("second item metadata = %#v", items[1])
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, hospitalCanonicalBaseURL+"/info/") {
			t.Errorf("non-local article URL = %q", item.URL)
		}
	}
	if got := hospitalPageCountFromHTML(raw); got != 17 {
		t.Fatalf("page count = %d, want 17", got)
	}
}

func TestHospitalReversePaginationMatchesDisplayedLogicalPages(t *testing.T) {
	checks := map[int]string{
		1:  hospitalCanonicalBaseURL + "/tzgg.htm",
		2:  hospitalCanonicalBaseURL + "/tzgg/16.htm",
		9:  hospitalCanonicalBaseURL + "/tzgg/9.htm",
		17: hospitalCanonicalBaseURL + "/tzgg/1.htm",
	}
	for page, want := range checks {
		if got := hospitalPageURL(hospitalCanonicalBaseURL, "tzgg", page, 17, ".htm"); got != want {
			t.Errorf("logical page %d URL = %q, want %q", page, got, want)
		}
	}
	for _, page := range []int{0, 18} {
		if got := hospitalPageURL(hospitalCanonicalBaseURL, "tzgg", page, 17, ".htm"); got != "" {
			t.Errorf("out-of-range logical page %d URL = %q, want empty", page, got)
		}
	}
	if got := hospitalPageURL(hospitalCanonicalBaseURL, "jkjy/azbjkjyzl", 4, 4, ".htm"); got != hospitalCanonicalBaseURL+"/jkjy/azbjkjyzl/1.htm" {
		t.Fatalf("nested last-page URL = %q", got)
	}
	if got := hospitalPageCountFromHTML([]byte(`<span class="p_pages"><span class="p_no_d">1</span></span>`)); got != 1 {
		t.Fatalf("single-page count = %d, want 1", got)
	}
}

func TestHospitalArticleExtractsTypePreciseDateBodyImagesAndOutsideAttachment(t *testing.T) {
	raw, err := os.ReadFile("testdata/hospital_article.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_hospitalArticle(raw, hospitalCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "带精确时间的校医院通知" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2026-09-01" || article.PublishedAt != "2026-09-01 14:20:35" {
		t.Fatalf("date fields = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Type != "通知公告" {
		t.Fatalf("type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || strings.Contains(article.Content, "2026年10月16日，不是发布日期") == false {
		t.Fatalf("article content was not retained accurately: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != hospitalCanonicalBaseURL+"/images/notice.png" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "校医院通知附件.pdf" || !strings.Contains(article.Attachments[0].URL, "/system/_content/download.jsp") {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, "src=\""+hospitalCanonicalBaseURL+"/images/notice.png\"") {
		t.Fatalf("image URL was not resolved in ContentHTML: %q", article.ContentHTML)
	}
	if strings.Contains(article.ContentHTML, "正文中的相关文章") == false {
		t.Fatalf("body links were lost from ContentHTML: %q", article.ContentHTML)
	}
}

func TestHospitalArticleUsesOnlySourceDateAndKeepsDateOnlyPrecision(t *testing.T) {
	raw, err := os.ReadFile("testdata/hospital_article_date_only.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_hospitalArticle(raw, hospitalCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "" {
		t.Fatalf("date-only source was normalized incorrectly: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	if strings.Contains(article.Date, "2026-09-16") || strings.Contains(article.PublishedAt, "2026-09-16") {
		t.Fatalf("body event time leaked into publication metadata: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
}

func TestHospitalFetchArticleVerifiesAndSanitizesResources(t *testing.T) {
	raw, err := os.ReadFile("testdata/hospital_article_resources.html")
	if err != nil {
		t.Fatal(err)
	}
	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0}
	validPDF := []byte("%PDF-1.7 hospital fixture")
	serverBase := "https://hospital.test"
	testClient := &http.Client{Transport: hospitalTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		response := func(status int, contentType string, body []byte) *http.Response {
			return &http.Response{
				StatusCode:    status,
				Status:        http.StatusText(status),
				Header:        http.Header{"Content-Type": []string{contentType}},
				Body:          io.NopCloser(bytes.NewReader(body)),
				ContentLength: int64(len(body)),
				Request:       r,
			}
		}
		switch r.URL.Path {
		case "/info/1009/9001.htm":
			return response(http.StatusOK, "text/html; charset=utf-8", raw), nil
		case "/good.png":
			return response(http.StatusOK, "image/png", validPNG), nil
		case "/bad.png", "/captcha.png":
			return response(http.StatusOK, "image/png", []byte("<!doctype html><html><body>请输入验证码</body></html>")), nil
		case "/missing.png", "/missing.pdf":
			return response(http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		case "/good.pdf":
			return response(http.StatusOK, "application/pdf", validPDF), nil
		case "/bad.pdf":
			return response(http.StatusOK, "application/pdf", []byte("not a PDF")), nil
		case "/wrong.pdf":
			return response(http.StatusOK, "text/plain", validPDF), nil
		case "/system/_content/download.jsp":
			return response(http.StatusOK, "text/html; charset=utf-8", []byte("<html><body>验证码下载</body></html>")), nil
		default:
			return response(http.StatusNotFound, "text/html; charset=utf-8", []byte("not found")), nil
		}
	})}

	site := AllSites["hospital"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = serverBase, testClient, true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	})

	article, body, err := hospitalFetchArticle(serverBase + "/info/1009/9001.htm")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("article raw response is empty")
	}
	if article.Date != "2026-09-04" || article.PublishedAt != "" {
		t.Fatalf("article date fields = (%q, %q), want date-only source", article.Date, article.PublishedAt)
	}
	if len(article.Images) != 1 || article.Images[0] != serverBase+"/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "有效附件.pdf" || article.Attachments[0].URL != serverBase+"/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, "src=\""+serverBase+"/good.png\"") || !strings.Contains(article.ContentHTML, "href=\""+serverBase+"/good.pdf\"") {
		t.Fatalf("valid resources were not retained: %q", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, "https://external.example/reference") {
		t.Fatalf("ordinary external link was not retained: %q", article.ContentHTML)
	}
	for _, forbidden := range []string{
		"bad.png", "missing.png", "captcha.png", "bad.pdf", "wrong.pdf", "missing.pdf",
		"CAPTCHA", "javascript:download",
	} {
		if strings.Contains(article.ContentHTML, forbidden) {
			t.Errorf("invalid resource leaked into ContentHTML (%q): %s", forbidden, article.ContentHTML)
		}
	}
}

func TestHospitalKeepsLegalEmptyListAndRejectsStaticCaptchaPages(t *testing.T) {
	empty, err := os.ReadFile("testdata/hospital_empty.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_hospitalList(empty, hospitalCanonicalBaseURL)
	if err != nil || len(items) != 0 {
		t.Fatalf("empty list = %#v, err=%v; want legal empty result", items, err)
	}

	static, err := os.ReadFile("testdata/hospital_static.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdapter_hospitalList(static, hospitalCanonicalBaseURL); err == nil || !strings.Contains(err.Error(), "not an article list") {
		t.Fatalf("static list result err=%v, want rejection", err)
	}
	if _, err := parseAdapter_hospitalArticle(static, hospitalCanonicalBaseURL); err == nil || !strings.Contains(err.Error(), "not an article") {
		t.Fatalf("static article result err=%v, want rejection", err)
	}

	captcha, err := os.ReadFile("testdata/hospital_captcha.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdapter_hospitalList(captcha, hospitalCanonicalBaseURL); err == nil || !strings.Contains(err.Error(), "captcha") {
		t.Fatalf("captcha list result err=%v, want captcha rejection", err)
	}
	if _, err := parseAdapter_hospitalArticle(captcha, hospitalCanonicalBaseURL); err == nil || !strings.Contains(err.Error(), "captcha") {
		t.Fatalf("captcha article result err=%v, want captcha rejection", err)
	}
}
