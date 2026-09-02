package scraper

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"swjtu-cli/internal/model"
)

// AggResult holds results from a single site fetch.
type AggResult struct {
	SiteID string
	Site   *BodaSite
	Items  []model.NewsItem
	Err    error
}

type aggregateSite struct {
	id   string
	site *BodaSite
}

// aggregateSites returns the same feed universe exposed by the SDK: the
// university news site plus every registered unit site. The old aggregate
// path only iterated AllSites and requested each site's synthetic index feed,
// silently omitting the main news site and every configured tab.
func aggregateSites(category string) []aggregateSite {
	sites := make([]aggregateSite, 0, len(AllSites)+1)
	if category == "" || category == "university" {
		sites = append(sites, aggregateSite{id: "news", site: NewsSite})
	}
	ids := make([]string, 0, len(AllSites))
	for id := range AllSites {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		site := AllSites[id]
		if category != "" && site.Category != category {
			continue
		}
		sites = append(sites, aggregateSite{id: id, site: site})
	}
	return sites
}

// siteFeedSlugs includes the synthetic homepage and every explicitly audited
// tab. A homepage can repeat a child-tab article, so callers merge by URL.
func siteFeedSlugs(site *BodaSite) []string {
	if site == nil {
		return nil
	}
	slugs := []string{"index"}
	seen := map[string]bool{"index": true}
	for _, raw := range site.SubPages {
		slug := strings.Trim(strings.TrimSpace(raw), "/")
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		slugs = append(slugs, slug)
	}
	return slugs
}

func mergeAggregateItems(dst []model.NewsItem, src []model.NewsItem) []model.NewsItem {
	positions := make(map[string]int, len(dst)+len(src))
	for i, item := range dst {
		if item.URL != "" {
			positions[item.URL] = i
		}
	}
	for _, item := range src {
		if item.URL == "" {
			continue
		}
		if index, ok := positions[item.URL]; ok {
			if aggregateItemIsBetter(item, dst[index]) {
				dst[index] = item
			}
			continue
		}
		positions[item.URL] = len(dst)
		dst = append(dst, item)
	}
	return dst
}

func aggregateItemIsBetter(candidate, current model.NewsItem) bool {
	if strings.TrimSpace(current.Title) == "" && strings.TrimSpace(candidate.Title) != "" {
		return true
	}
	if strings.TrimSpace(current.Type) == "" && strings.TrimSpace(candidate.Type) != "" {
		return true
	}
	if strings.TrimSpace(current.Date) == "" && strings.TrimSpace(candidate.Date) != "" {
		return true
	}
	return !publishedAtHasClock(current.PublishedAt) && publishedAtHasClock(candidate.PublishedAt)
}

func listSiteNews(site *BodaSite) ([]model.NewsItem, error) {
	var items []model.NewsItem
	var failures []string
	for _, slug := range siteFeedSlugs(site) {
		pageItems, err := site.ListNews(slug)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", slug, err))
			continue
		}
		items = mergeAggregateItems(items, pageItems)
	}
	if len(failures) > 0 {
		return items, fmt.Errorf("%d feed(s) failed: %s", len(failures), strings.Join(failures, "; "))
	}
	return items, nil
}

func listSiteNewsRange(site *BodaSite, from, to string, maxPages int) ([]model.NewsItem, error) {
	var items []model.NewsItem
	var failures []string
	for _, slug := range siteFeedSlugs(site) {
		pageItems, err := site.ListNewsRange(slug, from, to, maxPages)
		items = mergeAggregateItems(items, pageItems)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", slug, err))
			continue
		}
	}
	if len(failures) > 0 {
		return items, fmt.Errorf("%d feed(s) failed: %s", len(failures), strings.Join(failures, "; "))
	}
	return items, nil
}

// FetchAllNews fetches news from all sites concurrently.
// limit: max items per site (0 = all)
func FetchAllNews(limit int) []AggResult {
	var mu sync.Mutex
	var results []AggResult
	var wg sync.WaitGroup

	for _, entry := range aggregateSites("") {
		wg.Add(1)
		go func(id string, site *BodaSite) {
			defer wg.Done()
			items, err := listSiteNews(site)
			if limit > 0 && len(items) > limit {
				items = items[:limit]
			}
			mu.Lock()
			results = append(results, AggResult{SiteID: id, Site: site, Items: items, Err: err})
			mu.Unlock()
		}(entry.id, entry.site)
	}
	wg.Wait()
	return results
}

