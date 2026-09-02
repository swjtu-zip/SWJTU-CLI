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

func TestParseCwcLoginShellIsRejectedAsArticle(t *testing.T) {
	html, err := os.ReadFile("testdata/cwc.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseCwcArticle(html, "https://cwc.swjtu.edu.cn")
	if err == nil {
		t.Fatal("expected the CWC login SPA shell to return an article error")
	}
	if article != nil {
		t.Fatalf("expected no article on error, got %#v", article)
	}
	if !strings.Contains(err.Error(), "login SPA shell") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCwcDoesNotReportUnrecognisedPagesAsEmptySuccess(t *testing.T) {
	html := []byte(`<html><head><title>暂时无法访问</title></head><body>error</body></html>`)

	items, err := parseCwcNewsList(html, "https://cwc.swjtu.edu.cn")
	if err == nil {
		t.Fatalf("expected an error for an unrecognised list page, got %d items", len(items))
	}
	if items != nil {
		t.Fatalf("expected no items on error, got %d", len(items))
	}

	article, err := parseCwcArticle(html, "https://cwc.swjtu.edu.cn")
	if err == nil {
		t.Fatal("expected an error for an unrecognised article page")
	}
	if article != nil {
		t.Fatalf("expected no article on error, got %#v", article)
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
	if site.BaseURL != "https://cwc.swjtu.edu.cn" {
		t.Fatalf("CWC base URL = %q, want canonical CWC host", site.BaseURL)
	}
	if len(site.SubPages) != 0 {
		t.Fatalf("CWC has no currently reachable public list routes; got guessed sub-pages %#v", site.SubPages)
	}
}
