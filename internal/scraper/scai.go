package scraper

import "swjtu-cli/internal/model"

// parseScaiNews keeps the historical site-specific entry point used by
// existing callers and tests. The implementation lives with the adapter.
func parseScaiNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	return parseScaiAdapterList(html, baseURL)
}
