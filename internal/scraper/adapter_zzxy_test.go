package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const zzxyTestBaseURL = "http://zzxy.swjtu.edu.cn"

func zzxyFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestZzxyAdapterRegisteredWithAllAuditedListRoutes(t *testing.T) {
	want := []string{
		"xyyw", "tzgg", "szkc", "xwdt", "xsgg", "mtjj", "cgzs",
		"djjgh/djgz/djfc", "djjgh/djgz/tzgg", "djjgh/djgz/dwwj",
		"djjgh/jjgz", "djjgh/ghgz/xxzili", "djjgh/ltgz/gzdt",
		"xsgz/xsdt", "xsgz/tzgg", "xsgz/jyxx",
		"kyyxk/kxyj", "kyyxk/kxyj/xsdt", "kyyxk/kxyj/kytz",
		"kyyxk/kxyj/kycg", "kyyxk/kxyj/kyxm", "zlxz1/mbxz", "zlxz1/wjxz",
	}
	site := AllSites["zzxy"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("zzxy adapter hooks are not fully registered")
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("sub-pages = %#v, want %#v", site.SubPages, want)
	}
	if len(zzxyFeedTypes) != len(want) {
		t.Fatalf("feed type map has %d entries for %d routes", len(zzxyFeedTypes), len(want))
	}
	for _, slug := range want {
		if typ := zzxyFeedTypes[slug]; typ == "" {
			t.Errorf("route %q has no source section type", slug)
		}
	}
	if got := zzxyFeedTypes["xsgg"]; got != "学术公告" {
		t.Fatalf("xsgg type = %q, want source label 学术公告", got)
	}
}

func TestZzxyHomepageFixtureKeepsEveryVisiblePreviewColumn(t *testing.T) {
	body := zzxyFixture(t, "zzxy_live_homepage.html")
	items, err := parseAdapter_zzxyList(body, zzxyTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("got %d homepage items, want 5: %#v", len(items), items)
	}
	wantTypes := []string{"学院要闻", "媒体聚焦", "学术公告", "招聘与培训信息", "成果展示"}
	for i, want := range wantTypes {
		if items[i].Type != want {
			t.Errorf("homepage item %d type = %q, want %q", i, items[i].Type, want)
		}
	}
	if items[1].URL != "https://mp.weixin.qq.com/s/example" {
		t.Fatalf("media URL = %q, want the source external article URL", items[1].URL)
	}
	if items[2].Date != "2026-06-26" || items[2].PublishedAt != "2026-06-26" {
		t.Fatalf("academic preview date = %q/%q", items[2].Date, items[2].PublishedAt)
	}
}

func TestZzxyListFixturesParseStandardAndLegacyShapes(t *testing.T) {
	items, err := parseAdapter_zzxyList(zzxyFixture(t, "zzxy_live_list.html"), zzxyTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("standard list got %d items, want 2: %#v", len(items), items)
	}
	if items[0].Title != "再创佳绩！实现新突破——我院教师荣获全国高校青年教师教学竞赛国家级奖项" || items[0].Date != "2026-09-03" || items[0].Type != "学院要闻" {
		t.Fatalf("unexpected standard first item: %#v", items[0])
	}
	if items[0].URL != zzxyTestBaseURL+"/info/1065/23471.htm" {
		t.Fatalf("standard URL = %q", items[0].URL)
	}

	legacy, err := parseAdapter_zzxyList(zzxyFixture(t, "zzxy_live_conlist.html"), zzxyTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 2 || legacy[0].Date != "2026-04-16" || legacy[0].PublishedAt != "2026-04-16" || legacy[0].Type != "思政课程" {
		t.Fatalf("unexpected conlist result: %#v", legacy)
	}
	if got := zzxyPageCountFromFixture(t, "zzxy_live_conlist.html"); got != 2 {
		t.Fatalf("legacy total pages = %d, want 2", got)
	}
	if got := zzxyPageCountFromFixture(t, "zzxy_live_list.html"); got != 51 {
		t.Fatalf("standard total pages = %d, want 51", got)
	}
}

func zzxyPageCountFromFixture(t *testing.T, name string) int {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(zzxyFixture(t, name))))
	if err != nil {
		t.Fatal(err)
	}
	return zzxyPageCount(doc)
}

