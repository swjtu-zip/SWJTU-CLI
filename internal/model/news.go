package model

import "time"

type NewsItem struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Date  string `json:"date"`
	// PublishedAt preserves a source timestamp when the listing exposes a
	// time-of-day. Date remains the backwards-compatible calendar date.
	PublishedAt string `json:"published_at,omitempty"`
	// Type is the source's content/tab type when it can be identified.
	Type     string    `json:"type,omitempty"`
	ParsedAt time.Time `json:"parsed_at"`
}

type Article struct {
	Title  string `json:"title"`
	Source string `json:"source"`
	Date   string `json:"date"`
	// PublishedAt preserves minute/second precision from the source when
	// available; Date is retained for existing callers and date-only pages.
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

type Attachment struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size string `json:"size,omitempty"`
}
