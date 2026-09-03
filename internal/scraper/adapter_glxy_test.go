package scraper

import (
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
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
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
		"gjhz/hzxm",
		"mkslj/cyxz",
		"xsgz/btkx",
		"xsgz/txzz",
		"yyzj/yyhd",
		"yyzj/yyjz",
		"sztd2/tdjs",
		"sztd2/jzjs",
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
	}
	for _, tc := range cases {
		if got := glxyPageURL(base, tc.slug, tc.page, tc.total, tc.extension); got != tc.want {
			t.Errorf("page URL %s page %d/%d = %q, want %q", tc.slug, tc.page, tc.total, got, tc.want)
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
