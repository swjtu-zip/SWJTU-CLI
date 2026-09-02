package sdk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"swjtu-cli/internal/model"
	"swjtu-cli/internal/scraper"
)

func TestFeedsHaveStableUniqueIDs(t *testing.T) {
	feeds := Feeds()
	if len(feeds) < 50 {
		t.Fatalf("expected the registered SWJTU feeds, got %d", len(feeds))
	}
	seen := make(map[string]bool, len(feeds))
	for _, feed := range feeds {
		if feed.ID == "" || feed.SiteID == "" || feed.BaseURL == "" {
			t.Fatalf("incomplete feed: %+v", feed)
		}
		if seen[feed.ID] {
			t.Fatalf("duplicate feed ID: %s", feed.ID)
		}
		seen[feed.ID] = true
	}
	for _, id := range []string{"news:zhxw", "news:index/kxts", "news:index/tzgg", "news:index/xshd"} {
		if !seen[id] {
			t.Errorf("missing news tab feed %q", id)
		}
	}
}

func TestSiteForURLMatchesExactHost(t *testing.T) {
	if got := siteForURL("https://sic.swjtu.edu.cn/info/1381/7231.htm"); got != scraper.AllSites["sic"] {
		t.Fatalf("SIC URL resolved to the wrong site: %q", got.Name)
	}
	if got := siteForURL("https://ic.swjtu.edu.cn/info/1/2.htm"); got != scraper.AllSites["ic"] {
		t.Fatalf("IC URL resolved to the wrong site: %q", got.Name)
	}
}

func TestDownloadResourceUsesResponseFilename(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="测试.pdf"`)
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("pdf"))
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	resource, err := client.DownloadResource(context.Background(), server.URL+"/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resource.Body.Close()
	if resource.Filename != "测试.pdf" || resource.ContentType != "application/pdf" {
		t.Fatalf("unexpected resource metadata: %+v", resource)
	}
	body, err := io.ReadAll(resource.Body)
	if err != nil || string(body) != "pdf" {
		t.Fatalf("unexpected resource body: %q (%v)", body, err)
	}
}

func TestDownloadResourceRejectsHTMLErrorPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>登录页</body></html>"))
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	if _, err := client.DownloadResource(context.Background(), server.URL+"/download"); err == nil {
		t.Fatal("expected HTML resource response to be rejected")
	}
}

func TestDownloadResourceWithReferer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != "https://example.org/article/42" {
			t.Fatalf("Referer = %q", got)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("file"))
	}))
	defer server.Close()

	client := New(Options{HTTPClient: server.Client()})
	resource, err := client.DownloadResourceWithReferer(context.Background(), server.URL+"/download", "https://example.org/article/42")
	if err != nil {
		t.Fatal(err)
	}
	defer resource.Body.Close()
	body, err := io.ReadAll(resource.Body)
	if err != nil || string(body) != "file" {
		t.Fatalf("unexpected resource body: %q (%v)", body, err)
	}
}

func TestListFeedRangeReturnsPartialItemsWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/news.htm" {
			_, _ = w.Write([]byte("page-one"))
			return
		}
		if r.URL.Path == "/news/2.htm" {
			http.Error(w, "upstream page failure", http.StatusBadGateway)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	site := scraper.AllSites["sic"]
	oldBase, oldParse, oldPage, oldListPage, oldTotal := site.BaseURL, site.ParseFunc, site.PageURL, site.ListPageFunc, site.TotalPagesFunc
	defer func() {
		site.BaseURL, site.ParseFunc, site.PageURL, site.ListPageFunc, site.TotalPagesFunc = oldBase, oldParse, oldPage, oldListPage, oldTotal
	}()
	site.BaseURL = server.URL
	site.PageURL = nil
	site.ListPageFunc = nil
	site.TotalPagesFunc = func(string) (int, error) { return 3, nil }
	site.ParseFunc = func(raw []byte, _ string) ([]model.NewsItem, error) {
		if strings.Contains(string(raw), "page-one") {
			return []model.NewsItem{{Title: "第一页文章标题", URL: server.URL + "/info/1/1.htm", Date: "2026-08-31"}}, nil
		}
		return nil, fmt.Errorf("unexpected list body")
	}

	oldNoCache := scraper.NoCache
	scraper.NoCache = true
	defer func() { scraper.NoCache = oldNoCache }()
	client := New(Options{})
	items, err := client.ListFeedRange(context.Background(), Feed{
		ID: "sic:index", SiteID: "sic", Name: "首页新闻", Slug: "news", BaseURL: server.URL,
	}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), 0)
	if err == nil {
		t.Fatal("expected a surfaced later-page error")
	}
	if len(items) != 1 || items[0].Title != "第一页文章标题" {
		t.Fatalf("SDK partial items = %#v, err=%v", items, err)
	}
}
