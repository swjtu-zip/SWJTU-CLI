package scraper

import (
	"reflect"
	"strings"
	"testing"
)

func TestNewsAdapterRegistrationAndPageURL(t *testing.T) {
	if NewsSite.ParseFunc == nil || NewsSite.ParseArticleFunc == nil || NewsSite.PageURL == nil {
		t.Fatal("news adapter hooks are not registered")
	}
	wantSubPages := []string{"jdyw2", "zhxw", "mtjd", "ybdt", "index/kxts", "index/tzgg", "index/xshd"}
	if !reflect.DeepEqual(NewsSite.SubPages, wantSubPages) {
		t.Fatalf("news sub-pages = %#v, want %#v", NewsSite.SubPages, wantSubPages)
	}

	base := "https://news.swjtu.edu.cn"
	tests := []struct {
		name       string
		slug       string
		page       int
		totalPages int
		want       string
	}{
		{name: "current jdyw first", slug: "jdyw", page: 1, totalPages: 113, want: base + "/jdyw2.htm"},
		{name: "current jdyw second", slug: "jdyw", page: 2, totalPages: 113, want: base + "/jdyw2/112.htm"},
		{name: "current jdyw last", slug: "jdyw2", page: 113, totalPages: 113, want: base + "/jdyw2/1.htm"},
		{name: "comprehensive second", slug: "zhxw", page: 2, totalPages: 39, want: base + "/zhxw/38.htm"},
		{name: "department second", slug: "ybdt", page: 2, totalPages: 75, want: base + "/ybdt/74.htm"},
		{name: "media second", slug: "mtjd", page: 2, totalPages: 74, want: base + "/mtjd/73.htm"},
		{name: "science second", slug: "index/kxts", page: 2, totalPages: 10, want: base + "/index/kxts/9.htm"},
		{name: "notice second", slug: "index/tzgg", page: 2, totalPages: 18, want: base + "/index/tzgg/17.htm"},
		{name: "academic last", slug: "xshd", page: 50, totalPages: 50, want: base + "/index/xshd/1.htm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newsPageURL(base, tt.slug, tt.page, tt.totalPages, ".htm"); got != tt.want {
				t.Fatalf("newsPageURL() = %q, want %q", got, tt.want)
			}
		})
	}
	if got := newsPageURL(base, "jdyw2", 114, 113, ".htm"); got != "" {
		t.Fatalf("out-of-range news page URL = %q, want empty", got)
	}
	if got := newsPageURL(base, "jdyw2", 2, 0, ".htm"); got != "" {
		t.Fatalf("unknown-total news page URL = %q, want empty", got)
	}
}

func TestNewsListParserOnlyReadsMainList(t *testing.T) {
	html := loadTestHTML("news_list.html")
	items, err := parseAdapter_newsList(html, "https://news.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d list items, want 2 (sidebar must be excluded): %#v", len(items), items)
	}
	if items[0].Title != "西南交大学子暑期在全国体育赛事中斩获佳绩" || items[0].Date != "2026-08-29" || items[0].Type != "交大要闻" {
		t.Fatalf("unexpected first item: %#v", items[0])
	}
	if items[0].URL != "https://news.swjtu.edu.cn/info/8081/198746.htm" {
		t.Fatalf("unexpected first URL: %q", items[0].URL)
	}
	if items[1].Date != "2026-08-27" || items[1].PublishedAt != "2026-08-27 14:03:05" {
		t.Fatalf("unexpected second date precision: date=%q published_at=%q", items[1].Date, items[1].PublishedAt)
	}
	for _, item := range items {
		if strings.Contains(item.Title, "侧栏") || strings.Contains(item.Title, "导航") {
			t.Fatalf("non-list content leaked into results: %#v", item)
		}
	}
}

func TestNewsAcademicListDoesNotTreatEventTimeAsPublishDate(t *testing.T) {
	html := loadTestHTML("news_academic.html")
	items, err := parseAdapter_newsList(html, "https://news.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d academic items, want 2", len(items))
	}
	if items[0].Title != "【创源大讲堂】骨架编辑：仲胺的氮原子删除和插入" {
		t.Fatalf("unexpected academic title: %#v", items[0])
	}
	if items[0].Date != "" {
		t.Fatalf("event date was incorrectly used as publish date: %q", items[0].Date)
	}
	if items[0].Type != "学术活动" {
		t.Fatalf("column type was not retained: %q", items[0].Type)
	}
	if items[1].Title != "【创源大讲堂】《上扬子北部中生代复合-联合构造》" {
		t.Fatalf("valid academic title ending in a closing quote was dropped: %#v", items[1])
	}
}

