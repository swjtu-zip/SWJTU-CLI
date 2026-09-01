package scraper

import (
	"os"
	"strings"
	"testing"
)

func TestParseCwcLoginShellReturnsError(t *testing.T) {
	html, err := os.ReadFile("testdata/cwc.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseCwcNewsList(html, "https://cwc.swjtu.edu.cn")
	if err == nil {
		t.Fatal("expected the CWC login SPA shell to return an error")
	}
	if items != nil {
		t.Fatalf("expected no items on error, got %d", len(items))
	}
	if !strings.Contains(err.Error(), "login SPA shell") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCwcAdapterRegistered(t *testing.T) {
	site := AllSites["cwc"]
	if site == nil {
		t.Fatal("CWC site is not registered")
	}
	if site.ParseFunc == nil {
		t.Fatal("CWC list adapter is not registered")
	}
	if site.ParseArticleFunc == nil {
		t.Fatal("CWC article adapter is not registered")
	}
}
