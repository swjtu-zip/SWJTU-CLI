package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const icTestBaseURL = "https://ic.swjtu.edu.cn"

func loadICFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read IC fixture %s: %v", name, err)
	}
	return body
}

func TestICAdapterRegistrationAndFeeds(t *testing.T) {
	site := AllSites["ic"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil || site.ListPageFunc == nil {
		t.Fatal("IC adapter is not fully registered")
	}
	want := []string{"xwzx", "xwzx/tzgg"}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("IC feeds = %#v, want %#v", site.SubPages, want)
	}
	if site.BaseURL != icTestBaseURL {
		t.Fatalf("IC base URL = %q, want %q", site.BaseURL, icTestBaseURL)
	}
}

func TestICPageURLUsesReverseGeneratedFiles(t *testing.T) {
	base := icTestBaseURL
	want := map[string]string{
		"first":  base + "/xwzx.htm",
		"second": base + "/xwzx/2.htm",
		"middle": base + "/xwzx/2.htm",
		"last":   base + "/xwzx/1.htm",
	}
	got := map[string]string{
		"first":  icPageURL(base, "xwzx", 1, 3, ".htm"),
		"second": icPageURL(base, "xwzx", 2, 3, ".htm"),
		"middle": icPageURL(base, "xwzx", 2, 3, ".htm"),
		"last":   icPageURL(base, "xwzx", 3, 3, ".htm"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reverse page URLs = %#v, want %#v", got, want)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 3},
		{4, 3},
		{2, 0},
	} {
		if got := icPageURL(base, "xwzx", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
	if _, _, err := icListPage("xwzx", 0); err == nil {
		t.Fatal("IC custom list accepted page zero")
	}
}

func TestICHomepageParsesAllArticleModulesAndFiltersExternalLinks(t *testing.T) {
	items, err := parseAdapter_icList(loadICFixture(t, "ic.html"), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 9 {
		t.Fatalf("got %d homepage articles, want 9 (6 news + 3 local notice rows): %#v", len(items), items)
	}
	seen := make(map[string]bool)
	for _, item := range items {
		if seen[item.URL] {
			t.Fatalf("duplicate homepage URL %q", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, icTestBaseURL+"/info/") {
			t.Fatalf("external or non-article homepage URL %q", item.URL)
		}
		if item.Title == "" || item.Date == "" || item.Type == "" {
			t.Fatalf("incomplete homepage metadata: %+v", item)
		}
	}
	if !seen[icTestBaseURL+"/info/1061/2031.htm"] || !seen[icTestBaseURL+"/info/1091/1131.htm"] {
		t.Fatalf("visible homepage modules were not retained: %#v", seen)
	}
	if seen["https://iscit.swjtu.edu.cn/info/1102/8490.htm"] {
		t.Fatal("external ISCit notice leaked into IC feed")
	}
}

func TestICListParsesRowsAndExactPageCount(t *testing.T) {
	body := loadICFixture(t, "ic_list.html")
	items, err := parseAdapter_icList(body, icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 10 {
		t.Fatalf("got %d list items, want 10: %#v", len(items), items)
	}
	if items[0].Title != "东部校区第一条新闻" || items[0].Date != "2026-08-31" || items[0].PublishedAt != "2026-08-31 14:20:11" {
		t.Fatalf("first item metadata = %+v", items[0])
	}
	if items[0].Type != "新闻动态" {
		t.Fatalf("list item type = %q, want 新闻动态", items[0].Type)
	}
	total, err := icTotalPagesFromHTML(body, "xwzx")
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total pages = %d, want 3 from 共23条/10 rows", total)
	}
}

func TestICPageCountUsesSourceRowsWhenExternalRowsAreFiltered(t *testing.T) {
	body := string(loadICFixture(t, "ic_list.html"))
	body = strings.Replace(body, "href=\"info/1061/", "href=\"https://external.example/info/1061/", 5)
	items, err := parseAdapter_icList([]byte(body), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("filtered list items = %d, want 5", len(items))
	}
	total, err := icTotalPagesFromHTML([]byte(body), "xwzx")
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total pages = %d, want 3 from 23 source rows/10 rows, not parsed-row count", total)
	}
}

func TestICExplicitEmptyAndResourcePagesAreNotArticles(t *testing.T) {
	items, err := parseAdapter_icList(loadICFixture(t, "ic_empty.html"), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("explicit empty list = %#v, want non-nil empty slice", items)
	}
	if _, err := parseAdapter_icList(loadICFixture(t, "ic_resource.html"), icTestBaseURL); err == nil {
		t.Fatal("resource/download page was accepted as an article list")
	}
}

func TestICRejectsWAFAndNonArticleShells(t *testing.T) {
	if _, err := parseAdapter_icList(loadICFixture(t, "ic_waf.html"), icTestBaseURL); err == nil {
		t.Fatal("WAF shell was accepted as a list")
	}
	if _, err := parseAdapter_icArticle(loadICFixture(t, "ic_waf.html"), icTestBaseURL); err == nil {
		t.Fatal("WAF shell was accepted as an article")
	}
	if _, err := parseAdapter_icArticle(loadICFixture(t, "ic.html"), icTestBaseURL); err == nil {
		t.Fatal("homepage shell was accepted as an article")
	}
}

func TestICArticleExtractsMetadataBodyImagesAndAttachments(t *testing.T) {
	article, err := parseAdapter_icArticle(loadICFixture(t, "ic_article.html"), icTestBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "东部校区建设工作推进会" || article.Type != "新闻动态" {
		t.Fatalf("title/type = %q/%q", article.Title, article.Type)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:20:11" {
		t.Fatalf("date/published_at = %q/%q", article.Date, article.PublishedAt)
	}
	if article.Source != "东部校区管委会" {
		t.Fatalf("source = %q", article.Source)
	}
	if !strings.Contains(article.Content, "正文第一段") || strings.Contains(article.Content, "下一篇") {
		t.Fatalf("body extraction is inaccurate: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != icTestBaseURL+"/__local/ic-body.png" {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 2 {
		t.Fatalf("attachments = %#v, want body + outside attachment", article.Attachments)
	}
	if article.Attachments[0].Name != "会议通知.pdf" || article.Attachments[0].URL != icTestBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&wbfileid=ICFIX" {
		t.Fatalf("body attachment = %#v", article.Attachments[0])
	}
	if article.Attachments[1].Name != "附件2：校区安排.xlsx" || article.Attachments[1].URL != icTestBaseURL+"/__local/plan.xlsx" {
		t.Fatalf("outside attachment = %#v", article.Attachments[1])
	}
	if !strings.Contains(article.ContentHTML, `src="https://ic.swjtu.edu.cn/__local/ic-body.png"`) || !strings.Contains(article.ContentHTML, `href="https://ic.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl&amp;wbfileid=ICFIX"`) {
		t.Fatalf("resolved resources not retained in content HTML: %s", article.ContentHTML)
	}
}
