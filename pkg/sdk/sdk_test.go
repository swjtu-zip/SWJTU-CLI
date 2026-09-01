package sdk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
