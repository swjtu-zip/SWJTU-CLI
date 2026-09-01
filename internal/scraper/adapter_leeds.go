package scraper

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// Leeds has its own article template.  In particular, the publish date is
// rendered in .m-textq1 .info .date instead of one of the metadata blocks
// understood by the generic 博达 article parser.  Keep both hooks attached to
// the site so future Leeds list changes do not have to be folded into the
// shared parser.
func init() {
	registerAdapter("leeds", SiteAdapter{
		ParseList:    parseLeedsNews,
		ParseArticle: parseLeedsArticle,
	})
}

func parseLeedsNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseLeedsArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse leeds article html: %w", err)
	}

	// Scope the lookup to the article header.  The same page contains dates in
	// the “最新阅读” sidebar, which must never replace the publish date.
	if date := leedsArticleDate(doc); date != "" {
		article.Date = date
	}
	return article, nil
}

func leedsArticleDate(doc *goquery.Document) string {
	for _, selector := range []string{
		".m-textq1 > .info .date",
		".m-textq1 .info .date",
	} {
		date := strings.TrimSpace(doc.Find(selector).First().Text())
		if date == "" {
			continue
		}
		if match := bareDateRe.FindStringSubmatch(date); match != nil {
			if normalized := normalizeDateParts(match[1], match[2], match[3]); normalized != "" {
				return normalized
			}
		}
	}
	return ""
}
