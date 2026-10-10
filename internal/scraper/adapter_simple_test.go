package scraper

import "testing"

// These are the routes confirmed from the live navigation and list pages for
// the adapters that use the shared Boda parser. Keeping the counts in a test
// prevents a future cleanup from silently reverting to one homepage feed.
func TestAuditedBodaAdaptersExposeAllDiscoveredFeeds(t *testing.T) {
	want := map[string]int{
		"phys": 17, "ott": 12, "rsc": 17, "rvl": 22, "rwxy": 15,
		"scyshj": 6, "sj": 15, "sports": 20, "talent": 4, "xb": 7,
		"xczx": 2, "xgh": 10, "xuncha": 9, "xyb": 7, "ygb": 10, "zzxy": 23,
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
