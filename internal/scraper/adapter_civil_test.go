package scraper

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

func TestCivilAdapterRegistered(t *testing.T) {
	site := AllSites["civil"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.TotalPagesFunc == nil {
		t.Fatal("civil adapter hooks are not registered")
	}
	want := []string{
		"tzgg", "tmyw", "bkjx", "scientific", "xsgz", "zsjy", "djgz", "yjspy", "tmxy",
		"bkjx/bkjxgg", "bkjx/bkzyjs", "bkjx/bkpyfa", "bkjx/bkjxgl", "bkjx/glygzzd", "bkjx/wjbgxz",
		"scientific/kytzgg", "scientific/kydtxx", "scientific/kycgzs", "scientific/kyfczs", "scientific/xkjsxg", "scientific/ABEgjqk", "scientific/ITIgjqk", "scientific/glygzzd", "scientific/wjybgxz",
		"xsgz/xsgztz", "xsgz/xsgzdt", "xsgz/fdygzs", "xsgz/bjjskx", "xsgz/xzfczs", "xsgz/xhsc", "xsgz/wxycwz", "xsgz/xstd", "xsgz/xshdhb", "xsgz/xyglzd", "xsgz/wjybgxz",
		"zsjy/bkzsxg", "zsjy/yjszsxg", "zsjy/xyzpxx", "zsjy/sxszpxx", "zsjy/jyzdypx", "zsjy/gjzkzs", "zsjy/xyglzd", "zsjy/wjybgxz",
		"djgz/llyjxx", "djgz/zzjsfz", "djgz/djhdxx", "djgz/zbgzxx", "djgz/bmgzjy", "djgz/dyzxzs", "djgz/ghzj", "djgz/xyglzd", "djgz/wjybgxz",
		"yjspy/yjsgg", "yjspy/yjszyjs", "yjspy/yjspyfa", "yjspy/yjskcb", "yjspy/bsspy", "yjspy/bshldz", "yjspy/glygzzd", "yjspy/wjyxzlb",
		"tmxy/xyjz", "tmxy/xyh", "tmxy/xyglzd", "tmxy/wjybgxz",
		"zsjy/jxjypxzx_j", "zgzx/zsgxjyzx",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("civil sub-pages = %#v, want %#v", site.SubPages, want)
	}
}

func TestCivilAdapterListSnapshot(t *testing.T) {
	html, err := os.ReadFile("testdata/civil.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseCivilNews(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	legacyItems, err := parseBodaNewsList(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	legacyDates := 0
	for _, item := range legacyItems {
		if item.Date != "" {
			legacyDates++
		}
	}

	// The snapshot has six featured items that are not banner links and five
	// six-item category panels. Two featured entries are the site's known hero
	// announcements and are intentionally filtered by isValidNewsTitle.
	if got, want := len(items), 36; got != want {
		t.Fatalf("parsed %d items, want %d", got, want)
	}
	t.Logf("legacy parser: %d items/%d dates; civil adapter: %d items", len(legacyItems), legacyDates, len(items))

	dateRe := regexp.MustCompile(`^20\d{2}-\d{2}-\d{2}$`)
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if !dateRe.MatchString(item.Date) {
			t.Errorf("%q has non-normalized date %q", item.Title, item.Date)
		}
		if seen[item.URL] {
			t.Errorf("duplicate URL %q", item.URL)
		}
		seen[item.URL] = true
		if item.URL == "http://civil.swjtu.edu.cn/info/1152/70532.htm" || item.URL == "http://civil.swjtu.edu.cn/info/1161/71392.htm" {
			t.Errorf("banner URL leaked into list: %q", item.URL)
		}
	}

	want := map[string]struct {
		title string
		date  string
	}{
		"http://civil.swjtu.edu.cn/info/1152/72042.htm": {"土木工程学院举办2026届毕业生党员大会", "2026-05-29"},
		"http://civil.swjtu.edu.cn/info/1048/72392.htm": {"2027年土木工程学院研究生导师资格遴选与上岗聘任办法公示", "2026-06-12"},
		"http://civil.swjtu.edu.cn/info/1057/71882.htm": {"土木工程学院2026-2028年（本科）教育教学研究与改革项目申报公示", "2026-05-25"},
	}
	got := make(map[string]model.NewsItem, len(items))
	for _, item := range items {
		got[item.URL] = item
	}
	for url, expected := range want {
		item, ok := got[url]
		if !ok {
			t.Fatalf("missing expected item %q", url)
		}
		if item.Title != expected.title || item.Date != expected.date {
			t.Errorf("item %q = (%q, %q), want (%q, %q)", url, item.Title, item.Date, expected.title, expected.date)
		}
	}
	for url, wantType := range map[string]string{
		"http://civil.swjtu.edu.cn/info/1048/72392.htm": "通知公告",
		"http://civil.swjtu.edu.cn/info/1057/71882.htm": "教育教学",
		"http://civil.swjtu.edu.cn/info/1152/72042.htm": "土木要闻",
	} {
		if got := got[url].Type; got != wantType {
			t.Errorf("item %q type = %q, want %q", url, got, wantType)
		}
	}
}

func TestCivilAdapterCategoryCardDateAndNavigationScope(t *testing.T) {
	html, err := os.ReadFile("testdata/civil_list_live.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseCivilNews(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("parsed %d items, want 2", len(items))
	}
	if items[0].Title != "关于开展真实通知" || items[0].Date != "2026-08-01" {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[1].Title != "真实科研消息" || items[1].Date != "2026-07-27" {
		t.Fatalf("second item = %#v", items[1])
	}
	for _, item := range items {
		if item.Title == "导航伪文章链接" {
			t.Fatalf("navigation link leaked into items: %#v", item)
		}
	}
}

func TestCivilAdapterArticleMeta(t *testing.T) {
	html := []byte(`<html><body>
<main>
  <h3>土木工程学院举办2026届毕业生党员大会</h3>
  <div class="top-info">2026-05-29 11:50:46 来源：土木工程学院 分类： 阅读量：</div>
  <div class="v_news_content"><p>正文内容。</p></div>
</main>
</body></html>`)
	article, err := parseCivilArticle(html, "https://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "土木工程学院举办2026届毕业生党员大会" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2026-05-29" {
		t.Fatalf("date = %q, want 2026-05-29", article.Date)
	}
	if article.PublishedAt != "2026-05-29 11:50:46" {
		t.Fatalf("published_at = %q, want source timestamp", article.PublishedAt)
	}
	if article.Source != "土木工程学院" || article.Type != "" {
		t.Fatalf("metadata = source %q/type %q", article.Source, article.Type)
	}
	if article.Content != "正文内容。" {
		t.Fatalf("content = %q", article.Content)
	}
	if !strings.Contains(article.ContentHTML, "正文内容。") {
		t.Fatalf("content_html lost body: %q", article.ContentHTML)
	}
}

func TestCivilAdapterLiveDetailShape(t *testing.T) {
	html, err := os.ReadFile("testdata/civil_article_live.html")
	if err != nil {
		t.Fatal(err)
	}
	article, err := parseCivilArticle(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "土木工程学院2026年暑期实习安排" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2026-06-26" {
		t.Fatalf("date = %q, want 2026-06-26", article.Date)
	}
	if article.PublishedAt != "2026-06-26 10:03:47" {
		t.Fatalf("published_at = %q, want source seconds preserved", article.PublishedAt)
	}
	if article.Source != "" || article.Type != "" || article.Author != "" {
		t.Fatalf("empty civil metadata was not kept empty: source=%q type=%q author=%q", article.Source, article.Type, article.Author)
	}
	if article.Content != "各位老师、同学：\n\n2026年土木工程学院认识实习与生产实习的具体安排详见附件。" {
		t.Fatalf("content = %q", article.Content)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("attachments = %#v, want one CMS attachment", article.Attachments)
	}
	if article.Attachments[0].Name != "土木工程学院认识实习安排表.xlsx" || article.Attachments[0].URL != "http://civil.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1796209034&wbfileid=A875E791E31D455E18F3B471CC9C07BC" {
		t.Fatalf("attachment = %#v", article.Attachments[0])
	}
	if len(article.Images) != 1 || article.Images[0] != "http://civil.swjtu.edu.cn/__local/ABC/image.jpg" {
		t.Fatalf("images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="http://civil.swjtu.edu.cn/__local/ABC/image.jpg"`) {
		t.Fatalf("content_html did not retain resolved image: %q", article.ContentHTML)
	}
}

func TestCivilEmptyListAndReversePagination(t *testing.T) {
	emptyHTML, err := os.ReadFile("testdata/civil_empty_list.html")
	if err != nil {
		t.Fatal(err)
	}
	emptyItems, err := parseCivilNews(emptyHTML, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("reachable zero-item list should parse: %v", err)
	}
	if len(emptyItems) != 0 {
		t.Fatalf("empty list returned stale feature card: %#v", emptyItems)
	}
	if pages, err := civilTotalPagesFromHTML(emptyHTML); err != nil || pages != 1 {
		t.Fatalf("empty list page count = %d/%v, want 1 page", pages, err)
	}

	reverseHTML, err := os.ReadFile("testdata/civil_reverse_list.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseCivilNews(reverseHTML, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Type != "通知公告" || items[0].Date != "2026-08-04" || items[1].Title != "习近平给四所交通大学全体师生回信" || items[1].Date != "2026-04-07" {
		t.Fatalf("reverse list item = %#v", items)
	}
	if pages, err := civilTotalPagesFromHTML(reverseHTML); err != nil || pages != 3 {
		t.Fatalf("reverse list page count = %d/%v, want logical 3", pages, err)
	}
	if got := civilPageURL("http://civil.swjtu.edu.cn", "tzgg", 2, 3, ".htm"); got != "http://civil.swjtu.edu.cn/tzgg/2.htm" {
		t.Fatalf("logical page 2 URL = %q", got)
	}
}

func TestCivilListResourceAndLegacyURL(t *testing.T) {
	resourceHTML, err := os.ReadFile("testdata/civil_resource_list.html")
	if err != nil {
		t.Fatal(err)
	}
	resources, err := parseCivilNews(resourceHTML, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].URL != "http://civil.swjtu.edu.cn/__local/A/BC/fixture.docx?e=.docx" || resources[0].Type != "文件表格下载" || resources[0].PublishedAt != "2026-08-03 09:05:00" {
		t.Fatalf("resource list item = %#v", resources)
	}

	legacyHTML, err := os.ReadFile("testdata/civil_legacy_list.html")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := parseCivilNews(legacyHTML, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 1 || legacy[0].URL != "http://civil.ix2.com/vatuu/WebAction?setAction=newsDetail&viewType=web&newsId=1BB06039E0E20E4B" {
		t.Fatalf("legacy list item = %#v", legacy)
	}
}

func TestCivilCleansZeroWidthTitleAndRejectsVerificationShell(t *testing.T) {
	html := `<html><head><title>土木工程学院</title></head><body>
<h3 class="news-title">\u200b  带精确时间的土木通知</h3>
<div class="top-info">2026-08-01 09:05 来源： 分类：</div>
<div class="v_news_content"><p>正文段落。</p></div>
</body></html>`
	html = strings.Replace(html, `\u200b`, string('\u200b'), 1)
	article, err := parseCivilArticle([]byte(html), "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "带精确时间的土木通知" || article.Date != "2026-08-01" || article.PublishedAt != "2026-08-01 09:05:00" {
		t.Fatalf("zero-width article fields = %#v", article)
	}

	verification := []byte(`<html><head><title>WAF 安全验证</title></head><body>请求已被阻断，请完成验证码。</body></html>`)
	if _, err := parseCivilNews(verification, "http://civil.swjtu.edu.cn"); err == nil {
		t.Fatal("verification page was accepted as a list")
	}
}

func TestCivilPageURL(t *testing.T) {
	base := "http://civil.swjtu.edu.cn"
	if got := civilPageURL(base, "tzgg", 1, 197, ".htm"); got != base+"/tzgg.htm" {
		t.Errorf("page 1 URL = %q", got)
	}
	if got := civilPageURL(base, "tzgg", 2, 197, ".htm"); got != base+"/tzgg/196.htm" {
		t.Errorf("page 2 URL = %q", got)
	}
	if got := civilPageURL(base, "tzgg", 197, 197, ".htm"); got != base+"/tzgg/1.htm" {
		t.Errorf("last page URL = %q", got)
	}
	for _, tc := range []struct {
		name       string
		page       int
		totalPages int
	}{
		{name: "zero", page: 0, totalPages: 197},
		{name: "past last", page: 198, totalPages: 197},
		{name: "unknown reverse total", page: 2, totalPages: 0},
	} {
		t.Run("out of range/"+tc.name, func(t *testing.T) {
			if got := civilPageURL(base, "tzgg", tc.page, tc.totalPages, ".htm"); got != "" {
				t.Fatalf("civilPageURL() = %q for page %d/%d, want empty URL", got, tc.page, tc.totalPages)
			}
		})
	}
}

func TestCivilListParserRejectsUnrecognisedPage(t *testing.T) {
	shell := []byte(`<html><head><title>土木工程学院</title></head><body><nav><a href="/info/1000/1.htm">导航文章</a></nav><main>静态页面</main></body></html>`)
	if _, err := parseCivilNews(shell, "https://civil.swjtu.edu.cn"); err == nil {
		t.Fatal("unrecognised Civil page was accepted as an empty list")
	}
}
