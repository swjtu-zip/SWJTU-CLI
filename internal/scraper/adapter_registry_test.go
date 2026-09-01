package scraper

import "testing"

// Every registered website must have its own adapter entry. The adapters may
// share low-level helpers, but no site is allowed to silently fall back to a
// document-wide generic parser with no site-level extension point.
func TestEveryRegisteredSiteHasAdapter(t *testing.T) {
	if len(AllSites) != 70 {
		t.Fatalf("site registry has %d sites, want 70", len(AllSites))
	}
	for id, site := range AllSites {
		if site.ParseFunc == nil {
			t.Errorf("site %q has no list adapter", id)
		}
		if site.ParseArticleFunc == nil {
			t.Errorf("site %q has no article adapter", id)
		}
	}
	if NewsSite.ParseFunc == nil || NewsSite.ParseArticleFunc == nil {
		t.Error("news site has no adapter")
	}
}
