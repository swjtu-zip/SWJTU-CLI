package scraper

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestJournalsAdapterRegistrationAndNoGuessedFeeds(t *testing.T) {
	site := AllSites["journals"]
	if site == nil {
		t.Fatal("journals site is not registered")
	}
	if site.BaseURL != journalsCanonicalBaseURL {
		t.Fatalf("journals base URL = %q, want %q", site.BaseURL, journalsCanonicalBaseURL)
	}
	if site.ParseFunc == nil || site.ParseArticleFunc == nil || site.ArticleFetchFunc == nil {
		t.Fatal("journals adapter hooks are incomplete")
	}
	if len(site.SubPages) != 0 {
		t.Fatalf("journals has guessed article feeds: %#v", site.SubPages)
	}
	if site.PageURL != nil || site.ListPageFunc != nil || site.TotalPagesFunc != nil {
		t.Fatal("journals must not invent first/middle/last or reverse pagination for a directory-only origin")
	}
}

func TestJournalsHomepageRejectsExternalDirectoryAsNews(t *testing.T) {
	items, err := parseAdapter_journalsList(readJournalsFixture(t, "journals_home.html"), journalsCanonicalBaseURL)
	if err == nil || items != nil {
		t.Fatalf("journal directory accepted as a local article list: items=%#v err=%v", items, err)
	}
}

func TestJournalsStaticErrorAndLoginPagesAreRejected(t *testing.T) {
	for _, name := range []string{"journals_static.html", "journals_login.html", "journals_404.html"} {
		t.Run(name, func(t *testing.T) {
			body := readJournalsFixture(t, name)
			if items, err := parseAdapter_journalsList(body, journalsCanonicalBaseURL); err == nil || items != nil {
				t.Fatalf("list parser accepted %s: items=%#v err=%v", name, items, err)
			}
			if article, err := parseAdapter_journalsArticle(body, journalsCanonicalBaseURL); err == nil || article != nil {
				t.Fatalf("article parser accepted %s: article=%#v err=%v", name, article, err)
			}
		})
	}
}

func TestJournalsArticleUsesFixedMetadataAndBodyScope(t *testing.T) {
	article, err := parseAdapter_journalsArticle(readJournalsFixture(t, "journals_article.html"), journalsCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "期刊中心工作动态" || article.Source != "期刊社" {
		t.Fatalf("title/source = (%q, %q)", article.Title, article.Source)
	}
	if article.Date != "2026-08-31" || article.PublishedAt != "2026-08-31 14:34:05" {
		t.Fatalf("date metadata = (%q, %q)", article.Date, article.PublishedAt)
	}
	if article.Type != "期刊动态" {
		t.Fatalf("type = %q", article.Type)
	}
	if !strings.Contains(article.Content, "正文第一段。") || strings.Contains(article.Content, "侧栏不属于正文") {
		t.Fatalf("content escaped the fixed body scope: %q", article.Content)
	}
	if !reflect.DeepEqual(article.Images, []string{journalsCanonicalBaseURL + "/images/news.png"}) {
		t.Fatalf("images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "工作简报.pdf" || article.Attachments[0].URL != journalsCanonicalBaseURL+"/__local/brief.pdf?e=.pdf" {
		t.Fatalf("attachments = %#v", article.Attachments)
	}
}

func TestJournalsDateOnlyDoesNotReadBodyTime(t *testing.T) {
	article, err := parseAdapter_journalsArticle(readJournalsFixture(t, "journals_article_date_only.html"), journalsCanonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-08-30" || article.PublishedAt != "2026-08-30" {
		t.Fatalf("date-only metadata = (%q, %q)", article.Date, article.PublishedAt)
	}
	if strings.Contains(article.PublishedAt, "09:30") {
		t.Fatalf("body event time contaminated publication metadata: %q", article.PublishedAt)
	}
}

func TestJournalsFetchArticleVerifiesResourcesAndSanitizesHTML(t *testing.T) {
	detail := readJournalsFixture(t, "journals_article_resources.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/100/9001.htm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(detail)
		case "/images/good.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00})
		case "/images/captcha.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(w, "<html><body>请输入验证码下载附件</body></html>")
		case "/files/good.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = io.WriteString(w, "%PDF-1.7 fixture")
		case "/files/wrong.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = io.WriteString(w, "not a PDF")
		case "/files/captcha.pdf":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "<html><body>验证码下载附件</body></html>")
		case "/files/missing.pdf":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	article, raw, err := journalsFetchArticleAt(server.URL+"/info/100/9001.htm", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, detail) {
		t.Fatal("fetcher did not retain the original detail response")
	}
	if len(article.Images) != 1 || article.Images[0] != server.URL+"/images/good.png" {
		t.Fatalf("verified images = %#v", article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].URL != server.URL+"/files/good.pdf" || article.Attachments[0].Size == "" {
		t.Fatalf("verified attachments = %#v", article.Attachments)
	}
	if !strings.Contains(article.ContentHTML, server.URL+"/images/good.png") || !strings.Contains(article.ContentHTML, server.URL+"/files/good.pdf") {
		t.Fatalf("valid resources were removed from content HTML: %s", article.ContentHTML)
	}
	for _, rejected := range []string{"captcha.png", "wrong.pdf", "captcha.pdf", "missing.pdf"} {
		if strings.Contains(article.ContentHTML, rejected) {
			t.Fatalf("rejected resource %q survived content sanitization: %s", rejected, article.ContentHTML)
		}
	}
	if !strings.Contains(article.ContentHTML, "https://example.com/ordinary-link") {
		t.Fatal("ordinary external article link was removed")
	}

	if _, _, err := journalsFetchResource(server.URL+"/files/wrong.pdf", server.URL+"/info/100/9001.htm", false); err == nil {
		t.Fatal("wrong PDF magic was accepted")
	}
	if _, _, err := journalsFetchResource(server.URL+"/files/missing.pdf", server.URL+"/info/100/9001.htm", false); err == nil {
		t.Fatal("non-2xx resource was accepted")
	}
}

func TestJournalsResourceGuardsRejectPseudoCaptchaAndCrossSiteArticles(t *testing.T) {
	if _, ok := journalsArticleURL(journalsCanonicalBaseURL, "https://xnjdxb.swjtu.edu.cn/info/100/1.htm"); ok {
		t.Fatal("external journal portal was accepted as a local article")
	}
	for _, raw := range []string{"javascript:void(0)", "#", "/captcha/image.png", "/files/verifycode.pdf"} {
		if _, ok := journalsResourceURL(journalsCanonicalBaseURL, raw); ok {
			t.Fatalf("invalid resource URL %q was accepted", raw)
		}
	}
	if journalsArticleShell(readJournalsFixture(t, "journals_home.html")) {
		t.Fatal("journal directory was classified as an article shell")
	}
}

func readJournalsFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read journals fixture %q: %v", name, err)
	}
	return body
}
