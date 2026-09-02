package scraper

import "testing"

// These are the routes confirmed from the live navigation and list pages for
// the adapters that use the shared Boda parser. Keeping the counts in a test
// prevents a future cleanup from silently reverting to one homepage feed.
func TestAuditedBodaAdaptersExposeAllDiscoveredFeeds(t *testing.T) {
	want := map[string]int{
		"phys": 12, "ott": 12, "rsc": 11, "rvl": 11, "rwxy": 14,
		"scyshj": 8, "sj": 8, "sports": 6, "talent": 5, "xb": 7,
		"xczx": 11, "xgh": 8, "xuncha": 5, "xyb": 9, "ygb": 6, "zzxy": 7,
	}
	for id, count := range want {
		site := AllSites[id]
		if site == nil {
			t.Fatalf("site %q is not registered", id)
		}
		if len(site.SubPages) != count {
			t.Errorf("%s feed count = %d, want %d (%#v)", id, len(site.SubPages), count, site.SubPages)
		}
		if site.PageURL == nil {
			t.Errorf("%s has no page URL adapter for its reverse-numbered lists", id)
		}
	}
}