func TestZzxyTrueEmptyListSucceedsButStaticShellErrors(t *testing.T) {
	items, err := parseAdapter_zzxyList(zzxyFixture(t, "zzxy_live_empty.html"), zzxyTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty list = %#v, want non-nil empty result", items)
	}
	if items, err := parseAdapter_zzxyList(zzxyFixture(t, "zzxy_live_static.html"), zzxyTestBaseURL); err == nil || items != nil || !strings.Contains(err.Error(), "unrecognised") {
		t.Fatalf("static shell result = %#v, err=%v; want explicit parser error", items, err)
	}
}

func TestZzxyReversePaginationEveryAuditedRoute(t *testing.T) {
	totals := map[string]int{
		"xyyw": 51, "tzgg": 54, "szkc": 2, "xwdt": 8, "xsgg": 13, "mtjj": 8, "cgzs": 2,
		"djjgh/djgz/djfc": 9, "djjgh/djgz/tzgg": 3, "djjgh/djgz/dwwj": 2,
		"djjgh/jjgz": 1, "djjgh/ghgz/xxzili": 1, "djjgh/ltgz/gzdt": 1,
		"xsgz/xsdt": 44, "xsgz/tzgg": 35, "xsgz/jyxx": 2,
		"kyyxk/kxyj": 1, "kyyxk/kxyj/xsdt": 2, "kyyxk/kxyj/kytz": 1,
		"kyyxk/kxyj/kycg": 1, "kyyxk/kxyj/kyxm": 1, "zlxz1/mbxz": 1, "zlxz1/wjxz": 1,
	}
	for _, slug := range zzxySubPages {
		total := totals[slug]
		if total == 0 {
			t.Fatalf("missing audited total for %q", slug)
		}
		for _, page := range []int{1, (total + 1) / 2, total} {
			got := reverseBodaPageURL(zzxyTestBaseURL, slug, page, total, ".htm")
			if got == "" {
				t.Errorf("%s page %d/%d returned empty URL", slug, page, total)
			}
			if page == 1 {
				want := fmt.Sprintf("%s/%s.htm", zzxyTestBaseURL, slug)
				if got != want {
					t.Errorf("%s first URL = %q, want %q", slug, got, want)
				}
			} else {
				physical := total - page + 1
				want := fmt.Sprintf("%s/%s/%d.htm", zzxyTestBaseURL, slug, physical)
				if got != want {
					t.Errorf("%s logical page %d URL = %q, want %q", slug, page, got, want)
				}
			}
		}
		if got := reverseBodaPageURL(zzxyTestBaseURL, slug, total+1, total, ".htm"); got != "" {
			t.Errorf("%s overflow URL = %q, want empty", slug, got)
		}
		if got := reverseBodaPageURL(zzxyTestBaseURL, slug, 0, total, ".htm"); got != "" {
			t.Errorf("%s page zero URL = %q, want empty", slug, got)
		}
	}
}

