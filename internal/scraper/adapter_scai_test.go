package scraper

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestScaiAdapterList(t *testing.T) {
	data, err := os.ReadFile("testdata/scai.html")
	if err != nil {
		t.Fatal(err)
	}
	site := AllSites["scai"]
	if site == nil || site.ParseFunc == nil {
		t.Fatal("scai adapter was not registered")
	}

	items, err := site.ParseFunc(data, site.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 47 {
		t.Fatalf("got %d items, want 47 article entries from the snapshot", len(items))
	}

	if items[0].URL != "https://scai.swjtu.edu.cn/web/page-newsDetail.html?nid=0b9fffc8-b75c-4632-affb-24c45f3438e0" {
		t.Errorf("first URL = %q", items[0].URL)
	}
	if items[0].Title != "百卅校庆系列学术论坛（四）： 学院成功举办2026年科研报奖报告会暨第一期集中辅导" {
		t.Errorf("first title = %q", items[0].Title)
	}
	if items[0].Date != "2026-06-12" {
		t.Errorf("first date = %q", items[0].Date)
	}

	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.URL] {
			t.Errorf("duplicate URL %q", item.URL)
		}
		seen[item.URL] = true
		if !strings.HasPrefix(item.URL, "https://scai.swjtu.edu.cn/web/page-newsDetail.html?nid=") {
			t.Errorf("non-article URL %q", item.URL)
		}
		if item.Title == "" || strings.HasPrefix(item.Title, "了解详情") {
			t.Errorf("bad title %q", item.Title)
		}
		if _, err := time.Parse("2006-01-02", item.Date); err != nil {
			t.Errorf("item %q has non-ISO date %q", item.Title, item.Date)
		}
	}
}

func TestScaiAdapterArticleDate(t *testing.T) {
	const articleHTML = "<!doctype html><html><body>" +
		"<header><a href='/index.html'>首页</a></header>" +
		"<main><h1 class='news-title'>学院新闻标题</h1>" +
		"<div class='article-meta'>发布日期：Tue Jul 07 00:00:00 CST 2026</div>" +
		"<div class='content-main'><p>这是正文内容。</p>" +
		"<p>活动时间：2026年9月16日，正文中的日期不能覆盖发布日期。</p></div>" +
		"</main></body></html>"

	site := AllSites["scai"]
	if site == nil || site.ParseArticleFunc == nil {
		t.Fatal("scai article adapter was not registered")
	}
	article, err := site.ParseArticleFunc([]byte(articleHTML), site.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "学院新闻标题" {
		t.Errorf("title = %q", article.Title)
	}
	if article.Date != "2026-07-07" {
		t.Errorf("date = %q, want 2026-07-07", article.Date)
	}
	if !strings.Contains(article.Content, "这是正文内容") {
		t.Errorf("content = %q", article.Content)
	}
}

func TestScaiAdapterArticleTitleUsesContentHeadline(t *testing.T) {
	data, err := os.ReadFile("testdata/scai_article.html")
	if err != nil {
		t.Fatal(err)
	}

	site := AllSites["scai"]
	if site == nil || site.ParseArticleFunc == nil {
		t.Fatal("scai article adapter was not registered")
	}
	article, err := site.ParseArticleFunc(data, site.BaseURL)
	if err != nil {
		t.Fatal(err)
	}

	const want = "计算机与人工智能学院党委理论学习中心组召开专题学习会"
	if article.Title != want {
		t.Fatalf("title = %q, want %q", article.Title, want)
	}
	if article.Title == "计算机与人工智能学院" {
		t.Fatal("article title fell back to the school name")
	}
	if article.Date != "2026-07-07" {
		t.Errorf("date = %q, want 2026-07-07", article.Date)
	}
	if !strings.Contains(article.Content, "学院召开党委理论学习中心组") {
		t.Errorf("content = %q", article.Content)
	}
}
