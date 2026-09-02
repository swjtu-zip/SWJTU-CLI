// Package sdk exposes the SWJTU news scraping and resource-download
// capabilities for applications that need structured data instead of CLI
// output.
package sdk

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"swjtu-cli/internal/model"
	"swjtu-cli/internal/scraper"
)

// NewsItem is a structured entry from a news listing.
type NewsItem struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Date        string    `json:"date"`
	PublishedAt string    `json:"published_at,omitempty"`
	Type        string    `json:"type,omitempty"`
	ParsedAt    time.Time `json:"parsed_at"`
}

// Attachment is a document linked from an article.
type Attachment struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size string `json:"size,omitempty"`
}

// Article is the normalized representation of an article. RawHTML is
// populated by FetchArticleSnapshot and is intentionally not returned by the
// lightweight FetchArticle method.
type Article struct {
	Title        string       `json:"title"`
	Source       string       `json:"source"`
	Date         string       `json:"date"`
	PublishedAt  string       `json:"published_at,omitempty"`
	Type         string       `json:"type,omitempty"`
	Content      string       `json:"content"`
	ContentHTML  string       `json:"content_html,omitempty"`
	RawHTML      string       `json:"raw_html,omitempty"`
	Author       string       `json:"author"`
	Photographer string       `json:"photographer,omitempty"`
	Editor       string       `json:"editor,omitempty"`
	Images       []string     `json:"images,omitempty"`
	Attachments  []Attachment `json:"attachments,omitempty"`
	PrevURL      string       `json:"prev_url,omitempty"`
	PrevTitle    string       `json:"prev_title,omitempty"`
	NextURL      string       `json:"next_url,omitempty"`
	NextTitle    string       `json:"next_title,omitempty"`
}

