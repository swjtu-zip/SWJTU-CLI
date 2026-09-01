package scraper

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// jgdwAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("jgdw", SiteAdapter{
		ParseList:    parseAdapter_jgdwList,
		ParseArticle: parseAdapter_jgdwArticle,
	})
}

func parseAdapter_jgdwList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_jgdwArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse jgdw article html: %w", err)
	}

	// jgdw puts the channel name in .position h1 and the actual article title
	// in .art-tit h3. The generic parser sees the former first, so always
	// prefer the detail heading when this site-specific block is present.
	if title := jgdwArticleTitle(doc); title != "" {
		article.Title = title
	}
	return article, nil
}

func jgdwArticleTitle(doc *goquery.Document) string {
	return strings.Join(strings.Fields(doc.Find(".art-tit h3").First().Text()), " ")
}
