package scraper

import (
	"strings"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// xinliAdapter gives this website its own extension point even when its current HTML is compatible with 博达's common parser.
func init() {
	registerAdapter("xinli", SiteAdapter{
		ParseList:    parseAdapter_xinliList,
		ParseArticle: parseAdapter_xinliArticle,
	})
}

func parseAdapter_xinliList(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseBodaNewsList(html, baseURL)
}

func parseAdapter_xinliArticle(html []byte, baseURL string) (*model.Article, error) {
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}

	// The xinli detail template renders the section heading in an earlier
	// <h1> (for example, "中心要闻"). The article headline is the .box1 .bt
	// paragraph in the detail area and must take precedence over that heading.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, err
	}
	if title := strings.TrimSpace(doc.Find(".box1 .bt").First().Text()); title != "" {
		article.Title = strings.Join(strings.Fields(title), " ")
	}
	return article, nil
}
