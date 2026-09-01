package scraper

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestJournalsUsesHTTPSCanonicalAndHasNoGuessedFeeds(t *testing.T) {
	site := AllSites["journals"]
	if site == nil {
		t.Fatal("journals site is not registered")
	}
	if site.BaseURL != journalsCanonicalBaseURL {
		t.Fatalf("journals base URL = %q, want %q", site.BaseURL, journalsCanonicalBaseURL)
	}
	if len(site.SubPages) != 0 {
		t.Fatalf("journals has guessed article feeds: %#v", site.SubPages)
	}
}

func TestJournalsDirectoryDoesNotBecomeNews(t *testing.T) {
	body := mustReadJournalsFixture(t, "journals_home.html")
	items, err := parseAdapter_journalsList(body, journalsCanonicalBaseURL)
	if err == nil {
		t.Fatalf("expected the journal directory to be rejected, got %d items", len(items))
	}
	if items != nil {
		t.Fatalf("rejected directory returned items: %#v", items)
	}
}

func TestJournalsStaticAndBlockedPagesAreRejected(t *testing.T) {
	for _, name := range []string{"journals_static.html", "journals_login.html"} {
		t.Run(name, func(t *testing.T) {
			body := mustReadJournalsFixture(t, name)
			if items, err := parseAdapter_journalsList(body, journalsCanonicalBaseURL); err == nil || items != nil {
				t.Fatalf("list parser accepted %s: items=%d err=%v", name, len(items), err)
			}
			if article, err := parseAdapter_journalsArticle(body, journalsCanonicalBaseURL); err == nil || article != nil {
				t.Fatalf("article parser accepted %s: article=%#v err=%v", name, article, err)
			}
		})
	}
}

func TestJournalsArticlePreservesMetadataAndResources(t *testing.T) {
	body := mustReadJournalsFixture(t, "journals_article.html")
	article, err := parseAdapter_journalsArticle(body, journalsCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "期刊中心工作动态" {
		t.Fatalf("title = %q", article.Title)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:05" {
		t.Fatalf("date metadata = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Type != "期刊动态" {
		t.Fatalf("type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "正文第一段。") {
		t.Fatalf("content = %q", article.Content)
	}
	if !reflect.DeepEqual(article.Images, []string{journalsCanonicalBaseURL + "/images/news.png"}) {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "工作简报.pdf" ||
		article.Attachments[0].URL != journalsCanonicalBaseURL+"/__local/brief.pdf?e=.pdf" {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
	if want := `src="https://journals.swjtu.edu.cn/images/news.png"`; !strings.Contains(article.ContentHTML, want) {
		t.Fatalf("content HTML did not retain resolved image URL: %s", article.ContentHTML)
	}
	if want := `href="https://journals.swjtu.edu.cn/__local/brief.pdf?e=.pdf"`; !strings.Contains(article.ContentHTML, want) {
		t.Fatalf("content HTML did not retain resolved attachment URL: %s", article.ContentHTML)
	}
}

func mustReadJournalsFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
