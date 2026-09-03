package scraper

import "testing"

func TestScyshjAdapterParsesLiveRowsAndOwnsReversePagination(t *testing.T) {
	html := []byte(`<html><head><meta name="pageTitle" content="通知公告"></head><body>
<div class="main_titT"><a href="../index.htm">首页</a><a href="../tzxw.htm">通知新闻</a><a href="tzgg.htm">通知公告</a></div>
<div class="main_conL"><div class="main_conLT"><a href="tzgg.htm" class="cur">通知公告</a></div></div>
<div class="main_conRCb"><ul>
  <li><span>2026-06-06</span><a href="../info/1160/3128.htm"><em>聚焦优化提升 共谋生态迭代：四川省优化营商环境工作片区座谈会在眉山召开</em></a></li>
</ul></div>
<table><tr><td id="fanye1059072">共53条&nbsp;&nbsp;1/4&nbsp;<a href="tzgg/3.htm">下页</a><a href="tzgg/1.htm">尾页</a></td></tr></table>
</body></html>`)

	items, err := parseAdapter_scyshjList(html, "https://scyshj.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Title != "聚焦优化提升 共谋生态迭代：四川省优化营商环境工作片区座谈会在眉山召开" {
		t.Errorf("title = %q", items[0].Title)
	}
	if items[0].Date != "2026-06-06" || items[0].PublishedAt != "2026-06-06" {
		t.Errorf("date fields = %q/%q", items[0].Date, items[0].PublishedAt)
	}
	if items[0].Type != "通知公告" {
		t.Errorf("type = %q", items[0].Type)
	}
	if got := scyshjTotalPagesFromHTML(html); got != 4 {
		t.Fatalf("total pages = %d, want 4", got)
	}
	base := "https://scyshj.swjtu.edu.cn"
	if got := scyshjPageURL(base, "tzxw/tzgg", 2, 4, ".htm"); got != base+"/tzxw/tzgg/3.htm" {
		t.Fatalf("logical page 2 URL = %q", got)
	}
	if got := scyshjPageURL(base, "tzxw/tzgg", 5, 4, ".htm"); got != "" {
		t.Fatalf("out-of-range URL = %q", got)
	}
}
