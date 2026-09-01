package scraper

import (
	"os"
	"testing"
)

func TestSmeAdapterRegistration(t *testing.T) {
	site, ok := AllSites["sme"]
	if !ok {
		t.Fatal("sme site is not registered")
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.PageURL == nil {
		t.Fatalf("SME adapter hooks are incomplete: %+v", site)
	}
	if len(site.SubPages) != 4 || site.SubPages[0] != "xwtz/xyxw" {
		t.Fatalf("unexpected SME sub-pages: %#v", site.SubPages)
	}
}

func TestSmeListParserExtractsArticlesAndDates(t *testing.T) {
	html, err := os.ReadFile("testdata/sme.html")
	if err != nil {
		t.Fatal(err)
	}

	items, err := parseSmeNews(html, "https://sme.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d SME articles, want 3: %#v", len(items), items)
	}

	want := []struct {
		title string
		url   string
		date  string
	}{
		{
			title: "机械工程学院召开第四届教代会暨第五届工代会第一次会议",
			url:   "https://sme.swjtu.edu.cn/info/1094/65561.htm",
			date:  "2026-07-13",
		},
		{
			title: "机械工程学院党委书记袁艳平为全体教职工讲授专题党课",
			url:   "https://sme.swjtu.edu.cn/info/1094/65551.htm",
			date:  "2026-07-11",
		},
		{
			title: "关于开展2026年7月主题党日的通知",
			url:   "https://sme.swjtu.edu.cn/info/1066/65431.htm",
			date:  "2026-07-08",
		},
	}
	for i, expected := range want {
		if items[i].Title != expected.title || items[i].URL != expected.url || items[i].Date != expected.date {
			t.Errorf("item %d = (%q, %q, %q), want (%q, %q, %q)", i, items[i].Title, items[i].URL, items[i].Date, expected.title, expected.url, expected.date)
		}
	}
	for _, item := range items {
		if item.Title == "学院简介" || item.Title == "首页" || item.Title == "横幅文章不应进入列表" {
			t.Errorf("navigation/banner item leaked: %#v", item)
		}
	}
}

func TestSmeArticleParserNormalizesPublishDate(t *testing.T) {
	html, err := os.ReadFile("testdata/sme_article.html")
	if err != nil {
		t.Fatal(err)
	}

	article, err := parseSmeArticle(html, "https://sme.swjtu.edu.cn")
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "机械工程学院召开第四届教代会暨第五届工代会第一次会议" {
		t.Fatalf("unexpected article title: %q", article.Title)
	}
	if article.Date != "2026-07-13" {
		t.Fatalf("article date = %q, want 2026-07-13", article.Date)
	}
}

func TestSmePageURLUsesReverseGeneratedNumbering(t *testing.T) {
	const baseURL = "https://sme.swjtu.edu.cn"
	if got := smePageURL(baseURL, "xwtz/xyxw", 1, 177, ".htm"); got != baseURL+"/xwtz/xyxw.htm" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := smePageURL(baseURL, "xwtz/xyxw", 2, 177, ".htm"); got != baseURL+"/xwtz/xyxw/176.htm" {
		t.Fatalf("page 2 URL = %q", got)
	}
	if got := smePageURL(baseURL, "xwtz/xyxw", 177, 177, ".htm"); got != baseURL+"/xwtz/xyxw/1.htm" {
		t.Fatalf("last page URL = %q", got)
	}
}
