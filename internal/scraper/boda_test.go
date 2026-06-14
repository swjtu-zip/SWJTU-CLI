package scraper

import (
	"os"
	"testing"
)

func loadTestHTML(filename string) []byte {
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		return nil
	}
	return data
}

// TestJdywParse tests parsing of 交大要闻
func TestJdywParse(t *testing.T) {
	html := loadTestHTML("jdyw.html")
	if html == nil {
		t.Skip("testdata/jdyw.html not found")
	}
	items, err := parseBodaNewsList(html, "https://news.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
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
		if item.URL == "" {
			t.Errorf("item %d: empty URL", i+1)
		}
	}
}

// TestCivilParse tests parsing of 土木学院
func TestCivilParse(t *testing.T) {
	html := loadTestHTML("civil.html")
	if html == nil {
		t.Skip("testdata/civil.html not found")
	}
	items, err := parseBodaNewsList(html, "http://civil.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 10 {
		n = 10
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check no banner content
	for i, item := range items {
		if containsAny(item.Title, "习近平给四所交通大学全体师生回信") {
			t.Errorf("item %d: banner content leaked: %s", i+1, item.Title)
		}
	}
}

// TestClxyParse tests parsing of 材料学院
func TestClxyParse(t *testing.T) {
	html := loadTestHTML("clxy.html")
	if html == nil {
		t.Skip("testdata/clxy.html not found")
	}
	items, err := parseBodaNewsList(html, "http://clxy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 10 {
		n = 10
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check no navigation links
	for i, item := range items {
		if item.Title == "院务公开" || item.Title == "联系方式" {
			t.Errorf("item %d: navigation link leaked: %s", i+1, item.Title)
		}
	}
	// Check no external links
	for i, item := range items {
		if containsAny(item.URL, "jiuye.swjtu.edu.cn") {
			t.Errorf("item %d: external link leaked: %s", i+1, item.URL)
		}
	}
}

// TestScyshjParse tests parsing of 营商环境研究院
func TestScyshjParse(t *testing.T) {
	html := loadTestHTML("scyshj.html")
	if html == nil {
		t.Skip("testdata/scyshj.html not found")
	}
	items, err := parseBodaNewsList(html, "https://scyshj.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 5 {
		n = 5
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check no date prefix in title
	for i, item := range items {
		if hasDatePrefix(item.Title) {
			t.Errorf("item %d: date prefix in title: %s", i+1, item.Title)
		}
	}
}

// TestYgbParse tests parsing of 党委研究生工作部
func TestYgbParse(t *testing.T) {
	html := loadTestHTML("ygb.html")
	if html == nil {
		t.Skip("testdata/ygb.html not found")
	}
	items, err := parseBodaNewsList(html, "https://ygb.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
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
}

// TestXczxParse tests parsing of 乡村振兴研究院
func TestXczxParse(t *testing.T) {
	html := loadTestHTML("xczx.html")
	if html == nil {
		t.Skip("testdata/xczx.html not found")
	}
	items, err := parseBodaNewsList(html, "https://xczx.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected items, got 0")
	}
	n := len(items)
	if n > 5 {
		n = 5
	}
	for i, item := range items[:n] {
		t.Logf("[%d] %s | %s | %s", i+1, item.Date, item.Title, item.URL)
	}
	// Check no date prefix in title
	for i, item := range items {
		if hasDatePrefix(item.Title) {
			t.Errorf("item %d: date prefix in title: %s", i+1, item.Title)
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func hasDatePrefix(s string) bool {
	// Check for patterns like "07/16", "2025-08-12", "06-01"
	if len(s) < 5 {
		return false
	}
	// 07/16 pattern
	if s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9' && s[2] == '/' && s[3] >= '0' && s[3] <= '9' && s[4] >= '0' && s[4] <= '9' {
		return true
	}
	// 2025-08-12 pattern
	if len(s) >= 10 && s[0] == '2' && s[1] == '0' && s[4] == '-' && s[7] == '-' {
		return true
	}
	return false
}
