package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestClxyAdapterRegistration(t *testing.T) {
	site, ok := AllSites["clxy"]
	if !ok {
		t.Fatal("clxy site is not registered")
	}
	if site.BaseURL != clxyCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ArticleFetchFunc == nil {
		t.Fatalf("clxy adapter hooks are incomplete: %+v", site)
	}
	want := []string{
		"sylm_xwgg/xyxw",
		"sylm_xwgg/xygg",
		"jwgg",
		"jwgg/bks",
		"jwgg/yjs",
		"sylm_djgz",
		"sylm_djgz/tzgg",
		"sylm_djgz/djxw",
		"sylm_xsgz",
		"sylm_xsgz/gsgg",
		"sylm_xsgz/txkx",
		"sylm_xsgz/jyzp",
		"sylm_xshd",
		"sylm_xshd/xshb",
		"sylm_xshd/xsxw",
		"sylm_xshd/kjdt",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("clxy sub-pages = %#v, want %#v", site.SubPages, want)
	}
}

func TestClxyPageURLUsesReverseGeneratedNumbering(t *testing.T) {
	const baseURL = "https://clxy.swjtu.edu.cn"
	cases := []struct {
		route string
		page  int
		total int
		want  string
	}{
		{"sylm_xwgg/xyxw", 1, 112, baseURL + "/sylm_xwgg/xyxw.htm"},
		{"sylm_xwgg/xyxw", 2, 112, baseURL + "/sylm_xwgg/xyxw/111.htm"},
		{"sylm_xwgg/xyxw", 56, 112, baseURL + "/sylm_xwgg/xyxw/57.htm"},
		{"sylm_xwgg/xyxw", 111, 112, baseURL + "/sylm_xwgg/xyxw/2.htm"},
		{"sylm_xwgg/xyxw", 112, 112, baseURL + "/sylm_xwgg/xyxw/1.htm"},
		{"sylm_xshd/xshb", 2, 5, baseURL + "/sylm_xshd/xshb/4.htm"},
		{"sylm_xshd/xshb", 5, 5, baseURL + "/sylm_xshd/xshb/1.htm"},
	}
	for _, tc := range cases {
		if got := clxyPageURL(baseURL, tc.route, tc.page, tc.total, ".htm"); got != tc.want {
			t.Errorf("%s page %d/%d URL = %q, want %q", tc.route, tc.page, tc.total, got, tc.want)
		}
	}
}

func TestClxyPageURLRejectsOutOfRange(t *testing.T) {
	const baseURL = "https://clxy.swjtu.edu.cn"
	for _, tc := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 112},
		{name: "past last", page: 113, totalPages: 112},
		{name: "past one-page feed", page: 2, totalPages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clxyPageURL(baseURL, "sylm_xwgg/xyxw", tc.page, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("clxyPageURL() = %q for page %d/%d, want empty URL", got, tc.page, tc.totalPages)
			}
		})
	}
}