// FetchCategoryNews fetches news from all sites in a specific category.
func FetchCategoryNews(category string, limit int) []AggResult {
	var mu sync.Mutex
	var results []AggResult
	var wg sync.WaitGroup

	for _, entry := range aggregateSites(category) {
		wg.Add(1)
		go func(id string, site *BodaSite) {
			defer wg.Done()
			items, err := listSiteNews(site)
			if limit > 0 && len(items) > limit {
				items = items[:limit]
			}
			mu.Lock()
			results = append(results, AggResult{SiteID: id, Site: site, Items: items, Err: err})
			mu.Unlock()
		}(entry.id, entry.site)
	}
	wg.Wait()
	return results
}

// AggregateNews fetches from all sites and returns a flat list sorted by date.
func AggregateNews(category string, limitPerSite int) ([]AggItem, int, int) {
	var results []AggResult
	if category == "" {
		results = FetchAllNews(limitPerSite)
	} else {
		results = FetchCategoryNews(category, limitPerSite)
	}

	var items []AggItem
	success, fail := 0, 0
	for _, r := range results {
		if r.Err != nil {
			fail++
		} else {
			success++
		}
		for _, item := range r.Items {
			items = append(items, AggItem{
				NewsItem: item,
				SiteID:   r.SiteID,
				SiteName: r.Site.Name,
			})
		}
	}

	sortAggItems(items)
	return items, success, fail
}

// AggregateNewsRange fetches historical news from all sites within a date range.
func AggregateNewsRange(category, from, to string, maxPages int) ([]AggItem, int, int) {
	var mu sync.Mutex
	var results []AggResult
	var wg sync.WaitGroup

	for _, entry := range aggregateSites(category) {
		wg.Add(1)
		go func(id string, site *BodaSite) {
			defer wg.Done()
			items, err := listSiteNewsRange(site, from, to, maxPages)
			mu.Lock()
			results = append(results, AggResult{SiteID: id, Site: site, Items: items, Err: err})
			mu.Unlock()
		}(entry.id, entry.site)
	}
	wg.Wait()

	var items []AggItem
	success, fail := 0, 0
	for _, r := range results {
		if r.Err != nil {
			fail++
		} else {
			success++
		}
		for _, item := range r.Items {
			items = append(items, AggItem{
				NewsItem: item,
				SiteID:   r.SiteID,
				SiteName: r.Site.Name,
			})
		}
	}

	sortAggItems(items)
	return items, success, fail
}

// AggItem wraps a NewsItem with its source site info.
type AggItem struct {
	model.NewsItem
	SiteID   string
	SiteName string
}

// sortAggItems sorts by date descending. Items without dates go to the end.
func sortAggItems(items []AggItem) {
	// Simple insertion sort - good enough for the expected volume
	for i := 1; i < len(items); i++ {
		key := items[i]
		j := i - 1
		for j >= 0 && compareDate(key.Date, items[j].Date) > 0 {
			items[j+1] = items[j]
			j--
		}
		items[j+1] = key
	}
}

// compareDate compares two date strings. Returns >0 if a>b, <0 if a<b, 0 if equal.
func compareDate(a, b string) int {
	ta := parseDate(a)
	tb := parseDate(b)
	if ta.After(tb) {
		return 1
	}
	if ta.Before(tb) {
		return -1
	}
	return 0
}

// parseDate tries to parse various date formats used across SWJTU sites.
func parseDate(s string) time.Time {
	formats := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"Mon Jan 02 15:04:05 MST 2006",
		"2006/01/02",
		"2006-01-02",
		"2006.01.02",
		"2006.01",
	}
	for _, f := range formats {
		t, err := time.Parse(f, s)
		if err == nil {
			return t
		}
	}
	return time.Time{}
}

// PrintAggResult prints aggregated results.
func PrintAggResult(items []AggItem, success, fail int, showSite bool) {
	if len(items) == 0 {
		fmt.Println("  (无结果)")
		return
	}
	for i, item := range items {
		date := item.Date
		if date == "" {
			date = "----.--.--"
		}
		siteTag := ""
		if showSite {
			siteTag = fmt.Sprintf(" [%s]", item.SiteName)
		}
		fmt.Printf("  %2d. [%s]%s %s\n", i+1, date, siteTag, item.Title)
		fmt.Printf("      %s\n", item.URL)
	}
	fmt.Printf("\n共 %d 条 (来自 %d 个站点", len(items), success)
	if fail > 0 {
		fmt.Printf(", %d 个失败", fail)
	}
	fmt.Println(")")
}
