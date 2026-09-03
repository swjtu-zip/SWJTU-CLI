package scraper

import "testing"

// These are the routes confirmed from the live navigation and list pages for
// the adapters that use the shared Boda parser. Keeping the counts in a test
// prevents a future cleanup from silently reverting to one homepage feed.
func TestAuditedBodaAdaptersExposeAllDiscoveredFeeds(t *testing.T) {
	want := map[string]int{
		"phys": 11, "ott": 15, "rsc": 15, "rvl": 13, "rwxy": 15,
		"scyshj": 8, "sj": 8, "sports": 6, "talent": 4, "xb": 7,
		"xczx": 11, "xgh": 10, "xuncha": 9, "xyb": 8, "ygb": 7, "zzxy": 8,
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
