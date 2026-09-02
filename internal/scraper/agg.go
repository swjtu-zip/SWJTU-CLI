package scraper

import (
	"fmt"
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

// FetchAllNews fetches news from all sites concurrently.
// limit: max items per site (0 = all)
func FetchAllNews(limit int) []AggResult {
	var mu sync.Mutex
	var results []AggResult
	var wg sync.WaitGroup

	for id, site := range AllSites {
		wg.Add(1)
		go func(id string, site *BodaSite) {
			defer wg.Done()
			items, err := site.ListNews("index")
			if err != nil {
				mu.Lock()
				results = append(results, AggResult{SiteID: id, Site: site, Err: err})
				mu.Unlock()
				return
			}
			if limit > 0 && len(items) > limit {
				items = items[:limit]
			}
			mu.Lock()
			results = append(results, AggResult{SiteID: id, Site: site, Items: items})
			mu.Unlock()
		}(id, site)
	}
	wg.Wait()
	return results
}

// FetchCategoryNews fetches news from all sites in a specific category.
func FetchCategoryNews(category string, limit int) []AggResult {
	var mu sync.Mutex
	var results []AggResult
	var wg sync.WaitGroup

	for id, site := range AllSites {
		if site.Category != category {
			continue
		}
		wg.Add(1)
		go func(id string, site *BodaSite) {
			defer wg.Done()
			items, err := site.ListNews("index")
			if err != nil {
				mu.Lock()
				results = append(results, AggResult{SiteID: id, Site: site, Err: err})
				mu.Unlock()
				return
			}
			if limit > 0 && len(items) > limit {
				items = items[:limit]
			}
			mu.Lock()
			results = append(results, AggResult{SiteID: id, Site: site, Items: items})
			mu.Unlock()
		}(id, site)
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
			continue
		}
		success++
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
	sites := make(map[string]*BodaSite)
	if category == "" {
		for id, site := range AllSites {
			sites[id] = site
		}
	} else {
		for id, site := range AllSites {
			if site.Category == category {
				sites[id] = site
			}
		}
	}

	var mu sync.Mutex
	var results []AggResult
	var wg sync.WaitGroup

	for id, site := range sites {
		wg.Add(1)
		go func(id string, site *BodaSite) {
			defer wg.Done()
			items, err := site.ListNewsRange("index", from, to, maxPages)
			if err != nil {
				mu.Lock()
				results = append(results, AggResult{SiteID: id, Site: site, Err: err})
				mu.Unlock()
				return
			}
			mu.Lock()
			results = append(results, AggResult{SiteID: id, Site: site, Items: items})
			mu.Unlock()
		}(id, site)
	}
	wg.Wait()

	var items []AggItem
	success, fail := 0, 0
	for _, r := range results {
		if r.Err != nil {
			fail++
			continue
		}
		success++
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
		"01-02",
		"01.02",
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
