package scraper

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
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
		client: &http.Client{Timeout: 30 * time.Second},
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

// --- Academic Search ---

const searchBoxConfigureId = "15164479-81e6-40d1-9cc5-17fefa3905ae"

type SearchResult struct {
	Title      string `json:"title"`
	Authors    string `json:"author"`
	Journal    string `json:"journal_name"`
	Year       int    `json:"pub_year"`
	DOI        string `json:"doi"`
	Abstract   string `json:"abstract"`
	Keywords   string `json:"keyword"`
	Organ      string `json:"organ"`
	Publisher  string `json:"publisher"`
	ISSN       string `json:"issn"`
	CoreMark   string `json:"journal_core_mark"`
	DocType    int    `json:"doc_type"`
	Language   string `json:"language"`
	IsOA       string `json:"is_oa"`
	CitedCnt   string `json:"cited_cnt"`
	SourceType string `json:"srcdb_class_name"`
	CoverPath  string `json:"cover_path"`
}

// SearchType constants
const (
	SearchFullText = "U" // 全字段
	SearchTitle    = "T" // 题名
	SearchKeywords = "K" // 关键词
	SearchAuthor   = "A" // 作者
	SearchAbstract = "S" // 摘要
	SearchJournal  = "P" // 刊名
)

func (c *LibClient) Search(keyword, field string, page, size int) ([]SearchResult, int, error) {
	if field == "" {
		field = SearchFullText
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}

	// Generate a random variable name for the rule expression
	varName := fmt.Sprintf("@%s_%d_0_f", field, rand.Intn(999999))

	body := map[string]interface{}{
		"pageIndex":            page,
		"pageSize":             size,
		"keywords":             "", // Must be empty; filtering is done via rule
		"searchBoxConfigureId": searchBoxConfigureId,
		"rule": map[string]interface{}{
			"ruleBody": fmt.Sprintf("%s=%s", field, varName),
			"ruleParameters": []map[string]string{
				{"key": varName, "value": keyword},
			},
		},
		"filterRule":      map[string]interface{}{"ruleBody": "", "ruleParameters": []interface{}{}},
		"requestAggsType": 1,
	}

	data, err := c.post("/articlesearch/api/search/asset-search@4", body)
	if err != nil {
		return nil, 0, err
	}

	var resp struct {
		StatusCode int `json:"statusCode"`
		Data       struct {
			Hits struct {
				Total  int               `json:"total"`
				Source []json.RawMessage `json:"source"`
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
	for _, raw := range resp.Data.Hits.Source {
		var r SearchResult
		if err := json.Unmarshal(raw, &r); err == nil && r.Title != "" {
			results = append(results, r)
		}
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

// LibNewsSite scrapes library news from the博达 CMS news page.
var LibNewsSite = &BodaSite{
	Name:    "图书馆",
	BaseURL: "https://news.swjtu.edu.cn",
}

// Print helpers

func PrintSearchResults(results []SearchResult, total int, keyword string) {
	if len(results) == 0 {
		fmt.Println("  (无结果)")
		return
	}
	for i, r := range results {
		authors := r.Authors
		if len(authors) > 50 {
			authors = authors[:50] + "..."
		}
		journal := r.Journal
		if journal == "" {
			journal = "未知期刊"
		}
		fmt.Printf("  %2d. %s\n", i+1, r.Title)
		fmt.Printf("      %s | %s %d\n", authors, journal, r.Year)
		if r.DOI != "" {
			fmt.Printf("      DOI: %s\n", r.DOI)
		}
		if r.CoreMark != "" {
			fmt.Printf("      收录: %s\n", r.CoreMark)
		}
	}
	totalPages := (total + 19) / 20
	fmt.Printf("\n共 %d 条结果, 约 %d 页 (关键词: %s)\n", total, totalPages, keyword)
	fmt.Println("  用 --page N 查看第N页")
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
