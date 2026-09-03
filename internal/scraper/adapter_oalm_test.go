package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const oalmFixtureBaseURL = "https://oalm.swjtu.edu.cn"

func readOalmFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read oalm fixture %q: %v", name, err)
	}
	return body
}

func TestOalmAdapterRegistrationAndAuditedFeeds(t *testing.T) {
	site := AllSites["oalm"]
	if site == nil || site.BaseURL != oalmCanonicalBaseURL || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil || site.TotalPagesFunc == nil {
		t.Fatalf("oalm adapter hooks are incomplete: %#v", site)
	}
	want := []string{
		"xwgg/xwdt", "xwgg/tzgg", "djgz/djgh",
		"fwzn/sbjjk", "fwzn/fwzyk", "fwzn/sysaqk",
		"gzzd/gjflfg", "gzzd/xnglwj",
		"zlxz/sbjjk", "zlxz/sysaqk", "zlxz/fwzy",
		"djxx", "xxgs", "aqxx",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("oalm feeds = %#v, want %#v", site.SubPages, want)
	}
	for _, excluded := range []string{
		"bmgk/bmjj", "bmgk/jgsz", "bmgk/bmld", "bmgk/gzry",
		"atc/customer/index/index", "system/defaultcontent.jsp",
	} {
		if oalmKnownFeed(excluded) {
			t.Errorf("static/service/external route was treated as an article feed: %q", excluded)
		}
	}
}

func TestOalmReversePagination(t *testing.T) {
	checks := map[string]string{
		"1":  oalmFixtureBaseURL + "/xwgg/tzgg.htm",
		"2":  oalmFixtureBaseURL + "/xwgg/tzgg/60.htm",
		"31": oalmFixtureBaseURL + "/xwgg/tzgg/31.htm",
		"61": oalmFixtureBaseURL + "/xwgg/tzgg/1.htm",
	}
	for page, want := range checks {
		if got := oalmPageURL(oalmFixtureBaseURL, "xwgg/tzgg", atoiForOalmTest(t, page), 61, ".htm"); got != want {
			t.Errorf("logical page %s URL = %q, want %q", page, got, want)
		}
	}
	if got := oalmPageURL("http://oalm.swjtu.edu.cn/", "fwzn/sbjjk", 1, 1, "htm"); got != oalmFixtureBaseURL+"/fwzn/sbjjk.htm" {
		t.Fatalf("single-page URL = %q", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 61}, {62, 61}, {2, 0}, {2, 1},
	} {
		if got := oalmPageURL(oalmFixtureBaseURL, "xwgg/tzgg", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("invalid page %d/%d produced %q", tc.page, tc.total, got)
		}
	}
	if got := oalmPageURL(oalmFixtureBaseURL, "index", 2, 1, ".htm"); got != "" {
		t.Fatalf("homepage out-of-range URL = %q", got)
	}
	if _, _, err := oalmListPage("xwgg/tzgg", 0); err == nil {
		t.Fatal("oalm custom list page accepted page zero")
	}
}

func atoiForOalmTest(t *testing.T, value string) int {
	t.Helper()
	var result int
	for _, digit := range value {
		result = result*10 + int(digit-'0')
	}
	return result
}

func TestOalmHomepageIncludesHiddenCardsAndFiltersExternalLinks(t *testing.T) {
	items, err := parseAdapter_oalmList(readOalmFixture(t, "oalm.html"), oalmFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse oalm homepage: %v", err)
	}
	if len(items) != 26 {
		t.Fatalf("homepage items = %d, want 26 unique local articles", len(items))
	}
	byURL := make(map[string]bool, len(items))
	types := make(map[string]int)
	for _, item := range items {
		if byURL[item.URL] {
			t.Errorf("duplicate homepage article: %#v", item)
		}
		byURL[item.URL] = true
		types[item.Type]++
		if !strings.HasPrefix(item.URL, oalmFixtureBaseURL+"/info/") {
			t.Errorf("homepage leaked external/service URL: %#v", item)
		}
		if item.Title == "" || item.Date == "" || item.Type == "" {
			t.Errorf("homepage item lost metadata: %#v", item)
		}
	}
	for _, want := range []string{
		oalmFixtureBaseURL + "/info/1361/24201.htm",
		oalmFixtureBaseURL + "/info/1371/24791.htm",
		oalmFixtureBaseURL + "/info/1381/24861.htm",
	} {
		if !byURL[want] {
			t.Errorf("hidden homepage card article was not collected: %s", want)
		}
	}
	if types["调剂信息"] == 0 || types["信息公示"] == 0 || types["安全信息"] == 0 {
		t.Fatalf("hidden card types = %#v", types)
	}
}

