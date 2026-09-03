package scraper

import (
	"strings"
	"testing"
)

func TestOTTAdapterRegistersAuditedFeedsAndReversePages(t *testing.T) {
	site := AllSites["ott"]
	if site == nil || site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatal("ott adapter is not registered")
	}
	if len(site.SubPages) != len(ottSubPages) {
		t.Fatalf("ott feeds = %d, want %d", len(site.SubPages), len(ottSubPages))
	}
	wantFeeds := []string{"xwdt1/xydt", "y_j_y/yjydt", "g_j_d_x_k_j_y/kjydt"}
	for _, feed := range wantFeeds {
		found := false
		for _, got := range site.SubPages {
			if got == feed {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing OTT live feed %q", feed)
		}
	}
	if got := ottPageURL(site.BaseURL, "xwdt1/gzdt", 2, 16, ".htm"); got != site.BaseURL+"/xwdt1/gzdt/15.htm" {
		t.Fatalf("ott reverse page URL = %q", got)
	}
	if got := ottPageURL(site.BaseURL, "xwdt1/gzdt", 16, 16, ".htm"); got != site.BaseURL+"/xwdt1/gzdt/1.htm" {
		t.Fatalf("ott last page URL = %q", got)
	}
	if got := ottPageURL(site.BaseURL, "xwdt1/gzdt", 17, 16, ".htm"); got != "" {
		t.Fatalf("ott out-of-range page URL = %q, want empty", got)
	}
	if got := ottPageURL(site.BaseURL, "xwdt1/gzdt", 2, 0, ".htm"); got != "" {
		t.Fatalf("ott unknown-total page URL = %q, want empty", got)
	}
}

func TestOTTListParsesScopedMetadata(t *testing.T) {
	items := parseOTTListRows([]byte(`<html><div class="list-left-tt">工作动态</div><ul class="lb-list">
<li><a href="../info/1064/3432.htm"><div class="lb-list-date">25<br><span>2025.04</span></div><div class="lb-list-right"><div class="lb-list-tt">完整的技术转移文章标题</div></div></a></li>
<li><a href="https://news.swjtu.edu.cn/info/1011/1.htm"><div class="lb-list-tt">外部文章</div></a></li>
</ul></html>`), "http://ott.swjtu.edu.cn")
	if len(items) != 1 {
		t.Fatalf("ott items = %#v", items)
	}
	if items[0].Title != "完整的技术转移文章标题" || items[0].Date != "2025-04-25" || items[0].PublishedAt != "2025-04-25" || items[0].Type != "工作动态" {
		t.Fatalf("ott metadata = %#v", items[0])
	}
}

func TestOTTArticleAddsSectionType(t *testing.T) {
	article, err := parseAdapter_ottArticle([]byte(`<html><title>技术转移文章-西南交通大学技术转移研究院</title><meta name="pageType" content="3"><h1 class="article-tt">技术转移文章</h1><div class="list-banner-tt">工作动态</div><div id="vsb_content"><div class="v_news_content"><p>正文内容足够长且准确。</p><img data-src="/images/body.jpg"><a download="通知附件.pdf" href="/download?id=1">下载附件</a></div></div><div class="article-sm">发布日期：2025-04-25</div></html>`), "http://ott.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Type != "工作动态" || !strings.Contains(article.Content, "正文内容") || article.Date != "2025-04-25" {
		t.Fatalf("ott article = %#v", article)
	}
	if len(article.Images) != 1 || article.Images[0] != "http://ott.swjtu.edu.cn/images/body.jpg" || len(article.Attachments) != 1 || article.Attachments[0].Name != "通知附件.pdf" {
		t.Fatalf("ott article resources = images=%#v attachments=%#v", article.Images, article.Attachments)
	}
}
