package scraper

import (
	"os"
	"testing"
)

func TestDynamicOrRedirectSitesDoNotReportEmptySuccess(t *testing.T) {
	cases := []struct {
		id   string
		file string
	}{
		{id: "fad", file: "fad.html"},
		{id: "jwc", file: "jwc.html"},
		{id: "lib", file: "lib.html"},
		{id: "xg", file: "xg.html"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			body, err := os.ReadFile("testdata/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			site := AllSites[tc.id]
			items, err := site.ParseFunc(body, site.BaseURL)
			if err == nil {
				t.Fatalf("expected an unavailable/dynamic page error, got %d items", len(items))
			}
			if items != nil {
				t.Fatalf("expected no items on error, got %d", len(items))
			}
		})
	}

	items, err := parseAdapter_journalsList([]byte(`<html><body><div id="app"></div></body></html>`), AllSites["journals"].BaseURL)
	if err == nil || items != nil {
		t.Fatalf("journals empty shell: items=%d err=%v", len(items), err)
	}
}
