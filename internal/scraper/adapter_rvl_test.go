package scraper

import "testing"

func TestRvlAdapterParsesLiveRowsAndOwnsReversePagination(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="新闻动态"></head><body>
<div class="con_tit"><h3>新闻动态</h3></div>
<div class="conlistComm">
  <dl><dt><span><a href="xwdt.htm">[新闻动态]</a></span><a href="../info/1022/20378.htm" title="车辆工程与测控研究所2024级硕士研究生党支部组织开展“轨辙所向，青春以行”校级示范性学生党支部特色活动">截断标题</a></dt><dd>时间：2026-06-25&nbsp;&nbsp;阅读次数：1</dd></dl>
</div>
<div class="pb_sys_common"><span class="p_t">共152条</span><span class="p_pages"><span class="p_no_d">1</span><a href="xwdt/15.htm">2</a><a href="xwdt/1.htm">16</a></span><span class="p_t">1/16</span></div>
</body></html>`)

	items, err := parseAdapter_rvlList(html, "http://rvl.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Title != "车辆工程与测控研究所2024级硕士研究生党支部组织开展“轨辙所向，青春以行”校级示范性学生党支部特色活动" {
		t.Errorf("title = %q", items[0].Title)
	}
	if items[0].Date != "2026-06-25" || items[0].PublishedAt != "2026-06-25" {
		t.Errorf("date fields = %q/%q", items[0].Date, items[0].PublishedAt)
	}
	if items[0].Type != "新闻动态" {
		t.Errorf("type = %q", items[0].Type)
	}
	if got := rvlTotalPagesFromHTML(html); got != 16 {
		t.Fatalf("total pages = %d, want 16", got)
	}
	base := "http://rvl.swjtu.edu.cn"
	if got := rvlPageURL(base, "djgz/xwdt", 2, 16, ".htm"); got != base+"/djgz/xwdt/15.htm" {
		t.Fatalf("logical page 2 URL = %q", got)
	}
	if got := rvlPageURL(base, "djgz/xwdt", 16, 16, ".htm"); got != base+"/djgz/xwdt/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
	if got := rvlPageURL(base, "djgz/xwdt", 17, 16, ".htm"); got != "" {
		t.Fatalf("out-of-range URL = %q", got)
	}
}

func TestRvlAdapterRegistersLiveNoticeTabs(t *testing.T) {
	want := map[string]bool{
		"djgz/dwgzgg":   true,
		"yjsjy/yjspygg": true,
		"djgz/xwdt":     true,
		"kxyj/kygg":     true,
		"xsgz/tzgg":     true,
		"rsgz/tzgg":     true,
	}
	for _, slug := range AllSites["rvl"].SubPages {
		delete(want, slug)
	}
	for slug := range want {
		t.Errorf("rvl missing discovered tab %q", slug)
	}
	if AllSites["rvl"].PageURL == nil || AllSites["rvl"].TotalPagesFunc == nil {
		t.Fatal("rvl reverse pagination hooks are not registered")
	}
}
