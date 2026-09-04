package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const chemFixtureBaseURL = "https://chem.swjtu.edu.cn"

func TestChemAdapterRegistrationAndPageURL(t *testing.T) {
	site, ok := AllSites["chem"]
	if !ok {
		t.Fatal("chem site is not registered")
	}
	if site.BaseURL != chemCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("chem adapter hooks are incomplete: %+v", site)
	}
	wantSubPages := []string{
		"index/xyxw",
		"index/xytz",
		"index/jwgg",
		"index/jwgg/yjsgg",
		"index/ttxw",
		"index/kxyj",
		"rcpy",
		"rcpy/yjsds",
		"rcpy/yjspygg",
		"rcpy/wjzlxz",
		"kxyj",
		"kxyj/kytz",
		"kxyj/kytd",
		"kxyj/kydt",
		"kxyj/xshd",
		"kxyj/sypt",
		"dqgz",
		"dqgz/djc",
		"dqgz/djdt",
		"dqgz/zzjsydyfz",
		"dqgz/jwgz",
		"dqgz/ghgz",
		"xsgz",
		"xsgz/xgtz",
		"xsgz/xgdt",
		"xsgz/gsgg",
		"aqgz",
		"aqgz/aqxz",
		"aqgz/gzzd",
		"aqgz/xgzy",
		"rczp",
		"xygz",
		"xygz/xyfc",
		"xygz/bysfc",
		"hzjl",
		"hzjl/gjhhz",
		"hzjl/xqhz",
		"zsjy",
		"zsjy/zsgz",
		"zsjy/jygz",
	}
	if !reflect.DeepEqual(site.SubPages, wantSubPages) {
		t.Fatalf("chem sub-pages = %#v, want %#v", site.SubPages, wantSubPages)
	}

	cases := []struct {
		slug  string
		page  int
		total int
		want  string
	}{
		{"index/xyxw", 1, 31, chemFixtureBaseURL + "/index/xyxw.htm"},
		{"index/xyxw", 2, 31, chemFixtureBaseURL + "/index/xyxw/30.htm"},
		{"index/xyxw", 16, 31, chemFixtureBaseURL + "/index/xyxw/16.htm"},
		{"index/xyxw", 30, 31, chemFixtureBaseURL + "/index/xyxw/2.htm"},
		{"index/xyxw", 31, 31, chemFixtureBaseURL + "/index/xyxw/1.htm"},
		{"index/jwgg/yjsgg", 2, 11, chemFixtureBaseURL + "/index/jwgg/yjsgg/10.htm"},
		{"index/jwgg/yjsgg", 11, 11, chemFixtureBaseURL + "/index/jwgg/yjsgg/1.htm"},
		{"rcpy", 2, 2, chemFixtureBaseURL + "/rcpy/1.htm"},
	}
	for _, tc := range cases {
		if got := chemPageURL(chemFixtureBaseURL, tc.slug, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("chemPageURL(%q, page %d/%d) = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestChemMeasuredFeedsUseReversePaginationAndRejectOutOfRange(t *testing.T) {
	measured := map[string]int{
		"index/xyxw": 31, "index/xytz": 7, "index/jwgg": 20, "index/jwgg/yjsgg": 11,
		"index/ttxw": 5, "index/kxyj": 1, "rcpy": 2, "rcpy/yjsds": 1,
		"rcpy/yjspygg": 1, "rcpy/wjzlxz": 2, "kxyj": 13, "kxyj/kytz": 13,
		"kxyj/kytd": 13, "kxyj/kydt": 12, "kxyj/xshd": 13, "kxyj/sypt": 13,
		"dqgz": 15, "dqgz/djc": 15, "dqgz/djdt": 15, "dqgz/zzjsydyfz": 15,
		"dqgz/jwgz": 15, "dqgz/ghgz": 15, "xsgz": 30, "xsgz/xgtz": 9,
		"xsgz/xgdt": 30, "xsgz/gsgg": 30, "aqgz": 1, "aqgz/aqxz": 1,
		"aqgz/gzzd": 1, "aqgz/xgzy": 1, "rczp": 1, "xygz": 1, "xygz/xyfc": 1,
		"xygz/bysfc": 1, "hzjl": 1, "hzjl/gjhhz": 1, "hzjl/xqhz": 1,
		"zsjy": 2, "zsjy/zsgz": 1, "zsjy/jygz": 2,
	}
	if len(measured) != len(chemSubPages) {
		t.Fatalf("measured feed count = %d, registered feed count = %d", len(measured), len(chemSubPages))
	}
	for _, slug := range chemSubPages {
		total, ok := measured[slug]
		if !ok {
			t.Fatalf("registered feed %q has no measured pagination", slug)
		}
		first := chemPageURL(chemFixtureBaseURL, slug, 1, total, ".htm")
		if want := chemFixtureBaseURL + "/" + slug + ".htm"; first != want {
			t.Errorf("%s first URL = %q, want %q", slug, first, want)
		}
		last := chemPageURL(chemFixtureBaseURL, slug, total, total, ".htm")
		if total == 1 {
			want := chemFixtureBaseURL + "/" + slug + ".htm"
			if last != want {
				t.Errorf("%s one-page URL = %q, want %q", slug, last, want)
			}
		} else if !strings.HasSuffix(last, "/1.htm") {
			t.Errorf("%s last URL = %q, want physical /1.htm", slug, last)
		}
		if total > 2 {
			middle := (total + 1) / 2
			wantPhysical := total - middle + 1
			want := chemFixtureBaseURL + "/" + slug + "/" + strconv.Itoa(wantPhysical) + ".htm"
			if got := chemPageURL(chemFixtureBaseURL, slug, middle, total, ".htm"); got != want {
				t.Errorf("%s middle URL = %q, want %q", slug, got, want)
			}
		}
		if got := chemPageURL(chemFixtureBaseURL, slug, total+1, total, ".htm"); got != "" {
			t.Errorf("%s out-of-range URL = %q, want empty", slug, got)
		}
		if got := chemPageURL(chemFixtureBaseURL, slug, 2, 0, ".htm"); got != "" {
			t.Errorf("%s unknown-total page 2 URL = %q, want empty", slug, got)
		}
	}
}

func TestChemPageURLRejectsOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 31},
		{name: "past last", page: 32, totalPages: 31},
		{name: "past one-page feed", page: 2, totalPages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chemPageURL(chemFixtureBaseURL, "index/xyxw", tc.page, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("chemPageURL() = %q for page %d/%d, want empty URL", got, tc.page, tc.totalPages)
			}
		})
	}
}

