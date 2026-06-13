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

// LibClient handles SWJTU library API interactions.
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

// --- News (via 博达 CMS scraper since lib site is SPA) ---

// LibNewsSite scrapes library news from the博达 CMS news page.
// The library main site is SPA, but news content is also published
// through the university news system.
var LibNewsSite = &BodaSite{
	Name:    "图书馆",
	BaseURL: "https://news.swjtu.edu.cn",
}

// GetNewsCategories returns news categories relevant to the library.
func (c *LibClient) GetNewsCategories() map[string]string {
	return map[string]string{
		"tzgg": "通知公告",
		"xshd": "学术活动",
	}
}

// Print helpers

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