// Feed identifies a crawlable news listing.
type Feed struct {
	ID       string `json:"id"`
	SiteID   string `json:"site_id"`
	SiteName string `json:"site_name"`
	Category string `json:"category"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
}

// Options configures a Client.
type Options struct {
	HTTPClient *http.Client
	UserAgent  string
}

// Client provides structured access to SWJTU-CLI's scraper.
type Client struct {
	httpClient *http.Client
	userAgent  string
}

// New creates a scraper client. The scraper's existing five-minute response
// cache remains enabled for listing and article requests.
func New(opts Options) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "swjtu-archive/1.0 (+https://news.swjtu.edu.cn)"
	}
	return &Client{httpClient: hc, userAgent: ua}
}

// Feeds returns the known site/category combinations in stable order.
func Feeds() []Feed {
	feeds := []Feed{
		{ID: "news:jdyw", SiteID: "news", SiteName: scraper.NewsSite.Name, Category: "university", Slug: "jdyw", Name: scraper.NewsCategories["jdyw"], BaseURL: scraper.NewsSite.BaseURL},
		{ID: "news:mtjd", SiteID: "news", SiteName: scraper.NewsSite.Name, Category: "university", Slug: "mtjd", Name: scraper.NewsCategories["mtjd"], BaseURL: scraper.NewsSite.BaseURL},
		{ID: "news:ybdt", SiteID: "news", SiteName: scraper.NewsSite.Name, Category: "university", Slug: "ybdt", Name: scraper.NewsCategories["ybdt"], BaseURL: scraper.NewsSite.BaseURL},
	}
	newsAliases := map[string]bool{"jdyw": true, "jdyw2": true, "mtjd": true, "ybdt": true}
	for _, slug := range scraper.NewsSite.SubPages {
		if newsAliases[slug] {
			continue
		}
		name := scraper.NewsFeedNames[slug]
		if name == "" {
			name = slug
		}
		feeds = append(feeds, Feed{
			ID: "news:" + slug, SiteID: "news", SiteName: scraper.NewsSite.Name,
			Category: "university", Slug: slug, Name: name, BaseURL: scraper.NewsSite.BaseURL,
		})
	}
	ids := make([]string, 0, len(scraper.AllSites))
	for id := range scraper.AllSites {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		site := scraper.AllSites[id]
		feeds = append(feeds, Feed{
			ID: id + ":index", SiteID: id, SiteName: site.Name, Category: site.Category,
			Slug: "index", Name: "首页新闻", BaseURL: site.BaseURL,
		})
		for _, slug := range site.SubPages {
			feeds = append(feeds, Feed{
				ID: id + ":" + slug, SiteID: id, SiteName: site.Name, Category: site.Category,
				Slug: slug, Name: slug, BaseURL: site.BaseURL,
			})
		}
	}
	return feeds
}

// ListFeed fetches one listing page.
func (c *Client) ListFeed(ctx context.Context, feed Feed, page int) ([]NewsItem, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	var items []model.NewsItem
	var err error
	if feed.SiteID == "news" {
		items, err = scraper.NewsSite.ListNewsPage(feed.Slug, page)
	} else {
		site, ok := scraper.AllSites[feed.SiteID]
		if !ok {
			return nil, fmt.Errorf("unknown site %q", feed.SiteID)
		}
		items, err = site.ListNewsPage(feed.Slug, page)
	}
	if err != nil {
		return nil, err
	}
	result := make([]NewsItem, 0, len(items))
	for _, item := range items {
		itemType := item.Type
		if itemType == "" {
			itemType = feed.Name
		}
		result = append(result, NewsItem{Title: item.Title, URL: item.URL, Date: item.Date, PublishedAt: item.PublishedAt, Type: itemType, ParsedAt: item.ParsedAt})
	}
	return result, contextErr(ctx)
}

// ListFeedRange fetches listing pages and keeps entries in the inclusive date
// range. A zero maxPages uses the scraper's default pagination behavior.
func (c *Client) ListFeedRange(ctx context.Context, feed Feed, from, to time.Time, maxPages int) ([]NewsItem, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	fromText := from.Format("2006-01-02")
	toText := to.Format("2006-01-02")
	var items []model.NewsItem
	var err error
	if feed.SiteID == "news" {
		items, err = scraper.NewsSite.ListNewsRange(feed.Slug, fromText, toText, maxPages)
	} else {
		site, ok := scraper.AllSites[feed.SiteID]
		if !ok {
			return nil, fmt.Errorf("unknown site %q", feed.SiteID)
		}
		items, err = site.ListNewsRange(feed.Slug, fromText, toText, maxPages)
	}
	if err != nil {
		return nil, err
	}
	result := make([]NewsItem, 0, len(items))
	for _, item := range items {
		itemType := item.Type
		if itemType == "" {
			itemType = feed.Name
		}
		result = append(result, NewsItem{Title: item.Title, URL: item.URL, Date: item.Date, PublishedAt: item.PublishedAt, Type: itemType, ParsedAt: item.ParsedAt})
	}
	return result, contextErr(ctx)
}

// FetchArticle parses an article without retaining the original response body.
func (c *Client) FetchArticle(ctx context.Context, articleURL string) (*Article, error) {
	art, _, err := c.FetchArticleSnapshot(ctx, articleURL)
	if err != nil {
		return nil, err
	}
	art.RawHTML = ""
	return art, nil
}

// FetchArticleSnapshot parses an article and returns the exact response body.
func (c *Client) FetchArticleSnapshot(ctx context.Context, articleURL string) (*Article, []byte, error) {
	if err := contextErr(ctx); err != nil {
		return nil, nil, err
	}
	site := siteForURL(articleURL)
	art, raw, err := site.GetArticleSnapshot(articleURL)
	if err != nil {
		return nil, nil, err
	}
	return convertArticle(art), raw, contextErr(ctx)
}

// Resource is a streamed response for an image or attachment.
type Resource struct {
	Body        io.ReadCloser
	ContentType string
	Filename    string
	Size        int64
}

// DownloadResource fetches an article resource. The caller must close Body.
func (c *Client) DownloadResource(ctx context.Context, resourceURL string) (*Resource, error) {
	return c.DownloadResourceWithReferer(ctx, resourceURL, "")
}

// DownloadResourceWithReferer fetches an article resource while identifying
// the detail page that linked it. A number of the university CMS download
// endpoints return a verification page to a direct request but serve the
// actual file when the browser-style Referer is present. The old method stays
// as a compatibility wrapper for callers that do not have article context.
// The caller must close Body.
func (c *Client) DownloadResourceWithReferer(ctx context.Context, resourceURL, referer string) (*Resource, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create resource request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "*/*")
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch resource %s: %w", resourceURL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch resource %s: status %d", resourceURL, resp.StatusCode)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch resource %s: received HTML instead of a downloadable resource", resourceURL)
	}
	filename := filenameFromResponse(resp, resourceURL)
	return &Resource{Body: resp.Body, ContentType: resp.Header.Get("Content-Type"), Filename: filename, Size: resp.ContentLength}, nil
}

func siteForURL(rawURL string) *scraper.BodaSite {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Hostname() != "" {
		host := strings.ToLower(parsed.Hostname())
		for _, site := range scraper.AllSites {
			base, baseErr := url.Parse(site.BaseURL)
			if baseErr == nil && strings.EqualFold(host, base.Hostname()) {
				return site
			}
		}
		if base, baseErr := url.Parse(scraper.NewsSite.BaseURL); baseErr == nil && strings.EqualFold(host, base.Hostname()) {
			return scraper.NewsSite
		}
		return scraper.NewsSite
	}

	// Preserve the old fallback for malformed or relative URLs. Normal HTTP
	// URLs take the exact-host path above; substring matching would confuse
	// sic.swjtu.edu.cn with ic.swjtu.edu.cn and silently skip SIC's adapter.
	for _, site := range scraper.AllSites {
		if strings.Contains(rawURL, site.BaseURL) || strings.Contains(rawURL, strings.TrimPrefix(site.BaseURL, "http://")) || strings.Contains(rawURL, strings.TrimPrefix(site.BaseURL, "https://")) {
			return site
		}
	}
	return scraper.NewsSite
}

func convertArticle(art *model.Article) *Article {
	result := &Article{
		Title: art.Title, Source: art.Source, Date: art.Date, PublishedAt: art.PublishedAt, Type: art.Type, Content: art.Content,
		ContentHTML: art.ContentHTML, RawHTML: art.RawHTML, Author: art.Author,
		Photographer: art.Photographer, Editor: art.Editor, Images: append([]string(nil), art.Images...),
		PrevURL: art.PrevURL, PrevTitle: art.PrevTitle, NextURL: art.NextURL, NextTitle: art.NextTitle,
	}
	for _, att := range art.Attachments {
		result.Attachments = append(result.Attachments, Attachment{Name: att.Name, URL: att.URL, Size: att.Size})
	}
	return result
}

func contextErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func filenameFromResponse(resp *http.Response, rawURL string) string {
	if value := resp.Header.Get("Content-Disposition"); value != "" {
		if _, params, err := mime.ParseMediaType(value); err == nil {
			if name := params["filename*"]; name != "" {
				if decoded, err := url.PathUnescape(strings.TrimPrefix(name, "UTF-8''")); err == nil {
					return decoded
				}
			}
			if name := params["filename"]; name != "" {
				return name
			}
		}
	}
	if parsed, err := url.Parse(rawURL); err == nil {
		if name := path.Base(parsed.Path); name != "." && name != "/" && name != "" {
			if decoded, err := url.PathUnescape(name); err == nil {
				return decoded
			}
			return name
		}
	}
	if resp.ContentLength >= 0 {
		return "resource-" + strconv.FormatInt(resp.ContentLength, 10)
	}
	return "resource"
}
