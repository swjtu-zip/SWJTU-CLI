package scraper

import (
	"strings"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// cmAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("cm", SiteAdapter{
		ParseList:    parseAdapter_cmList,
		ParseArticle: parseAdapter_cmArticle,
	})
}

func parseAdapter_cmList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_cmArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	// CM's mobile navigation contains an <h1>首页</h1> before the article. The
	// real headline is the h4 in .dtl-tit; prefer that site-specific field
	// after the common parser has extracted the article body and metadata.
	doc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if docErr == nil {
		if title := strings.TrimSpace(doc.Find(".dtl-tit h4").First().Text()); title != "" {
			article.Title = title
		}
	}
	return article, nil
}
