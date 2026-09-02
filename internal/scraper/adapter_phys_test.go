package scraper

import "testing"

func TestPhysAdapterRegistersAuditedArticleFeeds(t *testing.T) {
	site := AllSites["phys"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil {
		t.Fatal("phys adapter is not registered")
	}
	want := map[string]bool{
		"xwgg/xydt": true, "kxyj/xsdt": true, "kxyj/kytd": true,
		"rcpy/bkspy": true, "rcpy/yjspy": true,
		"xsfz/gqtgz": true, "xsfz/xgdt": true, "xsfz/xgtz": true,
		"xygz/xyjy": true, "dqgz/jjgz": true, "dqgz/pyfz": true, "dqgz/xxjy": true,
	}
	got := make(map[string]bool, len(site.SubPages))
	for _, slug := range site.SubPages {
		got[slug] = true
	}
	if len(got) != len(want) {
		t.Fatalf("phys feed count = %d, want %d (%v)", len(got), len(want), site.SubPages)
	}
	for slug := range want {
		if !got[slug] {
			t.Errorf("phys audited feed %q is missing", slug)
		}
	}
}