func TestNewsListParserReturnsEmptyOnlyForAnExplicitEmptyList(t *testing.T) {
	items, err := parseAdapter_newsList(loadTestHTML("news_empty_list.html"), "https://news.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty news list = %#v, want a non-nil empty result", items)
	}

	malformed := []byte(`<div class="ny-right"><div class="list2"><ul><li><a href="javascript:;">坏行</a></li></ul></div></div>`)
	if _, err := parseAdapter_newsList(malformed, "https://news.swjtu.edu.cn"); err == nil {
		t.Fatal("malformed article row was accepted as an empty list")
	}
}

func TestNewsArticleParserExtractsMetadataResourcesAndAttachments(t *testing.T) {
	html := loadTestHTML("news_article.html")
	article, err := parseAdapter_newsArticle(html, "https://news.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "王平调研东部（国际）校区首批本科生搬迁入驻情况" {
		t.Fatalf("unexpected title: %q", article.Title)
	}
	if article.Source != "党委宣传部（新闻中心）" {
		t.Fatalf("unexpected source: %q", article.Source)
	}
	if article.Date != "2026-07-18" || article.PublishedAt != "2026-07-18 14:03:05" {
		t.Fatalf("unexpected date precision: date=%q published_at=%q", article.Date, article.PublishedAt)
	}
	if article.Type != "交大要闻" {
		t.Fatalf("unexpected article type: %q", article.Type)
	}
	if article.Author != "张三 李四" || article.Photographer != "王五" || article.Editor != "赵六" {
		t.Fatalf("unexpected credits: author=%q photographer=%q editor=%q", article.Author, article.Photographer, article.Editor)
	}
	if !strings.Contains(article.Content, "第一段正文") || !strings.Contains(article.Content, "第二段正文") || strings.Contains(article.Content, "导航不应被当作正文") {
		t.Fatalf("unexpected article content: %q", article.Content)
	}
	if len(article.Images) != 1 || article.Images[0] != "https://news.swjtu.edu.cn/__local/A/BB/CC/photo.jpg" {
		t.Fatalf("unexpected images: %#v", article.Images)
	}
	if len(article.Attachments) != 1 {
		t.Fatalf("got %d attachments, want only the usable direct file: %#v", len(article.Attachments), article.Attachments)
	}
	if article.Attachments[0].Name != "附件1 方案.docx" || article.Attachments[0].URL != "https://news.swjtu.edu.cn/__local/F/0A/09/file.png" {
		t.Fatalf("unexpected first attachment: %#v", article.Attachments[0])
	}
	if !newsIsCaptchaDownloadURL("https://news.swjtu.edu.cn/system/_content/download.jsp?urltype=news.DownloadAttachUrl") {
		t.Fatal("CAPTCHA download endpoint was not classified as an unusable resource")
	}
	if !strings.Contains(article.ContentHTML, `src="https://news.swjtu.edu.cn/__local/A/BB/CC/photo.jpg"`) || !strings.Contains(article.ContentHTML, `href="https://news.swjtu.edu.cn/__local/F/0A/09/file.png"`) {
		t.Fatalf("content HTML did not retain resolved resources: %s", article.ContentHTML)
	}
	if strings.Contains(article.ContentHTML, "data:image/") || strings.Contains(strings.Join(article.Images, "\n"), "data:") {
		t.Fatalf("pseudo image leaked into article resources: html=%s images=%#v", article.ContentHTML, article.Images)
	}
}

func TestNewsParsersRejectHomepageShell(t *testing.T) {
	home := []byte(`<html><body><nav><a href="info/8081/198746.htm">首页导航新闻标题</a></nav><div class="ny-news"><h3>最新新闻</h3><a href="info/8051/114076.htm">侧栏</a></div></body></html>`)
	if _, err := parseAdapter_newsList(home, "https://news.swjtu.edu.cn"); err == nil {
		t.Fatal("homepage shell was accepted as a news list")
	}
	if _, err := parseAdapter_newsArticle(home, "https://news.swjtu.edu.cn"); err == nil {
		t.Fatal("homepage shell was accepted as an article")
	}
}
