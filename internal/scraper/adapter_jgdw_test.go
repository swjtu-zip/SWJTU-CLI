package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestJgdwAdapterRegistered(t *testing.T) {
	site := AllSites["jgdw"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("jgdw adapter hooks are not registered")
	}
	want := []string{"djyw", "jcdj", "zgzj", "qngz", "index/tzgg", "index/tjxw"}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("jgdw subpages = %#v, want %#v", site.SubPages, want)
	}
	if site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("jgdw pagination hooks are not registered")
	}
	if got := site.PageURL(site.BaseURL, "jcdj", 1, 38, ".htm"); got != "https://jgdw.swjtu.edu.cn/jcdj.htm" {
		t.Fatalf("first jgdw page = %q", got)
	}
	if got := site.PageURL(site.BaseURL, "jcdj", 2, 38, ".htm"); got != "https://jgdw.swjtu.edu.cn/jcdj/37.htm" {
		t.Fatalf("second jgdw page = %q", got)
	}
	if got := site.PageURL(site.BaseURL, "jcdj", 38, 38, ".htm"); got != "https://jgdw.swjtu.edu.cn/jcdj/1.htm" {
		t.Fatalf("last jgdw page = %q", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 38},
		{39, 38},
		{2, 0},
	} {
		if got := site.PageURL(site.BaseURL, "jcdj", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
	if got := site.PageURL("", "jcdj", 1, 38, ".htm"); got != "" {
		t.Fatalf("empty jgdw base returned %q", got)
	}
}

func TestJgdwLiveListBoundaryFixtures(t *testing.T) {
	const baseURL = "https://jgdw.swjtu.edu.cn"
	tests := []struct {
		slug       string
		typ        string
		totalPages int
		pages      []struct {
			fixture string
			page    int
			count   int
			url     string
			date    string
		}
	}{
		{
			slug: "djyw", typ: "党建要闻", totalPages: 7,
			pages: []struct {
				fixture string
				page    int
				count   int
				url     string
				date    string
			}{
				{"jgdw_live_djyw_first.html", 1, 2, "/info/1012/16350.htm", "2026-07-07"},
				{"jgdw_live_djyw_middle.html", 4, 2, "/info/1012/6575.htm", "2022-11-02"},
				{"jgdw_live_djyw_last.html", 7, 3, "/info/1012/1367.htm", "2014-11-02"},
			},
		},
		{
			slug: "jcdj", typ: "基层党建", totalPages: 38,
			pages: []struct {
				fixture string
				page    int
				count   int
				url     string
				date    string
			}{
				{"jgdw_live_jcdj_first.html", 1, 2, "/info/1013/16430.htm", "2026-07-10"},
				{"jgdw_live_jcdj_middle.html", 19, 2, "/info/1013/14040.htm", "2025-05-23"},
				{"jgdw_live_jcdj_last.html", 38, 2, "/info/1013/8630.htm", "2024-01-12"},
			},
		},
		{
			slug: "zgzj", typ: "职工之家", totalPages: 4,
			pages: []struct {
				fixture string
				page    int
				count   int
				url     string
				date    string
			}{
				{"jgdw_live_zgzj_first.html", 1, 2, "/info/1014/16390.htm", "2026-07-10"},
				{"jgdw_live_zgzj_middle.html", 3, 2, "/info/1014/9840.htm", "2024-05-15"},
				{"jgdw_live_zgzj_last.html", 4, 2, "/info/1014/1815.htm", "2020-07-09"},
			},
		},
		{
			slug: "qngz", typ: "青年工作", totalPages: 2,
			pages: []struct {
				fixture string
				page    int
				count   int
				url     string
				date    string
			}{
				{"jgdw_live_qngz_first.html", 1, 2, "/info/1016/15410.htm", "2026-01-13"},
				{"jgdw_live_qngz_last.html", 2, 2, "/info/1016/11250.htm", "2024-08-27"},
			},
		},
		{
			slug: "index/tzgg", typ: "通知公告", totalPages: 13,
			pages: []struct {
				fixture string
				page    int
				count   int
				url     string
				date    string
			}{
				{"jgdw_live_tzgg_first.html", 1, 2, "/info/1018/15950.htm", "2026-04-21"},
				{"jgdw_live_tzgg_middle.html", 8, 2, "/info/1018/1491.htm", "2018-09-27"},
				{"jgdw_live_tzgg_last.html", 13, 3, "/info/1018/1543.htm", "2016-06-06"},
			},
		},
		{
			slug: "index/tjxw", typ: "图集新闻", totalPages: 1,
			pages: []struct {
				fixture string
				page    int
				count   int
				url     string
				date    string
			}{
				{"jgdw_live_tjxw_empty.html", 1, 0, "", ""},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.slug, func(t *testing.T) {
			for _, page := range tc.pages {
				body := readJgdwFixture(t, page.fixture)
				items, err := parseAdapter_jgdwList(body, baseURL)
				if err != nil {
					t.Fatalf("page %d parse failed: %v", page.page, err)
				}
				if len(items) != page.count {
					t.Fatalf("page %d items = %d, want %d", page.page, len(items), page.count)
				}
				if got := parseTotalPages(body); got != tc.totalPages {
					t.Fatalf("page %d total pages = %d, want %d", page.page, got, tc.totalPages)
				}
				if page.count == 0 {
					continue
				}
				if items[0].Type != tc.typ || items[0].URL != baseURL+page.url || items[0].Date != page.date || items[0].PublishedAt != page.date {
					t.Fatalf("page %d first item = %#v", page.page, items[0])
				}
			}
			if got := jgdwPageURL(baseURL, tc.slug, tc.totalPages+1, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("out-of-range page returned %q", got)
			}
		})
	}

	body := readJgdwFixture(t, "jgdw_live_404.html")
	if _, err := parseAdapter_jgdwList(body, baseURL); err == nil {
		t.Fatal("live 404 response was accepted as a list")
	}
}

func TestJgdwLiveArticleUsesFixedMetadataAndBody(t *testing.T) {
	body := readJgdwFixture(t, "jgdw_live_article_notice.html")
	article, err := parseAdapter_jgdwArticle(body, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于开展机关教职工世界读书日主题活动的通知" || article.Type != "通知公告" {
		t.Fatalf("article identity = (%q, %q)", article.Title, article.Type)
	}
	if article.Date != "2026-04-21" || article.PublishedAt != "2026-04-21" {
		t.Fatalf("date-only metadata = (%q, %q)", article.Date, article.PublishedAt)
	}
	if strings.Contains(article.Date, "05-15") || strings.Contains(article.PublishedAt, "05-15") {
		t.Fatalf("body deadline contaminated publication date: (%q, %q)", article.Date, article.PublishedAt)
	}
	if !containsString(article.Content, "2026年4月23日—5月15日") {
		t.Fatalf("body activity date missing: %q", article.Content)
	}
	if !containsString(article.ContentHTML, "https://jgdw.swjtu.edu.cn/__local/valid.png") || !containsString(article.ContentHTML, "https://example.org/guide.pdf") {
		t.Fatalf("content HTML lost resolved body resources: %s", article.ContentHTML)
	}
	if len(article.Images) != 2 || article.Images[0] != "https://jgdw.swjtu.edu.cn/__local/valid.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 4 {
		t.Fatalf("candidate local attachments = %#v", article.Attachments)
	}
}

func TestJgdwFetchArticleVerifiesResourcesAndFiltersCaptcha(t *testing.T) {
	detail := readJgdwFixture(t, "jgdw_live_article_notice.html")
	captcha := readJgdwFixture(t, "jgdw_attachment_captcha_live.html")
	const detailPath = "/info/1018/15950.htm"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != detailPath && r.Header.Get("Referer") == "" {
			t.Errorf("resource request %s did not carry a referer", r.URL.String())
		}
		switch r.URL.Path {
		case detailPath:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(detail)
		case "/__local/valid.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0})
		case "/__local/bad.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(captcha)
		case "/files/valid.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.7\nvalid"))
		case "/files/bad.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("not a PDF"))
		case "/files/wrong.pdf":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("%PDF-1.7 but HTML content"))
		case "/system/_content/download.jsp":
			if r.URL.Query().Get("wbfileid") == "VALID" {
				w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
				_, _ = w.Write([]byte{'P', 'K', 3, 4, 0, 0, 0, 0})
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			_, _ = w.Write(captcha)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldNoCache := NoCache
	NoCache = true
	ClearCache()
	t.Cleanup(func() {
		NoCache = oldNoCache
		ClearCache()
	})

	article, raw, err := jgdwFetchArticleAt(server.URL+detailPath, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || article.Date != "2026-04-21" || article.PublishedAt != "2026-04-21" {
		t.Fatalf("fetched article metadata = (%q, %q), raw=%d", article.Date, article.PublishedAt, len(raw))
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/__local/valid.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !containsString(article.ContentHTML, server.URL+"/__local/valid.png") || !containsString(article.ContentHTML, server.URL+"/files/valid.pdf") || !containsString(article.ContentHTML, "https://example.org/guide.pdf") {
		t.Fatalf("valid resources missing from content HTML: %s", article.ContentHTML)
	}
	for _, invalid := range []string{"bad.png", "bad.pdf", "验证码伪附件.pdf", "captcha", "createimage"} {
		if containsString(article.ContentHTML, invalid) {
			t.Fatalf("invalid resource leaked into content HTML (%q): %s", invalid, article.ContentHTML)
		}
	}
	if _, _, err := jgdwFetchResource(server.URL+"/files/missing.pdf", server.URL, server.URL+detailPath, false); err == nil {
		t.Fatal("HTTP 404 resource was accepted")
	}
	if _, _, err := jgdwFetchResource(server.URL+"/files/wrong.pdf", server.URL, server.URL+detailPath, false); err == nil {
		t.Fatal("HTML Content-Type resource was accepted")
	}
	if jgdwResourceBodyIsUsable("text/html; charset=utf-8", captcha, server.URL+"/files/a.pdf", false) {
		t.Fatal("CAPTCHA HTML was accepted as an attachment")
	}
}

func readJgdwFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestJgdwListParsesTypeDatesAndAbsoluteURLs(t *testing.T) {
	html, err := os.ReadFile("testdata/jgdw_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_jgdwList(html, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].URL != "https://jgdw.swjtu.edu.cn/info/1012/16350.htm" || items[0].Date != "2026-07-07" || items[0].PublishedAt != "2026-07-07" {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[0].Type != "党建要闻" {
		t.Fatalf("first item type = %q", items[0].Type)
	}
	if items[1].Date != "2026-06-05" || items[1].PublishedAt != "2026-06-05 14:03:05" {
		t.Fatalf("precise item dates = (%q, %q)", items[1].Date, items[1].PublishedAt)
	}
}

func TestJgdwHomepageIncludesAllVisibleModules(t *testing.T) {
	html, err := os.ReadFile("testdata/jgdw.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_jgdwList(html, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 20 {
		t.Fatalf("homepage items = %d, want all three news panels and notice module", len(items))
	}
	types := map[string]int{}
	for _, item := range items {
		types[item.Type]++
	}
	for _, typ := range []string{"党建要闻", "基层党建", "职工之家", "通知公告"} {
		if types[typ] == 0 {
			t.Fatalf("homepage has no %q module: %#v", typ, types)
		}
	}
	const noticeURL = "https://jgdw.swjtu.edu.cn/info/1018/15950.htm"
	foundNotice := false
	for _, item := range items {
		if item.URL == noticeURL {
			foundNotice = true
			if item.Title != "关于开展机关教职工世界读书日主题活动的通知" || strings.Contains(item.Title, "04-21") {
				t.Fatalf("nested notice title = %q, want title without date", item.Title)
			}
		}
	}
	if !foundNotice {
		t.Fatalf("homepage omitted nested notice %s", noticeURL)
	}
}

func TestJgdwEmptyListIsAcceptedButStaticAndErrorAreRejected(t *testing.T) {
	empty, err := os.ReadFile("testdata/jgdw_empty.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_jgdwList(empty, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("legal empty list rejected: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty list = %#v, want non-nil empty slice", items)
	}
	for _, name := range []string{"jgdw_static.html", "jgdw_error.html"} {
		body, readErr := os.ReadFile("testdata/" + name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, parseErr := parseAdapter_jgdwList(body, "https://jgdw.swjtu.edu.cn"); parseErr == nil {
			t.Fatalf("%s was accepted as a list", name)
		}
	}
}

func TestJgdwArticleUsesDetailHeadingInsteadOfChannelName(t *testing.T) {
	html, err := os.ReadFile("testdata/jgdw_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_jgdwArticle(html, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}

	want := "机关与直属单位党委2026年党支部委员党务工作专题培训顺利举办"
	if article.Title != want {
		t.Fatalf("article title = %q, want %q", article.Title, want)
	}
	if article.Title == "职工之家" || article.Title == "基层党建" || article.Title == "党建要闻" {
		t.Fatalf("channel name leaked into article title: %q", article.Title)
	}
	if article.Date != "2026-06-05" {
		t.Fatalf("article date = %q, want 2026-06-05", article.Date)
	}
	if article.Type != "党建要闻" {
		t.Fatalf("article type = %q, want 党建要闻", article.Type)
	}
	if article.Content == "" {
		t.Fatal("article content is empty")
	}
}

func TestJgdwArticlePreservesPreciseMetadataAndOutsideAttachments(t *testing.T) {
	html, err := os.ReadFile("testdata/jgdw_article_attachments.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_jgdwArticle(html, "https://jgdw.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "机关与直属单位党委专题培训通知" || article.Type != "通知公告" {
		t.Fatalf("article identity = (%q, %q)", article.Title, article.Type)
	}
	if article.Date != "2026-06-05" || article.PublishedAt != "2026-06-05 14:03:05" {
		t.Fatalf("article dates = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Source != "组织部" {
		t.Fatalf("article source = %q", article.Source)
	}
	if article.Content == "" || article.ContentHTML == "" {
		t.Fatal("article body is empty")
	}
	if !containsString(article.ContentHTML, "https://jgdw.swjtu.edu.cn/images/training.png") {
		t.Fatalf("article HTML did not resolve image URL: %s", article.ContentHTML)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://jgdw.swjtu.edu.cn/images/training.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "培训通知.pdf" || article.Attachments[0].URL != "https://jgdw.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1746847036&wbfileid=ABC123" {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
}

func TestJgdwArticleRejectsEmptyShell(t *testing.T) {
	body, err := os.ReadFile("testdata/jgdw_article_empty.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdapter_jgdwArticle(body, "https://jgdw.swjtu.edu.cn"); err == nil {
		t.Fatal("empty article shell was accepted")
	}
}

func containsString(value, want string) bool {
	return strings.Contains(value, want)
}
