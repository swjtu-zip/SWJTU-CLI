package scraper

import (
	"os"
	"strings"
	"testing"
)

func TestDqxyAdapterList(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_list.html")
	if err != nil {
		t.Skip("fixture missing")
	}
	items, err := parseDqxyNews(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("no items parsed")
	}
	for _, item := range items {
		if !strings.Contains(item.URL, "/info/") {
			t.Errorf("bad URL %q", item.URL)
		}
		if len(item.Date) != 10 || item.Date[4] != '-' {
			t.Errorf("item %q has non-ISO date %q", item.Title, item.Date)
		}
	}
	t.Logf("%d items, first: %q %s", len(items), items[0].Title, items[0].Date)
}

func TestDqxyAdapterArticle(t *testing.T) {
	data, err := os.ReadFile("testdata/dqxy_article.html")
	if err != nil {
		t.Skip("fixture missing")
	}
	art, err := parseDqxyArticle(data, "http://dqxy.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if art.Title == "" {
		t.Error("empty title")
	}
	if art.Date != "2026-07-19" {
		t.Errorf("date = %q, want 2026-07-19", art.Date)
	}
	if len(art.Content) < 100 {
		t.Errorf("content too short: %d", len(art.Content))
	}
}