func TestOalmListParsesBothListTemplatesAndPreciseDates(t *testing.T) {
	items, err := parseAdapter_oalmList(readOalmFixture(t, "oalm_list.html"), oalmFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse oalm news list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("news list items = %d, want two local rows", len(items))
	}
	if items[0].Title != "列表中的完整新闻标题" || items[0].Date != "2026-09-01" || items[0].PublishedAt != "2026-09-01 14:20:35" || items[0].Type != "新闻动态" {
		t.Fatalf("news list first metadata = %#v", items[0])
	}
	if items[1].Date != "2026-08-31" || items[1].PublishedAt != "" {
		t.Fatalf("date-only news row = %#v", items[1])
	}

	notices, err := parseAdapter_oalmList(readOalmFixture(t, "oalm_notice_list.html"), oalmFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse oalm notice list: %v", err)
	}
	if len(notices) != 2 {
		t.Fatalf("notice list items = %d, want two rows", len(notices))
	}
	if notices[0].Title != "关于实验室安全工作的通知" || notices[0].Date != "2026-07-07" || notices[0].Type != "通知公告" {
		t.Fatalf("notice first metadata = %#v", notices[0])
	}
	if notices[1].Title != "关于设备申购的通知" || notices[1].Date != "2026-06-26" {
		t.Fatalf("notice second metadata = %#v", notices[1])
	}
}

func TestOalmArticleExtractsDetailMetadataBodyImagesAndOutsideAttachments(t *testing.T) {
	article, err := parseAdapter_oalmArticle(readOalmFixture(t, "oalm_article.html"), oalmFixtureBaseURL)
	if err != nil {
		t.Fatalf("parse oalm article: %v", err)
	}
	if article.Title != "关于实验室安全工作的完整通知标题" || article.Type != "通知公告" || article.Date != "2026-09-01" || article.PublishedAt != "2026-09-01 14:20:35" {
		t.Fatalf("article metadata = %#v", article)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || !strings.Contains(article.Content, "正文中的历史日期") || strings.Contains(article.Content, "页脚噪声") {
		t.Fatalf("article body scope/content is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != oalmFixtureBaseURL+"/images/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://oalm.swjtu.edu.cn/images/body.png"`) {
		t.Fatalf("article HTML did not resolve image URL: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件1：实验室安全检查表.xlsx" || !strings.Contains(article.Attachments[0].URL, "wbfileid=OALM123") {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
}

func TestOalmAllowsExplicitEmptyListsAndRejectsStaticOrCaptchaShells(t *testing.T) {
	items, err := parseAdapter_oalmList(readOalmFixture(t, "oalm_empty.html"), oalmFixtureBaseURL)
	if err != nil || len(items) != 0 {
		t.Fatalf("empty oalm list = %#v, err=%v; want a legal empty result", items, err)
	}
	for _, name := range []string{"oalm_static.html", "oalm_captcha.html"} {
		if _, err := parseAdapter_oalmList(readOalmFixture(t, name), oalmFixtureBaseURL); err == nil {
			t.Errorf("%s was accepted as an oalm list", name)
		}
		if _, err := parseAdapter_oalmArticle(readOalmFixture(t, name), oalmFixtureBaseURL); err == nil {
			t.Errorf("%s was accepted as an oalm article", name)
		}
	}
	malformed := []byte(`<meta name="pageType" content="2"><div class="xwdt-right-content"><div class="current-title">新闻动态</div><ul class="news-list"><li id="line_u9_1"><a href="/info/1191/24201.htm"></a><span class="time">2026-09-01</span></li></ul></div>`)
	if _, err := parseAdapter_oalmList(malformed, oalmFixtureBaseURL); err == nil {
		t.Fatal("malformed local OALM row was silently discarded")
	}
}
