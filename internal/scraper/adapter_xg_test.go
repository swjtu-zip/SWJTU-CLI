package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

func readXGFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read xg fixture %s: %v", name, err)
	}
	return body
}

func TestXGIndexFeedParsesHomepageSections(t *testing.T) {
	// The served /web homepage renders the 通知公告/扬华新闻/多彩学院/学工之家
	// section lists server-side; 公示专栏 rows are javascript: placeholders and
	// 学生活动 rows are event cards, so neither becomes news.
	baseURL := "https://xg.swjtu.edu.cn/web"
	raw := readXGFixture(t, "xg_home_live.html")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	for _, selector := range []string{".notice-box", ".news-box", ".colorful-college", ".xg-box"} {
		section := doc.Find(selector).First()
		if section.Length() == 0 {
			t.Fatalf("homepage section %s missing", selector)
		}
		typ := strings.TrimSpace(section.Find(".title-name").First().Text())
		section.Find("ul > li").Each(func(_ int, row *goquery.Selection) {
			xgAppendListItem(&items, seen, row.Find("a[href]").First(), row, typ, baseURL)
		})
	}
	if len(items) != 36 {
		t.Fatalf("homepage items = %d, want 36: %#v", len(items), items)
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Type]++
		if item.Date == "" || item.Title == "" {
			t.Fatalf("incomplete item: %#v", item)
		}
		if !xgIsDetailURL(item.URL) {
			t.Fatalf("non-detail URL leaked: %#v", item)
		}
	}
	want := map[string]int{"通知公告": 10, "扬华新闻": 10, "多彩学院": 8, "学工之家": 8}
	for typ, n := range want {
		if counts[typ] != n {
			t.Fatalf("section %q rows = %d, want %d", typ, counts[typ], n)
		}
	}
}

func TestXGIndexFeedFetchesHomepage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/web" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(readXGFixture(t, "xg_home_live.html"))
	}))
	defer server.Close()

	site := AllSites["xg"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL+"/web", server.Client(), true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	})

	items, err := site.ListNewsPage("index", 1)
	if err != nil || len(items) != 36 {
		t.Fatalf("index items = %d, err=%v", len(items), err)
	}
	if total, err := site.GetTotalPages("index"); err != nil || total != 1 {
		t.Fatalf("index total = %d, err=%v", total, err)
	}
	if _, err := site.ListNewsPage("index", 2); err == nil {
		t.Fatal("index page 2 was accepted")
	}
}

func TestXGColourfulCollegeDetailIsAFetchableArticle(t *testing.T) {
	detailURL := "https://xg.swjtu.edu.cn/web/Home/ColourfulCollegeDetail?M3=42df2hgg-f0j0-e32f-bha1-12edhff2jjh5.shtml"
	if !xgIsDetailURL(detailURL) {
		t.Fatalf("ColourfulCollegeDetail rejected: %q", detailURL)
	}

	// The ColourfulCollegeNewsList rows are ColourfulCollegeDetail links; the
	// list feed only yields items once that path is recognised as a detail.
	items, err := xgParseListHTML(readXGFixture(t, "xg_cc_list_live.html"), "https://xg.swjtu.edu.cn/web", "多彩学院")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("colourful college list yielded no items")
	}
	for _, item := range items {
		if !xgIsDetailURL(item.URL) {
			t.Fatalf("non-detail item URL: %#v", item)
		}
	}

	article, err := parseAdapter_xgArticle(readXGFixture(t, "xg_cc_detail_live.html"), "https://xg.swjtu.edu.cn/web")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "抓日常、严经常、聚合力——电气工程学院扎实推进学风建设" || article.Date != "2026-09-18" {
		t.Fatalf("article metadata = %#v", article)
	}
	if strings.TrimSpace(article.Content) == "" {
		t.Fatal("article body is empty")
	}
}

func TestXGJinghuaVideoListParsesOnclickCards(t *testing.T) {
	// 菁华岁月 renders graduation-photo galleries as ul.video-list cards
	// whose rows carry the detail URL in an onclick handler, not in an
	// anchor href.
	items, err := xgParseListHTML(readXGFixture(t, "xg_jh_list_live.html"), "https://xg.swjtu.edu.cn/web", "菁华岁月")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 14 {
		t.Fatalf("video list items = %d, want 14: %#v", len(items), items)
	}
	for _, item := range items {
		if item.Type != "菁华岁月" || item.Title == "" {
			t.Fatalf("incomplete item: %#v", item)
		}
		if !xgIsDetailURL(item.URL) {
			t.Fatalf("non-detail URL leaked: %#v", item)
		}
	}
	if _, ok := xgFeedSpecFor("Home/NewsList?LJw34Jmu=011c_8kL82P8kl=lzm4.shtml"); !ok {
		t.Fatal("菁华岁月 feed is no longer registered")
	}
}

func TestXGFileAndLoginRoutesAreNotFeeds(t *testing.T) {
	// 办事流程/表格下载 are pure getfile tables and 规章制度 now requires a
	// CAS login; none of them can ever archive as articles.
	for _, slug := range []string{
		"File/DownloadList?LJw34Jmu=010c0d.shtml",
		"File/DownloadList?LJw34Jmu=010c0c.shtml",
		"Home/DTNewsList?LJw34Jmu=010c01.shtml",
	} {
		if _, ok := xgFeedSpecFor(slug); ok {
			t.Fatalf("removed xg feed %q is still registered", slug)
		}
		if _, _, err := xgListPage(slug, 1); err == nil {
			t.Fatalf("removed xg feed %q still lists", slug)
		}
	}
}
