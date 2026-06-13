package scraper

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"swjtu-cli/internal/model"
)

const (
	libTokenURL = "https://lib.swjtu.edu.cn/jwt/api/Auth/AccessToken"
	libGateway  = "https://lib.swjtu.edu.cn/gateway"
	libOrgCode  = "swjtu"
)

type LibClient struct {
	token     string
	expiresAt time.Time
	client    *http.Client
}

func NewLibClient() *LibClient {
	return &LibClient{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *LibClient) getToken() (string, error) {
	if c.token != "" && time.Now().Before(c.expiresAt) {
		return c.token, nil
	}
	body := fmt.Sprintf(`{"orgCode":"%s"}`, libOrgCode)
	req, _ := http.NewRequest("POST", libTokenURL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		StatusCode string `json:"statusCode"`
		Data       struct {
			Token          string `json:"token"`
			TokenExpiredAt string `json:"tokenExpiredAt"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	if result.StatusCode != "200" || result.Data.Token == "" {
		return "", fmt.Errorf("token failed: %s", result.StatusCode)
	}
	c.token = result.Data.Token
	c.expiresAt = time.Now().Add(25 * time.Minute)
	return c.token, nil
}

func (c *LibClient) post(path string, body interface{}) ([]byte, error) {
	token, err := c.getToken()
	if err != nil {
		return nil, err
	}
	u := libGateway + path
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", u, strings.NewReader(string(jsonBody)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, nil
}

func (c *LibClient) get(path string, params map[string]string) ([]byte, error) {
	token, err := c.getToken()
	if err != nil {
		return nil, err
	}
	u := libGateway + path
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		u += "?" + q.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, nil
}

// --- Search ---

type SearchResult struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	URL     string `json:"url"`
	AppType string `json:"app_type"`
	PubTime string `json:"pub_time"`
}

// SearchType constants for the library search API
const (
	SearchTypeFullText    = 254    // 全字段
	SearchTypeKeywords    = 155643 // 关键词
	SearchTypeAbstract    = 153595 // 摘要
	SearchTypeTitle       = 163161 // 题名
	SearchTypeAuthor      = 20571  // 作者
	SearchTypeJournal     = 72     // 刊名
	SearchTypeISSN        = 12     // ISSN
	SearchTypeISBN        = 2      // ISBN
)

func (c *LibClient) Search(keyword string, searchType, page, size int) ([]SearchResult, int, error) {
	if searchType == 0 {
		searchType = SearchTypeFullText
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}

	body := map[string]interface{}{
		"pageIndex": page,
		"pageSize":  size,
		"keyWord":   keyword,
		"type":      searchType,
	}

	data, err := c.post("/articlesearch/api/search/match-component", body)
	if err != nil {
		return nil, 0, err
	}

	var resp struct {
		StatusCode int `json:"statusCode"`
		Data       struct {
			Hits struct {
				Total  int `json:"total"`
				Source []struct {
					Title    string `json:"title"`
					Summary  string `json:"summary"`
					URL      string `json:"url"`
					AppType  string `json:"app_type"`
					PubTime  string `json:"pub_time"`
				} `json:"source"`
			} `json:"hits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0, fmt.Errorf("decode search: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, 0, fmt.Errorf("search failed: status %d", resp.StatusCode)
	}

	var results []SearchResult
	for _, hit := range resp.Data.Hits.Source {
		results = append(results, SearchResult{
			Title:   hit.Title,
			Summary: hit.Summary,
			URL:     hit.URL,
			AppType: hit.AppType,
			PubTime: hit.PubTime,
		})
	}
	return results, resp.Data.Hits.Total, nil
}

// --- Databases ---

type LibDatabase struct {
	Name        string `json:"databaseName"`
	EnglishAbbr string `json:"englishAbbr"`
}

func (c *LibClient) GetDatabases() ([]LibDatabase, error) {
	data, err := c.get("/articlesearch/api/search-const/available-database-collect-kind", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		StatusCode int           `json:"statusCode"`
		Data       []LibDatabase `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode databases: %w", err)
	}
	return resp.Data, nil
}

// --- Print helpers ---

func PrintSearchResults(results []SearchResult, total int, keyword string) {
	if len(results) == 0 {
		fmt.Println("  (无结果)")
		return
	}
	for i, r := range results {
		fmt.Printf("  %2d. %s\n", i+1, r.Title)
		if r.Summary != "" {
			s := r.Summary
			if len(s) > 100 {
				s = s[:100] + "..."
			}
			fmt.Printf("      %s\n", s)
		}
		if r.URL != "" {
			fmt.Printf("      %s\n", r.URL)
		}
	}
	fmt.Printf("\n共 %d 条结果 (关键词: %s)\n", total, keyword)
}

func PrintLibDatabases(items []LibDatabase) {
	if len(items) == 0 {
		fmt.Println("  (无结果)")
		return
	}
	for i, item := range items {
		fmt.Printf("  %2d. %-12s %s\n", i+1, item.EnglishAbbr, item.Name)
	}
	fmt.Printf("\n共 %d 个数据库\n", len(items))
}

// LibNewsSite scrapes library news from the博达 CMS news page.
var LibNewsSite = &BodaSite{
	Name:    "图书馆",
	BaseURL: "https://news.swjtu.edu.cn",
}

func PrintLibNews(items []model.NewsItem) {
	if len(items) == 0 {
		fmt.Println("  (无结果)")
		return
	}
	for i, item := range items {
		date := item.Date
		if date == "" {
			date = "----.--.--"
		}
		fmt.Printf("  %2d. [%s] %s\n", i+1, date, item.Title)
		fmt.Printf("      %s\n", item.URL)
	}
	fmt.Printf("\n共 %d 条\n", len(items))
}
