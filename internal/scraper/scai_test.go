package scraper

import (
	"testing"
)

func TestScaiParse(t *testing.T) {
	html := loadTestHTML("scai.html")
	if html == nil {
		t.Skip("testdata/scai.html not found")
	}
	items, err := parseScaiNews(html, "https://scai.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	t.Logf("Found %d items", len(items))
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 5 {
		n = 5
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
		if item.Title == "" {
			t.Errorf("item %d: empty title", i+1)
		}
	}
	// Check no banner content
	for i, item := range items {
		if containsAny(item.Title, "习近平") {
			t.Errorf("item %d: banner content: %s", i+1, item.Title)
		}
	}
}