func TestChemHomeParserScopesRealModulesAndTabs(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_home.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_chemList(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 7 {
		t.Fatalf("got %d chem home articles, want 7: %#v", len(items), items)
	}
	want := []struct {
		url   string
		title string
		date  string
		typ   string
	}{
		{chemFixtureBaseURL + "/info/1086/1001.htm", "真实学院新闻", "2026-07-16", "学院新闻"},
		{chemFixtureBaseURL + "/info/1086/1002.htm", "另一条学院新闻", "2026-07-15", "学院新闻"},
		{chemFixtureBaseURL + "/info/1098/2001.htm", "真实通知公告", "2026-06-06", "通知公告"},
		{chemFixtureBaseURL + "/info/1119/3001.htm", "本科生真实公告", "2026-07-01", "教务公告"},
		{chemFixtureBaseURL + "/info/1120/3002.htm", "研究生真实公告", "2026-06-16", "教务公告"},
		{chemFixtureBaseURL + "/info/1133/4001.htm", "学术报告真实文章", "2026-07-12", "科研动态"},
		{chemFixtureBaseURL + "/info/1132/4002.htm", "科研进展真实文章", "2026-07-15", "科研动态"},
	}
	seen := make(map[string]bool)
	for i, expected := range want {
		item := items[i]
		if item.URL != expected.url || item.Title != expected.title || item.Date != expected.date || item.Type != expected.typ {
			t.Errorf("home item %d = (%q, %q, %q, %q), want (%q, %q, %q, %q)", i, item.URL, item.Title, item.Date, item.Type, expected.url, expected.title, expected.date, expected.typ)
		}
		if seen[item.URL] {
			t.Errorf("duplicate home article URL: %q", item.URL)
		}
		seen[item.URL] = true
	}
	for _, item := range items {
		if strings.Contains(item.Title, "伪文章") || strings.Contains(item.Title, "导航") || strings.Contains(item.Title, "轮播") || strings.Contains(item.Title, "侧栏") || strings.Contains(item.Title, "页脚") {
			t.Errorf("non-article page chrome leaked into home results: %#v", item)
		}
	}
}

func TestChemLiveHomepageFixtureCoversVisibleModulesAndHiddenTabs(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_live_home.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_chemList(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 14 {
		t.Fatalf("got %d unique live home articles, want 14: %#v", len(items), items)
	}
	wantTypes := map[string]int{
		"学院新闻": 4,
		"通知公告": 2,
		"教务公告": 2,
		"科研动态": 2,
		"党建动态": 2,
		"学生工作": 2,
	}
	gotTypes := make(map[string]int)
	for _, item := range items {
		if !strings.HasPrefix(item.URL, chemFixtureBaseURL+"/info/") {
			t.Errorf("non-article URL leaked from live home: %#v", item)
		}
		gotTypes[item.Type]++
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("live home module types = %#v, want %#v", gotTypes, wantTypes)
	}
	for _, item := range items {
		if item.URL == chemFixtureBaseURL+"/info/1098/15639.htm" && item.Date != "2026-09-02" {
			t.Errorf("notice tab date = %q, want 2026-09-02", item.Date)
		}
		if strings.Contains(item.Title, "伪文章") || strings.Contains(item.Title, "页脚") {
			t.Errorf("page chrome leaked from live home: %#v", item)
		}
	}
}

func TestChemLiveListFixturesCoverFirstMiddleLastAndExternalFilter(t *testing.T) {
	cases := []struct {
		fixture string
		count   int
		first   string
		date    string
	}{
		{"chem_live_list_first.html", 3, "诺贝尔化学奖获得者Benjamin List教授受聘西南交通大学名誉教授", "2026-05-18"},
		{"chem_live_list_middle.html", 2, "“科教携手·共筑未来”中冶检测认证四川有限公司总经理刘进波一行来我院交流座谈", "2024-11-26"},
		{"chem_live_list_last.html", 2, "2023年化学学院国家自然科学基金申报的辅导会顺利举行", "2023-01-29"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			html, err := os.ReadFile("testdata/" + tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			items, err := parseAdapter_chemList(html, chemFixtureBaseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.count {
				t.Fatalf("got %d items, want %d: %#v", len(items), tc.count, items)
			}
			if items[0].Title != tc.first || items[0].Date != tc.date || items[0].Type != "学院新闻" {
				t.Fatalf("first item = %#v", items[0])
			}
			for _, item := range items {
				if strings.Contains(item.URL, "news.swjtu.edu.cn") {
					t.Errorf("external last-page row leaked: %#v", item)
				}
			}
		})
	}
}

func TestChemLiveEmptyFeedIsNotAStaticOrShellResponse(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_live_empty_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_chemList(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("live empty feed = %#v, want non-nil empty slice", items)
	}
	static, err := os.ReadFile("testdata/chem_live_static_profile.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdapter_chemList(static, chemFixtureBaseURL); err == nil {
		t.Fatal("static profile fixture was accepted as a chem list")
	}
}

func TestChemListParserExtractsCategoryDateAndFiltersExternal(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_list.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_chemList(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d chem list articles, want 2: %#v", len(items), items)
	}
	if items[0].URL != chemFixtureBaseURL+"/info/1098/2001.htm" || items[0].Title != "关于开展真实通知" || items[0].Date != "2026-06-16" || items[0].Type != "通知公告" {
		t.Errorf("unexpected first list item: %#v", items[0])
	}
	if items[1].URL != chemFixtureBaseURL+"/info/1086/2002.htm" || items[1].Date != "2026-07-15" || items[1].Type != "学院新闻" {
		t.Errorf("unexpected second list item: %#v", items[1])
	}
	for _, item := range items {
		if strings.Contains(item.URL, "example.com") || strings.Contains(item.Title, "伪文章") {
			t.Errorf("external or fake list entry leaked: %#v", item)
		}
	}
}

func TestChemLegacyListAcceptsSourceContentURL(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_legacy_list.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_chemList(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d legacy chem articles, want 1: %#v", len(items), items)
	}
	wantURL := chemFixtureBaseURL + "/content.jsp?urltype=news.NewsContentUrl&wbtreeid=1117&wbnewsid=14809"
	if items[0].URL != wantURL || items[0].Type != "头条新闻" || items[0].Date != "2026-06-08" {
		t.Errorf("unexpected legacy list item: %#v, want URL %q", items[0], wantURL)
	}
}

func TestChemArticleParserExtractsMetadataBodyOutsideImageAndAttachment(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_article_attachment.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_chemArticle(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于真实通知" || article.Date != "2026-06-16" || article.PublishedAt != "2026-06-16 09:27:00" {
		t.Fatalf("unexpected article publication fields: title=%q date=%q published_at=%q", article.Title, article.Date, article.PublishedAt)
	}
	if article.Type != "学院通知" || article.Source != "化学学院" || article.Author != "张三" {
		t.Fatalf("unexpected article metadata: type=%q source=%q author=%q", article.Type, article.Source, article.Author)
	}
	if article.Content != "这是正文内容。" {
		t.Fatalf("article content = %q, want 正文内容", article.Content)
	}
	wantImages := []string{
		chemFixtureBaseURL + "/__local/body.png",
		chemFixtureBaseURL + "/__local/extra.png",
	}
	if !reflect.DeepEqual(article.Images, wantImages) {
		t.Fatalf("article images = %#v, want %#v", article.Images, wantImages)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("got %d attachments, want 1: %#v", len(article.Attachments), article.Attachments)
	}
	wantAttachmentURL := chemFixtureBaseURL + "/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1897809613&wbfileid=ABC123"
	if article.Attachments[0].Name != "测试材料.pdf" || article.Attachments[0].URL != wantAttachmentURL {
		t.Fatalf("unexpected attachment: %#v, want URL %q", article.Attachments[0], wantAttachmentURL)
	}
}

func TestChemLiveArticleFixturesUseFixedMetadataAndFilterPlaceholders(t *testing.T) {
	recentHTML, err := os.ReadFile("testdata/chem_live_article_recent.html")
	if err != nil {
		t.Fatal(err)
	}
	recent, err := parseAdapter_chemArticle(recentHTML, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if recent.Title != "化学学院关于选聘2026级新生班导师的通知" || recent.Date != "2026-09-02" || recent.PublishedAt != "2026-09-02 10:43:00" || recent.Type != "学院通知" {
		t.Fatalf("recent fixed metadata = %#v", recent)
	}
	if !strings.Contains(recent.Content, "2026年9月5日前") || strings.Contains(recent.Date, "2026-09-05") {
		t.Fatalf("body deadline leaked into publication metadata: %#v", recent)
	}
	if len(recent.Images) != 1 || recent.Images[0] != chemFixtureBaseURL+"/__local/recent.png" {
		t.Fatalf("recent images = %#v", recent.Images)
	}
	if !strings.Contains(recent.ContentHTML, chemFixtureBaseURL+"/__local/recent.png") {
		t.Fatalf("complete body HTML lost its image: %q", recent.ContentHTML)
	}

	oldHTML, err := os.ReadFile("testdata/chem_live_article_old.html")
	if err != nil {
		t.Fatal(err)
	}
	old, err := parseAdapter_chemArticle(oldHTML, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if old.Title != "功能高分子团队助力川渝竹产业高质量发展" || old.Date != "2023-09-13" || old.PublishedAt != "2023-09-13 16:04:00" || old.Type != "校企合作" {
		t.Fatalf("old fixed metadata = %#v", old)
	}
	if !strings.Contains(old.Content, "2023年9月6日至8日") || len(old.Images) != 1 || old.Images[0] != chemFixtureBaseURL+"/__local/old.jpg" {
		t.Fatalf("old body/resources = %#v", old)
	}
	if strings.Contains(old.ContentHTML, "tracking.jpg") {
		t.Fatalf("1x1 tracking image leaked into content HTML: %q", old.ContentHTML)
	}
}

func TestChemLiveAttachmentFixtureKeepsSourceAttachmentCandidate(t *testing.T) {
	html, err := os.ReadFile("testdata/chem_live_article_attachment.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_chemArticle(html, chemFixtureBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "化学学院研究生申请学位创新成果要求的规定（2026修订版）" || article.Date != "2026-07-13" || article.PublishedAt != "2026-07-13 09:53:00" || article.Type != "文件资料下载" {
		t.Fatalf("attachment article metadata = %#v", article)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "规定（2026修订版）.pdf" || !strings.Contains(article.Attachments[0].URL, "download.jsp") {
		t.Fatalf("source attachment candidate = %#v", article.Attachments)
	}
}

func TestChemParserRejectsShellAndErrorPages(t *testing.T) {
	if _, err := parseAdapter_chemList([]byte(`<html><body><div id="app"></div></body></html>`), chemFixtureBaseURL); err == nil {
		t.Fatal("SPA shell was accepted as a chem list")
	}
	if _, err := parseAdapter_chemList([]byte(`<main><div class="profile"><h1>学院概况</h1></div></main>`), chemFixtureBaseURL); err == nil {
		t.Fatal("static profile page was accepted as a chem list")
	}
	if _, err := parseAdapter_chemArticle([]byte(`<html><head><title>系统提示</title></head><body>您访问的页面未找到</body></html>`), chemFixtureBaseURL); err == nil {
		t.Fatal("source error page was accepted as a chem article")
	}
}

func TestChemLiveFetchVerifiesHTTPContentTypeMagicAndCaptchaResources(t *testing.T) {
	source, err := os.ReadFile("testdata/chem_article_attachment.html")
	if err != nil {
		t.Fatal(err)
	}
	html := strings.Replace(string(source), "<p>这是正文内容。</p>", "<p>这是正文内容。</p><p><a href=\"/good.pdf\">有效附件.pdf</a></p>", 1)
	html = strings.Replace(html, "<img src=\"/__local/body.png\" alt=\"正文图片\">", "<img src=\"/__local/body.png\" alt=\"正文图片\"><img src=\"/__local/pixel.png\" width=\"1\" height=\"1\">", 1)

	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/info/1000/1.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		case "/__local/body.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})
		case "/__local/pixel.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})
		case "/__local/extra.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<!doctype html><html><body>请输入验证码</body></html>"))
		case "/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\n"))
		case "/system/_content/download.jsp":
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			_, _ = w.Write([]byte("<!doctype html><html><body><h1>附件下载</h1>请输入验证码下载附件</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := AllSites["chem"]
	oldBaseURL, oldNoCache := site.BaseURL, NoCache
	site.BaseURL = server.URL
	NoCache = true
	t.Cleanup(func() {
		site.BaseURL = oldBaseURL
		NoCache = oldNoCache
		ClearCache()
	})
	ClearCache()

	article, body, err := site.GetArticleSnapshot(server.URL + "/info/1000/1.htm")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != html || article.Title != "关于真实通知" || article.Date != "2026-06-16" || article.PublishedAt != "2026-06-16 09:27:00" {
		t.Fatalf("unexpected live article result: article=%+v body=%d", article, len(body))
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/__local/body.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "有效附件.pdf" || article.Attachments[0].URL != server.URL+"/good.pdf" || article.Attachments[0].Size != "9" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if requests["/__local/pixel.png"] != 0 {
		t.Fatalf("1x1 placeholder was fetched: %d requests", requests["/__local/pixel.png"])
	}
	if requests["/__local/extra.png"] == 0 || requests["/system/_content/download.jsp"] == 0 {
		t.Fatalf("invalid resource candidates were not checked: %#v", requests)
	}
	if strings.Contains(article.ContentHTML, "extra.png") || strings.Contains(article.ContentHTML, "pixel.png") || strings.Contains(article.ContentHTML, "download.jsp") {
		t.Fatalf("rejected resources leaked into ContentHTML: %q", article.ContentHTML)
	}
	if !strings.Contains(article.ContentHTML, server.URL+"/__local/body.png") || !strings.Contains(article.ContentHTML, server.URL+"/good.pdf") {
		t.Fatalf("verified resources were not retained in ContentHTML: %q", article.ContentHTML)
	}
}
