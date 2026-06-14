package scraper

import (
	"sync"
	"time"
)

type cacheEntry struct {
	data      []byte
	fetchedAt time.Time
}

var (
	cache    = make(map[string]cacheEntry)
	cacheMu  sync.RWMutex
	cacheTTL = 5 * time.Minute
)

// FetchWithCache fetches a URL with caching support.
// If nocache is true, bypasses the cache.
func FetchWithCache(url string, nocache bool) ([]byte, error) {
	if !nocache {
		cacheMu.RLock()
		if entry, ok := cache[url]; ok {
			if time.Since(entry.fetchedAt) < cacheTTL {
				cacheMu.RUnlock()
				return entry.data, nil
			}
		}
		cacheMu.RUnlock()
	}

	data, err := fetchRaw(url)
	if err != nil {
		return nil, err
	}

	if !nocache {
		cacheMu.Lock()
		cache[url] = cacheEntry{data: data, fetchedAt: time.Now()}
		cacheMu.Unlock()
	}

	return data, nil
}

// ClearCache clears the entire cache.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = make(map[string]cacheEntry)
}

// CacheSize returns the number of cached entries.
func CacheSize() int {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return len(cache)
}
