package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func loadGlxyFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestGlxyAdapterRegistrationAndPageURL(t *testing.T) {
	site, ok := AllSites["glxy"]
	if !ok {
		t.Fatal("glxy site is not registered")
	}
	if site.BaseURL != glxyBaseURL {
		t.Fatalf("base URL = %q, want %q", site.BaseURL, glxyBaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("glxy adapter hooks are incomplete: %+v", site)
	}
	if len(site.SubPages) != len(glxySubPages) {
		t.Fatalf("sub-pages = %#v, want %#v", site.SubPages, glxySubPages)
	}
	for i, want := range glxySubPages {
		if site.SubPages[i] != want {
			t.Fatalf("sub-page %d = %q, want %q", i, site.SubPages[i], want)
		}
	}
	for _, want := range []string{
		"gjhz/gjjl",
		"mkslj/cyxz",
		"xsgz/btkx",
		"xsgz/txzz",
		"yyzj/yyhd",
		"yyzj/yyjz",
		"sztd2/tdjs",
	} {
		found := false
		for _, slug := range site.SubPages {
			if slug == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("confirmed live list route %q is not registered", want)
		}
	}
	for _, slug := range site.SubPages {
		if slug == "gjhz/hzxm" {
			t.Fatal("external-only gjhz/hzxm page must not be registered as an article list")
		}
	}

	const base = "https://glxy.swjtu.edu.cn/"
	cases := []struct {
		slug, extension, want string
		page, total           int
	}{
		{"xwtz/xyxw", ".htm", base + "xwtz/xyxw.htm", 1, 118},
		{"xwtz/xyxw", ".htm", base + "xwtz/xyxw/117.htm", 2, 118},
		{"xwtz/xyxw", ".htm", base + "xwtz/xyxw/60.htm", 59, 118},
		{"xwtz/xyxw", ".htm", base + "xwtz/xyxw/1.htm", 118, 118},
		{"zthd", ".htm", base + "zthd.htm", 1, 1},
		{"xwtz/xyxw", "html", base + "xwtz/xyxw/117.html", 2, 118},
		{"xwtz/xyxw", ".htm", "", 2, 0},
		{"xwtz/xyxw", ".htm", "", 119, 118},
		{"xwtz/xyxw", ".htm", "", 0, 118},
		{"xwtz/xyxw", ".htm", "", 1, -1},
		{"xwtz/xyxw", ".htm", base + "xwtz/xyxw.htm", 1, 0},
	}
	for _, tc := range cases {
		if got := glxyPageURL(base, tc.slug, tc.page, tc.total, tc.extension); got != tc.want {
			t.Errorf("page URL %s page %d/%d = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
		}
	}
	if got := glxyPageURL("", "xwtz/xyxw", 1, 118, ".htm"); got != "" {
		t.Fatalf("empty base URL = %q, want empty", got)
	}
	for _, slug := range glxySubPages {
		if slug == "sztd2/jzjs" {
			t.Fatal("static兼职教授 page must not be registered as an article list")
		}
	}
}

func TestGlxyHomepageScopesEveryRealModule(t *testing.T) {
	items, err := parseGlxyNews(loadGlxyFixture(t, "glxy.html"), glxyBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 61 {
		t.Fatalf("got %d homepage articles, want 61 unique real entries", len(items))
	}

	byURL := make(map[string]struct {
		title, typ, date string
	}, len(items))
	for _, item := range items {
		byURL[item.URL] = struct {
			title, typ, date string
		}{item.Title, item.Type, item.Date}
		if strings.Contains(item.URL, "rcpy/") || strings.Contains(item.URL, "zthd.htm") || strings.Contains(item.URL, "thesis.swjtu.edu.cn") {
			t.Fatalf("static/landing/external link leaked into feed: %#v", item)
		}
	}
	want := map[string]struct {
		title, typ, date string
	}{
		glxyBaseURL + "/info/1338/59025.htm": {"经济管理学院举行第三期专业学位研究生企业导师聘任仪式暨校企合作研讨会", "学院新闻", "2026-06-08"},
		glxyBaseURL + "/info/1331/57265.htm": {"关于开展2026年上半年教师资格认定工作的通知", "党政办公", "2026-04-14"},
		glxyBaseURL + "/info/1075/59095.htm": {"重庆邮电大学人工智能学院夏书银教授学术讲座", "学术活动", ""},
		glxyBaseURL + "/info/1460/57785.htm": {"我院胡书老师在Production and Operations Management期刊发表学术论文", "科研成果", ""},
		glxyBaseURL + "/info/1333/58955.htm": {"经济管理学院关于2027年博士、硕士生指导教师岗位任职资格遴选与招生资格...", "研究生", "2026-06-04"},
		glxyBaseURL + "/info/1336/59105.htm": {"关于2026-2027学年第一学期本科教材选用的公示", "公示公告", "2026-06-10"},
	}
	for url, expected := range want {
		got, ok := byURL[url]
		if !ok {
			t.Fatalf("homepage entry %s was not parsed", url)
		}
		if got != expected {
			t.Errorf("homepage entry %s = %#v, want %#v", url, got, expected)
		}
	}
	for _, typ := range []string{"党政办公", "本科生", "研究生", "学生工作", "科研工作", "公示公告", "其它"} {
		found := false
		for _, item := range items {
			if item.Type == typ {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("homepage notice tab %q produced no item", typ)
		}
	}
}

func TestGlxyListParserKeepsTypesAndMinutePrecision(t *testing.T) {
	html := loadGlxyFixture(t, "glxy_adapter_list.html")
	items, err := parseGlxyNews(html, glxyBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list entries, want 2: %#v", len(items), items)
	}
	if items[0].Title != "列表中的真实学院新闻" || items[0].URL != glxyBaseURL+"/info/1338/60195.htm" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "" || items[0].Type != "综合" {
		t.Fatalf("unexpected first list entry: %#v", items[0])
	}
	if items[1].Date != "2026-08-30" || items[1].PublishedAt != "2026-08-30 14:34:00" {
		t.Fatalf("minute precision was not retained: %#v", items[1])
	}
	if got := glxyTotalPagesFromHTML(html); got != 38 {
		t.Fatalf("total pages = %d, want 38", got)
	}

	research := []byte(`<html><head><meta name="pageTitle" content="科研成果"></head><body><ul class="dj_lb1"><li><a href="../info/1460/57785.htm">真实科研成果标题</a><span class="sj">2026年04月30日</span></li></ul></body></html>`)
	researchItems, err := parseGlxyNews(research, glxyBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(researchItems) != 1 || researchItems[0].Type != "科研成果" || researchItems[0].Date != "2026-04-30" || researchItems[0].PublishedAt != "" {
		t.Fatalf("unexpected research list entry: %#v", researchItems)
	}
}

func TestGlxyDistinguishesLegalEmptyListsAndShells(t *testing.T) {
	empty := []byte(`<html><head><meta name="pageType" content="2"><meta name="pageTitle" content="其它"></head><body><ul class="xw-ul"></ul><div class="p_pages">首页 上一页 下一页 尾页</div></body></html>`)
	items, err := parseGlxyNews(empty, glxyBaseURL)
	if err != nil {
		t.Fatalf("legal empty list returned error: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("legal empty list = %#v, want non-nil empty slice", items)
	}
	if _, err := parseGlxyNews([]byte(`<html><body><div id="app"></div></body></html>`), glxyBaseURL); err == nil {
		t.Fatal("homepage shell was accepted as a list")
	}
	externalOnly := []byte(`<html><head><meta name="pageTitle" content="合作项目"></head><body><ul class="dj_lb1"><li><a href="https://fad.swjtu.edu.cn/info/1024/6675.htm">外站转发文章</a><span class="sj">2021年12月01日</span></li></ul></body></html>`)
	if _, err := parseGlxyNews(externalOnly, glxyBaseURL); err == nil {
		t.Fatal("external-only list was silently accepted as an empty local list")
	}
}

func TestGlxyArticleExtractsTypeDateBodyImageAndAttachments(t *testing.T) {
	article, err := parseGlxyArticle(loadGlxyFixture(t, "glxy_adapter_article.html"), glxyBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "关于测试工作的通知" || article.Type != "本科生" || article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:00" {
		t.Fatalf("unexpected article metadata: %+v", article)
	}
	if article.Source != "学院办公室" || article.Author != "测试作者" {
		t.Fatalf("unexpected article credits: source=%q author=%q", article.Source, article.Author)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || !strings.Contains(article.Content, "这是正文第二段") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != glxyBaseURL+"/__local/body.jpg" {
		t.Fatalf("unexpected article images: %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("got %d attachments, want body and outside attachments: %#v", len(article.Attachments), article.Attachments)
	}
	if article.Attachments[0].Name != "正文附件.pdf" || article.Attachments[0].URL != glxyBaseURL+"/files/body.pdf" {
		t.Fatalf("unexpected body attachment: %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "附件：通知表.xlsx" || !strings.Contains(article.Attachments[1].URL, "wbfileid=GLXYFIXTURE") {
		t.Fatalf("unexpected outside attachment: %#v", article.Attachments[1])
	}
	if !strings.Contains(article.ContentHTML, `src="`+glxyBaseURL+`/__local/body.jpg"`) || !strings.Contains(article.ContentHTML, `href="`+glxyBaseURL+`/files/body.pdf"`) {
		t.Fatalf("resolved resources missing from ContentHTML: %s", article.ContentHTML)
	}
}

func TestGlxyArticleRejectsStaticCaptchaAndErrorPages(t *testing.T) {
	static := []byte(`<html><head><meta name="pageType" content="2"></head><body><form name="_newscontent_fromname"><h1>静态简介</h1><div class="v_news_content"><p>静态页正文</p></div></form></body></html>`)
	if _, err := parseGlxyArticle(static, glxyBaseURL); err == nil {
		t.Fatal("static page was accepted as an article")
	}
	captcha := []byte(`<html><head><title>安全验证</title></head><body>请输入验证码</body></html>`)
	if _, err := parseGlxyArticle(captcha, glxyBaseURL); err == nil {
		t.Fatal("captcha page was accepted as an article")
	}
	if _, err := parseGlxyArticle([]byte(`<html><head><title>404 页面不存在</title></head><body>404</body></html>`), glxyBaseURL); err == nil {
		t.Fatal("error page was accepted as an article")
	}
}

func TestGlxyExternalOnlyHzxmLiveListStaysRejected(t *testing.T) {
	// The live gjhz/hzxm page lists 12 rows, every one of them a 转发 notice
	// pointing at fad.swjtu.edu.cn. The feed is no longer registered; this
	// canary must keep rejecting such external-only lists if they are ever
	// parsed again, instead of silently archiving nothing.
	items, err := parseGlxyNews(loadGlxyFixture(t, "glxy_live_hzxm.html"), glxyBaseURL)
	if err == nil {
		t.Fatalf("external-only hzxm list was silently accepted: %#v", items)
	}
	if !strings.Contains(err.Error(), "no supported local article entries") {
		t.Fatalf("hzxm rejection reason changed: %v", err)
	}
}

func TestGlxyIndexFeedTotalPagesAcceptsAuditedHomepage(t *testing.T) {
	homePage := loadGlxyFixture(t, "glxy_live_home.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.htm" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(homePage)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	site := AllSites["glxy"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	site.BaseURL, NoCache = server.URL, true
	t.Cleanup(func() {
		site.BaseURL, NoCache = oldBase, oldNoCache
		ClearCache()
	})

	total, err := site.GetTotalPages("index")
	if err != nil || total != 1 {
		t.Fatalf("glxy index total pages = %d, %v; want 1", total, err)
	}
	items, err := site.ListNewsRange("index", "2000-01-01", "2099-12-31", 0)
	if err != nil {
		t.Fatalf("glxy index range feed failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("glxy index range feed returned no homepage articles")
	}

	shellServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>glxy</title></head><body><div id="app"></div></body></html>`))
	}))
	defer shellServer.Close()
	site.BaseURL = shellServer.URL
	if _, err := site.GetTotalPages("index"); err == nil {
		t.Fatal("glxy index total pages accepted a homepage without an article digest")
	}
}

func TestGlxyArticleUsesFixedDateAndSafeContentResources(t *testing.T) {
	articleHTML := []byte(`<!doctype html>
<html><head><meta name="pageType" content="3"></head><body>
<div class="nav-b"><a href="/index.htm">首页</a><a href="/xwtz.htm">新闻通知</a><a href="/zthd.htm">专题活动</a><a href="#">正文</a></div>
<form name="_newscontent_fromname">
  <div class="newxq-boxy"><h1>旧模板的固定标题</h1><p><span>2026年01月20日 14:40</span><a href="/zthd.htm">返回列表</a></p></div>
  <div class="v_news_content">
    <p>正文中的活动日期：2099年12月31日，不能当发布日期。</p>
    <p><img src="data:image/png;base64,placeholder"><img src="/spacer.gif"><img src="javascript:void(0)"><img data-src="/__local/real.png" src="data:image/gif;base64,lazy-placeholder"></p>
    <p><a href="javascript:download()">恶意附件.pdf</a><a href="data:application/pdf;base64,placeholder" download="恶意.pdf">恶意下载</a><a href="/files/real.pdf">正式附件.pdf</a></p>
  </div>
</form></body></html>`)

	article, err := parseGlxyArticle(articleHTML, glxyBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "旧模板的固定标题" || article.Type != "专题活动" {
		t.Fatalf("unexpected title/type: title=%q type=%q", article.Title, article.Type)
	}
	if article.Date != "2026-01-20" || article.PublishedAt != "2026-01-20 14:40:00" {
		t.Fatalf("fixed publication date was not retained: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	if !strings.Contains(article.Content, "2099年12月31日") {
		t.Fatalf("正文内容 was unexpectedly discarded: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != glxyBaseURL+"/__local/real.png" {
		t.Fatalf("unsafe or placeholder images leaked: %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "正式附件.pdf" || article.Attachments[0].URL != glxyBaseURL+"/files/real.pdf" {
		t.Fatalf("unsafe attachments leaked: %#v", article.Attachments)
	}
	for _, forbidden := range []string{"data:", "javascript:", "spacer.gif", "lazy-placeholder"} {
		if strings.Contains(strings.ToLower(article.ContentHTML), strings.ToLower(forbidden)) {
			t.Fatalf("forbidden content resource %q leaked into HTML: %s", forbidden, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, `src="`+glxyBaseURL+`/__local/real.png"`) || !strings.Contains(article.ContentHTML, `href="`+glxyBaseURL+`/files/real.pdf"`) {
		t.Fatalf("safe content resources were not resolved: %s", article.ContentHTML)
	}
}
