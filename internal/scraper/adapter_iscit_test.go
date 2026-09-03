package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestIscitAdapterRegistered(t *testing.T) {
	site := AllSites["iscit"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("iscit adapter hooks are not registered")
	}
	want := []string{
		"xw/zxxw", "xw/xsdt", "dqgz/dqxw", "xsgz/txxw",
		"tz/yw", "tz/dq", "tz/jx", "tz/xg", "tz/sz", "tz/ky",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("iscit feeds = %#v, want %#v", site.SubPages, want)
	}
	if got := iscitPageURL("https://iscit.swjtu.edu.cn", "dqgz/dqxw", 2, 13, ".htm"); got != "https://iscit.swjtu.edu.cn/dqgz/dqxw/12.htm" {
		t.Fatalf("reverse page URL = %q", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 13},
		{14, 13},
		{2, 0},
	} {
		if got := iscitPageURL("https://iscit.swjtu.edu.cn", "dqgz/dqxw", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
	if _, _, err := iscitListPage("dqgz/dqxw", 0); err == nil {
		t.Fatal("ISCit custom list accepted page zero")
	}
}

func TestIscitListRequiresPaginationEvidence(t *testing.T) {
	raw := []byte(strings.TrimSpace(`
<!doctype html>
<html><head><meta name="pageType" content="2"></head><body>
<div class="new-list"><div class="newlb-box"><ul>
  <li><a href="/info/1102/8490.htm">一条新闻</a></li>
</ul></div></div>
</body></html>`))
	if _, err := iscitTotalPagesFromHTML(raw, "dqgz/dqxw"); err == nil {
		t.Fatal("ISCit short list without pagination was treated as complete")
	}
}

func TestIscitListFiltersNonArticlesAndMergesDuplicates(t *testing.T) {
	html, err := os.ReadFile("testdata/iscit.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseIscitNews(html, "https://iscit.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].URL != "https://iscit.swjtu.edu.cn/info/1102/8490.htm" {
		t.Fatalf("first URL = %q", items[0].URL)
	}
	if items[0].Title != "2025届推免专题" || items[0].Date != "2024-10-16" {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[1].URL != "https://iscit.swjtu.edu.cn/info/1091/1131.htm" || items[1].Date != "2024-06-07" {
		t.Fatalf("second item = %+v", items[1])
	}
}

func TestIscitArticleDate(t *testing.T) {
	html, err := os.ReadFile("testdata/iscit_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseIscitArticle(html, "https://iscit.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "【讲座预告】探寻智能出行与交通安全研究的结合点" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2025-02-26" {
		t.Fatalf("date = %q, want 2025-02-26", article.Date)
	}
}

func TestNormalizeIscitDateDoesNotInventYear(t *testing.T) {
	if got := normalizeIscitDate("07-08"); got != "" {
		t.Fatalf("date = %q, want empty", got)
	}
}
