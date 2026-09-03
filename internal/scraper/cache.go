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
	cacheN   int64
)

const cacheMaxBytes int64 = 64 << 20

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
		now := time.Now()
		for key, entry := range cache {
			if now.Sub(entry.fetchedAt) >= cacheTTL {
				delete(cache, key)
				cacheN -= int64(len(entry.data))
			}
		}
		if previous, ok := cache[url]; ok {
			cacheN -= int64(len(previous.data))
		}
		if int64(len(data)) <= cacheMaxBytes {
			cache[url] = cacheEntry{data: data, fetchedAt: now}
			cacheN += int64(len(data))
			for cacheN > cacheMaxBytes {
				oldestKey := ""
				var oldest time.Time
				for key, entry := range cache {
					if oldestKey == "" || entry.fetchedAt.Before(oldest) {
						oldestKey, oldest = key, entry.fetchedAt
					}
				}
				if oldestKey == "" {
					break
				}
				cacheN -= int64(len(cache[oldestKey].data))
				delete(cache, oldestKey)
			}
		} else {
			delete(cache, url)
		}
		cacheMu.Unlock()
	}

	return data, nil
}

// ClearCache clears the entire cache.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = make(map[string]cacheEntry)
	cacheN = 0
}

// CacheSize returns the number of cached entries.
func CacheSize() int {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return len(cache)
}