func TestZzxyArticleFixtureUsesDetailTitleFixedDateAndBodyResources(t *testing.T) {
	article, err := parseAdapter_zzxyArticle(zzxyFixture(t, "zzxy_live_article.html"), zzxyTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "再创佳绩！实现新突破——我院教师荣获全国高校青年教师教学竞赛国家级奖项" {
		t.Fatalf("article title = %q, want detail headline", article.Title)
	}
	if article.Date != "2026-09-03" || article.PublishedAt != "2026-09-03 11:45:00" {
		t.Fatalf("article date = %q/%q", article.Date, article.PublishedAt)
	}
	if article.Type != "学院要闻" {
		t.Fatalf("article type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "8月25日至28日") || !strings.Contains(article.Content, "荣获思想政治课专项组三等奖") {
		t.Fatalf("article body is incomplete: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != zzxyTestBaseURL+"/__local/9/DA/6F/262E48D6912DFE1971C4998D0C9_A211BBE6_74FC9.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="`+zzxyTestBaseURL+`/__local/9/DA/6F/262E48D6912DFE1971C4998D0C9_A211BBE6_74FC9.png"`) {
		t.Fatalf("ContentHTML did not retain normalized image URL: %s", article.ContentHTML)
	}
}

func TestZzxyArticleFixtureExtractsPDFViewerAndOutsideAttachments(t *testing.T) {
	article, err := parseAdapter_zzxyArticle(zzxyFixture(t, "zzxy_live_article_attachments.html"), zzxyTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "通知公告" || article.Date != "2026-09-03" || article.PublishedAt != "2026-09-03 15:02:00" {
		t.Fatalf("article metadata = type %q date %q/%q", article.Type, article.Date, article.PublishedAt)
	}
	if len(article.Images) != 1 || article.Images[0] != zzxyTestBaseURL+"/__local/1/C2/62/6D4569CAE8EB40C9764FA8286B2_FC586FBD_1E6E3.jpg" {
		t.Fatalf("PDF preview image = %#v", article.Images)
	}
	if len(article.Attachments) != 3 {
		t.Fatalf("attachments = %#v, want embedded PDF plus two source links", article.Attachments)
	}
	if article.Attachments[0].Name != "B225DB299E87053ED4358F91681_53EAFE60_1AD1D.pdf" || !strings.HasSuffix(article.Attachments[0].URL, "/__local/B/FD/AE/B225DB299E87053ED4358F91681_53EAFE60_1AD1D.pdf") {
		t.Fatalf("embedded PDF = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "参加答辩学生报名表（样表）.xlsx" || !strings.Contains(article.Attachments[1].URL, "wbfileid=B95E3E555E2964DD5EA766D71387CCD0") {
		t.Fatalf("first outside attachment = %#v", article.Attachments[1])
	}
	if article.Attachments[2].Name != "博士研究生论文发表及科研成果信息登记表.docx" || !strings.Contains(article.Attachments[2].URL, "wbfileid=794AFC08F2B1A7C860D47CF73B3CAFB4") {
		t.Fatalf("second outside attachment = %#v", article.Attachments[2])
	}
}

func TestZzxyDateNeverFallsBackToBodyActivityDate(t *testing.T) {
	html := []byte(`<html><body><div class="right"><div class="title"><h3>通知公告</h3></div></div>
<form name="_newscontent_fromname"><div class="hd"><h3>一则需要固定发布日期的通知</h3></div>
<div class="bd"><div id="vsb_content"><div class="v_news_content"><p>活动日期：2030-12-31，报名截止：2031-01-02。</p></div></div></div></form></body></html>`)
	article, err := parseAdapter_zzxyArticle(html, zzxyTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "" || article.PublishedAt != "" {
		t.Fatalf("body date leaked into metadata: %q/%q", article.Date, article.PublishedAt)
	}
	if date, publishedAt := zzxyDateValues("发布者：日期：2026-09-03 15:02:07 点击数：1"); date != "2026-09-03" || publishedAt != "2026-09-03 15:02:07" {
		t.Fatalf("precise fixed date = %q/%q", date, publishedAt)
	}
}

func TestZzxyResourceVerificationRejectsCaptchaAndChecksMagic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/real.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\nreal"))
		case "/captcha.xlsx":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><title>附件下载</title><h1>请输入验证码下载附件</h1>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	verified := zzxyVerifyAttachments([]model.Attachment{
		{Name: "real.pdf", URL: server.URL + "/real.pdf"},
		{Name: "captcha.xlsx", URL: server.URL + "/captcha.xlsx"},
	}, server.URL)
	if len(verified) != 1 || verified[0].Name != "real.pdf" || verified[0].Size != "13" {
		t.Fatalf("verified attachments = %#v", verified)
	}
	valid := []struct {
		contentType string
		body        []byte
	}{
		{"application/pdf", []byte("%PDF-1.7")},
		{"image/jpeg", []byte{0xff, 0xd8, 0xff, 0xe0}},
		{"application/octet-stream", []byte{0x50, 0x4b, 0x03, 0x04}},
		{"text/plain", []byte("plain text")},
	}
	for _, tc := range valid {
		if !zzxyResourceBodyIsUsable(tc.contentType, tc.body) {
			t.Errorf("valid resource rejected: %q %#v", tc.contentType, tc.body)
		}
	}
	invalid := []struct {
		contentType string
		body        []byte
	}{
		{"text/html", []byte("<html>login</html>")},
		{"application/octet-stream", []byte("<!doctype html><h1>请输入验证码下载附件</h1>")},
		{"application/pdf", []byte("<html>Cloudflare</html>")},
		{"image/png", []byte("not an image")},
		{"application/pdf", nil},
	}
	for _, tc := range invalid {
		if zzxyResourceBodyIsUsable(tc.contentType, tc.body) {
			t.Errorf("invalid resource accepted: %q %#v", tc.contentType, tc.body)
		}
	}
}
