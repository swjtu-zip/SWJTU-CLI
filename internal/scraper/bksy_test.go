package scraper

import (
	"testing"
)

func TestBksyRegistered(t *testing.T) {
	site, ok := AllSites["bksy"]
	if !ok {
		t.Fatal("bksy site is not registered")
	}
	if site.BaseURL != "https://bksy.swjtu.edu.cn" {
		t.Fatalf("unexpected base URL: %q", site.BaseURL)
	}
	if site.Category != "admin" || site.ParseFunc == nil {
		t.Fatalf("unexpected site configuration: %+v", site)
	}
	if len(site.SubPages) == 0 || site.SubPages[0] != "tzgg/qb" {
		t.Fatalf("unexpected sub-pages: %#v", site.SubPages)
	}
}

func TestBksyPageURL(t *testing.T) {
	const base = "https://bksy.swjtu.edu.cn"
	if got := bksyPageURL(base, "tzgg/qb", 1, 243, ".htm"); got != base+"/tzgg/qb.htm" {
		t.Fatalf("unexpected first page URL: %q", got)
	}
	if got := bksyPageURL(base, "tzgg/qb", 2, 243, ".htm"); got != base+"/tzgg/qb/242.htm" {
		t.Fatalf("unexpected second page URL: %q", got)
	}
	if got := bksyPageURL(base, "tzgg/qb", 243, 243, ".htm"); got != base+"/tzgg/qb/1.htm" {
		t.Fatalf("unexpected last page URL: %q", got)
	}
}

func TestBksyParseCategory(t *testing.T) {
	html := loadTestHTML("bksy.html")
	if html == nil {
		t.Fatal("testdata/bksy.html not found")
	}

	items, err := parseBksyNews(html, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 announcements, got %d", len(items))
	}

	if items[0].Title != "关于开展2026年本科生转专业相关工作的通知" || items[0].Date != "2026/07/08" {
		t.Fatalf("unexpected first item: %+v", items[0])
	}
	if items[0].URL != "https://bksy.swjtu.edu.cn/info/1121/94301.htm" {
		t.Fatalf("unexpected first URL: %q", items[0].URL)
	}
	if items[2].URL != "https://bksy.swjtu.edu.cn/__local/file.pdf" {
		t.Fatalf("unexpected attachment URL: %q", items[2].URL)
	}
}

func TestBksyParseHomepage(t *testing.T) {
	html := []byte(`<main>
<div id="content-1"><ul>
  <li><a href="info/1121/94301.htm"><p class="content line1">关于开展2026年本科生转专业相关工作的通知 </p><time>2026/07/08</time></a></li>
  <li><a href="info/1151/94201.htm"><p class="content line1">关于做好2026-2027学年第一学期选课工作的通知</p><time>2026/07/02</time></a></li>
</ul></div>
<div class="other"><a href="info/9999/1.htm">不应从其他首页模块提取</a></div>
</main>`)

	items, err := parseBksyNews(html, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 homepage announcements, got %d", len(items))
	}
}

func TestBksyArticleFields(t *testing.T) {
	html := []byte(`<main>
  <div class="news-title fw">关于开展2026年本科生转专业相关工作的通知</div>
  <div class="tips">
    <div class="left-details">2026-07-08 09:24</div>
    <div class="middle2">来源：本科生院</div>
  </div>
  <div class="article-content"><div class="v_news_content"><h3>开头说明</h3><p>正文内容</p></div></div>
  <div class="left-news-details"><ul><li>附件【<a href="/system/_content/download.jsp?id=1">附件.pdf</a>】</li></ul></div>
</main>`)

	article, err := parseBodaArticle(html, "https://bksy.swjtu.edu.cn")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if article.Title != "关于开展2026年本科生转专业相关工作的通知" || article.Date != "2026-07-08" || article.Source != "本科生院" {
		t.Fatalf("unexpected article fields: %+v", article)
	}
	if article.Content != "开头说明\n\n正文内容" {
		t.Fatalf("unexpected article content: %q", article.Content)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "附件.pdf" || article.Attachments[0].URL != "https://bksy.swjtu.edu.cn/system/_content/download.jsp?id=1" {
		t.Fatalf("unexpected article attachments: %#v", article.Attachments)
	}
}
