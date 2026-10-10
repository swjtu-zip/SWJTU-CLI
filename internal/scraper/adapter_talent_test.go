package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func readTalentFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read talent fixture %s: %v", name, err)
	}
	return body
}

func TestTalentListRowsCarryTitleOutsideTheAnchor(t *testing.T) {
	// The current template nests the date/MORE+ anchors inside the row's
	// main anchor, so the HTML parser closes the outer anchor before the
	// .title box. The parser must recover the title from the row instead of
	// treating the split date in the first anchor as the title.
	items, err := parseAdapter_talentList(readTalentFixture(t, "talent_tzgg_live.html"), "https://talent.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 10 {
		t.Fatalf("tzgg rows = %d, want 10: %#v", len(items), items)
	}
	first := items[0]
	if first.Title != "关于2026年度国家自然科学基金项目申请与结题等有关事项的通告" {
		t.Fatalf("first title = %q", first.Title)
	}
	if first.Date != "2026-01-14" || first.Type != "通知公告" {
		t.Fatalf("first item = %#v", first)
	}
	if first.URL != "https://talent.swjtu.edu.cn/info/1151/4941.htm" {
		t.Fatalf("first URL = %q", first.URL)
	}
	for _, item := range items {
		if strings.TrimSpace(item.Title) == "" || item.Date == "" {
			t.Fatalf("incomplete item: %#v", item)
		}
	}
}

func TestTalentHomepageFixtureStillParses(t *testing.T) {
	items, err := parseAdapter_talentList(readTalentFixture(t, "talent.html"), "https://talent.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("homepage fixture returned no items")
	}
}

func TestTalentListNewsPageFetchesAuditedRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xwzx/tzgg.htm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(readTalentFixture(t, "talent_tzgg_live.html"))
	}))
	defer server.Close()

	site := AllSites["talent"]
	oldBase, oldClient, oldNoCache := site.BaseURL, defaultClient, NoCache
	site.BaseURL, defaultClient, NoCache = server.URL, server.Client(), true
	ClearCache()
	t.Cleanup(func() {
		site.BaseURL, defaultClient, NoCache = oldBase, oldClient, oldNoCache
		ClearCache()
	})

	items, err := site.ListNewsPage("xwzx/tzgg", 1)
	if err != nil || len(items) != 10 {
		t.Fatalf("ListNewsPage items=%d err=%v", len(items), err)
	}
	if items[0].Title != "关于2026年度国家自然科学基金项目申请与结题等有关事项的通告" {
		t.Fatalf("first item = %#v", items[0])
	}
}
