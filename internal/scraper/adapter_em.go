package scraper

import (
	"strings"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// emAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("em", SiteAdapter{
		ParseList:    parseAdapter_emList,
		ParseArticle: parseAdapter_emArticle,
	})
}

func parseAdapter_emList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_emArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	// em detail pages put the section heading in an earlier h1 and the real
	// headline in .show01 h5. The shared parser sees the former first.
	if doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html))); docErr == nil {
		if title := cleanTitle(strings.TrimSpace(doc.Find(".show01 h5").First().Text())); title != "" {
			article.Title = title
		}
	}
	return article, nil
}