func TestClxyListParserExtractsRowsDatesAndTypes(t *testing.T) {
	html, err := os.ReadFile("testdata/clxy_list.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_clxyList(html, "https://clxy.swjtu.edu.cn")
	if err == nil || !strings.Contains(err.Error(), "outside the site") {
		t.Fatalf("external row was accepted as a successful complete feed: items=%#v err=%v", items, err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d clxy list items, want 3: %#v", len(items), items)
	}
	want := []struct {
		title string
		url   string
		date  string
		typ   string
	}{
		{"列表文章一", "https://clxy.swjtu.edu.cn/info/1145/31306.htm", "2026-06-06", "学院新闻"},
		{"列表文章二", "https://clxy.swjtu.edu.cn/info/1145/31286.htm", "2026-06-02", "学院新闻"},
		{"列表文章三", "https://clxy.swjtu.edu.cn/info/1145/31186.htm", "2026-05-29", "学院新闻"},
	}
	for i, expected := range want {
		got := items[i]
		if got.Title != expected.title || got.URL != expected.url || got.Date != expected.date || got.Type != expected.typ {
			t.Errorf("item %d = (%q, %q, %q, %q), want (%q, %q, %q, %q)", i, got.Title, got.URL, got.Date, got.Type, expected.title, expected.url, expected.date, expected.typ)
		}
	}
	for _, item := range items {
		if strings.Contains(item.Title, "导航") || strings.Contains(item.Title, "轮播") || strings.Contains(item.URL, "jiuye.swjtu.edu.cn") {
			t.Errorf("non-article link leaked into clxy list: %#v", item)
		}
	}
}

func TestClxyHomeParserKeepsVisibleAndHiddenTabs(t *testing.T) {
	html, err := os.ReadFile("testdata/clxy_home_tabs.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseAdapter_clxyList(html, "https://clxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		title string
		date  string
		typ   string
	}{
		{"学院新闻文章", "2026-06-06", "学院新闻"},
		{"本科教务文章", "2026-06-03", "本科生"},
		{"研究生教务文章", "2026-06-02", "研究生"},
		{"学术海报文章", "", "学术海报"},
		{"学术新闻文章", "2026-05-20", "学术新闻"},
		{"科技动态文章", "2018-05-14", "科技动态"},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d home items, want %d: %#v", len(items), len(want), items)
	}
	for i, expected := range want {
		if items[i].Title != expected.title || items[i].Date != expected.date || items[i].Type != expected.typ {
			t.Errorf("home item %d = (%q, %q, %q), want (%q, %q, %q)", i, items[i].Title, items[i].Date, items[i].Type, expected.title, expected.date, expected.typ)
		}
	}
}

func TestClxyHomePrefersSpecificListTypeForRepeatedArticle(t *testing.T) {
	html := []byte(`<div id="pc"><div class="content">
<div class="item-long"><h3 class="title">材料要闻<a class="more">更多</a></h3><ul class="news-img"><li><a href="info/1145/31306.htm" class="pic-link"><img src="/__local/feature.png"></a><a href="info/1145/31306.htm" class="text-link">重复文章</a></li></ul></div>
<div class="item-s"><h3 class="title">学院新闻<a class="more">更多</a></h3><ul><li class="list-item"><a href="info/1145/31306.htm">重复文章</a><span class="date">2026-06-06</span></li></ul></div>
</div></div>`)
	items, err := parseAdapter_clxyList(html, "https://clxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != "学院新闻" || items[0].Date != "2026-06-06" {
		t.Fatalf("repeated item = %#v", items)
	}
}

func TestClxyArticleParserExtractsMetadataImagesAndExternalAttachment(t *testing.T) {
	html, err := os.ReadFile("testdata/clxy_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseAdapter_clxyArticle(html, "https://clxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "材料学院附件测试文章" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Source != "材料学院" || article.Date != "2026-05-28" || article.PublishedAt != "2026-05-28 15:50:00" {
		t.Fatalf("metadata = source=%q date=%q published_at=%q", article.Source, article.Date, article.PublishedAt)
	}
	if article.Type != "学院公告" {
		t.Fatalf("type = %q, want 学院公告", article.Type)
	}
	if article.Content != "正文第一段。\n\n正文第二段。" {
		t.Fatalf("content = %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://clxy.swjtu.edu.cn/__local/body.png" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	attachment := article.Attachments[0]
	wantURL := "https://clxy.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1451357099&wbfileid=TESTFILE"
	if attachment.Name != "测试附件.pdf" || attachment.URL != wantURL {
		t.Fatalf("attachment = %#v, want name/url %q/%q", attachment, "测试附件.pdf", wantURL)
	}
}

func TestClxyArticleParserIgnoresBodyNavigationAndMailLinks(t *testing.T) {
	html := []byte(`<html><body><div class="content"><div class="article">
<h1>材料学院链接分类测试</h1>
<div class="source">来源：材料学院&nbsp;&nbsp;日期：2026年08月28日 10:20</div>
<div class="v_news_content"><p>正文。</p>
<p><a href="mailto:example@example.com">联系邮箱</a></p>
<p><a href="http://jwc.swjtu.edu.cn/vatuu/WebAction?setAction=download&amp;fileType=rule&amp;fileId=RULE1">相关规章</a></p>
</div>
<div class="attachments"><a href="/system/_content/download.jsp?id=FILE1">通知附件</a></div>
</div></div></body></html>`)
	article, err := parseAdapter_clxyArticle(html, "https://clxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != "https://clxy.swjtu.edu.cn/system/_content/download.jsp?id=FILE1" {
		t.Fatalf("body navigation links were classified as attachments: %#v", article.Attachments)
	}
}

func TestClxyParserRejectsErrorAndNonArticleShells(t *testing.T) {
	if _, err := parseAdapter_clxyList([]byte(`<html><head><title>系统提示</title></head><body>您没有访问当前栏目的权限。</body></html>`), "https://clxy.swjtu.edu.cn"); err == nil {
		t.Fatal("error page was accepted as a list")
	}
	if _, err := parseAdapter_clxyArticle([]byte(`<html><head><title>西南交通大学材料科学与工程学院</title></head><body><div class="nav">首页</div></body></html>`), "https://clxy.swjtu.edu.cn"); err == nil {
		t.Fatal("home shell was accepted as an article")
	}
}

func TestClxyLiveHomeAndListFixtures(t *testing.T) {
	home, err := os.ReadFile("testdata/clxy_home_live.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_clxyList(home, clxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("live homepage produced no local articles")
	}
	for _, typ := range []string{"学院新闻", "学院公告", "本科生", "研究生", "通知公告", "党建新闻", "公示公告", "团学快讯", "学术海报", "学术新闻", "科技动态"} {
		found := false
		for _, item := range items {
			if item.Type == typ {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("live homepage did not expose local tab type %q", typ)
		}
	}

	for _, name := range []string{"first", "middle", "last"} {
		list, err := os.ReadFile("testdata/clxy_list_xyxw_" + name + "_live.html")
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parseAdapter_clxyList(list, clxyCanonicalBaseURL)
		if err != nil || len(parsed) == 0 {
			t.Fatalf("live xyxw %s page: items=%d err=%v", name, len(parsed), err)
		}
		if got := parseTotalPages(list); got != 112 {
			t.Fatalf("live xyxw %s total pages = %d, want 112", name, got)
		}
	}
	oob, err := os.ReadFile("testdata/clxy_list_404_live.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdapter_clxyList(oob, clxyCanonicalBaseURL); err == nil {
		t.Fatal("live out-of-range response was accepted as an empty list")
	}
}

func TestClxyLiveExternalOnlyListIsNotSuccessful(t *testing.T) {
	html, err := os.ReadFile("testdata/clxy_list_jyzp_external_live.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseAdapter_clxyList(html, clxyCanonicalBaseURL)
	if err == nil || !strings.Contains(err.Error(), "outside the site") {
		t.Fatalf("external-only employment list was accepted: items=%#v err=%v", items, err)
	}
	if len(items) != 0 {
		t.Fatalf("external-only employment list leaked local items: %#v", items)
	}
}

func TestClxyLiveArticlesUseFixedMetadataAndFilterPseudoImages(t *testing.T) {
	news, err := os.ReadFile("testdata/clxy_article_recent_news_live.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseAdapter_clxyArticle(news, clxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "材料学院在校企联合科研攻关方面取得重大进展" || article.Date != "2026-06-06" || article.PublishedAt != "2026-06-06 10:07:00" || article.Type != "学院新闻" {
		t.Fatalf("recent article metadata = %#v", article)
	}
	if article.Author != "材料科学与工程学院" {
		t.Fatalf("recent article author = %q", article.Author)
	}
	if len(article.Images) != 1 || !strings.HasSuffix(article.Images[0], ".png") || strings.Contains(article.ContentHTML, "blob:") {
		t.Fatalf("recent article resources = images=%#v html has blob=%v", article.Images, strings.Contains(article.ContentHTML, "blob:"))
	}

	notice, err := os.ReadFile("testdata/clxy_article_recent_notice_live.html")
	if err != nil {
		t.Fatal(err)
	}
	noticeArticle, err := parseAdapter_clxyArticle(notice, clxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if noticeArticle.Date != "2026-05-28" || noticeArticle.PublishedAt != "2026-05-28 15:50:00" || noticeArticle.Type != "学院公告" {
		t.Fatalf("notice metadata used a body date or wrong type: %#v", noticeArticle)
	}
	if len(noticeArticle.Attachments) != 1 || !strings.Contains(noticeArticle.Attachments[0].Name, "博士") {
		t.Fatalf("notice attachment was not classified from the fixed content area: %#v", noticeArticle.Attachments)
	}

	academic, err := os.ReadFile("testdata/clxy_article_academic_live.html")
	if err != nil {
		t.Fatal(err)
	}
	academicArticle, err := parseAdapter_clxyArticle(academic, clxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if academicArticle.Date != "2026-05-20" || academicArticle.PublishedAt != "2026-05-20 18:57:00" || academicArticle.Type != "学术新闻" || len(academicArticle.Images) != 4 {
		t.Fatalf("academic article metadata/resources = %#v", academicArticle)
	}

	poster, err := os.ReadFile("testdata/clxy_article_poster_live.html")
	if err != nil {
		t.Fatal(err)
	}
	posterArticle, err := parseAdapter_clxyArticle(poster, clxyCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if posterArticle.Date != "2021-04-15" || posterArticle.PublishedAt != "2021-04-15 21:48:00" || posterArticle.Type != "学术海报" {
		t.Fatalf("poster metadata = %#v", posterArticle)
	}
	if len(posterArticle.Images) != 0 || strings.Contains(posterArticle.ContentHTML, "blob:") {
		t.Fatalf("pseudo image was retained: images=%#v content_html=%q", posterArticle.Images, posterArticle.ContentHTML)
	}
}

func TestClxyResourceValidationRejectsCaptchaAndChecksMagic(t *testing.T) {
	captcha, err := os.ReadFile("testdata/clxy_attachment_captcha_live.html")
	if err != nil {
		t.Fatal(err)
	}
	if clxyResourceBodyIsUsable("text/html;charset=UTF-8", captcha, "名单.pdf", false) {
		t.Fatal("captcha HTML was accepted as a PDF attachment")
	}
	if clxyResourceBodyIsUsable("image/png", []byte("\x89PNG\r\n\x1a\n"), "poster.png", true) == false {
		t.Fatal("valid PNG magic was rejected")
	}
	if clxyResourceBodyIsUsable("image/png", []byte("<html>captcha</html>"), "poster.png", true) {
		t.Fatal("HTML body with an image content type was accepted")
	}
	if clxyResourceBodyIsUsable("application/pdf", []byte("%PDF-1.7"), "名单.pdf", false) == false {
		t.Fatal("valid PDF magic was rejected")
	}

	permission, err := os.ReadFile("testdata/clxy_permission_live.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdapter_clxyList(permission, clxyCanonicalBaseURL); err == nil {
		t.Fatal("live permission response was accepted as a list")
	}
}
