package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func readJwFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read jw fixture %s: %v", name, err)
	}
	return body
}

func TestJwAdapterRegistrationAndReversePagination(t *testing.T) {
	site := AllSites["jw"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("jw adapter hooks are incomplete: %#v", site)
	}
	if site.BaseURL != jwBaseURL {
		t.Fatalf("jw base URL = %q, want %q", site.BaseURL, jwBaseURL)
	}
	want := []string{
		"gzdt", "gzdt/sjdt", "gzdt/xxdt", "gzdt/ejjw",
		"ywgz", "lzwh", "jsjy", "tzgg", "bszn",
	}
	if !reflect.DeepEqual(site.SubPages, want) {
		t.Fatalf("jw feeds = %#v, want %#v", site.SubPages, want)
	}
	if got := jwPageURL(jwBaseURL, "gzdt", 1, 49, ".htm"); got != jwBaseURL+"/gzdt.htm" {
		t.Fatalf("first page URL = %q", got)
	}
	if got := jwPageURL(jwBaseURL, "gzdt", 2, 49, ".htm"); got != jwBaseURL+"/gzdt/48.htm" {
		t.Fatalf("second page URL = %q", got)
	}
	if got := jwPageURL(jwBaseURL, "gzdt", 25, 49, ".htm"); got != jwBaseURL+"/gzdt/25.htm" {
		t.Fatalf("middle page URL = %q", got)
	}
	if got := jwPageURL(jwBaseURL, "gzdt", 49, 49, ".htm"); got != jwBaseURL+"/gzdt/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	for _, tc := range []struct {
		page, total int
	}{
		{0, 49},
		{50, 49},
		{2, 0},
	} {
		if got := jwPageURL(jwBaseURL, "gzdt", tc.page, tc.total, ".htm"); got != "" {
			t.Errorf("out-of-range page %d/%d returned %q", tc.page, tc.total, got)
		}
	}
	if got := parseTotalPages(readJwFixture(t, "jw_list.html")); got != 11 {
		t.Fatalf("pagination total = %d, want 11", got)
	}
}

func TestJwHomepageIncludesHiddenTabsAndArticleModules(t *testing.T) {
	items, err := parseAdapter_jwList(readJwFixture(t, "jw.html"), jwBaseURL)
	if err != nil {
		t.Fatalf("parse jw homepage: %v", err)
	}
	if len(items) != 40 {
		t.Fatalf("homepage unique article count = %d, want 40 in fixture", len(items))
	}
	wantTypes := map[string]string{
		"7199": "工作动态",
		"7299": "学校动态",
		"3976": "要闻关注",
		"7349": "通知公告",
		"7209": "支部建设",
		"7259": "二级纪委",
		"7119": "警示教育",
		"7339": "廉政文化",
	}
	for _, item := range items {
		if strings.Contains(item.URL, "/4709.htm") || strings.Contains(item.URL, "/3298.htm") {
			t.Fatalf("resource-download module leaked into homepage: %#v", item)
		}
		if !strings.HasPrefix(item.URL, jwBaseURL+"/info/") {
			t.Fatalf("non-local homepage link leaked: %#v", item)
		}
		for id, typ := range wantTypes {
			if strings.Contains(item.URL, "/"+id+".htm") && item.Type != typ {
				t.Errorf("homepage article %s type = %q, want %q", id, item.Type, typ)
			}
		}
	}
	for _, id := range []string{"7199", "7299", "3976", "7349", "7209", "7259", "7119", "7339"} {
		found := false
		for _, item := range items {
			if strings.Contains(item.URL, "/"+id+".htm") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("homepage module article %s was not retained", id)
		}
	}
}

func TestJwListScopesRowsAndPreservesMetadata(t *testing.T) {
	items, err := parseAdapter_jwList(readJwFixture(t, "jw_list.html"), jwBaseURL)
	if err != nil {
		t.Fatalf("parse jw list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("list items = %d, want 2", len(items))
	}
	if items[0].Title != "带精确时间的通知" || items[0].Date != "2026-07-17" || items[0].PublishedAt != "2026-07-17 15:06:00" || items[0].Type != "通知公告" {
		t.Fatalf("first list item metadata = %#v", items[0])
	}
	if items[1].Title != "学校动态文章" || items[1].Date != "2025-04-07" || items[1].Type != "学校动态" {
		t.Fatalf("second list item metadata = %#v", items[1])
	}
	for _, item := range items {
		if !strings.HasPrefix(item.URL, jwBaseURL+"/info/") {
			t.Fatalf("non-article URL leaked into list: %#v", item)
		}
	}
	if date, publishedAt := jwDateFields("发布时间：2026年7月19日 13:41:22"); date != "2026-07-19" || publishedAt != "2026-07-19 13:41:22" {
		t.Fatalf("second-level publication time = %q/%q", date, publishedAt)
	}
}

func TestJwDistinguishesEmptyStaticAndErrorPages(t *testing.T) {
	items, err := parseAdapter_jwList(readJwFixture(t, "jw_empty.html"), jwBaseURL)
	if err != nil {
		t.Fatalf("legal empty list rejected: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty list returned items: %#v", items)
	}
	for _, fixture := range []string{"jw_static.html", "jw_error.html"} {
		if _, err := parseAdapter_jwList(readJwFixture(t, fixture), jwBaseURL); err == nil {
			t.Errorf("%s was accepted as a list", fixture)
		}
	}
}

func TestJwArticleExtractsMetadataBodyImagesAndOutsideAttachments(t *testing.T) {
	article, err := parseAdapter_jwArticle(readJwFixture(t, "jw_article.html"), jwBaseURL)
	if err != nil {
		t.Fatalf("parse jw article: %v", err)
	}
	if article.Title != "校区纪委组织开展廉洁主题系列教育活动" || article.Date != "2026-07-19" || article.PublishedAt != "2026-07-19 13:41:00" || article.Type != "二级纪委" {
		t.Fatalf("article metadata = %#v", article)
	}
	if article.Source != "峨眉校区纪委" || article.Author != "王江丽" || article.Editor != "陈诗伟" {
		t.Fatalf("article credits = %#v", article)
	}
	if !strings.Contains(article.Content, "这是正文第一段") || strings.Contains(article.Content, "导航噪声") {
		t.Fatalf("article body scope is wrong: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != jwBaseURL+"/images/body.png" {
		t.Fatalf("article images = %#v", article.Images)
	}
	if !strings.Contains(article.ContentHTML, `src="https://jw.swjtu.edu.cn/images/body.png"`) {
		t.Fatalf("article ContentHTML did not retain resolved image: %s", article.ContentHTML)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "测试文件.docx" || article.Attachments[0].URL != jwBaseURL+"/system/_content/download.jsp?urltype=news.DownloadAttachUrl&owner=1447716176&wbfileid=FIXTURE" {
		t.Fatalf("article attachments = %#v", article.Attachments)
	}
}

func TestJwArticleRejectsNonArticleResponses(t *testing.T) {
	for _, fixture := range []string{"jw.html", "jw_list.html", "jw_static.html", "jw_error.html"} {
		if _, err := parseAdapter_jwArticle(readJwFixture(t, fixture), jwBaseURL); err == nil {
			t.Errorf("%s was accepted as an article", fixture)
		}
	}
}
