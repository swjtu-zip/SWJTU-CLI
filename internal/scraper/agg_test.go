package scraper

import (
	"strings"
	"testing"

	"swjtu-cli/internal/model"
)

func TestAggregateSitesIncludesUniversityAndAllTabs(t *testing.T) {
	sites := aggregateSites("")
	if len(sites) != len(AllSites)+1 {
		t.Fatalf("aggregate sites = %d, want %d", len(sites), len(AllSites)+1)
	}
	if sites[0].id != "news" || sites[0].site != NewsSite {
		t.Fatalf("first aggregate site = %#v, want university news", sites[0])
	}

	slugs := siteFeedSlugs(&BodaSite{SubPages: []string{"foo", "/foo/", "bar"}})
	if got, want := strings.Join(slugs, ","), "index,foo,bar"; got != want {
		t.Fatalf("site feed slugs = %q, want %q", got, want)
	}
}

func TestMergeAggregateItemsKeepsBestMetadata(t *testing.T) {
	url := "https://example.com/info/1/1.htm"
	items := mergeAggregateItems(nil, []model.NewsItem{{URL: url, Title: "标题", Date: ""}})
	items = mergeAggregateItems(items, []model.NewsItem{{
		URL: url, Title: "标题", Date: "2026-08-31", PublishedAt: "2026-08-31 12:30:00", Type: "学院新闻",
	}})
	if len(items) != 1 || items[0].Date != "2026-08-31" || items[0].Type != "学院新闻" || !publishedAtHasClock(items[0].PublishedAt) {
		t.Fatalf("merged metadata = %#v", items)
	}
}
